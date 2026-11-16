package security

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

func TestShouldSkipPath_SegmentBoundary(t *testing.T) {
	cases := []struct {
		path  string
		skips []string
		want  bool
	}{
		{"/public", []string{"/public"}, true},
		{"/public/", []string{"/public"}, true},
		{"/public/css/app.css", []string{"/public"}, true},
		{"/publicity-admin", []string{"/public"}, false},
		{"/public-admin", []string{"/public"}, false},
		{"/publi", []string{"/public"}, false},
		{"/public", []string{"/public/"}, true},
		{"/public/x", []string{"/public/"}, true},
		{"/publicity", []string{"/public/"}, false},
		{"/anything", []string{"/"}, true},
		{"/anything", []string{""}, false},
		{"/health", []string{"/metrics", "/health"}, true},
		{"/healthz", []string{"/health"}, false},
		{"/", nil, false},
	}

	for _, c := range cases {
		if got := shouldSkipPath(c.path, c.skips); got != c.want {
			t.Errorf("shouldSkipPath(%q, %q) = %v, want %v", c.path, c.skips, got, c.want)
		}
	}
}

// A skip entry of "/public" used to exempt "/publicity-admin" from JWT
// authentication because the match was a bare string prefix.
func TestJWTMiddleware_SkipPathDoesNotCoverSiblingRoute(t *testing.T) {
	cfg := DefaultJWTConfig()
	cfg.SigningKey = "k0123456789abcdef0123456789abcdef"
	cfg.SkipPaths = []string{"/public"}

	jm, err := NewJWTManager(cfg, logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	mw := JWTMiddleware(jm)(func(ctx forge.Context) error { return ctx.String(http.StatusOK, "in") })

	for path, want := range map[string]int{
		"/public":            http.StatusOK,
		"/public/index.html": http.StatusOK,
		"/publicity-admin":   http.StatusUnauthorized,
		"/admin/users":       http.StatusUnauthorized,
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		_ = mw(forge_http.NewContext(w, req, nil))

		if w.Code != want {
			t.Errorf("%s: status %d, want %d", path, w.Code, want)
		}
	}
}

// Every middleware with SkipPaths goes through the same helper, so a sibling
// route is never exempted by any of them.
func TestSkipPaths_AllManagersUseSegmentBoundary(t *testing.T) {
	skips := []string{"/public"}
	log := logger.NewNoopLogger()

	csrfCfg := DefaultCSRFConfig()
	csrfCfg.SkipPaths = skips
	rlCfg := DefaultRateLimitConfig()
	rlCfg.SkipPaths = skips
	corsCfg := DefaultCORSConfig()
	corsCfg.SkipPaths = skips
	keyCfg := DefaultAPIKeyConfig()
	keyCfg.SkipPaths = skips
	hdrCfg := DefaultSecurityHeadersConfig()
	hdrCfg.SkipPaths = skips
	jwtCfg := DefaultJWTConfig()
	jwtCfg.SigningKey = "k0123456789abcdef0123456789abcdef"
	jwtCfg.SkipPaths = skips

	jm, err := NewJWTManager(jwtCfg, log)
	if err != nil {
		t.Fatal(err)
	}

	rl := NewMemoryRateLimiter(rlCfg, log, nil)

	checks := map[string]func(string) bool{
		"csrf":    NewCSRFProtection(csrfCfg, log).shouldSkipPath,
		"rate":    rl.shouldSkipPath,
		"cors":    NewCORSManager(corsCfg, log).shouldSkipPath,
		"apikey":  NewAPIKeyManager(keyCfg, log).shouldSkipPath,
		"headers": NewSecurityHeadersManager(hdrCfg, log).shouldSkipPath,
		"jwt":     jm.shouldSkipPath,
	}

	for name, skip := range checks {
		if !skip("/public/x") {
			t.Errorf("%s: /public/x should be skipped", name)
		}

		if skip("/publicity-admin") {
			t.Errorf("%s: /publicity-admin must not be skipped", name)
		}
	}
}
