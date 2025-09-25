package di

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Resolver handles dependency resolution with simplified exact type matching
type Resolver struct {
	container *Container
	cache     map[string]interface{}
	mu        sync.RWMutex
}

// NewResolver creates a new resolver
func NewResolver(container *Container) *Resolver {
	return &Resolver{
		container: container,
		cache:     make(map[string]interface{}),
	}
}

// CreateInstance creates an instance of a service using exact type resolution
func (r *Resolver) CreateInstance(ctx context.Context, registration *ServiceRegistration) (interface{}, error) {
	// Check if instance already exists
	if registration.Instance != nil {
		return registration.Instance, nil
	}

	// Check cache for singleton
	if registration.Singleton {
		cacheKey := registration.Type.String()
		r.mu.RLock()
		if instance, exists := r.cache[cacheKey]; exists {
			r.mu.RUnlock()
			return instance, nil
		}
		r.mu.RUnlock()
	}

	// Create new instance
	instance, err := r.createNewInstance(ctx, registration)
	if err != nil {
		return nil, err
	}

	// Cache singleton
	if registration.Singleton {
		cacheKey := registration.Type.String()
		r.mu.Lock()
		r.cache[cacheKey] = instance
		r.mu.Unlock()
	}

	return instance, nil
}

// createNewInstance creates a new instance using constructor injection
func (r *Resolver) createNewInstance(ctx context.Context, registration *ServiceRegistration) (interface{}, error) {
	if registration.Constructor == nil {
		return nil, common.ErrInvalidConfig("constructor", fmt.Errorf("no constructor provided for service %s", registration.Name))
	}

	constructorType := reflect.TypeOf(registration.Constructor)
	if constructorType.Kind() != reflect.Func {
		return nil, common.ErrInvalidConfig("constructor", fmt.Errorf("constructor must be a function"))
	}

	constructorValue := reflect.ValueOf(registration.Constructor)

	// Resolve dependencies
	numParams := constructorType.NumIn()
	params := make([]reflect.Value, numParams)

	for i := 0; i < numParams; i++ {
		paramType := constructorType.In(i)

		// Special handling for context types
		if paramType == reflect.TypeOf((*context.Context)(nil)).Elem() {
			params[i] = reflect.ValueOf(ctx)
			continue
		}

		if paramType == reflect.TypeOf((*common.ForgeContext)(nil)).Elem() {
			forgeCtx := common.NewForgeContext(ctx, r.container, r.container.logger, r.container.metrics, r.container.config)
			params[i] = reflect.ValueOf(forgeCtx)
			continue
		}

		// Resolve dependency using exact type matching
		dependency, err := r.resolveDependencyExact(ctx, paramType)
		if err != nil {
			return nil, common.ErrDependencyNotFound(registration.Name, paramType.String()).WithCause(err)
		}

		params[i] = reflect.ValueOf(dependency)
	}

	// Call constructor
	results := constructorValue.Call(params)

	// Handle results
	if len(results) == 0 {
		return nil, common.ErrInvalidConfig("constructor", fmt.Errorf("constructor must return at least one value"))
	}

	instance := results[0].Interface()

	// Check for error return
	if len(results) > 1 {
		if err, ok := results[len(results)-1].Interface().(error); ok && err != nil {
			return nil, common.ErrServiceStartFailed(registration.Name, err)
		}
	}

	return instance, nil
}

