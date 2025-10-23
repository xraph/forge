package di

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/xraph/forge/v0/pkg/common"
)

// Mock service for testing
type MockService struct {
	name         string
	dependencies []string
	started      bool
	stopped      bool
	healthError  error
}

func (m *MockService) Name() string {
	return m.name
}

func (m *MockService) Dependencies() []string {
	return m.dependencies
}

func (m *MockService) Start(ctx context.Context) error {
	m.started = true
	return nil
}

func (m *MockService) Stop(ctx context.Context) error {
	m.stopped = true
	return nil
}

func (m *MockService) OnHealthCheck(ctx context.Context) error {
	return m.healthError
}

// Mock interceptor for testing
type MockInterceptor struct {
	beforeCreateCalled  bool
	afterCreateCalled   bool
	beforeDestroyCalled bool
	afterDestroyCalled  bool
	beforeCreateError   error
	afterCreateError    error
	beforeDestroyError  error
	afterDestroyError   error
}

func (m *MockInterceptor) BeforeCreate(ctx context.Context, serviceType reflect.Type) error {
	m.beforeCreateCalled = true
	return m.beforeCreateError
}

func (m *MockInterceptor) AfterCreate(ctx context.Context, serviceType reflect.Type, instance interface{}) error {
	m.afterCreateCalled = true
	return m.afterCreateError
}

func (m *MockInterceptor) BeforeDestroy(ctx context.Context, serviceType reflect.Type, instance interface{}) error {
	m.beforeDestroyCalled = true
	return m.beforeDestroyError
}

func (m *MockInterceptor) AfterDestroy(ctx context.Context, serviceType reflect.Type) error {
	m.afterDestroyCalled = true
	return m.afterDestroyError
}

func createTestContainerWithMocks() *Container {
	config := ContainerConfig{
		Logger: &mockLogger{},
	}
	return NewContainer(config).(*Container)
}

