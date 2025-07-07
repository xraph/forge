package core

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge/database"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
)

// ServiceScope defines the lifecycle of a service
type ServiceScope int

const (
	ScopeSingleton ServiceScope = iota
	ScopeTransient
	ScopeScoped
)

// ServiceDefinition holds service registration information
type ServiceDefinition struct {
	Name         string
	Instance     interface{}
	Factory      func() interface{}
	FactoryCtx   func(context.Context, Container) interface{}
	Scope        ServiceScope
	Dependencies []string
	Tags         []string
	Priority     int
	StartTimeout time.Duration // NEW: Per-service start timeout
}

// ServiceState represents the state of a service
type ServiceState int32

const (
	ServiceStateNotStarted ServiceState = iota
	ServiceStateStarting
	ServiceStateStarted
	ServiceStateStopping
	ServiceStateStopped
	ServiceStateFailed
)

// serviceInstance holds a service instance and its state
type serviceInstance struct {
	instance  interface{}
	state     int32 // atomic ServiceState
	err       error
	mu        sync.RWMutex
	startTime time.Time
}

// container implements the Container interface
type container struct {
	mu        sync.RWMutex
	services  map[string]*ServiceDefinition
	instances map[string]*serviceInstance
	scoped    map[string]interface{}
	logger    logger.Logger
	ctx       context.Context

	// State management with atomic operations
	state     int32 // atomic ServiceState
	startOnce sync.Once
	stopOnce  sync.Once

	// Component caches
	databases map[string]database.SQLDatabase
	nosqlDbs  map[string]database.NoSQLDatabase
	caches    map[string]database.Cache

	// Startup configuration
	startupTimeout time.Duration
	serviceTimeout time.Duration
}

// NewContainer creates a new dependency injection container
func NewContainer(logger logger.Logger) Container {
	c := &container{
		services:       make(map[string]*ServiceDefinition),
		instances:      make(map[string]*serviceInstance),
		scoped:         make(map[string]interface{}),
		databases:      make(map[string]database.SQLDatabase),
		nosqlDbs:       make(map[string]database.NoSQLDatabase),
		caches:         make(map[string]database.Cache),
		logger:         logger,
		ctx:            context.Background(),
		state:          int32(ServiceStateNotStarted),
		startupTimeout: 60 * time.Second, // Overall startup timeout
		serviceTimeout: 30 * time.Second, // Per-service startup timeout
	}

	// Register self
	c.services["container"] = &ServiceDefinition{
		Name:         "container",
		Instance:     c,
		Scope:        ScopeSingleton,
		StartTimeout: 1 * time.Second,
	}
	c.instances["container"] = &serviceInstance{
		instance: c,
		state:    int32(ServiceStateStarted),
	}

	// Register logger
	c.services["logger"] = &ServiceDefinition{
		Name:         "logger",
		Instance:     logger,
		Scope:        ScopeSingleton,
		StartTimeout: 1 * time.Second,
	}
	c.instances["logger"] = &serviceInstance{
		instance: logger,
		state:    int32(ServiceStateStarted),
	}

	return c
}

// isStarted checks if the container is started (thread-safe)
func (c *container) isStarted() bool {
	return atomic.LoadInt32(&c.state) == int32(ServiceStateStarted)
}

// setState sets the container state atomically
func (c *container) setState(state ServiceState) {
	atomic.StoreInt32(&c.state, int32(state))
}

// getState gets the container state atomically
func (c *container) getState() ServiceState {
	return ServiceState(atomic.LoadInt32(&c.state))
}

// Register registers a service instance
func (c *container) Register(name string, instance interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	c.services[name] = &ServiceDefinition{
		Name:         name,
		Instance:     instance,
		Scope:        ScopeSingleton,
		StartTimeout: c.serviceTimeout,
	}

	c.instances[name] = &serviceInstance{
		instance: instance,
		state:    int32(ServiceStateNotStarted),
	}

	c.logger.Debug("Service registered",
		logger.String("name", name),
		logger.String("type", reflect.TypeOf(instance).String()),
	)

	return nil
}

