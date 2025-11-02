package backends

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMDNSBackend(t *testing.T) {
	tests := []struct {
		name           string
		config         MDNSConfig
		wantErr        bool
		expectedDomain string
		expectedTTL    uint32
	}{
		{
			name: "default config",
			config: MDNSConfig{
				Domain: "",
				TTL:    0,
			},
			wantErr:        false,
			expectedDomain: "local.",
			expectedTTL:    120,
		},
		{
			name: "custom config",
			config: MDNSConfig{
				Domain:        "custom.local.",
				BrowseTimeout: 5 * time.Second,
				TTL:           60,
				IPv6:          true,
			},
			wantErr:        false,
			expectedDomain: "custom.local.",
			expectedTTL:    60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewMDNSBackend(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, backend)
			} else {
				require.NoError(t, err)
				require.NotNil(t, backend)
				assert.Equal(t, "mdns", backend.Name())
				assert.Equal(t, tt.expectedDomain, backend.config.Domain)
				assert.Equal(t, tt.expectedTTL, backend.config.TTL)
			}
		})
	}
}

func TestMDNSBackend_Initialize(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)
	require.NotNil(t, backend)

	ctx := context.Background()
	err = backend.Initialize(ctx)
	assert.NoError(t, err)
}

func TestMDNSBackend_RegisterAndDiscover(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Create test service instance
	service := &ServiceInstance{
		ID:      "test-service-1",
		Name:    "test-service",
		Version: "1.0.0",
		Address: "127.0.0.1",
		Port:    8080,
		Tags:    []string{"http", "v1"},
		Metadata: map[string]string{
			"env": "test",
		},
	}

	// Test registration
	t.Run("register service", func(t *testing.T) {
		err := backend.Register(ctx, service)
		assert.NoError(t, err)
	})

	// Wait for mDNS to propagate
	time.Sleep(500 * time.Millisecond)

	// Test discovery
	t.Run("discover service", func(t *testing.T) {
		instances, err := backend.Discover(ctx, "test-service")
		require.NoError(t, err)
		require.NotEmpty(t, instances, "should find at least one instance")

		// Verify instance data
		found := false
		for _, inst := range instances {
			if inst.ID == service.ID {
				found = true
				assert.Equal(t, service.Name, inst.Name)
				assert.Equal(t, service.Version, inst.Version)
				assert.Equal(t, service.Port, inst.Port)
				assert.Contains(t, inst.Tags, "http")
				assert.Contains(t, inst.Tags, "v1")
				assert.Equal(t, "test", inst.Metadata["env"])
				break
			}
		}
		assert.True(t, found, "should find registered service")
	})

	// Test deregistration
	t.Run("deregister service", func(t *testing.T) {
		err := backend.Deregister(ctx, service.ID)
		assert.NoError(t, err)

		// Verify service is removed
		time.Sleep(500 * time.Millisecond)
		instances, err := backend.Discover(ctx, "test-service")
		require.NoError(t, err)

		// Service should not be found
		for _, inst := range instances {
			assert.NotEqual(t, service.ID, inst.ID, "deregistered service should not be found")
		}
	})
}

func TestMDNSBackend_RegisterValidation(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	tests := []struct {
		name     string
		instance *ServiceInstance
		wantErr  bool
		errMsg   string
	}{
		{
			name: "missing ID",
			instance: &ServiceInstance{
				Name: "test",
				Port: 8080,
			},
			wantErr: true,
			errMsg:  "ID is required",
		},
		{
			name: "missing name",
			instance: &ServiceInstance{
				ID:   "test-1",
				Port: 8080,
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing port",
			instance: &ServiceInstance{
				ID:   "test-1",
				Name: "test",
				Port: 0,
			},
			wantErr: true,
			errMsg:  "port is required",
		},
		{
			name: "valid instance",
			instance: &ServiceInstance{
				ID:   "test-1",
				Name: "test",
				Port: 8080,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := backend.Register(ctx, tt.instance)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				// Clean up
				backend.Deregister(ctx, tt.instance.ID)
			}
		})
	}
}

