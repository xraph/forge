package metrics

import (
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics/exporters"
)

type otlpSource interface {
	OTLPProducer(temporality exporters.Temporality) *exporters.OTLPProducer
}

func producedNames(t *testing.T, runtime bool) map[string]bool {
	t.Helper()

	cfg := DefaultCollectorConfig()
	cfg.Features.RuntimeMetrics = runtime

	m := New(cfg, logger.NewNoopLogger())
	m.Counter("orders_created_total").Inc()

	src, ok := m.(otlpSource)
	if !ok {
		t.Fatal("the collector does not offer an OTLP producer")
	}

	sms, err := src.OTLPProducer(exporters.Cumulative).Produce(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool)

	for _, sm := range sms {
		for _, met := range sm.Metrics {
			names[met.Name] = true

			if met.Name == "forge_orders_created_total" {
				if s, ok := met.Data.(metricdata.Sum[float64]); !ok || s.DataPoints[0].Value != 1 {
					t.Fatalf("orders_created_total = %+v", met.Data)
				}
			}
		}
	}

	return names
}

func TestCollectorOTLPProducerCarriesRegistryAndRuntime(t *testing.T) {
	names := producedNames(t, true)

	if !names["forge_orders_created_total"] || !names["go_goroutines"] {
		t.Fatalf("names = %v, want the registry counter and go_goroutines", names)
	}
}

func TestCollectorOTLPProducerWithoutRuntime(t *testing.T) {
	if names := producedNames(t, false); names["go_goroutines"] {
		t.Fatal("runtime metrics are off, so go_goroutines must not be pushed")
	}
}

func TestNoOpMetricsHasNoOTLPProducer(t *testing.T) {
	if _, ok := NewNoOpMetrics().(otlpSource); ok {
		t.Fatal("the no-op collector must not offer a producer")
	}
}
