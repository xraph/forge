package di

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Container DIContainer implements the dependency injection container
type Container struct {
	services              map[reflect.Type]*ServiceRegistration
	namedServices         map[string]*ServiceRegistration
	instances             map[reflect.Type]interface{}
	namedInstances        map[string]interface{}
	referenceNameMappings map[string]string
	lifecycle             *LifecycleManager
	configurator          *Configurator
	resolver              *Resolver
	validator             common.Validator
	interceptors          []Interceptor
	mu                    sync.RWMutex
	started               bool
	logger                common.Logger
	metrics               common.Metrics
	config                common.ConfigManager
	errorHandler          common.ErrorHandler
}

// ServiceRegistration represents a service registration
type ServiceRegistration struct {
	Name           string
	Type           reflect.Type
	Constructor    interface{}
	Singleton      bool
	Lifecycle      common.ServiceLifecycle
	Config         interface{}
	Dependencies   []string
	Tags           map[string]string
	Interceptors   []Interceptor
	Factory        ServiceFactory
	Instance       interface{}
	Created        time.Time
	ReferenceNames []string
	mu             sync.RWMutex
}

// ServiceFactory defines a function that creates service instances
type ServiceFactory func(ctx context.Context, container common.Container) (interface{}, error)

// Interceptor defines an interceptor for service creation
type Interceptor interface {
	BeforeCreate(ctx context.Context, serviceType reflect.Type) error
	AfterCreate(ctx context.Context, serviceType reflect.Type, instance interface{}) error
	BeforeDestroy(ctx context.Context, serviceType reflect.Type, instance interface{}) error
	AfterDestroy(ctx context.Context, serviceType reflect.Type) error
}

// ContainerConfig contains configuration for the DI container
type ContainerConfig struct {
	Logger       common.Logger
	Metrics      common.Metrics
	Config       common.ConfigManager
	ErrorHandler common.ErrorHandler
	Interceptors []Interceptor
}

// NewContainer creates a new DI container
func NewContainer(config ContainerConfig) common.Container {
	container := &Container{
		services:              make(map[reflect.Type]*ServiceRegistration),
		namedServices:         make(map[string]*ServiceRegistration),
		instances:             make(map[reflect.Type]interface{}),
		namedInstances:        make(map[string]interface{}),
		referenceNameMappings: make(map[string]string),
		interceptors:          config.Interceptors,
		logger:                config.Logger,
		metrics:               config.Metrics,
		config:                config.Config,
		errorHandler:          config.ErrorHandler,
	}

	// Initialize lifecycle manager
	container.lifecycle = NewLifecycleManager(container)

	// Initialize configurator
	container.configurator = NewConfigurator(container, config.Config)

	// Initialize resolver
	container.resolver = NewResolver(container)

	// Initialize validator
	container.validator = NewValidator(container)

	return container
}

