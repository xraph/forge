package security

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

func okHandler(ctx forge.Context) error { return ctx.String(http.StatusOK, "OK") }

func csrfCookieFrom(t *testing.T, w *httptest.ResponseRecorder, name string) string {
	t.Helper()

	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}

	return ""
}

// Unauthenticated GETs used to store one map entry per distinct csrf_token
// cookie value, keyed by that attacker-chosen value. 2000 requests with 60 KB
// cookies held about 130 MB. The middleware must hold no per-request state.
func TestCSRFMiddleware_DoesNotStoreClientSuppliedValues(t *testing.T) {
	csrf := NewCSRFProtection(DefaultCSRFConfig(), logger.NewNoopLogger())
	mw := CSRFMiddleware(csrf, NewCookieManager(DefaultCookieOptions()))(okHandler)

	big := strings.Repeat("A", 60*1024)

	for i := range 2000 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: fmt.Sprintf("%d%s", i, big)})

		w := httptest.NewRecorder()
		_ = mw(forge_http.NewContext(w, req, nil))

		// The planted value must be replaced by a server-signed token.
		if got := csrfCookieFrom(t, w, "csrf_token"); got == "" || strings.Contains(got, big) {
			t.Fatalf("request %d: planted cookie was not replaced", i)
		}
	}

	csrf.mu.Lock()
	n := len(csrf.tokens)
	csrf.mu.Unlock()

	if n != 0 {
		t.Fatalf("middleware stored %d entries, want 0", n)
	}
}

func TestCSRFMiddleware_SignedDoubleSubmitRoundTrip(t *testing.T) {
	csrf := NewCSRFProtection(DefaultCSRFConfig(), logger.NewNoopLogger())
	mw := CSRFMiddleware(csrf, NewCookieManager(DefaultCookieOptions()))(okHandler)

	// A GET hands out a token in the cookie and the response header.
	w := httptest.NewRecorder()
	_ = mw(forge_http.NewContext(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), nil))

	token := csrfCookieFrom(t, w, "csrf_token")
	if token == "" || w.Header().Get("X-Csrf-Token") != token {
		t.Fatalf("cookie %q and header %q must carry the same token", token, w.Header().Get("X-Csrf-Token"))
	}

	post := func(cookie, header string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "csrf_token", Value: cookie})
		}

		if header != "" {
			req.Header.Set("X-Csrf-Token", header)
		}

		rec := httptest.NewRecorder()
		_ = mw(forge_http.NewContext(rec, req, nil))

		return rec.Code
	}

	if code := post(token, token); code != http.StatusOK {
		t.Fatalf("matching signed token: status %d, want 200", code)
	}

	other, _ := csrf.IssueToken("")
	if code := post(token, other); code != http.StatusForbidden {
		t.Fatalf("mismatched token: status %d, want 403", code)
	}

	// An attacker who can set a cookie (for example from a sibling
	// subdomain) still cannot pick a value the server accepts.
	if code := post("attacker-value", "attacker-value"); code != http.StatusForbidden {
		t.Fatalf("unsigned planted token: status %d, want 403", code)
	}

	// A token signed by a different key is rejected.
	foreign, _ := NewCSRFProtection(DefaultCSRFConfig(), logger.NewNoopLogger()).IssueToken("")
	if code := post(foreign, foreign); code != http.StatusForbidden {
		t.Fatalf("token from another key: status %d, want 403", code)
	}

	// A second GET keeps the valid token instead of rotating it.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})

	w2 := httptest.NewRecorder()
	_ = mw(forge_http.NewContext(w2, req, nil))

	if csrfCookieFrom(t, w2, "csrf_token") != "" || w2.Header().Get("X-Csrf-Token") != token {
		t.Fatal("a valid token should be kept and echoed, not reissued")
	}
}

