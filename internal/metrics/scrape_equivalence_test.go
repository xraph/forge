package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/internal/metrics/exporters"
	"github.com/xraph/go-utils/metrics"
)

// The streaming scrape path replaced one that materialized every metric as
// nested maps. It has to expose exactly the same thing: same families, same
// types, same values. Comparing the two bridges' output is the cheapest way to
// say so, and it keeps saying so as the exposition code changes.
func TestScrape_StreamingMatchesSnapshot(t *testing.T) {
	c := New(nil, nil).(*collector)

	c.Counter("orders_total").Add(7)
	c.Counter("orders_total", metrics.WithLabels(map[string]string{"region": "eu"})).Add(3)
	c.Gauge("queue_depth").Set(42)
	c.Gauge("pool_size", metrics.WithLabels(map[string]string{"pool": "db"})).Set(8)

	hist := c.Histogram("request_bytes")
	for _, v := range []float64{1, 5, 25, 250, 2500} {
		hist.Observe(v)
	}

	timer := c.Timer("handler_seconds")
	for i := range 20 {
		timer.Record(time.Duration(i) * time.Millisecond)
	}

	cfg := exporters.PrometheusConfig{Namespace: "forge"}

	snapshot, err := exporters.NewPrometheusBridge(c.GetMetrics, cfg).GatherText()
	if err != nil {
		t.Fatalf("snapshot GatherText: %v", err)
	}

	streaming, err := exporters.NewStreamingPrometheusBridge(c.streamMetrics, cfg).GatherText()
	if err != nil {
		t.Fatalf("streaming GatherText: %v", err)
	}

	if string(snapshot) != string(streaming) {
		t.Errorf("streaming exposition differs from snapshot\n--- snapshot ---\n%s\n--- streaming ---\n%s",
			snapshot, streaming)
	}

	// Guard against both paths being equally empty.
	for _, want := range []string{
		"forge_orders_total",
		"forge_queue_depth",
		"forge_request_bytes_bucket",
		"forge_handler_seconds",
	} {
		if !strings.Contains(string(streaming), want) {
			t.Errorf("exposition missing %s", want)
		}
	}
}

// A descriptor is immutable, so the bridge caches it rather than rebuilding one
// per metric per scrape. Repeated scrapes must stay identical.
func TestScrape_StableAcrossScrapes(t *testing.T) {
	c := New(nil, nil).(*collector)
	c.Counter("stable_total").Inc()
	c.Gauge("stable_gauge", metrics.WithLabels(map[string]string{"a": "1"})).Set(2)

	bridge := exporters.NewStreamingPrometheusBridge(c.streamMetrics,
		exporters.PrometheusConfig{Namespace: "forge"})

	first, err := bridge.GatherText()
	if err != nil {
		t.Fatalf("first GatherText: %v", err)
	}

	second, err := bridge.GatherText()
	if err != nil {
		t.Fatalf("second GatherText: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("scrape not stable\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// Timer used to delegate to the embedded implementation rather than the
// registry, so timers never reached the scrape at all. Counter, Gauge and
// Histogram always did.
func TestCollector_TimerReachesTheScrape(t *testing.T) {
	c := New(nil, nil).(*collector)

	timer := c.Timer("work_seconds")
	for i := range 10 {
		timer.Record(time.Duration(i+1) * time.Millisecond)
	}

	if _, ok := c.GetMetrics()["work_seconds"]; !ok {
		t.Error("timer missing from GetMetrics")
	}

	var streamed bool

	c.registry.Stream(func(key string, sample exporters.Sample) bool {
		if key == "work_seconds" && sample.Kind == exporters.SampleTimer {
			streamed = true
		}

		return true
	})

	if !streamed {
		t.Error("timer missing from the metric stream")
	}

	out, err := exporters.NewStreamingPrometheusBridge(c.streamMetrics,
		exporters.PrometheusConfig{Namespace: "forge"}).GatherText()
	if err != nil {
		t.Fatalf("GatherText: %v", err)
	}

	if !strings.Contains(string(out), "forge_work_seconds") {
		t.Errorf("timer missing from exposition:\n%s", out)
	}
}
