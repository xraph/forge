package exporters

import (
	"strings"
	"testing"

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
