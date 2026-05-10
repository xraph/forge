package pilot

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// HealthProvider is the slice of DataCollector health.go reads.
type HealthProvider interface {
	CollectHealth(ctx context.Context) *collector.HealthData
}

func healthHandler(p HealthProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (HealthList, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (HealthList, error) {
		if p == nil {
			return HealthList{}, &contract.Error{Code: contract.CodeUnavailable, Message: "health provider not configured"}
		}
		data := p.CollectHealth(ctx)
		if data == nil {
			return HealthList{}, nil
		}
		entries := make([]HealthEntry, 0, len(data.Services))
		for _, svc := range data.Services {
			entries = append(entries, HealthEntry{
				Name:       svc.Name,
				Status:     svc.Status,
				Message:    svc.Message,
				DurationMs: svc.Duration.Milliseconds(),
				Critical:   svc.Critical,
			})
		}
		return HealthList{
			OverallStatus:  data.OverallStatus,
			HealthySummary: data.Summary.Healthy,
			Total:          data.Summary.Total,
			Services:       entries,
		}, nil
	}
}
