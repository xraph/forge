package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xraph/forge/v0/pkg/common"
	"golang.org/x/crypto/argon2"
)

// AuthManager provides centralized authentication management
type AuthManager struct {
	config    AuthConfig
	providers map[string]AuthProvider
	mu        sync.RWMutex
}

// AuthConfig contains authentication configuration
type AuthConfig struct {
	JWTSecret        string               `yaml:"jwt_secret" env:"FORGE_JWT_SECRET"`
	JWTExpiration    time.Duration        `yaml:"jwt_expiration" default:"24h"`
	PasswordSalt     string               `yaml:"password_salt" env:"FORGE_PASSWORD_SALT"`
	SessionTimeout   time.Duration        `yaml:"session_timeout" default:"8h"`
	MaxLoginAttempts int                  `yaml:"max_login_attempts" default:"5"`
	LockoutDuration  time.Duration        `yaml:"lockout_duration" default:"15m"`
	EnableMFA        bool                 `yaml:"enable_mfa" default:"false"`
	EnableSSO        bool                 `yaml:"enable_sso" default:"false"`
	Providers        []AuthProviderConfig `yaml:"providers"`
	Logger           common.Logger        `yaml:"-"`
	Metrics          common.Metrics       `yaml:"-"`
}

// AuthProviderConfig contains provider-specific configuration
type AuthProviderConfig struct {
	Name         string            `yaml:"name"`
	Type         string            `yaml:"type"` // "oauth2", "saml", "ldap", "local"
	Enabled      bool              `yaml:"enabled"`
	Config       map[string]string `yaml:"config"`
	ClientID     string            `yaml:"client_id"`
	ClientSecret string            `yaml:"client_secret"`
	RedirectURL  string            `yaml:"redirect_url"`
	Scopes       []string          `yaml:"scopes"`
}

// AuthProvider interface for different authentication providers
type AuthProvider interface {
	Name() string
	Type() string
	Authenticate(ctx context.Context, credentials Credentials) (*AuthResult, error)
	RefreshToken(ctx context.Context, token string) (*AuthResult, error)
	ValidateToken(ctx context.Context, token string) (*Claims, error)
	Logout(ctx context.Context, token string) error
}

// Credentials represents authentication credentials
type Credentials struct {
	Username   string            `json:"username"`
	Password   string            `json:"password"`
	Email      string            `json:"email"`
	Token      string            `json:"token"`
	Provider   string            `json:"provider"`
	Attributes map[string]string `json:"attributes"`
	MFA        string            `json:"mfa,omitempty"`
}

// AuthResult represents the result of authentication
type AuthResult struct {
	Success      bool              `json:"success"`
	Token        string            `json:"token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at"`
	User         *User             `json:"user"`
	Provider     string            `json:"provider"`
	Attributes   map[string]string `json:"attributes"`
}

