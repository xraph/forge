package pilot

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// ServicesProvider is the slice of the collector's API the pilot calls.
// Splitting it out lets tests stub the collector without the full DataCollector.
type ServicesProvider interface {
	CollectServices(ctx context.Context) []collector.ServiceInfo
	CollectServiceDetail(ctx context.Context, name string) *collector.ServiceDetail
}

func servicesListHandler(p ServicesProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (ServicesList, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (ServicesList, error) {
		return ServicesList{Services: p.CollectServices(ctx)}, nil
	}
}

func servicesDetailHandler(p ServicesProvider) func(ctx context.Context, in ServiceDetailInput, _ contract.Principal) (*ServiceDetailResponse, error) {
	return func(ctx context.Context, in ServiceDetailInput, _ contract.Principal) (*ServiceDetailResponse, error) {
		if in.Name == "" {
			return nil, &contract.Error{Code: contract.CodeBadRequest, Message: "name is required"}
		}
		d := p.CollectServiceDetail(ctx, in.Name)
		if d == nil {
			return nil, &contract.Error{Code: contract.CodeNotFound, Message: "service " + in.Name + " not found"}
		}
		return d, nil
	}
}
