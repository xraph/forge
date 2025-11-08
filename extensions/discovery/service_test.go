package discovery

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/discovery/backends"
)

// mockLogger implements forge.Logger for testing.
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...forge.Field)      {}
func (m *mockLogger) Info(msg string, fields ...forge.Field)       {}
func (m *mockLogger) Warn(msg string, fields ...forge.Field)       {}
func (m *mockLogger) Error(msg string, fields ...forge.Field)      {}
func (m *mockLogger) Fatal(msg string, fields ...forge.Field)      {}
func (m *mockLogger) Debugf(template string, args ...any)          {}
func (m *mockLogger) Infof(template string, args ...any)           {}
func (m *mockLogger) Warnf(template string, args ...any)           {}
func (m *mockLogger) Errorf(template string, args ...any)          {}
func (m *mockLogger) Fatalf(template string, args ...any)          {}
func (m *mockLogger) With(fields ...forge.Field) forge.Logger      { return m }
func (m *mockLogger) WithContext(ctx context.Context) forge.Logger { return m }
func (m *mockLogger) Named(name string) forge.Logger               { return m }
func (m *mockLogger) Sugar() forge.SugarLogger                     { return &mockSugarLogger{} }
func (m *mockLogger) Sync() error                                  { return nil }

// mockSugarLogger implements forge.SugarLogger for testing.
type mockSugarLogger struct{}

func (m *mockSugarLogger) Debugw(msg string, keysAndValues ...any) {}
func (m *mockSugarLogger) Infow(msg string, keysAndValues ...any)  {}
func (m *mockSugarLogger) Warnw(msg string, keysAndValues ...any)  {}
func (m *mockSugarLogger) Errorw(msg string, keysAndValues ...any) {}
func (m *mockSugarLogger) Fatalw(msg string, keysAndValues ...any) {}
func (m *mockSugarLogger) With(args ...any) forge.SugarLogger      { return m }

func newMockLogger() forge.Logger {
	return &mockLogger{}
}

func TestNewService(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	logger := newMockLogger()

	service := NewService(backend, logger)

	assert.NotNil(t, service)
	assert.NotNil(t, service.backend)
	assert.NotNil(t, service.logger)
}

func TestService_Register(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	instance := &ServiceInstance{
		ID:      "test-1",
		Name:    "test-service",
		Address: "localhost",
		Port:    8080,
		Tags:    []string{"api"},
		Status:  HealthStatusPassing,
	}

	err := service.Register(ctx, instance)
	assert.NoError(t, err)

	// Verify registration
	instances, err := service.Discover(ctx, "test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "test-1", instances[0].ID)
}

func TestService_Deregister(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register instance
	instance := &ServiceInstance{
		ID:      "test-1",
		Name:    "test-service",
		Address: "localhost",
		Port:    8080,
	}
	err := service.Register(ctx, instance)
	require.NoError(t, err)

	// Deregister
	err = service.Deregister(ctx, "test-1")
	assert.NoError(t, err)

	// Verify deregistration
	instances, err := service.Discover(ctx, "test-service")
	assert.NoError(t, err)
	assert.Empty(t, instances)
}

func TestService_Discover(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register multiple instances
	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:      fmt.Sprintf("test-%d", i),
			Name:    "test-service",
			Address: "localhost",
			Port:    8080 + i,
		}
		err := service.Register(ctx, instance)
		require.NoError(t, err)
	}

	// Discover
	instances, err := service.Discover(ctx, "test-service")
	assert.NoError(t, err)
	assert.Len(t, instances, 3)
}

func TestService_DiscoverWithTags(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register instances with different tags
	instances := []*ServiceInstance{
		{
			ID:      "test-1",
			Name:    "test-service",
			Address: "localhost",
			Port:    8081,
			Tags:    []string{"api", "v1"},
		},
		{
			ID:      "test-2",
			Name:    "test-service",
			Address: "localhost",
			Port:    8082,
			Tags:    []string{"api", "v2"},
		},
		{
			ID:      "test-3",
			Name:    "test-service",
			Address: "localhost",
			Port:    8083,
			Tags:    []string{"worker"},
		},
	}

	for _, inst := range instances {
		err := service.Register(ctx, inst)
		require.NoError(t, err)
	}

	// Discover with tags
	found, err := service.DiscoverWithTags(ctx, "test-service", []string{"api", "v1"})
	assert.NoError(t, err)
	assert.Len(t, found, 1)
	assert.Equal(t, "test-1", found[0].ID)

	// Discover with different tags
	found, err = service.DiscoverWithTags(ctx, "test-service", []string{"api"})
	assert.NoError(t, err)
	assert.Len(t, found, 2)
}

