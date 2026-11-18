package forge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrapeThroughRouter(t *testing.T, a App) string {
	t.Helper()

	w := httptest.NewRecorder()
	a.Router().ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_/metrics", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("/_/metrics = %d", w.Code)
	}

	return w.Body.String()
}

// /_/metrics reuses a render for MetricsScrapeCacheTTL, and a negative TTL
// renders every scrape afresh.
func TestMetricsEndpoint_ScrapeCacheTTL(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	for _, tc := range []struct {
		name   string
		ttl    time.Duration
		cached bool
	}{
		// A minute rather than the default second, so the test cannot race
		// the window.
		{"cached", time.Minute, true},
		{"disabled", -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(WithAppEnvironment("test"), WithMetricsScrapeCacheTTL(tc.ttl), WithAppLogger(NewNoopLogger()))
			if err := a.Start(context.Background()); err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = a.Stop(context.Background()) })

			scrapeThroughRouter(t, a)
			a.Metrics().Counter("scrape_cache_probe_total").Inc()

			seen := strings.Contains(scrapeThroughRouter(t, a), "scrape_cache_probe_total")
			if seen == tc.cached {
				t.Errorf("second scrape saw the new series = %v, want %v", seen, !tc.cached)
			}
		})
	}
}