// resolveDependencyExact resolves a dependency using exact type matching
func (r *Resolver) resolveDependencyExact(ctx context.Context, paramType reflect.Type) (interface{}, error) {
	if r.container.logger != nil {
		r.container.logger.Debug("resolving constructor dependency",
			logger.String("param_type", paramType.String()),
			logger.String("param_kind", paramType.Kind().String()),
		)
	}

	// Strategy 1: Try exact type match in services map
	r.container.mu.RLock()
	if registration, exists := r.container.services[paramType]; exists {
		// FIXED: Added RUnlock to prevent deadlock.
		r.container.mu.RUnlock()
		if r.container.logger != nil {
			r.container.logger.Debug("found exact type match in services map")
		}
		return r.CreateInstance(ctx, registration)
	}

	// Strategy 2: Try exact type match in named services
	for name, registration := range r.container.namedServices {
		if registration.Type == paramType {
			// FIXED: Added RUnlock to prevent deadlock.
			r.container.mu.RUnlock()
			if r.container.logger != nil {
				r.container.logger.Debug("found exact type match in named services",
					logger.String("service_name", name))
			}
			return r.CreateInstance(ctx, registration)
		}
	}

	// Strategy 3: Interface matching (only if param is interface)
	if paramType.Kind() == reflect.Interface {
		if r.container.logger != nil {
			r.container.logger.Debug("param is interface, checking implementations")
		}

		// Check services map for implementations
		for serviceType, registration := range r.container.services {
			if r.implementsInterface(serviceType, paramType) {
				// FIXED: Added RUnlock to prevent deadlock.
				r.container.mu.RUnlock()
				if r.container.logger != nil {
					r.container.logger.Debug("found interface implementation in services map",
						logger.String("impl_type", serviceType.String()))
				}
				return r.CreateInstance(ctx, registration)
			}
		}

		// Check named services for implementations
		for name, registration := range r.container.namedServices {
			if r.implementsInterface(registration.Type, paramType) {
				// FIXED: Added RUnlock to prevent deadlock.
				r.container.mu.RUnlock()
				if r.container.logger != nil {
					r.container.logger.Debug("found interface implementation in named services",
						logger.String("service_name", name),
						logger.String("impl_type", registration.Type.String()))
				}
				return r.CreateInstance(ctx, registration)
			}
		}
	}

	// Strategy 4: Try reference name resolution as fallback
	referenceMappings := r.container.referenceNameMappings
	r.container.mu.RUnlock()

	// Try the type name as a reference
	typeName := paramType.Name()
	if typeName != "" {
		if actualName, mapped := referenceMappings[typeName]; mapped {
			if r.container.logger != nil {
				r.container.logger.Debug("found reference mapping for type name",
					logger.String("type_name", typeName),
					logger.String("actual_service", actualName))
			}
			return r.container.ResolveNamed(actualName)
		}
	}

	// Try the full type string as a reference
	typeString := paramType.String()
	if actualName, mapped := referenceMappings[typeString]; mapped {
		if r.container.logger != nil {
			r.container.logger.Debug("found reference mapping for type string",
				logger.String("type_string", typeString),
				logger.String("actual_service", actualName))
		}
		return r.container.ResolveNamed(actualName)
	}

	if r.container.logger != nil {
		r.container.logger.Error("failed to resolve dependency",
			logger.String("param_type", paramType.String()))
	}

	return nil, common.ErrServiceNotFound(paramType.String())
}

// implementsInterface checks if a type implements an interface
func (r *Resolver) implementsInterface(implType, interfaceType reflect.Type) bool {
	// Direct implementation check
	if implType.Implements(interfaceType) {
		return true
	}

	// Check if pointer to type implements the interface
	if reflect.PtrTo(implType).Implements(interfaceType) {
		return true
	}

	return false
}

// ClearCache clears the resolver cache
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]interface{})
}

// GetCacheStats returns cache statistics
func (r *Resolver) GetCacheStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]interface{}{
		"cached_instances": len(r.cache),
		"cache_keys":       r.getCacheKeys(),
	}
}

// getCacheKeys returns all cache keys (for debugging)
func (r *Resolver) getCacheKeys() []string {
	keys := make([]string, 0, len(r.cache))
	for key := range r.cache {
		keys = append(keys, key)
	}
	return keys
}

// Configurator handles service configuration
type Configurator struct {
	container *Container
	config    common.ConfigManager
}

// NewConfigurator creates a new configurator
func NewConfigurator(container *Container, config common.ConfigManager) *Configurator {
	return &Configurator{
		container: container,
		config:    config,
	}
}

// Configure configures a service instance
func (c *Configurator) Configure(ctx context.Context, instance interface{}, configData interface{}) error {
	if configData == nil {
		return nil
	}

	// Use reflection to set configuration
	instanceValue := reflect.ValueOf(instance)
	if instanceValue.Kind() == reflect.Ptr {
		instanceValue = instanceValue.Elem()
	}

	configValue := reflect.ValueOf(configData)
	if configValue.Kind() == reflect.Ptr {
		configValue = configValue.Elem()
	}

	// Check if instance has a Configure method
	if method := instanceValue.MethodByName("Configure"); method.IsValid() {
		results := method.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(configData)})
		if len(results) > 0 {
			if err, ok := results[0].Interface().(error); ok && err != nil {
				return err
			}
		}
		return nil
	}

	// Check if instance has a SetConfig method
	if method := instanceValue.MethodByName("SetConfig"); method.IsValid() {
		method.Call([]reflect.Value{reflect.ValueOf(configData)})
		return nil
	}

	// Try to match fields by name
	return c.configureByFields(instanceValue, configValue)
}

// configureByFields configures instance by matching fields
func (c *Configurator) configureByFields(instanceValue, configValue reflect.Value) error {
	instanceType := instanceValue.Type()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		configField := configType.Field(i)
		configFieldValue := configValue.Field(i)

		// Find matching field in instance
		if _, found := instanceType.FieldByName(configField.Name); found {
			instanceFieldValue := instanceValue.FieldByName(configField.Name)

			if instanceFieldValue.CanSet() && instanceFieldValue.Type() == configFieldValue.Type() {
				instanceFieldValue.Set(configFieldValue)
			}
		}
	}

	return nil
}

// BindConfiguration binds configuration from config manager
func (c *Configurator) BindConfiguration(ctx context.Context, instance interface{}, configKey string) error {
	if c.config == nil {
		return nil
	}

	// Try to bind configuration
	if err := c.config.Bind(configKey, instance); err != nil {
		return common.ErrConfigError(fmt.Sprintf("failed to bind configuration for key '%s'", configKey), err)
	}

	return nil
}
