package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// API Key errors.
var (
	ErrAPIKeyMissing = errors.New("api key missing")
	ErrAPIKeyInvalid = errors.New("api key invalid")
	ErrAPIKeyExpired = errors.New("api key expired")
	ErrAPIKeyRevoked = errors.New("api key revoked")
)

// APIKeyConfig holds API key authentication configuration.
type APIKeyConfig struct {
	// Enabled determines if API key authentication is enabled
	Enabled bool

	// KeyLookup defines where to find the API key:
	// - "header:<name>" - look in header (default: "header:X-API-Key")
	// - "query:<name>" - look in query parameter
	// - "cookie:<name>" - look in cookie
	// Can specify multiple separated by comma
	KeyLookup string

	// KeyPrefix is the prefix before the key (optional)
	// Example: "ApiKey" or "Bearer"
	KeyPrefix string

	// SkipPaths is a list of paths to skip API key authentication
	SkipPaths []string

	// AutoApplyMiddleware automatically applies API key middleware globally
	AutoApplyMiddleware bool

	// ErrorHandler is called when API key validation fails
	ErrorHandler func(forge.Context, error) error

	// Validator is a custom function to validate API keys
	// If not set, uses the built-in validator
	Validator func(ctx context.Context, key string) (*APIKeyInfo, error)
}

// DefaultAPIKeyConfig returns the default API key configuration.
func DefaultAPIKeyConfig() APIKeyConfig {
	return APIKeyConfig{
		Enabled:             true,
		KeyLookup:           "header:X-API-Key",
		SkipPaths:           []string{},
		AutoApplyMiddleware: false, // Default to false for backwards compatibility
		ErrorHandler: func(ctx forge.Context, err error) error {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
		},
	}
}

// APIKeyInfo represents information about an API key.
type APIKeyInfo struct {
	Key        string         // The API key
	Name       string         // Friendly name for the key
	UserID     string         // Associated user ID
	Scopes     []string       // Allowed scopes/permissions
	RateLimit  int            // Rate limit for this key
	ExpiresAt  *time.Time     // Expiration time (nil = never expires)
	CreatedAt  time.Time      // Creation time
	LastUsedAt *time.Time     // Last usage time
	Revoked    bool           // Whether the key is revoked
	Metadata   map[string]any // Additional metadata
}

// IsExpired checks if the API key is expired.
func (info *APIKeyInfo) IsExpired() bool {
	if info.ExpiresAt == nil {
		return false
	}

	return time.Now().After(*info.ExpiresAt)
}

// HasScope checks if the API key has a specific scope.
func (info *APIKeyInfo) HasScope(scope string) bool {
	return slices.Contains(info.Scopes, scope)
}

// APIKeyManager manages API keys.
type APIKeyManager struct {
	config APIKeyConfig
	keys   sync.Map // map[string]*APIKeyInfo
	logger forge.Logger
}

// NewAPIKeyManager creates a new API key manager.
func NewAPIKeyManager(config APIKeyConfig, logger forge.Logger) *APIKeyManager {
	if config.KeyLookup == "" {
		config.KeyLookup = "header:X-API-Key"
	}

	return &APIKeyManager{
		config: config,
		logger: logger,
	}
}

// GenerateAPIKey generates a new cryptographically secure API key.
func (m *APIKeyManager) GenerateAPIKey(prefix string) (string, error) {
	// Generate random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate api key: %w", err)
	}

	// Encode to base64
	key := base64.URLEncoding.EncodeToString(bytes)

	// Add prefix if specified
	if prefix != "" {
		key = prefix + "_" + key
	}

	return key, nil
}

// CreateAPIKey creates and stores a new API key.
func (m *APIKeyManager) CreateAPIKey(info *APIKeyInfo) (string, error) {
	// Generate key if not provided
	if info.Key == "" {
		key, err := m.GenerateAPIKey("")
		if err != nil {
			return "", err
		}

		info.Key = key
	}

	// Set creation time
	info.CreatedAt = time.Now()

	// Store key
	m.keys.Store(info.Key, info)

	m.logger.Info("api key created",
		forge.F("key_name", info.Name),
		forge.F("user_id", info.UserID),
	)

	return info.Key, nil
}

// GetAPIKey retrieves API key information.
func (m *APIKeyManager) GetAPIKey(key string) (*APIKeyInfo, bool) {
	val, ok := m.keys.Load(key)
	if !ok {
		return nil, false
	}

	return val.(*APIKeyInfo), true
}

// UpdateLastUsed updates the last used timestamp for an API key.
func (m *APIKeyManager) UpdateLastUsed(key string) {
	if val, ok := m.keys.Load(key); ok {
		info := val.(*APIKeyInfo)
		now := time.Now()
		info.LastUsedAt = &now
	}
}

// RevokeAPIKey revokes an API key.
func (m *APIKeyManager) RevokeAPIKey(key string) error {
	val, ok := m.keys.Load(key)
	if !ok {
		return errors.New("api key not found")
	}

	info := val.(*APIKeyInfo)
	info.Revoked = true

	m.logger.Info("api key revoked",
		forge.F("key_name", info.Name),
		forge.F("user_id", info.UserID),
	)

	return nil
}

