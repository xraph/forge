package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xraph/forge"
)

// JWT errors
var (
	ErrJWTMissing    = errors.New("jwt token missing")
	ErrJWTInvalid    = errors.New("jwt token invalid")
	ErrJWTExpired    = errors.New("jwt token expired")
	ErrJWTNotYetValid = errors.New("jwt token not yet valid")
)

// JWTConfig holds JWT configuration
type JWTConfig struct {
	// Enabled determines if JWT authentication is enabled
	Enabled bool

	// SigningKey is the secret key for signing tokens (for HS256/HS384/HS512)
	SigningKey string

	// SigningMethod is the JWT signing algorithm
	// Options: HS256, HS384, HS512, RS256, RS384, RS512, ES256, ES384, ES512
	// Default: HS256
	SigningMethod string

	// TokenLookup defines where to find the token:
	// - "header:<name>" - look in header (default: "header:Authorization")
	// - "query:<name>" - look in query parameter
	// - "cookie:<name>" - look in cookie
	// - "form:<name>" - look in form field
	// Can specify multiple separated by comma: "header:Authorization,cookie:jwt"
	TokenLookup string

	// TokenPrefix is the prefix before the token in Authorization header
	// Default: "Bearer"
	TokenPrefix string

	// Issuer is the JWT issuer (iss claim)
	Issuer string

	// Audience is the JWT audience (aud claim)
	Audience string

	// TTL is the token time-to-live
	// Default: 1 hour
	TTL time.Duration

	// RefreshTTL is the refresh token time-to-live
	// Default: 7 days
	RefreshTTL time.Duration

	// SkipPaths is a list of paths to skip JWT authentication
	SkipPaths []string

	// AutoApplyMiddleware automatically applies JWT middleware globally
	AutoApplyMiddleware bool

	// ErrorHandler is called when JWT validation fails
	ErrorHandler func(forge.Context, error) error
}

// DefaultJWTConfig returns the default JWT configuration
func DefaultJWTConfig() JWTConfig {
	return JWTConfig{
		Enabled:             true,
		SigningMethod:       "HS256",
		TokenLookup:         "header:Authorization",
		TokenPrefix:         "Bearer",
		TTL:                 1 * time.Hour,
		RefreshTTL:          7 * 24 * time.Hour,
		SkipPaths:           []string{},
		AutoApplyMiddleware: false, // Default to false for backwards compatibility
		ErrorHandler: func(ctx forge.Context, err error) error {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
		},
	}
}

