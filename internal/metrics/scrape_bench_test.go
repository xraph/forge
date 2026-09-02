package metrics

import (
	"fmt"
	"testing"

	"github.com/xraph/forge/internal/metrics/exporters"
)

// The scrape path's cost is proportional to how many metrics the installed
// extensions have registered between them, so these benchmarks scale the metric
// count rather than the scrape count.
//
// GetMetrics builds the whole set as nested maps: a map per counter, histogram
// and timer, and a boxed float64 per gauge. Stream hands the exporter one
// Sample at a time and allocates only what a histogram's buckets require.
func seedMetrics(c *collector, groups int) {
	for i := range groups {
		c.Counter(fmt.Sprintf("ext_%d_requests_total", i)).Inc()
		c.Gauge(fmt.Sprintf("ext_%d_connections", i)).Set(float64(i))
		c.Histogram(fmt.Sprintf("ext_%d_latency", i)).Observe(0.1)
		c.Timer(fmt.Sprintf("ext_%d_duration", i)).Record(1)
	}
}

// benchScrape measures a whole scrape: reading every metric and encoding it in
// the Prometheus text format. Comparing the read paths alone would be
// misleading, because the two of them divide the work differently between the
// reader and the exporter.
func benchScrape(b *testing.B, groups int, streaming bool) {
	c := New(nil, nil).(*collector)
	seedMetrics(c, groups)

	cfg := exporters.PrometheusConfig{Namespace: "forge"}

	var bridge *exporters.PrometheusBridge
	if streaming {
		bridge = exporters.NewStreamingPrometheusBridge(c.streamMetrics, cfg)
	} else {
		bridge = exporters.NewPrometheusBridge(c.GetMetrics, cfg)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := bridge.GatherText(); err != nil {
			b.Fatalf("GatherText: %v", err)
		}
	}
}

func BenchmarkScrape_Snapshot_50(b *testing.B)   { benchScrape(b, 50, false) }
func BenchmarkScrape_Streaming_50(b *testing.B)  { benchScrape(b, 50, true) }
func BenchmarkScrape_Snapshot_200(b *testing.B)  { benchScrape(b, 200, false) }
func BenchmarkScrape_Streaming_200(b *testing.B) { benchScrape(b, 200, true) }
