package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/middleware"
)

// APIKeyConfig contains configuration for API key authentication
type APIKeyConfig struct {
	// Token extraction
	TokenLookup    string `yaml:"token_lookup" json:"token_lookup" default:"header:X-API-Key"`
	AuthScheme     string `yaml:"auth_scheme" json:"auth_scheme" default:""`
	ContextKey     string `yaml:"context_key" json:"context_key" default:"api_key"`
	UserContextKey string `yaml:"user_context_key" json:"user_context_key" default:"user"`

	// Key validation
	Keys           map[string]APIKeyInfo `yaml:"keys" json:"keys"`
	CaseSensitive  bool                  `yaml:"case_sensitive" json:"case_sensitive" default:"true"`
	RequiredScopes []string              `yaml:"required_scopes" json:"required_scopes"`

	// Security
	SkipPaths   []string      `yaml:"skip_paths" json:"skip_paths"`
	RateLimit   int           `yaml:"rate_limit" json:"rate_limit" default:"1000"`
	RateWindow  time.Duration `yaml:"rate_window" json:"rate_window" default:"1h"`
	LogAttempts bool          `yaml:"log_attempts" json:"log_attempts" default:"true"`

	// External validation
	ValidationURL     string        `yaml:"validation_url" json:"validation_url"`
	ValidationMethod  string        `yaml:"validation_method" json:"validation_method" default:"GET"`
	ValidationTimeout time.Duration `yaml:"validation_timeout" json:"validation_timeout" default:"5s"`
	CacheValidation   bool          `yaml:"cache_validation" json:"cache_validation" default:"true"`
	CacheTTL          time.Duration `yaml:"cache_ttl" json:"cache_ttl" default:"5m"`
}

// APIKeyInfo contains information about an API key
type APIKeyInfo struct {
	Name        string            `yaml:"name" json:"name"`
	UserID      string            `yaml:"user_id" json:"user_id"`
	Username    string            `yaml:"username" json:"username"`
	Email       string            `yaml:"email" json:"email"`
	Roles       []string          `yaml:"roles" json:"roles"`
	Scopes      []string          `yaml:"scopes" json:"scopes"`
	Permissions []string          `yaml:"permissions" json:"permissions"`
	Metadata    map[string]string `yaml:"metadata" json:"metadata"`
	Active      bool              `yaml:"active" json:"active" default:"true"`
	CreatedAt   time.Time         `yaml:"created_at" json:"created_at"`
	ExpiresAt   *time.Time        `yaml:"expires_at" json:"expires_at"`
	LastUsed    *time.Time        `yaml:"last_used" json:"last_used"`
	UsageCount  int64             `yaml:"usage_count" json:"usage_count"`
}

// APIKeyMiddleware implements API key authentication middleware
type APIKeyMiddleware struct {
	*middleware.BaseServiceMiddleware
	config          APIKeyConfig
	validationCache map[string]*cacheEntry
	usageStats      map[string]*usageStats
	httpClient      *http.Client
}

// ValidationResponse represents external validation response
type ValidationResponse struct {
	Valid  bool        `json:"valid"`
	UserID string      `json:"user_id"`
	User   *APIKeyInfo `json:"user"`
	Error  string      `json:"error"`
}

// cacheEntry represents a cached validation result
type cacheEntry struct {
	result    *APIKeyInfo
	timestamp time.Time
}

// usageStats tracks API key usage statistics
type usageStats struct {
	requestCount int64
	lastUsed     time.Time
	errorCount   int64
}

// NewAPIKeyMiddleware creates a new API key authentication middleware
func NewAPIKeyMiddleware(config APIKeyConfig) *APIKeyMiddleware {
	return &APIKeyMiddleware{
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware("apikey-auth", 15, []string{"config-manager"}),
		config:                config,
		validationCache:       make(map[string]*cacheEntry),
		usageStats:            make(map[string]*usageStats),
		httpClient: &http.Client{
			Timeout: config.ValidationTimeout,
		},
	}
}

