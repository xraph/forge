package auth

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/middleware"
)

// JWTConfig contains configuration for JWT authentication
type JWTConfig struct {
	SecretKey        string        `yaml:"secret_key" json:"secret_key"`
	PublicKey        string        `yaml:"public_key" json:"public_key"`
	PrivateKey       string        `yaml:"private_key" json:"private_key"`
	Algorithm        string        `yaml:"algorithm" json:"algorithm" default:"HS256"`
	TokenLookup      string        `yaml:"token_lookup" json:"token_lookup" default:"header:Authorization"`
	AuthScheme       string        `yaml:"auth_scheme" json:"auth_scheme" default:"Bearer"`
	ContextKey       string        `yaml:"context_key" json:"context_key" default:"user"`
	ClaimsContextKey string        `yaml:"claims_context_key" json:"claims_context_key" default:"jwt_claims"`
	ExpirationTime   time.Duration `yaml:"expiration_time" json:"expiration_time" default:"24h"`
	SkipPaths        []string      `yaml:"skip_paths" json:"skip_paths"`
	RequiredClaims   []string      `yaml:"required_claims" json:"required_claims"`
	Issuer           string        `yaml:"issuer" json:"issuer"`
	Audience         string        `yaml:"audience" json:"audience"`
}

// JWTMiddleware implements JWT authentication middleware
type JWTMiddleware struct {
	*middleware.BaseServiceMiddleware
	config     JWTConfig
	secretKey  interface{}
	publicKey  *rsa.PublicKey
	privateKey *rsa.PrivateKey
}

// JWTClaims represents JWT claims
type JWTClaims struct {
	UserID   string                 `json:"user_id"`
	Username string                 `json:"username"`
	Email    string                 `json:"email"`
	Roles    []string               `json:"roles"`
	Scopes   []string               `json:"scopes"`
	Custom   map[string]interface{} `json:"custom,omitempty"`
	jwt.RegisteredClaims
}

// NewJWTMiddleware creates a new JWT authentication middleware
func NewJWTMiddleware(config JWTConfig) *JWTMiddleware {
	return &JWTMiddleware{
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware("jwt-auth", 10, []string{"config-manager"}),
		config:                config,
	}
}

// Initialize initializes the JWT middleware
func (j *JWTMiddleware) Initialize(container common.Container) error {
	if err := j.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// Load configuration from container if needed
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		var jwtConfig JWTConfig
		if err := configManager.(common.ConfigManager).Bind("auth.jwt", &jwtConfig); err == nil {
			j.config = jwtConfig
		}
	}

	// Set defaults
	if j.config.Algorithm == "" {
		j.config.Algorithm = "HS256"
	}
	if j.config.TokenLookup == "" {
		j.config.TokenLookup = "header:Authorization"
	}
	if j.config.AuthScheme == "" {
		j.config.AuthScheme = "Bearer"
	}
	if j.config.ContextKey == "" {
		j.config.ContextKey = "user"
	}
	if j.config.ClaimsContextKey == "" {
		j.config.ClaimsContextKey = "jwt_claims"
	}

	// Initialize keys based on algorithm
	if err := j.initializeKeys(); err != nil {
		return fmt.Errorf("failed to initialize JWT keys: %w", err)
	}

	return nil
}

// Handler returns the JWT authentication handler
func (j *JWTMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			j.UpdateStats(1, 0, 0, nil)

			// Check if path should be skipped
			if j.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				j.UpdateStats(0, 0, time.Since(start), nil)
				return
			}

			// Extract token from request
			token, err := j.extractToken(r)
			if err != nil {
				j.UpdateStats(0, 1, time.Since(start), err)
				j.writeUnauthorizedResponse(w, "Missing or invalid token")
				return
			}

			// Validate and parse token
			claims, err := j.validateToken(token)
			if err != nil {
				j.UpdateStats(0, 1, time.Since(start), err)
				j.writeUnauthorizedResponse(w, "Invalid token")
				return
			}

			// Validate required claims
			if err := j.validateRequiredClaims(claims); err != nil {
				j.UpdateStats(0, 1, time.Since(start), err)
				j.writeUnauthorizedResponse(w, "Missing required claims")
				return
			}

			// Create new context with user information
			ctx := j.addUserToContext(r.Context(), claims)
			r = r.WithContext(ctx)

			// Continue to next handler
			next.ServeHTTP(w, r)

			// Update latency statistics
			j.UpdateStats(0, 0, time.Since(start), nil)
		})
	}
}

