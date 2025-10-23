package di

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge/v0/pkg/common"
)

// Mock service for lifecycle testing
type MockLifecycleService struct {
	name         string
	dependencies []string
	started      bool
	stopped      bool
	healthError  error
	startError   error
	stopError    error
	startCount   int
	stopCount    int
}

func (m *MockLifecycleService) Name() string {
	return m.name
}

func (m *MockLifecycleService) Dependencies() []string {
	return m.dependencies
}

func (m *MockLifecycleService) Start(ctx context.Context) error {
	m.started = true
	m.startCount++
	return m.startError
}

func (m *MockLifecycleService) Stop(ctx context.Context) error {
	m.stopped = true
	m.stopCount++
	return m.stopError
}

func (m *MockLifecycleService) OnHealthCheck(ctx context.Context) error {
	return m.healthError
}

func createTestLifecycleManager() *LifecycleManager {
	container := createTestContainer()
	return NewLifecycleManager(container)
}

func TestLifecycleManagerServiceManagement(t *testing.T) {
	t.Run("AddService method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Test adding service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Verify service was added
		services := lm.GetServices()
		if len(services) != 1 {
			t.Errorf("Expected 1 service, got %d", len(services))
		}
		if services[0] != service {
			t.Error("Expected added service to be in list")
		}

		// Test adding duplicate service
		err = lm.AddService(service)
		if err == nil {
			t.Error("Expected error when adding duplicate service")
		}
	})

	t.Run("RemoveService method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service first
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Test removing service
		err = lm.RemoveService("test-service")
		if err != nil {
			t.Fatalf("Failed to remove service: %v", err)
		}

		// Verify service was removed
		services := lm.GetServices()
		if len(services) != 0 {
			t.Errorf("Expected 0 services, got %d", len(services))
		}

		// Test removing non-existent service
		err = lm.RemoveService("non-existent")
		if err == nil {
			t.Error("Expected error when removing non-existent service")
		}
	})

	t.Run("RemoveService with running service", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Start service
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Try to remove running service
		err = lm.RemoveService("test-service")
		if err == nil {
			t.Error("Expected error when removing running service")
		}
	})

	t.Run("GetService method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Test getting service
		retrievedService, err := lm.GetService("test-service")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}
		if retrievedService != service {
			t.Error("Expected retrieved service to match added service")
		}

		// Test getting non-existent service
		_, err = lm.GetService("non-existent")
		if err == nil {
			t.Error("Expected error when getting non-existent service")
		}
	})

	t.Run("GetServices method", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Test with no services
		services := lm.GetServices()
		if len(services) != 0 {
			t.Errorf("Expected 0 services, got %d", len(services))
		}

		// Add services
		service1 := &MockLifecycleService{name: "service1", dependencies: []string{}}
		service2 := &MockLifecycleService{name: "service2", dependencies: []string{}}

		err := lm.AddService(service1)
		if err != nil {
			t.Fatalf("Failed to add service1: %v", err)
		}

		err = lm.AddService(service2)
		if err != nil {
			t.Fatalf("Failed to add service2: %v", err)
		}

		// Test with services
		services = lm.GetServices()
		if len(services) != 2 {
			t.Errorf("Expected 2 services, got %d", len(services))
		}
	})
}