func TestContainerUtilityMethods(t *testing.T) {
	t.Run("Has method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test with nil type
		if container.Has(nil) {
			t.Error("Expected Has(nil) to return false")
		}

		// Test with unregistered type
		if container.Has((*TestStruct)(nil)) {
			t.Error("Expected Has with unregistered type to return false")
		}

		// Register a service by type (not by name)
		err := container.Register(common.ServiceDefinition{
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Test with registered type
		if !container.Has((*TestStruct)(nil)) {
			t.Error("Expected Has with registered type to return true")
		}
	})

	t.Run("HasNamed method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test with unregistered name
		if container.HasNamed("nonexistent") {
			t.Error("Expected HasNamed with unregistered name to return false")
		}

		// Register a named service
		err := container.Register(common.ServiceDefinition{
			Name: "named-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "named"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register named service: %v", err)
		}

		// Test with registered name
		if !container.HasNamed("named-service") {
			t.Error("Expected HasNamed with registered name to return true")
		}
	})

	t.Run("Services method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test with no services
		services := container.Services()
		if len(services) != 0 {
			t.Errorf("Expected 0 services, got %d", len(services))
		}

		// Register some services
		err := container.Register(common.ServiceDefinition{
			Name: "service1",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "service1"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service1: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Type: (*AnotherStruct)(nil),
			Constructor: func() *AnotherStruct {
				return &AnotherStruct{Value: 42}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service2: %v", err)
		}

		// Test with services
		services = container.Services()
		if len(services) != 2 {
			t.Errorf("Expected 2 services, got %d", len(services))
		}
	})

	t.Run("IsStarted method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test before start
		if container.IsStarted() {
			t.Error("Expected container to not be started initially")
		}

		// Start container
		err := container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Test after start
		if !container.IsStarted() {
			t.Error("Expected container to be started")
		}

		// Stop container
		err = container.Stop(context.Background())
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}

		// Test after stop
		if container.IsStarted() {
			t.Error("Expected container to not be started after stop")
		}
	})

	t.Run("GetStats method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test initial stats
		stats := container.GetStats()
		if stats.ServicesRegistered != 0 {
			t.Errorf("Expected 0 services registered, got %d", stats.ServicesRegistered)
		}
		if stats.InstancesCreated != 0 {
			t.Errorf("Expected 0 instances created, got %d", stats.InstancesCreated)
		}
		if stats.Started {
			t.Error("Expected container to not be started")
		}
		if stats.Interceptors != 0 {
			t.Errorf("Expected 0 interceptors, got %d", stats.Interceptors)
		}
		if stats.ReferenceMappings != 0 {
			t.Errorf("Expected 0 reference mappings, got %d", stats.ReferenceMappings)
		}

		// Register a service
		err := container.Register(common.ServiceDefinition{
			Name: "test-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Test stats after registration
		stats = container.GetStats()
		if stats.ServicesRegistered != 1 {
			t.Errorf("Expected 1 service registered, got %d", stats.ServicesRegistered)
		}
		if stats.ReferenceMappings == 0 {
			t.Error("Expected reference mappings to be created")
		}
	})

	t.Run("NamedServices method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test with no services
		namedServices := container.NamedServices()
		if len(namedServices) != 0 {
			t.Errorf("Expected 0 named services, got %d", len(namedServices))
		}

		// Register a named service
		err := container.Register(common.ServiceDefinition{
			Name: "named-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "named"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register named service: %v", err)
		}

		// Test with named service
		namedServices = container.NamedServices()
		if len(namedServices) != 1 {
			t.Errorf("Expected 1 named service, got %d", len(namedServices))
		}
		if _, exists := namedServices["named-service"]; !exists {
			t.Error("Expected to find named service")
		}
	})

	t.Run("LifecycleManager method", func(t *testing.T) {
		container := createTestContainerWithMocks()
		lm := container.LifecycleManager()
		if lm == nil {
			t.Error("Expected lifecycle manager to be non-nil")
		}
	})

	t.Run("GetConfigurator method", func(t *testing.T) {
		container := createTestContainerWithMocks()
		configurator := container.GetConfigurator()
		if configurator == nil {
			t.Error("Expected configurator to be non-nil")
		}
	})

	t.Run("GetResolver method", func(t *testing.T) {
		container := createTestContainerWithMocks()
		resolver := container.GetResolver()
		if resolver == nil {
			t.Error("Expected resolver to be non-nil")
		}
	})

	t.Run("GetValidator method", func(t *testing.T) {
		container := createTestContainerWithMocks()
		validator := container.GetValidator()
		if validator == nil {
			t.Error("Expected validator to be non-nil")
		}
	})
}

func TestInterceptorManagement(t *testing.T) {
	t.Run("AddInterceptor method", func(t *testing.T) {
		container := createTestContainerWithMocks()
		interceptor := &MockInterceptor{}

		// Test adding interceptor
		container.AddInterceptor(interceptor)

		// Verify interceptor was added
		interceptors := container.GetInterceptors()
		if len(interceptors) != 1 {
			t.Errorf("Expected 1 interceptor, got %d", len(interceptors))
		}
		if interceptors[0] != interceptor {
			t.Error("Expected added interceptor to be in list")
		}
	})

	t.Run("RemoveInterceptor method", func(t *testing.T) {
		container := createTestContainerWithMocks()
		interceptor1 := &MockInterceptor{}
		interceptor2 := &MockInterceptor{}

		// Add interceptors
		container.AddInterceptor(interceptor1)
		container.AddInterceptor(interceptor2)

		// Verify both are added
		interceptors := container.GetInterceptors()
		if len(interceptors) != 2 {
			t.Errorf("Expected 2 interceptors, got %d", len(interceptors))
		}

		// Remove one interceptor
		container.RemoveInterceptor(interceptor1)

		// Verify only one remains
		interceptors = container.GetInterceptors()
		if len(interceptors) != 1 {
			t.Errorf("Expected 1 interceptor, got %d", len(interceptors))
		}
		if interceptors[0] != interceptor2 {
			t.Error("Expected remaining interceptor to be interceptor2")
		}
	})

	t.Run("GetInterceptors method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test with no interceptors
		interceptors := container.GetInterceptors()
		if len(interceptors) != 0 {
			t.Errorf("Expected 0 interceptors, got %d", len(interceptors))
		}

		// Add interceptors
		interceptor1 := &MockInterceptor{}
		interceptor2 := &MockInterceptor{}
		container.AddInterceptor(interceptor1)
		container.AddInterceptor(interceptor2)

		// Test with interceptors
		interceptors = container.GetInterceptors()
		if len(interceptors) != 2 {
			t.Errorf("Expected 2 interceptors, got %d", len(interceptors))
		}

		// Verify interceptors are copied (not same slice)
		interceptors[0] = nil
		interceptors2 := container.GetInterceptors()
		if interceptors2[0] == nil {
			t.Error("Expected interceptors to be copied, not shared")
		}
	})
}

