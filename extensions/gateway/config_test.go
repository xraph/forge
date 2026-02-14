package gateway

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if !config.Enabled {
		t.Error("expected gateway to be enabled by default")
	}

	if config.BasePath == "" {
		t.Error("expected default base path")
	}

	if config.HealthCheck.Interval == 0 {
		t.Error("expected default health check interval")
	}

	if config.LoadBalancing.Strategy == "" {
		t.Error("expected default load balancing strategy")
	}
}

func TestWithEnabled(t *testing.T) {
	config := DefaultConfig()
	WithEnabled(false)(&config)

	if config.Enabled {
		t.Error("expected enabled to be false")
	}
}

func TestWithBasePath(t *testing.T) {
	config := DefaultConfig()
	WithBasePath("/custom")(&config)

	if config.BasePath != "/custom" {
		t.Errorf("expected base path /custom, got %s", config.BasePath)
	}
}

func TestWithRoute(t *testing.T) {
	config := DefaultConfig()

	routeConfig := RouteConfig{
		Path:    "/api/test",
		Methods: []string{"GET", "POST"},
		Targets: []TargetConfig{{URL: "http://localhost:8080"}},
	}

	WithRoute(routeConfig)(&config)

	if len(config.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(config.Routes))
	}

	if config.Routes[0].Path != "/api/test" {
		t.Errorf("expected path /api/test, got %s", config.Routes[0].Path)
	}
}

func TestWithRoutes(t *testing.T) {
	config := DefaultConfig()

	routes := []RouteConfig{
		{Path: "/api/route1", Targets: []TargetConfig{{URL: "http://localhost:8080"}}},
		{Path: "/api/route2", Targets: []TargetConfig{{URL: "http://localhost:8081"}}},
	}

	WithRoutes(routes)(&config)

	if len(config.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(config.Routes))
	}
}

func TestWithTimeouts(t *testing.T) {
	config := DefaultConfig()

	timeoutConfig := TimeoutConfig{
		Connect: 5 * time.Second,
		Read:    10 * time.Second,
		Write:   10 * time.Second,
		Idle:    30 * time.Second,
	}

	WithTimeouts(timeoutConfig)(&config)

	if config.Timeouts.Connect != 5*time.Second {
		t.Errorf("expected connect timeout 5s, got %v", config.Timeouts.Connect)
	}
}

func TestWithRetry(t *testing.T) {
	config := DefaultConfig()

	retryConfig := RetryConfig{
		Enabled:      true,
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
	}

	WithRetry(retryConfig)(&config)

	if config.Retry.MaxAttempts != 5 {
		t.Errorf("expected max attempts 5, got %d", config.Retry.MaxAttempts)
	}
}

func TestWithCircuitBreaker(t *testing.T) {
	config := DefaultConfig()

	cbConfig := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 10,
		SuccessThreshold: 3,
		Timeout:          20 * time.Second,
	}

	WithCircuitBreaker(cbConfig)(&config)

	if config.CircuitBreaker.FailureThreshold != 10 {
		t.Errorf("expected failure threshold 10, got %d", config.CircuitBreaker.FailureThreshold)
	}
}

func TestWithRateLimiting(t *testing.T) {
	config := DefaultConfig()

	rlConfig := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 1000,
		Burst:          100,
	}

	WithRateLimiting(rlConfig)(&config)

	if config.RateLimiting.RequestsPerSec != 1000 {
		t.Errorf("expected 1000 rps, got %.0f", config.RateLimiting.RequestsPerSec)
	}
}

func TestWithHealthCheck(t *testing.T) {
	config := DefaultConfig()

	hcConfig := HealthCheckConfig{
		Enabled:  true,
		Interval: 5 * time.Second,
		Timeout:  2 * time.Second,
		Path:     "/custom-health",
	}

	WithHealthCheck(hcConfig)(&config)

	if config.HealthCheck.Interval != 5*time.Second {
		t.Errorf("expected interval 5s, got %v", config.HealthCheck.Interval)
	}
}

