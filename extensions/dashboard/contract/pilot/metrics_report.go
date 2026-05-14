package pilot

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// MetricsReportProvider is the slice of DataCollector metrics_report.go reads.
type MetricsReportProvider interface {
	CollectMetricsReport(ctx context.Context) *collector.MetricsReport
}

func metricsReportHandler(p MetricsReportProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (MetricsReportResponse, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (MetricsReportResponse, error) {
		if p == nil {
			return MetricsReportResponse{}, &contract.Error{Code: contract.CodeUnavailable, Message: "metrics report provider not configured"}
		}
		report := p.CollectMetricsReport(ctx)
		if report == nil {
			return MetricsReportResponse{}, nil
		}
		collectors := make([]CollectorEntry, 0, len(report.Collectors))
		for _, c := range report.Collectors {
			collectors = append(collectors, CollectorEntry{
				Name:           c.Name,
				Type:           c.Type,
				MetricsCount:   c.MetricsCount,
				Status:         c.Status,
				LastCollection: formatTime(c.LastCollection),
			})
		}
		topMetrics := make([]MetricEntryDTO, 0, len(report.TopMetrics))
		for _, m := range report.TopMetrics {
			topMetrics = append(topMetrics, MetricEntryDTO{
				Name:  m.Name,
				Type:  m.Type,
				Value: m.Value,
			})
		}
		return MetricsReportResponse{
			TotalMetrics:  report.TotalMetrics,
			MetricsByType: report.MetricsByType,
			Collectors:    collectors,
			TopMetrics:    topMetrics,
		}, nil
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