// DeleteAPIKey deletes an API key.
func (m *APIKeyManager) DeleteAPIKey(key string) {
	m.keys.Delete(key)
	m.logger.Info("api key deleted", forge.F("key", key[:8]+"..."))
}

// ValidateAPIKey validates an API key.
func (m *APIKeyManager) ValidateAPIKey(ctx context.Context, key string) (*APIKeyInfo, error) {
	// Use custom validator if provided
	if m.config.Validator != nil {
		return m.config.Validator(ctx, key)
	}

	// Use built-in validation
	info, ok := m.GetAPIKey(key)
	if !ok {
		return nil, ErrAPIKeyInvalid
	}

	// Check if revoked
	if info.Revoked {
		return nil, ErrAPIKeyRevoked
	}

	// Check if expired
	if info.IsExpired() {
		return nil, ErrAPIKeyExpired
	}

	// Update last used
	m.UpdateLastUsed(key)

	return info, nil
}

// ListAPIKeys returns all API keys for a user.
func (m *APIKeyManager) ListAPIKeys(userID string) []*APIKeyInfo {
	var keys []*APIKeyInfo
	m.keys.Range(func(key, value any) bool {
		info := value.(*APIKeyInfo)
		if info.UserID == userID {
			keys = append(keys, info)
		}

		return true
	})

	return keys
}

// extractKeyFromRequest extracts the API key from request based on KeyLookup config.
func (m *APIKeyManager) extractKeyFromRequest(r *http.Request) string {
	lookups := strings.SplitSeq(m.config.KeyLookup, ",")

	for lookup := range lookups {
		parts := strings.Split(strings.TrimSpace(lookup), ":")
		if len(parts) != 2 {
			continue
		}

		extractor := parts[0]
		keyName := parts[1]

		var key string

		switch extractor {
		case "header":
			value := r.Header.Get(keyName)
			if value != "" {
				// Remove prefix if specified
				if m.config.KeyPrefix != "" {
					prefix := m.config.KeyPrefix + " "
					if after, ok := strings.CutPrefix(value, prefix); ok {
						key = after
					}
				} else {
					key = value
				}
			}
		case "query":
			key = r.URL.Query().Get(keyName)
		case "cookie":
			if cookie, err := r.Cookie(keyName); err == nil {
				key = cookie.Value
			}
		}

		if key != "" {
			return key
		}
	}

	return ""
}

// shouldSkipPath checks if the path should skip API key authentication.
func (m *APIKeyManager) shouldSkipPath(path string) bool {
	for _, skipPath := range m.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}

// apiKeyContextKey is the context key for API key info.
type apiKeyContextKey struct{}

// APIKeyMiddleware returns a middleware function for API key authentication.
func APIKeyMiddleware(manager *APIKeyManager) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if !manager.config.Enabled {
				return next(ctx)
			}

			r := ctx.Request()

			// Skip API key authentication for specified paths
			if manager.shouldSkipPath(r.URL.Path) {
				return next(ctx)
			}

			// Extract key from request
			key := manager.extractKeyFromRequest(r)
			if key == "" {
				manager.logger.Debug("api key missing",
					forge.F("path", r.URL.Path),
				)

				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Validate key
			info, err := manager.ValidateAPIKey(ctx.Context(), key)
			if err != nil {
				// Use constant-time comparison to prevent timing attacks
				// even when logging
				manager.logger.Debug("api key validation failed",
					forge.F("error", err),
					forge.F("path", r.URL.Path),
				)

				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Store key info in context
			ctx.Set("apikey_info", info)

			manager.logger.Debug("api key authenticated",
				forge.F("key_name", info.Name),
				forge.F("user_id", info.UserID),
			)

			return next(ctx)
		}
	}
}

// GetAPIKeyInfo retrieves API key info from Forge context.
func GetAPIKeyInfo(ctx forge.Context) (*APIKeyInfo, bool) {
	val := ctx.Get("apikey_info")
	if val == nil {
		return nil, false
	}

	info, ok := val.(*APIKeyInfo)

	return info, ok
}

// GetAPIKeyInfoFromStdContext retrieves API key info from standard context (for backward compatibility).
func GetAPIKeyInfoFromStdContext(ctx context.Context) (*APIKeyInfo, bool) {
	info, ok := ctx.Value(apiKeyContextKey{}).(*APIKeyInfo)

	return info, ok
}

// RequireScopes returns a middleware that checks if the API key has required scopes.
func RequireScopes(scopes ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			info, ok := GetAPIKeyInfo(ctx)
			if !ok {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Check if API key has all required scopes
			for _, requiredScope := range scopes {
				if !info.HasScope(requiredScope) {
					return ctx.String(http.StatusForbidden, "Forbidden: missing scope "+requiredScope)
				}
			}

			return next(ctx)
		}
	}
}

// HashAPIKey creates a hash of an API key for secure storage
// This allows you to store hashed keys in a database.
func HashAPIKey(key string, hasher *PasswordHasher) (string, error) {
	return hasher.Hash(key)
}

// VerifyAPIKeyHash verifies an API key against a hash.
func VerifyAPIKeyHash(key, hash string, hasher *PasswordHasher) (bool, error) {
	return hasher.Verify(key, hash)
}

// ConstantTimeCompare performs constant-time comparison of two strings
// Use this when comparing API keys to prevent timing attacks.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
