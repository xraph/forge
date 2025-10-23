package di

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Container DIContainer implements the dependency injection container
type Container struct {
	services              map[reflect.Type]*ServiceRegistration
	namedServices         map[string]*ServiceRegistration
	instances             map[reflect.Type]interface{}
	namedInstances        map[string]interface{}
	referenceNameMappings map[string]string
	dependencyGraph       *DependencyGraph
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

	collisionHandlingStrategy CollisionStrategy
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
	Logger                    common.Logger
	Metrics                   common.Metrics
	Config                    common.ConfigManager
	ErrorHandler              common.ErrorHandler
	Interceptors              []Interceptor
	CollisionHandlingStrategy CollisionStrategy
}

type CollisionStrategy string

const (
	CollisionStrategyFail            CollisionStrategy = "fail"
	CollisionStrategySkip            CollisionStrategy = "skip"
	CollisionStrategyUseFullTypeName CollisionStrategy = "full"
	CollisionStrategyOverwrite       CollisionStrategy = "overwrite"
)

// NewContainer creates a new DI container
func NewContainer(config ContainerConfig) common.Container {
	container := &Container{
		services:                  make(map[reflect.Type]*ServiceRegistration),
		namedServices:             make(map[string]*ServiceRegistration),
		instances:                 make(map[reflect.Type]interface{}),
		namedInstances:            make(map[string]interface{}),
		referenceNameMappings:     make(map[string]string),
		dependencyGraph:           NewDependencyGraph(),
		interceptors:              config.Interceptors,
		logger:                    config.Logger,
		metrics:                   config.Metrics,
		config:                    config.Config,
		errorHandler:              config.ErrorHandler,
		collisionHandlingStrategy: config.CollisionHandlingStrategy,
	}

	container.lifecycle = NewLifecycleManager(container)
	container.configurator = NewConfigurator(container, config.Config)
	container.resolver = NewResolver(container)
	container.validator = NewValidator(container)

	return container
}

// Register registers a service with the container
func (c *Container) Register(definition common.ServiceDefinition) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return common.ErrContainerError("register", fmt.Errorf("cannot register services after container has started"))
	}

	var serviceType reflect.Type
	if definition.Type != nil {
		serviceType = reflect.TypeOf(definition.Type)
		if serviceType.Kind() == reflect.Ptr && serviceType.Elem().Kind() == reflect.Interface {
			serviceType = serviceType.Elem()
		}
	} else {
		return common.ErrContainerError("register", fmt.Errorf("service type cannot be nil for registration"))
	}

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

	// Validate constructor and extract dependencies
	if definition.Constructor != nil {
		factory, constructorDeps, err := c.createFactoryWithDependencies(definition.Constructor)
		if err != nil {
			return common.ErrContainerError("create_factory", err)
		}
		registration.Factory = factory
		// Merge explicit dependencies with constructor dependencies
		registration.Dependencies = c.mergeDependencies(registration.Dependencies, constructorDeps)
	} else if definition.Instance != nil {
		registration.Instance = definition.Instance
	}

	// Register service
	if definition.Name != "" {
		if _, exists := c.namedServices[definition.Name]; exists {
			return common.ErrServiceAlreadyExists(definition.Name)
		}
		c.namedServices[definition.Name] = registration
		c.generateSimpleReferenceNames(definition.Name, serviceType)
	} else {
		if _, exists := c.services[serviceType]; exists {
			return common.ErrServiceAlreadyExists(serviceType.String())
		}
		c.services[serviceType] = registration
	}

	// Add to dependency graph for ordering
	serviceName := c.getServiceName(registration)
	if err := c.dependencyGraph.AddNode(serviceName, registration.Dependencies); err != nil {
		// Rollback registration on dependency graph error
		if definition.Name != "" {
			delete(c.namedServices, definition.Name)
			c.cleanupReferenceMappingsForService(definition.Name)
		} else {
			delete(c.services, serviceType)
		}
		return common.ErrContainerError("dependency_graph", err)
	}

	if c.logger != nil {
		c.logger.Debug("service registered",
			logger.String("service_name", serviceName),
			logger.String("service_type", serviceType.String()),
			logger.String("dependencies", fmt.Sprintf("%v", registration.Dependencies)),
		)
	}

	return nil
}