// Register registers a service with the container
func (c *Container) Register(definition common.ServiceDefinition) error {
	c.mu.Lock()

	if c.started {
		c.mu.Unlock()
		return common.ErrContainerError("register", fmt.Errorf("cannot register services after container has started"))
	}

	// Get the service type from the definition
	var serviceType reflect.Type
	if definition.Type != nil {
		serviceType = reflect.TypeOf(definition.Type)
		// If it's a pointer to an interface, get the interface type
		if serviceType.Kind() == reflect.Ptr {
			serviceType = serviceType.Elem()
		}
	} else {
		c.mu.Unlock()
		return common.ErrContainerError("register", fmt.Errorf("service type cannot be nil"))
	}

	// Check if service already exists
	if definition.Name != "" {
		if _, exists := c.namedServices[definition.Name]; exists {
			c.mu.Unlock()
			c.logger.Warn("service already registered, skipping",
				logger.String("name", definition.Name),
				logger.Error(common.ErrServiceAlreadyExists(definition.Name)))

			return nil
		}
	} else {
		if _, exists := c.services[serviceType]; exists {
			c.mu.Unlock()

			c.logger.Warn("service already registered, skipping",
				logger.String("name", serviceType.String()),
				logger.Error(common.ErrServiceAlreadyExists(serviceType.String())))

			return nil
		}
	}

	// Create service registration
	registration := &ServiceRegistration{
		Name:         definition.Name,
		Type:         serviceType,
		Constructor:  definition.Constructor,
		Singleton:    definition.Singleton,
		Lifecycle:    definition.Lifecycle,
		Config:       definition.Config,
		Dependencies: definition.Dependencies,
		Tags:         definition.Tags,
		Created:      time.Now(),
	}

	// NEW: Handle reference names from definition
	if extDefinition, ok := definition.Extensions["referenceNames"]; ok {
		if refNames, ok := extDefinition.([]string); ok {
			registration.ReferenceNames = refNames
		}
	}

	// Create factory function
	if definition.Constructor != nil {
		factory, err := c.createFactory(definition.Constructor)
		if err != nil {
			c.mu.Unlock()
			return common.ErrContainerError("create_factory", err)
		}
		registration.Factory = factory
	} else if definition.Instance != nil {
		registration.Instance = definition.Instance
	}

	// Register service
	if definition.Name != "" {
		c.namedServices[definition.Name] = registration
	} else {
		c.services[serviceType] = registration
	}

	// Register reference name mappings
	actualName := definition.Name
	if actualName == "" {
		actualName = serviceType.String()
	}

	//  Automatically map reflection type name to actual service name if string name is provided
	if definition.Name != "" {
		// Add default mapping from type name to actual service name
		typeName := serviceType.Name()
		if typeName != "" && typeName != definition.Name {
			// Check if mapping already exists
			if existing, exists := c.referenceNameMappings[typeName]; exists && existing != actualName {
				c.mu.Unlock()
				return common.ErrContainerError("reference_mapping",
					fmt.Errorf("type name '%s' already mapped to service '%s', cannot map to '%s'",
						typeName, existing, actualName))
			}
			c.referenceNameMappings[typeName] = actualName
		}

		// Also add the full type string as a mapping if different from type name
		typeString := serviceType.String()
		if typeString != typeName && typeString != definition.Name {
			if existing, exists := c.referenceNameMappings[typeString]; exists && existing != actualName {
				c.mu.Unlock()
				return common.ErrContainerError("reference_mapping",
					fmt.Errorf("type string '%s' already mapped to service '%s', cannot map to '%s'",
						typeString, existing, actualName))
			}
			c.referenceNameMappings[typeString] = actualName
		}
	}

	// Register explicit reference names from definition
	for _, refName := range registration.ReferenceNames {
		if existing, exists := c.referenceNameMappings[refName]; exists && existing != actualName {
			c.mu.Unlock()
			return common.ErrContainerError("reference_mapping",
				fmt.Errorf("reference name '%s' already mapped to service '%s', cannot map to '%s'",
					refName, existing, actualName))
		}
		c.referenceNameMappings[refName] = actualName
	}

	// Record metrics and logging (same as before)
	if c.metrics != nil {
		c.metrics.Counter("forge.di.services_registered").Inc()
		c.metrics.Gauge("forge.di.services_count").Set(float64(len(c.services) + len(c.namedServices)))
	}

	if c.logger != nil {
		serviceName := definition.Name
		if serviceName == "" {
			serviceName = serviceType.String()
		}

		c.logger.Info("service registered",
			logger.String("name", serviceName),
			logger.String("type", serviceType.String()),
			logger.Bool("singleton", definition.Singleton),
			logger.String("reference_names", fmt.Sprintf("%v", registration.ReferenceNames)),
			logger.Int("total_services", len(c.services)+len(c.namedServices)),
		)
	}

	// Auto-registration logic (same as before)
	var needsAutoRegistration bool
	if c.lifecycle != nil {
		serviceInterface := reflect.TypeOf((*common.Service)(nil)).Elem()
		needsAutoRegistration = serviceType.Implements(serviceInterface)
	}

	c.mu.Unlock()

	if needsAutoRegistration {
		instance, err := c.createInstance(context.Background(), registration)
		if err != nil {
			c.mu.Lock()
			if definition.Name != "" {
				delete(c.namedServices, definition.Name)
			} else {
				delete(c.services, serviceType)
			}
			// Clean up all reference mappings for this service
			c.cleanupReferenceMappingsForService(actualName)
			c.mu.Unlock()
			return common.ErrContainerError("create_service_instance", err)
		}

		if service, ok := instance.(common.Service); ok {
			if err := c.lifecycle.AddService(service); err != nil {
				c.mu.Lock()
				if definition.Name != "" {
					delete(c.namedServices, definition.Name)
				} else {
					delete(c.services, serviceType)
				}
				c.cleanupReferenceMappingsForService(actualName)
				c.mu.Unlock()
				return common.ErrContainerError("add_service_to_lifecycle", err)
			}

			if c.logger != nil {
				c.logger.Info("service automatically added to lifecycle manager",
					logger.String("name", service.Name()),
				)
			}
		}
	}

	return nil
}

