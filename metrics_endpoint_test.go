package forge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/xraph/forge/internal/logger"
)

// TestMetricsEndpoint verifies that /_/metrics serves a valid Prometheus scrape
// including both runtime (go_goroutines) and user-registered (probe_total) metrics,
// and that the real promhttp handler is used (supporting content negotiation).
func TestMetricsEndpoint(t *testing.T) {
	app := NewApp(AppConfig{
		Name:          "metrics-test",
		Version:       "1.0.0",
		Environment:   "test",
		Logger:        logger.NewTestLogger(),
		MetricsConfig: DefaultMetricsConfig(),
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer app.Stop(ctx)

	// Register a Forge metric via the public accessor.
	app.Metrics().Counter("probe_total").Inc()

	t.Run("plain_text", func(t *testing.T) {
		// No Accept header → promhttp serves text/plain; version=0.0.4
		req := httptest.NewRequest(http.MethodGet, "/_/metrics", nil)
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
		}

		ct := w.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
			t.Errorf("unexpected Content-Type %q; want text/plain or openmetrics", ct)
		}

		body := w.Body.String()

		// Parse with expfmt to verify valid Prometheus text format.
		if strings.Contains(ct, "text/plain") {
			parser := expfmt.TextParser{}
			if _, err := parser.TextToMetricFamilies(strings.NewReader(body)); err != nil {
				t.Errorf("expfmt parse error: %v", err)
			}
		}

		// Assert go runtime metrics are present (proves GoCollector is wired).
		if !strings.Contains(body, "go_goroutines") {
			t.Errorf("expected go_goroutines in body; got:\n%s", truncate(body, 500))
		}

		// Assert user-registered metric flows through.
		if !strings.Contains(body, "probe_total") {
			t.Errorf("expected probe_total in body; got:\n%s", truncate(body, 500))
		}
	})

	t.Run("openmetrics_negotiation", func(t *testing.T) {
		// Accept: application/openmetrics-text → real promhttp negotiates OpenMetrics.
		// The old Export-based handler ignores Accept and always returns text/plain.
		// Only the real promhttp.Handler honours this header.
		req := httptest.NewRequest(http.MethodGet, "/_/metrics", nil)
		req.Header.Set("Accept", "application/openmetrics-text;version=1.0.0;q=0.5")
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200 for openmetrics, got %d; body: %s", w.Code, w.Body.String())
		}

		ct := w.Header().Get("Content-Type")
		// promhttp returns application/openmetrics-text when Accept header requests it.
		if !strings.Contains(ct, "openmetrics") {
			t.Errorf("expected openmetrics Content-Type when Accept requests it; got %q", ct)
		}

		body := w.Body.String()
		if !strings.Contains(body, "go_goroutines") {
			t.Errorf("expected go_goroutines in openmetrics body; got:\n%s", truncate(body, 500))
		}
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
