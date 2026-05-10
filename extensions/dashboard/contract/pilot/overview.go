package pilot

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// OverviewProvider is the slice of DataCollector overview.go reads. Splitting
// the surface keeps tests fixture-friendly without dragging in the full
// DataCollector wiring.
type OverviewProvider interface {
	CollectOverview(ctx context.Context) *collector.OverviewData
}

func overviewHandler(p OverviewProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (OverviewResponse, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (OverviewResponse, error) {
		if p == nil {
			return OverviewResponse{}, &contract.Error{Code: contract.CodeUnavailable, Message: "overview provider not configured"}
		}
		data := p.CollectOverview(ctx)
		if data == nil {
			return OverviewResponse{}, nil
		}
		return OverviewResponse{
			OverallHealth:   data.OverallHealth,
			TotalServices:   data.TotalServices,
			HealthyServices: data.HealthyServices,
			TotalMetrics:    data.TotalMetrics,
			UptimeSeconds:   int64(data.Uptime.Seconds()),
			Version:         data.Version,
			Environment:     data.Environment,
			Summary:         data.Summary,
		}, nil
	}
}
