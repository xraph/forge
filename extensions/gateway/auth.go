package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge"
)

// AuthProvider is the gateway's interface for authentication providers.
// This mirrors the auth extension's AuthProvider interface, decoupled to
// avoid a hard dependency on extensions/auth.
type AuthProvider interface {
	// Name returns the provider name.
	Name() string

	// Authenticate validates a request and returns an auth context or error.
	Authenticate(ctx context.Context, r *http.Request) (*GatewayAuthContext, error)
}

// GatewayAuthContext holds authenticated subject information for gateway requests.
type GatewayAuthContext struct {
	// Subject is the authenticated entity (user ID, service ID, etc.)
	Subject string `json:"subject"`

	// Claims holds additional authentication claims (roles, permissions, etc.)
	Claims map[string]any `json:"claims,omitempty"`

	// Scopes holds OAuth2 scopes or permission strings
	Scopes []string `json:"scopes,omitempty"`

	// ProviderName identifies which auth provider authenticated this request
	ProviderName string `json:"providerName"`
}

// HasScope checks if the auth context has a specific scope.
func (a *GatewayAuthContext) HasScope(scope string) bool {
	for _, s := range a.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasScopes checks if the auth context has all specified scopes.
func (a *GatewayAuthContext) HasScopes(scopes ...string) bool {
	for _, scope := range scopes {
		if !a.HasScope(scope) {
			return false
		}
	}
	return true
}

// AuthRegistry manages authentication providers for the gateway.
// It supports resolving auth from the Forge auth extension, as well as
// registering custom gateway-specific providers.
type AuthRegistry interface {
	// Get returns a provider by name.
	Get(name string) (AuthProvider, bool)

	// Has checks if a provider exists.
	Has(name string) bool

	// List returns all registered provider names.
	List() []string

	// Register adds a custom auth provider.
	Register(provider AuthProvider) error
}

// GatewayAuth handles authentication for gateway requests.
// It operates at the HTTP transport level (before request proxying),
// supporting multiple auth strategies and per-route configuration.
type GatewayAuth struct {
	config     AuthConfig
	logger     forge.Logger
	providers  map[string]AuthProvider
	forgeAuth  AuthRegistry // Optional integration with Forge auth extension
}

// NewGatewayAuth creates a new gateway auth handler.
func NewGatewayAuth(config AuthConfig, logger forge.Logger) *GatewayAuth {
	return &GatewayAuth{
		config:    config,
		logger:    logger,
		providers: make(map[string]AuthProvider),
	}
}

// SetForgeAuth sets the optional Forge auth extension integration.
func (ga *GatewayAuth) SetForgeAuth(registry AuthRegistry) {
	ga.forgeAuth = registry
}

// RegisterProvider registers a custom gateway-level auth provider.
func (ga *GatewayAuth) RegisterProvider(provider AuthProvider) {
	ga.providers[provider.Name()] = provider
}

// Authenticate checks if a request is authenticated based on configuration.
// It supports:
//   - Global auth (applied to all routes unless skipped)
//   - Per-route auth overrides (specific providers, scopes, skip)
//   - Multiple providers tried in order (OR logic)
//   - Auth context forwarding to upstream via headers
func (ga *GatewayAuth) Authenticate(r *http.Request, route *Route) (*GatewayAuthContext, error) {
	if !ga.config.Enabled {
		return nil, nil
	}

	// Check per-route auth config
	routeAuth := route.Auth
	if routeAuth != nil && routeAuth.SkipAuth {
		return nil, nil
	}

	if routeAuth != nil && !routeAuth.Enabled {
		return nil, nil
	}

	// Determine which providers to use
	providerNames := ga.config.Providers
	if routeAuth != nil && len(routeAuth.Providers) > 0 {
		providerNames = routeAuth.Providers
	}

	if len(providerNames) == 0 {
		// No providers configured - check for default policy
		if ga.config.DefaultPolicy == "allow" {
			return nil, nil
		}
		// Default deny when auth is enabled but no providers
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "no auth providers configured"}
	}

	// Try each provider in order (OR logic - any success passes)
	var lastErr error
	for _, name := range providerNames {
		provider := ga.getProvider(name)
		if provider == nil {
			ga.logger.Debug("auth provider not found",
				forge.F("provider", name),
			)
			continue
		}

		authCtx, err := provider.Authenticate(r.Context(), r)
		if err != nil {
			lastErr = err
			ga.logger.Debug("auth provider failed",
				forge.F("provider", name),
				forge.F("error", err),
			)
			continue
		}

		if authCtx == nil {
			continue
		}

		// Check scopes if configured
		if routeAuth != nil && len(routeAuth.Scopes) > 0 {
			if !authCtx.HasScopes(routeAuth.Scopes...) {
				return nil, &AuthError{
					Code:    http.StatusForbidden,
					Message: "insufficient scopes",
				}
			}
		}

		authCtx.ProviderName = name
		return authCtx, nil
	}

	// All providers failed
	if lastErr != nil {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: lastErr.Error()}
	}

	return nil, &AuthError{Code: http.StatusUnauthorized, Message: "authentication required"}
}

// ForwardAuthHeaders adds authentication context to upstream request headers.
// This allows upstream services to access the authenticated user info
// without re-authenticating.
func (ga *GatewayAuth) ForwardAuthHeaders(r *http.Request, authCtx *GatewayAuthContext) {
	if authCtx == nil || !ga.config.ForwardHeaders {
		return
	}

	r.Header.Set("X-Auth-Subject", authCtx.Subject)
	r.Header.Set("X-Auth-Provider", authCtx.ProviderName)

	if len(authCtx.Scopes) > 0 {
		r.Header.Set("X-Auth-Scopes", strings.Join(authCtx.Scopes, ","))
	}

	// Forward selected claims as headers
	if role, ok := authCtx.Claims["role"]; ok {
		if roleStr, isStr := role.(string); isStr {
			r.Header.Set("X-Auth-Role", roleStr)
		}
	}

	if email, ok := authCtx.Claims["email"]; ok {
		if emailStr, isStr := email.(string); isStr {
			r.Header.Set("X-Auth-Email", emailStr)
		}
	}
}