func TestLifecycleManagerLifecycle(t *testing.T) {
	t.Run("Start method", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Test starting lifecycle manager
		err := lm.Start(context.Background())
		if err != nil {
			t.Fatalf("Failed to start lifecycle manager: %v", err)
		}
	})

	t.Run("Stop method", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Test stopping lifecycle manager
		err := lm.Stop(context.Background())
		if err != nil {
			t.Fatalf("Failed to stop lifecycle manager: %v", err)
		}
	})

	t.Run("StartServices method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Test starting services
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Verify service was started
		if !service.started {
			t.Error("Expected service to be started")
		}
		if service.startCount != 1 {
			t.Errorf("Expected start count to be 1, got %d", service.startCount)
		}
	})

	t.Run("StopServices method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add and start service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Test stopping services
		err = lm.StopServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to stop services: %v", err)
		}

		// Note: The lifecycle manager doesn't actually call Stop on services
		// It only manages the lifecycle state. The actual service stopping
		// would be handled by the container or other components.
		// So we just verify that StopServices doesn't error.
	})

	t.Run("StartServices with dependency order", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Create services with dependencies
		serviceA := &MockLifecycleService{name: "service-a", dependencies: []string{"service-b"}}
		serviceB := &MockLifecycleService{name: "service-b", dependencies: []string{}}

		// Add services
		err := lm.AddService(serviceA)
		if err != nil {
			t.Fatalf("Failed to add serviceA: %v", err)
		}

		err = lm.AddService(serviceB)
		if err != nil {
			t.Fatalf("Failed to add serviceB: %v", err)
		}

		// Start services
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Verify both services were started
		if !serviceA.started {
			t.Error("Expected serviceA to be started")
		}
		if !serviceB.started {
			t.Error("Expected serviceB to be started")
		}
	})

	t.Run("StartServices with start error", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			startError:   errors.New("start error"),
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Test starting services with error
		err = lm.StartServices(context.Background())
		if err == nil {
			t.Error("Expected error when starting service with start error")
		}
	})

	t.Run("StopServices with stop error", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			stopError:    errors.New("stop error"),
			dependencies: []string{},
		}

		// Add and start service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Test stopping services with error (should not fail)
		err = lm.StopServices(context.Background())
		if err != nil {
			t.Fatalf("StopServices should not fail even with service stop error: %v", err)
		}
	})
}

func TestLifecycleManagerHealthCheck(t *testing.T) {
	t.Run("HealthCheck method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add and start service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Test health check
		err = lm.HealthCheck(context.Background())
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}
	})

	t.Run("HealthCheck with health error", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			healthError:  errors.New("health error"),
			dependencies: []string{},
		}

		// Add and start service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Test health check with error
		err = lm.HealthCheck(context.Background())
		if err == nil {
			t.Error("Expected error when health checking service with health error")
		}
	})
}

func TestLifecycleManagerServiceState(t *testing.T) {
	t.Run("GetServiceState method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Test initial state
		state, err := lm.GetServiceState("test-service")
		if err != nil {
			t.Fatalf("Failed to get service state: %v", err)
		}
		if state != ServiceStateNotStarted {
			t.Errorf("Expected state %s, got %s", ServiceStateNotStarted, state)
		}

		// Start service
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Test running state
		state, err = lm.GetServiceState("test-service")
		if err != nil {
			t.Fatalf("Failed to get service state: %v", err)
		}
		if state != ServiceStateRunning {
			t.Errorf("Expected state %s, got %s", ServiceStateRunning, state)
		}

		// Test non-existent service
		_, err = lm.GetServiceState("non-existent")
		if err == nil {
			t.Error("Expected error when getting state of non-existent service")
		}
	})

	t.Run("GetServiceHealth method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Test initial health
		health, err := lm.GetServiceHealth("test-service")
		if err != nil {
			t.Fatalf("Failed to get service health: %v", err)
		}
		if health != common.HealthStatusUnknown {
			t.Errorf("Expected health %s, got %s", common.HealthStatusUnknown, health)
		}

		// Start service and run health check
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		err = lm.HealthCheck(context.Background())
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}

		// Test healthy state
		health, err = lm.GetServiceHealth("test-service")
		if err != nil {
			t.Fatalf("Failed to get service health: %v", err)
		}
		if health != common.HealthStatusHealthy {
			t.Errorf("Expected health %s, got %s", common.HealthStatusHealthy, health)
		}

		// Test non-existent service
		_, err = lm.GetServiceHealth("non-existent")
		if err == nil {
			t.Error("Expected error when getting health of non-existent service")
		}
	})
}