// User represents an authenticated user
type User struct {
	ID          string            `json:"id"`
	Username    string            `json:"username"`
	Email       string            `json:"email"`
	FirstName   string            `json:"first_name"`
	LastName    string            `json:"last_name"`
	Roles       []string          `json:"roles"`
	Permissions []string          `json:"permissions"`
	Attributes  map[string]string `json:"attributes"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	LastLogin   time.Time         `json:"last_login"`
	IsActive    bool              `json:"is_active"`
	IsVerified  bool              `json:"is_verified"`
}

// Claims represents JWT claims
type Claims struct {
	UserID      string            `json:"user_id"`
	Username    string            `json:"username"`
	Email       string            `json:"email"`
	Roles       []string          `json:"roles"`
	Permissions []string          `json:"permissions"`
	Provider    string            `json:"provider"`
	Attributes  map[string]string `json:"attributes"`
	jwt.RegisteredClaims
}

// NewAuthManager creates a new authentication manager
func NewAuthManager(config AuthConfig) (*AuthManager, error) {
	if config.JWTSecret == "" {
		return nil, errors.New("JWT secret is required")
	}

	if config.PasswordSalt == "" {
		return nil, errors.New("password salt is required")
	}

	manager := &AuthManager{
		config:    config,
		providers: make(map[string]AuthProvider),
	}

	// Initialize default providers
	if err := manager.initializeProviders(); err != nil {
		return nil, fmt.Errorf("failed to initialize providers: %w", err)
	}

	return manager, nil
}

// initializeProviders initializes authentication providers
func (am *AuthManager) initializeProviders() error {
	for _, providerConfig := range am.config.Providers {
		if !providerConfig.Enabled {
			continue
		}

		provider, err := am.createProvider(providerConfig)
		if err != nil {
			return fmt.Errorf("failed to create provider %s: %w", providerConfig.Name, err)
		}

		am.providers[providerConfig.Name] = provider
	}

	return nil
}

// createProvider creates a provider based on configuration
func (am *AuthManager) createProvider(config AuthProviderConfig) (AuthProvider, error) {
	switch config.Type {
	case "oauth2":
		return NewOAuth2Provider(config)
	case "saml":
		return NewSAMLProvider(config)
	case "ldap":
		return NewLDAPProvider(config)
	case "local":
		return NewLocalProvider(config, am.config)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", config.Type)
	}
}

// Authenticate authenticates a user with the specified provider
func (am *AuthManager) Authenticate(ctx context.Context, credentials Credentials) (*AuthResult, error) {
	am.mu.RLock()
	provider, exists := am.providers[credentials.Provider]
	am.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("provider %s not found", credentials.Provider)
	}

	result, err := provider.Authenticate(ctx, credentials)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Generate JWT token
	if result.Success {
		token, err := am.generateJWT(result.User)
		if err != nil {
			return nil, fmt.Errorf("failed to generate JWT: %w", err)
		}
		result.Token = token
		result.ExpiresAt = time.Now().Add(am.config.JWTExpiration)
	}

	return result, nil
}

// ValidateToken validates a JWT token
func (am *AuthManager) ValidateToken(ctx context.Context, token string) (*Claims, error) {
	claims := &Claims{}

	parsedToken, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(am.config.JWTSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	if !parsedToken.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// RefreshToken refreshes an authentication token
func (am *AuthManager) RefreshToken(ctx context.Context, refreshToken string) (*AuthResult, error) {
	// Validate the refresh token
	claims, err := am.ValidateToken(ctx, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Create new user object from claims
	user := &User{
		ID:          claims.UserID,
		Username:    claims.Username,
		Email:       claims.Email,
		Roles:       claims.Roles,
		Permissions: claims.Permissions,
		Attributes:  claims.Attributes,
	}

	// Generate new JWT token
	token, err := am.generateJWT(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new JWT: %w", err)
	}

	return &AuthResult{
		Success:   true,
		Token:     token,
		ExpiresAt: time.Now().Add(am.config.JWTExpiration),
		User:      user,
	}, nil
}

// Logout logs out a user
func (am *AuthManager) Logout(ctx context.Context, token string) error {
	// In a production system, you would typically:
	// 1. Add the token to a blacklist
	// 2. Invalidate the session
	// 3. Notify the provider
	// For now, we'll just validate the token exists
	_, err := am.ValidateToken(ctx, token)
	return err
}

// generateJWT generates a JWT token for a user
func (am *AuthManager) generateJWT(user *User) (string, error) {
	claims := &Claims{
		UserID:      user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Roles:       user.Roles,
		Permissions: user.Permissions,
		Attributes:  user.Attributes,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(am.config.JWTExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "forge-framework",
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(am.config.JWTSecret))
}

// HashPassword hashes a password using Argon2
func (am *AuthManager) HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)

	// Combine salt and hash
	combined := make([]byte, 16+32)
	copy(combined[:16], salt)
	copy(combined[16:], hash)

	return base64.StdEncoding.EncodeToString(combined), nil
}

// VerifyPassword verifies a password against its hash
func (am *AuthManager) VerifyPassword(password, hash string) bool {
	decoded, err := base64.StdEncoding.DecodeString(hash)
	if err != nil {
		return false
	}

	if len(decoded) != 48 { // 16 bytes salt + 32 bytes hash
		return false
	}

	salt := decoded[:16]
	expectedHash := decoded[16:]

	actualHash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)

	return subtle.ConstantTimeCompare(expectedHash, actualHash) == 1
}

// GetProvider returns a provider by name
func (am *AuthManager) GetProvider(name string) (AuthProvider, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	provider, exists := am.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", name)
	}

	return provider, nil
}

// ListProviders returns a list of available providers
func (am *AuthManager) ListProviders() []string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	providers := make([]string, 0, len(am.providers))
	for name := range am.providers {
		providers = append(providers, name)
	}

	return providers
}

// GetConfig returns the current configuration
func (am *AuthManager) GetConfig() AuthConfig {
	return am.config
}

// UpdateConfig updates the configuration
func (am *AuthManager) UpdateConfig(config AuthConfig) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	am.config = config
	return am.initializeProviders()
}