// JWTClaims represents JWT claims
type JWTClaims struct {
	UserID   string                 `json:"user_id"`
	Username string                 `json:"username,omitempty"`
	Email    string                 `json:"email,omitempty"`
	Roles    []string               `json:"roles,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	jwt.RegisteredClaims
}

// JWTManager manages JWT token generation and validation
type JWTManager struct {
	config        JWTConfig
	logger        forge.Logger
	signingMethod jwt.SigningMethod
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(config JWTConfig, logger forge.Logger) (*JWTManager, error) {
	// Set defaults
	if config.SigningMethod == "" {
		config.SigningMethod = "HS256"
	}
	if config.TokenLookup == "" {
		config.TokenLookup = "header:Authorization"
	}
	if config.TokenPrefix == "" {
		config.TokenPrefix = "Bearer"
	}
	if config.TTL == 0 {
		config.TTL = 1 * time.Hour
	}
	if config.RefreshTTL == 0 {
		config.RefreshTTL = 7 * 24 * time.Hour
	}

	// Validate signing key for HMAC methods
	if strings.HasPrefix(config.SigningMethod, "HS") && config.SigningKey == "" {
		return nil, errors.New("signing key is required for HMAC signing methods")
	}

	// Get signing method
	signingMethod := jwt.GetSigningMethod(config.SigningMethod)
	if signingMethod == nil {
		return nil, fmt.Errorf("unsupported signing method: %s", config.SigningMethod)
	}

	return &JWTManager{
		config:        config,
		logger:        logger,
		signingMethod: signingMethod,
	}, nil
}

// GenerateToken generates a new JWT token
func (jm *JWTManager) GenerateToken(claims *JWTClaims) (string, error) {
	now := time.Now()

	// Set registered claims
	claims.Issuer = jm.config.Issuer
	claims.Audience = jwt.ClaimStrings{jm.config.Audience}
	claims.IssuedAt = jwt.NewNumericDate(now)
	claims.ExpiresAt = jwt.NewNumericDate(now.Add(jm.config.TTL))
	claims.NotBefore = jwt.NewNumericDate(now)

	// Generate ID if not set
	if claims.ID == "" {
		id, err := generateTokenID()
		if err != nil {
			return "", fmt.Errorf("failed to generate token ID: %w", err)
		}
		claims.ID = id
	}

	// Create token
	token := jwt.NewWithClaims(jm.signingMethod, claims)

	// Sign token
	signedToken, err := token.SignedString([]byte(jm.config.SigningKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// GenerateRefreshToken generates a new refresh token
func (jm *JWTManager) GenerateRefreshToken(userID string) (string, error) {
	now := time.Now()

	claims := &JWTClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jm.config.Issuer,
			Audience:  jwt.ClaimStrings{jm.config.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(jm.config.RefreshTTL)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	// Generate ID
	id, err := generateTokenID()
	if err != nil {
		return "", fmt.Errorf("failed to generate token ID: %w", err)
	}
	claims.ID = id

	// Create token with "refresh" type
	token := jwt.NewWithClaims(jm.signingMethod, claims)

	// Sign token
	signedToken, err := token.SignedString([]byte(jm.config.SigningKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return signedToken, nil
}

// ValidateToken validates a JWT token and returns the claims
func (jm *JWTManager) ValidateToken(tokenString string) (*JWTClaims, error) {
	// Parse token
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if token.Method != jm.signingMethod {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jm.config.SigningKey), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrJWTExpired
		}
		if errors.Is(err, jwt.ErrTokenNotValidYet) {
			return nil, ErrJWTNotYetValid
		}
		return nil, ErrJWTInvalid
	}

	// Extract claims
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, ErrJWTInvalid
	}

	// Validate audience
	if jm.config.Audience != "" {
		validAudience := false
		for _, aud := range claims.Audience {
			if aud == jm.config.Audience {
				validAudience = true
				break
			}
		}
		if !validAudience {
			return nil, ErrJWTInvalid
		}
	}

	// Validate issuer
	if jm.config.Issuer != "" {
		if claims.Issuer != jm.config.Issuer {
			return nil, ErrJWTInvalid
		}
	}

	return claims, nil
}

// extractTokenFromRequest extracts the JWT token from request based on TokenLookup config
func (jm *JWTManager) extractTokenFromRequest(r *http.Request) string {
	lookups := strings.Split(jm.config.TokenLookup, ",")

	for _, lookup := range lookups {
		parts := strings.Split(strings.TrimSpace(lookup), ":")
		if len(parts) != 2 {
			continue
		}

		extractor := parts[0]
		key := parts[1]

		var token string
		switch extractor {
		case "header":
			auth := r.Header.Get(key)
			if auth != "" {
				// Remove prefix (e.g., "Bearer ")
				if jm.config.TokenPrefix != "" {
					prefix := jm.config.TokenPrefix + " "
					if strings.HasPrefix(auth, prefix) {
						token = strings.TrimPrefix(auth, prefix)
					}
				} else {
					token = auth
				}
			}
		case "query":
			token = r.URL.Query().Get(key)
		case "cookie":
			if cookie, err := r.Cookie(key); err == nil {
				token = cookie.Value
			}
		case "form":
			token = r.FormValue(key)
		}

		if token != "" {
			return token
		}
	}

	return ""
}

// shouldSkipPath checks if the path should skip JWT authentication
func (jm *JWTManager) shouldSkipPath(path string) bool {
	for _, skipPath := range jm.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// jwtContextKey is the context key for JWT claims
type jwtContextKey struct{}

// JWTMiddleware returns a middleware function for JWT authentication
func JWTMiddleware(jwtManager *JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !jwtManager.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip JWT authentication for specified paths
			if jwtManager.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from request
			token := jwtManager.extractTokenFromRequest(r)
			if token == "" {
				jwtManager.logger.Debug("jwt token missing",
					forge.F("path", r.URL.Path),
				)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Validate token
			claims, err := jwtManager.ValidateToken(token)
			if err != nil {
				jwtManager.logger.Debug("jwt validation failed",
					forge.F("error", err),
					forge.F("path", r.URL.Path),
				)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Store claims in context
			ctx := context.WithValue(r.Context(), jwtContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetJWTClaims retrieves JWT claims from context
func GetJWTClaims(ctx context.Context) (*JWTClaims, bool) {
	claims, ok := ctx.Value(jwtContextKey{}).(*JWTClaims)
	return claims, ok
}

// RequireRoles returns a middleware that checks if the user has required roles
func RequireRoles(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetJWTClaims(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if user has any of the required roles
			hasRole := false
			for _, requiredRole := range roles {
				for _, userRole := range claims.Roles {
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
				http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// generateTokenID generates a random token ID
func generateTokenID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

