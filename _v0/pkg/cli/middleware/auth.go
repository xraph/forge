package middleware

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/cli"
	"github.com/xraph/forge/v0/pkg/cli/prompt"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// AuthMiddleware provides authentication for CLI commands
type AuthMiddleware struct {
	*cli.BaseMiddleware
	logger common.Logger
	config AuthConfig
}

// AuthConfig contains authentication middleware configuration
type AuthConfig struct {
	Required         bool                  `yaml:"required" json:"required"`
	TokenFile        string                `yaml:"token_file" json:"token_file"`
	TokenEnvVar      string                `yaml:"token_env_var" json:"token_env_var"`
	ExcludeCommands  []string              `yaml:"exclude_commands" json:"exclude_commands"`
	SessionFile      string                `yaml:"session_file" json:"session_file"`
	SessionTimeout   time.Duration         `yaml:"session_timeout" json:"session_timeout"`
	Providers        []AuthProvider        `yaml:"providers" json:"providers"`
	DefaultProvider  string                `yaml:"default_provider" json:"default_provider"`
	CacheCredentials bool                  `yaml:"cache_credentials" json:"cache_credentials"`
	VerifyToken      bool                  `yaml:"verify_token" json:"verify_token"`
	TokenValidation  TokenValidationConfig `yaml:"token_validation" json:"token_validation"`
}

// AuthProvider represents an authentication provider
type AuthProvider struct {
	Name         string            `yaml:"name" json:"name"`
	Type         string            `yaml:"type" json:"type"`
	ClientID     string            `yaml:"client_id" json:"client_id"`
	ClientSecret string            `yaml:"client_secret" json:"client_secret"`
	AuthURL      string            `yaml:"auth_url" json:"auth_url"`
	TokenURL     string            `yaml:"token_url" json:"token_url"`
	RedirectURL  string            `yaml:"redirect_url" json:"redirect_url"`
	Scopes       []string          `yaml:"scopes" json:"scopes"`
	Config       map[string]string `yaml:"config" json:"config"`
}

// TokenValidationConfig contains token validation settings
type TokenValidationConfig struct {
	ValidateExpiry   bool          `yaml:"validate_expiry" json:"validate_expiry"`
	ValidateIssuer   bool          `yaml:"validate_issuer" json:"validate_issuer"`
	ValidateAudience bool          `yaml:"validate_audience" json:"validate_audience"`
	RefreshThreshold time.Duration `yaml:"refresh_threshold" json:"refresh_threshold"`
	MaxAge           time.Duration `yaml:"max_age" json:"max_age"`
}