func TestMDNSBackend_DiscoverWithTags(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Register multiple services with different tags
	services := []*ServiceInstance{
		{
			ID:   "api-prod-1",
			Name: "api",
			Port: 8080,
			Tags: []string{"http", "production", "v2"},
		},
		{
			ID:   "api-dev-1",
			Name: "api",
			Port: 8081,
			Tags: []string{"http", "development", "v2"},
		},
		{
			ID:   "api-staging-1",
			Name: "api",
			Port: 8082,
			Tags: []string{"http", "staging", "v1"},
		},
	}

	for _, svc := range services {
		err := backend.Register(ctx, svc)
		require.NoError(t, err)
	}
	defer func() {
		for _, svc := range services {
			backend.Deregister(ctx, svc.ID)
		}
	}()

	// Wait for propagation
	time.Sleep(time.Second)

	tests := []struct {
		name     string
		tags     []string
		minCount int
		maxCount int
	}{
		{
			name:     "filter by production",
			tags:     []string{"production"},
			minCount: 1,
			maxCount: 1,
		},
		{
			name:     "filter by v2",
			tags:     []string{"v2"},
			minCount: 2,
			maxCount: 2,
		},
		{
			name:     "filter by http",
			tags:     []string{"http"},
			minCount: 3,
			maxCount: 3,
		},
		{
			name:     "filter by multiple tags",
			tags:     []string{"http", "production"},
			minCount: 1,
			maxCount: 1,
		},
		{
			name:     "no tags (all)",
			tags:     []string{},
			minCount: 3,
			maxCount: 3,
		},
		{
			name:     "non-existent tag",
			tags:     []string{"nonexistent"},
			minCount: 0,
			maxCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instances, err := backend.DiscoverWithTags(ctx, "api", tt.tags)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(instances), tt.minCount, "should have at least %d instance(s)", tt.minCount)
			assert.LessOrEqual(t, len(instances), tt.maxCount, "should have at most %d instance(s)", tt.maxCount)

			// Verify all returned instances have the required tags
			if len(tt.tags) > 0 {
				for _, inst := range instances {
					for _, tag := range tt.tags {
						assert.Contains(t, inst.Tags, tag, "instance should have tag: %s", tag)
					}
				}
			}
		})
	}
}

