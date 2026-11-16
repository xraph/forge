package forge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

// 5xx bodies used to carry the internal error text in every environment
// except exactly "production", so an unset environment, "prod" or "staging"
// sent driver errors with hosts, users and databases to any client.
func TestExposeInternalErrors_OnlyExplicitDevelopment(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Cleanup(func() { shared.SetExposeInternalErrors(false) })

	tru, fals := true, false

	cases := []struct {
		name   string
		build  func() App
		expose bool
	}{
		{"New with no environment", func() App { return New() }, false},
		{"DefaultAppConfig untouched", func() App { return NewApp(DefaultAppConfig()) }, false},
		{"empty literal", func() App { return NewApp(AppConfig{}) }, false},
		{"prod", func() App { return New(WithAppEnvironment("prod")) }, false},
		{"staging", func() App { return New(WithAppEnvironment("staging")) }, false},
		{"production", func() App { return New(WithAppEnvironment("production")) }, false},
		{"qa", func() App { return New(WithAppEnvironment("qa")) }, false},
		{"development option", func() App { return New(WithAppEnvironment("development")) }, true},
		{"Development literal", func() App { return NewApp(AppConfig{Environment: "Development"}) }, true},
		{"dev", func() App { return New(WithAppEnvironment("dev")) }, true},
		{"local", func() App { return New(WithAppEnvironment("local")) }, true},
		{"test", func() App { return New(WithAppEnvironment("test")) }, true},
		{"override on in production", func() App {
			return NewApp(AppConfig{Environment: "production", ExposeInternalErrors: &tru})
		}, true},
		{"override off in development", func() App {
			return New(WithAppEnvironment("development"), WithExposeInternalErrors(false))
		}, false},
		{"override off literal", func() App {
			return NewApp(AppConfig{Environment: "test", ExposeInternalErrors: &fals})
		}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.build()

			_ = a.Router().GET("/boom", func(Context) error {
				return InternalError(errors.New(`pq: password authentication failed for user "app_rw" host=10.0.3.17 database=prod`))
			})

			w := httptest.NewRecorder()
			a.Router().ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/boom", nil))

			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", w.Code)
			}

			leaked := strings.Contains(w.Body.String(), "10.0.3.17")
			if leaked != tc.expose {
				t.Errorf("error text in body = %v, want %v; body %s", leaked, tc.expose, w.Body.String())
			}
		})
	}
}
