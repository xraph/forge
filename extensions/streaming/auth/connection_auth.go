package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xraph/forge/extensions/auth"
)

// connectionAuthenticator implements ConnectionAuthenticator using the auth extension registry.
type connectionAuthenticator struct {
	registry  auth.Registry
	providers []string
	scopes    []string
}

// NewConnectionAuthenticator creates a new connection authenticator.
func NewConnectionAuthenticator(registry auth.Registry, providers []string) ConnectionAuthenticator {
	return &connectionAuthenticator{
		registry:  registry,
		providers: providers,
	}
}

// AuthenticateConnection verifies the connection on WebSocket/SSE upgrade.
func (ca *connectionAuthenticator) AuthenticateConnection(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	if len(ca.providers) == 0 {
		return nil, errors.New("no auth providers configured")
	}

	// Try each provider (OR logic)
	var lastErr error

	for _, providerName := range ca.providers {
		provider, err := ca.registry.Get(providerName)
		if err != nil {
			lastErr = fmt.Errorf("provider %s not found: %w", providerName, err)

			continue
		}

		authCtx, err := provider.Authenticate(ctx, r)
		if err != nil {
			lastErr = err

			continue
		}

		// Check required scopes if any
		if len(ca.scopes) > 0 {
			if !authCtx.HasScopes(ca.scopes...) {
				lastErr = fmt.Errorf("missing required scopes: %v", ca.scopes)

				continue
			}
		}

		return authCtx, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("authentication failed: %w", lastErr)
	}

	return nil, errors.New("no auth providers succeeded")
}

// RequireAuth sets the auth providers (OR logic).
func (ca *connectionAuthenticator) RequireAuth(providers ...string) error {
	if len(providers) == 0 {
		return errors.New("at least one provider required")
	}

	ca.providers = providers

	return nil
}

// RequireScopes sets required scopes (AND logic).
func (ca *connectionAuthenticator) RequireScopes(scopes ...string) error {
	ca.scopes = scopes

	return nil
}
