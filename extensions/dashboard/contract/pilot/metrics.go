package pilot

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// MetricsProvider is the slice of DataCollector the metrics.summary handler needs.
type MetricsProvider interface {
	CollectMetrics(ctx context.Context) *collector.MetricsData
}

// metricsSummarySub returns a typed subscription handler that emits a
// MetricsSummary every interval until ctx is cancelled. The interval is
// injectable so tests can use millisecond ticks instead of 5 seconds.
func metricsSummarySub(p MetricsProvider, interval time.Duration) func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan MetricsSummary, func(), error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan MetricsSummary, func(), error) {
		out := make(chan MetricsSummary, 4)
		ticker := time.NewTicker(interval)
		go func() {
			defer close(out)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					data := p.CollectMetrics(ctx)
					if data == nil {
						continue
					}
					ev := MetricsSummary{
						TotalMetrics: data.Stats.TotalMetrics,
						Counters:     data.Stats.Counters,
						Gauges:       data.Stats.Gauges,
						Histograms:   data.Stats.Histograms,
						TS:           t.Unix(),
					}
					select {
					case out <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return out, func() {}, nil
	}
}
