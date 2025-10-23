package di

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Test interfaces and structs for type handling tests
type TestService interface {
	GetName() string
}

type TestStruct struct {
	Name string
}

func (ts *TestStruct) GetName() string {
	return ts.Name
}

type AnotherService interface {
	GetValue() int
}

type AnotherStruct struct {
	Value int
}

func (as *AnotherStruct) GetValue() int {
	return as.Value
}

// Test service that depends on other services
type CompositeService struct {
	TestSvc    TestService
	AnotherSvc AnotherService
	Name       string
}

func (cs *CompositeService) GetName() string {
	return cs.Name
}

func (cs *CompositeService) Dependencies() []string {
	return []string{}
}

func (cs *CompositeService) OnStart(ctx context.Context) error {
	return nil
}

func (cs *CompositeService) OnStop(ctx context.Context) error {
	return nil
}

func (cs *CompositeService) OnHealthCheck(ctx context.Context) error {
	return nil
}

// Mock logger for tests
type mockLogger struct{}

func (m *mockLogger) Debugf(template string, args ...interface{}) {
}

func (m *mockLogger) Infof(template string, args ...interface{}) {
}

func (m *mockLogger) Warnf(template string, args ...interface{}) {
}

func (m *mockLogger) Errorf(template string, args ...interface{}) {
}

func (m *mockLogger) Fatalf(template string, args ...interface{}) {
}

func (m *mockLogger) Named(name string) logger.Logger {
	return m
}

func (m *mockLogger) Sugar() logger.SugarLogger {
	return nil
}

func (m *mockLogger) Sync() error {
	return nil
}

