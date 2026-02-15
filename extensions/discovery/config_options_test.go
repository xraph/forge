package discovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// FARP Variadic Option Tests
// =============================================================================

func TestWithFARP(t *testing.T) {
	config := DefaultConfig()
	farpCfg := FARPConfig{
		Enabled:      true,
		AutoRegister: true,
		Strategy:     "hybrid",
		Capabilities: []string{"rest", "grpc"},
	}

	WithFARP(farpCfg)(&config)

	assert.True(t, config.FARP.Enabled)
	assert.True(t, config.FARP.AutoRegister)
	assert.Equal(t, "hybrid", config.FARP.Strategy)
	assert.ElementsMatch(t, []string{"rest", "grpc"}, config.FARP.Capabilities)
}

func TestWithFARPEnabled(t *testing.T) {
	config := DefaultConfig()

	// FARP is disabled by default
	assert.False(t, config.FARP.Enabled)

	WithFARPEnabled(true)(&config)
	assert.True(t, config.FARP.Enabled)

	WithFARPEnabled(false)(&config)
	assert.False(t, config.FARP.Enabled)
}

func TestWithFARPAutoRegister(t *testing.T) {
	config := DefaultConfig()

	WithFARPAutoRegister(false)(&config)
	assert.False(t, config.FARP.AutoRegister)

	WithFARPAutoRegister(true)(&config)
	assert.True(t, config.FARP.AutoRegister)
}

func TestWithFARPStrategy(t *testing.T) {
	strategies := []string{"push", "pull", "hybrid"}

	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			config := DefaultConfig()
			WithFARPStrategy(strategy)(&config)
			assert.Equal(t, strategy, config.FARP.Strategy)
		})
	}
}

func TestWithFARPSchemas(t *testing.T) {
	config := DefaultConfig()
	schemas := []FARPSchemaConfig{
		{
			Type:        "openapi",
			SpecVersion: "3.1.0",
			ContentType: "application/json",
			Location: FARPLocationConfig{
				Type: "http",
				URL:  "/openapi.json",
			},
		},
		{
			Type:        "asyncapi",
			SpecVersion: "2.0.0",
			ContentType: "application/yaml",
			Location: FARPLocationConfig{
				Type: "http",
				URL:  "/asyncapi.yaml",
			},
		},
	}

	WithFARPSchemas(schemas...)(&config)

	assert.Len(t, config.FARP.Schemas, 2)
	assert.Equal(t, "openapi", config.FARP.Schemas[0].Type)
	assert.Equal(t, "asyncapi", config.FARP.Schemas[1].Type)
}

func TestWithFARPEndpoints(t *testing.T) {
	config := DefaultConfig()
	endpoints := FARPEndpointsConfig{
		Health:         "/health",
		Metrics:        "/metrics",
		OpenAPI:        "/openapi.json",
		AsyncAPI:       "/asyncapi.yaml",
		GraphQL:        "/graphql",
		GRPCReflection: true,
	}

	WithFARPEndpoints(endpoints)(&config)

	assert.Equal(t, "/health", config.FARP.Endpoints.Health)
	assert.Equal(t, "/metrics", config.FARP.Endpoints.Metrics)
	assert.Equal(t, "/openapi.json", config.FARP.Endpoints.OpenAPI)
	assert.Equal(t, "/asyncapi.yaml", config.FARP.Endpoints.AsyncAPI)
	assert.Equal(t, "/graphql", config.FARP.Endpoints.GraphQL)
	assert.True(t, config.FARP.Endpoints.GRPCReflection)
}

func TestWithFARPCapabilities(t *testing.T) {
	config := DefaultConfig()

	WithFARPCapabilities("rest", "grpc", "websocket")(&config)

	assert.Len(t, config.FARP.Capabilities, 3)
	assert.ElementsMatch(t, []string{"rest", "grpc", "websocket"}, config.FARP.Capabilities)
}

// =============================================================================
// mDNS Variadic Option Tests
// =============================================================================

