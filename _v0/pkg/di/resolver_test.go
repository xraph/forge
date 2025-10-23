package di

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xraph/forge/v0/pkg/common"
)

// Mock configurable service for testing
type MockConfigurableService struct {
	ConfigValue string
	Configured  bool
}

func (m *MockConfigurableService) Configure(ctx context.Context, config interface{}) error {
	// Handle map[string]interface{}
	if configMap, ok := config.(map[string]interface{}); ok {
		if value, exists := configMap["value"]; exists {
			if str, ok := value.(string); ok {
				m.ConfigValue = str
				m.Configured = true
			}
		}
		return nil
	}

	// Handle struct with Value field
	configValue := reflect.ValueOf(config)
	if configValue.Kind() == reflect.Struct {
		valueField := configValue.FieldByName("Value")
		if valueField.IsValid() && valueField.CanInterface() {
			if str, ok := valueField.Interface().(string); ok {
				m.ConfigValue = str
				m.Configured = true
			}
		}
	}

	return nil
}

// Mock service with SetConfig method
type MockSetConfigService struct {
	ConfigValue string
	Configured  bool
}

func (m *MockSetConfigService) SetConfig(config interface{}) {
	// Handle map[string]interface{}
	if configMap, ok := config.(map[string]interface{}); ok {
		if value, exists := configMap["value"]; exists {
			if str, ok := value.(string); ok {
				m.ConfigValue = str
				m.Configured = true
			}
		}
		return
	}

	// Handle struct with Value field
	configValue := reflect.ValueOf(config)
	if configValue.Kind() == reflect.Struct {
		valueField := configValue.FieldByName("Value")
		if valueField.IsValid() && valueField.CanInterface() {
			if str, ok := valueField.Interface().(string); ok {
				m.ConfigValue = str
				m.Configured = true
			}
		}
	}
}

// Mock service with field-based configuration
type MockFieldConfigService struct {
	Value      string
	Number     int
	Enabled    bool
	Configured bool
}

// Mock config manager for testing
type MockConfigManager struct {
	configs map[string]interface{}
}

func (m *MockConfigManager) Get(key string) interface{} {
	return m.configs[key]
}

func (m *MockConfigManager) Set(key string, value interface{}) {
	m.configs[key] = value
}

func (m *MockConfigManager) Bind(key string, target interface{}) error {
	if config, exists := m.configs[key]; exists {
		// Simple reflection-based binding for testing
		targetValue := reflect.ValueOf(target)
		if targetValue.Kind() == reflect.Ptr {
			targetValue = targetValue.Elem()
		}
		configValue := reflect.ValueOf(config)
		if configValue.Kind() == reflect.Ptr {
			configValue = configValue.Elem()
		}
		if targetValue.Type() == configValue.Type() {
			targetValue.Set(configValue)
		}
	}
	return nil
}

func (m *MockConfigManager) Watch(key string, callback func(interface{})) error {
	return nil
}

func (m *MockConfigManager) Unwatch(key string) error {
	return nil
}

func (m *MockConfigManager) Keys() []string {
	keys := make([]string, 0, len(m.configs))
	for k := range m.configs {
		keys = append(keys, k)
	}
	return keys
}

func (m *MockConfigManager) Clear() {
	m.configs = make(map[string]interface{})
}

func (m *MockConfigManager) Has(key string) bool {
	_, exists := m.configs[key]
	return exists
}

func (m *MockConfigManager) Delete(key string) {
	delete(m.configs, key)
}

func (m *MockConfigManager) Size() int {
	return len(m.configs)
}

func (m *MockConfigManager) All() map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m.configs {
		result[k] = v
	}
	return result
}

func (m *MockConfigManager) AllKeys() []string {
	return m.Keys()
}

func (m *MockConfigManager) AllSettings() map[string]interface{} {
	return m.All()
}

func (m *MockConfigManager) GetAllSettings() map[string]interface{} {
	return m.All()
}

func createTestResolver() *Resolver {
	container := createTestContainer()
	return NewResolver(container)
}

func createTestConfigurator() *Configurator {
	container := createTestContainer()
	return NewConfigurator(container, nil)
}

