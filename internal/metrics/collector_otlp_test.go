package metrics

import (
	"runtime"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics/exporters"
)

type otlpSource interface {
	OTLPProducer(temporality exporters.Temporality) *exporters.OTLPProducer
}

// produceWith builds a collector from cfg, bumps the orders counter and returns
// one cumulative push, grouped by metric name.
func produceWith(t *testing.T, cfg *CollectorConfig) map[string]metricdata.Metrics {
	t.Helper()

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

	got := make(map[string]metricdata.Metrics)

	for _, sm := range sms {
		for _, met := range sm.Metrics {
			if _, dup := got[met.Name]; dup {
				t.Fatalf("%s was pushed twice", met.Name)
			}

			got[met.Name] = met
		}
	}

	return got
}

// requireOrdersCounter checks the orders counter arrived as one point of 1.
func requireOrdersCounter(t *testing.T, got map[string]metricdata.Metrics, name string) {
	t.Helper()

	met, ok := got[name]
	if !ok {
		t.Fatalf("%s missing from %d metrics", name, len(got))
	}

	s, ok := met.Data.(metricdata.Sum[float64])
	if !ok || len(s.DataPoints) == 0 || s.DataPoints[0].Value != 1 {
		t.Fatalf("%s = %+v, want a sum with a point of 1", name, met.Data)
	}
}

func runtimeConfig(on bool) *CollectorConfig {
	cfg := DefaultCollectorConfig()
	cfg.Features.RuntimeMetrics = on

	return cfg
}

func TestCollectorOTLPProducerCarriesRegistryAndRuntime(t *testing.T) {
	got := produceWith(t, runtimeConfig(true))

	requireOrdersCounter(t, got, "forge_orders_created_total")

	// client_golang's process collector reads /proc on Linux and the OS APIs on
	// macOS; this was checked on macOS and Linux only, so other platforms skip it.
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if _, ok := got["process_cpu_seconds_total"]; !ok {
			t.Fatalf("process_cpu_seconds_total missing on %s; names = %v", runtime.GOOS, keys(got))
		}
	}

	g, ok := got["go_goroutines"].Data.(metricdata.Gauge[float64])
	if !ok || len(g.DataPoints) != 1 {
		t.Fatalf("go_goroutines = %+v, want one gauge point, once", got["go_goroutines"])
	}
}

func TestCollectorOTLPProducerWithoutRuntime(t *testing.T) {
	got := produceWith(t, runtimeConfig(false))

	if _, ok := got["go_goroutines"]; ok {
		t.Fatal("runtime metrics are off, so go_goroutines must not be pushed")
	}

	if _, ok := got["process_cpu_seconds_total"]; ok {
		t.Fatal("runtime metrics are off, so process_ series must not be pushed")
	}
}

func TestCollectorOTLPProducerUsesConfiguredNamespace(t *testing.T) {
	cfg := runtimeConfig(false)
	cfg.Collection.Namespace = "shop"

	got := produceWith(t, cfg)

	requireOrdersCounter(t, got, "shop_orders_created_total")

	if _, ok := got["forge_orders_created_total"]; ok {
		t.Fatal("the default namespace leaked into a collector configured with shop")
	}
}

func keys(m map[string]metricdata.Metrics) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}

func TestNoOpMetricsHasNoOTLPProducer(t *testing.T) {
	if _, ok := NewNoOpMetrics().(otlpSource); ok {
		t.Fatal("the no-op collector must not offer a producer")
	}
}
