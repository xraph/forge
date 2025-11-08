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

	// OpenAPISchemes returns all security schemes for OpenAPI generation
	OpenAPISchemes() map[string]SecurityScheme
}

type registry struct {
	providers map[string]AuthProvider
	container forge.Container
	logger    forge.Logger
	mu        sync.RWMutex
}

// NewRegistry creates a new auth provider registry.
func NewRegistry(container forge.Container, logger forge.Logger) Registry {
	return &registry{
		providers: make(map[string]AuthProvider),
		container: container,
		logger:    logger,
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
func (r *registry) MiddlewareWithScopes(providerName string, scopes ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			provider, err := r.Get(providerName)
			if err != nil {
				r.logger.Warn("auth provider not found")

				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			req := ctx.Request()

			authCtx, err := provider.Authenticate(ctx.Context(), req)
			if err != nil {
				r.logger.Warn("authentication failed")

				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Check required scopes
			if len(scopes) > 0 && !authCtx.HasScopes(scopes...) {
				r.logger.Warn("insufficient scopes")

				return ctx.String(http.StatusForbidden, "Forbidden")
			}

			// Authentication and authorization succeeded
			authCtx.ProviderName = providerName
			ctx.Set("auth_context", authCtx)

			r.logger.Debug("authentication succeeded with scopes")

			return next(ctx)
		}
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
