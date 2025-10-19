package di

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/xraph/forge/pkg/common"
)

// Test types for integration testing
type IntegrationServiceA struct {
	Name string
	B    *IntegrationServiceB
}

type IntegrationServiceB struct {
	Value int
	C     *IntegrationServiceC
}

type IntegrationServiceC struct {
	Message string
}

type IntegrationServiceD struct {
	Data string
}

// Test interfaces
type IntegrationInterface interface {
	DoSomething() string
}

func (s *IntegrationServiceA) DoSomething() string {
	return "ServiceA: " + s.Name
}

func (s *IntegrationServiceB) DoSomething() string {
	return "ServiceB: " + string(rune(s.Value))
}

func (s *IntegrationServiceC) DoSomething() string {
	return "ServiceC: " + s.Message
}

func (s *IntegrationServiceD) DoSomething() string {
	return "ServiceD: " + s.Data
}

// Mock lifecycle service for integration testing
type MockIntegrationLifecycleService struct {
	name         string
	dependencies []string
	startCount   int
	stopCount    int
	startError   error
	stopError    error
	mu           sync.RWMutex
}

func (m *MockIntegrationLifecycleService) GetName() string {
	return m.name
}

func (m *MockIntegrationLifecycleService) GetDependencies() []string {
	return m.dependencies
}

func (m *MockIntegrationLifecycleService) OnStart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCount++
	return m.startError
}

func (m *MockIntegrationLifecycleService) OnStop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCount++
	return m.stopError
}

func (m *MockIntegrationLifecycleService) GetStartCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.startCount
}

func (m *MockIntegrationLifecycleService) GetStopCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stopCount
}