// RegisterSingleton registers a singleton service with a factory
func (c *container) RegisterSingleton(name string, factory func() interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	c.services[name] = &ServiceDefinition{
		Name:         name,
		Factory:      factory,
		Scope:        ScopeSingleton,
		StartTimeout: c.serviceTimeout,
	}

	c.logger.Debug("Singleton service registered", logger.String("name", name))
	return nil
}

// RegisterScoped registers a scoped service with a factory
func (c *container) RegisterScoped(name string, factory func() interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	c.services[name] = &ServiceDefinition{
		Name:         name,
		Factory:      factory,
		Scope:        ScopeScoped,
		StartTimeout: c.serviceTimeout,
	}

	c.logger.Debug("Scoped service registered", logger.String("name", name))
	return nil
}

// RegisterTransient registers a transient service with a factory
func (c *container) RegisterTransient(name string, factory func() interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	c.services[name] = &ServiceDefinition{
		Name:         name,
		Factory:      factory,
		Scope:        ScopeTransient,
		StartTimeout: c.serviceTimeout,
	}

	c.logger.Debug("Transient service registered", logger.String("name", name))
	return nil
}

// RegisterWithDependencies registers a service with explicit dependencies
func (c *container) RegisterWithDependencies(name string, factory func() interface{}, dependencies []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	c.services[name] = &ServiceDefinition{
		Name:         name,
		Factory:      factory,
		Scope:        ScopeSingleton,
		Dependencies: dependencies,
		StartTimeout: c.serviceTimeout,
	}

	c.logger.Debug("Service with dependencies registered",
		logger.String("name", name),
		logger.Any("dependencies", dependencies),
	)
	return nil
}

// RegisterContextual registers a service with context-aware factory
func (c *container) RegisterContextual(name string, factory func(context.Context, Container) interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.services[name]; exists {
		return fmt.Errorf("service %s already registered", name)
	}

	c.services[name] = &ServiceDefinition{
		Name:         name,
		FactoryCtx:   factory,
		Scope:        ScopeSingleton,
		StartTimeout: c.serviceTimeout,
	}

	c.logger.Debug("Contextual service registered", logger.String("name", name))
	return nil
}

// Resolve resolves a service by name
func (c *container) Resolve(name string) (interface{}, error) {
	c.mu.RLock()
	service, exists := c.services[name]
	c.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("service %s not found", name)
	}

	return c.resolveService(service)
}

// MustResolve resolves a service and panics if not found
func (c *container) MustResolve(name string) interface{} {
	instance, err := c.Resolve(name)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve service %s: %v", name, err))
	}
	return instance
}

// ResolveInto resolves dependencies into a struct using reflection
func (c *container) ResolveInto(target interface{}) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	targetValue = targetValue.Elem()
	targetType := targetValue.Type()

	for i := 0; i < targetValue.NumField(); i++ {
		field := targetValue.Field(i)
		fieldType := targetType.Field(i)

		// Check for inject tag
		injectTag := fieldType.Tag.Get("inject")
		if injectTag == "" {
			continue
		}

		// Skip if field is not settable
		if !field.CanSet() {
			c.logger.Warn("Cannot set field", logger.String("field", fieldType.Name))
			continue
		}

		// Resolve the dependency
		instance, err := c.Resolve(injectTag)
		if err != nil {
			return fmt.Errorf("failed to resolve dependency %s for field %s: %w",
				injectTag, fieldType.Name, err)
		}

		// Set the field value
		instanceValue := reflect.ValueOf(instance)
		if !instanceValue.Type().AssignableTo(field.Type()) {
			return fmt.Errorf("cannot assign %s to field %s of type %s",
				instanceValue.Type(), fieldType.Name, field.Type())
		}

		field.Set(instanceValue)
		c.logger.Debug("Dependency injected",
			logger.String("field", fieldType.Name),
			logger.String("service", injectTag),
		)
	}

	return nil
}

