package security

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// SessionContextKey is the context key for storing the session.
type SessionContextKey struct{}

// SessionMiddlewareOptions holds options for the session middleware.
type SessionMiddlewareOptions struct {
	// Store is the session store to use
	Store SessionStore

	// CookieManager is the cookie manager to use
	CookieManager *CookieManager

	// Config is the session configuration
	Config SessionConfig

	// Logger for logging
	Logger forge.Logger

	// Metrics for metrics
	Metrics forge.Metrics

	// OnSessionCreated is called when a new session is created
	OnSessionCreated func(session *Session)

	// OnSessionExpired is called when a session expires
	OnSessionExpired func(sessionID string)

	// SkipPaths is a list of paths to skip session handling
	SkipPaths []string
}

// SessionMiddleware creates middleware for session management.
func SessionMiddleware(opts SessionMiddlewareOptions) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(forgeCtx forge.Context) error {
			r := forgeCtx.Request()
			w := forgeCtx.Response()

			// Check if we should skip this path
			if shouldSkipPath(r.URL.Path, opts.SkipPaths) {
				return next(forgeCtx)
			}

			ctx := forgeCtx.Context()

			// Try to get session from cookie
			sessionID, err := opts.CookieManager.GetCookie(r, opts.Config.CookieName)
			if err != nil {
				// No session cookie, continue without session
				return next(forgeCtx)
			}

			// Get session from store
			session, err := opts.Store.Get(ctx, sessionID)
			if err != nil {
				if errors.Is(err, ErrSessionNotFound) || errors.Is(err, ErrSessionExpired) {
					// Session not found or expired, delete cookie
					opts.CookieManager.DeleteCookie(w, opts.Config.CookieName, nil)

					if opts.OnSessionExpired != nil {
						opts.OnSessionExpired(sessionID)
					}

					if opts.Metrics != nil {
						opts.Metrics.Counter("security.sessions.expired_on_access").Inc()
					}

					return next(forgeCtx)
				}

				// Other error, log and continue
				if opts.Logger != nil {
					opts.Logger.Error("failed to get session",
						forge.F("session_id", sessionID),
						forge.F("error", err),
					)
				}

				return next(forgeCtx)
			}

			// Auto-renew session if enabled
			if opts.Config.AutoRenew {
				if err := opts.Store.Touch(ctx, sessionID, opts.Config.TTL); err != nil {
					if opts.Logger != nil {
						opts.Logger.Error("failed to touch session",
							forge.F("session_id", sessionID),
							forge.F("error", err),
						)
					}
				}
			}

			// Store session in Forge context
			forgeCtx.Set("session", session)

			// Track metrics
			if opts.Metrics != nil {
				opts.Metrics.Counter("security.sessions.accessed").Inc()
			}

			// Continue with session in context
			return next(forgeCtx)
		}
	}
}

// CreateSession creates a new session and sets the session cookie.
func CreateSession(
	ctx context.Context,
	w http.ResponseWriter,
	userID string,
	store SessionStore,
	cookieManager *CookieManager,
	config SessionConfig,
	metadata map[string]any,
) (*Session, error) {
	// Create new session
	session, err := NewSession(userID, config.TTL)
	if err != nil {
		return nil, err
	}

	// Add metadata to session
	maps.Copy(session.Data, metadata)

	// Store session
	if err := store.Create(ctx, session, config.TTL); err != nil {
		return nil, err
	}

	// Set session cookie
	cookieOpts := &CookieOptions{
		MaxAge: int(config.TTL.Seconds()),
	}
	cookieManager.SetCookie(w, config.CookieName, session.ID, cookieOpts)

	return session, nil
}

// GetSession retrieves the session from standard context (for backward compatibility).
func GetSession(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(SessionContextKey{}).(*Session)

	return session, ok
}

// GetSessionFromForgeContext retrieves the session from Forge context.
func GetSessionFromForgeContext(ctx forge.Context) (*Session, bool) {
	val := ctx.Get("session")
	if val == nil {
		return nil, false
	}

	session, ok := val.(*Session)

	return session, ok
}

// MustGetSession retrieves the session from context or panics.
func MustGetSession(ctx context.Context) *Session {
	session, ok := GetSession(ctx)
	if !ok {
		panic("session not found in context")
	}

	return session
}

// MustGetSessionFromForgeContext retrieves the session from Forge context or panics.
func MustGetSessionFromForgeContext(ctx forge.Context) *Session {
	session, ok := GetSessionFromForgeContext(ctx)
	if !ok {
		panic("session not found in context")
	}

	return session
}

// DestroySession destroys the session and deletes the cookie.
func DestroySession(
	ctx context.Context,
	w http.ResponseWriter,
	store SessionStore,
	cookieManager *CookieManager,
	cookieName string,
) error {
	// Get session from context
	session, ok := GetSession(ctx)
	if !ok {
		return nil // No session to destroy
	}

	// Delete session from store
	if err := store.Delete(ctx, session.ID); err != nil {
		return err
	}

	// Delete session cookie
	cookieManager.DeleteCookie(w, cookieName, nil)

	return nil
}

// UpdateSession updates the session data in the store.
func UpdateSession(
	ctx context.Context,
	store SessionStore,
	ttl time.Duration,
) error {
	session, ok := GetSession(ctx)
	if !ok {
		return ErrSessionNotFound
	}

	return store.Update(ctx, session, ttl)
}

// RequireSession creates middleware that requires a valid session.
func RequireSession(unauthorizedHandler forge.Handler) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			_, ok := GetSessionFromForgeContext(ctx)
			if !ok {
				return unauthorizedHandler(ctx)
			}

			return next(ctx)
		}
	}
}

// RequireSessionFunc creates middleware that requires a valid session with a handler function.
func RequireSessionFunc(unauthorizedHandler forge.Handler) forge.Middleware {
	return RequireSession(unauthorizedHandler)
}

// RequireSessionSimple creates middleware that returns 401 Unauthorized if no session exists.
func RequireSessionSimple() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			_, ok := GetSessionFromForgeContext(ctx)
			if !ok {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			return next(ctx)
		}
	}
}

// shouldSkipPath checks if the path should skip session handling.
func shouldSkipPath(path string, skipPaths []string) bool {
	for _, skipPath := range skipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}