func (m *mockLogger) Debug(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Info(msg string, fields ...logger.Field)       {}
func (m *mockLogger) Warn(msg string, fields ...logger.Field)       {}
func (m *mockLogger) Error(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Fatal(msg string, fields ...logger.Field)      {}
func (m *mockLogger) With(fields ...logger.Field) common.Logger     { return m }
func (m *mockLogger) WithContext(ctx context.Context) common.Logger { return m }

func createTestContainer() *Container {
	config := ContainerConfig{
		Logger: &mockLogger{},
	}
	return NewContainer(config).(*Container)
}

func TestTypeStorageAndRetrieval(t *testing.T) {
	t.Run("Register struct as pointer type", func(t *testing.T) {
		container := createTestContainer()
		// Register as *TestStruct
		err := container.Register(common.ServiceDefinition{
			Name: "test-struct-ptr",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
			Singleton: true,
		})

		if err != nil {
			t.Fatalf("Failed to register pointer struct: %v", err)
		}

		// Verify the type is stored as *TestStruct, not TestStruct
		registration := container.namedServices["test-struct-ptr"]
		if registration == nil {
			t.Fatal("Service not found")
		}

		expectedType := reflect.TypeOf((*TestStruct)(nil))
		if registration.Type != expectedType {
			t.Errorf("Expected type %v, got %v", expectedType, registration.Type)
		}
	})

	t.Run("Register struct as value type", func(t *testing.T) {
		container := createTestContainer()
		// Register as TestStruct (value)
		err := container.Register(common.ServiceDefinition{
			Name: "test-struct-val",
			Type: TestStruct{},
			Constructor: func() TestStruct {
				return TestStruct{Name: "test"}
			},
			Singleton: true,
		})

		if err != nil {
			t.Fatalf("Failed to register value struct: %v", err)
		}

		// Verify the type is stored as TestStruct, not *TestStruct
		registration := container.namedServices["test-struct-val"]
		if registration == nil {
			t.Fatal("Service not found")
		}

		expectedType := reflect.TypeOf(TestStruct{})
		if registration.Type != expectedType {
			t.Errorf("Expected type %v, got %v", expectedType, registration.Type)
		}
	})

	t.Run("Register interface as interface type", func(t *testing.T) {
		container := createTestContainer()
		// Register as TestService interface
		err := container.Register(common.ServiceDefinition{
			Name: "test-service-iface",
			Type: (*TestService)(nil),
			Constructor: func() TestService {
				return &TestStruct{Name: "test"}
			},
			Singleton: true,
		})

		if err != nil {
			t.Fatalf("Failed to register interface: %v", err)
		}

		// Verify the type is stored as TestService interface
		registration := container.namedServices["test-service-iface"]
		if registration == nil {
			t.Fatal("Service not found")
		}

		expectedType := reflect.TypeOf((*TestService)(nil)).Elem()
		if registration.Type != expectedType {
			t.Errorf("Expected type %v, got %v", expectedType, registration.Type)
		}
	})
}

func TestExactTypeResolution(t *testing.T) {
	t.Run("Resolve pointer struct by exact type", func(t *testing.T) {
		container := createTestContainer()
		// Register *TestStruct
		err := container.Register(common.ServiceDefinition{
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Resolve by exact type *TestStruct
		instance, err := container.Resolve((*TestStruct)(nil))
		if err != nil {
			t.Fatalf("Resolution failed: %v", err)
		}

		testStruct, ok := instance.(*TestStruct)
		if !ok {
			t.Fatalf("Expected *TestStruct, got %T", instance)
		}

		if testStruct.Name != "test" {
			t.Errorf("Expected name 'test', got '%s'", testStruct.Name)
		}
	})

	t.Run("Resolve value struct by exact type", func(t *testing.T) {
		container := createTestContainer()
		// Register TestStruct as value
		err := container.Register(common.ServiceDefinition{
			Type: TestStruct{},
			Constructor: func() TestStruct {
				return TestStruct{Name: "test-val"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Resolve by exact type TestStruct
		instance, err := container.Resolve(TestStruct{})
		if err != nil {
			t.Fatalf("Resolution failed: %v", err)
		}

		testStruct, ok := instance.(TestStruct)
		if !ok {
			t.Fatalf("Expected TestStruct, got %T", instance)
		}

		if testStruct.Name != "test-val" {
			t.Errorf("Expected name 'test-val', got '%s'", testStruct.Name)
		}
	})

	t.Run("Resolve interface by implementation", func(t *testing.T) {
		container := createTestContainer()
		// Register TestStruct which implements TestService
		err := container.Register(common.ServiceDefinition{
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "interface-test"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Resolve by the interface type
		instance, err := container.Resolve((*TestService)(nil))
		if err != nil {
			t.Fatalf("Resolution failed: %v", err)
		}

		testService, ok := instance.(TestService)
		if !ok {
			t.Fatalf("Expected TestService, got %T", instance)
		}

		if testService.GetName() != "interface-test" {
			t.Errorf("Expected name 'interface-test', got '%s'", testService.GetName())
		}
	})
}

func TestConstructorDependencyResolution(t *testing.T) {
	container := createTestContainer()

	// Register dependencies first
	err := container.Register(common.ServiceDefinition{
		Name: "test-service",
		Type: (*TestService)(nil),
		Constructor: func() TestService {
			return &TestStruct{Name: "dependency"}
		},
		Singleton: true,
	})
	if err != nil {
		t.Fatalf("Failed to register TestService: %v", err)
	}

	err = container.Register(common.ServiceDefinition{
		Name: "another-service",
		Type: (*AnotherService)(nil),
		Constructor: func() AnotherService {
			return &AnotherStruct{Value: 42}
		},
		Singleton: true,
	})
	if err != nil {
		t.Fatalf("Failed to register AnotherService: %v", err)
	}

	t.Run("Constructor with interface dependencies", func(t *testing.T) {
		// Register composite service that depends on both interfaces
		err := container.Register(common.ServiceDefinition{
			Name: "composite-service",
			Type: (*CompositeService)(nil),
			Constructor: func(ts TestService, as AnotherService) *CompositeService {
				return &CompositeService{
					TestSvc:    ts,
					AnotherSvc: as,
					Name:       "composite",
				}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Failed to register CompositeService: %v", err)
		}

		// Resolve the composite service
		instance, err := container.ResolveNamed("composite-service")
		if err != nil {
			t.Fatalf("Failed to resolve composite service: %v", err)
		}

		composite, ok := instance.(*CompositeService)
		if !ok {
			t.Fatalf("Expected *CompositeService, got %T", instance)
		}

		if composite.Name != "composite" {
			t.Errorf("Expected name 'composite', got '%s'", composite.Name)
		}

		if composite.TestSvc.GetName() != "dependency" {
			t.Errorf("Expected TestSvc name 'dependency', got '%s'", composite.TestSvc.GetName())
		}

		if composite.AnotherSvc.GetValue() != 42 {
			t.Errorf("Expected AnotherSvc value 42, got %d", composite.AnotherSvc.GetValue())
		}
	})
}

func TestTypeCompatibilityScenarios(t *testing.T) {
	t.Run("Interface registered, struct requested - should fail", func(t *testing.T) {
		container := createTestContainer()

		// Register as TestService interface
		err := container.Register(common.ServiceDefinition{
			Name: "test-service-iface",
			Type: (*TestService)(nil),
			Constructor: func() TestService {
				return &TestStruct{Name: "test"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Try to resolve as *TestStruct - should fail because resolution is strict.
		// We registered an interface, not a concrete type.
		_, err = container.Resolve((*TestStruct)(nil))
		if err == nil {
			t.Error("Expected resolution to fail when requesting concrete type for interface registration")
		}
	})

	t.Run("Pointer registered, value requested - should fail", func(t *testing.T) {
		container := createTestContainer()

		// Register as *TestStruct
		err := container.Register(common.ServiceDefinition{
			Name: "test-struct-ptr2",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Try to resolve as TestStruct value - should fail
		_, err = container.Resolve(TestStruct{})
		if err == nil {
			t.Error("Expected resolution to fail when requesting value type for pointer registration")
		}
	})

	t.Run("Value registered, pointer requested - should fail", func(t *testing.T) {
		container := createTestContainer()

		// Register as TestStruct value
		err := container.Register(common.ServiceDefinition{
			Name: "test-struct-val2",
			Type: TestStruct{},
			Constructor: func() TestStruct {
				return TestStruct{Name: "test"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Try to resolve as *TestStruct - should fail
		_, err = container.Resolve((*TestStruct)(nil))
		if err == nil {
			t.Error("Expected resolution to fail when requesting pointer type for value registration")
		}
	})
}

func TestReferenceNameGeneration(t *testing.T) {
	t.Run("Reference names generated for named service", func(t *testing.T) {
		container := createTestContainer()
		// Register a named service
		err := container.Register(common.ServiceDefinition{
			Name: "my-test-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "test"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Check that reference mappings were created
		mappings := container.GetReferenceMappings()

		// Should have mappings for various forms of the type name
		found := false
		for refName, actualName := range mappings {
			if actualName == "my-test-service" {
				t.Logf("Found reference mapping: %s -> %s", refName, actualName)
				found = true
			}
		}

		if !found {
			t.Error("No reference mappings found for registered service")
		}
	})

	t.Run("Reference name resolution", func(t *testing.T) {
		container := createTestContainer()
		// Register a service
		err := container.Register(common.ServiceDefinition{
			Name: "ref-test-service",
			Type: (*AnotherStruct)(nil),
			Constructor: func() *AnotherStruct {
				return &AnotherStruct{Value: 123}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Try to resolve by a reference name that should be auto-generated
		mappings := container.GetReferenceMappings()
		var testRefName string
		for refName, actualName := range mappings {
			if actualName == "ref-test-service" {
				testRefName = refName
				break
			}
		}

		if testRefName == "" {
			t.Skip("No reference name found to test with")
		}

		// Resolve by reference name
		instance, err := container.ResolveNamed(testRefName)
		if err != nil {
			t.Fatalf("Failed to resolve by reference name '%s': %v", testRefName, err)
		}

		anotherStruct, ok := instance.(*AnotherStruct)
		if !ok {
			t.Fatalf("Expected *AnotherStruct, got %T", instance)
		}

		if anotherStruct.Value != 123 {
			t.Errorf("Expected value 123, got %d", anotherStruct.Value)
		}
	})
}

func TestSingletonBehavior(t *testing.T) {
	t.Run("Singleton instances are reused", func(t *testing.T) {
		container := createTestContainer()
		counter := 0
		err := container.Register(common.ServiceDefinition{
			Name: "singleton-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				counter++
				return &TestStruct{Name: "singleton"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Resolve twice
		instance1, err := container.ResolveNamed("singleton-service")
		if err != nil {
			t.Fatalf("First resolution failed: %v", err)
		}

		instance2, err := container.ResolveNamed("singleton-service")
		if err != nil {
			t.Fatalf("Second resolution failed: %v", err)
		}

		// Should be the same instance
		if instance1 != instance2 {
			t.Error("Singleton instances should be identical")
		}

		// Constructor should only be called once
		if counter != 1 {
			t.Errorf("Expected constructor to be called once, called %d times", counter)
		}
	})

	t.Run("Non-singleton instances are recreated", func(t *testing.T) {
		container := createTestContainer()
		counter := 0
		err := container.Register(common.ServiceDefinition{
			Name: "transient-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				counter++
				return &TestStruct{Name: "transient"}
			},
			Singleton: false,
		})
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}

		// Resolve twice
		instance1, err := container.ResolveNamed("transient-service")
		if err != nil {
			t.Fatalf("First resolution failed: %v", err)
		}

		instance2, err := container.ResolveNamed("transient-service")
		if err != nil {
			t.Fatalf("Second resolution failed: %v", err)
		}

		// Should be different instances
		if instance1 == instance2 {
			t.Error("Non-singleton instances should be different")
		}

		// Constructor should be called twice
		if counter != 2 {
			t.Errorf("Expected constructor to be called twice, called %d times", counter)
		}
	})
}

func TestCircularDependencyDetection(t *testing.T) {
	// Interfaces for cycle detection test
	type ServiceA interface{}
	type ServiceB interface{}
	type ServiceC interface{}

	t.Run("Should detect a direct circular dependency", func(t *testing.T) {
		container := createTestContainer()

		// Register first service
		err := container.Register(common.ServiceDefinition{
			Name:         "service-a",
			Type:         (*ServiceA)(nil),
			Constructor:  func(b ServiceB) ServiceA { return struct{}{} },
			Dependencies: []string{"service-b"},
		})
		if err != nil {
			t.Fatalf("Failed to register service-a: %v", err)
		}

		// Register second service - this should fail due to circular dependency
		err = container.Register(common.ServiceDefinition{
			Name:         "service-b",
			Type:         (*ServiceB)(nil),
			Constructor:  func(a ServiceA) ServiceB { return struct{}{} },
			Dependencies: []string{"service-a"},
		})
		if err == nil {
			t.Fatal("Expected circular dependency error during registration, but got nil")
		}

		var forgeErr *common.ForgeError
		if !errors.As(err, &forgeErr) {
			t.Errorf("Expected ForgeError, got %T: %v", err, err)
		}

		// Check if it's a container error with circular dependency cause
		if forgeErr.Code != "CONTAINER_ERROR" {
			t.Errorf("Expected CONTAINER_ERROR, got %s", forgeErr.Code)
		}

		// Check the cause is a circular dependency
		if forgeErr.Cause == nil {
			t.Error("Expected cause to be set")
		} else {
			var causeErr *common.ForgeError
			if !errors.As(forgeErr.Cause, &causeErr) || causeErr.Code != common.ErrCodeCircularDependency {
				t.Errorf("Expected cause to be ErrCircularDependency, got %T: %v", forgeErr.Cause, forgeErr.Cause)
			}
		}
		t.Logf("Correctly detected error: %v", err)
	})

	t.Run("Should detect a longer transitive circular dependency", func(t *testing.T) {
		container := createTestContainer()

		// Register services in order: A, B, then C (which creates A->B->C->A cycle)
		err := container.Register(common.ServiceDefinition{
			Name:         "service-a",
			Type:         (*ServiceA)(nil),
			Constructor:  func(b ServiceB) ServiceA { return struct{}{} },
			Dependencies: []string{"service-b"},
		})
		if err != nil {
			t.Fatalf("Failed to register service-a: %v", err)
		}

		// B -> C (this should fail because A->B and B->C creates A->B->C, but C will depend on A)
		err = container.Register(common.ServiceDefinition{
			Name:         "service-b",
			Type:         (*ServiceB)(nil),
			Constructor:  func(c ServiceC) ServiceB { return struct{}{} },
			Dependencies: []string{"service-c"},
		})
		if err == nil {
			t.Fatal("Expected circular dependency error when registering service-b, but got nil")
		}

		var forgeErr *common.ForgeError
		if !errors.As(err, &forgeErr) {
			t.Errorf("Expected ForgeError, got %T: %v", err, err)
		}

		// Check if it's a container error with circular dependency cause
		if forgeErr.Code != "CONTAINER_ERROR" {
			t.Errorf("Expected CONTAINER_ERROR, got %s", forgeErr.Code)
		}

		// Check the cause is a circular dependency
		if forgeErr.Cause == nil {
			t.Error("Expected cause to be set")
		} else {
			var causeErr *common.ForgeError
			if !errors.As(forgeErr.Cause, &causeErr) || causeErr.Code != common.ErrCodeCircularDependency {
				t.Errorf("Expected cause to be ErrCircularDependency, got %T: %v", forgeErr.Cause, forgeErr.Cause)
			}
		}
		t.Logf("Correctly detected error: %v", err)
	})

	t.Run("Should pass validation for a valid dependency graph (DAG)", func(t *testing.T) {
		container := createTestContainer()
		// A -> B
		_ = container.Register(common.ServiceDefinition{
			Name:         "service-a",
			Type:         (*ServiceA)(nil),
			Constructor:  func(b ServiceB) ServiceA { return struct{}{} },
			Dependencies: []string{"service-b"},
		})
		// C -> B
		_ = container.Register(common.ServiceDefinition{
			Name:         "service-c",
			Type:         (*ServiceC)(nil),
			Constructor:  func(b ServiceB) ServiceC { return struct{}{} },
			Dependencies: []string{"service-b"},
		})
		// B (no dependencies)
		_ = container.Register(common.ServiceDefinition{
			Name:        "service-b",
			Type:        (*ServiceB)(nil),
			Constructor: func() ServiceB { return struct{}{} },
		})

		// This is a valid Directed Acyclic Graph (DAG), so validation should pass.
		err := container.validator.ValidateAll()
		if err != nil {
			t.Fatalf("Expected validation to pass for a valid graph, but got error: %v", err)
		}
	})
}

func TestContainerLifecycle(t *testing.T) {
	t.Run("Registration after start should fail", func(t *testing.T) {
		container := createTestContainer()
		// Start the container
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
			t.Error("Expected registration to fail after container start")
		}

		// Stop the container
		container.Stop(context.Background())
	})
}