// Resolve resolves a service from the container
func (c *Container) Resolve(serviceType interface{}) (interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	targetType := reflect.TypeOf(serviceType)
	if targetType.Kind() == reflect.Ptr {
		targetType = targetType.Elem()
	}

	// Strategy 1: Try exact type match in services map (existing behavior)
	if registration, exists := c.services[targetType]; exists {
		return c.createInstance(context.Background(), registration)
	}

	// Strategy 2: Use the existing enhanced resolver logic
	ctx := context.Background()
	instance, err := c.resolver.resolveDependencyWithEnhancedTypeMatching(ctx, targetType)
	if err == nil {
		return instance, nil
	}

	// If all strategies fail, return the original error
	return nil, common.ErrServiceNotFound(targetType.String())
}

// Has checks if a service is registered
func (c *Container) Has(serviceType interface{}) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	targetType := reflect.TypeOf(serviceType)
	if targetType.Kind() == reflect.Ptr {
		targetType = targetType.Elem()
	}

	_, exists := c.services[targetType]
	return exists
}

// HasNamed checks if a named service is registered
func (c *Container) HasNamed(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, exists := c.namedServices[name]
	return exists
}

// Services returns all registered services
func (c *Container) Services() []common.ServiceDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	services := make([]common.ServiceDefinition, 0, len(c.services)+len(c.namedServices))

	for _, registration := range c.services {
		services = append(services, common.ServiceDefinition{
			Name:         registration.Name,
			Type:         registration.Type,
			Constructor:  registration.Constructor,
			Singleton:    registration.Singleton,
			Lifecycle:    registration.Lifecycle,
			Config:       registration.Config,
			Dependencies: registration.Dependencies,
			Tags:         registration.Tags,
		})
	}

	for _, registration := range c.namedServices {
		services = append(services, common.ServiceDefinition{
			Name:         registration.Name,
			Type:         registration.Type,
			Constructor:  registration.Constructor,
			Singleton:    registration.Singleton,
			Lifecycle:    registration.Lifecycle,
			Config:       registration.Config,
			Dependencies: registration.Dependencies,
			Tags:         registration.Tags,
		})
	}

	return services
}

// Start starts all services in dependency order
func (c *Container) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return common.ErrContainerError("start", fmt.Errorf("container already started"))
	}

	startTime := time.Now()

	// Validate dependencies
	if err := c.validator.ValidateAll(); err != nil {
		return common.ErrContainerError("validate", err)
	}

	// Start lifecycle manager
	if err := c.lifecycle.Start(ctx); err != nil {
		return common.ErrContainerError("lifecycle_start", err)
	}

	c.started = true

	if c.logger != nil {
		c.logger.Info("container started",
			logger.Duration("startup_time", time.Since(startTime)),
			logger.Int("services", len(c.services)+len(c.namedServices)),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.di.container_started").Inc()
		c.metrics.Histogram("forge.di.container_startup_time").Observe(time.Since(startTime).Seconds())
	}

	return nil
}

