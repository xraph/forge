package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