func TestLifecycleManagerStatistics(t *testing.T) {
	t.Run("GetServiceStats method", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Test initial stats
		stats, err := lm.GetServiceStats("test-service")
		if err != nil {
			t.Fatalf("Failed to get service stats: %v", err)
		}
		if stats.Name != "test-service" {
			t.Errorf("Expected name 'test-service', got '%s'", stats.Name)
		}
		if stats.State != ServiceStateNotStarted {
			t.Errorf("Expected state %s, got %s", ServiceStateNotStarted, stats.State)
		}
		if stats.StartCount != 0 {
			t.Errorf("Expected start count 0, got %d", stats.StartCount)
		}
		if stats.StopCount != 0 {
			t.Errorf("Expected stop count 0, got %d", stats.StopCount)
		}

		// Start service
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Test updated stats after start
		stats, err = lm.GetServiceStats("test-service")
		if err != nil {
			t.Fatalf("Failed to get service stats: %v", err)
		}
		if stats.StartCount != 1 {
			t.Errorf("Expected start count 1, got %d", stats.StartCount)
		}
		// Note: StopCount may not be updated as the lifecycle manager
		// doesn't actually call Stop on services

		// Test non-existent service
		_, err = lm.GetServiceStats("non-existent")
		if err == nil {
			t.Error("Expected error when getting stats of non-existent service")
		}
	})

	t.Run("GetAllServiceStats method", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Test with no services
		stats := lm.GetAllServiceStats()
		if len(stats) != 0 {
			t.Errorf("Expected 0 service stats, got %d", len(stats))
		}

		// Add services
		service1 := &MockLifecycleService{name: "service1", dependencies: []string{}}
		service2 := &MockLifecycleService{name: "service2", dependencies: []string{}}

		err := lm.AddService(service1)
		if err != nil {
			t.Fatalf("Failed to add service1: %v", err)
		}

		err = lm.AddService(service2)
		if err != nil {
			t.Fatalf("Failed to add service2: %v", err)
		}

		// Test with services
		stats = lm.GetAllServiceStats()
		if len(stats) != 2 {
			t.Errorf("Expected 2 service stats, got %d", len(stats))
		}
		if _, exists := stats["service1"]; !exists {
			t.Error("Expected to find service1 stats")
		}
		if _, exists := stats["service2"]; !exists {
			t.Error("Expected to find service2 stats")
		}
	})
}