func TestMDNSBackend_Watch(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Track watch notifications
	notifications := make(chan []*ServiceInstance, 10)

	// Set up watcher
	err = backend.Watch(ctx, "watch-test", func(instances []*ServiceInstance) {
		notifications <- instances
	})
	require.NoError(t, err)

	// Wait for initial notification (should be empty)
	select {
	case initial := <-notifications:
		assert.Empty(t, initial, "initial notification should be empty")
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive initial notification")
	}

	// Register a service
	service := &ServiceInstance{
		ID:   "watch-service-1",
		Name: "watch-test",
		Port: 9000,
	}
	err = backend.Register(ctx, service)
	require.NoError(t, err)

	// Wait for watch notification
	select {
	case instances := <-notifications:
		assert.NotEmpty(t, instances, "should receive notification with service")
		found := false
		for _, inst := range instances {
			if inst.ID == service.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "notification should include registered service")
	case <-time.After(10 * time.Second):
		t.Fatal("did not receive notification after registration")
	}

	// Deregister service
	err = backend.Deregister(ctx, service.ID)
	require.NoError(t, err)

	// Wait for watch notification of deregistration
	select {
	case instances := <-notifications:
		for _, inst := range instances {
			assert.NotEqual(t, service.ID, inst.ID, "deregistered service should not be in notification")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("did not receive notification after deregistration")
	}
}

func TestMDNSBackend_ListServices(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Register multiple service types
	services := []*ServiceInstance{
		{ID: "web-1", Name: "web", Port: 80},
		{ID: "web-2", Name: "web", Port: 8080},
		{ID: "api-1", Name: "api", Port: 3000},
		{ID: "db-1", Name: "database", Port: 5432},
	}

	for _, svc := range services {
		err := backend.Register(ctx, svc)
		require.NoError(t, err)
	}
	defer func() {
		for _, svc := range services {
			backend.Deregister(ctx, svc.ID)
		}
	}()

	// List services
	serviceNames, err := backend.ListServices(ctx)
	require.NoError(t, err)

	// Should have 3 unique service names
	assert.Len(t, serviceNames, 3, "should have 3 unique service types")

	// Check all service names are present
	serviceMap := make(map[string]bool)
	for _, name := range serviceNames {
		serviceMap[name] = true
	}
	assert.True(t, serviceMap["web"], "should have web service")
	assert.True(t, serviceMap["api"], "should have api service")
	assert.True(t, serviceMap["database"], "should have database service")
}

func TestMDNSBackend_Health(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Health check should pass
	err = backend.Health(ctx)
	assert.NoError(t, err)
}

func TestMDNSBackend_Close(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Register a service
	service := &ServiceInstance{
		ID:   "close-test-1",
		Name: "close-test",
		Port: 9999,
	}
	err = backend.Register(ctx, service)
	require.NoError(t, err)

	// Close backend
	err = backend.Close()
	assert.NoError(t, err)

	// Verify state is cleaned up
	assert.Empty(t, backend.services, "services should be cleared")
	assert.Empty(t, backend.watchers, "watchers should be cleared")
}

func TestMDNSBackend_DuplicateRegistration(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	service := &ServiceInstance{
		ID:   "dup-test-1",
		Name: "dup-test",
		Port: 7777,
	}

	// First registration should succeed
	err = backend.Register(ctx, service)
	require.NoError(t, err)

	// Second registration with same ID should fail
	err = backend.Register(ctx, service)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// Clean up
	backend.Deregister(ctx, service.ID)
}

func TestMDNSBackend_DeregisterNonExistent(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Deregister non-existent service should fail
	err = backend.Deregister(ctx, "nonexistent-service")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMDNSBackend_DiscoverNonExistentService(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: time.Second,
	})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Discover non-existent service should return empty list
	instances, err := backend.Discover(ctx, "nonexistent-service")
	assert.NoError(t, err, "should not error on empty results")
	assert.Empty(t, instances, "should return empty list")
}

func TestMDNSBackend_MultipleWatchers(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Set up multiple watchers for the same service
	notifications1 := make(chan []*ServiceInstance, 10)
	notifications2 := make(chan []*ServiceInstance, 10)

	err = backend.Watch(ctx, "multi-watch", func(instances []*ServiceInstance) {
		notifications1 <- instances
	})
	require.NoError(t, err)

	err = backend.Watch(ctx, "multi-watch", func(instances []*ServiceInstance) {
		notifications2 <- instances
	})
	require.NoError(t, err)

	// Both should receive initial notification
	select {
	case <-notifications1:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher 1 did not receive initial notification")
	}

	select {
	case <-notifications2:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher 2 did not receive initial notification")
	}

	// Register a service
	service := &ServiceInstance{
		ID:   "multi-watch-1",
		Name: "multi-watch",
		Port: 6666,
	}
	err = backend.Register(ctx, service)
	require.NoError(t, err)
	defer backend.Deregister(ctx, service.ID)

	// Both watchers should receive notification
	receivedCount := 0
	timeout := time.After(10 * time.Second)

	for receivedCount < 2 {
		select {
		case <-notifications1:
			receivedCount++
		case <-notifications2:
			receivedCount++
		case <-timeout:
			t.Fatalf("only received %d/2 notifications", receivedCount)
		}
	}

	assert.Equal(t, 2, receivedCount, "both watchers should receive notification")
}