func TestWithMDNSServiceType(t *testing.T) {
	config := DefaultConfig()

	WithMDNSServiceType("_myapp._tcp")(&config)

	assert.Equal(t, "_myapp._tcp", config.MDNS.ServiceType)
}

func TestWithMDNSServiceTypes(t *testing.T) {
	config := DefaultConfig()

	WithMDNSServiceTypes("_user._tcp", "_order._tcp", "_payment._tcp")(&config)

	assert.Len(t, config.MDNS.ServiceTypes, 3)
	assert.ElementsMatch(t, []string{"_user._tcp", "_order._tcp", "_payment._tcp"}, config.MDNS.ServiceTypes)
}

func TestWithMDNSWatchInterval(t *testing.T) {
	config := DefaultConfig()

	WithMDNSWatchInterval(15 * time.Second)(&config)

	assert.Equal(t, 15*time.Second, config.MDNS.WatchInterval)
}

func TestWithMDNSBrowseTimeout(t *testing.T) {
	config := DefaultConfig()

	WithMDNSBrowseTimeout(5 * time.Second)(&config)

	assert.Equal(t, 5*time.Second, config.MDNS.BrowseTimeout)
}

func TestWithMDNSTTL(t *testing.T) {
	config := DefaultConfig()

	WithMDNSTTL(60)(&config)

	assert.Equal(t, uint32(60), config.MDNS.TTL)
}

func TestWithMDNSInterface(t *testing.T) {
	config := DefaultConfig()

	WithMDNSInterface("eth0")(&config)

	assert.Equal(t, "eth0", config.MDNS.Interface)
}

func TestWithMDNSIPv6(t *testing.T) {
	config := DefaultConfig()

	// IPv6 is disabled by default
	assert.False(t, config.MDNS.IPv6)

	WithMDNSIPv6(true)(&config)
	assert.True(t, config.MDNS.IPv6)
}

// =============================================================================
// Service Identity Variadic Option Tests
// =============================================================================

func TestWithServiceID(t *testing.T) {
	config := DefaultConfig()

	WithServiceID("instance-001")(&config)

	assert.Equal(t, "instance-001", config.Service.ID)
}

func TestWithServiceVersion(t *testing.T) {
	config := DefaultConfig()

	WithServiceVersion("2.1.0")(&config)

	assert.Equal(t, "2.1.0", config.Service.Version)
}

func TestWithAutoDeregister(t *testing.T) {
	config := DefaultConfig()

	// Auto-deregister is enabled by default
	assert.True(t, config.Service.EnableAutoDeregister)

	WithAutoDeregister(false)(&config)
	assert.False(t, config.Service.EnableAutoDeregister)

	WithAutoDeregister(true)(&config)
	assert.True(t, config.Service.EnableAutoDeregister)
}

// =============================================================================
// Existing Options (regression tests)
// =============================================================================

func TestWithEnabled_Config(t *testing.T) {
	config := DefaultConfig()

	WithEnabled(false)(&config)
	assert.False(t, config.Enabled)

	WithEnabled(true)(&config)
	assert.True(t, config.Enabled)
}

func TestWithBackend_Config(t *testing.T) {
	backends := []string{"memory", "consul", "etcd", "kubernetes", "eureka", "mdns"}

	for _, b := range backends {
		t.Run(b, func(t *testing.T) {
			config := DefaultConfig()
			WithBackend(b)(&config)
			assert.Equal(t, b, config.Backend)
		})
	}
}

func TestWithServiceName_Config(t *testing.T) {
	config := DefaultConfig()

	WithServiceName("my-service")(&config)

	assert.Equal(t, "my-service", config.Service.Name)
}

func TestWithServiceAddress_Config(t *testing.T) {
	config := DefaultConfig()

	WithServiceAddress("10.0.0.1", 9090)(&config)

	assert.Equal(t, "10.0.0.1", config.Service.Address)
	assert.Equal(t, 9090, config.Service.Port)
}

