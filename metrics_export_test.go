package forge

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/xraph/forge/internal/logger"
)

// metricsReceiver records OTLP metric posts on /v1/metrics and answers every
// other path (traces) with 200 so tracing export stays quiet.
// ExportMetricsServiceRequest decodes as MetricsData: same field 1.
type metricsReceiver struct {
	*httptest.Server

	mu      sync.Mutex
	got     []*metricspb.MetricsData
	headers []http.Header
}

func newMetricsReceiver(t *testing.T) *metricsReceiver {
	t.Helper()

	r := &metricsReceiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)

		if req.URL.Path == "/v1/metrics" {
			var md metricspb.MetricsData
			if err := proto.Unmarshal(body, &md); err == nil {
				r.mu.Lock()
				r.got = append(r.got, &md)
				r.headers = append(r.headers, req.Header.Clone())
				r.mu.Unlock()
			}
		}

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.Close)

	return r
}

func clearOTELEnv(t *testing.T) {
	t.Helper()

	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_SDK_DISABLED",
		"OTEL_METRICS_EXPORTER", "OTEL_METRIC_EXPORT_INTERVAL", "OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS", "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE",
	} {
		t.Setenv(k, "")
	}
}

func TestAppPushesHTTPMetricsOnStop(t *testing.T) {
	clearOTELEnv(t)

	recv := newMetricsReceiver(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", recv.URL)

	a := New(WithAppName("pushed"))
	_ = a.Router().GET("/users/:id", func(c Context) error { return c.String(http.StatusOK, "ok") })

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	serve(t, a, http.MethodGet, "/users/7", "")

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	recv.mu.Lock()
	defer recv.mu.Unlock()

	var routed bool

	var serviceName string

	for _, md := range recv.got {
		for _, rm := range md.GetResourceMetrics() {
			for _, kv := range rm.GetResource().GetAttributes() {
				if kv.GetKey() == "service.name" {
					serviceName = kv.GetValue().GetStringValue()
				}
			}

			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					if m.GetName() != "http.server.request.duration" || m.GetUnit() != "s" {
						continue
					}

					for _, dp := range m.GetHistogram().GetDataPoints() {
						for _, kv := range dp.GetAttributes() {
							if kv.GetKey() == "http.route" && kv.GetValue().GetStringValue() == "/users/:id" {
								routed = true
							}
						}
					}
				}
			}
		}
	}

	if !routed {
		t.Fatalf("no http.server.request.duration point with http.route=/users/:id in %d pushes", len(recv.got))
	}

	if serviceName != "pushed" {
		t.Fatalf("service.name = %q, want the app name, as on traces", serviceName)
	}
}

func TestAppDoesNotPushWithoutEndpoint(t *testing.T) {
	clearOTELEnv(t)

	a := New(WithAppName("quiet"))
	if a.(*app).metricsPush != nil {
		t.Fatal("no endpoint is configured, so nothing should push")
	}
}

func TestAppMetricsExportFromConfigFile(t *testing.T) {
	clearOTELEnv(t)

	recv := newMetricsReceiver(t)

	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("metrics_export", map[string]any{
		"endpoint":    recv.URL + "/v1/metrics",
		"headers":     map[string]any{"X-Api-Key": "k"},
		"interval":    "30s",
		"temporality": "delta",
	})

	a := New(WithAppName("from-file"), WithAppConfigManager(cm))

	push := a.(*app).metricsPush
	if push == nil {
		t.Fatal("the file sets an endpoint, so push should run")
	}

	if s := push.Settings(); s.Interval != 30*time.Second || s.Temporality != "delta" {
		t.Fatalf("settings = %+v, want 30s and delta from the file", s)
	}

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	recv.mu.Lock()
	defer recv.mu.Unlock()

	if len(recv.headers) == 0 || recv.headers[0].Get("X-Api-Key") != "k" {
		t.Fatalf("headers = %v, want X-Api-Key from the file", recv.headers)
	}

	// The setting has to reach the wire too, not only the pusher's copy of it.
	var sums, hists int

	for _, md := range recv.got {
		for _, rm := range md.GetResourceMetrics() {
			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					var temporality metricspb.AggregationTemporality

					switch {
					case m.GetSum() != nil:
						sums++
						temporality = m.GetSum().GetAggregationTemporality()
					case m.GetHistogram() != nil:
						hists++
						temporality = m.GetHistogram().GetAggregationTemporality()
					default:
						continue
					}

					if temporality != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
						t.Errorf("%s has temporality %v on the wire, want DELTA", m.GetName(), temporality)
					}
				}
			}
		}
	}

	if sums+hists == 0 {
		t.Fatal("no Sum or Histogram reached the receiver, so the temporality went unchecked")
	}
}

func TestAppBadMetricsEndpointStillBoots(t *testing.T) {
	clearOTELEnv(t)

	log, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
	}

	a := New(WithAppName("bad-metrics"), WithAppLogger(log),
		WithAppMetricsExportConfig(MetricsExportConfig{Endpoint: "localhost:4318"}))

	if a.(*app).metricsPush != nil {
		t.Fatal("an invalid endpoint must not build a pusher")
	}

	errs := 0

	for _, e := range log.GetLogsByLevel("ERROR") {
		if e.Message == "telemetry: metrics push disabled" {
			errs++
		}
	}

	if errs != 1 {
		t.Fatalf("logged %d push-disabled errors, want 1", errs)
	}
}