func TestCSRFProtection_VerifyToken(t *testing.T) {
	cfg := DefaultCSRFConfig()
	cfg.Secret = "shared-secret-for-all-replicas"
	a := NewCSRFProtection(cfg, logger.NewNoopLogger())
	b := NewCSRFProtection(cfg, logger.NewNoopLogger())

	tok, err := a.IssueToken("sess-1")
	if err != nil {
		t.Fatal(err)
	}

	if err := b.VerifyToken(tok, "sess-1"); err != nil {
		t.Fatalf("replica with the same Secret rejected the token: %v", err)
	}

	if err := a.VerifyToken(tok, "sess-2"); !errors.Is(err, ErrCSRFTokenInvalid) {
		t.Fatalf("wrong binding: %v, want ErrCSRFTokenInvalid", err)
	}

	if err := a.VerifyToken("", ""); !errors.Is(err, ErrCSRFTokenMissing) {
		t.Fatalf("empty token: %v, want ErrCSRFTokenMissing", err)
	}

	// Flip one character inside the signature. The last character is avoided
	// because part of it is padding bits.
	tampered := []byte(tok)

	i := len(tampered) - 5
	if tampered[i] == 'A' {
		tampered[i] = 'B'
	} else {
		tampered[i] = 'A'
	}

	if err := a.VerifyToken(string(tampered), "sess-1"); !errors.Is(err, ErrCSRFTokenInvalid) {
		t.Fatalf("tampered token: %v, want ErrCSRFTokenInvalid", err)
	}

	expiredCfg := cfg
	expiredCfg.TTL = -time.Hour
	exp := NewCSRFProtection(expiredCfg, logger.NewNoopLogger())
	exp.config.TTL = -time.Hour // NewCSRFProtection replaces a non-positive TTL

	old, _ := exp.IssueToken("")
	if err := exp.VerifyToken(old, ""); !errors.Is(err, ErrCSRFTokenExpired) {
		t.Fatalf("expired token: %v, want ErrCSRFTokenExpired", err)
	}
}

func TestCSRFMiddleware_BindToSession(t *testing.T) {
	cfg := DefaultCSRFConfig()
	cfg.BindToSession = true
	csrf := NewCSRFProtection(cfg, logger.NewNoopLogger())

	withSession := func(id string, next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			ctx.Set("session", &Session{ID: id})

			return next(ctx)
		}
	}

	mw := withSession("sess-a", CSRFMiddleware(csrf, NewCookieManager(DefaultCookieOptions()))(okHandler))
	mwOther := withSession("sess-b", CSRFMiddleware(csrf, NewCookieManager(DefaultCookieOptions()))(okHandler))

	w := httptest.NewRecorder()
	_ = mw(forge_http.NewContext(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), nil))
	token := csrfCookieFrom(t, w, "csrf_token")

	post := func(h forge.Handler) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
		req.Header.Set("X-Csrf-Token", token)

		rec := httptest.NewRecorder()
		_ = h(forge_http.NewContext(rec, req, nil))

		return rec.Code
	}

	if code := post(mw); code != http.StatusOK {
		t.Fatalf("same session: status %d, want 200", code)
	}

	if code := post(mwOther); code != http.StatusForbidden {
		t.Fatalf("token from another session: status %d, want 403", code)
	}
}

func TestCSRFProtection_SetTokenIsBounded(t *testing.T) {
	csrf := NewCSRFProtection(DefaultCSRFConfig(), logger.NewNoopLogger())

	for i := range maxStoredCSRFTokens + 50 {
		csrf.SetToken(fmt.Sprintf("s%d", i), "t")
	}

	csrf.mu.Lock()
	n := len(csrf.tokens)
	csrf.mu.Unlock()

	if n > maxStoredCSRFTokens {
		t.Fatalf("store holds %d entries, cap is %d", n, maxStoredCSRFTokens)
	}

	if err := csrf.ValidateToken(fmt.Sprintf("s%d", maxStoredCSRFTokens+49), "t"); err != nil {
		t.Fatalf("most recent token missing: %v", err)
	}
}

func TestCSRFProtection_TokenLookupParsedOnce(t *testing.T) {
	for lookup, build := range map[string]func(string) *http.Request{
		"header:X-My-Token": func(tok string) *http.Request {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			r.Header.Set("X-My-Token", tok)

			return r
		},
		"query:csrf": func(tok string) *http.Request {
			return httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/?csrf="+tok, nil)
		},
		"form:csrf": func(tok string) *http.Request {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("csrf="+tok))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			return r
		},
	} {
		cfg := DefaultCSRFConfig()
		cfg.TokenLookup = lookup
		csrf := NewCSRFProtection(cfg, logger.NewNoopLogger())

		if got := csrf.extractTokenFromRequest(build("tok123")); got != "tok123" {
			t.Errorf("%s: extracted %q, want tok123", lookup, got)
		}
	}
}
