package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// AuthMiddleware provides authentication functionality
type AuthMiddleware struct {
	*BaseMiddleware
	config AuthConfig
}

// AuthConfig represents authentication configuration
type AuthConfig struct {
	// Authentication methods
	Methods []AuthMethod `json:"methods"`

	// JWT configuration
	JWT JWTConfig `json:"jwt"`

	// API Key configuration
	APIKey APIKeyConfig `json:"api_key"`

	// Basic Auth configuration
	BasicAuth BasicAuthConfig `json:"basic_auth"`

	// Session configuration
	Session SessionConfig `json:"session"`

	// Path configuration
	SkipPaths     []string `json:"skip_paths"`
	RequiredPaths []string `json:"required_paths"`

	// Error handling
	UnauthorizedHandler func(http.ResponseWriter, *http.Request) `json:"-"`
	ForbiddenHandler    func(http.ResponseWriter, *http.Request) `json:"-"`

	// User loading
	UserLoader UserLoader `json:"-"`

	// Validation
	ValidateUser func(User) error `json:"-"`

	// Caching
	CacheUsers    bool          `json:"cache_users"`
	CacheDuration time.Duration `json:"cache_duration"`
}

// AuthMethod represents different authentication methods
type AuthMethod string

const (
	AuthMethodJWT         AuthMethod = "jwt"
	AuthMethodAPIKey      AuthMethod = "api_key"
	AuthMethodBasicAuth   AuthMethod = "basic_auth"
	AuthMethodSession     AuthMethod = "session"
	AuthMethodBearerToken AuthMethod = "bearer_token"
)

// JWTConfig represents JWT configuration
type JWTConfig struct {
	Secret           string        `json:"secret"`
	Algorithm        string        `json:"algorithm"`
	Issuer           string        `json:"issuer"`
	Audience         string        `json:"audience"`
	ExpirationTime   time.Duration `json:"expiration_time"`
	RefreshTime      time.Duration `json:"refresh_time"`
	CookieName       string        `json:"cookie_name"`
	HeaderName       string        `json:"header_name"`
	TokenLookup      string        `json:"token_lookup"`
	SkipRefresh      bool          `json:"skip_refresh"`
	ValidateExp      bool          `json:"validate_exp"`
	ValidateIssuer   bool          `json:"validate_issuer"`
	ValidateAudience bool          `json:"validate_audience"`
}

// APIKeyConfig represents API key configuration
type APIKeyConfig struct {
	HeaderName  string            `json:"header_name"`
	QueryParam  string            `json:"query_param"`
	Keys        map[string]string `json:"keys"` // key -> user_id
	Validator   APIKeyValidator   `json:"-"`
	RateLimiter APIKeyRateLimiter `json:"-"`
}

// BasicAuthConfig represents basic authentication configuration
type BasicAuthConfig struct {
	Realm     string                           `json:"realm"`
	Users     map[string]string                `json:"users"` // username -> password
	Validator BasicAuthValidator               `json:"-"`
	Hasher    func(password string) string     `json:"-"`
	Verifier  func(hash, password string) bool `json:"-"`
}

// SessionConfig represents session configuration
type SessionConfig struct {
	CookieName     string        `json:"cookie_name"`
	CookieDomain   string        `json:"cookie_domain"`
	CookiePath     string        `json:"cookie_path"`
	CookieSecure   bool          `json:"cookie_secure"`
	CookieHTTPOnly bool          `json:"cookie_http_only"`
	CookieMaxAge   int           `json:"cookie_max_age"`
	SessionTimeout time.Duration `json:"session_timeout"`
	Store          SessionStore  `json:"-"`
}