// initializeKeys initializes the cryptographic keys
func (j *JWTMiddleware) initializeKeys() error {
	switch j.config.Algorithm {
	case "HS256", "HS384", "HS512":
		if j.config.SecretKey == "" {
			return fmt.Errorf("secret key is required for HMAC algorithms")
		}
		j.secretKey = []byte(j.config.SecretKey)

	case "RS256", "RS384", "RS512":
		if j.config.PublicKey == "" {
			return fmt.Errorf("public key is required for RSA algorithms")
		}

		// Parse public key
		publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(j.config.PublicKey))
		if err != nil {
			return fmt.Errorf("failed to parse RSA public key: %w", err)
		}
		j.publicKey = publicKey
		j.secretKey = publicKey

		// Parse private key if provided (for token generation)
		if j.config.PrivateKey != "" {
			privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(j.config.PrivateKey))
			if err != nil {
				return fmt.Errorf("failed to parse RSA private key: %w", err)
			}
			j.privateKey = privateKey
		}

	default:
		return fmt.Errorf("unsupported JWT algorithm: %s", j.config.Algorithm)
	}

	return nil
}

// extractToken extracts the JWT token from the request
func (j *JWTMiddleware) extractToken(r *http.Request) (string, error) {
	// Parse token lookup format: "header:Authorization", "query:token", "cookie:jwt"
	parts := strings.Split(j.config.TokenLookup, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid token lookup format")
	}

	lookupType := parts[0]
	lookupKey := parts[1]

	var tokenString string

	switch lookupType {
	case "header":
		authHeader := r.Header.Get(lookupKey)
		if authHeader == "" {
			return "", fmt.Errorf("missing authorization header")
		}

		// Remove auth scheme (e.g., "Bearer ")
		if j.config.AuthScheme != "" {
			prefix := j.config.AuthScheme + " "
			if !strings.HasPrefix(authHeader, prefix) {
				return "", fmt.Errorf("invalid authorization scheme")
			}
			tokenString = strings.TrimPrefix(authHeader, prefix)
		} else {
			tokenString = authHeader
		}

	case "query":
		tokenString = r.URL.Query().Get(lookupKey)
		if tokenString == "" {
			return "", fmt.Errorf("missing token in query parameter")
		}

	case "cookie":
		cookie, err := r.Cookie(lookupKey)
		if err != nil {
			return "", fmt.Errorf("missing token in cookie")
		}
		tokenString = cookie.Value

	default:
		return "", fmt.Errorf("unsupported token lookup type: %s", lookupType)
	}

	if tokenString == "" {
		return "", fmt.Errorf("empty token")
	}

	return tokenString, nil
}

// validateToken validates and parses the JWT token
func (j *JWTMiddleware) validateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate algorithm
		if token.Method.Alg() != j.config.Algorithm {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}

	// Validate registered claims
	if j.config.Issuer != "" && claims.Issuer != j.config.Issuer {
		return nil, fmt.Errorf("invalid issuer")
	}

	// todo: fix this
	// if j.config.Audience != "" && !claims.VerifyAudience(j.config.Audience, true) {
	// 	return nil, fmt.Errorf("invalid audience")
	// }

	return claims, nil
}