func TestWithLoadBalancing(t *testing.T) {
	config := DefaultConfig()

	lbConfig := LoadBalancingConfig{
		Strategy: StrategyWeightedRoundRobin,
		HashKey:  "user-id",
	}

	WithLoadBalancing(lbConfig)(&config)

	if config.LoadBalancing.Strategy != StrategyWeightedRoundRobin {
		t.Errorf("expected strategy %s, got %s", StrategyWeightedRoundRobin, config.LoadBalancing.Strategy)
	}
}

func TestWithAuth(t *testing.T) {
	config := DefaultConfig()

	authConfig := AuthConfig{
		Enabled: true,
		Providers: []AuthProviderConfig{
			{
				Type: "jwt",
				Config: map[string]any{
					"secret": "test-secret",
				},
			},
		},
	}

	WithAuth(authConfig)(&config)

	if len(config.Auth.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(config.Auth.Providers))
	}
}

func TestWithTLS(t *testing.T) {
	config := DefaultConfig()

	tlsConfig := TLSConfig{
		Enabled:  true,
		CertFile: "/path/to/cert.pem",
		KeyFile:  "/path/to/key.pem",
	}

	WithTLS(tlsConfig)(&config)

	if config.TLS.CertFile != "/path/to/cert.pem" {
		t.Errorf("expected cert file path, got %s", config.TLS.CertFile)
	}
}

func TestWithCaching(t *testing.T) {
	config := DefaultConfig()

	cacheConfig := CachingConfig{
		Enabled:    true,
		DefaultTTL: 5 * time.Minute,
	}

	WithCaching(cacheConfig)(&config)

	if config.Caching.DefaultTTL != 5*time.Minute {
		t.Errorf("expected TTL 5m, got %v", config.Caching.DefaultTTL)
	}
}

func TestWithDiscovery(t *testing.T) {
	config := DefaultConfig()

	discoveryConfig := DiscoveryConfig{
		Enabled:      true,
		AutoPrefix:   true,
		StripPrefix:  true,
		ServiceNames: []string{"service-a", "service-b"},
	}

	WithDiscovery(discoveryConfig)(&config)

	if len(config.Discovery.ServiceNames) != 2 {
		t.Fatalf("expected 2 services, got %d", len(config.Discovery.ServiceNames))
	}
}

func TestWithDiscoveryEnabled(t *testing.T) {
	config := DefaultConfig()
	WithDiscoveryEnabled(true)(&config)

	if !config.Discovery.Enabled {
		t.Error("expected discovery to be enabled")
	}
}

func TestWithOpenAPI(t *testing.T) {
	config := DefaultConfig()

	openAPIConfig := OpenAPIConfig{
		Enabled:         true,
		Path:            "/openapi.json",
		UIPath:          "/swagger",
		Title:           "Test Gateway",
		RefreshInterval: 60 * time.Second,
	}

	WithOpenAPI(openAPIConfig)(&config)

	if config.OpenAPI.Title != "Test Gateway" {
		t.Errorf("expected title 'Test Gateway', got %s", config.OpenAPI.Title)
	}
}

func TestWithOpenAPIEnabled(t *testing.T) {
	config := DefaultConfig()
	WithOpenAPIEnabled(true)(&config)

	if !config.OpenAPI.Enabled {
		t.Error("expected OpenAPI to be enabled")
	}
}

func TestWithDashboard(t *testing.T) {
	config := DefaultConfig()

	dashboardConfig := DashboardConfig{
		Enabled:  true,
		BasePath: "/admin",
		Title:    "Custom Dashboard",
	}

	WithDashboard(dashboardConfig)(&config)

	if config.Dashboard.Title != "Custom Dashboard" {
		t.Errorf("expected title 'Custom Dashboard', got %s", config.Dashboard.Title)
	}
}

func TestWithDashboardEnabled(t *testing.T) {
	config := DefaultConfig()
	WithDashboardEnabled(true)(&config)

	if !config.Dashboard.Enabled {
		t.Error("expected dashboard to be enabled")
	}
}

