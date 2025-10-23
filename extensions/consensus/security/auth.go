package security

import (
	"context"
	"crypto/subtle"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// AuthManager manages authentication and authorization
type AuthManager struct {
	config   AuthConfig
	tokens   map[string]*TokenInfo
	tokensMu sync.RWMutex
	logger   forge.Logger
}

// AuthConfig contains authentication configuration
type AuthConfig struct {
	Enabled      bool
	TokenTimeout time.Duration
	AdminTokens  []string
	ReadTokens   []string
	WriteTokens  []string
	RequireAuth  bool
}

// TokenInfo contains information about an authentication token
type TokenInfo struct {
	Token     string
	Role      Role
	CreatedAt time.Time
	LastUsed  time.Time
	ExpiresAt time.Time
	Metadata  map[string]string
}

// Role represents an authorization role
type Role string

const (
	// RoleAdmin has full access
	RoleAdmin Role = "admin"
	// RoleWrite has read and write access
	RoleWrite Role = "write"
	// RoleRead has read-only access
	RoleRead Role = "read"
	// RoleNone has no access
	RoleNone Role = "none"
)

// Permission represents a permission type
type Permission string

const (
	// PermissionRead allows read operations
	PermissionRead Permission = "read"
	// PermissionWrite allows write operations
	PermissionWrite Permission = "write"
	// PermissionAdmin allows admin operations
	PermissionAdmin Permission = "admin"
)

// NewAuthManager creates a new authentication manager
func NewAuthManager(config AuthConfig, logger forge.Logger) *AuthManager {
	if config.TokenTimeout == 0 {
		config.TokenTimeout = 24 * time.Hour
	}

	am := &AuthManager{
		config: config,
		tokens: make(map[string]*TokenInfo),
		logger: logger,
	}

	// Register configured tokens
	now := time.Now()
	expiresAt := now.Add(config.TokenTimeout)

	for _, token := range config.AdminTokens {
		am.tokens[token] = &TokenInfo{
			Token:     token,
			Role:      RoleAdmin,
			CreatedAt: now,
			LastUsed:  now,
			ExpiresAt: expiresAt,
			Metadata:  make(map[string]string),
		}
	}

	for _, token := range config.WriteTokens {
		am.tokens[token] = &TokenInfo{
			Token:     token,
			Role:      RoleWrite,
			CreatedAt: now,
			LastUsed:  now,
			ExpiresAt: expiresAt,
			Metadata:  make(map[string]string),
		}
	}

	for _, token := range config.ReadTokens {
		am.tokens[token] = &TokenInfo{
			Token:     token,
			Role:      RoleRead,
			CreatedAt: now,
			LastUsed:  now,
			ExpiresAt: expiresAt,
			Metadata:  make(map[string]string),
		}
	}

	if config.Enabled {
		logger.Info("authentication enabled",
			forge.F("admin_tokens", len(config.AdminTokens)),
			forge.F("write_tokens", len(config.WriteTokens)),
			forge.F("read_tokens", len(config.ReadTokens)),
		)
	}

	return am
}

// Authenticate authenticates a token
func (am *AuthManager) Authenticate(ctx context.Context, token string) (*TokenInfo, error) {
	if !am.config.Enabled {
		// Authentication disabled, return default admin role
		return &TokenInfo{
			Role: RoleAdmin,
		}, nil
	}

	am.tokensMu.RLock()
	tokenInfo, exists := am.tokens[token]
	am.tokensMu.RUnlock()

	if !exists {
		am.logger.Warn("authentication failed - invalid token")
		return nil, fmt.Errorf("invalid token")
	}

	// Check if token has expired
	if time.Now().After(tokenInfo.ExpiresAt) {
		am.logger.Warn("authentication failed - token expired",
			forge.F("token", maskToken(token)),
		)
		return nil, fmt.Errorf("token expired")
	}

	// Update last used time
	am.tokensMu.Lock()
	tokenInfo.LastUsed = time.Now()
	am.tokensMu.Unlock()

	am.logger.Debug("authentication successful",
		forge.F("role", tokenInfo.Role),
	)

	return tokenInfo, nil
}

// AuthorizeRequest checks if a token has permission for a request
func (am *AuthManager) AuthorizeRequest(ctx context.Context, token string, permission Permission) error {
	tokenInfo, err := am.Authenticate(ctx, token)
	if err != nil {
		return err
	}

	if !am.hasPermission(tokenInfo.Role, permission) {
		am.logger.Warn("authorization failed",
			forge.F("role", tokenInfo.Role),
			forge.F("permission", permission),
		)
		return fmt.Errorf("insufficient permissions")
	}

	am.logger.Debug("authorization successful",
		forge.F("role", tokenInfo.Role),
		forge.F("permission", permission),
	)

	return nil
}

// hasPermission checks if a role has a permission
func (am *AuthManager) hasPermission(role Role, permission Permission) bool {
	switch permission {
	case PermissionRead:
		return role == RoleAdmin || role == RoleWrite || role == RoleRead
	case PermissionWrite:
		return role == RoleAdmin || role == RoleWrite
	case PermissionAdmin:
		return role == RoleAdmin
	default:
		return false
	}
}

// AddToken dynamically adds a token
func (am *AuthManager) AddToken(token string, role Role, timeout time.Duration) {
	if timeout == 0 {
		timeout = am.config.TokenTimeout
	}

	now := time.Now()
	am.tokensMu.Lock()
	am.tokens[token] = &TokenInfo{
		Token:     token,
		Role:      role,
		CreatedAt: now,
		LastUsed:  now,
		ExpiresAt: now.Add(timeout),
		Metadata:  make(map[string]string),
	}
	am.tokensMu.Unlock()

	am.logger.Info("token added",
		forge.F("role", role),
		forge.F("timeout", timeout),
	)
}

// RemoveToken removes a token
func (am *AuthManager) RemoveToken(token string) {
	am.tokensMu.Lock()
	delete(am.tokens, token)
	am.tokensMu.Unlock()

	am.logger.Info("token removed")
}

// RevokeExpiredTokens removes expired tokens
func (am *AuthManager) RevokeExpiredTokens() int {
	now := time.Now()
	expired := 0

	am.tokensMu.Lock()
	for token, info := range am.tokens {
		if now.After(info.ExpiresAt) {
			delete(am.tokens, token)
			expired++
		}
	}
	am.tokensMu.Unlock()

	if expired > 0 {
		am.logger.Info("revoked expired tokens",
			forge.F("count", expired),
		)
	}

	return expired
}

// GetTokenInfo returns information about a token
func (am *AuthManager) GetTokenInfo(token string) (*TokenInfo, error) {
	am.tokensMu.RLock()
	defer am.tokensMu.RUnlock()

	info, exists := am.tokens[token]
	if !exists {
		return nil, fmt.Errorf("token not found")
	}

	return info, nil
}

// ListTokens returns all active tokens (masked)
func (am *AuthManager) ListTokens() []TokenInfo {
	am.tokensMu.RLock()
	defer am.tokensMu.RUnlock()

	tokens := make([]TokenInfo, 0, len(am.tokens))
	for _, info := range am.tokens {
		// Create a copy with masked token
		maskedInfo := *info
		maskedInfo.Token = maskToken(info.Token)
		tokens = append(tokens, maskedInfo)
	}

	return tokens
}

// ValidateToken validates a token using constant-time comparison
func (am *AuthManager) ValidateToken(provided, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// IsEnabled returns true if authentication is enabled
func (am *AuthManager) IsEnabled() bool {
	return am.config.Enabled
}

// maskToken masks a token for logging
func maskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

// AuthMiddleware returns a middleware that enforces authentication
func (am *AuthManager) AuthMiddleware(permission Permission) func(forge.Context) error {
	return func(ctx forge.Context) error {
		if !am.config.Enabled {
			return nil // Authentication disabled
		}

		// Extract token from Authorization header
		token := ctx.Request().Header.Get("Authorization")
		if token == "" {
			return ctx.JSON(401, map[string]interface{}{
				"error": "missing authorization token",
			})
		}

		// Remove "Bearer " prefix if present
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}

		// Authenticate and authorize
		if err := am.AuthorizeRequest(ctx.Request().Context(), token, permission); err != nil {
			return ctx.JSON(403, map[string]interface{}{
				"error": err.Error(),
			})
		}

		return nil
	}
}
