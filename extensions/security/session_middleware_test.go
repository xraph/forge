package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

func newSessionTestMiddleware(t *testing.T, store SessionStore, cfg SessionConfig) forge.Middleware {
	t.Helper()

	return SessionMiddleware(SessionMiddlewareOptions{
		Store:         store,
		CookieManager: NewCookieManager(DefaultCookieOptions()),
		Config:        cfg,
		Logger:        logger.NewNoopLogger(),
	})
}

// Logout used to be a silent no-op: SessionMiddleware only stored the session
// in the forge context, while DestroySession read the standard context, which
// nothing populated. The session stayed in the store and no cookie was cleared.
func TestDestroySession_AfterMiddlewareEndsTheSession(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	cm := NewCookieManager(DefaultCookieOptions())
	cfg := DefaultConfig().Session

	sess, err := NewSession("alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Create(context.Background(), sess, time.Hour); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: sess.ID})

	w := httptest.NewRecorder()

	var destroyErr error

	handler := newSessionTestMiddleware(t, store, cfg)(func(ctx forge.Context) error {
		if _, ok := GetSession(ctx.Context()); !ok {
			t.Error("GetSession found nothing in the request context")
		}

		destroyErr = DestroySession(ctx.Context(), ctx.Response(), store, cm, cfg.CookieName)

		return ctx.String(http.StatusOK, "bye")
	})
	_ = handler(forge_http.NewContext(w, req, nil))

	if destroyErr != nil {
		t.Fatalf("DestroySession: %v", destroyErr)
	}

	if _, err := store.Get(context.Background(), sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session still in store after logout, Get err = %v", err)
	}

	setCookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, cfg.CookieName+"=") || !strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("logout did not clear the cookie, Set-Cookie = %q", setCookie)
	}
}

func TestSessionHelpers_ReportMissingSession(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	cm := NewCookieManager(DefaultCookieOptions())

	err := DestroySession(context.Background(), httptest.NewRecorder(), store, cm, "forge_session")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("DestroySession without a session = %v, want ErrSessionNotFound", err)
	}

	if err := UpdateSession(context.Background(), store, time.Hour); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("UpdateSession without a session = %v, want ErrSessionNotFound", err)
	}

	if _, ok := GetSession(WithSession(context.Background(), nil)); ok {
		t.Fatal("GetSession reported a nil session as present")
	}
}

// UpdateSession must see the session the middleware loaded and persist changes
// a handler made to it.
func TestUpdateSession_AfterMiddlewarePersistsData(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	cfg := DefaultConfig().Session

	sess, _ := NewSession("alice", time.Hour)
	_ = store.Create(context.Background(), sess, time.Hour)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/profile", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: sess.ID})

	handler := newSessionTestMiddleware(t, store, cfg)(func(ctx forge.Context) error {
		s, ok := GetSession(ctx.Context())
		if !ok {
			t.Fatal("no session in request context")
		}

		s.SetData("theme", "dark")

		return UpdateSession(ctx.Context(), store, time.Hour)
	})

	if err := handler(forge_http.NewContext(httptest.NewRecorder(), req, nil)); err != nil {
		t.Fatalf("handler: %v", err)
	}

	got, err := store.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	if v, _ := got.GetData("theme"); v != "dark" {
		t.Fatalf("theme = %v, want dark", v)
	}
}

// The audit middleware must attribute a request to the session even when it
// runs before SessionMiddleware in the chain.
func TestAuditMiddleware_AttributesSessionUser(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	cfg := DefaultConfig().Session

	sess, _ := NewSession("alice", time.Hour)
	_ = store.Create(context.Background(), sess, time.Hour)

	var got *AuditEntry

	auditCfg := DefaultAuditConfig()
	auditCfg.Level = "all"
	auditCfg.Handler = func(e *AuditEntry) { got = e }
	al := NewAuditLogger(auditCfg, logger.NewNoopLogger())

	ok := func(ctx forge.Context) error { return ctx.String(http.StatusOK, "ok") }
	handler := AuditMiddleware(al)(newSessionTestMiddleware(t, store, cfg)(ok))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: sess.ID})
	_ = handler(forge_http.NewContext(httptest.NewRecorder(), req, nil))

	if got == nil {
		t.Fatal("no audit entry logged")
	}

	if got.UserID != "alice" || got.SessionID != sess.ID {
		t.Fatalf("audit entry user=%q session=%q, want alice/%s", got.UserID, got.SessionID, sess.ID)
	}
}