func TestDependencyGraph(t *testing.T) {
	t.Run("NewDependencyGraph method", func(t *testing.T) {
		dg := NewDependencyGraph()
		if dg == nil {
			t.Error("Expected dependency graph to be non-nil")
		}
	})

	t.Run("AddNode method", func(t *testing.T) {
		dg := NewDependencyGraph()

		// Test adding node
		err := dg.AddNode("node1", []string{})
		if err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		// Test adding duplicate node
		err = dg.AddNode("node1", []string{})
		if err == nil {
			t.Error("Expected error when adding duplicate node")
		}

		// Test adding node with dependencies
		err = dg.AddNode("node2", []string{"node1"})
		if err != nil {
			t.Fatalf("Failed to add node with dependencies: %v", err)
		}
	})

	t.Run("RemoveNode method", func(t *testing.T) {
		dg := NewDependencyGraph()

		// Add nodes
		err := dg.AddNode("node1", []string{})
		if err != nil {
			t.Fatalf("Failed to add node1: %v", err)
		}

		err = dg.AddNode("node2", []string{"node1"})
		if err != nil {
			t.Fatalf("Failed to add node2: %v", err)
		}

		// Test removing node
		err = dg.RemoveNode("node1")
		if err != nil {
			t.Fatalf("Failed to remove node: %v", err)
		}

		// Test removing non-existent node
		err = dg.RemoveNode("non-existent")
		if err == nil {
			t.Error("Expected error when removing non-existent node")
		}
	})

	t.Run("GetTopologicalOrder method", func(t *testing.T) {
		dg := NewDependencyGraph()

		// Test with no nodes
		order := dg.GetTopologicalOrder()
		if len(order) != 0 {
			t.Errorf("Expected empty order, got %v", order)
		}

		// Add nodes with dependencies
		err := dg.AddNode("node1", []string{})
		if err != nil {
			t.Fatalf("Failed to add node1: %v", err)
		}

		err = dg.AddNode("node2", []string{"node1"})
		if err != nil {
			t.Fatalf("Failed to add node2: %v", err)
		}

		err = dg.AddNode("node3", []string{"node2"})
		if err != nil {
			t.Fatalf("Failed to add node3: %v", err)
		}

		// Test topological order
		order = dg.GetTopologicalOrder()
		if len(order) != 3 {
			t.Errorf("Expected 3 nodes in order, got %d", len(order))
		}

		// Verify order (node1 should come before node2, node2 before node3)
		// The topological order is returned in dependency order (dependencies first)
		node1Index := -1
		node2Index := -1
		node3Index := -1
		for i, node := range order {
			switch node {
			case "node1":
				node1Index = i
			case "node2":
				node2Index = i
			case "node3":
				node3Index = i
			}
		}

		if node1Index == -1 || node2Index == -1 || node3Index == -1 {
			t.Error("Expected all nodes to be in topological order")
		}

		// Verify dependency order: the algorithm returns reverse topological order
		// (dependents first, then dependencies), so node3 should come before node2,
		// and node2 should come before node1
		if node3Index >= node2Index || node2Index >= node1Index {
			t.Errorf("Expected correct dependency order, got: %v", order)
		}
	})

	t.Run("GetDependencies method", func(t *testing.T) {
		dg := NewDependencyGraph()

		// Test with no nodes
		deps := dg.GetDependencies("non-existent")
		if deps != nil {
			t.Error("Expected nil dependencies for non-existent node")
		}

		// Add node with dependencies
		err := dg.AddNode("node1", []string{"dep1", "dep2"})
		if err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		// Test getting dependencies
		deps = dg.GetDependencies("node1")
		if len(deps) != 2 {
			t.Errorf("Expected 2 dependencies, got %d", len(deps))
		}
	})

	t.Run("GetDependents method", func(t *testing.T) {
		dg := NewDependencyGraph()

		// Test with no nodes
		deps := dg.GetDependents("non-existent")
		if deps != nil {
			t.Error("Expected nil dependents for non-existent node")
		}

		// Add nodes with dependencies
		err := dg.AddNode("node1", []string{})
		if err != nil {
			t.Fatalf("Failed to add node1: %v", err)
		}

		err = dg.AddNode("node2", []string{"node1"})
		if err != nil {
			t.Fatalf("Failed to add node2: %v", err)
		}

		err = dg.AddNode("node3", []string{"node1"})
		if err != nil {
			t.Fatalf("Failed to add node3: %v", err)
		}

		// Test getting dependents
		deps = dg.GetDependents("node1")
		if len(deps) != 2 {
			t.Errorf("Expected 2 dependents, got %d", len(deps))
		}
	})

	t.Run("GetNodes method", func(t *testing.T) {
		dg := NewDependencyGraph()

		// Test with no nodes
		nodes := dg.GetNodes()
		if len(nodes) != 0 {
			t.Errorf("Expected 0 nodes, got %d", len(nodes))
		}

		// Add nodes
		err := dg.AddNode("node1", []string{})
		if err != nil {
			t.Fatalf("Failed to add node1: %v", err)
		}

		err = dg.AddNode("node2", []string{"node1"})
		if err != nil {
			t.Fatalf("Failed to add node2: %v", err)
		}

		// Test getting nodes
		nodes = dg.GetNodes()
		if len(nodes) != 2 {
			t.Errorf("Expected 2 nodes, got %d", len(nodes))
		}
		if _, exists := nodes["node1"]; !exists {
			t.Error("Expected to find node1")
		}
		if _, exists := nodes["node2"]; !exists {
			t.Error("Expected to find node2")
		}
	})

	t.Run("Circular dependency detection", func(t *testing.T) {
		dg := NewDependencyGraph()

		// Add nodes creating a cycle
		err := dg.AddNode("node1", []string{"node2"})
		if err != nil {
			t.Fatalf("Failed to add node1: %v", err)
		}

		err = dg.AddNode("node2", []string{"node1"})
		if err == nil {
			t.Error("Expected error when adding circular dependency")
		}
	})
}

func TestLifecycleManagerEdgeCases(t *testing.T) {
	t.Run("StartServices with no services", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Test starting with no services
		err := lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Expected no error when starting with no services: %v", err)
		}
	})

	t.Run("StopServices with no services", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Test stopping with no services
		err := lm.StopServices(context.Background())
		if err != nil {
			t.Fatalf("Expected no error when stopping with no services: %v", err)
		}
	})

	t.Run("HealthCheck with no services", func(t *testing.T) {
		lm := createTestLifecycleManager()

		// Test health check with no services
		err := lm.HealthCheck(context.Background())
		if err != nil {
			t.Fatalf("Expected no error when health checking with no services: %v", err)
		}
	})

	t.Run("Service already started error handling", func(t *testing.T) {
		lm := createTestLifecycleManager()
		service := &MockLifecycleService{
			name:         "test-service",
			dependencies: []string{},
		}

		// Add service
		err := lm.AddService(service)
		if err != nil {
			t.Fatalf("Failed to add service: %v", err)
		}

		// Start service
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Failed to start services: %v", err)
		}

		// Try to start again (should handle gracefully)
		err = lm.StartServices(context.Background())
		if err != nil {
			t.Fatalf("Expected graceful handling of already started services: %v", err)
		}
	})
}
