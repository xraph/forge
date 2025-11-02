package backends

import (
	"context"
	"fmt"
	"sync"

	consulapi "github.com/hashicorp/consul/api"
)

// ConsulBackend implements service discovery using Consul
type ConsulBackend struct {
	client   *consulapi.Client
	config   ConsulConfig
	watchers map[string][]func([]*ServiceInstance)
	mu       sync.RWMutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewConsulBackend creates a new Consul backend
func NewConsulBackend(config ConsulConfig) (*ConsulBackend, error) {
	// Create Consul client configuration
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = config.Address

	if config.Token != "" {
		consulConfig.Token = config.Token
	}

	if config.Datacenter != "" {
		consulConfig.Datacenter = config.Datacenter
	}

	// TLS configuration
	if config.TLSEnabled {
		consulConfig.Scheme = "https"
		tlsConfig := &consulapi.TLSConfig{}

		if config.TLSCAFile != "" {
			tlsConfig.CAFile = config.TLSCAFile
		}
		if config.TLSCert != "" {
			tlsConfig.CertFile = config.TLSCert
		}
		if config.TLSKey != "" {
			tlsConfig.KeyFile = config.TLSKey
		}

		consulConfig.TLSConfig = *tlsConfig
	}

	// Create client
	client, err := consulapi.NewClient(consulConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %w", err)
	}

	return &ConsulBackend{
		client:   client,
		config:   config,
		watchers: make(map[string][]func([]*ServiceInstance)),
		stopCh:   make(chan struct{}),
	}, nil
}

// Name returns the backend name
func (b *ConsulBackend) Name() string {
	return "consul"
}

// Initialize initializes the backend
func (b *ConsulBackend) Initialize(ctx context.Context) error {
	// Test connection
	_, err := b.client.Agent().Self()
	if err != nil {
		return fmt.Errorf("failed to connect to consul: %w", err)
	}

	return nil
}

// Register registers a service instance with Consul
func (b *ConsulBackend) Register(ctx context.Context, instance *ServiceInstance) error {
	if instance.ID == "" {
		return fmt.Errorf("service instance ID is required")
	}
	if instance.Name == "" {
		return fmt.Errorf("service name is required")
	}

	// Create Consul service registration
	registration := &consulapi.AgentServiceRegistration{
		ID:      instance.ID,
		Name:    instance.Name,
		Tags:    instance.Tags,
		Port:    instance.Port,
		Address: instance.Address,
		Meta:    instance.Metadata,
	}

	// Add health check if service has health status
	if instance.Status == HealthStatusPassing {
		registration.Check = &consulapi.AgentServiceCheck{
			TTL:                            "30s",
			DeregisterCriticalServiceAfter: "1m",
		}
	}

	// Register with Consul
	if err := b.client.Agent().ServiceRegister(registration); err != nil {
		return fmt.Errorf("failed to register service with consul: %w", err)
	}

	// Update TTL check to passing
	if instance.Status == HealthStatusPassing {
		if err := b.client.Agent().UpdateTTL("service:"+instance.ID, "", "passing"); err != nil {
			// Non-fatal error
		}
	}

	// Notify watchers
	b.notifyWatchers(instance.Name)

	return nil
}

// Deregister deregisters a service instance from Consul
func (b *ConsulBackend) Deregister(ctx context.Context, serviceID string) error {
	if err := b.client.Agent().ServiceDeregister(serviceID); err != nil {
		return fmt.Errorf("failed to deregister service from consul: %w", err)
	}

	return nil
}

// Discover discovers service instances by name from Consul
func (b *ConsulBackend) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	// Query Consul for service instances
	services, _, err := b.client.Health().Service(serviceName, "", false, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to discover service from consul: %w", err)
	}

	// Convert Consul entries to ServiceInstance
	instances := make([]*ServiceInstance, 0, len(services))
	for _, entry := range services {
		instance := b.convertConsulEntry(entry)
		instances = append(instances, instance)
	}

	return instances, nil
}

