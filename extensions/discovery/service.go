package discovery

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"

	"github.com/xraph/forge"
)

// Service provides high-level service discovery operations
type Service struct {
	backend         Backend
	logger          forge.Logger
	roundRobinIndex atomic.Uint64
}

// NewService creates a new service discovery service
func NewService(backend Backend, logger forge.Logger) *Service {
	return &Service{
		backend: backend,
		logger:  logger,
	}
}

// Register registers a service instance
func (s *Service) Register(ctx context.Context, instance *ServiceInstance) error {
	if err := s.backend.Register(ctx, instance); err != nil {
		s.logger.Warn("failed to register service",
			forge.F("service_id", instance.ID),
			forge.F("service_name", instance.Name),
			forge.F("error", err),
		)
		return err
	}

	s.logger.Info("service registered",
		forge.F("service_id", instance.ID),
		forge.F("service_name", instance.Name),
		forge.F("address", fmt.Sprintf("%s:%d", instance.Address, instance.Port)),
	)

	return nil
}

// Deregister deregisters a service instance
func (s *Service) Deregister(ctx context.Context, serviceID string) error {
	if err := s.backend.Deregister(ctx, serviceID); err != nil {
		s.logger.Warn("failed to deregister service",
			forge.F("service_id", serviceID),
			forge.F("error", err),
		)
		return err
	}

	s.logger.Info("service deregistered", forge.F("service_id", serviceID))
	return nil
}

// Discover discovers service instances by name
func (s *Service) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	instances, err := s.backend.Discover(ctx, serviceName)
	if err != nil {
		s.logger.Warn("failed to discover service",
			forge.F("service", serviceName),
			forge.F("error", err),
		)
		return nil, err
	}

	s.logger.Debug("service discovered",
		forge.F("service", serviceName),
		forge.F("instances", len(instances)),
	)

	return instances, nil
}

// DiscoverWithTags discovers service instances by name and tags
func (s *Service) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error) {
	instances, err := s.backend.DiscoverWithTags(ctx, serviceName, tags)
	if err != nil {
		s.logger.Warn("failed to discover service with tags",
			forge.F("service", serviceName),
			forge.F("tags", tags),
			forge.F("error", err),
		)
		return nil, err
	}

	s.logger.Debug("service discovered with tags",
		forge.F("service", serviceName),
		forge.F("tags", tags),
		forge.F("instances", len(instances)),
	)

	return instances, nil
}

// DiscoverHealthy discovers only healthy service instances
func (s *Service) DiscoverHealthy(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	instances, err := s.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	healthy := make([]*ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		if instance.IsHealthy() {
			healthy = append(healthy, instance)
		}
	}

	return healthy, nil
}

// SelectInstance selects a single service instance using load balancing strategy
func (s *Service) SelectInstance(ctx context.Context, serviceName string, strategy LoadBalanceStrategy) (*ServiceInstance, error) {
	instances, err := s.DiscoverHealthy(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		return nil, fmt.Errorf("no healthy instances found for service: %s", serviceName)
	}

	var selected *ServiceInstance

	switch strategy {
	case LoadBalanceRoundRobin:
		selected = s.selectRoundRobin(instances)
	case LoadBalanceRandom:
		selected = s.selectRandom(instances)
	default:
		selected = s.selectRoundRobin(instances) // Default to round-robin
	}

	s.logger.Debug("service instance selected",
		forge.F("service", serviceName),
		forge.F("instance_id", selected.ID),
		forge.F("strategy", strategy),
	)

	return selected, nil
}

// Watch watches for changes to a service
func (s *Service) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	return s.backend.Watch(ctx, serviceName, onChange)
}

// ListServices lists all registered services
func (s *Service) ListServices(ctx context.Context) ([]string, error) {
	services, err := s.backend.ListServices(ctx)
	if err != nil {
		s.logger.Warn("failed to list services", forge.F("error", err))
		return nil, err
	}

	return services, nil
}

// GetServiceURL gets a service URL using load balancing
func (s *Service) GetServiceURL(ctx context.Context, serviceName string, scheme string, strategy LoadBalanceStrategy) (string, error) {
	instance, err := s.SelectInstance(ctx, serviceName, strategy)
	if err != nil {
		return "", err
	}

	if scheme == "" {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, instance.Address, instance.Port), nil
}

// selectRoundRobin selects an instance using round-robin
func (s *Service) selectRoundRobin(instances []*ServiceInstance) *ServiceInstance {
	index := s.roundRobinIndex.Add(1) - 1
	return instances[int(index)%len(instances)]
}

// selectRandom selects a random instance
func (s *Service) selectRandom(instances []*ServiceInstance) *ServiceInstance {
	return instances[rand.Intn(len(instances))]
}

// Health checks backend health
func (s *Service) Health(ctx context.Context) error {
	return s.backend.Health(ctx)
}

