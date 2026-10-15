package forge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/logger"
)

// An error a global middleware returns reaches the client with its status,
// and the HTTP metrics record that status. Before tracing and metrics moved
// out of the forge chain, the PureMiddleware wrapper around them dropped the
// error and the client got an empty 200.
func TestGlobalMiddlewareErrorReachesClient(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tracing bool
	}{
		{"tracing=on", true},
		{"tracing=off", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			t.Setenv("OTEL_SDK_DISABLED", "")

			tracing := tc.tracing
			a := New(
				WithAppName("rejecting"),
				WithAppLogger(logger.NewNoopLogger()),
				WithAppTracingConfig(TracingConfig{Enabled: &tracing}),
			)
			a.Router().UseGlobal(func(Handler) Handler {
				return func(Context) error { return Unauthorized("no") }
			})
			_ = a.Router().GET("/secret", func(c Context) error { return c.String(http.StatusOK, "ok") })

			rec := httptest.NewRecorder()
			a.Router().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/secret", nil))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}

			for _, s := range histogramLabels(t, a, "http_server_request_duration_seconds") {
				if s["http_response_status_code"] == "401" && s["_count"] == "1" {
					return
				}
			}

			t.Fatalf("no duration series with status 401: %v", histogramLabels(t, a, "http_server_request_duration_seconds"))
		})
	}
}