// createFactoryWithDependencies creates a factory and extracts constructor dependencies
func (c *Container) createFactoryWithDependencies(constructor interface{}) (ServiceFactory, []string, error) {
	constructorType := reflect.TypeOf(constructor)
	if constructorType.Kind() != reflect.Func {
		return nil, nil, fmt.Errorf("constructor must be a function")
	}

	constructorValue := reflect.ValueOf(constructor)
	var dependencies []string

	// Extract dependencies from constructor parameters
	numParams := constructorType.NumIn()
	for i := 0; i < numParams; i++ {
		paramType := constructorType.In(i)

		// Skip context types as they're handled specially
		if paramType == reflect.TypeOf((*context.Context)(nil)).Elem() {
			continue
		}
		if paramType == reflect.TypeOf((*common.ForgeContext)(nil)).Elem() {
			continue
		}

		// Find service name for this parameter type
		depName := c.findDependencyName(paramType)
		if depName != "" {
			dependencies = append(dependencies, depName)
		}
	}

	factory := func(ctx context.Context, container common.Container) (interface{}, error) {
		params := make([]reflect.Value, numParams)

		for i := 0; i < numParams; i++ {
			paramType := constructorType.In(i)

			// Handle special context types
			if paramType == reflect.TypeOf((*context.Context)(nil)).Elem() {
				params[i] = reflect.ValueOf(ctx)
				continue
			}

			if paramType == reflect.TypeOf((*common.ForgeContext)(nil)).Elem() {
				forgeCtx := common.NewForgeContext(ctx, c, c.logger, c.metrics, c.config)
				params[i] = reflect.ValueOf(forgeCtx)
				continue
			}

			// Resolve dependency
			dependency, err := c.resolveDependency(paramType)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve dependency %s for constructor: %w", paramType.String(), err)
			}

			params[i] = reflect.ValueOf(dependency)
		}

		results := constructorValue.Call(params)
		if len(results) == 0 {
			return nil, fmt.Errorf("constructor must return at least one value")
		}

		instance := results[0].Interface()
		if len(results) > 1 {
			if err, ok := results[len(results)-1].Interface().(error); ok && err != nil {
				return nil, err
			}
		}

		return instance, nil
	}

	return factory, dependencies, nil
}

// findDependencyName finds the service name for a given type
func (c *Container) findDependencyName(paramType reflect.Type) string {
	// First check if there's a named service with this exact type
	for name, reg := range c.namedServices {
		if c.typesMatch(reg.Type, paramType) {
			return name
		}
	}

	// Check if there's a service registered by exact type
	for serviceType := range c.services {
		if c.typesMatch(serviceType, paramType) {
			return serviceType.String()
		}
	}

	// For interfaces, check if any registered service implements it
	if paramType.Kind() == reflect.Interface {
		// Check named services
		for name, reg := range c.namedServices {
			if c.implementsInterface(reg.Type, paramType) {
				return name
			}
		}

		// Check type-registered services
		for serviceType := range c.services {
			if c.implementsInterface(serviceType, paramType) {
				return serviceType.String()
			}
		}
	}

	return ""
}

// typesMatch checks if two types are compatible
func (c *Container) typesMatch(type1, type2 reflect.Type) bool {
	// Direct match
	if type1 == type2 {
		return true
	}

	// Interface implementation check
	if type2.Kind() == reflect.Interface {
		if type1.Implements(type2) {
			return true
		}
		if reflect.PtrTo(type1).Implements(type2) {
			return true
		}
	}

	return false
}

// implementsInterface checks if a type implements an interface
func (c *Container) implementsInterface(implType, interfaceType reflect.Type) bool {
	if implType.Implements(interfaceType) {
		return true
	}
	if reflect.PtrTo(implType).Implements(interfaceType) {
		return true
	}
	return false
}

