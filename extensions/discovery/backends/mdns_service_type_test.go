package backends

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMDNSCustomServiceType tests registration and discovery with custom service type.
func TestMDNSCustomServiceType(t *testing.T) {
	// Skip in CI environments where multicast mDNS may be restricted
	if testing.Short() {
		t.Skip("Skipping mDNS discovery test in short mode (CI environments)")
	}

	// Use unique service name to avoid collisions
	serviceName := fmt.Sprintf("my-service-%d", time.Now().UnixNano())

	// Create backend with custom service type (using unique test service type)
	backend, err := NewMDNSBackend(MDNSConfig{
		Domain:        "local.",
		ServiceType:   "_forgetest._tcp", // Unique test service type to avoid collisions
		BrowseTimeout: 3 * time.Second,   // Increased for Mac reliability
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	defer backend.Close()

	// Register a service without explicit address (let backend auto-detect)
	instance := &ServiceInstance{
		ID:   fmt.Sprintf("test-service-1-%d", time.Now().UnixNano()),
		Name: serviceName,
		// Address removed - let backend auto-detect valid addresses
		Port:    8080,
		Version: "1.0.0",
		Metadata: map[string]string{
			"env": "test",
		},
	}

	err = backend.Register(ctx, instance)
	require.NoError(t, err)

	// Wait longer for mDNS to propagate on Mac
	time.Sleep(1 * time.Second)

	// Discover should find the service using custom type
	discovered, err := backend.Discover(ctx, serviceName)
	require.NoError(t, err)

	// Filter to only our test service (in case other services exist on the machine)
	var found *ServiceInstance
	for _, inst := range discovered {
		if inst.ID == instance.ID {
			found = inst
			break
		}
	}

	require.NotNil(t, found, "should discover our test service")
	assert.Equal(t, instance.ID, found.ID)
	assert.Equal(t, instance.Port, found.Port)

	// Should have mdns.service_type in metadata
	assert.Equal(t, "_forgetest._tcp", found.Metadata["mdns.service_type"])

	// Cleanup
	err = backend.Deregister(ctx, instance.ID)
	require.NoError(t, err)
}

// TestMDNSMultiTypeDiscovery tests discovering multiple service types (gateway use case).
func TestMDNSMultiTypeDiscovery(t *testing.T) {
	// Skip in CI environments where multicast mDNS may be restricted
	if testing.Short() {
		t.Skip("Skipping mDNS discovery test in short mode (CI environments)")
	}

	// Create backend with multiple service types
	backend, err := NewMDNSBackend(MDNSConfig{
		Domain: "local.",
		ServiceTypes: []string{
			"_octopus._tcp",
			"_farp._tcp",
			"_http._tcp",
		},
		BrowseTimeout: 3 * time.Second, // Increased for Mac
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

	// Use unique IDs to avoid collisions
	timestamp := time.Now().UnixNano()

	// Register service 1 with _octopus._tcp (no explicit address)
	service1 := &ServiceInstance{
		ID:      fmt.Sprintf("octopus-service-1-%d", timestamp),
		Name:    "octopus",
		Port:    8080,
		Version: "1.0.0",
	}
	err = backend1.Register(ctx, service1)
	require.NoError(t, err)

	defer backend1.Deregister(ctx, service1.ID)

	// Register service 2 with _farp._tcp (no explicit address)
	service2 := &ServiceInstance{
		ID:      fmt.Sprintf("farp-service-1-%d", timestamp),
		Name:    "farp",
		Port:    9090,
		Version: "1.0.0",
	}
	err = backend2.Register(ctx, service2)
	require.NoError(t, err)

	defer backend2.Deregister(ctx, service2.ID)

	// Wait longer for mDNS to propagate on Mac
	time.Sleep(2 * time.Second)

	// Discover all types
	allServices, err := backend.DiscoverAllTypes(ctx)
	require.NoError(t, err)

	// Filter to only our test services
	filtered := make([]*ServiceInstance, 0)
	for _, svc := range allServices {
		if svc.ID == service1.ID || svc.ID == service2.ID {
			filtered = append(filtered, svc)
		}
	}

	// Should find both services
	assert.GreaterOrEqual(t, len(filtered), 2, "should discover services from multiple types")

	// Verify we have both service types
	serviceTypes := make(map[string]bool)

	for _, svc := range filtered {
		if svcType, ok := svc.Metadata["mdns.service_type"]; ok {
			serviceTypes[svcType] = true
		}
	}

	// We should have discovered at least one service type
	assert.NotEmpty(t, serviceTypes, "should have service types in metadata")
}

// TestExtractServiceNameFromType tests the service name extraction helper.
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

// TestMDNSDefaultServiceType tests that service type defaults to service name.
func TestMDNSDefaultServiceType(t *testing.T) {
	// Skip in CI environments where multicast mDNS may be restricted
	if testing.Short() {
		t.Skip("Skipping mDNS discovery test in short mode (CI environments)")
	}

	// Use unique service name to avoid collisions
	serviceName := fmt.Sprintf("my-service-%d", time.Now().UnixNano())

	// Create backend without custom service type
	backend, err := NewMDNSBackend(MDNSConfig{
		Domain:        "local.",
		BrowseTimeout: 3 * time.Second, // Increased for Mac
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = backend.Initialize(ctx)
	require.NoError(t, err)

	defer backend.Close()

	// Register a service without explicit address
	instance := &ServiceInstance{
		ID:      fmt.Sprintf("test-default-type-%d", time.Now().UnixNano()),
		Name:    serviceName,
		Port:    8080,
		Version: "1.0.0",
	}

	err = backend.Register(ctx, instance)
	require.NoError(t, err)

	defer backend.Deregister(ctx, instance.ID)

	// Wait longer for mDNS to propagate on Mac
	time.Sleep(1 * time.Second)

	// Discover should work with default type
	discovered, err := backend.Discover(ctx, serviceName)
	require.NoError(t, err)

	// Filter to only our test service
	var found *ServiceInstance
	for _, inst := range discovered {
		if inst.ID == instance.ID {
			found = inst
			break
		}
	}

	require.NotNil(t, found, "should discover service with default type")

	// Should have default service type in metadata (sanitized)
	expectedType := fmt.Sprintf("_%s._tcp", sanitizeServiceName(serviceName))
	assert.Equal(t, expectedType, found.Metadata["mdns.service_type"])
}

// TestMDNSWatchIntervalConfig tests watch interval configuration.
func TestMDNSWatchIntervalConfig(t *testing.T) {
	customInterval := 45 * time.Second

	backend, err := NewMDNSBackend(MDNSConfig{
		Domain:        "local.",
		WatchInterval: customInterval,
	})
	require.NoError(t, err)

	assert.Equal(t, customInterval, backend.config.WatchInterval)
}

// TestMDNSConfigDefaults tests that defaults are applied correctly.
func TestMDNSConfigDefaults(t *testing.T) {
	backend, err := NewMDNSBackend(MDNSConfig{})
	require.NoError(t, err)

	// Check defaults
	assert.Equal(t, "local.", backend.config.Domain)
	assert.Equal(t, 3*time.Second, backend.config.BrowseTimeout)
	assert.Equal(t, 30*time.Second, backend.config.WatchInterval)
	assert.Equal(t, uint32(120), backend.config.TTL)
}