// Stop stops all services in reverse dependency order
func (c *Container) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return common.ErrContainerError("stop", fmt.Errorf("container not started"))
	}

	stopTime := time.Now()

	// Stop lifecycle manager
	if err := c.lifecycle.Stop(ctx); err != nil {
		if c.logger != nil {
			c.logger.Error("failed to stop lifecycle manager", logger.Error(err))
		}
	}

	// Clear instances
	c.instances = make(map[reflect.Type]interface{})
	c.namedInstances = make(map[string]interface{})

	c.started = false

	if c.logger != nil {
		c.logger.Info(
			"container stopped",
			logger.Duration("shutdown_time", time.Since(stopTime)),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.di.container_stopped").Inc()
		c.metrics.Histogram("forge.di.container_shutdown_time").Observe(time.Since(stopTime).Seconds())
	}

	return nil
}

// HealthCheck performs health checks on all services
func (c *Container) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return common.ErrHealthCheckFailed("container", fmt.Errorf("container not started"))
	}

	return c.lifecycle.HealthCheck(ctx)
}

// createInstance creates an instance of a service
func (c *Container) createInstance(ctx context.Context, registration *ServiceRegistration) (interface{}, error) {
	// Check if singleton instance already exists
	if registration.Singleton {

		registration.mu.RLock()
		if registration.Instance != nil {
			instance := registration.Instance
			registration.mu.RUnlock()
			return instance, nil
		}
		registration.mu.RUnlock()

		// Double-check locking pattern
		registration.mu.Lock()
		defer registration.mu.Unlock()
		if registration.Instance != nil {
			return registration.Instance, nil
		}
	}

	// Run before create interceptors
	for _, interceptor := range c.interceptors {
		if err := interceptor.BeforeCreate(ctx, registration.Type); err != nil {
			return nil, common.ErrContainerError("before_create", err)
		}
	}

	for _, interceptor := range registration.Interceptors {
		if err := interceptor.BeforeCreate(ctx, registration.Type); err != nil {
			return nil, common.ErrContainerError("before_create", err)
		}
	}

	// Create instance
	var instance interface{}
	var err error

	if registration.Factory != nil {
		instance, err = registration.Factory(ctx, c)
	} else {
		instance, err = c.resolver.CreateInstance(ctx, registration)
	}

	if err != nil {
		return nil, common.ErrContainerError("create_instance", err)
	}

	// Configure instance
	if registration.Config != nil {
		if err := c.configurator.Configure(ctx, instance, registration.Config); err != nil {
			return nil, common.ErrContainerError("configure", err)
		}
	}

	// Run after create interceptors
	for _, interceptor := range c.interceptors {
		if err := interceptor.AfterCreate(ctx, registration.Type, instance); err != nil {
			return nil, common.ErrContainerError("after_create", err)
		}
	}

	for _, interceptor := range registration.Interceptors {
		if err := interceptor.AfterCreate(ctx, registration.Type, instance); err != nil {
			return nil, common.ErrContainerError("after_create", err)
		}
	}

	// Store singleton instance
	if registration.Singleton {
		registration.Instance = instance
	}

	// Record metrics
	if c.metrics != nil {
		c.metrics.Counter("forge.di.instances_created").Inc()
		if registration.Singleton {
			c.metrics.Counter("forge.di.singletons_created").Inc()
		}
	}

	return instance, nil
}

