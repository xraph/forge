package forge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/logger"
)

// BenchmarkRequestObservability measures one GET /users/:id through forge.New
// with tracing and HTTP server metrics each on and off.
func BenchmarkRequestObservability(b *testing.B) {
	for _, tc := range []struct {
		name    string
		tracing bool
		metrics bool
	}{
		{"tracing=off/metrics=off", false, false},
		{"tracing=off/metrics=on", false, true},
		{"tracing=on/metrics=off", true, false},
		{"tracing=on/metrics=on", true, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			b.Setenv("OTEL_SDK_DISABLED", "")

			tracing := tc.tracing
			metricsCfg := DefaultMetricsConfig()
			metricsCfg.Features.HTTPMetrics = tc.metrics

			a := New(
				WithAppName("bench"),
				WithAppLogger(logger.NewNoopLogger()),
				WithAppTracingConfig(TracingConfig{Enabled: &tracing}),
				WithAppMetricsConfig(metricsCfg),
			)
			_ = a.Router().GET("/users/:id", func(c Context) error { return c.String(http.StatusOK, "ok") })

			h := a.Router()
			req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/users/42", nil)

			b.ReportAllocs()

			for b.Loop() {
				h.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}
