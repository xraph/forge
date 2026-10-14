package forge

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/xraph/forge/internal/shared"
)

func scrapeApp(t *testing.T, a App, openMetrics bool) string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_/metrics", nil)
	if openMetrics {
		req.Header.Set("Accept", "application/openmetrics-text; version=1.0.0")
	}

	rec := httptest.NewRecorder()
	a.Metrics().(shared.PrometheusProvider).PrometheusHandler().ServeHTTP(rec, req)

	return rec.Body.String()
}

func TestScrapeHasSemconvHTTPSeries(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("scrape"))
	_ = a.Router().GET("/users/:id", func(c Context) error { return c.String(http.StatusOK, "ok") })
	_ = a.Router().GET("/fail", func(Context) error { return InternalError(errors.New("x")) })
	_ = a.Router().GET("/_/probe", func(c Context) error { return c.String(http.StatusOK, "ok") })

	for _, p := range []string{"/users/1", "/users/2", "/fail", "/missing", "/_/probe"} {
		a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))
	}

	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), "BREW", "/users/1", nil))

	text := scrapeApp(t, a, false)

	p := expfmt.NewTextParser(model.LegacyValidation)

	families, err := p.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatalf("scrape does not parse: %v", err)
	}

	dur, ok := families["http_server_request_duration_seconds"]
	if !ok {
		t.Fatalf("no http_server_request_duration_seconds family:\n%s", text)
	}

	counts := map[string]uint64{}

	for _, m := range dur.GetMetric() {
		labels := map[string]string{}
		for _, l := range m.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}

		key := labels["http_request_method"] + " " + labels["http_route"] + " " + labels["http_response_status_code"] + " " + labels["error_type"]
		counts[key] += m.GetHistogram().GetSampleCount()
	}

	want := map[string]uint64{
		"GET /users/:id 200 ": 2,
		"GET /fail 500 500":   1,
		"GET  404 ":           1,
	}

	for k, n := range want {
		if counts[k] != n {
			t.Errorf("series %q count = %d, want %d (all: %v)", k, counts[k], n, counts)
		}
	}

	// BREW reaches the router as an unknown method. Whether the adapter
	// answers 404 or 405, and whether a route matched, the method label must
	// be _OTHER and there must be exactly one such series.
	other := 0

	for k, n := range counts {
		if strings.HasPrefix(k, "_OTHER ") {
			other += int(n)
		}

		if strings.HasPrefix(k, "BREW") {
			t.Errorf("raw method leaked into a label: %q", k)
		}
	}

	if other != 1 {
		t.Errorf("_OTHER observations = %d, want 1 (all: %v)", other, counts)
	}

	for k := range counts {
		if strings.Contains(k, "/_/probe") {
			t.Errorf("excluded /_/ path was recorded: %q", k)
		}
	}

	for name := range families {
		if strings.HasPrefix(name, "forge_http") {
			t.Errorf("old series still exported: %s", name)
		}
	}
}

func TestScrapeExemplarForSampledRequest(t *testing.T) {
	a := New(WithAppName("ex"))
	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })
	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))

	if !strings.Contains(scrapeApp(t, a, true), "trace_id=") {
		t.Fatal("OpenMetrics scrape has no exemplar although development samples every request")
	}

	zero := 0.0
	b := New(WithAppName("ex0"), WithAppTracingConfig(TracingConfig{SampleRatio: &zero}))
	_ = b.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })
	b.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))

	if strings.Contains(scrapeApp(t, b, true), "trace_id=") {
		t.Fatal("exemplar present for an unsampled request")
	}
}

func TestScrapeRacesRequests(t *testing.T) {
	a := New(WithAppName("race"))
	_ = a.Router().GET("/r/:n", func(c Context) error { return c.String(http.StatusOK, "ok") })

	var wg sync.WaitGroup

	for i := range 200 {
		wg.Go(func() {
			a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/r/%d", i), nil))

			if i%10 == 0 {
				p := expfmt.NewTextParser(model.LegacyValidation)
				if _, err := p.TextToMetricFamilies(strings.NewReader(scrapeApp(t, a, false))); err != nil {
					t.Errorf("scrape during traffic failed: %v", err)
				}
			}
		})
	}

	wg.Wait()
}

// histogramLabels returns the label sets of every series in a histogram
// family, with the sample count of each.
func histogramLabels(t *testing.T, a App, family string) []map[string]string {
	t.Helper()

	p := expfmt.NewTextParser(model.LegacyValidation)

	families, err := p.TextToMetricFamilies(strings.NewReader(scrapeApp(t, a, false)))
	if err != nil {
		t.Fatalf("scrape does not parse: %v", err)
	}

	f, ok := families[family]
	if !ok {
		t.Fatalf("no %s family in the scrape", family)
	}

	var out []map[string]string

	for _, m := range f.GetMetric() {
		labels := map[string]string{"_count": strconv.FormatUint(m.GetHistogram().GetSampleCount(), 10)}
		for _, l := range m.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}

		out = append(out, labels)
	}

	return out
}

// The route on the metric and on the span come from the same match, so a
// dashboard can join them.
func TestScrapeRouteMatchesSpanRoute(t *testing.T) {
	a, exp := newTracedApp(t, TracingConfig{})
	_ = a.Router().GET("/users/:id", func(c Context) error { return c.String(http.StatusOK, "ok") })

	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/7", nil))

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}

	spanRoute := ""

	for _, kv := range spans[0].Attributes {
		if kv.Key == "http.route" {
			spanRoute = kv.Value.AsString()
		}
	}

	series := histogramLabels(t, a, "http_server_request_duration_seconds")
	if len(series) != 1 {
		t.Fatalf("duration series = %v, want one", series)
	}

	if spanRoute == "" || series[0]["http_route"] != spanRoute {
		t.Fatalf("metric http_route = %q, span http.route = %q, want equal and non-empty", series[0]["http_route"], spanRoute)
	}
}

// A global middleware that rejects a request without calling next, the way
// an auth extension does, still sits inside the metrics middleware.
func TestScrapeCountsRequestRejectedByGlobalMiddleware(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("rejected"))
	a.Router().UseGlobal(func(Handler) Handler {
		return func(c Context) error { return c.String(http.StatusUnauthorized, "no") }
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

	t.Fatalf("no duration series with status 401 and count 1: %v", histogramLabels(t, a, "http_server_request_duration_seconds"))
}