func TestService_DiscoverHealthy(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register instances with different health statuses
	instances := []*ServiceInstance{
		{
			ID:      "test-1",
			Name:    "test-service",
			Address: "localhost",
			Port:    8081,
			Status:  HealthStatusPassing,
		},
		{
			ID:      "test-2",
			Name:    "test-service",
			Address: "localhost",
			Port:    8082,
			Status:  HealthStatusCritical,
		},
		{
			ID:      "test-3",
			Name:    "test-service",
			Address: "localhost",
			Port:    8083,
			Status:  HealthStatusPassing,
		},
	}

	for _, inst := range instances {
		err := service.Register(ctx, inst)
		require.NoError(t, err)
	}

	// Discover only healthy
	healthy, err := service.DiscoverHealthy(ctx, "test-service")
	assert.NoError(t, err)
	assert.Len(t, healthy, 2)

	for _, inst := range healthy {
		assert.True(t, inst.IsHealthy())
	}
}

func TestService_SelectInstance_RoundRobin(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register 3 instances
	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:      fmt.Sprintf("test-%d", i),
			Name:    "test-service",
			Address: "localhost",
			Port:    8080 + i,
			Status:  HealthStatusPassing,
		}
		err := service.Register(ctx, instance)
		require.NoError(t, err)
	}

	// Select instances in round-robin fashion
	selectedIDs := make([]string, 0, 6)

	for range 6 {
		instance, err := service.SelectInstance(ctx, "test-service", LoadBalanceRoundRobin)
		require.NoError(t, err)

		selectedIDs = append(selectedIDs, instance.ID)
	}

	// Should cycle through all instances
	assert.Equal(t, "test-1", selectedIDs[0])
	assert.Equal(t, "test-2", selectedIDs[1])
	assert.Equal(t, "test-3", selectedIDs[2])
	assert.Equal(t, "test-1", selectedIDs[3])
	assert.Equal(t, "test-2", selectedIDs[4])
	assert.Equal(t, "test-3", selectedIDs[5])
}

func TestService_SelectInstance_Random(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register 3 instances
	for i := 1; i <= 3; i++ {
		instance := &ServiceInstance{
			ID:      fmt.Sprintf("test-%d", i),
			Name:    "test-service",
			Address: "localhost",
			Port:    8080 + i,
			Status:  HealthStatusPassing,
		}
		err := service.Register(ctx, instance)
		require.NoError(t, err)
	}

	// Select random instances
	selectedIDs := make(map[string]int)

	for range 100 {
		instance, err := service.SelectInstance(ctx, "test-service", LoadBalanceRandom)
		require.NoError(t, err)

		selectedIDs[instance.ID]++
	}

	// All instances should be selected at least once (with high probability)
	assert.Positive(t, selectedIDs["test-1"])
	assert.Positive(t, selectedIDs["test-2"])
	assert.Positive(t, selectedIDs["test-3"])
}

func TestService_SelectInstance_NoHealthyInstances(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register unhealthy instance
	instance := &ServiceInstance{
		ID:      "test-1",
		Name:    "test-service",
		Address: "localhost",
		Port:    8080,
		Status:  HealthStatusCritical,
	}
	err := service.Register(ctx, instance)
	require.NoError(t, err)

	// Try to select - should fail
	_, err = service.SelectInstance(ctx, "test-service", LoadBalanceRoundRobin)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy instances")
}

func TestService_GetServiceURL(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	instance := &ServiceInstance{
		ID:      "test-1",
		Name:    "test-service",
		Address: "localhost",
		Port:    8080,
		Status:  HealthStatusPassing,
	}
	err := service.Register(ctx, instance)
	require.NoError(t, err)

	// Get URL with default scheme
	url, err := service.GetServiceURL(ctx, "test-service", "", LoadBalanceRoundRobin)
	assert.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", url)

	// Get URL with custom scheme
	url, err = service.GetServiceURL(ctx, "test-service", "https", LoadBalanceRoundRobin)
	assert.NoError(t, err)
	assert.Equal(t, "https://localhost:8080", url)
}

func TestService_ListServices(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Register instances for different services
	services := []string{"api", "worker", "scheduler"}
	for _, svc := range services {
		instance := &ServiceInstance{
			ID:      svc + "-1",
			Name:    svc,
			Address: "localhost",
			Port:    8080,
		}
		err := service.Register(ctx, instance)
		require.NoError(t, err)
	}

	// List services
	list, err := service.ListServices(ctx)
	assert.NoError(t, err)
	assert.Len(t, list, 3)
	assert.ElementsMatch(t, services, list)
}