// createFactory creates a factory function from a constructor
func (c *Container) createFactory(constructor interface{}) (ServiceFactory, error) {
	constructorType := reflect.TypeOf(constructor)
	if constructorType.Kind() != reflect.Func {
		return nil, fmt.Errorf("constructor must be a function")
	}

	constructorValue := reflect.ValueOf(constructor)

	return func(ctx context.Context, container common.Container) (interface{}, error) {
		// Get function parameters
		numParams := constructorType.NumIn()
		params := make([]reflect.Value, numParams)

		for i := 0; i < numParams; i++ {
			paramType := constructorType.In(i)

			// Special handling for context
			if paramType == reflect.TypeOf((*context.Context)(nil)).Elem() {
				params[i] = reflect.ValueOf(ctx)
				continue
			}

			// Special handling for ForgeContext
			if paramType == reflect.TypeOf((*common.ForgeContext)(nil)).Elem() {
				forgeCtx := common.NewForgeContext(ctx, container, c.logger, c.metrics, c.config)
				params[i] = reflect.ValueOf(forgeCtx)
				continue
			}

			// Enhanced dependency resolution with reference name support
			var dependency interface{}
			var err error

			// First try the enhanced type matching (includes reference names)
			dependency, err = c.resolver.resolveDependencyWithEnhancedTypeMatching(ctx, paramType)
			if err != nil {
				// If that fails, try resolving by type name as a reference name
				typeName := paramType.Name()
				if typeName != "" {
					dependency, err = c.resolver.resolveDependencyByName(ctx, typeName)
				}
			}

			if err != nil {
				return nil, fmt.Errorf("failed to resolve dependency %s: %w", paramType.String(), err)
			}

			params[i] = reflect.ValueOf(dependency)
		}

		// Call constructor
		results := constructorValue.Call(params)

		// Check for errors
		if len(results) == 0 {
			return nil, fmt.Errorf("constructor must return at least one value")
		}

		instance := results[0].Interface()

		// Check if last result is an error
		if len(results) > 1 {
			if err, ok := results[len(results)-1].Interface().(error); ok && err != nil {
				return nil, err
			}
		}

		return instance, nil
	}, nil
}

// LifecycleManager returns the lifecycle manager
func (c *Container) LifecycleManager() common.LifecycleManager {
	return c.lifecycle
}

// GetConfigurator returns the configurator
func (c *Container) GetConfigurator() *Configurator {
	return c.configurator
}

// GetResolver returns the resolver
func (c *Container) GetResolver() *Resolver {
	return c.resolver
}

// GetValidator returns the validator
func (c *Container) GetValidator() common.Validator {
	return c.validator
}

// AddInterceptor adds an interceptor to the container
func (c *Container) AddInterceptor(interceptor Interceptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interceptors = append(c.interceptors, interceptor)
}

// RemoveInterceptor removes an interceptor from the container
func (c *Container) RemoveInterceptor(interceptor Interceptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, existing := range c.interceptors {
		if existing == interceptor {
			c.interceptors = append(c.interceptors[:i], c.interceptors[i+1:]...)
			break
		}
	}
}

// GetInterceptors returns all interceptors
func (c *Container) GetInterceptors() []Interceptor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	interceptors := make([]Interceptor, len(c.interceptors))
	copy(interceptors, c.interceptors)
	return interceptors
}

// IsStarted returns true if the container has been started
func (c *Container) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

// GetStats returns statistics about the container
func (c *Container) GetStats() ContainerStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return ContainerStats{
		ServicesRegistered: len(c.services) + len(c.namedServices),
		InstancesCreated:   len(c.instances) + len(c.namedInstances),
		Started:            c.started,
		Interceptors:       len(c.interceptors),
		ReferenceMappings:  len(c.referenceNameMappings),
	}
}

// NamedServices returns statistics about the container
func (c *Container) NamedServices() map[string]*ServiceRegistration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.namedServices
}

// AddReferenceMapping adds a reference name mapping for an existing service
func (c *Container) AddReferenceMapping(referenceName, actualServiceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return common.ErrContainerError("add_reference_mapping",
			fmt.Errorf("cannot add reference mappings after container has started"))
	}

	// Check if reference name already exists
	if _, exists := c.referenceNameMappings[referenceName]; exists {
		return common.ErrContainerError("add_reference_mapping",
			fmt.Errorf("reference name '%s' already mapped", referenceName))
	}

	// Check if actual service exists
	if _, exists := c.namedServices[actualServiceName]; !exists {
		// Check if it's a type-based service
		found := false
		for _, registration := range c.services {
			if registration.Type.String() == actualServiceName {
				found = true
				break
			}
		}
		if !found {
			return common.ErrServiceNotFound(actualServiceName)
		}
	}

	c.referenceNameMappings[referenceName] = actualServiceName

	if c.logger != nil {
		c.logger.Info("reference mapping added",
			logger.String("reference_name", referenceName),
			logger.String("actual_service", actualServiceName),
		)
	}

	return nil
}