// mergeDependencies combines explicit and inferred dependencies
func (c *Container) mergeDependencies(explicit, inferred []string) []string {
	seen := make(map[string]bool)
	var result []string

	// Add explicit dependencies first
	for _, dep := range explicit {
		if !seen[dep] {
			result = append(result, dep)
			seen[dep] = true
		}
	}

	// Add inferred dependencies
	for _, dep := range inferred {
		if !seen[dep] {
			result = append(result, dep)
			seen[dep] = true
		}
	}

	return result
}

// getServiceName returns the service name for a registration
func (c *Container) getServiceName(registration *ServiceRegistration) string {
	if registration.Name != "" {
		return registration.Name
	}
	return registration.Type.String()
}

// generateSimpleReferenceNames generates reference names for a service
func (c *Container) generateSimpleReferenceNames(serviceName string, serviceType reflect.Type) {
	var referenceNames []string

	if serviceType.Name() != "" {
		referenceNames = append(referenceNames, serviceType.Name())
	}

	referenceNames = append(referenceNames, serviceType.String())

	if serviceType.PkgPath() != "" && serviceType.Name() != "" {
		pathParts := strings.Split(serviceType.PkgPath(), "/")
		if len(pathParts) > 0 {
			packageName := pathParts[len(pathParts)-1]
			packageRef := packageName + "." + serviceType.Name()
			referenceNames = append(referenceNames, packageRef)
		}
	}

	for _, refName := range referenceNames {
		if refName == serviceName {
			continue
		}

		if existing, exists := c.referenceNameMappings[refName]; !exists {
			c.referenceNameMappings[refName] = serviceName
		} else if existing != serviceName && c.logger != nil {
			c.logger.Debug("skipping reference name due to conflict",
				logger.String("reference_name", refName),
				logger.String("existing_service", existing),
				logger.String("new_service", serviceName),
			)
		}
	}
}

