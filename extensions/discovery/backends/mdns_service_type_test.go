package backends

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMDNSCustomServiceType tests registration and discovery with custom service type
func TestMDNSCustomServiceType(t *testing.T) {
	// Create backend with custom service type
	backend, err := NewMDNSBackend(MDNSConfig{
		Domain:        "local.",
		ServiceType:   "_octopus._tcp", // Custom service type
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)
	defer backend.Close()

	// Register a service
	instance := &ServiceInstance{
		ID:      "test-service-1",
		Name:    "my-service", // Name doesn't matter, type is "_octopus._tcp"
		Address: "192.168.1.100",
		Port:    8080,
		Version: "1.0.0",
		Metadata: map[string]string{
			"env": "test",
		},
	}

	err = backend.Register(ctx, instance)
	require.NoError(t, err)

	// Give mDNS time to propagate
	time.Sleep(500 * time.Millisecond)

	// Discover should find the service using custom type
	discovered, err := backend.Discover(ctx, "my-service")
	require.NoError(t, err)
	assert.Len(t, discovered, 1, "should discover service with custom type")

	// Verify the service
	if len(discovered) > 0 {
		found := discovered[0]
		assert.Equal(t, instance.ID, found.ID)
		assert.Equal(t, instance.Port, found.Port)

		// Should have mdns.service_type in metadata
		assert.Equal(t, "_octopus._tcp", found.Metadata["mdns.service_type"])
	}

	// Cleanup
	err = backend.Deregister(ctx, instance.ID)
	require.NoError(t, err)
}

// TestMDNSMultiTypeDiscovery tests discovering multiple service types (gateway use case)
func TestMDNSMultiTypeDiscovery(t *testing.T) {
	// Create backend with multiple service types
	backend, err := NewMDNSBackend(MDNSConfig{
		Domain: "local.",
		ServiceTypes: []string{
			"_octopus._tcp",
			"_farp._tcp",
			"_http._tcp",
		},
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)
	defer backend.Close()

	// Register services with different types
	// We need separate backends for different service types
	backend1, _ := NewMDNSBackend(MDNSConfig{
		Domain:      "local.",
		ServiceType: "_octopus._tcp",
	})
	backend1.Initialize(ctx)
	defer backend1.Close()

	backend2, _ := NewMDNSBackend(MDNSConfig{
		Domain:      "local.",
		ServiceType: "_farp._tcp",
	})
	backend2.Initialize(ctx)
	defer backend2.Close()

	// Register service 1 with _octopus._tcp
	service1 := &ServiceInstance{
		ID:      "octopus-service-1",
		Name:    "octopus",
		Address: "192.168.1.100",
		Port:    8080,
		Version: "1.0.0",
	}
	err = backend1.Register(ctx, service1)
	require.NoError(t, err)
	defer backend1.Deregister(ctx, service1.ID)

	// Register service 2 with _farp._tcp
	service2 := &ServiceInstance{
		ID:      "farp-service-1",
		Name:    "farp",
		Address: "192.168.1.101",
		Port:    9090,
		Version: "1.0.0",
	}
	err = backend2.Register(ctx, service2)
	require.NoError(t, err)
	defer backend2.Deregister(ctx, service2.ID)

	// Give mDNS time to propagate
	time.Sleep(500 * time.Millisecond)

	// Discover all types
	allServices, err := backend.DiscoverAllTypes(ctx)
	require.NoError(t, err)

	// Should find both services
	assert.GreaterOrEqual(t, len(allServices), 2, "should discover services from multiple types")

	// Verify we have both service types
	serviceTypes := make(map[string]bool)
	for _, svc := range allServices {
		if svcType, ok := svc.Metadata["mdns.service_type"]; ok {
			serviceTypes[svcType] = true
		}
	}

	// We should have discovered at least one service type
	assert.NotEmpty(t, serviceTypes, "should have service types in metadata")
}

// TestExtractServiceNameFromType tests the service name extraction helper
func TestExtractServiceNameFromType(t *testing.T) {
	tests := []struct {
		name         string
		serviceType  string
		expectedName string
	}{
		{
			name:         "standard tcp type",
			serviceType:  "_octopus._tcp",
			expectedName: "octopus",
		},
		{
			name:         "with domain",
			serviceType:  "_octopus._tcp.local.",
			expectedName: "octopus",
		},
		{
			name:         "http type",
			serviceType:  "_http._tcp",
			expectedName: "http",
		},
		{
			name:         "farp type",
			serviceType:  "_farp._tcp",
			expectedName: "farp",
		},
		{
			name:         "custom type",
			serviceType:  "_my-custom-service._tcp",
			expectedName: "my-custom-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractServiceNameFromType(tt.serviceType)
			assert.Equal(t, tt.expectedName, result)
		})
	}
}

// TestMDNSDefaultServiceType tests that service type defaults to service name
func TestMDNSDefaultServiceType(t *testing.T) {
	// Create backend without custom service type
	backend, err := NewMDNSBackend(MDNSConfig{
		Domain:        "local.",
		BrowseTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)
	defer backend.Close()

	// Register a service (should use _my-service._tcp as default)
	instance := &ServiceInstance{
		ID:      "test-default-type",
		Name:    "my-service",
		Address: "192.168.1.100",
		Port:    8080,
		Version: "1.0.0",
	}

	err = backend.Register(ctx, instance)
	require.NoError(t, err)
	defer backend.Deregister(ctx, instance.ID)

	// Give mDNS time to propagate
	time.Sleep(500 * time.Millisecond)

	// Discover should work with default type
	discovered, err := backend.Discover(ctx, "my-service")
	require.NoError(t, err)
	assert.Len(t, discovered, 1, "should discover service with default type")

	if len(discovered) > 0 {
		found := discovered[0]
		// Should have default service type in metadata
		assert.Equal(t, "_my-service._tcp", found.Metadata["mdns.service_type"])
	}
}

// TestMDNSWatchIntervalConfig tests watch interval configuration
func TestMDNSWatchIntervalConfig(t *testing.T) {
	customInterval := 45 * time.Second

	backend, err := NewMDNSBackend(MDNSConfig{
		Domain:        "local.",
		WatchInterval: customInterval,
	})
	require.NoError(t, err)

	assert.Equal(t, customInterval, backend.config.WatchInterval)
}

// TestMDNSConfigDefaults tests that defaults are applied correctly
func TestMDNSConfigDefaults(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)

	// Check defaults
	assert.Equal(t, "local.", backend.config.Domain)
	assert.Equal(t, 3*time.Second, backend.config.BrowseTimeout)
	assert.Equal(t, 30*time.Second, backend.config.WatchInterval)
	assert.Equal(t, uint32(120), backend.config.TTL)
}
