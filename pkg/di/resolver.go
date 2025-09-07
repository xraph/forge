package di

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/xraph/forge/pkg/common"
)

// Resolver handles dependency resolution with enhanced type alias support
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

// CreateInstance creates an instance of a service
func (r *Resolver) CreateInstance(ctx context.Context, registration *ServiceRegistration) (interface{}, error) {
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

// createNewInstance creates a new instance using constructor
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

		// Resolve dependency with enhanced type matching
		dependency, err := r.resolveDependencyWithEnhancedTypeMatching(ctx, paramType)
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

// resolveDependencyWithEnhancedTypeMatching resolves a dependency with comprehensive type matching
func (r *Resolver) resolveDependencyWithEnhancedTypeMatching(ctx context.Context, paramType reflect.Type) (interface{}, error) {
	// Strategy 1: Try exact type match first (most reliable)
	if dependency, err := r.tryExactTypeMatch(ctx, paramType); err == nil {
		return dependency, nil
	}

	// Strategy 2: Try type alias matching
	if dependency, err := r.tryTypeAliasMatch(ctx, paramType); err == nil {
		return dependency, nil
	}

	// Strategy 3: Try interface compatibility
	if paramType.Kind() == reflect.Interface {
		if dependency, err := r.tryInterfaceMatch(ctx, paramType); err == nil {
			return dependency, nil
		}
	}

	// Strategy 4: Try named service resolution (fallback)
	if dependency, err := r.tryNamedServiceResolution(ctx, paramType); err == nil {
		return dependency, nil
	}

	return nil, common.ErrServiceNotFound(paramType.String())
}

// tryExactTypeMatch attempts to find an exact type match
func (r *Resolver) tryExactTypeMatch(ctx context.Context, paramType reflect.Type) (interface{}, error) {
	// Check registered services by exact type
	if registration, exists := r.container.services[paramType]; exists {
		return r.CreateInstance(ctx, registration)
	}

	// Check named services for exact type match
	for _, registration := range r.container.namedServices {
		if registration.Type == paramType {
			return r.CreateInstance(ctx, registration)
		}
	}

	return nil, common.ErrServiceNotFound(paramType.String())
}

// tryTypeAliasMatch attempts to match type aliases
func (r *Resolver) tryTypeAliasMatch(ctx context.Context, paramType reflect.Type) (interface{}, error) {
	// Check registered services with enhanced type alias matching
	for registeredType, registration := range r.container.services {
		if r.isTypeAlias(paramType, registeredType) {
			return r.CreateInstance(ctx, registration)
		}
	}

	// Check named services with enhanced type alias matching
	for _, registration := range r.container.namedServices {
		if r.isTypeAlias(paramType, registration.Type) {
			return r.CreateInstance(ctx, registration)
		}
	}

	return nil, common.ErrServiceNotFound(paramType.String())
}

// tryInterfaceMatch attempts to find interface implementations
func (r *Resolver) tryInterfaceMatch(ctx context.Context, interfaceType reflect.Type) (interface{}, error) {
	// Look for implementations in registered services
	for serviceType, registration := range r.container.services {
		if r.implementsInterface(serviceType, interfaceType) {
			return r.CreateInstance(ctx, registration)
		}
	}

	// Check named services
	for _, registration := range r.container.namedServices {
		if r.implementsInterface(registration.Type, interfaceType) {
			return r.CreateInstance(ctx, registration)
		}
	}

	return nil, common.ErrServiceNotFound(interfaceType.String())
}

// tryNamedServiceResolution attempts to resolve by common naming patterns
func (r *Resolver) tryNamedServiceResolution(ctx context.Context, paramType reflect.Type) (interface{}, error) {
	// Try common naming patterns
	typeName := paramType.Name()
	if typeName == "" {
		return nil, common.ErrServiceNotFound(paramType.String())
	}

	// Generate possible service names
	possibleNames := []string{
		strings.ToLower(typeName),
		strings.ToLower(typeName) + "-service",
		strings.ToLower(typeName) + "service",
		strings.ToLower(strings.TrimSuffix(typeName, "Service")),
		strings.ToLower(strings.TrimSuffix(typeName, "Interface")),
		typeName,                // exact case
		strings.Title(typeName), // title case
	}

	// First, try direct named service resolution
	for _, name := range possibleNames {
		if registration, exists := r.container.namedServices[name]; exists {
			if r.isCompatibleType(paramType, registration.Type) {
				return r.CreateInstance(ctx, registration)
			}
		}
	}

	// NEW: Try reference name mappings
	r.container.mu.RLock()
	referenceMappings := r.container.referenceNameMappings
	r.container.mu.RUnlock()

	for _, name := range possibleNames {
		if actualName, mapped := referenceMappings[name]; mapped {
			if registration, exists := r.container.namedServices[actualName]; exists {
				if r.isCompatibleType(paramType, registration.Type) {
					return r.CreateInstance(ctx, registration)
				}
			}
		}
	}

	return nil, common.ErrServiceNotFound(paramType.String())
}

// isTypeAlias checks if two types are aliases of each other with comprehensive checking
func (r *Resolver) isTypeAlias(type1, type2 reflect.Type) bool {
	// Direct comparison
	if type1 == type2 {
		return true
	}

	// String representation comparison (most reliable for type aliases)
	if type1.String() == type2.String() {
		return true
	}

	// Check underlying types
	if type1.Kind() == type2.Kind() {
		// For named types, check if they have the same name and package
		if type1.Name() != "" && type2.Name() != "" {
			// Same name in same package
			if type1.Name() == type2.Name() && type1.PkgPath() == type2.PkgPath() {
				return true
			}

			// Type alias patterns - one might be an alias of the other
			if r.isLikelyTypeAlias(type1, type2) {
				return true
			}
		}
	}

	return false
}

// isLikelyTypeAlias checks for common type alias patterns
func (r *Resolver) isLikelyTypeAlias(type1, type2 reflect.Type) bool {
	name1, name2 := type1.Name(), type2.Name()
	pkg1, pkg2 := type1.PkgPath(), type2.PkgPath()

	// Check for cross-package aliases (e.g., core.Logger = logger.Logger)
	if name1 == name2 && pkg1 != pkg2 {
		// Common pattern: package name is contained in the type name
		// e.g., logger.Logger and core.Logger where core imports logger
		return true
	}

	// Check for interface aliases across packages
	if type1.Kind() == reflect.Interface && type2.Kind() == reflect.Interface {
		// If they have the same method signature, they're likely aliases
		if r.haveSameMethodSignature(type1, type2) {
			return true
		}
	}

	return false
}

// haveSameMethodSignature checks if two interfaces have the same method signature
func (r *Resolver) haveSameMethodSignature(interface1, interface2 reflect.Type) bool {
	if interface1.NumMethod() != interface2.NumMethod() {
		return false
	}

	for i := 0; i < interface1.NumMethod(); i++ {
		method1 := interface1.Method(i)
		method2, found := interface2.MethodByName(method1.Name)
		if !found {
			return false
		}

		if method1.Type.String() != method2.Type.String() {
			return false
		}
	}

	return true
}

// implementsInterface checks if a type implements an interface with comprehensive checking
func (r *Resolver) implementsInterface(implType, interfaceType reflect.Type) bool {
	// Direct implementation check
	if implType.Implements(interfaceType) {
		return true
	}

	// Check if pointer to type implements the interface
	if reflect.PtrTo(implType).Implements(interfaceType) {
		return true
	}

	// For interface-to-interface, check if they're compatible
	if implType.Kind() == reflect.Interface && interfaceType.Kind() == reflect.Interface {
		return r.interfacesAreCompatible(implType, interfaceType)
	}

	return false
}

// interfacesAreCompatible checks if two interfaces are compatible
func (r *Resolver) interfacesAreCompatible(impl, target reflect.Type) bool {
	// Check if implementation interface has all methods of target interface
	for i := 0; i < target.NumMethod(); i++ {
		targetMethod := target.Method(i)
		implMethod, found := impl.MethodByName(targetMethod.Name)
		if !found {
			return false
		}

		if implMethod.Type.String() != targetMethod.Type.String() {
			return false
		}
	}

	return true
}

// isCompatibleType checks if two types are compatible for dependency injection
func (r *Resolver) isCompatibleType(paramType, serviceType reflect.Type) bool {
	return r.isTypeAlias(paramType, serviceType) ||
		r.implementsInterface(serviceType, paramType) ||
		r.implementsInterface(paramType, serviceType)
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

// getCacheKeys returns the keys in the cache
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