func TestWithServiceTags_Config(t *testing.T) {
	config := DefaultConfig()

	WithServiceTags("api", "v2", "production")(&config)

	assert.Len(t, config.Service.Tags, 3)
	assert.ElementsMatch(t, []string{"api", "v2", "production"}, config.Service.Tags)
}

func TestWithMDNS_Config(t *testing.T) {
	config := DefaultConfig()

	WithMDNS("custom.local.")(&config)

	assert.Equal(t, "mdns", config.Backend)
	assert.Equal(t, "custom.local.", config.MDNS.Domain)
}

func TestWithMDNS_EmptyDomain(t *testing.T) {
	config := DefaultConfig()

	WithMDNS("")(&config)

	assert.Equal(t, "mdns", config.Backend)
	// Domain should remain the default
	assert.Equal(t, "local.", config.MDNS.Domain)
}

// =============================================================================
// Combined Option Tests
// =============================================================================

func TestCombinedMDNSAndFARPOptions(t *testing.T) {
	config := DefaultConfig()

	// Configure for a service that uses mDNS with FARP
	opts := []ConfigOption{
		WithBackend("mdns"),
		WithServiceName("user-service"),
		WithServiceAddress("10.0.0.5", 8080),
		WithServiceVersion("1.2.0"),
		WithServiceID("user-svc-001"),
		WithServiceTags("api", "production"),
		WithMDNSServiceType("_forge._tcp"),
		WithMDNSBrowseTimeout(5 * time.Second),
		WithMDNSTTL(60),
		WithFARPEnabled(true),
		WithFARPAutoRegister(true),
		WithFARPStrategy("push"),
		WithFARPEndpoints(FARPEndpointsConfig{
			Health:  "/health",
			OpenAPI: "/openapi.json",
		}),
		WithFARPCapabilities("rest"),
	}

	for _, opt := range opts {
		opt(&config)
	}

	assert.Equal(t, "mdns", config.Backend)
	assert.Equal(t, "user-service", config.Service.Name)
	assert.Equal(t, "10.0.0.5", config.Service.Address)
	assert.Equal(t, 8080, config.Service.Port)
	assert.Equal(t, "1.2.0", config.Service.Version)
	assert.Equal(t, "user-svc-001", config.Service.ID)
	assert.ElementsMatch(t, []string{"api", "production"}, config.Service.Tags)
	assert.Equal(t, "_forge._tcp", config.MDNS.ServiceType)
	assert.Equal(t, 5*time.Second, config.MDNS.BrowseTimeout)
	assert.Equal(t, uint32(60), config.MDNS.TTL)
	assert.True(t, config.FARP.Enabled)
	assert.True(t, config.FARP.AutoRegister)
	assert.Equal(t, "push", config.FARP.Strategy)
	assert.Equal(t, "/health", config.FARP.Endpoints.Health)
	assert.Equal(t, "/openapi.json", config.FARP.Endpoints.OpenAPI)
	assert.ElementsMatch(t, []string{"rest"}, config.FARP.Capabilities)
}

func TestCombinedGatewayMDNSOptions(t *testing.T) {
	config := DefaultConfig()

	// Configure for a gateway that browses multiple mDNS service types
	opts := []ConfigOption{
		WithBackend("mdns"),
		WithServiceName("api-gateway"),
		WithMDNSServiceTypes("_user._tcp", "_order._tcp", "_payment._tcp"),
		WithMDNSWatchInterval(10 * time.Second),
		WithMDNSIPv6(true),
		WithFARPEnabled(true),
	}

	for _, opt := range opts {
		opt(&config)
	}

	assert.Equal(t, "mdns", config.Backend)
	assert.Equal(t, "api-gateway", config.Service.Name)
	assert.Len(t, config.MDNS.ServiceTypes, 3)
	assert.Equal(t, 10*time.Second, config.MDNS.WatchInterval)
	assert.True(t, config.MDNS.IPv6)
	assert.True(t, config.FARP.Enabled)
}