// Resolve resolves a service by its type
func (c *Container) Resolve(serviceType interface{}) (interface{}, error) {
	if serviceType == nil {
		return nil, common.ErrContainerError("resolve", fmt.Errorf("cannot resolve a nil type"))
	}

	targetType := reflect.TypeOf(serviceType)
	if targetType.Kind() == reflect.Ptr && targetType.Elem().Kind() == reflect.Interface {
		targetType = targetType.Elem()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.resolveDependency(targetType)
}

// resolveDependency resolves a dependency by type with better error handling
func (c *Container) resolveDependency(paramType reflect.Type) (interface{}, error) {
	// Strategy 1: Try exact type match in services map
	if registration, exists := c.services[paramType]; exists {
		return c.createInstance(context.Background(), registration)
	}

	// Strategy 2: Try exact type match in named services
	for _, registration := range c.namedServices {
		if registration.Type == paramType {
			return c.createInstance(context.Background(), registration)
		}
	}

	// Strategy 3: Interface matching (only if param is interface)
	if paramType.Kind() == reflect.Interface {
		// Check services map for implementations
		for serviceType, registration := range c.services {
			if c.implementsInterface(serviceType, paramType) {
				return c.createInstance(context.Background(), registration)
			}
		}

		// Check named services for implementations
		for _, registration := range c.namedServices {
			if c.implementsInterface(registration.Type, paramType) {
				return c.createInstance(context.Background(), registration)
			}
		}
	}

	// Strategy 4: Try reference name resolution as fallback
	typeName := paramType.Name()
	if typeName != "" {
		if actualName, mapped := c.referenceNameMappings[typeName]; mapped {
			if registration, exists := c.namedServices[actualName]; exists {
				return c.createInstance(context.Background(), registration)
			}
		}
	}

	// Try the full type string as a reference
	typeString := paramType.String()
	if actualName, mapped := c.referenceNameMappings[typeString]; mapped {
		if registration, exists := c.namedServices[actualName]; exists {
			return c.createInstance(context.Background(), registration)
		}
	}

	return nil, common.ErrServiceNotFound(paramType.String())
}

// ResolveNamed resolves a service by its name or a registered reference name
func (c *Container) ResolveNamed(name string) (interface{}, error) {
	c.mu.RLock()
	registration, exists := c.namedServices[name]
	if !exists {
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

// Has checks if a service is registered by type
func (c *Container) Has(serviceType interface{}) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if serviceType == nil {
		return false
	}

	targetType := reflect.TypeOf(serviceType)
	if targetType.Kind() == reflect.Ptr && targetType.Elem().Kind() == reflect.Interface {
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

// Services returns all registered service definitions
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

// Start validates and starts all services in dependency order
func (c *Container) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return common.ErrContainerError("start", fmt.Errorf("container already started"))
	}

	startTime := time.Now()

	// Validate all dependencies exist
	if err := c.validateAllDependencies(); err != nil {
		return common.ErrContainerError("validate_dependencies", err)
	}

	// Validate for circular dependencies
	if err := c.validator.ValidateAll(); err != nil {
		return common.ErrContainerError("validate", err)
	}

	// Pre-create instances in dependency order to avoid resolution issues
	if err := c.initializeServicesInOrder(ctx); err != nil {
		return common.ErrContainerError("initialize_services", err)
	}

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

// validateAllDependencies ensures all declared dependencies exist
func (c *Container) validateAllDependencies() error {
	allServices := make(map[string]bool)

	// Collect all service names
	for _, registration := range c.services {
		serviceName := c.getServiceName(registration)
		allServices[serviceName] = true
	}

	for name := range c.namedServices {
		allServices[name] = true
	}

	// Check that all dependencies exist
	for _, registration := range c.services {
		serviceName := c.getServiceName(registration)
		for _, dep := range registration.Dependencies {
			if !allServices[dep] && !c.isSpecialDependency(dep) {
				return common.ErrDependencyNotFound(serviceName, dep)
			}
		}
	}

	for _, registration := range c.namedServices {
		for _, dep := range registration.Dependencies {
			if !allServices[dep] && !c.isSpecialDependency(dep) {
				return common.ErrDependencyNotFound(registration.Name, dep)
			}
		}
	}

	return nil
}

// isSpecialDependency checks if a dependency is a special system dependency
func (c *Container) isSpecialDependency(dep string) bool {
	specialDeps := map[string]bool{
		"context.Context":      true,
		"common.ForgeContext":  true,
		"common.Logger":        true,
		"common.Metrics":       true,
		"common.ConfigManager": true,
	}
	return specialDeps[dep]
}

// initializeServicesInOrder creates singleton instances in dependency order
func (c *Container) initializeServicesInOrder(ctx context.Context) error {
	// Get topological order from dependency graph
	orderedServices := c.dependencyGraph.GetTopologicalOrder()

	if c.logger != nil {
		c.logger.Debug("initializing services in order",
			logger.String("order", fmt.Sprintf("%v", orderedServices)),
		)
	}

	for _, serviceName := range orderedServices {
		var registration *ServiceRegistration
		var found bool

		// Find the registration
		if reg, exists := c.namedServices[serviceName]; exists {
			registration = reg
			found = true
		} else {
			// Try to find by type name
			for _, reg := range c.services {
				if c.getServiceName(reg) == serviceName {
					registration = reg
					found = true
					break
				}
			}
		}

		if !found {
			continue
		}

		// Pre-create singleton instances
		if registration.Singleton && registration.Instance == nil {
			if c.logger != nil {
				c.logger.Debug("pre-initializing singleton service",
					logger.String("service", serviceName),
				)
			}

			instance, err := c.createInstance(ctx, registration)
			if err != nil {
				return fmt.Errorf("failed to initialize service %s: %w", serviceName, err)
			}

			registration.Instance = instance
		}
	}

	return nil
}

// Stop stops all services
func (c *Container) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return common.ErrContainerError("stop", fmt.Errorf("container not started"))
	}

	stopTime := time.Now()

	if err := c.lifecycle.Stop(ctx); err != nil {
		if c.logger != nil {
			c.logger.Error("failed to stop lifecycle manager", logger.Error(err))
		}
	}

	// Clear instances
	c.instances = make(map[reflect.Type]interface{})
	c.namedInstances = make(map[string]interface{})

	// Clear singleton instances from registrations
	for _, registration := range c.services {
		registration.Instance = nil
	}
	for _, registration := range c.namedServices {
		registration.Instance = nil
	}

	c.started = false

	if c.logger != nil {
		c.logger.Info("container stopped",
			logger.Duration("shutdown_time", time.Since(stopTime)),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.di.container_stopped").Inc()
		c.metrics.Histogram("forge.di.container_shutdown_time").Observe(time.Since(stopTime).Seconds())
	}

	return nil
}

// HealthCheck performs health checks on all managed services
func (c *Container) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return common.ErrHealthCheckFailed("container", fmt.Errorf("container not started"))
	}

	return c.lifecycle.HealthCheck(ctx)
}

