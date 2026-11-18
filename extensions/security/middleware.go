package security

import (
	"context"
	"maps"
	"net/http"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
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
			if err == nil && sessionTimedOut(session, opts.Config, time.Now()) {
				// Idle or absolute timeout reached. Remove it so the ID can
				// never be used again, then treat it like any expired session.
				_ = opts.Store.Delete(ctx, sessionID)
				err = ErrSessionExpired
			}

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

			// Record the access. With AutoRenew the expiry moves forward by
			// TTL, capped at the absolute lifetime. Without it the expiry is
			// left where it is, but LastAccessedAt still has to move or the
			// idle timeout would fire on a session that is in active use.
			if opts.Config.AutoRenew || opts.Config.IdleTimeout > 0 {
				now := time.Now()
				ttl := sessionRenewTTL(session, opts.Config, now)

				if err := opts.Store.Touch(ctx, sessionID, ttl); err != nil {
					if opts.Logger != nil {
						opts.Logger.Error("failed to touch session",
							forge.F("session_id", sessionID),
							forge.F("error", err),
						)
					}
				} else {
					session.LastAccessedAt = now
					session.ExpiresAt = now.Add(ttl)
				}
			}

			// Publish the session in both places handlers look for it. The
			// forge context serves GetSessionFromForgeContext, and the request's
			// context.Context serves GetSession, DestroySession and
			// UpdateSession. Setting only the forge value left those helpers
			// blind, so logout silently did nothing.
			forgeCtx.Set("session", session)
			forgeCtx.WithContext(WithSession(ctx, session))

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

// GetSession retrieves the session that SessionMiddleware placed in the
// request's context.Context.
func GetSession(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(SessionContextKey{}).(*Session)
	if !ok || session == nil {
		return nil, false
	}

	return session, true
}

// WithSession returns a copy of ctx that carries session under
// SessionContextKey, so GetSession, DestroySession and UpdateSession can find
// it. SessionMiddleware does this for every request with a valid session
// cookie; call it yourself when you create a session mid-request and want
// later code in the same request to see it.
func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, SessionContextKey{}, session)
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
	// A missing session is reported rather than swallowed. Returning nil here
	// made a logout handler believe it had ended the session when it had not.
	session, ok := GetSession(ctx)
	if !ok || session == nil {
		return ErrSessionNotFound
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
	if !ok || session == nil {
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

// defaultSessionAbsoluteTimeout caps a session's lifetime when
// SessionConfig.AbsoluteTimeout is not set.
const defaultSessionAbsoluteTimeout = 7 * 24 * time.Hour

// sessionAbsoluteTimeout returns the maximum lifetime of a session under cfg.
func sessionAbsoluteTimeout(cfg SessionConfig) time.Duration {
	if cfg.AbsoluteTimeout > 0 {
		return cfg.AbsoluteTimeout
	}

	return max(defaultSessionAbsoluteTimeout, cfg.TTL)
}

// sessionTimedOut reports whether the session has passed its idle timeout or
// its absolute lifetime. Stores only track ExpiresAt, so these two limits are
// enforced here, on the request path, for every store.
func sessionTimedOut(session *Session, cfg SessionConfig, now time.Time) bool {
	if cfg.IdleTimeout > 0 && !session.LastAccessedAt.IsZero() &&
		now.Sub(session.LastAccessedAt) > cfg.IdleTimeout {
		return true
	}

	if !session.CreatedAt.IsZero() && now.After(session.CreatedAt.Add(sessionAbsoluteTimeout(cfg))) {
		return true
	}

	return false
}

// sessionRenewTTL returns the TTL to pass to SessionStore.Touch for this
// access. AutoRenew extends to a full TTL; otherwise the current expiry is
// kept. Either way the result never reaches past the absolute lifetime.
func sessionRenewTTL(session *Session, cfg SessionConfig, now time.Time) time.Duration {
	ttl := session.ExpiresAt.Sub(now)
	if cfg.AutoRenew && cfg.TTL > 0 {
		ttl = cfg.TTL
	}

	if !session.CreatedAt.IsZero() {
		if remaining := session.CreatedAt.Add(sessionAbsoluteTimeout(cfg)).Sub(now); ttl > remaining {
			ttl = remaining
		}
	}

	return max(ttl, 0)
}