// Initialize initializes the API key middleware
func (am *APIKeyMiddleware) Initialize(container common.Container) error {
	if err := am.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// Load configuration from container if needed
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		var apiKeyConfig APIKeyConfig
		if err := configManager.(common.ConfigManager).Bind("auth.apikey", &apiKeyConfig); err == nil {
			// Merge with provided config
			if len(apiKeyConfig.Keys) > 0 {
				am.config.Keys = apiKeyConfig.Keys
			}
			if apiKeyConfig.ValidationURL != "" {
				am.config.ValidationURL = apiKeyConfig.ValidationURL
			}
			if len(apiKeyConfig.SkipPaths) > 0 {
				am.config.SkipPaths = apiKeyConfig.SkipPaths
			}
		}
	}

	// Set defaults
	if am.config.TokenLookup == "" {
		am.config.TokenLookup = "header:X-API-Key"
	}
	if am.config.ContextKey == "" {
		am.config.ContextKey = "api_key"
	}
	if am.config.UserContextKey == "" {
		am.config.UserContextKey = "user"
	}
	if am.config.ValidationMethod == "" {
		am.config.ValidationMethod = "GET"
	}
	if am.config.ValidationTimeout == 0 {
		am.config.ValidationTimeout = 5 * time.Second
	}
	if am.config.CacheTTL == 0 {
		am.config.CacheTTL = 5 * time.Minute
	}
	if am.config.RateLimit == 0 {
		am.config.RateLimit = 1000
	}
	if am.config.RateWindow == 0 {
		am.config.RateWindow = time.Hour
	}

	// Initialize keys map if nil
	if am.config.Keys == nil {
		am.config.Keys = make(map[string]APIKeyInfo)
	}

	// Set created time for keys without it
	now := time.Now()
	for key, info := range am.config.Keys {
		if info.CreatedAt.IsZero() {
			info.CreatedAt = now
		}
		if info.Active == false && info.Active != true {
			info.Active = true // Default to active
		}
		am.config.Keys[key] = info
	}

	return nil
}

// Handler returns the API key authentication handler
func (am *APIKeyMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			am.UpdateStats(1, 0, 0, nil)

			// Check if path should be skipped
			if am.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				am.UpdateStats(0, 0, time.Since(start), nil)
				return
			}

			// Extract API key from request
			apiKey, err := am.extractAPIKey(r)
			if err != nil {
				am.UpdateStats(0, 1, time.Since(start), err)
				am.writeUnauthorizedResponse(w, "Missing or invalid API key")
				return
			}

			// Validate API key
			keyInfo, err := am.validateAPIKey(r.Context(), apiKey)
			if err != nil {
				am.UpdateStats(0, 1, time.Since(start), err)
				am.writeUnauthorizedResponse(w, "Invalid API key")
				return
			}

			// Check if key is active
			if !keyInfo.Active {
				am.UpdateStats(0, 1, time.Since(start), fmt.Errorf("inactive API key"))
				am.writeUnauthorizedResponse(w, "API key is inactive")
				return
			}

			// Check expiration
			if keyInfo.ExpiresAt != nil && time.Now().After(*keyInfo.ExpiresAt) {
				am.UpdateStats(0, 1, time.Since(start), fmt.Errorf("expired API key"))
				am.writeUnauthorizedResponse(w, "API key has expired")
				return
			}

			// Check required scopes
			if err := am.validateScopes(keyInfo); err != nil {
				am.UpdateStats(0, 1, time.Since(start), err)
				am.writeForbiddenResponse(w, "Insufficient permissions")
				return
			}

			// Check rate limiting
			if am.isRateLimited(apiKey) {
				am.UpdateStats(0, 1, time.Since(start), fmt.Errorf("rate limit exceeded"))
				am.writeRateLimitResponse(w, "Rate limit exceeded")
				return
			}

			// Update usage statistics
			am.updateUsage(apiKey)

			// Create new context with API key information
			ctx := am.addAPIKeyToContext(r.Context(), apiKey, keyInfo)
			r = r.WithContext(ctx)

			// Log successful authentication if configured
			if am.config.LogAttempts && am.Logger() != nil {
				am.Logger().Info("API key authentication successful",
					logger.String("api_key", am.maskAPIKey(apiKey)),
					logger.String("user_id", keyInfo.UserID),
					logger.String("username", keyInfo.Username),
					logger.String("path", r.URL.Path),
				)
			}

			// Continue to next handler
			next.ServeHTTP(w, r)

			// Update latency statistics
			am.UpdateStats(0, 0, time.Since(start), nil)
		})
	}
}

