package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkMetricsScrape measures a /_/metrics scrape through the router on an
// app that has served traffic on many routes, so the HTTP series dominate.
// Each route saw three statuses, which is what makes a real scrape large.
func BenchmarkMetricsScrape(b *testing.B) {
	for _, routes := range []int{0, 50, 200} {
		b.Run(fmt.Sprintf("routes=%d", routes), func(b *testing.B) {
			b.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

			cfg := DefaultAppConfig()
			cfg.EnableConfigAutoDiscovery = false
			cfg.Logger = NewNoopLogger()
			a := New(WithConfig(cfg))

			for i := range routes {
				_ = a.Router().GET(fmt.Sprintf("/r%d/:id", i), func(c Context) error {
					switch c.Param("id") {
					case "e":
						return c.String(http.StatusInternalServerError, "x")
					case "n":
						return c.String(http.StatusNotFound, "x")
					default:
						return c.String(http.StatusOK, "ok")
					}
				})
			}

			if err := a.Start(context.Background()); err != nil {
				b.Fatal(err)
			}

			defer func() { _ = a.Stop(context.Background()) }()

			h := a.Router()

			for i := range routes {
				for _, id := range []string{"1", "e", "n"} {
					h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(b.Context(), http.MethodGet, fmt.Sprintf("/r%d/%s", i, id), nil))
				}
			}

			req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/_/metrics", nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				b.Fatalf("scrape status %d", rr.Code)
			}

			b.ReportAllocs()

			for b.Loop() {
				h.ServeHTTP(&discardResponseWriter{h: http.Header{}}, req)
			}

			b.ReportMetric(float64(rr.Body.Len()), "resp_bytes")
		})
	}
}

type discardResponseWriter struct{ h http.Header }

func (d *discardResponseWriter) Header() http.Header         { return d.h }
func (d *discardResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardResponseWriter) WriteHeader(int)             {}