func TestIntegrationEndToEnd(t *testing.T) {
	t.Run("Complete service lifecycle", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Register services with dependencies
		err := container.Register(common.ServiceDefinition{
			Name: "service-c",
			Type: (*IntegrationServiceC)(nil),
			Constructor: func() *IntegrationServiceC {
				return &IntegrationServiceC{Message: "Hello from C"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service C: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name:         "service-b",
			Type:         (*IntegrationServiceB)(nil),
			Constructor:  func(c *IntegrationServiceC) *IntegrationServiceB { return &IntegrationServiceB{Value: 42, C: c} },
			Dependencies: []string{"service-c"},
		})
		if err != nil {
			t.Fatalf("Failed to register service B: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name:         "service-a",
			Type:         (*IntegrationServiceA)(nil),
			Constructor:  func(b *IntegrationServiceB) *IntegrationServiceA { return &IntegrationServiceA{Name: "Test", B: b} },
			Dependencies: []string{"service-b"},
		})
		if err != nil {
			t.Fatalf("Failed to register service A: %v", err)
		}

		// Start container
		ctx := context.Background()
		err = container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Resolve services
		serviceA, err := container.Resolve((*IntegrationServiceA)(nil))
		if err != nil {
			t.Fatalf("Failed to resolve service A: %v", err)
		}

		serviceB, err := container.Resolve((*IntegrationServiceB)(nil))
		if err != nil {
			t.Fatalf("Failed to resolve service B: %v", err)
		}

		serviceC, err := container.Resolve((*IntegrationServiceC)(nil))
		if err != nil {
			t.Fatalf("Failed to resolve service C: %v", err)
		}

		// Verify service instances
		if serviceA == nil {
			t.Error("Service A should not be nil")
		}
		if serviceB == nil {
			t.Error("Service B should not be nil")
		}
		if serviceC == nil {
			t.Error("Service C should not be nil")
		}

		// Verify dependency injection
		a := serviceA.(*IntegrationServiceA)
		if a.B == nil {
			t.Error("Service A should have dependency B injected")
		}
		if a.B.C == nil {
			t.Error("Service B should have dependency C injected")
		}

		// Verify data integrity
		if a.Name != "Test" {
			t.Errorf("Expected Name 'Test', got '%s'", a.Name)
		}
		if a.B.Value != 42 {
			t.Errorf("Expected Value 42, got %d", a.B.Value)
		}
		if a.B.C.Message != "Hello from C" {
			t.Errorf("Expected Message 'Hello from C', got '%s'", a.B.C.Message)
		}

		// Stop container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})

	t.Run("Interface resolution", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Register services implementing the same interface
		err := container.Register(common.ServiceDefinition{
			Name: "service-a",
			Type: (*IntegrationServiceA)(nil),
			Constructor: func() *IntegrationServiceA {
				return &IntegrationServiceA{Name: "A"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service A: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name: "service-b",
			Type: (*IntegrationServiceB)(nil),
			Constructor: func() *IntegrationServiceB {
				return &IntegrationServiceB{Value: 100}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service B: %v", err)
		}

		// Start container
		ctx := context.Background()
		err = container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Resolve by interface
		interfaceA, err := container.Resolve((*IntegrationInterface)(nil))
		if err != nil {
			t.Fatalf("Failed to resolve interface A: %v", err)
		}

		interfaceB, err := container.Resolve((*IntegrationInterface)(nil))
		if err != nil {
			t.Fatalf("Failed to resolve interface B: %v", err)
		}

		// Verify interface implementations
		if interfaceA == nil {
			t.Error("Interface A should not be nil")
		}
		if interfaceB == nil {
			t.Error("Interface B should not be nil")
		}

		// Verify they are different instances
		if interfaceA == interfaceB {
			t.Error("Interface instances should be different")
		}

		// Test interface methods
		resultA := interfaceA.(IntegrationInterface).DoSomething()
		resultB := interfaceB.(IntegrationInterface).DoSomething()

		if resultA == "" {
			t.Error("Interface A should return non-empty string")
		}
		if resultB == "" {
			t.Error("Interface B should return non-empty string")
		}

		// Stop container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})

	t.Run("Named service resolution", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Register named services
		err := container.Register(common.ServiceDefinition{
			Name: "primary-service",
			Type: (*IntegrationServiceA)(nil),
			Constructor: func() *IntegrationServiceA {
				return &IntegrationServiceA{Name: "Primary"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register primary service: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name: "secondary-service",
			Type: (*IntegrationServiceB)(nil),
			Constructor: func() *IntegrationServiceB {
				return &IntegrationServiceB{Value: 200}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register secondary service: %v", err)
		}

		// Start container
		ctx := context.Background()
		err = container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Resolve by name
		primary, err := container.ResolveNamed("primary-service")
		if err != nil {
			t.Fatalf("Failed to resolve primary service: %v", err)
		}

		secondary, err := container.ResolveNamed("secondary-service")
		if err != nil {
			t.Fatalf("Failed to resolve secondary service: %v", err)
		}

		// Verify named services
		if primary == nil {
			t.Error("Primary service should not be nil")
		}
		if secondary == nil {
			t.Error("Secondary service should not be nil")
		}

		// Verify they are different types
		if primary == secondary {
			t.Error("Primary and secondary services should be different")
		}

		// Stop container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})
}

func TestIntegrationConcurrentAccess(t *testing.T) {
	t.Run("Concurrent service resolution", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Register services
		err := container.Register(common.ServiceDefinition{
			Name: "service-a",
			Type: (*IntegrationServiceA)(nil),
			Constructor: func() *IntegrationServiceA {
				return &IntegrationServiceA{Name: "Concurrent A"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service A: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name: "service-b",
			Type: (*IntegrationServiceB)(nil),
			Constructor: func() *IntegrationServiceB {
				return &IntegrationServiceB{Value: 300}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service B: %v", err)
		}

		// Start container
		ctx := context.Background()
		err = container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Concurrent resolution
		const numGoroutines = 10
		const numResolutions = 100

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines*numResolutions)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < numResolutions; j++ {
					// Resolve service A
					serviceA, err := container.Resolve((*IntegrationServiceA)(nil))
					if err != nil {
						errors <- err
						return
					}
					if serviceA == nil {
						errors <- fmt.Errorf("goroutine %d: service A is nil", goroutineID)
						return
					}

					// Resolve service B
					serviceB, err := container.Resolve((*IntegrationServiceB)(nil))
					if err != nil {
						errors <- err
						return
					}
					if serviceB == nil {
						errors <- fmt.Errorf("goroutine %d: service B is nil", goroutineID)
						return
					}

					// Verify service resolution consistency
					serviceA2, err := container.Resolve((*IntegrationServiceA)(nil))
					if err != nil {
						errors <- err
						return
					}
					if serviceA2 == nil {
						errors <- fmt.Errorf("goroutine %d: service A2 should not be nil", goroutineID)
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent resolution error: %v", err)
		}
	})

	t.Run("Concurrent container operations", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Register initial services
		err := container.Register(common.ServiceDefinition{
			Name: "service-a",
			Type: (*IntegrationServiceA)(nil),
			Constructor: func() *IntegrationServiceA {
				return &IntegrationServiceA{Name: "Concurrent A"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service A: %v", err)
		}

		// Start container
		ctx := context.Background()
		err = container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Concurrent operations
		const numGoroutines = 5
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines*2)

		// Goroutine 1: Resolve services
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, err := container.Resolve((*IntegrationServiceA)(nil))
				if err != nil {
					errors <- err
					return
				}
			}
		}()

		// Goroutine 2: Resolve services by name
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, err := container.ResolveNamed("service-a")
				if err != nil {
					errors <- err
					return
				}
			}
		}()

		// Goroutine 3: Get container stats
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				stats := container.(*Container).GetStats()
				if stats.ServicesRegistered == 0 {
					errors <- fmt.Errorf("stats should have registered services")
					return
				}
			}
		}()

		// Goroutine 4: Check if container is started
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if !container.(*Container).IsStarted() {
					errors <- fmt.Errorf("container should be started")
					return
				}
			}
		}()

		// Goroutine 5: Get services list
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				services := container.Services()
				if len(services) == 0 {
					errors <- fmt.Errorf("services should not be empty")
					return
				}
			}
		}()

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent operation error: %v", err)
		}

		// Stop container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})
}

func TestIntegrationErrorHandling(t *testing.T) {
	t.Run("Service resolution errors", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Start container
		ctx := context.Background()
		err := container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Try to resolve non-existent service
		_, err = container.Resolve((*IntegrationServiceA)(nil))
		if err == nil {
			t.Error("Expected error when resolving non-existent service")
		}

		// Try to resolve non-existent named service
		_, err = container.ResolveNamed("non-existent")
		if err == nil {
			t.Error("Expected error when resolving non-existent named service")
		}

		// Stop container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})

	t.Run("Container lifecycle errors", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Try to start container twice
		ctx := context.Background()
		err := container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		err = container.Start(ctx)
		if err == nil {
			t.Error("Expected error when starting container twice")
		}

		// Try to stop container before starting
		container2 := NewContainer(ContainerConfig{})
		err = container2.Stop(ctx)
		if err == nil {
			t.Error("Expected error when stopping container before starting")
		}

		// Stop the started container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})
}

func TestIntegrationComplexScenarios(t *testing.T) {
	t.Run("Deep dependency chain", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Create a deep dependency chain: A -> B -> C -> D
		err := container.Register(common.ServiceDefinition{
			Name: "service-d",
			Type: (*IntegrationServiceD)(nil),
			Constructor: func() *IntegrationServiceD {
				return &IntegrationServiceD{Data: "Deep D"}
			},
		})
		if err != nil {
			t.Fatalf("Failed to register service D: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name:         "service-c",
			Type:         (*IntegrationServiceC)(nil),
			Constructor:  func(d *IntegrationServiceD) *IntegrationServiceC { return &IntegrationServiceC{Message: "Deep C"} },
			Dependencies: []string{"service-d"},
		})
		if err != nil {
			t.Fatalf("Failed to register service C: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name:         "service-b",
			Type:         (*IntegrationServiceB)(nil),
			Constructor:  func(c *IntegrationServiceC) *IntegrationServiceB { return &IntegrationServiceB{Value: 400, C: c} },
			Dependencies: []string{"service-c"},
		})
		if err != nil {
			t.Fatalf("Failed to register service B: %v", err)
		}

		err = container.Register(common.ServiceDefinition{
			Name:         "service-a",
			Type:         (*IntegrationServiceA)(nil),
			Constructor:  func(b *IntegrationServiceB) *IntegrationServiceA { return &IntegrationServiceA{Name: "Deep A", B: b} },
			Dependencies: []string{"service-b"},
		})
		if err != nil {
			t.Fatalf("Failed to register service A: %v", err)
		}

		// Start container
		ctx := context.Background()
		err = container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Resolve the top-level service
		serviceA, err := container.Resolve((*IntegrationServiceA)(nil))
		if err != nil {
			t.Fatalf("Failed to resolve service A: %v", err)
		}

		// Verify the entire chain
		a := serviceA.(*IntegrationServiceA)
		if a.B == nil {
			t.Error("Service A should have dependency B")
		}
		if a.B.C == nil {
			t.Error("Service B should have dependency C")
		}

		// Verify data integrity through the chain
		if a.Name != "Deep A" {
			t.Errorf("Expected Name 'Deep A', got '%s'", a.Name)
		}
		if a.B.Value != 400 {
			t.Errorf("Expected Value 400, got %d", a.B.Value)
		}
		if a.B.C.Message != "Deep C" {
			t.Errorf("Expected Message 'Deep C', got '%s'", a.B.C.Message)
		}

		// Stop container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})

	t.Run("Multiple services with same interface", func(t *testing.T) {
		container := NewContainer(ContainerConfig{})

		// Register multiple services implementing the same interface
		services := []struct {
			name  string
			value string
		}{
			{"service-a", "A"},
			{"service-b", "B"},
			{"service-c", "C"},
			{"service-d", "D"},
		}

		for _, svc := range services {
			err := container.Register(common.ServiceDefinition{
				Name: svc.name,
				Type: (*IntegrationServiceA)(nil),
				Constructor: func() *IntegrationServiceA {
					return &IntegrationServiceA{Name: svc.value}
				},
			})
			if err != nil {
				t.Fatalf("Failed to register %s: %v", svc.name, err)
			}
		}

		// Start container
		ctx := context.Background()
		err := container.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start container: %v", err)
		}

		// Resolve all services
		resolvedServices := make([]*IntegrationServiceA, len(services))
		for i, svc := range services {
			service, err := container.ResolveNamed(svc.name)
			if err != nil {
				t.Fatalf("Failed to resolve %s: %v", svc.name, err)
			}
			resolvedServices[i] = service.(*IntegrationServiceA)
		}

		// Verify all services are different instances
		for i := 0; i < len(resolvedServices); i++ {
			for j := i + 1; j < len(resolvedServices); j++ {
				if resolvedServices[i] == resolvedServices[j] {
					t.Errorf("Services %d and %d should be different instances", i, j)
				}
			}
		}

		// Verify data integrity
		for i, svc := range services {
			if resolvedServices[i].Name != svc.value {
				t.Errorf("Expected Name '%s', got '%s'", svc.value, resolvedServices[i].Name)
			}
		}

		// Stop container
		err = container.Stop(ctx)
		if err != nil {
			t.Fatalf("Failed to stop container: %v", err)
		}
	})
}