// RemoveReferenceMapping removes a reference name mapping
func (c *Container) RemoveReferenceMapping(referenceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return common.ErrContainerError("remove_reference_mapping",
			fmt.Errorf("cannot remove reference mappings after container has started"))
	}

	if _, exists := c.referenceNameMappings[referenceName]; !exists {
		return common.ErrContainerError("remove_reference_mapping",
			fmt.Errorf("reference name '%s' not found", referenceName))
	}

	delete(c.referenceNameMappings, referenceName)

	if c.logger != nil {
		c.logger.Info("reference mapping removed",
			logger.String("reference_name", referenceName),
		)
	}

	return nil
}

// GetReferenceMappings returns all reference name mappings
func (c *Container) GetReferenceMappings() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	mappings := make(map[string]string, len(c.referenceNameMappings))
	for refName, actualName := range c.referenceNameMappings {
		mappings[refName] = actualName
	}
	return mappings
}

// ResolveByReference resolves a service using a reference name
func (c *Container) ResolveByReference(referenceName string) (interface{}, error) {
	c.mu.RLock()
	actualName, exists := c.referenceNameMappings[referenceName]
	c.mu.RUnlock()

	if !exists {
		return nil, common.ErrServiceNotFound(fmt.Sprintf("reference '%s'", referenceName))
	}

	return c.ResolveNamed(actualName)
}

// ResolveNamed resolves a named service from the container (MODIFIED to support reference names)
func (c *Container) ResolveNamed(name string) (interface{}, error) {
	c.mu.RLock()

	// First try direct name resolution
	registration, exists := c.namedServices[name]
	if !exists {
		// Try reference name mapping
		if actualName, mapped := c.referenceNameMappings[name]; mapped {
			if actualRegistration, actualExists := c.namedServices[actualName]; actualExists {
				registration = actualRegistration
				exists = true
			}
		}
	}

	c.mu.RUnlock()

	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return c.createInstance(context.Background(), registration)
}

// Enhanced dependency resolution with reference name support
func (r *Resolver) resolveDependencyByName(ctx context.Context, dependencyName string) (interface{}, error) {
	r.container.mu.RLock()
	defer r.container.mu.RUnlock()

	// Strategy 1: Try direct named service resolution
	if registration, exists := r.container.namedServices[dependencyName]; exists {
		return r.CreateInstance(ctx, registration)
	}

	// Strategy 2: Try reference name mapping
	if actualName, mapped := r.container.referenceNameMappings[dependencyName]; mapped {
		if registration, exists := r.container.namedServices[actualName]; exists {
			return r.CreateInstance(ctx, registration)
		}
	}

	// Strategy 3: Try type-based resolution as fallback
	for serviceType, registration := range r.container.services {
		if serviceType.Name() == dependencyName || serviceType.String() == dependencyName {
			return r.CreateInstance(ctx, registration)
		}
	}

	return nil, common.ErrServiceNotFound(dependencyName)
}

func (c *Container) cleanupReferenceMappingsForService(serviceName string) {
	// Find and remove all mappings that point to this service
	toRemove := make([]string, 0)
	for refName, actualName := range c.referenceNameMappings {
		if actualName == serviceName {
			toRemove = append(toRemove, refName)
		}
	}

	for _, refName := range toRemove {
		delete(c.referenceNameMappings, refName)
	}
}

// ContainerStats contains statistics about the container
type ContainerStats struct {
	ServicesRegistered int  `json:"services_registered"`
	InstancesCreated   int  `json:"instances_created"`
	Started            bool `json:"started"`
	Interceptors       int  `json:"interceptors"`
	ReferenceMappings  int  `json:"reference_mappings"`
}