// extractAPIKey extracts the API key from the request
func (am *APIKeyMiddleware) extractAPIKey(r *http.Request) (string, error) {
	// Parse token lookup format: "header:X-API-Key", "query:api_key", "cookie:api_key"
	parts := strings.Split(am.config.TokenLookup, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid token lookup format")
	}

	lookupType := parts[0]
	lookupKey := parts[1]

	var apiKey string

	switch lookupType {
	case "header":
		authHeader := r.Header.Get(lookupKey)
		if authHeader == "" {
			return "", fmt.Errorf("missing API key header")
		}

		// Remove auth scheme if specified
		if am.config.AuthScheme != "" {
			prefix := am.config.AuthScheme + " "
			if !strings.HasPrefix(authHeader, prefix) {
				return "", fmt.Errorf("invalid authorization scheme")
			}
			apiKey = strings.TrimPrefix(authHeader, prefix)
		} else {
			apiKey = authHeader
		}

	case "query":
		apiKey = r.URL.Query().Get(lookupKey)
		if apiKey == "" {
			return "", fmt.Errorf("missing API key in query parameter")
		}

	case "cookie":
		cookie, err := r.Cookie(lookupKey)
		if err != nil {
			return "", fmt.Errorf("missing API key in cookie")
		}
		apiKey = cookie.Value

	default:
		return "", fmt.Errorf("unsupported token lookup type: %s", lookupType)
	}

	if apiKey == "" {
		return "", fmt.Errorf("empty API key")
	}

	return apiKey, nil
}

// validateAPIKey validates the API key
func (am *APIKeyMiddleware) validateAPIKey(ctx context.Context, apiKey string) (*APIKeyInfo, error) {
	// Check cache first if external validation is enabled
	if am.config.ValidationURL != "" && am.config.CacheValidation {
		if entry, exists := am.validationCache[apiKey]; exists {
			if time.Since(entry.timestamp) < am.config.CacheTTL {
				return entry.result, nil
			}
			// Remove expired entry
			delete(am.validationCache, apiKey)
		}
	}

	// Try external validation first if configured
	if am.config.ValidationURL != "" {
		keyInfo, err := am.validateAPIKeyExternal(ctx, apiKey)
		if err == nil {
			// Cache the result
			if am.config.CacheValidation {
				am.validationCache[apiKey] = &cacheEntry{
					result:    keyInfo,
					timestamp: time.Now(),
				}
			}
			return keyInfo, nil
		}
		// If external validation fails, fall back to local validation
		if am.Logger() != nil {
			am.Logger().Warn("external API key validation failed, falling back to local",
				logger.String("api_key", am.maskAPIKey(apiKey)),
				logger.Error(err),
			)
		}
	}

	// Local validation
	return am.validateAPIKeyLocal(apiKey)
}

// validateAPIKeyExternal validates API key using external service
func (am *APIKeyMiddleware) validateAPIKeyExternal(ctx context.Context, apiKey string) (*APIKeyInfo, error) {
	// Create request to validation service
	req, err := http.NewRequestWithContext(ctx, am.config.ValidationMethod, am.config.ValidationURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create validation request: %w", err)
	}

	// Add API key to request
	if am.config.ValidationMethod == "GET" {
		q := req.URL.Query()
		q.Add("api_key", apiKey)
		req.URL.RawQuery = q.Encode()
	} else {
		req.Header.Set("X-API-Key", apiKey)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Forge-APIKey-Middleware/1.0")

	// Make request
	resp, err := am.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("validation request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("validation failed with status %d", resp.StatusCode)
	}

	// Parse response
	var validationResp ValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&validationResp); err != nil {
		return nil, fmt.Errorf("failed to decode validation response: %w", err)
	}

	if !validationResp.Valid {
		return nil, fmt.Errorf("API key validation failed: %s", validationResp.Error)
	}

	if validationResp.User == nil {
		return nil, fmt.Errorf("no user information in validation response")
	}

	return validationResp.User, nil
}

