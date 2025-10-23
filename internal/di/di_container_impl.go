package di

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/shared"
)

// containerImpl implements Container
type containerImpl struct {
	services  map[string]*serviceRegistration
	instances map[string]any
	graph     *DependencyGraph
	started   bool
	mu        sync.RWMutex
}

// serviceRegistration holds service registration details
type serviceRegistration struct {
	name         string
	factory      Factory
	singleton    bool
	scoped       bool
	dependencies []string
	groups       []string
	metadata     map[string]string
	instance     any
	started      bool
	mu           sync.RWMutex
}

// newContainerImpl creates a new DI container implementation
func newContainerImpl() Container {
	return &containerImpl{
		services:  make(map[string]*serviceRegistration),
		instances: make(map[string]any),
		graph:     NewDependencyGraph(),
	}
}

// Register adds a service factory to the container
func (c *containerImpl) Register(name string, factory Factory, opts ...RegisterOption) error {
	// Merge options
	merged := mergeOptions(opts)
	if name == "" {
		return fmt.Errorf("service name cannot be empty")
	}
	if factory == nil {
		return errors.ErrInvalidFactory
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[name]; exists {
		return errors.ErrServiceAlreadyExists(name)
	}

	// Create registration from merged options
	reg := &serviceRegistration{
		name:         name,
		factory:      factory,
		singleton:    merged.Lifecycle == "singleton",
		scoped:       merged.Lifecycle == "scoped",
		dependencies: merged.Dependencies,
		groups:       merged.Groups,
		metadata:     merged.Metadata,
	}

	// Add to services map
	c.services[name] = reg

	// Add to dependency graph
	c.graph.AddNode(name, reg.dependencies)

	return nil
}

// Resolve returns a service by name
func (c *containerImpl) Resolve(name string) (any, error) {
	c.mu.RLock()
	reg, exists := c.services[name]
	c.mu.RUnlock()

	if !exists {
		return nil, errors.ErrServiceNotFound(name)
	}

	// Singleton: return cached instance
	if reg.singleton {
		// Fast path: check if already created (read lock)
		reg.mu.RLock()
		if reg.instance != nil {
			instance := reg.instance
			reg.mu.RUnlock()
			return instance, nil
		}
		reg.mu.RUnlock()

		// Slow path: create instance (write lock for entire creation)
		// Use a separate creation lock to avoid blocking reads once created
		reg.mu.Lock()
		defer reg.mu.Unlock()

		// Double-check after acquiring write lock
		if reg.instance != nil {
			return reg.instance, nil
		}

		// Call factory while holding lock (container lock is separate, so no deadlock)
		// Note: factory may call c.Resolve() which uses c.mu (different lock)
		instance, err := reg.factory(c)
		if err != nil {
			return nil, errors.NewServiceError(name, "resolve", err)
		}

		reg.instance = instance
		return instance, nil
	}

	// Scoped services should be resolved from scope, not container
	if reg.scoped {
		return nil, fmt.Errorf("scoped service %s must be resolved from a scope", name)
	}

	// Transient: create new instance each time
	instance, err := reg.factory(c)
	if err != nil {
		return nil, errors.NewServiceError(name, "resolve", err)
	}

	return instance, nil
}

// Has checks if a service is registered
func (c *containerImpl) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.services[name]
	return exists
}

// Services returns all registered service names
func (c *containerImpl) Services() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.services))
	for name := range c.services {
		names = append(names, name)
	}
	return names
}

// BeginScope creates a new scope for request-scoped services
func (c *containerImpl) BeginScope() Scope {
	return newScope(c)
}

// Start initializes all services in dependency order
func (c *containerImpl) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errors.ErrContainerStarted
	}

	// Get services in dependency order
	order, err := c.graph.TopologicalSort()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	// Start services in order (without holding container lock)
	for _, name := range order {
		if err := c.startService(ctx, name); err != nil {
			// Rollback: stop already started services
			c.stopServices(ctx, order)
			return errors.NewServiceError(name, "start", err)
		}
	}

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	return nil
}

// Stop shuts down all services in reverse order
func (c *containerImpl) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil // Not an error, just no-op
	}

	// Get services in dependency order, then reverse
	order, err := c.graph.TopologicalSort()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	// Stop in reverse order (without holding container lock)
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		if err := c.stopService(ctx, name); err != nil {
			// Continue stopping other services, but collect error
			return errors.NewServiceError(name, "stop", err)
		}
	}

	c.mu.Lock()
	c.started = false
	c.mu.Unlock()
	return nil
}

// Health checks all services
func (c *containerImpl) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for name, reg := range c.services {
		// Only check singleton services that have been instantiated
		if !reg.singleton || reg.instance == nil {
			continue
		}

		if checker, ok := reg.instance.(shared.HealthChecker); ok {
			if err := checker.Health(ctx); err != nil {
				return errors.NewServiceError(name, "health", err)
			}
		}
	}

	return nil
}

// Inspect returns diagnostic information about a service
func (c *containerImpl) Inspect(name string) ServiceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	reg, exists := c.services[name]
	if !exists {
		return ServiceInfo{Name: name}
	}

	reg.mu.RLock()
	defer reg.mu.RUnlock()

	lifecycle := "transient"
	if reg.singleton {
		lifecycle = "singleton"
	} else if reg.scoped {
		lifecycle = "scoped"
	}

	typeName := "unknown"
	if reg.instance != nil {
		typeName = fmt.Sprintf("%T", reg.instance)
	}

	healthy := false
	if checker, ok := reg.instance.(shared.HealthChecker); ok {
		healthy = checker.Health(context.Background()) == nil
	}

	return ServiceInfo{
		Name:         name,
		Type:         typeName,
		Lifecycle:    lifecycle,
		Dependencies: reg.dependencies,
		Started:      reg.started,
		Healthy:      healthy,
		Metadata:     reg.metadata,
	}
}

// startService starts a single service
func (c *containerImpl) startService(ctx context.Context, name string) error {
	reg := c.services[name]

	// Resolve the service instance (creates if needed)
	instance, err := c.Resolve(name)
	if err != nil {
		return err
	}

	// Call Start if service implements Service interface
	if svc, ok := instance.(shared.Service); ok {
		if err := svc.Start(ctx); err != nil {
			return err
		}
		reg.mu.Lock()
		reg.started = true
		reg.mu.Unlock()
	}

	return nil
}

// stopService stops a single service
func (c *containerImpl) stopService(ctx context.Context, name string) error {
	reg := c.services[name]

	reg.mu.RLock()
	instance := reg.instance
	started := reg.started
	reg.mu.RUnlock()

	if !started || instance == nil {
		return nil
	}

	// Call Stop if service implements Service interface
	if svc, ok := instance.(shared.Service); ok {
		if err := svc.Stop(ctx); err != nil {
			return err
		}
		reg.mu.Lock()
		reg.started = false
		reg.mu.Unlock()
	}

	return nil
}

// stopServices stops multiple services (for rollback)
func (c *containerImpl) stopServices(ctx context.Context, names []string) {
	for i := len(names) - 1; i >= 0; i-- {
		_ = c.stopService(ctx, names[i])
	}
}
