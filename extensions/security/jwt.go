package security

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xraph/forge"
)

// JWT errors.
var (
	ErrJWTMissing     = errors.New("jwt token missing")
	ErrJWTInvalid     = errors.New("jwt token invalid")
	ErrJWTExpired     = errors.New("jwt token expired")
	ErrJWTNotYetValid = errors.New("jwt token not yet valid")
	// ErrJWTWrongType is returned when a refresh token is presented where an
	// access token is expected, or the other way round.
	ErrJWTWrongType = errors.New("jwt token has the wrong type")
)

// Token types carried in the "typ" claim.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// JWTConfig holds JWT configuration.
type JWTConfig struct {
	// Enabled determines if JWT authentication is enabled
	Enabled bool `yaml:"enabled"`

	// SigningKey signs tokens. For HS256/HS384/HS512 it is the shared
	// secret. For RS*, PS*, ES* and EdDSA it is the PEM-encoded private key,
	// and the public key used for verification is derived from it.
	SigningKey string `yaml:"signing_key"`

	// PublicKey is an optional PEM-encoded public key for RS*, PS*, ES* and
	// EdDSA. When set it is used to verify tokens, which lets a service that
	// only verifies leave SigningKey empty. Ignored for HMAC methods.
	PublicKey string `yaml:"public_key"`

	// RefreshSigningKey is an optional separate key for refresh tokens, in
	// the same format as SigningKey. When set, a leaked access key cannot be
	// used to mint refresh tokens. Defaults to SigningKey.
	RefreshSigningKey string `yaml:"refresh_signing_key"`

	// SigningMethod is the JWT signing algorithm
	// Options: HS256, HS384, HS512, RS256, RS384, RS512, ES256, ES384, ES512
	// Default: HS256
	SigningMethod string `yaml:"signing_method"`

	// TokenLookup defines where to find the token:
	// - "header:<name>" - look in header (default: "header:Authorization")
	// - "query:<name>" - look in query parameter
	// - "cookie:<name>" - look in cookie
	// - "form:<name>" - look in form field
	// Can specify multiple separated by comma: "header:Authorization,cookie:jwt"
	TokenLookup string `yaml:"token_lookup"`

	// TokenPrefix is the prefix before the token in Authorization header
	// Default: "Bearer"
	TokenPrefix string `yaml:"token_prefix"`

	// Issuer is the JWT issuer (iss claim)
	Issuer string `yaml:"issuer"`

	// Audience is the JWT audience (aud claim)
	Audience string `yaml:"audience"`

	// TTL is the token time-to-live
	// Default: 1 hour
	TTL time.Duration `yaml:"ttl"`

	// RefreshTTL is the refresh token time-to-live
	// Default: 7 days
	RefreshTTL time.Duration `yaml:"refresh_ttl"`

	// SkipPaths is a list of paths to skip JWT authentication
	SkipPaths []string `yaml:"skip_paths"`

	// AutoApplyMiddleware automatically applies JWT middleware globally
	AutoApplyMiddleware bool `yaml:"auto_apply_middleware"`

	// ErrorHandler is called when JWT validation fails
	ErrorHandler func(forge.Context, error) error `yaml:"-"`
}

// DefaultJWTConfig returns the default JWT configuration.
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

