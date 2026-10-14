package exporters

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func histSample(unit string, ex []Exemplar) Sample {
	return Sample{
		Kind:  SampleHistogram,
		Count: 2,
		Sum:   0.3,
		Unit:  unit,
		Buckets: []Bucket{
			{UpperBound: 0.1, Count: 1},
			{UpperBound: 0.25, Count: 0},
			{UpperBound: 0.5, Count: 1},
		},
		Exemplars: ex,
	}
}

func scrape(t *testing.T, stream StreamFunc, openMetrics bool) string {
	t.Helper()

	b := NewStreamingPrometheusBridge(stream, PrometheusConfig{Namespace: "forge"})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	if openMetrics {
		req.Header.Set("Accept", "application/openmetrics-text; version=1.0.0")
	}

	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, req)

	return rec.Body.String()
}

func TestSemconvNamesAreUnprefixedWithUnitSuffix(t *testing.T) {
	body := scrape(t, func(yield func(string, Sample) bool) {
		yield("http.server.request.duration{http.route=/users/:id}", histSample("s", nil))
		yield("orders_created_total", Sample{Kind: SampleCounter, Value: 3})
	}, false)

	if !strings.Contains(body, `http_server_request_duration_seconds_bucket{http_route="/users/:id",le="0.1"} 1`) {
		t.Fatalf("semconv histogram missing or prefixed:\n%s", body)
	}

	if !strings.Contains(body, "forge_orders_created_total 3") {
		t.Fatalf("user metric lost its prefix:\n%s", body)
	}

	if strings.Contains(body, "forge_http_server") {
		t.Fatalf("semconv metric was prefixed:\n%s", body)
	}
}

func TestSemconvNamespacesPinned(t *testing.T) {
	want := []string{"http.server.", "http.client.", "rpc.", "messaging.", "db.client."}
	if strings.Join(semconvNamespaces, "|") != strings.Join(want, "|") {
		t.Fatalf("semconvNamespaces = %v", semconvNamespaces)
	}
}

func TestBracedUnitAddsNoSuffix(t *testing.T) {
	body := scrape(t, func(yield func(string, Sample) bool) {
		yield("http.server.active_requests", Sample{Kind: SampleGauge, Value: 4, Unit: "{request}"})
	}, false)

	if !strings.Contains(body, "http_server_active_requests 4") {
		t.Fatalf("body:\n%s", body)
	}
}

func TestExemplarsOnlyInOpenMetrics(t *testing.T) {
	ex := []Exemplar{{Value: 0.4, TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", Timestamp: time.Unix(1, 0)}}
	stream := func(yield func(string, Sample) bool) {
		yield("http.server.request.duration", histSample("s", ex))
	}

	om := scrape(t, stream, true)
	if !strings.Contains(om, `trace_id="4bf92f3577b34da6a3ce929d0e0e4736"`) {
		t.Fatalf("OpenMetrics output has no exemplar:\n%s", om)
	}

	text := scrape(t, stream, false)
	if strings.Contains(text, "trace_id") {
		t.Fatalf("text format carried an exemplar:\n%s", text)
	}

	p := expfmt.NewTextParser(model.LegacyValidation)
	if _, err := p.TextToMetricFamilies(strings.NewReader(text)); err != nil {
		t.Fatalf("text output does not parse: %v", err)
	}
}
