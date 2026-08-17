package auth

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/xraph/forge"
)

// Registry manages authentication providers.
// It provides thread-safe registration and retrieval of auth providers,
// and can create middleware that chains multiple providers.
type Registry interface {
	// Register registers an auth provider
	Register(provider AuthProvider) error

	// Unregister removes a provider by name
	Unregister(name string) error

	// Get retrieves a provider by name
	Get(name string) (AuthProvider, error)

	// Has checks if a provider exists
	Has(name string) bool

	// List returns all registered provider names
	List() []string

	// Middleware creates combined middleware for multiple providers.
	// When multiple providers are specified, they are tried in order (OR logic).
	// Authentication succeeds if ANY provider succeeds.
	Middleware(providerNames ...string) forge.Middleware

	// MiddlewareAnd creates middleware requiring ALL providers to succeed (AND logic).
	MiddlewareAnd(providerNames ...string) forge.Middleware

	// MiddlewareWithScopes creates middleware with required scopes
	MiddlewareWithScopes(providerName string, scopes ...string) forge.Middleware

	// MiddlewareWithRequirement authenticates through the requirement's
	// providers and then hands the result to the authorizer.
	MiddlewareWithRequirement(req Requirement) forge.Middleware

	// OpenAPISchemes returns all security schemes for OpenAPI generation
	OpenAPISchemes() map[string]SecurityScheme

	// SetAuthorizer replaces the authorization decision maker. Passing nil is
	// ignored, so a misconfigured caller cannot leave the registry without one.
	SetAuthorizer(a Authorizer)

	// Authorizer returns the current decision maker. Never nil.
	Authorizer() Authorizer
}

type registry struct {
	providers  map[string]AuthProvider
	authorizer Authorizer
	container  forge.Container
	logger     forge.Logger
	mu         sync.RWMutex
}

// NewRegistry creates a new auth provider registry.
func NewRegistry(container forge.Container, logger forge.Logger) Registry {
	return &registry{
		providers:  make(map[string]AuthProvider),
		authorizer: NewDefaultAuthorizer(),
		container:  container,
		logger:     logger,
	}
}

func (r *registry) Register(provider AuthProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := provider.Name()
	if name == "" {
		return fmt.Errorf("%w: provider name cannot be empty", ErrInvalidConfiguration)
	}

	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("%w: provider %q already registered", ErrProviderExists, name)
	}

	r.providers[name] = provider
	r.logger.Info("auth provider registered")

	return nil
}

func (r *registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; !exists {
		return fmt.Errorf("%w: provider %q", ErrProviderNotFound, name)
	}

	delete(r.providers, name)
	r.logger.Info("auth provider unregistered")

	return nil
}

func (r *registry) Get(name string) (AuthProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[name]
	if !exists {
		return nil, fmt.Errorf("%w: provider %q", ErrProviderNotFound, name)
	}

	return provider, nil
}

func (r *registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.providers[name]

	return exists
}

func (r *registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	return names
}

// Middleware creates combined middleware for multiple providers (OR logic).
func (r *registry) Middleware(providerNames ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			// If no providers specified, pass through
			if len(providerNames) == 0 {
				return next(ctx)
			}

			req := ctx.Request()

			// Try each provider in order
			for _, name := range providerNames {
				provider, err := r.Get(name)
				if err != nil {
					r.logger.Debug("auth provider not found")

					continue
				}

				authCtx, err := provider.Authenticate(ctx.Context(), req)
				if err != nil {
					r.logger.Debug("authentication failed")

					continue
				}

				// Authentication succeeded
				authCtx.ProviderName = name
				ctx.Set("auth_context", authCtx)

				r.logger.Debug("authentication succeeded")

				return next(ctx)
			}

			// All providers failed
			r.logger.Warn("authentication failed for all providers")

			return ctx.String(http.StatusUnauthorized, "Unauthorized")
		}
	}
}

