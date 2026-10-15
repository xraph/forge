package collectors_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics"
	"github.com/xraph/forge/internal/metrics/collectors"
	"github.com/xraph/forge/internal/shared"
)

func routedHandler(pattern string, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := shared.RouteMatchFromContext(r.Context()); m != nil {
			m.Pattern = pattern
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte("hello"))
	})
}

func newCollector(t *testing.T) shared.Metrics {
	t.Helper()

	return metrics.New(metrics.DefaultCollectorConfig(), logger.NewNoopLogger())
}

func scrape(t *testing.T, m shared.Metrics, openMetrics bool) string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if openMetrics {
		req.Header.Set("Accept", "application/openmetrics-text; version=1.0.0")
	}

	rec := httptest.NewRecorder()
	m.(shared.PrometheusProvider).PrometheusHandler().ServeHTTP(rec, req)

	return rec.Body.String()
}

func family(t *testing.T, m shared.Metrics, name string) *dto.MetricFamily {
	t.Helper()

	// A zero-value TextParser rejects every name since prometheus/common
	// v0.65, so pick the classic validation scheme explicitly.
	p := expfmt.NewTextParser(model.LegacyValidation)

	fams, err := p.TextToMetricFamilies(strings.NewReader(scrape(t, m, false)))
	if err != nil {
		t.Fatalf("scrape does not parse: %v", err)
	}

	return fams[name]
}

// series returns "method route status error_type" -> observation count.
func series(t *testing.T, m shared.Metrics) map[string]uint64 {
	t.Helper()

	out := map[string]uint64{}

	f := family(t, m, "http_server_request_duration_seconds")
	if f == nil {
		return out
	}

	for _, mt := range f.GetMetric() {
		l := map[string]string{}
		for _, p := range mt.GetLabel() {
			l[p.GetName()] = p.GetValue()
		}

		key := l["http_request_method"] + " " + l["http_route"] + " " + l["http_response_status_code"] + " " + l["error_type"]
		out[key] += mt.GetHistogram().GetSampleCount()
	}

	return out
}

// activeRequests sums the http_server_active_requests gauge across label sets.
func activeRequests(t *testing.T, m shared.Metrics) float64 {
	t.Helper()

	f := family(t, m, "http_server_active_requests")
	if f == nil {
		return 0
	}

	var sum float64
	for _, mt := range f.GetMetric() {
		sum += mt.GetGauge().GetValue()
	}

	return sum
}

func serve(t *testing.T, mw func(http.Handler) http.Handler, h http.Handler, method, path string) {
	t.Helper()

	mw(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), method, path, nil))
}

func TestHTTPServerMetricsLabels(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{ExcludePaths: []string{"/_/"}}).Middleware()

	serve(t, mw, routedHandler("/users/:id", 200), http.MethodGet, "/users/1")
	serve(t, mw, routedHandler("", 404), http.MethodGet, "/nope")
	serve(t, mw, routedHandler("/pay", 503), http.MethodPost, "/pay")

	got := series(t, m)

	for key, want := range map[string]uint64{
		"GET /users/:id 200 ": 1,
		"GET  404 ":           1,
		"POST /pay 503 503":   1,
	} {
		if got[key] != want {
			t.Errorf("series %q = %d, want %d (all %v)", key, got[key], want, got)
		}
	}
}

func TestHTTPServerMetricsExcludedPath(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{ExcludePaths: []string{"/_/"}}).Middleware()

	serve(t, mw, routedHandler("/_/health", 200), http.MethodGet, "/_/health")

	if got := series(t, m); len(got) != 0 {
		t.Fatalf("excluded path was recorded: %v", got)
	}
}

func TestHTTPServerMetricsInventedMethodsCollapse(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{}).Middleware()

	for i := range 1000 {
		serve(t, mw, routedHandler("", 405), "M"+strings.Repeat("X", i%50)+string(rune('A'+i%26)), "/")
	}

	got := series(t, m)
	if len(got) != 1 || got["_OTHER  405 "] != 1000 {
		t.Fatalf("want one _OTHER series with 1000 observations, got %v", got)
	}
}

func TestHTTPServerMetricsExemplarOnlyWhenSampled(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{}).Middleware()

	sampled := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled,
	})
	unsampled := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{3}, SpanID: trace.SpanID{4}})

	for _, sc := range []trace.SpanContext{sampled, unsampled} {
		req := httptest.NewRequestWithContext(trace.ContextWithSpanContext(t.Context(), sc), http.MethodGet, "/x", nil)
		mw(routedHandler("/x", 200)).ServeHTTP(httptest.NewRecorder(), req)
	}

	om := scrape(t, m, true)

	if !strings.Contains(om, trace.TraceID{1}.String()) {
		t.Fatalf("sampled trace has no exemplar:\n%s", om)
	}

	if strings.Contains(om, trace.TraceID{3}.String()) {
		t.Fatalf("unsampled trace got an exemplar:\n%s", om)
	}
}