func TestMDNSBackend_SanitizeServiceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"MyService", "myservice"},
		{"my_service", "my-service"},
		{"my service", "my-service"},
		{"My_Service Name", "my-service-name"},
		{"API-Service", "api-service"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeServiceName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMDNSBackend_ConcurrentOperations(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Perform concurrent registrations
	const numServices = 10
	errChan := make(chan error, numServices)

	for i := 0; i < numServices; i++ {
		go func(id int) {
			service := &ServiceInstance{
				ID:   formatServiceID("concurrent", id),
				Name: "concurrent-test",
				Port: 5000 + id,
			}
			errChan <- backend.Register(ctx, service)
		}(i)
	}

	// Check all registrations succeeded
	for i := 0; i < numServices; i++ {
		err := <-errChan
		assert.NoError(t, err, "concurrent registration %d should succeed", i)
	}

	// Wait for propagation
	time.Sleep(time.Second)

	// Discover should find all services
	instances, err := backend.Discover(ctx, "concurrent-test")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(instances), numServices, "should find at least %d services", numServices)

	// Concurrent deregistrations
	for i := 0; i < numServices; i++ {
		go func(id int) {
			serviceID := formatServiceID("concurrent", id)
			errChan <- backend.Deregister(ctx, serviceID)
		}(i)
	}

	// Check all deregistrations succeeded
	for i := 0; i < numServices; i++ {
		err := <-errChan
		assert.NoError(t, err, "concurrent deregistration %d should succeed", i)
	}
}

func TestMDNSBackend_MetadataEncoding(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)
	defer backend.Close()

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	// Test with various metadata
	service := &ServiceInstance{
		ID:      "metadata-test-1",
		Name:    "metadata-test",
		Version: "2.1.0",
		Port:    8888,
		Tags:    []string{"http", "grpc", "production"},
		Metadata: map[string]string{
			"region":    "us-west-2",
			"zone":      "a",
			"container": "docker",
			"protocol":  "https",
		},
	}

	err = backend.Register(ctx, service)
	require.NoError(t, err)
	defer backend.Deregister(ctx, service.ID)

	// Wait for propagation
	time.Sleep(time.Second)

	// Discover and verify metadata
	instances, err := backend.Discover(ctx, "metadata-test")
	require.NoError(t, err)
	require.NotEmpty(t, instances)

	found := false
	for _, inst := range instances {
		if inst.ID == service.ID {
			found = true
			assert.Equal(t, service.Version, inst.Version)
			assert.ElementsMatch(t, service.Tags, inst.Tags)
			for key, value := range service.Metadata {
				assert.Equal(t, value, inst.Metadata[key], "metadata key %s should match", key)
			}
			break
		}
	}
	assert.True(t, found, "should find service with metadata")
}

// Helper function to format service ID
func formatServiceID(prefix string, id int) string {
	return fmt.Sprintf("%s-%d", prefix, id)
}

// Benchmark tests
func BenchmarkMDNSBackend_Register(b *testing.B) {
	backend, _ := NewMDNSBackend(MDNSConfig{})
	defer backend.Close()

	ctx := context.Background()
	backend.Initialize(ctx)

	service := &ServiceInstance{
		ID:   "bench-service",
		Name: "bench",
		Port: 9999,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.ID = formatServiceID("bench", i)
		backend.Register(ctx, service)
	}
}

func BenchmarkMDNSBackend_Discover(b *testing.B) {
	backend, _ := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: time.Second,
	})
	defer backend.Close()

	ctx := context.Background()
	backend.Initialize(ctx)

	// Register a service
	service := &ServiceInstance{
		ID:   "bench-service-1",
		Name: "bench",
		Port: 9999,
	}
	backend.Register(ctx, service)
	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.Discover(ctx, "bench")
	}
}

func BenchmarkMDNSBackend_DiscoverWithTags(b *testing.B) {
	backend, _ := NewMDNSBackend(MDNSConfig{
		BrowseTimeout: time.Second,
	})
	defer backend.Close()

	ctx := context.Background()
	backend.Initialize(ctx)

	// Register services with tags
	for i := 0; i < 5; i++ {
		service := &ServiceInstance{
			ID:   formatServiceID("bench", i),
			Name: "bench",
			Port: 9000 + i,
			Tags: []string{"http", "v1"},
		}
		backend.Register(ctx, service)
	}
	time.Sleep(time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backend.DiscoverWithTags(ctx, "bench", []string{"http"})
	}
}
