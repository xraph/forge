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

	p, err := NewMetricsPusher(t.Context(), MetricsSettings{
		Enabled:  true,
		Endpoint: recv.srv.URL + "/v1/metrics",
		Headers:  map[string]string{"X-Api-Key": "secret"},
		Interval: time.Hour, // only the shutdown flush exports
	}, res, stubProducer{}, logger.NewNoopLogger())
	if err != nil {
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

func TestMetricsPusherRejectsSchemelessEndpoint(t *testing.T) {
	_, err := NewMetricsPusher(t.Context(), MetricsSettings{Enabled: true, Endpoint: "localhost:4318"},
		resource.Empty(), stubProducer{}, logger.NewNoopLogger())
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("err = %v, want ErrInvalidEndpoint", err)
	}
}

func TestMetricsPusherNilShutdown(t *testing.T) {
	var p *MetricsPusher
	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}
