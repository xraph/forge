package di

import (
	"fmt"
	"reflect"

	"github.com/xraph/forge/pkg/common"
)

// Validator handles dependency validation
type Validator struct {
	container *Container
}

// NewValidator creates a new validator
func NewValidator(container *Container) common.Validator {
	return &Validator{
		container: container,
	}
}

// validateDependencies validates that all dependencies exist
func (v *Validator) validateDependencies() error {
	allServices := make(map[string]bool)

	// Collect all service names
	for _, registration := range v.container.services {
		if registration.Name != "" {
			allServices[registration.Name] = true
		} else {
			allServices[registration.Type.String()] = true
		}
	}

	for name := range v.container.namedServices {
		allServices[name] = true
	}

	// Check dependencies
	for _, registration := range v.container.services {
		for _, dep := range registration.Dependencies {
			if !allServices[dep] {
				serviceName := registration.Name
				if serviceName == "" {
					serviceName = registration.Type.String()
				}
				return common.ErrDependencyNotFound(serviceName, dep)
			}
		}
	}

	for _, registration := range v.container.namedServices {
		for _, dep := range registration.Dependencies {
			if !allServices[dep] {
				return common.ErrDependencyNotFound(registration.Name, dep)
			}
		}
	}

	return nil
}

// validateCircularDependencies validates that there are no circular dependencies
func (v *Validator) validateCircularDependencies() error {
	// Build dependency graph
	graph := make(map[string][]string)

	for _, registration := range v.container.services {
		serviceName := registration.Name
		if serviceName == "" {
			serviceName = registration.Type.String()
		}
		graph[serviceName] = registration.Dependencies
	}

	for _, registration := range v.container.namedServices {
		graph[registration.Name] = registration.Dependencies
	}

	// Use DFS to detect cycles
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for service := range graph {
		if !visited[service] {
			if path := v.detectCycle(service, graph, visited, recStack, []string{}); path != nil {
				return common.ErrCircularDependency(path)
			}
		}
	}

	return nil
}

// detectCycle detects circular dependencies using DFS
func (v *Validator) detectCycle(service string, graph map[string][]string, visited, recStack map[string]bool, path []string) []string {
	visited[service] = true
	recStack[service] = true
	path = append(path, service)

	for _, dep := range graph[service] {
		if !visited[dep] {
			if cyclePath := v.detectCycle(dep, graph, visited, recStack, path); cyclePath != nil {
				return cyclePath
			}
		} else if recStack[dep] {
			// Found cycle
			cycleStart := -1
			for i, node := range path {
				if node == dep {
					cycleStart = i
					break
				}
			}
			if cycleStart != -1 {
				return append(path[cycleStart:], dep)
			}
		}
	}

	recStack[service] = false
	return nil
}

// validateServiceDefinitions validates individual service definitions
func (v *Validator) validateServiceDefinitions() error {
	for _, registration := range v.container.services {
		if err := v.validateServiceDefinition(registration); err != nil {
			return err
		}
	}

	for _, registration := range v.container.namedServices {
		if err := v.validateServiceDefinition(registration); err != nil {
			return err
		}
	}

	return nil
}

// validateServiceDefinition validates a single service definition
func (v *Validator) validateServiceDefinition(registration *ServiceRegistration) error {
	serviceName := registration.Name
	if serviceName == "" {
		serviceName = registration.Type.String()
	}

	if registration.Type == nil {
		return common.ErrValidationError("type", fmt.Errorf("service %s has no type", serviceName))
	}

	if registration.Constructor != nil {
		constructorType := reflect.TypeOf(registration.Constructor)
		if constructorType.Kind() != reflect.Func {
			return common.ErrValidationError("constructor", fmt.Errorf("service %s constructor must be a function", serviceName))
		}
		if constructorType.NumOut() == 0 {
			return common.ErrValidationError("constructor", fmt.Errorf("service %s constructor must return at least one value", serviceName))
		}

		returnType := constructorType.Out(0)
		if registration.Type.Kind() == reflect.Interface {
			if !returnType.Implements(registration.Type) && !reflect.PtrTo(returnType).Implements(registration.Type) {
				return common.ErrValidationError("constructor",
					fmt.Errorf("service %s constructor return type %s does not implement service interface %s",
						serviceName, returnType, registration.Type))
			}
		} else {
			if returnType != registration.Type {
				return common.ErrValidationError("constructor",
					fmt.Errorf("service %s constructor return type %s does not match service type %s",
						serviceName, returnType, registration.Type))
			}
		}
	}

	return nil
}

// ValidateService validates a specific service
func (v *Validator) ValidateService(serviceName string) error {
	// Check if service exists
	var registration *ServiceRegistration
	var exists bool

	if registration, exists = v.container.namedServices[serviceName]; !exists {
		// Try to find by type name
		for _, reg := range v.container.services {
			if reg.Type.Name() == serviceName {
				registration = reg
				exists = true
				break
			}
		}
	}

	if !exists {
		return common.ErrServiceNotFound(serviceName)
	}

	return v.validateServiceDefinition(registration)
}