// resolveService resolves a specific service definition
func (c *container) resolveService(service *ServiceDefinition) (interface{}, error) {
	switch service.Scope {
	case ScopeSingleton:
		return c.resolveSingleton(service)
	case ScopeScoped:
		return c.resolveScoped(service)
	case ScopeTransient:
		return c.resolveTransient(service)
	default:
		return nil, fmt.Errorf("unknown service scope: %v", service.Scope)
	}
}

// resolveSingleton resolves a singleton service
func (c *container) resolveSingleton(service *ServiceDefinition) (interface{}, error) {
	c.mu.RLock()
	if instance, exists := c.instances[service.Name]; exists {
		c.mu.RUnlock()
		instance.mu.RLock()
		defer instance.mu.RUnlock()
		if instance.err != nil {
			return nil, instance.err
		}
		return instance.instance, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check pattern
	if instance, exists := c.instances[service.Name]; exists {
		instance.mu.RLock()
		defer instance.mu.RUnlock()
		if instance.err != nil {
			return nil, instance.err
		}
		return instance.instance, nil
	}

	// Create instance
	instance, err := c.createInstance(service)
	if err != nil {
		c.instances[service.Name] = &serviceInstance{
			instance: nil,
			state:    int32(ServiceStateFailed),
			err:      err,
		}
		return nil, err
	}

	c.instances[service.Name] = &serviceInstance{
		instance: instance,
		state:    int32(ServiceStateNotStarted),
	}

	return instance, nil
}

// resolveScoped resolves a scoped service
func (c *container) resolveScoped(service *ServiceDefinition) (interface{}, error) {
	c.mu.RLock()
	if instance, exists := c.scoped[service.Name]; exists {
		c.mu.RUnlock()
		return instance, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check pattern
	if instance, exists := c.scoped[service.Name]; exists {
		return instance, nil
	}

	// Create instance
	instance, err := c.createInstance(service)
	if err != nil {
		return nil, err
	}

	c.scoped[service.Name] = instance
	return instance, nil
}

// resolveTransient resolves a transient service (always creates new instance)
func (c *container) resolveTransient(service *ServiceDefinition) (interface{}, error) {
	return c.createInstance(service)
}

// createInstance creates a new instance of a service
func (c *container) createInstance(service *ServiceDefinition) (interface{}, error) {
	if service.Instance != nil {
		return service.Instance, nil
	}

	var instance interface{}

	if service.FactoryCtx != nil {
		instance = service.FactoryCtx(c.ctx, c)
	} else if service.Factory != nil {
		instance = service.Factory()
	} else {
		return nil, fmt.Errorf("no factory or instance available for service %s", service.Name)
	}

	// Inject dependencies if the instance is a struct pointer
	if err := c.ResolveInto(instance); err != nil {
		c.logger.Warn("Failed to inject dependencies",
			logger.String("service", service.Name),
			logger.Error(err),
		)
		// Don't fail here, as not all services need dependency injection
	}

	c.logger.Debug("Service instance created",
		logger.String("name", service.Name),
		logger.String("type", reflect.TypeOf(instance).String()),
	)

	return instance, nil
}

// Database access methods

// SQL returns a SQL database connection
func (c *container) SQL(name ...string) database.SQLDatabase {
	dbName := "default"
	if len(name) > 0 {
		dbName = name[0]
	}

	c.mu.RLock()
	if db, exists := c.databases[dbName]; exists {
		c.mu.RUnlock()
		return db
	}
	c.mu.RUnlock()

	// Try to resolve from container
	instance, err := c.Resolve("database:" + dbName)
	if err != nil {
		c.logger.Error("Failed to resolve SQL database",
			logger.String("name", dbName),
			logger.Error(err),
		)
		return nil
	}

	if db, ok := instance.(database.SQLDatabase); ok {
		c.mu.Lock()
		c.databases[dbName] = db
		c.mu.Unlock()
		return db
	}

	c.logger.Error("Resolved instance is not a SQL database",
		logger.String("name", dbName),
		logger.String("type", reflect.TypeOf(instance).String()),
	)
	return nil
}

// NoSQL returns a NoSQL database connection
func (c *container) NoSQL(name ...string) database.NoSQLDatabase {
	dbName := "default"
	if len(name) > 0 {
		dbName = name[0]
	}

	c.mu.RLock()
	if db, exists := c.nosqlDbs[dbName]; exists {
		c.mu.RUnlock()
		return db
	}
	c.mu.RUnlock()

	// Try to resolve from container
	instance, err := c.Resolve("nosql:" + dbName)
	if err != nil {
		c.logger.Error("Failed to resolve NoSQL database",
			logger.String("name", dbName),
			logger.Error(err),
		)
		return nil
	}

	if db, ok := instance.(database.NoSQLDatabase); ok {
		c.mu.Lock()
		c.nosqlDbs[dbName] = db
		c.mu.Unlock()
		return db
	}

	c.logger.Error("Resolved instance is not a NoSQL database",
		logger.String("name", dbName),
		logger.String("type", reflect.TypeOf(instance).String()),
	)
	return nil
}

// Cache returns a cache connection
func (c *container) Cache(name ...string) database.Cache {
	cacheName := "default"
	if len(name) > 0 {
		cacheName = name[0]
	}

	c.mu.RLock()
	if cache, exists := c.caches[cacheName]; exists {
		c.mu.RUnlock()
		return cache
	}
	c.mu.RUnlock()

	// Try to resolve from container
	instance, err := c.Resolve("cache:" + cacheName)
	if err != nil {
		c.logger.Error("Failed to resolve cache",
			logger.String("name", cacheName),
			logger.Error(err),
		)
		return nil
	}

	if cache, ok := instance.(database.Cache); ok {
		c.mu.Lock()
		c.caches[cacheName] = cache
		c.mu.Unlock()
		return cache
	}

	c.logger.Error("Resolved instance is not a cache",
		logger.String("name", cacheName),
		logger.String("type", reflect.TypeOf(instance).String()),
	)
	return nil
}

// Service access methods

// Jobs returns the job processor
func (c *container) Jobs() jobs.Processor {
	instance, err := c.Resolve("jobs")
	if err != nil {
		c.logger.Error("Failed to resolve jobs processor", logger.Error(err))
		return nil
	}

	if processor, ok := instance.(jobs.Processor); ok {
		return processor
	}

	c.logger.Error("Resolved instance is not a job processor",
		logger.String("type", reflect.TypeOf(instance).String()),
	)
	return nil
}

// Metrics returns the metrics collector
func (c *container) Metrics() observability.Metrics {
	instance, err := c.Resolve("metrics")
	if err != nil {
		c.logger.Error("Failed to resolve metrics collector", logger.Error(err))
		return nil
	}

	if metrics, ok := instance.(observability.Metrics); ok {
		return metrics
	}

	c.logger.Error("Resolved instance is not a metrics collector",
		logger.String("type", reflect.TypeOf(instance).String()),
	)
	return nil
}

// Tracer returns the tracer
func (c *container) Tracer() observability.Tracer {
	instance, err := c.Resolve("tracer")
	if err != nil {
		c.logger.Error("Failed to resolve tracer", logger.Error(err))
		return nil
	}

	if tracer, ok := instance.(observability.Tracer); ok {
		return tracer
	}

	c.logger.Error("Resolved instance is not a tracer",
		logger.String("type", reflect.TypeOf(instance).String()),
	)
	return nil
}

// Health returns the health checker
func (c *container) Health() observability.Health {
	instance, err := c.Resolve("health")
	if err != nil {
		c.logger.Error("Failed to resolve health checker", logger.Error(err))
		return nil
	}

	if health, ok := instance.(observability.Health); ok {
		return health
	}

	c.logger.Error("Resolved instance is not a health checker",
		logger.String("type", reflect.TypeOf(instance).String()),
	)
	return nil
}

// Lifecycle methods

// Start starts all registered services that implement lifecycle interfaces
func (c *container) Start(ctx context.Context) error {
	// Use atomic check and sync.Once to ensure we only start once
	if c.isStarted() {
		c.logger.Debug("Container already started, skipping")
		return nil
	}

	var startErr error
	c.startOnce.Do(func() {
		c.setState(ServiceStateStarting)
		c.ctx = ctx

		c.logger.Info("Starting container")

		// Create overall startup timeout context
		startupCtx, cancel := context.WithTimeout(ctx, c.startupTimeout)
		defer cancel()

		// Get services sorted by priority and dependencies
		startOrder, err := c.getStartOrder()
		if err != nil {
			startErr = fmt.Errorf("failed to determine start order: %w", err)
			c.setState(ServiceStateFailed)
			return
		}

		c.logger.Debug("Service start order determined",
			logger.Strings("order", startOrder),
			logger.Int("total_services", len(startOrder)),
		)

		// Start services in dependency order with timeouts
		for i, name := range startOrder {
			select {
			case <-startupCtx.Done():
				startErr = fmt.Errorf("container startup timeout exceeded")
				c.setState(ServiceStateFailed)
				return
			default:
			}

			c.logger.Debug("Starting service",
				logger.String("name", name),
				logger.Int("order", i+1),
				logger.Int("total", len(startOrder)),
			)

			if err := c.startServiceWithTimeout(startupCtx, name); err != nil {
				startErr = fmt.Errorf("failed to start service %s: %w", name, err)
				c.setState(ServiceStateFailed)
				return
			}

			c.logger.Debug("Service started successfully",
				logger.String("name", name),
				logger.Int("order", i+1),
			)
		}

		c.setState(ServiceStateStarted)
		c.logger.Info("Container started successfully",
			logger.Int("services_started", len(startOrder)),
			logger.Duration("startup_duration", time.Since(time.Now())),
		)
	})

	return startErr
}

// startServiceWithTimeout starts a specific service with timeout protection
func (c *container) startServiceWithTimeout(ctx context.Context, name string) error {
	c.mu.RLock()
	service := c.services[name]
	serviceInstance, exists := c.instances[name]
	c.mu.RUnlock()

	if !exists {
		// Resolve the service first to create the instance
		instance, err := c.Resolve(name)
		if err != nil {
			return fmt.Errorf("failed to resolve service %s: %w", name, err)
		}

		c.mu.RLock()
		serviceInstance = c.instances[name]
		c.mu.RUnlock()

		if serviceInstance == nil {
			return fmt.Errorf("service instance %s not found after resolution", name)
		}

		// Update instance if it was created during resolution
		serviceInstance.mu.Lock()
		if serviceInstance.instance == nil {
			serviceInstance.instance = instance
		}
		serviceInstance.mu.Unlock()
	}

	// Check if already started
	serviceInstance.mu.Lock()
	currentState := ServiceState(atomic.LoadInt32(&serviceInstance.state))
	if currentState == ServiceStateStarted {
		serviceInstance.mu.Unlock()
		return nil
	}
	if currentState == ServiceStateStarting {
		serviceInstance.mu.Unlock()
		return fmt.Errorf("service %s is already starting", name)
	}

	// Set starting state
	atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateStarting))
	instance := serviceInstance.instance
	serviceInstance.startTime = time.Now()
	serviceInstance.mu.Unlock()

	// Check if service implements Starter interface
	if starter, ok := instance.(interface{ Start(context.Context) error }); ok {
		// Use service-specific timeout or default
		timeout := service.StartTimeout
		if timeout == 0 {
			timeout = c.serviceTimeout
		}

		// Create timeout context for this specific service
		serviceCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Start service with timeout protection
		startErr := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					startErr <- fmt.Errorf("service startup panic: %v", r)
				}
			}()
			startErr <- starter.Start(serviceCtx)
		}()

		select {
		case err := <-startErr:
			if err != nil {
				serviceInstance.mu.Lock()
				atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateFailed))
				serviceInstance.err = err
				serviceInstance.mu.Unlock()
				return fmt.Errorf("failed to start service %s: %w", name, err)
			}
		case <-serviceCtx.Done():
			serviceInstance.mu.Lock()
			atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateFailed))
			serviceInstance.err = fmt.Errorf("service start timeout (%v)", timeout)
			serviceInstance.mu.Unlock()
			return fmt.Errorf("service %s start timeout after %v", name, timeout)
		}

		c.logger.Debug("Service started",
			logger.String("name", name),
			logger.Duration("start_time", time.Since(serviceInstance.startTime)),
		)
	}

	// Set started state
	serviceInstance.mu.Lock()
	atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateStarted))
	serviceInstance.err = nil
	serviceInstance.mu.Unlock()

	return nil
}