// AuthContext contains authentication context
type AuthContext struct {
	Authenticated bool              `json:"authenticated"`
	UserID        string            `json:"user_id"`
	Username      string            `json:"username"`
	Email         string            `json:"email"`
	Token         string            `json:"token"`
	TokenType     string            `json:"token_type"`
	ExpiresAt     time.Time         `json:"expires_at"`
	Scopes        []string          `json:"scopes"`
	Provider      string            `json:"provider"`
	Metadata      map[string]string `json:"metadata"`
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(logger common.Logger) cli.CLIMiddleware {
	return NewAuthMiddlewareWithConfig(logger, DefaultAuthConfig())
}

// NewAuthMiddlewareWithConfig creates auth middleware with custom config
func NewAuthMiddlewareWithConfig(logger common.Logger, config AuthConfig) cli.CLIMiddleware {
	return &AuthMiddleware{
		BaseMiddleware: cli.NewBaseMiddleware("auth", 15),
		logger:         logger,
		config:         config,
	}
}

// Execute executes the authentication middleware
func (am *AuthMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Skip authentication for excluded commands
	if am.shouldExcludeCommand(ctx) {
		return next()
	}

	// Skip if authentication is not required and no credentials are available
	if !am.config.Required && !am.hasCredentials() {
		return next()
	}

	// Perform authentication
	authContext, err := am.authenticate(ctx)
	if err != nil {
		if am.config.Required {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// Log warning but continue for optional auth
		if am.logger != nil {
			am.logger.Warn("authentication failed, continuing without auth",
				logger.Error(err),
			)
		}
		return next()
	}

	// Set authentication context
	am.setAuthContext(ctx, authContext)

	return next()
}

// authenticate performs the authentication process
func (am *AuthMiddleware) authenticate(ctx cli.CLIContext) (*AuthContext, error) {
	// Try to load existing session first
	if am.config.SessionFile != "" {
		if authContext, err := am.loadSession(); err == nil && am.isValidSession(authContext) {
			return authContext, nil
		}
	}

	// Try to authenticate with available credentials
	authContext, err := am.authenticateWithCredentials(ctx)
	if err != nil {
		return nil, err
	}

	// Save session if configured
	if am.config.CacheCredentials && am.config.SessionFile != "" {
		if err := am.saveSession(authContext); err != nil {
			if am.logger != nil {
				am.logger.Warn("failed to save auth session",
					logger.Error(err),
				)
			}
		}
	}

	return authContext, nil
}

// authenticateWithCredentials attempts authentication with available credentials
func (am *AuthMiddleware) authenticateWithCredentials(ctx cli.CLIContext) (*AuthContext, error) {
	// Try token from environment variable
	if am.config.TokenEnvVar != "" {
		if token := os.Getenv(am.config.TokenEnvVar); token != "" {
			return am.authenticateWithToken(token, "env")
		}
	}

	// Try token from file
	if am.config.TokenFile != "" {
		if token, err := am.loadTokenFromFile(); err == nil && token != "" {
			return am.authenticateWithToken(token, "file")
		}
	}

	// Try interactive authentication if no token found
	return am.interactiveAuthentication(ctx)
}

// authenticateWithToken authenticates using a token
func (am *AuthMiddleware) authenticateWithToken(token, source string) (*AuthContext, error) {
	// Validate token if required
	if am.config.VerifyToken {
		if err := am.validateToken(token); err != nil {
			return nil, fmt.Errorf("token validation failed: %w", err)
		}
	}

	// Parse token to extract user info (simplified)
	userInfo, err := am.parseToken(token)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	return &AuthContext{
		Authenticated: true,
		UserID:        userInfo["user_id"],
		Username:      userInfo["username"],
		Email:         userInfo["email"],
		Token:         token,
		TokenType:     "bearer",
		Provider:      source,
		Metadata: map[string]string{
			"source": source,
		},
	}, nil
}

// interactiveAuthentication performs interactive authentication
func (am *AuthMiddleware) interactiveAuthentication(ctx cli.CLIContext) (*AuthContext, error) {
	if len(am.config.Providers) == 0 {
		return nil, fmt.Errorf("no authentication providers configured")
	}

	// Use default provider or prompt for selection
	provider := am.getProvider(am.config.DefaultProvider)
	if provider == nil {
		selectedProvider, err := am.selectProvider(ctx)
		if err != nil {
			return nil, err
		}
		provider = selectedProvider
	}

	// Perform authentication based on provider type
	switch provider.Type {
	case "oauth2":
		return am.oauth2Authentication(provider)
	case "basic":
		return am.basicAuthentication(ctx, provider)
	case "api_key":
		return am.apiKeyAuthentication(ctx, provider)
	default:
		return nil, fmt.Errorf("unsupported authentication provider type: %s", provider.Type)
	}
}

// oauth2Authentication performs OAuth2 authentication
func (am *AuthMiddleware) oauth2Authentication(provider *AuthProvider) (*AuthContext, error) {
	// This would implement OAuth2 flow
	// For simplicity, returning a mock implementation
	return nil, fmt.Errorf("OAuth2 authentication not implemented")
}

// basicAuthentication performs basic username/password authentication
func (am *AuthMiddleware) basicAuthentication(ctx cli.CLIContext, provider *AuthProvider) (*AuthContext, error) {
	prompter := prompt.NewPrompter(os.Stdin, os.Stdout)

	// Get username
	username, err := prompter.Input("Username", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get username: %w", err)
	}

	// Get password
	password, err := prompter.Password("Password")
	if err != nil {
		return nil, fmt.Errorf("failed to get password: %w", err)
	}

	// Authenticate with provider (this would make actual API call)
	token, err := am.authenticateBasic(provider, username, password)
	if err != nil {
		return nil, err
	}

	return &AuthContext{
		Authenticated: true,
		Username:      username,
		Token:         token,
		TokenType:     "bearer",
		Provider:      provider.Name,
	}, nil
}

// apiKeyAuthentication performs API key authentication
func (am *AuthMiddleware) apiKeyAuthentication(ctx cli.CLIContext, provider *AuthProvider) (*AuthContext, error) {
	prompter := prompt.NewPrompter(os.Stdin, os.Stdout)

	// Get API key
	apiKey, err := prompter.Password("API Key")
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	// Validate API key with provider
	userInfo, err := am.validateAPIKey(provider, apiKey)
	if err != nil {
		return nil, err
	}

	return &AuthContext{
		Authenticated: true,
		UserID:        userInfo["user_id"],
		Username:      userInfo["username"],
		Token:         apiKey,
		TokenType:     "api_key",
		Provider:      provider.Name,
	}, nil
}

// Helper methods

// shouldExcludeCommand checks if command should be excluded from authentication
func (am *AuthMiddleware) shouldExcludeCommand(ctx cli.CLIContext) bool {
	commandName := ctx.Command().Name()
	for _, excluded := range am.config.ExcludeCommands {
		if excluded == commandName {
			return true
		}
	}
	return false
}

// hasCredentials checks if any credentials are available
func (am *AuthMiddleware) hasCredentials() bool {
	// Check environment variable
	if am.config.TokenEnvVar != "" && os.Getenv(am.config.TokenEnvVar) != "" {
		return true
	}

	// Check token file
	if am.config.TokenFile != "" {
		if _, err := os.Stat(am.config.TokenFile); err == nil {
			return true
		}
	}

	return false
}

// loadTokenFromFile loads token from file
func (am *AuthMiddleware) loadTokenFromFile() (string, error) {
	if am.config.TokenFile == "" {
		return "", fmt.Errorf("no token file configured")
	}

	data, err := os.ReadFile(am.config.TokenFile)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

// validateToken validates a token
func (am *AuthMiddleware) validateToken(token string) error {
	// This would implement actual token validation
	// For now, just check if token is not empty
	if token == "" {
		return fmt.Errorf("empty token")
	}
	return nil
}

// parseToken parses token to extract user information
func (am *AuthMiddleware) parseToken(token string) (map[string]string, error) {
	// This would implement actual token parsing (JWT, etc.)
	// For now, return mock data
	return map[string]string{
		"user_id":  "user123",
		"username": "user",
		"email":    "user@example.com",
	}, nil
}

// loadSession loads authentication session from file
func (am *AuthMiddleware) loadSession() (*AuthContext, error) {
	// This would implement session loading
	return nil, fmt.Errorf("session loading not implemented")
}

// saveSession saves authentication session to file
func (am *AuthMiddleware) saveSession(authContext *AuthContext) error {
	// This would implement session saving
	return nil
}

// isValidSession checks if session is still valid
func (am *AuthMiddleware) isValidSession(authContext *AuthContext) bool {
	if authContext == nil || !authContext.Authenticated {
		return false
	}

	// Check expiry
	if !authContext.ExpiresAt.IsZero() && time.Now().After(authContext.ExpiresAt) {
		return false
	}

	return true
}

// getProvider returns provider by name
func (am *AuthMiddleware) getProvider(name string) *AuthProvider {
	for _, provider := range am.config.Providers {
		if provider.Name == name {
			return &provider
		}
	}
	return nil
}

// selectProvider allows user to select authentication provider
func (am *AuthMiddleware) selectProvider(ctx cli.CLIContext) (*AuthProvider, error) {
	if len(am.config.Providers) == 1 {
		return &am.config.Providers[0], nil
	}

	prompter := prompt.NewPrompter(os.Stdin, os.Stdout)

	var options []string
	for _, provider := range am.config.Providers {
		options = append(options, fmt.Sprintf("%s (%s)", provider.Name, provider.Type))
	}

	index, err := prompter.Select("Select authentication provider", options)
	if err != nil {
		return nil, err
	}

	return &am.config.Providers[index], nil
}

// authenticateBasic performs basic authentication with provider
func (am *AuthMiddleware) authenticateBasic(provider *AuthProvider, username, password string) (string, error) {
	// This would make actual API call to authenticate
	return "mock_token", nil
}

// validateAPIKey validates API key with provider
func (am *AuthMiddleware) validateAPIKey(provider *AuthProvider, apiKey string) (map[string]string, error) {
	// This would make actual API call to validate
	return map[string]string{
		"user_id":  "api_user",
		"username": "api_user",
	}, nil
}

// setAuthContext sets authentication context in CLI context
func (am *AuthMiddleware) setAuthContext(ctx cli.CLIContext, authContext *AuthContext) {
	ctx.Set("auth_context", authContext)
	ctx.Set("authenticated", authContext.Authenticated)
	ctx.Set("user_id", authContext.UserID)
	ctx.Set("username", authContext.Username)
	ctx.Set("token", authContext.Token)
}

// DefaultAuthConfig returns default authentication configuration
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		Required:         false,
		TokenEnvVar:      "AUTH_TOKEN",
		ExcludeCommands:  []string{"help", "completion", "version"},
		SessionTimeout:   24 * time.Hour,
		DefaultProvider:  "",
		CacheCredentials: true,
		VerifyToken:      false,
		TokenValidation: TokenValidationConfig{
			ValidateExpiry:   true,
			RefreshThreshold: 5 * time.Minute,
			MaxAge:           7 * 24 * time.Hour,
		},
	}
}

// RequiredAuthConfig returns configuration that requires authentication
func RequiredAuthConfig() AuthConfig {
	config := DefaultAuthConfig()
	config.Required = true
	config.VerifyToken = true
	return config
}