func TestReferenceMappings(t *testing.T) {
	t.Run("AddReferenceMapping method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Register a service first
		err := container.Register(common.ServiceDefinition{
			Name: "test-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Test adding reference mapping
		err = container.AddReferenceMapping("ref-name", "test-service")
		if err != nil {
			t.Fatalf("Failed to add reference mapping: %v", err)
		}

		// Verify mapping was added
		mappings := container.GetReferenceMappings()
		if mappings["ref-name"] != "test-service" {
			t.Error("Expected reference mapping to be added")
		}

		// Test adding duplicate mapping
		err = container.AddReferenceMapping("ref-name", "test-service")
		if err == nil {
			t.Error("Expected error when adding duplicate reference mapping")
		}

		// Test adding mapping for non-existent service
		err = container.AddReferenceMapping("ref-name2", "non-existent")
		if err == nil {
			t.Error("Expected error when adding mapping for non-existent service")
		}

		// Test adding mapping after container start
		err = container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		err = container.AddReferenceMapping("ref-name3", "test-service")
		if err == nil {
			t.Error("Expected error when adding mapping after container start")
		}
	})

	t.Run("RemoveReferenceMapping method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Register a service first
		err := container.Register(common.ServiceDefinition{
			Name: "test-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Add reference mapping
		err = container.AddReferenceMapping("ref-name", "test-service")
		if err != nil {
			t.Fatalf("Failed to add reference mapping: %v", err)
		}

		// Test removing existing mapping
		err = container.RemoveReferenceMapping("ref-name")
		if err != nil {
			t.Fatalf("Failed to remove reference mapping: %v", err)
		}

		// Verify mapping was removed
		mappings := container.GetReferenceMappings()
		if _, exists := mappings["ref-name"]; exists {
			t.Error("Expected reference mapping to be removed")
		}

		// Test removing non-existent mapping
		err = container.RemoveReferenceMapping("non-existent")
		if err == nil {
			t.Error("Expected error when removing non-existent mapping")
		}
	})

	t.Run("ResolveByReference method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Register a service
		err := container.Register(common.ServiceDefinition{
			Name: "test-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Add reference mapping
		err = container.AddReferenceMapping("ref-name", "test-service")
		if err != nil {
			t.Fatalf("Failed to add reference mapping: %v", err)
		}

		// Start container
		err = container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Test resolving by reference
		instance, err := container.ResolveByReference("ref-name")
		if err != nil {
			t.Fatalf("Failed to resolve by reference: %v", err)
		}

		testStruct, ok := instance.(*TestStruct)
		if !ok {
			t.Fatalf("Expected *TestStruct, got %T", instance)
		}
		if testStruct.Name != "test" {
			t.Errorf("Expected name 'test', got '%s'", testStruct.Name)
		}

		// Test resolving non-existent reference
		_, err = container.ResolveByReference("non-existent")
		if err == nil {
			t.Error("Expected error when resolving non-existent reference")
		}
	})

	t.Run("GetReferenceMappings method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test with no mappings
		mappings := container.GetReferenceMappings()
		if len(mappings) != 0 {
			t.Errorf("Expected 0 mappings, got %d", len(mappings))
		}

		// Register a service and add mapping
		err := container.Register(common.ServiceDefinition{
			Name: "test-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		err = container.AddReferenceMapping("ref-name", "test-service")
		if err != nil {
			t.Fatalf("Failed to add reference mapping: %v", err)
		}

		// Test with mappings
		mappings = container.GetReferenceMappings()
		if len(mappings) < 1 {
			t.Errorf("Expected at least 1 mapping, got %d", len(mappings))
		}
		if mappings["ref-name"] != "test-service" {
			t.Error("Expected correct mapping")
		}

		// Verify mappings are copied (not same map)
		mappings["ref-name"] = "modified"
		mappings2 := container.GetReferenceMappings()
		if mappings2["ref-name"] != "test-service" {
			t.Error("Expected mappings to be copied, not shared")
		}
	})
}

func TestHealthCheck(t *testing.T) {
	t.Run("HealthCheck method", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Test health check before start
		err := container.HealthCheck(context.Background())
		if err == nil {
			t.Error("Expected error when health checking before start")
		}

		// Start container
		err = container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Test health check after start
		err = container.HealthCheck(context.Background())
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}

		// Stop container
		err = container.Stop(context.Background())
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}

		// Test health check after stop
		err = container.HealthCheck(context.Background())
		if err == nil {
			t.Error("Expected error when health checking after stop")
		}
	})
}

func TestContainerEdgeCases(t *testing.T) {
	t.Run("Registration after start", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Start container
		err := container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Try to register after start
		err = container.Register(common.ServiceDefinition{
			Name: "after-start",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
		})
		if err == nil {
			t.Error("Expected error when registering after start")
		}
	})

	t.Run("Double start", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Start container
		err := container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Try to start again
		err = container.Start(context.Background())
		if err == nil {
			t.Error("Expected error when starting already started container")
		}
	})

	t.Run("Stop before start", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Try to stop before start
		err := container.Stop(context.Background())
		if err == nil {
			t.Error("Expected error when stopping before start")
		}
	})

	t.Run("Concurrent access", func(t *testing.T) {
		container := createTestContainerWithMocks()
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		// Register a service
		err := container.Register(common.ServiceDefinition{
			Name: "concurrent-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "concurrent"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Start container
		err = container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Concurrent resolution
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := container.ResolveNamed("concurrent-service")
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent resolution failed: %v", err)
		}
	})
}