// User represents an authenticated user
type User struct {
	ID          string                 `json:"id"`
	Username    string                 `json:"username"`
	Email       string                 `json:"email"`
	Roles       []string               `json:"roles"`
	Permissions []string               `json:"permissions"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// UserLoader loads user information
type UserLoader interface {
	LoadUser(ctx context.Context, identifier string) (*User, error)
	LoadUserByAPIKey(ctx context.Context, apiKey string) (*User, error)
	LoadUserByUsername(ctx context.Context, username string) (*User, error)
}

// APIKeyValidator validates API keys
type APIKeyValidator interface {
	ValidateAPIKey(ctx context.Context, apiKey string) (*User, error)
}

// BasicAuthValidator validates basic auth credentials
type BasicAuthValidator interface {
	ValidateBasicAuth(ctx context.Context, username, password string) (*User, error)
}

// APIKeyRateLimiter provides rate limiting for API keys
type APIKeyRateLimiter interface {
	AllowAPIKey(ctx context.Context, apiKey string) bool
	RemainingRequests(ctx context.Context, apiKey string) int
}

// SessionStore manages session storage
type SessionStore interface {
	Get(ctx context.Context, sessionID string) (*User, error)
	Set(ctx context.Context, sessionID string, user *User) error
	Delete(ctx context.Context, sessionID string) error
	Exists(ctx context.Context, sessionID string) bool
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(config AuthConfig) Middleware {
	return &AuthMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"auth",
			PriorityAuth,
			"Authentication and authorization middleware",
		),
		config: config,
	}
}

// DefaultAuthConfig returns default authentication configuration
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		Methods: []AuthMethod{AuthMethodJWT, AuthMethodAPIKey},
		JWT: JWTConfig{
			Algorithm:        "HS256",
			ExpirationTime:   24 * time.Hour,
			RefreshTime:      1 * time.Hour,
			HeaderName:       "Authorization",
			TokenLookup:      "header:Authorization",
			ValidateExp:      true,
			ValidateIssuer:   false,
			ValidateAudience: false,
		},
		APIKey: APIKeyConfig{
			HeaderName: "X-API-Key",
			QueryParam: "api_key",
			Keys:       make(map[string]string),
		},
		BasicAuth: BasicAuthConfig{
			Realm: "Restricted Area",
			Users: make(map[string]string),
		},
		Session: SessionConfig{
			CookieName:     "session_id",
			CookiePath:     "/",
			CookieSecure:   true,
			CookieHTTPOnly: true,
			CookieMaxAge:   86400, // 24 hours
			SessionTimeout: 30 * time.Minute,
		},
		SkipPaths:     []string{"/health", "/metrics", "/favicon.ico"},
		CacheUsers:    true,
		CacheDuration: 5 * time.Minute,
	}
}

// Handle implements the Middleware interface
func (am *AuthMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for certain paths
		if am.shouldSkipAuth(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Try to authenticate using configured methods
		user, err := am.authenticate(r)
		if err != nil {
			am.handleAuthError(w, r, err)
			return
		}

		// No user found but authentication not required
		if user == nil && !am.isAuthRequired(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Authentication required but no user found
		if user == nil {
			am.handleUnauthorized(w, r)
			return
		}

		// Validate user if validator is configured
		if am.config.ValidateUser != nil {
			if err := am.config.ValidateUser(*user); err != nil {
				am.handleForbidden(w, r, err)
				return
			}
		}

		// Add user to context
		ctx := context.WithValue(r.Context(), router.UserIDKey, user.ID)
		ctx = context.WithValue(ctx, "user", user)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// shouldSkipAuth checks if authentication should be skipped
func (am *AuthMiddleware) shouldSkipAuth(r *http.Request) bool {
	path := r.URL.Path
	for _, skipPath := range am.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// isAuthRequired checks if authentication is required for the path
func (am *AuthMiddleware) isAuthRequired(r *http.Request) bool {
	path := r.URL.Path
	for _, requiredPath := range am.config.RequiredPaths {
		if path == requiredPath || strings.HasPrefix(path, requiredPath) {
			return true
		}
	}
	return len(am.config.RequiredPaths) == 0 // If no required paths, auth is required everywhere
}

// authenticate attempts to authenticate the user using configured methods
func (am *AuthMiddleware) authenticate(r *http.Request) (*User, error) {
	var lastError error

	for _, method := range am.config.Methods {
		user, err := am.authenticateWithMethod(r, method)
		if err != nil {
			lastError = err
			continue
		}
		if user != nil {
			return user, nil
		}
	}

	return nil, lastError
}

// authenticateWithMethod authenticates using a specific method
func (am *AuthMiddleware) authenticateWithMethod(r *http.Request, method AuthMethod) (*User, error) {
	switch method {
	case AuthMethodJWT:
		return am.authenticateJWT(r)
	case AuthMethodAPIKey:
		return am.authenticateAPIKey(r)
	case AuthMethodBasicAuth:
		return am.authenticateBasicAuth(r)
	case AuthMethodSession:
		return am.authenticateSession(r)
	case AuthMethodBearerToken:
		return am.authenticateBearerToken(r)
	default:
		return nil, fmt.Errorf("unsupported auth method: %s", method)
	}
}

// authenticateJWT authenticates using JWT token
func (am *AuthMiddleware) authenticateJWT(r *http.Request) (*User, error) {
	// Extract token from request
	token := am.extractJWTToken(r)
	if token == "" {
		return nil, nil
	}

	// Validate and parse JWT
	claims, err := am.validateJWT(token)
	if err != nil {
		return nil, fmt.Errorf("invalid JWT: %w", err)
	}

	// Load user from claims
	user, err := am.loadUserFromJWT(r.Context(), claims)
	if err != nil {
		return nil, fmt.Errorf("failed to load user from JWT: %w", err)
	}

	return user, nil
}

// authenticateAPIKey authenticates using API key
func (am *AuthMiddleware) authenticateAPIKey(r *http.Request) (*User, error) {
	// Extract API key from request
	apiKey := am.extractAPIKey(r)
	if apiKey == "" {
		return nil, nil
	}

	// Check rate limiting
	if am.config.APIKey.RateLimiter != nil {
		if !am.config.APIKey.RateLimiter.AllowAPIKey(r.Context(), apiKey) {
			return nil, fmt.Errorf("API key rate limit exceeded")
		}
	}

	// Validate API key
	if am.config.APIKey.Validator != nil {
		return am.config.APIKey.Validator.ValidateAPIKey(r.Context(), apiKey)
	}

	// Check static keys
	if userID, exists := am.config.APIKey.Keys[apiKey]; exists {
		if am.config.UserLoader != nil {
			return am.config.UserLoader.LoadUser(r.Context(), userID)
		}
		return &User{ID: userID}, nil
	}

	return nil, fmt.Errorf("invalid API key")
}

// authenticateBasicAuth authenticates using basic authentication
func (am *AuthMiddleware) authenticateBasicAuth(r *http.Request) (*User, error) {
	username, password, ok := r.BasicAuth()
	if !ok {
		return nil, nil
	}

	// Use validator if configured
	if am.config.BasicAuth.Validator != nil {
		return am.config.BasicAuth.Validator.ValidateBasicAuth(r.Context(), username, password)
	}

	// Check static users
	if expectedPassword, exists := am.config.BasicAuth.Users[username]; exists {
		if am.verifyPassword(password, expectedPassword) {
			if am.config.UserLoader != nil {
				return am.config.UserLoader.LoadUserByUsername(r.Context(), username)
			}
			return &User{ID: username, Username: username}, nil
		}
	}

	return nil, fmt.Errorf("invalid basic auth credentials")
}

// authenticateSession authenticates using session
func (am *AuthMiddleware) authenticateSession(r *http.Request) (*User, error) {
	if am.config.Session.Store == nil {
		return nil, nil
	}

	// Extract session ID from cookie
	cookie, err := r.Cookie(am.config.Session.CookieName)
	if err != nil {
		return nil, nil
	}

	// Load user from session store
	user, err := am.config.Session.Store.Get(r.Context(), cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	return user, nil
}

// authenticateBearerToken authenticates using bearer token
func (am *AuthMiddleware) authenticateBearerToken(r *http.Request) (*User, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, nil
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, nil
	}

	token := parts[1]

	// Use user loader to validate token
	if am.config.UserLoader != nil {
		return am.config.UserLoader.LoadUser(r.Context(), token)
	}

	return nil, fmt.Errorf("bearer token validation not configured")
}

// extractJWTToken extracts JWT token from request
func (am *AuthMiddleware) extractJWTToken(r *http.Request) string {
	// Check header
	authHeader := r.Header.Get(am.config.JWT.HeaderName)
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}

	// Check cookie
	if am.config.JWT.CookieName != "" {
		cookie, err := r.Cookie(am.config.JWT.CookieName)
		if err == nil {
			return cookie.Value
		}
	}

	return ""
}

// extractAPIKey extracts API key from request
func (am *AuthMiddleware) extractAPIKey(r *http.Request) string {
	// Check header
	if am.config.APIKey.HeaderName != "" {
		if apiKey := r.Header.Get(am.config.APIKey.HeaderName); apiKey != "" {
			return apiKey
		}
	}

	// Check query parameter
	if am.config.APIKey.QueryParam != "" {
		if apiKey := r.URL.Query().Get(am.config.APIKey.QueryParam); apiKey != "" {
			return apiKey
		}
	}

	return ""
}

// validateJWT validates and parses JWT token
func (am *AuthMiddleware) validateJWT(token string) (map[string]interface{}, error) {
	// This is a simplified JWT validation
	// In a real implementation, you would use a proper JWT library
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode payload
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	// Validate expiration
	if am.config.JWT.ValidateExp {
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				return nil, fmt.Errorf("JWT token expired")
			}
		}
	}

	// Validate issuer
	if am.config.JWT.ValidateIssuer && am.config.JWT.Issuer != "" {
		if iss, ok := claims["iss"].(string); !ok || iss != am.config.JWT.Issuer {
			return nil, fmt.Errorf("invalid JWT issuer")
		}
	}

	// Validate audience
	if am.config.JWT.ValidateAudience && am.config.JWT.Audience != "" {
		if aud, ok := claims["aud"].(string); !ok || aud != am.config.JWT.Audience {
			return nil, fmt.Errorf("invalid JWT audience")
		}
	}

	// Validate signature (simplified)
	if am.config.JWT.Secret != "" {
		expectedSignature := am.signJWT(parts[0] + "." + parts[1])
		if parts[2] != expectedSignature {
			return nil, fmt.Errorf("invalid JWT signature")
		}
	}

	return claims, nil
}

// signJWT signs JWT payload
func (am *AuthMiddleware) signJWT(payload string) string {
	h := hmac.New(sha256.New, []byte(am.config.JWT.Secret))
	h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// loadUserFromJWT loads user from JWT claims
func (am *AuthMiddleware) loadUserFromJWT(ctx context.Context, claims map[string]interface{}) (*User, error) {
	// Extract user ID from claims
	userID, ok := claims["sub"].(string)
	if !ok {
		return nil, fmt.Errorf("missing user ID in JWT claims")
	}

	// Load user using user loader
	if am.config.UserLoader != nil {
		return am.config.UserLoader.LoadUser(ctx, userID)
	}

	// Create user from claims
	user := &User{
		ID: userID,
	}

	if username, ok := claims["username"].(string); ok {
		user.Username = username
	}
	if email, ok := claims["email"].(string); ok {
		user.Email = email
	}
	if roles, ok := claims["roles"].([]interface{}); ok {
		user.Roles = make([]string, len(roles))
		for i, role := range roles {
			if roleStr, ok := role.(string); ok {
				user.Roles[i] = roleStr
			}
		}
	}

	return user, nil
}

// verifyPassword verifies password against hash
func (am *AuthMiddleware) verifyPassword(password, hash string) bool {
	if am.config.BasicAuth.Verifier != nil {
		return am.config.BasicAuth.Verifier(hash, password)
	}

	// Simple comparison (in production, use proper hashing)
	return password == hash
}

// Error handling methods

func (am *AuthMiddleware) handleAuthError(w http.ResponseWriter, r *http.Request, err error) {
	am.logger.Error("Authentication error", logger.Error(err))
	am.handleUnauthorized(w, r)
}

func (am *AuthMiddleware) handleUnauthorized(w http.ResponseWriter, r *http.Request) {
	if am.config.UnauthorizedHandler != nil {
		am.config.UnauthorizedHandler(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   "Unauthorized",
		"message": "Authentication required",
		"code":    "UNAUTHORIZED",
	})
}

func (am *AuthMiddleware) handleForbidden(w http.ResponseWriter, r *http.Request, err error) {
	if am.config.ForbiddenHandler != nil {
		am.config.ForbiddenHandler(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   "Forbidden",
		"message": "Access denied",
		"code":    "FORBIDDEN",
	})
}

// Configure implements the Middleware interface
func (am *AuthMiddleware) Configure(config map[string]interface{}) error {
	// Implementation would update configuration based on provided map
	return nil
}

// Health implements the Middleware interface
func (am *AuthMiddleware) Health(ctx context.Context) error {
	// Check if required components are available
	if len(am.config.Methods) == 0 {
		return fmt.Errorf("no authentication methods configured")
	}

	// Test connections to external services if configured
	if am.config.UserLoader != nil {
		// Could test user loader connectivity
	}

	return nil
}

// Utility functions

// GetUserFromContext extracts user from request context
func GetUserFromContext(ctx context.Context) *User {
	if user, ok := ctx.Value("user").(*User); ok {
		return user
	}
	return nil
}

// GetUserFromRequest extracts user from request context
func GetUserFromRequest(r *http.Request) *User {
	return GetUserFromContext(r.Context())
}

// RequireRole creates middleware that requires specific roles
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUserFromRequest(r)
			if user == nil {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			hasRole := false
			for _, requiredRole := range roles {
				for _, userRole := range user.Roles {
					if userRole == requiredRole {
						hasRole = true
						break
					}
				}
				if hasRole {
					break
				}
			}

			if !hasRole {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission creates middleware that requires specific permissions
func RequirePermission(permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUserFromRequest(r)
			if user == nil {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			hasPermission := false
			for _, requiredPerm := range permissions {
				for _, userPerm := range user.Permissions {
					if userPerm == requiredPerm {
						hasPermission = true
						break
					}
				}
				if hasPermission {
					break
				}
			}

			if !hasPermission {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Memory-based implementations for testing

// MemoryUserLoader provides in-memory user loading
type MemoryUserLoader struct {
	users map[string]*User
}

func NewMemoryUserLoader() *MemoryUserLoader {
	return &MemoryUserLoader{
		users: make(map[string]*User),
	}
}

func (m *MemoryUserLoader) LoadUser(ctx context.Context, identifier string) (*User, error) {
	if user, exists := m.users[identifier]; exists {
		return user, nil
	}
	return nil, fmt.Errorf("user not found")
}

func (m *MemoryUserLoader) LoadUserByAPIKey(ctx context.Context, apiKey string) (*User, error) {
	// Implementation would map API keys to users
	return nil, fmt.Errorf("not implemented")
}

func (m *MemoryUserLoader) LoadUserByUsername(ctx context.Context, username string) (*User, error) {
	for _, user := range m.users {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (m *MemoryUserLoader) AddUser(user *User) {
	m.users[user.ID] = user
}

// MemorySessionStore provides in-memory session storage
type MemorySessionStore struct {
	sessions map[string]*User
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]*User),
	}
}

func (m *MemorySessionStore) Get(ctx context.Context, sessionID string) (*User, error) {
	if user, exists := m.sessions[sessionID]; exists {
		return user, nil
	}
	return nil, fmt.Errorf("session not found")
}

func (m *MemorySessionStore) Set(ctx context.Context, sessionID string, user *User) error {
	m.sessions[sessionID] = user
	return nil
}

func (m *MemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

func (m *MemorySessionStore) Exists(ctx context.Context, sessionID string) bool {
	_, exists := m.sessions[sessionID]
	return exists
}