// GetValidationReport returns a validation report
func (v *Validator) GetValidationReport() common.ValidationReport {
	report := common.ValidationReport{
		Services: make(map[string]common.ServiceValidationResult),
		Valid:    true,
	}

	// Validate all services
	for _, registration := range v.container.services {
		serviceName := registration.Name
		if serviceName == "" {
			serviceName = registration.Type.Name()
		}

		result := common.ServiceValidationResult{
			Name:         serviceName,
			Type:         registration.Type.String(),
			Dependencies: registration.Dependencies,
			Valid:        true,
		}

		if err := v.validateServiceDefinition(registration); err != nil {
			result.Valid = false
			result.Error = err.Error()
			report.Valid = false
		}

		report.Services[serviceName] = result
	}

	for _, registration := range v.container.namedServices {
		result := common.ServiceValidationResult{
			Name:         registration.Name,
			Type:         registration.Type.String(),
			Dependencies: registration.Dependencies,
			Valid:        true,
		}

		if err := v.validateServiceDefinition(registration); err != nil {
			result.Valid = false
			result.Error = err.Error()
			report.Valid = false
		}

		report.Services[registration.Name] = result
	}

	// Check for circular dependencies
	if err := v.validateCircularDependencies(); err != nil {
		report.Valid = false
		report.CircularDependencyError = err.Error()
	}

	return report
}

// ValidateServiceHandlers validates that all service handlers can resolve their dependencies
func (v *Validator) ValidateServiceHandlers(serviceHandlers map[string]*common.RouteHandlerInfo) error {
	for handlerKey, info := range serviceHandlers {
		// Validate that the service type can be resolved
		if info.ServiceType != nil {
			if err := v.validateServiceTypeResolution(info.ServiceType, info.ServiceName); err != nil {
				return common.ErrValidationError("service_handler",
					fmt.Errorf("handler %s cannot resolve service %s: %v", handlerKey, info.ServiceName, err))
			}
		}

		// Validate explicit dependencies
		for _, dep := range info.Dependencies {
			if !v.serviceExists(dep) {
				return common.ErrDependencyNotFound(handlerKey, dep)
			}
		}
	}
	return nil
}

// validateServiceTypeResolution validates that a service type can be resolved
func (v *Validator) validateServiceTypeResolution(serviceType reflect.Type, serviceName string) error {
	// Check if service type is registered
	if serviceType.Kind() == reflect.Interface {
		// For interface types, look for implementations
		found := false
		for registeredType := range v.container.services {
			if v.typesMatch(registeredType, serviceType) {
				found = true
				break
			}
			// Check if concrete type implements the interface
			if registeredType.Implements(serviceType) {
				found = true
				break
			}
		}

		// Check named services
		if !found {
			for _, registration := range v.container.namedServices {
				if v.typesMatch(registration.Type, serviceType) {
					found = true
					break
				}
				if registration.Type.Implements(serviceType) {
					found = true
					break
				}
			}
		}

		if !found {
			return common.ErrServiceNotFound(serviceType.String())
		}
	} else {
		// For concrete types, check direct registration
		found := false
		for registeredType := range v.container.services {
			if v.typesMatch(registeredType, serviceType) {
				found = true
				break
			}
		}

		// Check named services
		if !found {
			for _, registration := range v.container.namedServices {
				if v.typesMatch(registration.Type, serviceType) {
					found = true
					break
				}
			}
		}

		if !found {
			return common.ErrServiceNotFound(serviceType.String())
		}
	}

	return nil
}

// typesMatch checks if two types match, accounting for type aliases
func (v *Validator) typesMatch(type1, type2 reflect.Type) bool {
	// Direct comparison
	if type1 == type2 {
		return true
	}

	// Check if they have the same underlying type (for type aliases)
	if type1.Kind() == type2.Kind() &&
		type1.String() == type2.String() {
		return true
	}

	// For interfaces, check if they represent the same interface
	if type1.Kind() == reflect.Interface && type2.Kind() == reflect.Interface {
		// Compare package path and name
		if type1.PkgPath() == type2.PkgPath() && type1.Name() == type2.Name() {
			return true
		}

		// Check if one implements the other (for type aliases of interfaces)
		if type1.Implements(type2) && type2.Implements(type1) {
			return true
		}
	}

	return false
}

// serviceExists checks if a service exists by name
func (v *Validator) serviceExists(serviceName string) bool {
	// Check named services
	if _, exists := v.container.namedServices[serviceName]; exists {
		return true
	}

	// Check reference mappings
	if actualName, mapped := v.container.referenceNameMappings[serviceName]; mapped {
		if _, exists := v.container.namedServices[actualName]; exists {
			return true
		}
	}

	// Check services by type name
	for _, registration := range v.container.services {
		if registration.Type.Name() == serviceName {
			return true
		}
	}

	return false
}

// ValidateAll validates all registered services and handlers
func (v *Validator) ValidateAll() error {
	// Check for missing dependencies
	if err := v.validateDependencies(); err != nil {
		return err
	}

	// Check for circular dependencies
	if err := v.validateCircularDependencies(); err != nil {
		return err
	}

	// Validate service definitions
	if err := v.validateServiceDefinitions(); err != nil {
		return err
	}

	return nil
}

// ValidateAllWithHandlers validates services and handlers
func (v *Validator) ValidateAllWithHandlers(serviceHandlers map[string]*common.RouteHandlerInfo) error {
	// Validate services first
	if err := v.ValidateAll(); err != nil {
		return err
	}

	// Validate service handlers
	if err := v.ValidateServiceHandlers(serviceHandlers); err != nil {
		return err
	}

	return nil
}
