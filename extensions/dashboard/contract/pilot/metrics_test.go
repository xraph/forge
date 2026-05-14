package pilot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubMetrics struct {
	data *collector.MetricsData
}

func (s *stubMetrics) CollectMetrics(_ context.Context) *collector.MetricsData {
	return s.data
}

func TestMetricsSummarySub_EmitsOnTick(t *testing.T) {
	stub := &stubMetrics{data: &collector.MetricsData{
		Stats: collector.MetricsStats{TotalMetrics: 10, Counters: 4, Gauges: 3, Histograms: 3},
	}}
	h := metricsSummarySub(stub, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	ch, stop, err := h(ctx, struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before event")
		}
		var got MetricsSummary
		if err := json.Unmarshal(jsonOf(ev), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.TotalMetrics != 10 {
			t.Errorf("TotalMetrics = %d", got.TotalMetrics)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for tick")
	}
}

// jsonOf is a tiny helper for tests reading typed events out of the typed
// subscription handler (which returns chan MetricsSummary, not StreamEvent).
func jsonOf(v MetricsSummary) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestMetricsSummarySub_StopsOnCancel(t *testing.T) {
	stub := &stubMetrics{data: &collector.MetricsData{}}
	h := metricsSummarySub(stub, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	ch, stop, err := h(ctx, struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()
	// Drain a few events
	go func() {
		for range ch {
		}
	}()
	cancel()
	// Channel should close shortly after cancellation
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed — pass
			}
		case <-deadline:
			t.Fatal("channel did not close after cancel")
		}
	}
}