// getStartOrder returns services in start order based on dependencies and priorities
func (c *container) getStartOrder() ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Simple topological sort based on dependencies
	var order []string
	visited := make(map[string]bool)
	visiting := make(map[string]bool)

	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("circular dependency detected involving service %s", name)
		}
		if visited[name] {
			return nil
		}

		visiting[name] = true
		service := c.services[name]

		// Visit dependencies first
		for _, dep := range service.Dependencies {
			if _, exists := c.services[dep]; !exists {
				return fmt.Errorf("dependency %s not found for service %s", dep, name)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}

		visiting[name] = false
		visited[name] = true
		order = append(order, name)
		return nil
	}

	// Visit all services
	for name := range c.services {
		if err := visit(name); err != nil {
			return nil, err
		}
	}

	// Sort by priority within dependency groups
	sort.SliceStable(order, func(i, j int) bool {
		serviceI := c.services[order[i]]
		serviceJ := c.services[order[j]]
		return serviceI.Priority > serviceJ.Priority
	})

	return order, nil
}

// Stop stops all registered services
func (c *container) Stop(ctx context.Context) error {
	// Use atomic check and sync.Once to ensure we only stop once
	if c.getState() == ServiceStateStopped {
		c.logger.Debug("Container already stopped, skipping")
		return nil
	}

	var stopErr error
	c.stopOnce.Do(func() {
		c.setState(ServiceStateStopping)
		c.logger.Info("Stopping container")

		// Create stop timeout context
		stopCtx, cancel := context.WithTimeout(ctx, c.startupTimeout)
		defer cancel()

		// Get stop order (reverse of start order)
		startOrder, err := c.getStartOrder()
		if err != nil {
			c.logger.Error("Failed to determine stop order", logger.Error(err))
			// Continue with best effort stop
		}

		// Reverse the order for stopping
		var stopOrder []string
		for i := len(startOrder) - 1; i >= 0; i-- {
			stopOrder = append(stopOrder, startOrder[i])
		}

		// Stop services in reverse order with timeout
		var errors []error
		for _, name := range stopOrder {
			select {
			case <-stopCtx.Done():
				errors = append(errors, fmt.Errorf("stop timeout exceeded for remaining services"))
				break
			default:
			}

			if err := c.stopServiceWithTimeout(stopCtx, name); err != nil {
				errors = append(errors, fmt.Errorf("failed to stop service %s: %w", name, err))
			}
		}

		// Clear scoped instances
		c.mu.Lock()
		c.scoped = make(map[string]interface{})
		c.mu.Unlock()

		c.setState(ServiceStateStopped)

		if len(errors) > 0 {
			c.logger.Error("Errors occurred during container shutdown", logger.Any("errors", errors))
			stopErr = fmt.Errorf("multiple errors during shutdown: %v", errors)
		} else {
			c.logger.Info("Container stopped successfully")
		}
	})

	return stopErr
}

