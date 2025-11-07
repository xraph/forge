package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
	"github.com/xraph/forge/internal/logger"
)

func TestCSRFMiddleware_SafeMethods(t *testing.T) {
	log := logger.NewTestLogger()
	csrf := NewCSRFProtection(DefaultCSRFConfig(), log)
	cookieMgr := NewCookieManager(CookieOptions{
		Secure:   false,
		HttpOnly: true,
		SameSite: SameSiteLax,
		Path:     "/",
	})

	middleware := CSRFMiddleware(csrf, cookieMgr)

	// Test safe methods (GET, HEAD, OPTIONS) - should pass without CSRF token
	safeMethods := []string{"GET", "HEAD", "OPTIONS"}

	for _, method := range safeMethods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/test", nil)
			w := httptest.NewRecorder()
			ctx := di.NewContext(w, req, nil)

			called := false
			next := func(ctx forge.Context) error {
				called = true
				return ctx.String(http.StatusOK, "OK")
			}

			handler := middleware(next)
			err := handler(ctx)

			if err != nil {
				t.Errorf("unexpected error for %s method: %v", method, err)
			}

			if !called {
				t.Errorf("expected handler to be called for %s method", method)
			}

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200 for %s, got %d", method, w.Code)
			}
		})
	}
}

func TestCSRFMiddleware_UnsafeMethods_NoToken(t *testing.T) {
	log := logger.NewTestLogger()
	csrf := NewCSRFProtection(DefaultCSRFConfig(), log)
	cookieMgr := NewCookieManager(CookieOptions{
		Secure:   false,
		HttpOnly: true,
		SameSite: SameSiteLax,
		Path:     "/",
	})

	middleware := CSRFMiddleware(csrf, cookieMgr)

	// Test unsafe methods without token - should fail
	unsafeMethods := []string{"POST", "PUT", "DELETE", "PATCH"}

	for _, method := range unsafeMethods {
		t.Run(method+"_NoToken", func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/test", nil)
			w := httptest.NewRecorder()
			ctx := di.NewContext(w, req, nil)

			called := false
			next := func(ctx forge.Context) error {
				called = true
				return ctx.String(http.StatusOK, "OK")
			}

			handler := middleware(next)
			_ = handler(ctx)

			if called {
				t.Errorf("expected handler to not be called for %s method without token", method)
			}

			if w.Code != http.StatusForbidden {
				t.Errorf("expected status 403 for %s without token, got %d", method, w.Code)
			}
		})
	}
}

func TestCSRFMiddleware_SkipPaths(t *testing.T) {
	log := logger.NewTestLogger()
	config := DefaultCSRFConfig()
	config.SkipPaths = []string{"/api/public", "/webhooks"}
	csrf := NewCSRFProtection(config, log)
	cookieMgr := NewCookieManager(CookieOptions{
		Secure:   false,
		HttpOnly: true,
		SameSite: SameSiteLax,
		Path:     "/",
	})

	middleware := CSRFMiddleware(csrf, cookieMgr)

	// Test skipped paths with POST (should not require CSRF token)
	skipPaths := []string{"/api/public", "/webhooks"}

	for _, path := range skipPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("POST", path, nil)
			w := httptest.NewRecorder()
			ctx := di.NewContext(w, req, nil)

			called := false
			next := func(ctx forge.Context) error {
				called = true
				return ctx.String(http.StatusOK, "OK")
			}

			handler := middleware(next)
			err := handler(ctx)

			if err != nil {
				t.Errorf("unexpected error for skipped path: %v", err)
			}

			if !called {
				t.Error("expected handler to be called for skipped path")
			}

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200 for skipped path, got %d", w.Code)
			}
		})
	}
}

func TestCSRFMiddleware_DisabledConfig(t *testing.T) {
	log := logger.NewTestLogger()
	config := DefaultCSRFConfig()
	config.Enabled = false
	csrf := NewCSRFProtection(config, log)
	cookieMgr := NewCookieManager(CookieOptions{
		Secure:   false,
		HttpOnly: true,
		SameSite: SameSiteLax,
		Path:     "/",
	})

	middleware := CSRFMiddleware(csrf, cookieMgr)

	// Test POST without token when CSRF is disabled - should pass
	req := httptest.NewRequest("POST", "/api/test", nil)
	w := httptest.NewRecorder()
	ctx := di.NewContext(w, req, nil)

	called := false
	next := func(ctx forge.Context) error {
		called = true
		return ctx.String(http.StatusOK, "OK")
	}

	handler := middleware(next)
	err := handler(ctx)

	if err != nil {
		t.Errorf("unexpected error when CSRF is disabled: %v", err)
	}

	if !called {
		t.Error("expected handler to be called when CSRF is disabled")
	}

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 when CSRF is disabled, got %d", w.Code)
	}
}

func TestCSRFConfig_Validation(t *testing.T) {
	config := DefaultCSRFConfig()

	// Test default config is valid
	if config.Enabled != true {
		t.Error("expected default config to be enabled")
	}

	if config.TokenLength != 32 {
		t.Errorf("expected token length 32, got %d", config.TokenLength)
	}

	if config.TTL != 12*time.Hour {
		t.Errorf("expected TTL 12h, got %v", config.TTL)
	}

	if len(config.SafeMethods) == 0 {
		t.Error("expected safe methods to be defined")
	}
}

func TestCSRFConfig_TokenLookup(t *testing.T) {
	tests := []struct {
		name        string
		tokenLookup string
		expectType  string
	}{
		{"header lookup", "header:X-CSRF-Token", "header"},
		{"form lookup", "form:csrf_token", "form"},
		{"query lookup", "query:csrf", "query"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultCSRFConfig()
			config.TokenLookup = tt.tokenLookup

			if config.TokenLookup != tt.tokenLookup {
				t.Errorf("expected token lookup %s, got %s", tt.tokenLookup, config.TokenLookup)
			}
		})
	}
}