func TestContainerErrorPaths(t *testing.T) {
	t.Run("Invalid constructor", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Register with invalid constructor (not a function)
		err := container.Register(common.ServiceDefinition{
			Name:        "invalid-constructor",
			Type:        (*TestStruct)(nil),
			Constructor: "not-a-function",
		})
		if err == nil {
			t.Error("Expected error when registering with invalid constructor")
		}
	})

	t.Run("Nil type registration", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Register with nil type
		err := container.Register(common.ServiceDefinition{
			Name: "nil-type",
			Type: nil,
		})
		if err == nil {
			t.Error("Expected error when registering with nil type")
		}
	})

	t.Run("Constructor with no return value", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Register with constructor that returns nothing
		err := container.Register(common.ServiceDefinition{
			Name: "no-return",
			Type: (*TestStruct)(nil),
			Constructor: func() {
				// No return value
			},
		})
		if err != nil {
			t.Fatalf("Registration should succeed, validation happens during start: %v", err)
		}

		// Start container - this should fail due to invalid constructor
		err = container.Start(context.Background())
		if err == nil {
			t.Error("Expected error when starting container with invalid constructor")
		}
	})

	t.Run("Constructor with error return", func(t *testing.T) {
		container := createTestContainerWithMocks()

		// Register with constructor that returns error
		err := container.Register(common.ServiceDefinition{
			Name: "error-constructor",
			Type: (*TestStruct)(nil),
			Constructor: func() (*TestStruct, error) {
				return nil, errors.New("constructor error")
			},
		})
		if err != nil {
			t.Fatalf("Failed to register error constructor: %v", err)
		}

		// Start container
		err = container.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Try to resolve - should get the constructor error
		_, err = container.ResolveNamed("error-constructor")
		if err == nil {
			t.Error("Expected error when resolving service with error constructor")
		}
	})
}