func TestHTTPServerMetricsBodySizesOptIn(t *testing.T) {
	m := newCollector(t)
	serve(t, collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{}).Middleware(),
		routedHandler("/a", 200), http.MethodGet, "/a")

	if family(t, m, "http_server_response_body_size_bytes") != nil {
		t.Fatal("body sizes recorded although BodySizes is false")
	}

	m2 := newCollector(t)
	serve(t, collectors.NewHTTPServerMetrics(m2, collectors.HTTPServerOptions{BodySizes: true}).Middleware(),
		routedHandler("/a", 200), http.MethodGet, "/a")

	f := family(t, m2, "http_server_response_body_size_bytes")
	if f == nil {
		t.Fatal("body sizes missing although BodySizes is true")
	}

	// routedHandler writes "hello".
	if sum := f.GetMetric()[0].GetHistogram().GetSampleSum(); sum != 5 {
		t.Fatalf("response body size sum = %v, want 5", sum)
	}
}

// withHolder returns a context carrying a RouteMatch an outer request filled.
func withHolder(t *testing.T, pattern string) (context.Context, *shared.RouteMatch) {
	t.Helper()

	ctx, outer := shared.NewRouteMatch(t.Context())
	outer.Pattern = pattern

	return ctx, outer
}

func TestHTTPServerMetricsFreshRouteMatch(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{FreshRouteMatch: true}).Middleware()

	ctx, outer := withHolder(t, "/outer")
	mw(routedHandler("/inner", 200)).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(ctx, http.MethodGet, "/inner", nil))

	if got := series(t, m); got["GET /inner 200 "] != 1 || len(got) != 1 {
		t.Fatalf("want the inner handler's pattern, got %v", got)
	}

	if outer.Pattern != "/outer" {
		t.Fatalf("outer holder was overwritten: %q", outer.Pattern)
	}
}

func TestHTTPServerMetricsSharesRouteMatchByDefault(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{}).Middleware()

	// The tracing middleware runs first and installs the holder; the router
	// fills it. The metrics middleware must read that same holder.
	ctx, outer := withHolder(t, "")
	mw(routedHandler("/shared", 200)).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(ctx, http.MethodGet, "/shared", nil))

	if outer.Pattern != "/shared" {
		t.Fatalf("holder from the outer middleware was not shared: %q", outer.Pattern)
	}

	if got := series(t, m); got["GET /shared 200 "] != 1 {
		t.Fatalf("want the shared pattern recorded, got %v", got)
	}
}

// getHTTP is the label set of a plain GET over http.
var getHTTP = map[string]string{"http_request_method": "GET", "url_scheme": "http"}

func TestHTTPServerMetricsActiveRequests(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{}).Middleware()

	var during float64

	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		during = gaugeValue(t, m, "http_server_active_requests", getHTTP)

		w.WriteHeader(http.StatusNoContent)
	})

	serve(t, mw, h, http.MethodGet, "/a")

	if during != 1 {
		t.Fatalf("active requests during the handler = %v, want 1", during)
	}

	if after := gaugeValue(t, m, "http_server_active_requests", getHTTP); after != 0 {
		t.Fatalf("active requests after the handler = %v, want 0", after)
	}
}

// metric returns the series of name whose labels equal want. A label missing
// from want must be missing or empty on the series.
func metric(t *testing.T, m shared.Metrics, name string, want map[string]string) *dto.Metric {
	t.Helper()

	f := family(t, m, name)
	if f == nil {
		t.Fatalf("%s missing from the scrape", name)
	}

	for _, mt := range f.GetMetric() {
		got := map[string]string{}

		for _, p := range mt.GetLabel() {
			if p.GetValue() != "" {
				got[p.GetName()] = p.GetValue()
			}
		}

		if sameLabels(got, want) {
			return mt
		}
	}

	t.Fatalf("no %s series with labels %v", name, want)

	return nil
}

func sameLabels(got, want map[string]string) bool {
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}

	for k := range got {
		if _, ok := want[k]; !ok {
			return false
		}
	}

	return true
}

func gaugeValue(t *testing.T, m shared.Metrics, name string, labels map[string]string) float64 {
	t.Helper()

	return metric(t, m, name, labels).GetGauge().GetValue()
}

func counterValue(t *testing.T, m shared.Metrics, name string, labels map[string]string) float64 {
	t.Helper()

	return metric(t, m, name, labels).GetCounter().GetValue()
}

func histogramCount(t *testing.T, m shared.Metrics, name string, labels map[string]string) uint64 {
	t.Helper()

	return metric(t, m, name, labels).GetHistogram().GetSampleCount()
}