// createInstance handles the creation of a single service instance
func (c *Container) createInstance(ctx context.Context, registration *ServiceRegistration) (interface{}, error) {
	if registration.Singleton {
		registration.mu.RLock()
		if registration.Instance != nil {
			instance := registration.Instance
			registration.mu.RUnlock()
			return instance, nil
		}
		registration.mu.RUnlock()

		registration.mu.Lock()
		defer registration.mu.Unlock()
		if registration.Instance != nil {
			return registration.Instance, nil
		}
	}

	// Run interceptors
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

	// Run after-create interceptors
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

	if registration.Singleton {
		registration.Instance = instance
	}

	return instance, nil
}

// Utility methods
func (c *Container) LifecycleManager() common.LifecycleManager { return c.lifecycle }
func (c *Container) GetConfigurator() *Configurator            { return c.configurator }
func (c *Container) GetResolver() *Resolver                    { return c.resolver }
func (c *Container) GetValidator() common.Validator            { return c.validator }

func (c *Container) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

func (c *Container) AddInterceptor(interceptor Interceptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interceptors = append(c.interceptors, interceptor)
}

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

func (c *Container) GetInterceptors() []Interceptor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	interceptors := make([]Interceptor, len(c.interceptors))
	copy(interceptors, c.interceptors)
	return interceptors
}

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

func (c *Container) NamedServices() map[string]*ServiceRegistration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]*ServiceRegistration)
	for k, v := range c.namedServices {
		result[k] = v
	}
	return result
}

func (c *Container) AddReferenceMapping(referenceName, actualServiceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return common.ErrContainerError("add_reference_mapping", fmt.Errorf("cannot add mappings after start"))
	}

	if _, exists := c.referenceNameMappings[referenceName]; exists {
		return common.ErrContainerError("add_reference_mapping", fmt.Errorf("reference name '%s' already mapped", referenceName))
	}

	if _, exists := c.namedServices[actualServiceName]; !exists {
		found := false
		for _, reg := range c.services {
			if reg.Type.String() == actualServiceName {
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
			logger.String("reference", referenceName),
			logger.String("actual", actualServiceName))
	}

	return nil
}

func (c *Container) RemoveReferenceMapping(referenceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return common.ErrContainerError("remove_reference_mapping", fmt.Errorf("cannot remove mappings after start"))
	}

	if _, exists := c.referenceNameMappings[referenceName]; !exists {
		return common.ErrContainerError("remove_reference_mapping", fmt.Errorf("reference name '%s' not found", referenceName))
	}

	delete(c.referenceNameMappings, referenceName)

	if c.logger != nil {
		c.logger.Info("reference mapping removed", logger.String("reference", referenceName))
	}

	return nil
}

func (c *Container) GetReferenceMappings() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	mappings := make(map[string]string, len(c.referenceNameMappings))
	for k, v := range c.referenceNameMappings {
		mappings[k] = v
	}
	return mappings
}

func (c *Container) ResolveByReference(referenceName string) (interface{}, error) {
	c.mu.RLock()
	actualName, exists := c.referenceNameMappings[referenceName]
	c.mu.RUnlock()

	if !exists {
		return nil, common.ErrServiceNotFound(fmt.Sprintf("reference '%s'", referenceName))
	}

	return c.ResolveNamed(actualName)
}

func (c *Container) cleanupReferenceMappingsForService(serviceName string) {
	toRemove := make([]string, 0)
	for ref, actual := range c.referenceNameMappings {
		if actual == serviceName {
			toRemove = append(toRemove, ref)
		}
	}
	for _, ref := range toRemove {
		delete(c.referenceNameMappings, ref)
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