// getProvider resolves a provider by name, checking both local providers
// and the optional Forge auth extension registry.
func (ga *GatewayAuth) getProvider(name string) AuthProvider {
	// Check local gateway providers first
	if p, ok := ga.providers[name]; ok {
		return p
	}

	// Fall back to Forge auth extension
	if ga.forgeAuth != nil {
		if p, ok := ga.forgeAuth.Get(name); ok {
			return p
		}
	}

	return nil
}

// AuthError represents an authentication/authorization error.
type AuthError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *AuthError) Error() string {
	return e.Message
}

// --- Built-in Auth Providers ---

// APIKeyAuthProvider validates requests using API keys in headers or query params.
type APIKeyAuthProvider struct {
	name      string
	header    string
	queryParam string
	keys      map[string]*APIKeyEntry
}

// APIKeyEntry represents a registered API key with associated metadata.
type APIKeyEntry struct {
	Key      string   `json:"key"`
	Subject  string   `json:"subject"`
	Scopes   []string `json:"scopes,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// NewAPIKeyAuthProvider creates an API key auth provider.
func NewAPIKeyAuthProvider(name, header, queryParam string, keys []*APIKeyEntry) *APIKeyAuthProvider {
	keyMap := make(map[string]*APIKeyEntry, len(keys))
	for _, k := range keys {
		keyMap[k.Key] = k
	}

	if header == "" {
		header = "X-API-Key"
	}

	return &APIKeyAuthProvider{
		name:       name,
		header:     header,
		queryParam: queryParam,
		keys:       keyMap,
	}
}

func (p *APIKeyAuthProvider) Name() string { return p.name }

func (p *APIKeyAuthProvider) Authenticate(_ context.Context, r *http.Request) (*GatewayAuthContext, error) {
	// Check header first
	key := r.Header.Get(p.header)

	// Fall back to query param
	if key == "" && p.queryParam != "" {
		key = r.URL.Query().Get(p.queryParam)
	}

	if key == "" {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "API key required"}
	}

	entry, ok := p.keys[key]
	if !ok {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "invalid API key"}
	}

	return &GatewayAuthContext{
		Subject:  entry.Subject,
		Scopes:   entry.Scopes,
		Claims:   map[string]any{"api_key": true},
	}, nil
}

// BearerTokenAuthProvider validates Bearer tokens by forwarding them to a
// validation endpoint or using a configurable validation function.
type BearerTokenAuthProvider struct {
	name     string
	validate func(ctx context.Context, token string) (*GatewayAuthContext, error)
}

// NewBearerTokenAuthProvider creates a bearer token auth provider with a custom validator.
func NewBearerTokenAuthProvider(name string, validate func(ctx context.Context, token string) (*GatewayAuthContext, error)) *BearerTokenAuthProvider {
	return &BearerTokenAuthProvider{
		name:     name,
		validate: validate,
	}
}

func (p *BearerTokenAuthProvider) Name() string { return p.name }

func (p *BearerTokenAuthProvider) Authenticate(ctx context.Context, r *http.Request) (*GatewayAuthContext, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "authorization header required"}
	}

	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "bearer token required"}
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "empty bearer token"}
	}

	return p.validate(ctx, token)
}

// ForwardAuthProvider delegates authentication to an external auth service.
// It sends the original request headers to the auth endpoint and uses
// the response to determine authentication status.
type ForwardAuthProvider struct {
	name     string
	endpoint string
	headers  []string // Headers to forward to auth service
	client   *http.Client
}

// NewForwardAuthProvider creates a forward auth provider.
func NewForwardAuthProvider(name, endpoint string, headers []string) *ForwardAuthProvider {
	if len(headers) == 0 {
		headers = []string{"Authorization", "Cookie", "X-Forwarded-For"}
	}

	return &ForwardAuthProvider{
		name:     name,
		endpoint: endpoint,
		headers:  headers,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (p *ForwardAuthProvider) Name() string { return p.name }

func (p *ForwardAuthProvider) Authenticate(ctx context.Context, r *http.Request) (*GatewayAuthContext, error) {
	authReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint, nil)
	if err != nil {
		return nil, &AuthError{Code: http.StatusInternalServerError, Message: "failed to create auth request"}
	}

	// Forward configured headers
	for _, h := range p.headers {
		if v := r.Header.Get(h); v != "" {
			authReq.Header.Set(h, v)
		}
	}

	// Forward original request info
	authReq.Header.Set("X-Original-URI", r.RequestURI)
	authReq.Header.Set("X-Original-Method", r.Method)

	resp, err := p.client.Do(authReq)
	if err != nil {
		return nil, &AuthError{Code: http.StatusBadGateway, Message: "auth service unavailable"}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &AuthError{Code: resp.StatusCode, Message: "authentication failed"}
	}

	// Extract auth context from response headers
	authCtx := &GatewayAuthContext{
		Subject: resp.Header.Get("X-Auth-Subject"),
		Claims:  make(map[string]any),
	}

	if authCtx.Subject == "" {
		authCtx.Subject = "authenticated"
	}

	if role := resp.Header.Get("X-Auth-Role"); role != "" {
		authCtx.Claims["role"] = role
	}

	if scopes := resp.Header.Get("X-Auth-Scopes"); scopes != "" {
		authCtx.Scopes = strings.Split(scopes, ",")
	}

	return authCtx, nil
}