// validateRequiredClaims validates that required claims are present
func (j *JWTMiddleware) validateRequiredClaims(claims *JWTClaims) error {
	for _, requiredClaim := range j.config.RequiredClaims {
		switch requiredClaim {
		case "user_id":
			if claims.UserID == "" {
				return fmt.Errorf("missing required claim: user_id")
			}
		case "username":
			if claims.Username == "" {
				return fmt.Errorf("missing required claim: username")
			}
		case "email":
			if claims.Email == "" {
				return fmt.Errorf("missing required claim: email")
			}
		case "roles":
			if len(claims.Roles) == 0 {
				return fmt.Errorf("missing required claim: roles")
			}
		case "scopes":
			if len(claims.Scopes) == 0 {
				return fmt.Errorf("missing required claim: scopes")
			}
		default:
			// Check custom claims
			if claims.Custom == nil {
				return fmt.Errorf("missing required claim: %s", requiredClaim)
			}
			if _, exists := claims.Custom[requiredClaim]; !exists {
				return fmt.Errorf("missing required claim: %s", requiredClaim)
			}
		}
	}
	return nil
}

// addUserToContext adds user information to the request context
func (j *JWTMiddleware) addUserToContext(ctx context.Context, claims *JWTClaims) context.Context {
	// Add user information
	user := map[string]interface{}{
		"user_id":  claims.UserID,
		"username": claims.Username,
		"email":    claims.Email,
		"roles":    claims.Roles,
		"scopes":   claims.Scopes,
	}

	// Add custom claims
	if claims.Custom != nil {
		for key, value := range claims.Custom {
			user[key] = value
		}
	}

	ctx = context.WithValue(ctx, j.config.ContextKey, user)
	ctx = context.WithValue(ctx, j.config.ClaimsContextKey, claims)

	return ctx
}

// shouldSkipPath checks if the current path should skip authentication
func (j *JWTMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range j.config.SkipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// writeUnauthorizedResponse writes an unauthorized response
func (j *JWTMiddleware) writeUnauthorizedResponse(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "UNAUTHORIZED",
			"message": message,
		},
	}

	// Don't handle the error here to avoid infinite loops
	json.NewEncoder(w).Encode(response)
}

// GenerateToken generates a new JWT token with the given claims
func (j *JWTMiddleware) GenerateToken(claims *JWTClaims) (string, error) {
	if j.privateKey == nil && j.secretKey == nil {
		return "", fmt.Errorf("no signing key configured")
	}

	// Set default claims
	now := time.Now()
	if claims.IssuedAt == nil {
		claims.IssuedAt = jwt.NewNumericDate(now)
	}
	if claims.ExpiresAt == nil {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(j.config.ExpirationTime))
	}
	if claims.NotBefore == nil {
		claims.NotBefore = jwt.NewNumericDate(now)
	}
	if j.config.Issuer != "" {
		claims.Issuer = j.config.Issuer
	}
	if j.config.Audience != "" {
		claims.Audience = []string{j.config.Audience}
	}

	// Create token
	token := jwt.NewWithClaims(jwt.GetSigningMethod(j.config.Algorithm), claims)

	// Sign token
	var signingKey interface{}
	if j.privateKey != nil {
		signingKey = j.privateKey
	} else {
		signingKey = j.secretKey
	}

	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// GetUserFromContext extracts user information from context
func GetUserFromContext(ctx context.Context, contextKey string) (map[string]interface{}, bool) {
	if contextKey == "" {
		contextKey = "user"
	}

	user, ok := ctx.Value(contextKey).(map[string]interface{})
	return user, ok
}

// GetClaimsFromContext extracts JWT claims from context
func GetClaimsFromContext(ctx context.Context, claimsContextKey string) (*JWTClaims, bool) {
	if claimsContextKey == "" {
		claimsContextKey = "jwt_claims"
	}

	claims, ok := ctx.Value(claimsContextKey).(*JWTClaims)
	return claims, ok
}

// HasRole checks if the user has a specific role
func HasRole(ctx context.Context, role string, contextKey string) bool {
	user, ok := GetUserFromContext(ctx, contextKey)
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

// HasScope checks if the user has a specific scope
func HasScope(ctx context.Context, scope string, contextKey string) bool {
	user, ok := GetUserFromContext(ctx, contextKey)
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