// DiscoverWithTags discovers service instances by name and tags from Consul
func (b *ConsulBackend) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error) {
	if len(tags) == 0 {
		return b.Discover(ctx, serviceName)
	}

	// Consul supports filtering by a single tag
	// For multiple tags, we'll query with the first tag and filter the rest manually
	tag := tags[0]
	services, _, err := b.client.Health().Service(serviceName, tag, false, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to discover service from consul: %w", err)
	}

	// Convert and filter by remaining tags
	instances := make([]*ServiceInstance, 0, len(services))
	for _, entry := range services {
		instance := b.convertConsulEntry(entry)
		if instance.HasAllTags(tags) {
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// Watch watches for changes to a service in Consul
func (b *ConsulBackend) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	b.mu.Lock()
	b.watchers[serviceName] = append(b.watchers[serviceName], onChange)
	b.mu.Unlock()

	// Start watch goroutine if not already running
	b.wg.Add(1)
	go b.watchService(serviceName)

	return nil
}

// watchService watches a service for changes
func (b *ConsulBackend) watchService(serviceName string) {
	defer b.wg.Done()

	queryOpts := &consulapi.QueryOptions{
		WaitTime: 30, // 30 second blocking query
	}

	for {
		select {
		case <-b.stopCh:
			return
		default:
			services, meta, err := b.client.Health().Service(serviceName, "", false, queryOpts)
			if err != nil {
				// Continue on error
				continue
			}

			// Update query index for next call
			queryOpts.WaitIndex = meta.LastIndex

			// Convert and notify watchers
			instances := make([]*ServiceInstance, 0, len(services))
			for _, entry := range services {
				instance := b.convertConsulEntry(entry)
				instances = append(instances, instance)
			}

			b.mu.RLock()
			watchers := b.watchers[serviceName]
			b.mu.RUnlock()

			for _, watcher := range watchers {
				go watcher(instances)
			}
		}
	}
}

// ListServices lists all registered services in Consul
func (b *ConsulBackend) ListServices(ctx context.Context) ([]string, error) {
	services, _, err := b.client.Catalog().Services(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list services from consul: %w", err)
	}

	serviceNames := make([]string, 0, len(services))
	for name := range services {
		serviceNames = append(serviceNames, name)
	}

	return serviceNames, nil
}

// Health checks backend health
func (b *ConsulBackend) Health(ctx context.Context) error {
	_, err := b.client.Agent().Self()
	if err != nil {
		return fmt.Errorf("consul health check failed: %w", err)
	}
	return nil
}

// Close closes the backend
func (b *ConsulBackend) Close() error {
	close(b.stopCh)
	b.wg.Wait()
	return nil
}

// notifyWatchers notifies all watchers for a service
func (b *ConsulBackend) notifyWatchers(serviceName string) {
	instances, err := b.Discover(context.Background(), serviceName)
	if err != nil {
		return
	}

	b.mu.RLock()
	watchers := b.watchers[serviceName]
	b.mu.RUnlock()

	for _, watcher := range watchers {
		go watcher(instances)
	}
}

// convertConsulEntry converts a Consul health entry to ServiceInstance
func (b *ConsulBackend) convertConsulEntry(entry *consulapi.ServiceEntry) *ServiceInstance {
	// Determine health status
	status := HealthStatusPassing
	for _, check := range entry.Checks {
		switch check.Status {
		case consulapi.HealthCritical:
			status = HealthStatusCritical
		case consulapi.HealthWarning:
			if status != HealthStatusCritical {
				status = HealthStatusWarning
			}
		}
	}

	return &ServiceInstance{
		ID:       entry.Service.ID,
		Name:     entry.Service.Service,
		Version:  entry.Service.Meta["version"],
		Address:  entry.Service.Address,
		Port:     entry.Service.Port,
		Tags:     entry.Service.Tags,
		Metadata: entry.Service.Meta,
		Status:   status,
	}
}

// UpdateServiceHealth updates the health status of a service instance
func (b *ConsulBackend) UpdateServiceHealth(ctx context.Context, serviceID string, status HealthStatus) error {
	var consulStatus string
	switch status {
	case HealthStatusPassing:
		consulStatus = "passing"
	case HealthStatusWarning:
		consulStatus = "warn"
	case HealthStatusCritical:
		consulStatus = "critical"
	default:
		consulStatus = "passing"
	}

	// Update TTL check
	checkID := "service:" + serviceID
	if err := b.client.Agent().UpdateTTL(checkID, "", consulStatus); err != nil {
		return fmt.Errorf("failed to update service health: %w", err)
	}

	return nil
}

// GetServiceByID retrieves a specific service instance by ID
func (b *ConsulBackend) GetServiceByID(ctx context.Context, serviceID string) (*ServiceInstance, error) {
	services, err := b.client.Agent().Services()
	if err != nil {
		return nil, fmt.Errorf("failed to get services: %w", err)
	}

	service, ok := services[serviceID]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceID)
	}

	return &ServiceInstance{
		ID:       service.ID,
		Name:     service.Service,
		Version:  service.Meta["version"],
		Address:  service.Address,
		Port:     service.Port,
		Tags:     service.Tags,
		Metadata: service.Meta,
	}, nil
}

// ensureNamespace ensures the namespace exists (Consul Enterprise)
func (b *ConsulBackend) ensureNamespace(ctx context.Context) error {
	if b.config.Namespace == "" {
		return nil
	}

	// Create namespace if it doesn't exist (Enterprise only)
	// This is a no-op in OSS Consul
	return nil
}