func TestService_Watch(t *testing.T) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Setup watcher
	changes := make(chan []*ServiceInstance, 10)
	onChange := func(instances []*ServiceInstance) {
		changes <- instances
	}

	err := service.Watch(ctx, "test-service", onChange)
	require.NoError(t, err)

	// Should receive initial state (empty)
	select {
	case instances := <-changes:
		assert.Empty(t, instances)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive initial notification")
	}

	// Register instance
	instance := &ServiceInstance{
		ID:      "test-1",
		Name:    "test-service",
		Address: "localhost",
		Port:    8080,
	}
	err = service.Register(ctx, instance)
	require.NoError(t, err)

	// Should receive change notification
	select {
	case instances := <-changes:
		assert.Len(t, instances, 1)
		assert.Equal(t, "test-1", instances[0].ID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive change notification")
	}
}

func TestServiceInstance_HasTag(t *testing.T) {
	instance := &ServiceInstance{
		Tags: []string{"api", "v1", "production"},
	}

	assert.True(t, instance.HasTag("api"))
	assert.True(t, instance.HasTag("v1"))
	assert.True(t, instance.HasTag("production"))
	assert.False(t, instance.HasTag("v2"))
	assert.False(t, instance.HasTag("staging"))
}

func TestServiceInstance_HasAllTags(t *testing.T) {
	instance := &ServiceInstance{
		Tags: []string{"api", "v1", "production"},
	}

	assert.True(t, instance.HasAllTags([]string{"api", "v1"}))
	assert.True(t, instance.HasAllTags([]string{"production"}))
	assert.True(t, instance.HasAllTags([]string{}))
	assert.False(t, instance.HasAllTags([]string{"api", "v2"}))
	assert.False(t, instance.HasAllTags([]string{"staging"}))
}

func TestServiceInstance_IsHealthy(t *testing.T) {
	tests := []struct {
		name   string
		status HealthStatus
		want   bool
	}{
		{"passing", HealthStatusPassing, true},
		{"warning", HealthStatusWarning, false},
		{"critical", HealthStatusCritical, false},
		{"unknown", HealthStatusUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &ServiceInstance{Status: tt.status}
			assert.Equal(t, tt.want, instance.IsHealthy())
		})
	}
}

func TestServiceInstance_GetMetadata(t *testing.T) {
	instance := &ServiceInstance{
		Metadata: map[string]string{
			"region":      "us-east-1",
			"environment": "production",
		},
	}

	val, ok := instance.GetMetadata("region")
	assert.True(t, ok)
	assert.Equal(t, "us-east-1", val)

	val, ok = instance.GetMetadata("environment")
	assert.True(t, ok)
	assert.Equal(t, "production", val)

	_, ok = instance.GetMetadata("nonexistent")
	assert.False(t, ok)
}

func TestServiceInstance_URL(t *testing.T) {
	instance := &ServiceInstance{
		Address: "localhost",
		Port:    8080,
	}

	// Default scheme
	url := instance.URL("")
	assert.Equal(t, "http://localhost:8080", url)

	// Custom scheme
	url = instance.URL("https")
	assert.Equal(t, "https://localhost:8080", url)
}

// Benchmark tests.
func BenchmarkService_Register(b *testing.B) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	for i := 0; b.Loop(); i++ {
		instance := &ServiceInstance{
			ID:      fmt.Sprintf("test-%d", i),
			Name:    "test-service",
			Address: "localhost",
			Port:    8080,
		}
		service.Register(ctx, instance)
	}
}

func BenchmarkService_Discover(b *testing.B) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Pre-register instances
	for i := range 100 {
		instance := &ServiceInstance{
			ID:      fmt.Sprintf("test-%d", i),
			Name:    "test-service",
			Address: "localhost",
			Port:    8080 + i,
		}
		service.Register(ctx, instance)
	}

	for b.Loop() {
		service.Discover(ctx, "test-service")
	}
}

func BenchmarkService_SelectInstance_RoundRobin(b *testing.B) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Pre-register instances
	for i := range 10 {
		instance := &ServiceInstance{
			ID:      fmt.Sprintf("test-%d", i),
			Name:    "test-service",
			Address: "localhost",
			Port:    8080 + i,
			Status:  HealthStatusPassing,
		}
		service.Register(ctx, instance)
	}

	for b.Loop() {
		service.SelectInstance(ctx, "test-service", LoadBalanceRoundRobin)
	}
}

func BenchmarkService_SelectInstance_Random(b *testing.B) {
	backend, _ := backends.NewMemoryBackend()
	service := NewService(backend, newMockLogger())
	ctx := context.Background()

	// Pre-register instances
	for i := range 10 {
		instance := &ServiceInstance{
			ID:      fmt.Sprintf("test-%d", i),
			Name:    "test-service",
			Address: "localhost",
			Port:    8080 + i,
			Status:  HealthStatusPassing,
		}
		service.Register(ctx, instance)
	}

	for b.Loop() {
		service.SelectInstance(ctx, "test-service", LoadBalanceRandom)
	}
}