// validateAPIKeyLocal validates API key using local configuration
func (am *APIKeyMiddleware) validateAPIKeyLocal(apiKey string) (*APIKeyInfo, error) {
	// Look up API key in configuration
	for configKey, keyInfo := range am.config.Keys {
		var match bool
		if am.config.CaseSensitive {
			match = subtle.ConstantTimeCompare([]byte(apiKey), []byte(configKey)) == 1
		} else {
			match = strings.EqualFold(apiKey, configKey)
		}

		if match {
			// Create a copy to avoid modifying the original
			result := keyInfo
			return &result, nil
		}
	}

	return nil, fmt.Errorf("API key not found")
}

// validateScopes validates that the API key has required scopes
func (am *APIKeyMiddleware) validateScopes(keyInfo *APIKeyInfo) error {
	if len(am.config.RequiredScopes) == 0 {
		return nil
	}

	keyScopes := make(map[string]bool)
	for _, scope := range keyInfo.Scopes {
		keyScopes[scope] = true
	}

	for _, requiredScope := range am.config.RequiredScopes {
		if !keyScopes[requiredScope] {
			return fmt.Errorf("missing required scope: %s", requiredScope)
		}
	}

	return nil
}

// isRateLimited checks if the API key is rate limited
func (am *APIKeyMiddleware) isRateLimited(apiKey string) bool {
	if am.config.RateLimit <= 0 {
		return false
	}

	stats, exists := am.usageStats[apiKey]
	if !exists {
		return false
	}

	// Check if window has expired
	if time.Since(stats.lastUsed) > am.config.RateWindow {
		stats.requestCount = 0
		return false
	}

	return stats.requestCount >= int64(am.config.RateLimit)
}

// updateUsage updates usage statistics for the API key
func (am *APIKeyMiddleware) updateUsage(apiKey string) {
	now := time.Now()

	stats, exists := am.usageStats[apiKey]
	if !exists {
		am.usageStats[apiKey] = &usageStats{
			requestCount: 1,
			lastUsed:     now,
		}
		return
	}

	// Reset counter if window has passed
	if time.Since(stats.lastUsed) > am.config.RateWindow {
		stats.requestCount = 1
	} else {
		stats.requestCount++
	}

	stats.lastUsed = now

	// Update key info if it exists in local config
	if keyInfo, exists := am.config.Keys[apiKey]; exists {
		keyInfo.UsageCount++
		keyInfo.LastUsed = &now
		am.config.Keys[apiKey] = keyInfo
	}
}

// addAPIKeyToContext adds API key information to the request context
func (am *APIKeyMiddleware) addAPIKeyToContext(ctx context.Context, apiKey string, keyInfo *APIKeyInfo) context.Context {
	// Add API key
	ctx = context.WithValue(ctx, am.config.ContextKey, apiKey)

	// Add user information
	user := map[string]interface{}{
		"user_id":     keyInfo.UserID,
		"username":    keyInfo.Username,
		"email":       keyInfo.Email,
		"roles":       keyInfo.Roles,
		"scopes":      keyInfo.Scopes,
		"permissions": keyInfo.Permissions,
		"name":        keyInfo.Name,
		"metadata":    keyInfo.Metadata,
		"auth_method": "api_key",
	}

	ctx = context.WithValue(ctx, am.config.UserContextKey, user)

	return ctx
}