// stopServiceWithTimeout stops a specific service with timeout protection
func (c *container) stopServiceWithTimeout(ctx context.Context, name string) error {
	c.mu.RLock()
	service := c.services[name]
	serviceInstance, exists := c.instances[name]
	c.mu.RUnlock()

	if !exists {
		return nil // Service was never started
	}

	serviceInstance.mu.Lock()
	currentState := ServiceState(atomic.LoadInt32(&serviceInstance.state))
	if currentState == ServiceStateStopped || currentState == ServiceStateStopping {
		serviceInstance.mu.Unlock()
		return nil
	}

	// Set stopping state
	atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateStopping))
	instance := serviceInstance.instance
	serviceInstance.mu.Unlock()

	// Check if service implements Stopper interface
	if stopper, ok := instance.(interface{ Stop(context.Context) error }); ok {
		// Use service-specific timeout or default
		timeout := service.StartTimeout // Reuse start timeout for stop
		if timeout == 0 {
			timeout = c.serviceTimeout
		}

		// Create timeout context for this specific service
		serviceCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Stop service with timeout protection
		stopErr := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					stopErr <- fmt.Errorf("service stop panic: %v", r)
				}
			}()
			stopErr <- stopper.Stop(serviceCtx)
		}()

		select {
		case err := <-stopErr:
			if err != nil {
				serviceInstance.mu.Lock()
				atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateFailed))
				serviceInstance.err = err
				serviceInstance.mu.Unlock()
				return err
			}
		case <-serviceCtx.Done():
			serviceInstance.mu.Lock()
			atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateFailed))
			serviceInstance.err = fmt.Errorf("service stop timeout (%v)", timeout)
			serviceInstance.mu.Unlock()
			return fmt.Errorf("service %s stop timeout after %v", name, timeout)
		}

		c.logger.Debug("Service stopped", logger.String("name", name))
	}

	// Set stopped state
	serviceInstance.mu.Lock()
	atomic.StoreInt32(&serviceInstance.state, int32(ServiceStateStopped))
	serviceInstance.err = nil
	serviceInstance.mu.Unlock()

	return nil
}

