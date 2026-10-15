package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/xraph/forge/internal/logger"
)

// otlpReceiver records OTLP/HTTP metric posts. ExportMetricsServiceRequest
// has the wire shape of MetricsData (field 1, repeated ResourceMetrics), so
// the test decodes into MetricsData and keeps the collector package, and the
// gRPC it imports, out of the build.
type otlpReceiver struct {
	srv     *httptest.Server
	mu      sync.Mutex
	got     []*metricspb.MetricsData
	headers []http.Header
}

func newOTLPReceiver(t *testing.T) *otlpReceiver {
	t.Helper()

	r := &otlpReceiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)

		var md metricspb.MetricsData
		if err := proto.Unmarshal(body, &md); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		r.mu.Lock()
		r.got = append(r.got, &md)
		r.headers = append(r.headers, req.Header.Clone())
		r.mu.Unlock()

		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.srv.Close)

	return r
}

func (r *otlpReceiver) names() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]bool)

	for _, md := range r.got {
		for _, rm := range md.GetResourceMetrics() {
			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					out[m.GetName()] = true
				}
			}
		}
	}

	return out
}

func (r *otlpReceiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.got)
}

type stubProducer struct{}

func (stubProducer) Produce(context.Context) ([]metricdata.ScopeMetrics, error) {
	return []metricdata.ScopeMetrics{{
		Scope: instrumentation.Scope{Name: "test"},
		Metrics: []metricdata.Metrics{{
			Name: "pushed",
			Data: metricdata.Gauge[float64]{DataPoints: []metricdata.DataPoint[float64]{{Value: 1, Time: time.Now()}}},
		}},
	}}, nil
}

func TestMetricsPusherFlushesOnShutdown(t *testing.T) {
	recv := newOTLPReceiver(t)
	res := resource.NewSchemaless(attribute.String("service.name", "pusher-test"))

	p, err := NewMetricsPusher(MetricsSettings{
		Enabled:  true,
		Endpoint: recv.srv.URL + "/v1/metrics",
		Headers:  map[string]string{"X-Api-Key": "secret"},
		Interval: time.Hour, // only the shutdown flush exports
	}, res, stubProducer{}, logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if !recv.names()["pushed"] {
		t.Fatal("shutdown did not export the final collect")
	}

	recv.mu.Lock()
	defer recv.mu.Unlock()

	if recv.headers[0].Get("X-Api-Key") != "secret" {
		t.Fatalf("headers = %v, want X-Api-Key", recv.headers[0])
	}

	var serviceName string

	for _, kv := range recv.got[0].GetResourceMetrics()[0].GetResource().GetAttributes() {
		if kv.GetKey() == "service.name" {
			serviceName = kv.GetValue().GetStringValue()
		}
	}

	if serviceName != "pusher-test" {
		t.Fatalf("service.name = %q, want the shared resource's", serviceName)
	}
}

// newHourlyPusher builds a pusher whose interval never fires in a test, so
// every post the receiver sees comes from a Shutdown.
func newHourlyPusher(t *testing.T, recv *otlpReceiver) *MetricsPusher {
	t.Helper()

	p, err := NewMetricsPusher(MetricsSettings{
		Enabled:  true,
		Endpoint: recv.srv.URL + "/v1/metrics",
		Interval: time.Hour,
	}, resource.Empty(), stubProducer{}, logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestMetricsPusherNeverStartedExportsNothing(t *testing.T) {
	recv := newOTLPReceiver(t)
	p := newHourlyPusher(t, recv)

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if n := recv.count(); n != 0 {
		t.Fatalf("a pusher that never started posted %d times, want 0", n)
	}
}

func TestMetricsPusherNeverStartedDoesNotTick(t *testing.T) {
	recv := newOTLPReceiver(t)

	_, err := NewMetricsPusher(MetricsSettings{
		Enabled:  true,
		Endpoint: recv.srv.URL + "/v1/metrics",
		Interval: 10 * time.Millisecond,
	}, resource.Empty(), stubProducer{}, logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	if n := recv.count(); n != 0 {
		t.Fatalf("a pusher that never started posted %d times, want 0", n)
	}
}

func TestMetricsPusherRestarts(t *testing.T) {
	recv := newOTLPReceiver(t)
	p := newHourlyPusher(t, recv)

	for round := 1; round <= 2; round++ {
		if err := p.Start(t.Context()); err != nil {
			t.Fatalf("start %d: %v", round, err)
		}

		if err := p.Shutdown(t.Context()); err != nil {
			t.Fatalf("shutdown %d: %v", round, err)
		}

		if n := recv.count(); n != round {
			t.Fatalf("after shutdown %d the receiver has %d posts, want %d", round, n, round)
		}
	}
}

func TestMetricsPusherShutdownTwice(t *testing.T) {
	recv := newOTLPReceiver(t)
	p := newHourlyPusher(t, recv)

	if err := p.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	for i := range 2 {
		if err := p.Shutdown(t.Context()); err != nil {
			t.Fatalf("shutdown %d: %v", i+1, err)
		}
	}

	if n := recv.count(); n != 1 {
		t.Fatalf("receiver has %d posts, want 1: the second Shutdown has nothing to flush", n)
	}
}

func TestMetricsPusherStartTwiceKeepsOneReader(t *testing.T) {
	recv := newOTLPReceiver(t)
	p := newHourlyPusher(t, recv)

	for i := range 2 {
		if err := p.Start(t.Context()); err != nil {
			t.Fatalf("start %d: %v", i+1, err)
		}
	}

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if n := recv.count(); n != 1 {
		t.Fatalf("receiver has %d posts, want 1: a second Start while running must not add a reader", n)
	}
}

func TestMetricsPusherRejectsSchemelessEndpoint(t *testing.T) {
	_, err := NewMetricsPusher(MetricsSettings{Enabled: true, Endpoint: "localhost:4318"},
		resource.Empty(), stubProducer{}, logger.NewNoopLogger())
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("err = %v, want ErrInvalidEndpoint", err)
	}
}

func TestMetricsPusherNil(t *testing.T) {
	var p *MetricsPusher
	if err := p.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if s := p.Settings(); s.Enabled || s.Endpoint != "" {
		t.Fatalf("settings = %+v, want the zero value", s)
	}
}