// JWTClaims represents JWT claims.
type JWTClaims struct {
	UserID   string         `json:"user_id"`
	Username string         `json:"username,omitempty"`
	Email    string         `json:"email,omitempty"`
	Roles    []string       `json:"roles,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	//nolint:embeddedstructfieldcheck // moving it reorders an exported struct and breaks callers' unkeyed literals
	jwt.RegisteredClaims

	// TokenType is "access" or "refresh". The manager sets it when it signs a
	// token and requires the right value when it validates one, so a refresh
	// token can never be used as an access token.
	TokenType string `json:"typ,omitempty"`
}

// jwtKeys holds the key pair for one token type. sign is nil for a manager
// that only verifies.
type jwtKeys struct {
	sign   any
	verify any
}

// tokenSource is one parsed entry of a TokenLookup or KeyLookup setting.
type tokenSource struct {
	source string
	key    string
}

// parseTokenLookup splits "header:Authorization,cookie:jwt" into sources once,
// so requests do not re-split the setting.
func parseTokenLookup(lookup string) []tokenSource {
	var sources []tokenSource

	for entry := range strings.SplitSeq(lookup, ",") {
		source, key, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || source == "" || key == "" {
			continue
		}

		sources = append(sources, tokenSource{source: source, key: key})
	}

	return sources
}

// JWTManager manages JWT token generation and validation.
type JWTManager struct {
	config        JWTConfig
	logger        forge.Logger
	signingMethod jwt.SigningMethod
	accessKeys    jwtKeys
	refreshKeys   jwtKeys
	parser        *jwt.Parser
	lookups       []tokenSource
}

// NewJWTManager creates a new JWT manager.
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

	// Get signing method. "none" is refused outright: an unsigned token
	// must never authenticate anyone.
	if strings.EqualFold(config.SigningMethod, "none") {
		return nil, errors.New("jwt signing method \"none\" is not allowed")
	}

	signingMethod := jwt.GetSigningMethod(config.SigningMethod)
	if signingMethod == nil {
		return nil, fmt.Errorf("unsupported signing method: %s", config.SigningMethod)
	}

	accessKeys, err := parseJWTKeys(signingMethod, config.SigningKey, config.PublicKey)
	if err != nil {
		return nil, err
	}

	refreshKeys := accessKeys
	if config.RefreshSigningKey != "" {
		refreshKeys, err = parseJWTKeys(signingMethod, config.RefreshSigningKey, "")
		if err != nil {
			return nil, fmt.Errorf("refresh signing key: %w", err)
		}
	}

	// Pin the algorithm, require an expiry and check iss/aud in the parser
	// itself, so a token without exp is not accepted as valid forever.
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithExpirationRequired(),
	}
	if config.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(config.Issuer))
	}

	if config.Audience != "" {
		opts = append(opts, jwt.WithAudience(config.Audience))
	}

	return &JWTManager{
		config:        config,
		logger:        logger,
		signingMethod: signingMethod,
		accessKeys:    accessKeys,
		refreshKeys:   refreshKeys,
		parser:        jwt.NewParser(opts...),
		lookups:       parseTokenLookup(config.TokenLookup),
	}, nil
}

// parseJWTKeys turns the configured key material into sign and verify keys
// for method. HMAC methods take the secret as bytes. Asymmetric methods take
// PEM, so a misconfigured key fails here instead of making every token fail
// verification later.
func parseJWTKeys(method jwt.SigningMethod, signingKey, publicKey string) (jwtKeys, error) {
	switch m := method.(type) {
	case *jwt.SigningMethodHMAC:
		if signingKey == "" {
			return jwtKeys{}, errors.New("signing key is required for HMAC signing methods")
		}

		key := []byte(signingKey)

		return jwtKeys{sign: key, verify: key}, nil

	case *jwt.SigningMethodRSA, *jwt.SigningMethodRSAPSS:
		var keys jwtKeys

		if signingKey != "" {
			priv, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(signingKey))
			if err != nil {
				return jwtKeys{}, fmt.Errorf("signing key for %s must be a PEM RSA private key: %w", method.Alg(), err)
			}

			keys = jwtKeys{sign: priv, verify: &priv.PublicKey}
		}

		if publicKey != "" {
			pub, err := jwt.ParseRSAPublicKeyFromPEM([]byte(publicKey))
			if err != nil {
				return jwtKeys{}, fmt.Errorf("public key for %s must be a PEM RSA public key: %w", method.Alg(), err)
			}

			keys.verify = pub
		}

		return requireVerifyKey(method, keys)

	case *jwt.SigningMethodECDSA:
		var keys jwtKeys

		if signingKey != "" {
			priv, err := jwt.ParseECPrivateKeyFromPEM([]byte(signingKey))
			if err != nil {
				return jwtKeys{}, fmt.Errorf("signing key for %s must be a PEM EC private key: %w", method.Alg(), err)
			}

			if priv.Curve.Params().BitSize != m.CurveBits {
				return jwtKeys{}, fmt.Errorf("signing key curve is P-%d but %s needs P-%d", priv.Curve.Params().BitSize, method.Alg(), m.CurveBits)
			}

			keys = jwtKeys{sign: priv, verify: &priv.PublicKey}
		}

		if publicKey != "" {
			pub, err := jwt.ParseECPublicKeyFromPEM([]byte(publicKey))
			if err != nil {
				return jwtKeys{}, fmt.Errorf("public key for %s must be a PEM EC public key: %w", method.Alg(), err)
			}

			if pub.Curve.Params().BitSize != m.CurveBits {
				return jwtKeys{}, fmt.Errorf("public key curve is P-%d but %s needs P-%d", pub.Curve.Params().BitSize, method.Alg(), m.CurveBits)
			}

			keys.verify = pub
		}

		return requireVerifyKey(method, keys)

	case *jwt.SigningMethodEd25519:
		var keys jwtKeys

		if signingKey != "" {
			priv, err := jwt.ParseEdPrivateKeyFromPEM([]byte(signingKey))
			if err != nil {
				return jwtKeys{}, fmt.Errorf("signing key for %s must be a PEM Ed25519 private key: %w", method.Alg(), err)
			}

			edPriv, ok := priv.(ed25519.PrivateKey)
			if !ok {
				return jwtKeys{}, fmt.Errorf("signing key for %s is not an Ed25519 key", method.Alg())
			}

			keys = jwtKeys{sign: edPriv, verify: edPriv.Public()}
		}

		if publicKey != "" {
			pub, err := jwt.ParseEdPublicKeyFromPEM([]byte(publicKey))
			if err != nil {
				return jwtKeys{}, fmt.Errorf("public key for %s must be a PEM Ed25519 public key: %w", method.Alg(), err)
			}

			keys.verify = pub
		}

		return requireVerifyKey(method, keys)

	default:
		return jwtKeys{}, fmt.Errorf("unsupported signing method: %s", method.Alg())
	}
}

func requireVerifyKey(method jwt.SigningMethod, keys jwtKeys) (jwtKeys, error) {
	if keys.verify == nil {
		return jwtKeys{}, fmt.Errorf("%s needs a PEM signing key or public key", method.Alg())
	}

	return keys, nil
}

// GenerateToken generates a new access token.
func (jm *JWTManager) GenerateToken(claims *JWTClaims) (string, error) {
	now := time.Now()

	// Set registered claims
	claims.Issuer = jm.config.Issuer
	claims.Audience = nil

	if jm.config.Audience != "" {
		claims.Audience = jwt.ClaimStrings{jm.config.Audience}
	}

	claims.IssuedAt = jwt.NewNumericDate(now)
	claims.ExpiresAt = jwt.NewNumericDate(now.Add(jm.config.TTL))
	claims.NotBefore = jwt.NewNumericDate(now)
	claims.TokenType = TokenTypeAccess

	// Generate ID if not set
	if claims.ID == "" {
		id, err := generateTokenID()
		if err != nil {
			return "", fmt.Errorf("failed to generate token ID: %w", err)
		}

		claims.ID = id
	}

	signedToken, err := jm.sign(claims, jm.accessKeys)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// GenerateRefreshToken generates a new refresh token. It carries
// "typ":"refresh" and is signed with RefreshSigningKey when one is set, so it
// is only accepted by ValidateRefreshToken.
func (jm *JWTManager) GenerateRefreshToken(userID string) (string, error) {
	now := time.Now()

	claims := &JWTClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jm.config.Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(jm.config.RefreshTTL)),
			NotBefore: jwt.NewNumericDate(now),
		},
		TokenType: TokenTypeRefresh,
	}

	if jm.config.Audience != "" {
		claims.Audience = jwt.ClaimStrings{jm.config.Audience}
	}

	// Generate ID
	id, err := generateTokenID()
	if err != nil {
		return "", fmt.Errorf("failed to generate token ID: %w", err)
	}

	claims.ID = id

	signedToken, err := jm.sign(claims, jm.refreshKeys)
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return signedToken, nil
}

func (jm *JWTManager) sign(claims *JWTClaims, keys jwtKeys) (string, error) {
	if keys.sign == nil {
		return "", errors.New("this manager has no signing key and can only verify tokens")
	}

	return jwt.NewWithClaims(jm.signingMethod, claims).SignedString(keys.sign)
}

// ValidateToken validates an access token and returns its claims. Refresh
// tokens, tokens without an expiry and tokens without "typ":"access" are
// rejected.
func (jm *JWTManager) ValidateToken(tokenString string) (*JWTClaims, error) {
	return jm.validate(tokenString, jm.accessKeys, TokenTypeAccess)
}

// ValidateRefreshToken validates a refresh token and returns its claims.
// Access tokens are rejected.
func (jm *JWTManager) ValidateRefreshToken(tokenString string) (*JWTClaims, error) {
	return jm.validate(tokenString, jm.refreshKeys, TokenTypeRefresh)
}

func (jm *JWTManager) validate(tokenString string, keys jwtKeys, wantType string) (*JWTClaims, error) {
	token, err := jm.parser.ParseWithClaims(tokenString, &JWTClaims{}, func(*jwt.Token) (any, error) {
		// WithValidMethods has already pinned the algorithm.
		return keys.verify, nil
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

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, ErrJWTInvalid
	}

	if claims.TokenType != wantType {
		return nil, ErrJWTWrongType
	}

	return claims, nil
}

// extractTokenFromRequest extracts the JWT token from request based on TokenLookup config.
func (jm *JWTManager) extractTokenFromRequest(r *http.Request) string {
	for _, lookup := range jm.lookups {
		var token string

		switch lookup.source {
		case "header":
			auth := r.Header.Get(lookup.key)
			if auth != "" {
				// Remove prefix (e.g., "Bearer ")
				if jm.config.TokenPrefix != "" {
					if after, ok := strings.CutPrefix(auth, jm.config.TokenPrefix+" "); ok {
						token = after
					}
				} else {
					token = auth
				}
			}
		case "query":
			token = r.URL.Query().Get(lookup.key)
		case "cookie":
			if cookie, err := r.Cookie(lookup.key); err == nil {
				token = cookie.Value
			}
		case "form":
			token = r.FormValue(lookup.key)
		}

		if token != "" {
			return token
		}
	}

	return ""
}

// shouldSkipPath checks if the path should skip JWT authentication.
func (jm *JWTManager) shouldSkipPath(path string) bool {
	return shouldSkipPath(path, jm.config.SkipPaths)
}

// jwtContextKey is the context key for JWT claims.
type jwtContextKey struct{}

// JWTMiddleware returns a middleware function for JWT authentication.
func JWTMiddleware(jwtManager *JWTManager) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if !jwtManager.config.Enabled {
				return next(ctx)
			}

			r := ctx.Request()

			// Skip JWT authentication for specified paths
			if jwtManager.shouldSkipPath(r.URL.Path) {
				return next(ctx)
			}

			// Extract token from request
			token := jwtManager.extractTokenFromRequest(r)
			if token == "" {
				jwtManager.logger.Debug("jwt token missing",
					forge.F("path", r.URL.Path),
				)

				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Validate token
			claims, err := jwtManager.ValidateToken(token)
			if err != nil {
				jwtManager.logger.Debug("jwt validation failed",
					forge.F("error", err),
					forge.F("path", r.URL.Path),
				)

				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Store claims in the forge context and the request context, so
			// GetJWTClaims and GetJWTClaimsFromStdContext both find them.
			ctx.Set("jwt_claims", claims)
			ctx.WithContext(context.WithValue(ctx.Context(), jwtContextKey{}, claims))

			return next(ctx)
		}
	}
}

// GetJWTClaims retrieves JWT claims from Forge context.
func GetJWTClaims(ctx forge.Context) (*JWTClaims, bool) {
	val := ctx.Get("jwt_claims")
	if val == nil {
		return nil, false
	}

	claims, ok := val.(*JWTClaims)

	return claims, ok
}

// GetJWTClaimsFromStdContext retrieves JWT claims from standard context (for backward compatibility).
func GetJWTClaimsFromStdContext(ctx context.Context) (*JWTClaims, bool) {
	claims, ok := ctx.Value(jwtContextKey{}).(*JWTClaims)

	return claims, ok
}

// RequireRoles returns a middleware that checks if the user has required roles.
func RequireRoles(roles ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			claims, ok := GetJWTClaims(ctx)
			if !ok {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Check if user has any of the required roles
			hasRole := false

			for _, requiredRole := range roles {
				if slices.Contains(claims.Roles, requiredRole) {
					hasRole = true
				}

				if hasRole {
					break
				}
			}

			if !hasRole {
				return ctx.String(http.StatusForbidden, "Forbidden: insufficient permissions")
			}

			return next(ctx)
		}
	}
}

// generateTokenID generates a random token ID.
func generateTokenID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(bytes), nil
}