func TestResolverInstanceCreation(t *testing.T) {
	t.Run("CreateInstance method", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

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

		// Get the registration
		registration := container.namedServices["test-service"]

		// Test creating instance
		instance, err := resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create instance: %v", err)
		}

		testStruct, ok := instance.(*TestStruct)
		if !ok {
			t.Fatalf("Expected *TestStruct, got %T", instance)
		}
		if testStruct.Name != "test" {
			t.Errorf("Expected name 'test', got '%s'", testStruct.Name)
		}
	})

	t.Run("CreateInstance with singleton caching", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a singleton service
		err := container.Register(common.ServiceDefinition{
			Name: "singleton-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "singleton"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["singleton-service"]

		// Create instance twice
		instance1, err := resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create first instance: %v", err)
		}

		instance2, err := resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create second instance: %v", err)
		}

		// Should be the same instance for singleton
		if instance1 != instance2 {
			t.Error("Expected same instance for singleton service")
		}
	})

	t.Run("CreateInstance with non-singleton", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a non-singleton service
		err := container.Register(common.ServiceDefinition{
			Name: "transient-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "transient"}
			},
			Singleton: false,
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["transient-service"]

		// Create instance twice
		instance1, err := resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create first instance: %v", err)
		}

		instance2, err := resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create second instance: %v", err)
		}

		// Should be different instances for non-singleton
		if instance1 == instance2 {
			t.Error("Expected different instances for non-singleton service")
		}
	})

	t.Run("CreateInstance with existing instance", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service with existing instance
		existingInstance := &TestStruct{Name: "existing"}
		err := container.Register(common.ServiceDefinition{
			Name:     "existing-service",
			Type:     (*TestStruct)(nil),
			Instance: existingInstance,
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["existing-service"]

		// Test creating instance
		instance, err := resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create instance: %v", err)
		}

		// Should return the existing instance
		if instance != existingInstance {
			t.Error("Expected existing instance to be returned")
		}
	})

	t.Run("CreateInstance with constructor error", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service with constructor that returns error
		err := container.Register(common.ServiceDefinition{
			Name: "error-service",
			Type: (*TestStruct)(nil),
			Constructor: func() (*TestStruct, error) {
				return nil, errors.New("constructor error")
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["error-service"]

		// Test creating instance
		_, err = resolver.CreateInstance(context.Background(), registration)
		if err == nil {
			t.Error("Expected error when creating instance with constructor error")
		}
	})

	t.Run("CreateInstance with no constructor", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service without constructor
		err := container.Register(common.ServiceDefinition{
			Name: "no-constructor-service",
			Type: (*TestStruct)(nil),
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["no-constructor-service"]

		// Test creating instance
		_, err = resolver.CreateInstance(context.Background(), registration)
		if err == nil {
			t.Error("Expected error when creating instance without constructor")
		}
	})
}

func TestResolverDependencyResolution(t *testing.T) {
	t.Run("resolveDependencyExact with exact type match", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service by type
		err := container.Register(common.ServiceDefinition{
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "exact-match"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Test resolving by exact type
		instance, err := resolver.resolveDependencyExact(context.Background(), reflect.TypeOf((*TestStruct)(nil)))
		if err != nil {
			t.Fatalf("Failed to resolve dependency: %v", err)
		}

		testStruct, ok := instance.(*TestStruct)
		if !ok {
			t.Fatalf("Expected *TestStruct, got %T", instance)
		}
		if testStruct.Name != "exact-match" {
			t.Errorf("Expected name 'exact-match', got '%s'", testStruct.Name)
		}
	})

	t.Run("resolveDependencyExact with interface match", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service that implements an interface
		err := container.Register(common.ServiceDefinition{
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "interface-match"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Test resolving by interface - this should fail because the resolver
		// doesn't automatically find services that implement interfaces
		_, err = resolver.resolveDependencyExact(context.Background(), reflect.TypeOf((*TestService)(nil)))
		if err == nil {
			t.Error("Expected error when resolving by interface without explicit registration")
		}
	})

	t.Run("resolveDependencyExact with reference name", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a named service
		err := container.Register(common.ServiceDefinition{
			Name: "ref-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "ref-match"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// Add reference mapping
		err = container.AddReferenceMapping("TestStruct", "ref-service")
		if err != nil {
			t.Fatalf("Failed to add reference mapping: %v", err)
		}

		// Test resolving by reference name
		instance, err := resolver.resolveDependencyExact(context.Background(), reflect.TypeOf((*TestStruct)(nil)))
		if err != nil {
			t.Fatalf("Failed to resolve dependency: %v", err)
		}

		testStruct, ok := instance.(*TestStruct)
		if !ok {
			t.Fatalf("Expected *TestStruct, got %T", instance)
		}
		if testStruct.Name != "ref-match" {
			t.Errorf("Expected name 'ref-match', got '%s'", testStruct.Name)
		}
	})

	t.Run("resolveDependencyExact with missing dependency", func(t *testing.T) {
		resolver := createTestResolver()

		// Test resolving non-existent dependency
		_, err := resolver.resolveDependencyExact(context.Background(), reflect.TypeOf((*TestStruct)(nil)))
		if err == nil {
			t.Error("Expected error when resolving missing dependency")
		}
	})

	t.Run("implementsInterface method", func(t *testing.T) {
		resolver := createTestResolver()

		// Test direct implementation
		implType := reflect.TypeOf(TestStruct{})
		interfaceType := reflect.TypeOf((*TestService)(nil)).Elem()

		if !resolver.implementsInterface(implType, interfaceType) {
			t.Error("Expected TestStruct to implement TestService")
		}

		// Test pointer implementation
		ptrType := reflect.TypeOf((*TestStruct)(nil))
		if !resolver.implementsInterface(ptrType, interfaceType) {
			t.Error("Expected *TestStruct to implement TestService")
		}

		// Test non-implementation
		nonImplType := reflect.TypeOf(AnotherStruct{})
		if resolver.implementsInterface(nonImplType, interfaceType) {
			t.Error("Expected AnotherStruct to not implement TestService")
		}
	})
}

func TestResolverCacheManagement(t *testing.T) {
	t.Run("ClearCache method", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a singleton service
		err := container.Register(common.ServiceDefinition{
			Name: "cache-service",
			Type: (*TestStruct)(nil),
			Constructor: func() *TestStruct {
				return &TestStruct{Name: "cached"}
			},
			Singleton: true,
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["cache-service"]

		// Create instance to populate cache
		_, err = resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create instance: %v", err)
		}

		// Verify cache has entries
		stats := resolver.GetCacheStats()
		if stats["cached_instances"].(int) == 0 {
			t.Error("Expected cache to have entries")
		}

		// Clear cache
		resolver.ClearCache()

		// Verify cache is empty
		stats = resolver.GetCacheStats()
		if stats["cached_instances"].(int) != 0 {
			t.Error("Expected cache to be empty after clear")
		}
	})

	t.Run("GetCacheStats method", func(t *testing.T) {
		resolver := createTestResolver()

		// Test initial stats
		stats := resolver.GetCacheStats()
		if stats["cached_instances"].(int) != 0 {
			t.Error("Expected initial cache to be empty")
		}

		// Verify cache keys is an array
		keys, ok := stats["cache_keys"].([]string)
		if !ok {
			t.Error("Expected cache_keys to be []string")
		}
		if len(keys) != 0 {
			t.Error("Expected initial cache keys to be empty")
		}
	})
}

func TestResolverEdgeCases(t *testing.T) {
	t.Run("CreateInstance with invalid constructor", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service with invalid constructor (not a function)
		err := container.Register(common.ServiceDefinition{
			Name:        "invalid-constructor",
			Type:        (*TestStruct)(nil),
			Constructor: "not-a-function",
		})
		if err == nil {
			t.Error("Expected error when registering service with invalid constructor")
		}
		// The error is caught during registration, so we can't test CreateInstance
	})

	t.Run("CreateInstance with constructor returning no values", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service with constructor returning no values
		err := container.Register(common.ServiceDefinition{
			Name: "no-return-constructor",
			Type: (*TestStruct)(nil),
			Constructor: func() {
				// No return values
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["no-return-constructor"]

		// Test creating instance
		_, err = resolver.CreateInstance(context.Background(), registration)
		if err == nil {
			t.Error("Expected error when creating instance with no return constructor")
		}
	})

	t.Run("CreateInstance with context dependencies", func(t *testing.T) {
		resolver := createTestResolver()
		container := resolver.container

		// Register a service with context dependencies
		err := container.Register(common.ServiceDefinition{
			Name: "context-service",
			Type: (*TestStruct)(nil),
			Constructor: func(ctx context.Context) *TestStruct {
				return &TestStruct{Name: "context-aware"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		registration := container.namedServices["context-service"]

		// Test creating instance
		instance, err := resolver.CreateInstance(context.Background(), registration)
		if err != nil {
			t.Fatalf("Failed to create instance: %v", err)
		}

		testStruct, ok := instance.(*TestStruct)
		if !ok {
			t.Fatalf("Expected *TestStruct, got %T", instance)
		}
		if testStruct.Name != "context-aware" {
			t.Errorf("Expected name 'context-aware', got '%s'", testStruct.Name)
		}
	})
}