// Utility methods

// List returns a list of all registered services
func (c *container) List() map[string]*ServiceDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*ServiceDefinition)
	for name, service := range c.services {
		result[name] = service
	}
	return result
}

// IsRegistered checks if a service is registered
func (c *container) IsRegistered(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.services[name]
	return exists
}

// ClearScoped clears all scoped instances
func (c *container) ClearScoped() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scoped = make(map[string]interface{})
	c.logger.Debug("Scoped instances cleared")
}

// GetServicesByTag returns services with a specific tag
func (c *container) GetServicesByTag(tag string) map[string]*ServiceDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*ServiceDefinition)
	for name, service := range c.services {
		for _, serviceTag := range service.Tags {
			if serviceTag == tag {
				result[name] = service
				break
			}
		}
	}
	return result
}

// GetServiceState returns the state of a service
func (c *container) GetServiceState(name string) ServiceState {
	c.mu.RLock()
	serviceInstance, exists := c.instances[name]
	c.mu.RUnlock()

	if !exists {
		return ServiceStateNotStarted
	}

	return ServiceState(atomic.LoadInt32(&serviceInstance.state))
}

// IsServiceHealthy checks if a service is healthy
func (c *container) IsServiceHealthy(ctx context.Context, name string) error {
	instance, err := c.Resolve(name)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", name, err)
	}

	// Check if service implements health checker
	if healthChecker, ok := instance.(interface{ HealthCheck(context.Context) error }); ok {
		return healthChecker.HealthCheck(ctx)
	}

	// If no health check interface, assume healthy if started
	state := c.GetServiceState(name)
	if state != ServiceStateStarted {
		return fmt.Errorf("service %s is not in started state: %v", name, state)
	}

	return nil
}

// GetStartupReport returns a detailed startup report
func (c *container) GetStartupReport() map[string]ServiceStartupInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	report := make(map[string]ServiceStartupInfo)
	for name, instance := range c.instances {
		instance.mu.RLock()
		report[name] = ServiceStartupInfo{
			Name:      name,
			State:     ServiceState(atomic.LoadInt32(&instance.state)),
			StartTime: instance.startTime,
			Error:     instance.err,
		}
		instance.mu.RUnlock()
	}

	return report
}

// ServiceStartupInfo contains information about a service's startup
type ServiceStartupInfo struct {
	Name      string
	State     ServiceState
	StartTime time.Time
	Error     error
}