func TestWithCORS(t *testing.T) {
	config := DefaultConfig()

	corsConfig := CORSConfig{
		Enabled:      true,
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET", "POST"},
	}

	WithCORS(corsConfig)(&config)

	if len(config.CORS.AllowOrigins) != 1 {
		t.Fatalf("expected 1 allowed origin, got %d", len(config.CORS.AllowOrigins))
	}
}

func TestWithIPFilter(t *testing.T) {
	config := DefaultConfig()

	ipFilterConfig := IPFilterConfig{
		Enabled:   true,
		Whitelist: []string{"192.168.1.0/24"},
		Blacklist: []string{"10.0.0.0/8"},
	}

	WithIPFilter(ipFilterConfig)(&config)

	if len(config.IPFilter.Whitelist) != 1 {
		t.Fatalf("expected 1 whitelisted IP, got %d", len(config.IPFilter.Whitelist))
	}
}

func TestWithWebSocket(t *testing.T) {
	config := DefaultConfig()

	wsConfig := WebSocketConfig{
		Enabled:           true,
		ReadBufferSize:    2048,
		WriteBufferSize:   2048,
		HandshakeTimeout:  5 * time.Second,
		EnableCompression: true,
	}

	WithWebSocket(wsConfig)(&config)

	if config.WebSocket.ReadBufferSize != 2048 {
		t.Errorf("expected read buffer 2048, got %d", config.WebSocket.ReadBufferSize)
	}
}

func TestWithSSE(t *testing.T) {
	config := DefaultConfig()

	sseConfig := SSEConfig{
		Enabled:           true,
		RetryInterval:     3 * time.Second,
		KeepAliveInterval: 30 * time.Second,
	}

	WithSSE(sseConfig)(&config)

	if config.SSE.RetryInterval != 3*time.Second {
		t.Errorf("expected retry interval 3s, got %v", config.SSE.RetryInterval)
	}
}

func TestMultipleConfigOptions(t *testing.T) {
	config := NewExtension(
		WithEnabled(true),
		WithBasePath("/gateway"),
		WithRoute(RouteConfig{
			Path:    "/api/test",
			Targets: []TargetConfig{{URL: "http://localhost:8080"}},
		}),
		WithDiscoveryEnabled(true),
		WithOpenAPIEnabled(true),
		WithDashboardEnabled(true),
	).(*Extension).config

	if !config.Enabled {
		t.Error("expected gateway enabled")
	}

	if config.BasePath != "/gateway" {
		t.Errorf("expected base path /gateway, got %s", config.BasePath)
	}

	if len(config.Routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(config.Routes))
	}

	if !config.Discovery.Enabled {
		t.Error("expected discovery enabled")
	}

	if !config.OpenAPI.Enabled {
		t.Error("expected OpenAPI enabled")
	}

	if !config.Dashboard.Enabled {
		t.Error("expected dashboard enabled")
	}
}

func TestDefaultOpenAPIConfig(t *testing.T) {
	config := DefaultOpenAPIConfig()

	if !config.Enabled {
		t.Error("expected OpenAPI enabled by default")
	}

	if config.Path == "" {
		t.Error("expected default path")
	}

	if config.UIPath == "" {
		t.Error("expected default UI path")
	}

	if config.RefreshInterval == 0 {
		t.Error("expected default refresh interval")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{
			name:   "valid default config",
			config: DefaultConfig(),
			valid:  true,
		},
		{
			name: "valid with routes",
			config: Config{
				Enabled:  true,
				BasePath: "/gateway",
				Routes: []RouteConfig{
					{
						Path:    "/api/test",
						Targets: []TargetConfig{{URL: "http://localhost:8080"}},
					},
				},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation - just ensure no panics
			_ = tt.config.Enabled
			_ = tt.config.BasePath
			_ = len(tt.config.Routes)
		})
	}
}
