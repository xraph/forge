package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// AuthenticationProvider defines the interface for streaming authentication
type AuthenticationProvider interface {
	// Authenticate validates credentials and returns user information
	Authenticate(ctx context.Context, token string) (*AuthenticatedUser, error)

	// ValidateConnection validates if a user can establish a streaming connection
	ValidateConnection(ctx context.Context, user *AuthenticatedUser, roomID string, metadata map[string]interface{}) error

	// RefreshToken refreshes an authentication token
	RefreshToken(ctx context.Context, token string) (*AuthenticatedUser, error)

	// Authorize checks if a user is authorized for a specific action
	Authorize(ctx context.Context, user *AuthenticatedUser, action string, resource string) error
}

// AuthenticatedUser represents an authenticated user
type AuthenticatedUser struct {
	ID          string                 `json:"id"`
	Username    string                 `json:"username,omitempty"`
	Email       string                 `json:"email,omitempty"`
	Roles       []string               `json:"roles,omitempty"`
	Permissions []string               `json:"permissions,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Token       string                 `json:"token,omitempty"`
	ExpiresAt   time.Time              `json:"expires_at,omitempty"`
	IsAnonymous bool                   `json:"is_anonymous"`
}

// AuthenticationConfig contains authentication configuration
type AuthenticationConfig struct {
	Enabled           bool          `yaml:"enabled" default:"true"`
	RequireAuth       bool          `yaml:"require_auth" default:"false"`
	AllowAnonymous    bool          `yaml:"allow_anonymous" default:"true"`
	TokenExpiration   time.Duration `yaml:"token_expiration" default:"24h"`
	RefreshThreshold  time.Duration `yaml:"refresh_threshold" default:"1h"`
	MaxTokenAge       time.Duration `yaml:"max_token_age" default:"168h"`
	AllowedOrigins    []string      `yaml:"allowed_origins"`
	SecretKey         string        `yaml:"secret_key"`
	JWTIssuer         string        `yaml:"jwt_issuer" default:"forge-streaming"`
	JWTAudience       string        `yaml:"jwt_audience" default:"forge-users"`
	HeaderName        string        `yaml:"header_name" default:"Authorization"`
	QueryParamName    string        `yaml:"query_param_name" default:"token"`
	CookieName        string        `yaml:"cookie_name" default:"auth_token"`
	EnableCORS        bool          `yaml:"enable_cors" default:"true"`
	EnableRefresh     bool          `yaml:"enable_refresh" default:"true"`
	ValidationCaching bool          `yaml:"validation_caching" default:"true"`
	CacheExpiration   time.Duration `yaml:"cache_expiration" default:"5m"`
}

// DefaultAuthenticationConfig returns default authentication configuration
func DefaultAuthenticationConfig() AuthenticationConfig {
	return AuthenticationConfig{
		Enabled:           true,
		RequireAuth:       false,
		AllowAnonymous:    true,
		TokenExpiration:   24 * time.Hour,
		RefreshThreshold:  1 * time.Hour,
		MaxTokenAge:       7 * 24 * time.Hour,
		AllowedOrigins:    []string{"*"},
		JWTIssuer:         "forge-streaming",
		JWTAudience:       "forge-users",
		HeaderName:        "Authorization",
		QueryParamName:    "token",
		CookieName:        "auth_token",
		EnableCORS:        true,
		EnableRefresh:     true,
		ValidationCaching: true,
		CacheExpiration:   5 * time.Minute,
	}
}

// AuthenticationManager manages authentication for streaming connections
type AuthenticationManager struct {
	provider      AuthenticationProvider
	config        AuthenticationConfig
	logger        common.Logger
	metrics       common.Metrics
	cache         map[string]*cachedAuth
	cacheMu       sync.RWMutex
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
}

// cachedAuth represents cached authentication data
type cachedAuth struct {
	User      *AuthenticatedUser
	ExpiresAt time.Time
}

// NewAuthenticationManager creates a new authentication manager
func NewAuthenticationManager(
	provider AuthenticationProvider,
	config AuthenticationConfig,
	logger common.Logger,
	metrics common.Metrics,
) *AuthenticationManager {
	manager := &AuthenticationManager{
		provider: provider,
		config:   config,
		logger:   logger,
		metrics:  metrics,
		cache:    make(map[string]*cachedAuth),
		stopCh:   make(chan struct{}),
	}

	// Start cache cleanup if caching is enabled
	if config.ValidationCaching {
		manager.startCacheCleanup()
	}

	return manager
}

// AuthenticateConnection authenticates a streaming connection request
func (am *AuthenticationManager) AuthenticateConnection(
	ctx context.Context,
	r *http.Request,
	roomID string,
) (*AuthenticatedUser, error) {
	if !am.config.Enabled {
		return am.createAnonymousUser(), nil
	}

	// Extract token from request
	token, err := am.extractToken(r)
	if err != nil {
		if am.config.AllowAnonymous && !am.config.RequireAuth {
			return am.createAnonymousUser(), nil
		}
		return nil, common.ErrValidationError("token_extraction", err)
	}

	if token == "" {
		if am.config.AllowAnonymous && !am.config.RequireAuth {
			return am.createAnonymousUser(), nil
		}
		return nil, common.ErrValidationError("missing_token", fmt.Errorf("authentication token is required"))
	}

	// Check cache first
	if am.config.ValidationCaching {
		if cachedUser := am.getCachedAuth(token); cachedUser != nil {
			return cachedUser, nil
		}
	}

	// Authenticate with provider
	user, err := am.provider.Authenticate(ctx, token)
	if err != nil {
		if am.metrics != nil {
			am.metrics.Counter("streaming.auth.failures").Inc()
		}

		if am.config.AllowAnonymous && !am.config.RequireAuth {
			if am.logger != nil {
				am.logger.Warn("authentication failed, allowing anonymous access",
					logger.Error(err),
				)
			}
			return am.createAnonymousUser(), nil
		}

		return nil, common.ErrValidationError("authentication_failed", err)
	}

	// Validate connection permissions
	metadata := extractMetadata(r)
	if err := am.provider.ValidateConnection(ctx, user, roomID, metadata); err != nil {
		if am.metrics != nil {
			am.metrics.Counter("streaming.auth.authorization_failures").Inc()
		}
		return nil, common.ErrValidationError("connection_validation_failed", err)
	}

	// Cache the result
	if am.config.ValidationCaching {
		am.setCachedAuth(token, user)
	}

	if am.metrics != nil {
		am.metrics.Counter("streaming.auth.successes").Inc()
	}

	if am.logger != nil {
		am.logger.Debug("user authenticated for streaming connection",
			logger.String("user_id", user.ID),
			logger.String("room_id", roomID),
			logger.Bool("is_anonymous", user.IsAnonymous),
		)
	}

	return user, nil
}

// AuthorizeAction checks if a user is authorized for a specific action
func (am *AuthenticationManager) AuthorizeAction(
	ctx context.Context,
	user *AuthenticatedUser,
	action string,
	resource string,
) error {
	if !am.config.Enabled || user.IsAnonymous {
		// Allow all actions for anonymous users if anonymous access is enabled
		if am.config.AllowAnonymous {
			return nil
		}
		return common.ErrValidationError("unauthorized", fmt.Errorf("authentication required for action: %s", action))
	}

	return am.provider.Authorize(ctx, user, action, resource)
}

// RefreshUserToken refreshes a user's authentication token
func (am *AuthenticationManager) RefreshUserToken(ctx context.Context, token string) (*AuthenticatedUser, error) {
	if !am.config.Enabled || !am.config.EnableRefresh {
		return nil, common.ErrValidationError("refresh_disabled", fmt.Errorf("token refresh is disabled"))
	}

	user, err := am.provider.RefreshToken(ctx, token)
	if err != nil {
		return nil, common.ErrValidationError("refresh_failed", err)
	}

	// Update cache
	if am.config.ValidationCaching {
		am.setCachedAuth(user.Token, user)
		// Remove old token from cache
		am.removeCachedAuth(token)
	}

	if am.logger != nil {
		am.logger.Debug("user token refreshed",
			logger.String("user_id", user.ID),
		)
	}

	return user, nil
}

// extractToken extracts authentication token from various sources
func (am *AuthenticationManager) extractToken(r *http.Request) (string, error) {
	// 1. Check Authorization header
	if authHeader := r.Header.Get(am.config.HeaderName); authHeader != "" {
		// Handle Bearer token format
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer "), nil
		}
		return authHeader, nil
	}

	// 2. Check query parameter
	if token := r.URL.Query().Get(am.config.QueryParamName); token != "" {
		return token, nil
	}

	// 3. Check cookie
	if cookie, err := r.Cookie(am.config.CookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}

	// 4. Check WebSocket subprotocol (for WebSocket connections)
	if protocols := r.Header.Get("Sec-WebSocket-Protocol"); protocols != "" {
		protocolList := strings.Split(protocols, ",")
		for _, protocol := range protocolList {
			protocol = strings.TrimSpace(protocol)
			if strings.HasPrefix(protocol, "token.") {
				return strings.TrimPrefix(protocol, "token."), nil
			}
		}
	}

	return "", nil
}

// extractMetadata extracts metadata from the request
func extractMetadata(r *http.Request) map[string]interface{} {
	metadata := make(map[string]interface{})

	metadata["remote_addr"] = r.RemoteAddr
	metadata["user_agent"] = r.UserAgent()
	metadata["referer"] = r.Referer()

	// Extract custom headers
	for name, values := range r.Header {
		if strings.HasPrefix(name, "X-") {
			if len(values) == 1 {
				metadata[strings.ToLower(name)] = values[0]
			} else {
				metadata[strings.ToLower(name)] = values
			}
		}
	}

	// Extract query parameters
	for name, values := range r.URL.Query() {
		if name != "token" && name != "auth" {
			if len(values) == 1 {
				metadata["query_"+name] = values[0]
			} else {
				metadata["query_"+name] = values
			}
		}
	}

	return metadata
}

// createAnonymousUser creates an anonymous user
func (am *AuthenticationManager) createAnonymousUser() *AuthenticatedUser {
	return &AuthenticatedUser{
		ID:          fmt.Sprintf("anon_%d", time.Now().UnixNano()),
		Username:    "anonymous",
		IsAnonymous: true,
		Roles:       []string{"anonymous"},
		Permissions: []string{"connect", "read"},
		Metadata:    make(map[string]interface{}),
		ExpiresAt:   time.Now().Add(am.config.TokenExpiration),
	}
}

// getCachedAuth retrieves cached authentication
func (am *AuthenticationManager) getCachedAuth(token string) *AuthenticatedUser {
	am.cacheMu.RLock()
	defer am.cacheMu.RUnlock()

	if cached, exists := am.cache[token]; exists {
		if time.Now().Before(cached.ExpiresAt) {
			return cached.User
		}
		// Remove expired entry
		delete(am.cache, token)
	}

	return nil
}

// setCachedAuth caches authentication result
func (am *AuthenticationManager) setCachedAuth(token string, user *AuthenticatedUser) {
	am.cacheMu.Lock()
	defer am.cacheMu.Unlock()

	am.cache[token] = &cachedAuth{
		User:      user,
		ExpiresAt: time.Now().Add(am.config.CacheExpiration),
	}
}

// removeCachedAuth removes cached authentication
func (am *AuthenticationManager) removeCachedAuth(token string) {
	am.cacheMu.Lock()
	defer am.cacheMu.Unlock()
	delete(am.cache, token)
}

// startCacheCleanup starts the cache cleanup routine
func (am *AuthenticationManager) startCacheCleanup() {
	am.cleanupTicker = time.NewTicker(am.config.CacheExpiration / 2)

	go func() {
		defer am.cleanupTicker.Stop()

		for {
			select {
			case <-am.cleanupTicker.C:
				am.cleanupExpiredCache()
			case <-am.stopCh:
				return
			}
		}
	}()
}

// cleanupExpiredCache removes expired cache entries
func (am *AuthenticationManager) cleanupExpiredCache() {
	am.cacheMu.Lock()
	defer am.cacheMu.Unlock()

	now := time.Now()
	for token, cached := range am.cache {
		if now.After(cached.ExpiresAt) {
			delete(am.cache, token)
		}
	}
}

// Stop stops the authentication manager
func (am *AuthenticationManager) Stop() {
	close(am.stopCh)
	if am.cleanupTicker != nil {
		am.cleanupTicker.Stop()
	}
}

// ConnectionAuthMiddleware creates middleware for connection authentication
func (am *AuthenticationManager) ConnectionAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract room ID from request
			roomID := r.URL.Query().Get("room_id")

			// Authenticate connection
			user, err := am.AuthenticateConnection(ctx, r, roomID)
			if err != nil {
				if am.logger != nil {
					am.logger.Warn("connection authentication failed",
						logger.String("remote_addr", r.RemoteAddr),
						logger.Error(err),
					)
				}

				http.Error(w, "Authentication failed", http.StatusUnauthorized)
				return
			}

			// Add user to request context
			ctx = context.WithValue(ctx, "authenticated_user", user)
			r = r.WithContext(ctx)

			// Add CORS headers if enabled
			if am.config.EnableCORS {
				am.addCORSHeaders(w, r)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// addCORSHeaders adds CORS headers to the response
func (am *AuthenticationManager) addCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" && am.isOriginAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
	}
}

// isOriginAllowed checks if an origin is allowed
func (am *AuthenticationManager) isOriginAllowed(origin string) bool {
	for _, allowed := range am.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// GetAuthenticatedUser retrieves the authenticated user from context
func GetAuthenticatedUser(ctx context.Context) (*AuthenticatedUser, bool) {
	user, ok := ctx.Value("authenticated_user").(*AuthenticatedUser)
	return user, ok
}

// Default implementations

// SimpleAuthenticationProvider provides a simple in-memory authentication provider
type SimpleAuthenticationProvider struct {
	users  map[string]*AuthenticatedUser
	mu     sync.RWMutex
	logger common.Logger
}

// NewSimpleAuthenticationProvider creates a new simple authentication provider
func NewSimpleAuthenticationProvider(logger common.Logger) *SimpleAuthenticationProvider {
	return &SimpleAuthenticationProvider{
		users:  make(map[string]*AuthenticatedUser),
		logger: logger,
	}
}

// AddUser adds a user to the simple provider
func (p *SimpleAuthenticationProvider) AddUser(token string, user *AuthenticatedUser) {
	p.mu.Lock()
	defer p.mu.Unlock()
	user.Token = token
	p.users[token] = user
}

// RemoveUser removes a user from the simple provider
func (p *SimpleAuthenticationProvider) RemoveUser(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.users, token)
}

// Authenticate validates a token and returns user information
func (p *SimpleAuthenticationProvider) Authenticate(ctx context.Context, token string) (*AuthenticatedUser, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	user, exists := p.users[token]
	if !exists {
		return nil, fmt.Errorf("invalid token")
	}

	// Check if token is expired
	if !user.ExpiresAt.IsZero() && time.Now().After(user.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	return user, nil
}

// ValidateConnection validates if a user can establish a streaming connection
func (p *SimpleAuthenticationProvider) ValidateConnection(
	ctx context.Context,
	user *AuthenticatedUser,
	roomID string,
	metadata map[string]interface{},
) error {
	// Simple validation - check if user has connect permission
	for _, permission := range user.Permissions {
		if permission == "connect" || permission == "*" {
			return nil
		}
	}

	return fmt.Errorf("user does not have connect permission")
}

// RefreshToken refreshes an authentication token
func (p *SimpleAuthenticationProvider) RefreshToken(ctx context.Context, token string) (*AuthenticatedUser, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	user, exists := p.users[token]
	if !exists {
		return nil, fmt.Errorf("invalid token")
	}

	// Generate new token
	newToken := fmt.Sprintf("token_%d", time.Now().UnixNano())

	// Update user
	newUser := *user
	newUser.Token = newToken
	newUser.ExpiresAt = time.Now().Add(24 * time.Hour)

	// Update in map
	delete(p.users, token)
	p.users[newToken] = &newUser

	return &newUser, nil
}

// Authorize checks if a user is authorized for a specific action
func (p *SimpleAuthenticationProvider) Authorize(
	ctx context.Context,
	user *AuthenticatedUser,
	action string,
	resource string,
) error {
	// Simple authorization - check permissions
	for _, permission := range user.Permissions {
		if permission == action || permission == "*" {
			return nil
		}
	}

	return fmt.Errorf("user does not have permission for action: %s", action)
}

// Authentication actions
const (
	ActionConnect    = "connect"
	ActionDisconnect = "disconnect"
	ActionSend       = "send"
	ActionReceive    = "receive"
	ActionJoinRoom   = "join_room"
	ActionLeaveRoom  = "leave_room"
	ActionCreateRoom = "create_room"
	ActionDeleteRoom = "delete_room"
	ActionModerate   = "moderate"
	ActionAdmin      = "admin"
)

// Common permissions
const (
	PermissionConnect    = "connect"
	PermissionSend       = "send"
	PermissionReceive    = "receive"
	PermissionJoinRoom   = "join_room"
	PermissionLeaveRoom  = "leave_room"
	PermissionCreateRoom = "create_room"
	PermissionDeleteRoom = "delete_room"
	PermissionModerate   = "moderate"
	PermissionAdmin      = "admin"
	PermissionAll        = "*"
)