// MiddlewareAnd creates middleware requiring ALL providers to succeed.
func (r *registry) MiddlewareAnd(providerNames ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if len(providerNames) == 0 {
				return next(ctx)
			}

			req := ctx.Request()

			// All providers must succeed
			var combinedAuthCtx *AuthContext

			for i, name := range providerNames {
				provider, err := r.Get(name)
				if err != nil {
					r.logger.Warn("auth provider not found")

					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}

				authCtx, err := provider.Authenticate(ctx.Context(), req)
				if err != nil {
					r.logger.Warn("authentication failed")

					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}

				// Merge contexts (first provider wins for subject)
				if i == 0 {
					combinedAuthCtx = authCtx
					combinedAuthCtx.ProviderName = name
				} else {
					// Merge claims and scopes
					for k, v := range authCtx.Claims {
						if combinedAuthCtx.Claims == nil {
							combinedAuthCtx.Claims = make(map[string]any)
						}

						combinedAuthCtx.Claims[k] = v
					}

					combinedAuthCtx.Scopes = append(combinedAuthCtx.Scopes, authCtx.Scopes...)
				}
			}

			// All providers succeeded
			ctx.Set("auth_context", combinedAuthCtx)

			r.logger.Debug("authentication succeeded (AND mode)")

			return next(ctx)
		}
	}
}

// MiddlewareWithScopes creates middleware with required scopes.
//
// Kept for its existing callers. It is now expressed as a Requirement so that
// scope enforcement and role/permission enforcement cannot drift apart.
func (r *registry) MiddlewareWithScopes(providerName string, scopes ...string) forge.Middleware {
	return r.MiddlewareWithRequirement(Requirement{
		Providers: []string{providerName},
		Scopes:    scopes,
	})
}

// MiddlewareWithRequirement authenticates, then authorizes.
//
// Authentication reuses the existing provider chain semantics: Mode "and"
// requires every provider, anything else tries them in order and takes the
// first success. Authorization is delegated to the registry's Authorizer, so
// a policy engine sees exactly what the route declared.
func (r *registry) MiddlewareWithRequirement(req Requirement) forge.Middleware {
	authenticate := r.Middleware(req.Providers...)
	if req.Mode == "and" {
		authenticate = r.MiddlewareAnd(req.Providers...)
	}

	return func(next forge.Handler) forge.Handler {
		// The authorization check runs between authentication and the
		// handler, so it is wrapped as the "next" the auth middleware calls.
		authorize := func(ctx forge.Context) error {
			authCtx, _ := ctx.Get("auth_context").(*AuthContext)

			// Publish roles as a plain slice under "auth.subject.roles",
			// whether or not this route declares a requirement.
			//
			// Deliberately NOT "auth.roles": that string is already taken as
			// ROUTE metadata for the roles a route REQUIRES (set by
			// WithAnyRole in internal/router/router_auth_opts.go, read by the
			// OpenAPI generator to emit x-forge-authz). This key instead
			// carries what the authenticated SUBJECT HAS. The two live in
			// different maps — request context here, route metadata there —
			// so there is no runtime collision, but giving "requirement" and
			// "possession" the same literal invites exactly the kind of
			// confusion this rename fixes.
			//
			// internal/router cannot import this package (the extension
			// depends on forge, so the import would cycle), and Task 13's
			// RequireRole/RequireAllRoles interceptors live there and need
			// the roles. Setting this has to happen above the IsEmpty return
			// below, or an authenticate-only route would authenticate
			// successfully and still leave those interceptors with nothing
			// to read.
			if authCtx != nil {
				ctx.Set("auth.subject.roles", authCtx.Roles)
			}

			if req.IsEmpty() {
				return next(ctx)
			}

			if err := r.Authorizer().Authorize(ctx.Context(), authCtx, req); err != nil {
				r.logger.Warn("authorization denied")

				return ctx.String(http.StatusForbidden, err.Error())
			}

			return next(ctx)
		}

		return authenticate(authorize)
	}
}

// OpenAPISchemes returns all security schemes for OpenAPI generation.
func (r *registry) OpenAPISchemes() map[string]SecurityScheme {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemes := make(map[string]SecurityScheme, len(r.providers))
	for name, provider := range r.providers {
		schemes[name] = provider.OpenAPIScheme()
	}

	return schemes
}

// SetAuthorizer replaces the authorization decision maker.
//
// nil is silently ignored rather than rejected with an error: the guarded
// request path (a future task) reads Authorizer() on every request and
// assumes it is non-nil, so a nil write here would be a bug that only
// surfaces later as a panic. Refusing to accept nil keeps that invariant
// true no matter what a caller passes.
func (r *registry) SetAuthorizer(a Authorizer) {
	if a == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.authorizer = a
}

// Authorizer returns the current decision maker, guarded by the same mutex
// as providers because it is read on every guarded request and written at
// most once at startup, but concurrent readers still need a consistent view.
func (r *registry) Authorizer() Authorizer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.authorizer
}
