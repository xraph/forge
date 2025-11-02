package backends

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryBackend is an in-memory service discovery backend
// This is useful for local development and testing
type MemoryBackend struct {
	services map[string]map[string]*ServiceInstance // service name -> instance ID -> instance
	watchers map[string][]func([]*ServiceInstance)  // service name -> watchers
	mu       sync.RWMutex
}

// NewMemoryBackend creates a new memory backend
func NewMemoryBackend() (*MemoryBackend, error) {
	return &MemoryBackend{
		services: make(map[string]map[string]*ServiceInstance),
		watchers: make(map[string][]func([]*ServiceInstance)),
	}, nil
}

// Name returns the backend name
func (b *MemoryBackend) Name() string {
	return "memory"
}

// Initialize initializes the backend
func (b *MemoryBackend) Initialize(ctx context.Context) error {
	return nil
}

// Register registers a service instance
func (b *MemoryBackend) Register(ctx context.Context, instance *ServiceInstance) error {
	if instance.ID == "" {
		return fmt.Errorf("service instance ID is required")
	}
	if instance.Name == "" {
		return fmt.Errorf("service name is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Create service map if not exists
	if _, ok := b.services[instance.Name]; !ok {
		b.services[instance.Name] = make(map[string]*ServiceInstance)
	}

	// Store copy of instance
	instanceCopy := *instance
	instanceCopy.LastHeartbeat = time.Now().Unix()
	b.services[instance.Name][instance.ID] = &instanceCopy

	// Notify watchers
	b.notifyWatchers(instance.Name)

	return nil
}

// Deregister deregisters a service instance
func (b *MemoryBackend) Deregister(ctx context.Context, serviceID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Find and remove the service instance
	var found bool
	var serviceName string

	for name, instances := range b.services {
		if _, ok := instances[serviceID]; ok {
			delete(instances, serviceID)
			found = true
			serviceName = name
			break
		}
	}

	if !found {
		return fmt.Errorf("service instance not found: %s", serviceID)
	}

	// Notify watchers
	b.notifyWatchers(serviceName)

	return nil
}

// Discover discovers service instances by name
func (b *MemoryBackend) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	instances, ok := b.services[serviceName]
	if !ok {
		return []*ServiceInstance{}, nil
	}

	// Return copies of instances
	result := make([]*ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		instanceCopy := *instance
		result = append(result, &instanceCopy)
	}

	return result, nil
}

// DiscoverWithTags discovers service instances by name and tags
func (b *MemoryBackend) DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error) {
	instances, err := b.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return instances, nil
	}

	// Filter instances by tags
	filtered := make([]*ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		if instance.HasAllTags(tags) {
			filtered = append(filtered, instance)
		}
	}

	return filtered, nil
}

// Watch watches for changes to a service
func (b *MemoryBackend) Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Add watcher
	b.watchers[serviceName] = append(b.watchers[serviceName], onChange)

	// Immediately notify with current instances
	if instances, ok := b.services[serviceName]; ok {
		current := make([]*ServiceInstance, 0, len(instances))
		for _, instance := range instances {
			instanceCopy := *instance
			current = append(current, &instanceCopy)
		}
		go onChange(current)
	} else {
		go onChange([]*ServiceInstance{})
	}

	return nil
}

// ListServices lists all registered services
func (b *MemoryBackend) ListServices(ctx context.Context) ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	services := make([]string, 0, len(b.services))
	for name := range b.services {
		services = append(services, name)
	}

	return services, nil
}

// Health checks backend health
func (b *MemoryBackend) Health(ctx context.Context) error {
	return nil // Memory backend is always healthy
}

// Close closes the backend
func (b *MemoryBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Clear all data
	b.services = make(map[string]map[string]*ServiceInstance)
	b.watchers = make(map[string][]func([]*ServiceInstance))

	return nil
}

// notifyWatchers notifies all watchers for a service (must be called with lock held)
func (b *MemoryBackend) notifyWatchers(serviceName string) {
	watchers, ok := b.watchers[serviceName]
	if !ok || len(watchers) == 0 {
		return
	}

	// Get current instances
	instances, ok := b.services[serviceName]
	if !ok {
		instances = make(map[string]*ServiceInstance)
	}

	// Create copies for notification
	current := make([]*ServiceInstance, 0, len(instances))
	for _, instance := range instances {
		instanceCopy := *instance
		current = append(current, &instanceCopy)
	}

	// Notify watchers asynchronously
	for _, watcher := range watchers {
		w := watcher // Capture for goroutine
		go w(current)
	}
}

// GetInstanceCount returns the number of instances for a service
func (b *MemoryBackend) GetInstanceCount(serviceName string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if instances, ok := b.services[serviceName]; ok {
		return len(instances)
	}

	return 0
}

// GetTotalServiceCount returns the total number of registered services
func (b *MemoryBackend) GetTotalServiceCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.services)
}
