package exporters

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBridge_CounterAndGauge(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			`http_requests_total{method="GET"}`: map[string]any{"value": float64(5), "_type": "counter"},
			"queue_depth":                       float64(3),
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{Namespace: ""})

	expected := `
# HELP http_requests_total Forge counter http_requests_total
# TYPE http_requests_total counter
http_requests_total{method="GET"} 5
# HELP queue_depth Forge gauge queue_depth
# TYPE queue_depth gauge
queue_depth 3
`
	if err := testutil.CollectAndCompare(b.collector, strings.NewReader(expected),
		"http_requests_total", "queue_depth"); err != nil {
		t.Fatalf("unexpected exposition: %v", err)
	}
}

func TestBridge_GatherTextHasNoTimestamps(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{"queue_depth": float64(3)}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{})
	out, err := b.GatherText()
	if err != nil {
		t.Fatalf("GatherText: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "queue_depth") {
			// "queue_depth 3" -> exactly 2 fields; a trailing timestamp would make 3.
			if fields := strings.Fields(line); len(fields) != 2 {
				t.Fatalf("expected no timestamp, got %q", line)
			}
		}
	}
}

func TestBridge_HistogramMissingCountSkipped(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			"some_hist": map[string]any{
				// "count" key is intentionally absent
				"sum": float64(7.5),
				"buckets": map[float64]uint64{
					0.1: 1,
					0.5: 2,
				},
			},
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{Namespace: ""})

	if n := testutil.CollectAndCount(b.collector); n != 0 {
		t.Fatalf("expected 0 metrics when count is missing, got %d", n)
	}
}

func TestBridge_Histogram(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			"request_latency_seconds": map[string]any{
				"count": uint64(6),
				"sum":   float64(7.5),
				"buckets": map[float64]uint64{ // per-bucket (non-cumulative)
					0.1: 1,
					0.5: 2,
					1.0: 3,
				},
			},
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{})

	expected := `
# HELP request_latency_seconds Forge histogram request_latency_seconds
# TYPE request_latency_seconds histogram
request_latency_seconds_bucket{le="0.1"} 1
request_latency_seconds_bucket{le="0.5"} 3
request_latency_seconds_bucket{le="1"} 6
request_latency_seconds_bucket{le="+Inf"} 6
request_latency_seconds_sum 7.5
request_latency_seconds_count 6
`
	if err := testutil.CollectAndCompare(b.collector, strings.NewReader(expected),
		"request_latency_seconds"); err != nil {
		t.Fatalf("unexpected histogram exposition: %v", err)
	}
}

func TestBridge_Timer(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			"op_duration_seconds": map[string]any{
				"count": uint64(10),
				"mean":  200 * time.Millisecond,
				"p50":   150 * time.Millisecond,
				"p95":   400 * time.Millisecond,
				"p99":   900 * time.Millisecond,
			},
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{})

	// sum = mean.Seconds() * count = 0.2 * 10 = 2
	expected := `
# HELP op_duration_seconds Forge summary op_duration_seconds
# TYPE op_duration_seconds summary
op_duration_seconds{quantile="0.5"} 0.15
op_duration_seconds{quantile="0.95"} 0.4
op_duration_seconds{quantile="0.99"} 0.9
op_duration_seconds_sum 2
op_duration_seconds_count 10
`
	if err := testutil.CollectAndCompare(b.collector, strings.NewReader(expected),
		"op_duration_seconds"); err != nil {
		t.Fatalf("unexpected timer exposition: %v", err)
	}
}
