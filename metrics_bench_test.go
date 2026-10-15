package forge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/logger"
)

// BenchmarkRequestObservability measures one GET /users/:id through forge.New
// with tracing and HTTP server metrics each on and off. The extmw=1 cases add
// one pass-through UseGlobal middleware, the way an extension does, because
// the global chain only builds a forge Context when something is registered
// there.
func BenchmarkRequestObservability(b *testing.B) {
	for _, tc := range []struct {
		name    string
		tracing bool
		metrics bool
		extMW   bool
	}{
		{"tracing=off/metrics=off/extmw=0", false, false, false},
		{"tracing=off/metrics=on/extmw=0", false, true, false},
		{"tracing=on/metrics=off/extmw=0", true, false, false},
		{"tracing=on/metrics=on/extmw=0", true, true, false},
		{"tracing=off/metrics=off/extmw=1", false, false, true},
		{"tracing=on/metrics=on/extmw=1", true, true, true},
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

			if tc.extMW {
				a.Router().UseGlobal(func(next Handler) Handler {
					return func(c Context) error { return next(c) }
				})
			}

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