// Two requests for the same session used to receive the same *Session from
// the in-memory store, so concurrent SetData calls raced (and could crash
// with concurrent map writes). Run with -race.
func TestInMemorySessionStore_ConcurrentRequestsDoNotShareSession(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	s, _ := NewSession("u", time.Hour)
	_ = store.Create(context.Background(), s, time.Hour)

	var wg sync.WaitGroup

	for i := range 4 {
		wg.Go(func() {
			for j := range 500 {
				got, err := store.Get(context.Background(), s.ID)
				if err != nil {
					t.Error(err)

					return
				}

				got.SetData("k", i*j)

				_ = store.Touch(context.Background(), s.ID, time.Hour)
				_ = got.ExpiresAt
				_ = store.Update(context.Background(), got, time.Hour)
			}
		})
	}

	wg.Wait()
}

func TestInMemorySessionStore_ReturnsCopies(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	s, _ := NewSession("u", time.Hour)
	_ = store.Create(context.Background(), s, time.Hour)

	// Mutating the caller's object after Create must not reach the store.
	s.SetData("after-create", true)

	a, _ := store.Get(context.Background(), s.ID)
	if _, ok := a.GetData("after-create"); ok {
		t.Fatal("Create kept a reference to the caller's session")
	}

	a.SetData("unsaved", true)

	b, _ := store.Get(context.Background(), s.ID)
	if a == b {
		t.Fatal("Get returned the same pointer twice")
	}

	if _, ok := b.GetData("unsaved"); ok {
		t.Fatal("an unsaved change on one copy leaked into another")
	}
}

func runSessionRequest(t *testing.T, store SessionStore, cfg SessionConfig, id string) (*Session, *httptest.ResponseRecorder) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: id})

	w := httptest.NewRecorder()

	var got *Session

	_ = newSessionTestMiddleware(t, store, cfg)(func(ctx forge.Context) error {
		got, _ = GetSessionFromForgeContext(ctx)

		return ctx.String(http.StatusOK, "ok")
	})(forge_http.NewContext(w, req, nil))

	return got, w
}

func TestSessionMiddleware_EnforcesIdleTimeout(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	cfg := DefaultConfig().Session
	cfg.IdleTimeout = time.Minute

	s, _ := NewSession("u", time.Hour)
	s.LastAccessedAt = time.Now().Add(-2 * time.Minute)
	_ = store.Create(context.Background(), s, time.Hour)

	got, w := runSessionRequest(t, store, cfg, s.ID)
	if got != nil {
		t.Fatal("idle session was accepted")
	}

	if !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("idle session cookie was not cleared")
	}

	if _, err := store.Get(context.Background(), s.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("idle session still in store: %v", err)
	}
}

func TestSessionMiddleware_RefreshesLastAccessWithoutAutoRenew(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	cfg := DefaultConfig().Session
	cfg.AutoRenew = false
	cfg.IdleTimeout = time.Minute

	s, _ := NewSession("u", time.Hour)
	s.LastAccessedAt = time.Now().Add(-30 * time.Second)
	_ = store.Create(context.Background(), s, time.Hour)

	before, _ := store.Get(context.Background(), s.ID)

	if got, _ := runSessionRequest(t, store, cfg, s.ID); got == nil {
		t.Fatal("active session was rejected")
	}

	after, _ := store.Get(context.Background(), s.ID)
	if !after.LastAccessedAt.After(before.LastAccessedAt) {
		t.Fatal("LastAccessedAt was not refreshed")
	}

	if d := after.ExpiresAt.Sub(before.ExpiresAt); d < -time.Second || d > time.Second {
		t.Fatalf("expiry moved by %v without AutoRenew", d)
	}
}

func TestSessionMiddleware_AutoRenewStopsAtAbsoluteTimeout(t *testing.T) {
	store := NewInMemorySessionStore(logger.NewNoopLogger(), nil)
	cfg := DefaultConfig().Session
	cfg.AutoRenew = true
	cfg.TTL = time.Hour
	cfg.AbsoluteTimeout = 2 * time.Hour

	// Created 90 minutes ago: a full renewal would reach 2h30m, past the cap.
	s, _ := NewSession("u", time.Hour)
	s.CreatedAt = time.Now().Add(-90 * time.Minute)
	_ = store.Create(context.Background(), s, time.Hour)

	if got, _ := runSessionRequest(t, store, cfg, s.ID); got == nil {
		t.Fatal("session inside its lifetime was rejected")
	}

	after, _ := store.Get(context.Background(), s.ID)

	limit := s.CreatedAt.Add(cfg.AbsoluteTimeout)
	if after.ExpiresAt.After(limit.Add(time.Second)) {
		t.Fatalf("renewed past the absolute timeout: expires %v, limit %v", after.ExpiresAt, limit)
	}

	// Once the absolute lifetime has passed the session is gone, renewals or not.
	old, _ := NewSession("u", time.Hour)
	old.CreatedAt = time.Now().Add(-3 * time.Hour)
	_ = store.Create(context.Background(), old, time.Hour)

	if got, _ := runSessionRequest(t, store, cfg, old.ID); got != nil {
		t.Fatal("session past its absolute timeout was accepted")
	}
}
