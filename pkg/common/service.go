package common

import (
	"context"
	"fmt"
	"reflect"
)

// =============================================================================
// CORE SERVICE INTERFACES
// =============================================================================

// Service defines the interface that all business services must implement
type Service interface {
	// Name returns the unique name of the service
	Name() string

	// Dependencies returns the list of service names this service depends on
	Dependencies() []string

	// Start is called when the service should start
	Start(ctx context.Context) error

	// Stop is called when the service should stop
	Stop(ctx context.Context) error

	// OnHealthCheck is called to check if the service is healthy
	OnHealthCheck(ctx context.Context) error
}

// Controller defines the interface for HTTP controllers
type Controller interface {
	// Name returns the unique name of the controller
	Name() string

	// Prefix returns the route prefix for this controller (optional)
	Prefix() string

	// Dependencies returns the list of services this controller depends on
	Dependencies() []string

	// ConfigureRoutes configures routes using the provided router (UPDATED: flexible approach)
	ConfigureRoutes(router Router) error

	// Middleware returns controller-specific middleware
	Middleware() []any

	// Initialize initializes the controller with dependencies
	Initialize(container Container) error
}

// =============================================================================
// SERVICE DEFINITIONS
// =============================================================================

// ServiceDefinition defines how a service should be registered
type ServiceDefinition struct {
	Name          string            `json:"name"`
	Type          interface{}       `json:"-"`
	Constructor   interface{}       `json:"-"`
	Instance      interface{}       `json:"-"`
	Singleton     bool              `json:"singleton"`
	Dependencies  []string          `json:"dependencies"`
	Config        interface{}       `json:"config"`
	Lifecycle     ServiceLifecycle  `json:"-"`
	Extensions    map[string]any    `json:"extensions"`
	Tags          map[string]string `json:"tags"`
	ReferenceName string            `json:"reference_names"`
}

// ServiceName returns the name of the service.
// If Name is not empty, it returns Name.
// If Name is empty, it returns the string representation of Type.
// If both are empty/nil, it returns "unknown".
func (sd *ServiceDefinition) ServiceName() string {
	// If Name is explicitly set, use it
	if sd.Name != "" {
		return sd.Name
	}

	// If Name is empty, try to use the Type
	if sd.Type != nil {
		serviceType := reflect.TypeOf(sd.Type)
		// If it's a pointer to an interface, get the interface type
		if serviceType.Kind() == reflect.Ptr {
			serviceType = serviceType.Elem()
		}
		return serviceType.String()
	}

	// Fallback if both Name and Type are empty/nil
	return ""
}

func (sd *ServiceDefinition) ServiceType() (reflect.Type, error) {
	// Get the service type from the definition
	if sd.Type == nil {
		return nil, ErrContainerError("register", fmt.Errorf("service type cannot be nil"))
	}

	var serviceType reflect.Type
	serviceType = reflect.TypeOf(sd.Type)
	// If it's a pointer to an interface, get the interface type
	if serviceType.Kind() == reflect.Ptr {
		serviceType = serviceType.Elem()
	}
	return serviceType, nil
}

// GetReferenceName returns the reference name for this service
func (sd *ServiceDefinition) GetReferenceName() string {
	// Return explicit reference name if set
	if sd.ReferenceName != "" {
		return sd.ReferenceName
	}

	// Backward compatibility support for Extensions
	if extRef, exists := sd.Extensions["referenceName"]; exists {
		if refName, ok := extRef.(string); ok {
			return refName
		}
	}

	return ""
}

// GetAllReferenceNames returns the reference name for this service
func (sd *ServiceDefinition) GetAllReferenceNames() []string {
	// Return explicit reference name if set
	if sd.ReferenceName != "" {
		return []string{sd.ReferenceName}
	}

	// Backward compatibility support for Extensions
	if extRef, exists := sd.Extensions["referenceName"]; exists {
		if refName, ok := extRef.(string); ok {
			return []string{refName}
		}
	}

	return []string{""}
}

// ServiceLifecycle defines lifecycle hooks for services
type ServiceLifecycle struct {
	OnStart       func(ctx context.Context, service interface{}) error
	OnStop        func(ctx context.Context, service interface{}) error
	OnHealthCheck func(ctx context.Context, service interface{}) error
	OnConfig      func(ctx context.Context, service interface{}, config interface{}) error
}