// shouldSkipPath checks if the current path should skip authentication
func (am *APIKeyMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range am.config.SkipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// maskAPIKey masks an API key for logging
func (am *APIKeyMiddleware) maskAPIKey(apiKey string) string {
	if len(apiKey) <= 8 {
		return "***"
	}
	return apiKey[:4] + "***" + apiKey[len(apiKey)-4:]
}

// writeUnauthorizedResponse writes an unauthorized response
func (am *APIKeyMiddleware) writeUnauthorizedResponse(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "UNAUTHORIZED",
			"message": message,
		},
	}

	json.NewEncoder(w).Encode(response)
}

// writeForbiddenResponse writes a forbidden response
func (am *APIKeyMiddleware) writeForbiddenResponse(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "FORBIDDEN",
			"message": message,
		},
	}

	json.NewEncoder(w).Encode(response)
}

// writeRateLimitResponse writes a rate limit response
func (am *APIKeyMiddleware) writeRateLimitResponse(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", am.config.RateLimit))
	w.Header().Set("X-RateLimit-Window", am.config.RateWindow.String())
	w.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "RATE_LIMIT_EXCEEDED",
			"message": message,
		},
	}

	json.NewEncoder(w).Encode(response)
}

// GetAPIKeyFromContext extracts API key from context
func GetAPIKeyFromContext(ctx context.Context, contextKey string) (string, bool) {
	if contextKey == "" {
		contextKey = "api_key"
	}

	apiKey, ok := ctx.Value(contextKey).(string)
	return apiKey, ok
}

// GetAPIKeyUserFromContext extracts user information from context
func GetAPIKeyUserFromContext(ctx context.Context, userContextKey string) (map[string]interface{}, bool) {
	if userContextKey == "" {
		userContextKey = "user"
	}

	user, ok := ctx.Value(userContextKey).(map[string]interface{})
	return user, ok
}

// HasAPIKeyRole checks if the API key user has a specific role
func HasAPIKeyRole(ctx context.Context, role string, userContextKey string) bool {
	user, ok := GetAPIKeyUserFromContext(ctx, userContextKey)
	if !ok {
		return false
	}

	roles, ok := user["roles"].([]string)
	if !ok {
		return false
	}

	for _, userRole := range roles {
		if userRole == role {
			return true
		}
	}

	return false
}

// HasAPIKeyScope checks if the API key user has a specific scope
func HasAPIKeyScope(ctx context.Context, scope string, userContextKey string) bool {
	user, ok := GetAPIKeyUserFromContext(ctx, userContextKey)
	if !ok {
		return false
	}

	scopes, ok := user["scopes"].([]string)
	if !ok {
		return false
	}

	for _, userScope := range scopes {
		if userScope == scope {
			return true
		}
	}

	return false
}

// HasAPIKeyPermission checks if the API key user has a specific permission
func HasAPIKeyPermission(ctx context.Context, permission string, userContextKey string) bool {
	user, ok := GetAPIKeyUserFromContext(ctx, userContextKey)
	if !ok {
		return false
	}

	permissions, ok := user["permissions"].([]string)
	if !ok {
		return false
	}

	for _, userPermission := range permissions {
		if userPermission == permission {
			return true
		}
	}

	return false
}

// GetUsageStats returns usage statistics for all API keys
func (am *APIKeyMiddleware) GetUsageStats() map[string]usageStats {
	result := make(map[string]usageStats)
	for key, stats := range am.usageStats {
		result[am.maskAPIKey(key)] = *stats
	}
	return result
}

// ClearValidationCache clears the validation cache
func (am *APIKeyMiddleware) ClearValidationCache() {
	am.validationCache = make(map[string]*cacheEntry)
}

// HealthCheck performs a health check on the middleware
func (am *APIKeyMiddleware) HealthCheck(ctx context.Context) error {
	if err := am.BaseServiceMiddleware.HealthCheck(ctx); err != nil {
		return err
	}

	// Test external validation if configured
	if am.config.ValidationURL != "" {
		testCtx, cancel := context.WithTimeout(ctx, am.config.ValidationTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(testCtx, "GET", am.config.ValidationURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create health check request: %w", err)
		}

		resp, err := am.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("external validation service unreachable: %w", err)
		}
		resp.Body.Close()
	}

	return nil
}
