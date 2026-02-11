package gateway

import (
	"testing"
	"time"
)

// =============================================================================
// Basic Tests - These verify the core functionality works
// =============================================================================

func TestNewExtension(t *testing.T) {
	ext := NewExtension(
		WithEnabled(true),
		WithBasePath("/gateway"),
	)

	if ext == nil {
		t.Fatal("expected extension, got nil")
	}

	gateway, ok := ext.(*Extension)
	if !ok {
		t.Fatal("expected *Extension type")
	}

	if !gateway.config.Enabled {
		t.Error("expected gateway enabled")
	}

	if gateway.config.BasePath != "/gateway" {
		t.Errorf("expected base path /gateway, got %s", gateway.config.BasePath)
	}
}

func TestExtension_Name(t *testing.T) {
	ext := NewExtension().(*Extension)

	if ext.Name() != "gateway" {
		t.Errorf("expected name 'gateway', got %s", ext.Name())
	}
}

func TestExtension_Version(t *testing.T) {
	ext := NewExtension().(*Extension)

	if ext.Version() == "" {
		t.Error("expected non-empty version")
	}
}

// =============================================================================
// Route Manager Tests
// =============================================================================

func TestRouteManager_AddAndGet(t *testing.T) {
	rm := NewRouteManager()

	route := &Route{
		ID:      "test-route",
		Path:    "/api/test",
		Methods: []string{"GET"},
		Targets: []*Target{
			{ID: "t1", URL: "http://localhost:8080", Healthy: true},
		},
		Protocol: ProtocolHTTP,
		Enabled:  true,
	}

	err := rm.AddRoute(route)
	if err != nil {
		t.Fatalf("failed to add route: %v", err)
	}

	retrieved, ok := rm.GetRoute("test-route")
	if !ok {
		t.Fatal("route not found after adding")
	}

	if retrieved.ID != route.ID {
		t.Errorf("expected route ID %s, got %s", route.ID, retrieved.ID)
	}
}

func TestRouteManager_ListRoutes(t *testing.T) {
	rm := NewRouteManager()

	routes := []*Route{
		{ID: "route-1", Path: "/api/1", Protocol: ProtocolHTTP, Targets: []*Target{{ID: "t1", URL: "http://localhost:8080"}}},
		{ID: "route-2", Path: "/api/2", Protocol: ProtocolHTTP, Targets: []*Target{{ID: "t2", URL: "http://localhost:8081"}}},
	}

	for _, route := range routes {
		if err := rm.AddRoute(route); err != nil {
			t.Fatalf("failed to add route: %v", err)
		}
	}

	list := rm.ListRoutes()
	if len(list) != 2 {
		t.Errorf("expected 2 routes, got %d", len(list))
	}
}

// =============================================================================
// Load Balancer Tests
// =============================================================================

func TestNewLoadBalancer(t *testing.T) {
	strategies := []LoadBalanceStrategy{
		LBRoundRobin,
		LBWeightedRoundRobin,
		LBRandom,
		LBLeastConnections,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			lb := NewLoadBalancer(strategy)
			if lb == nil {
				t.Fatal("expected load balancer, got nil")
			}
		})
	}
}

// =============================================================================
// Circuit Breaker Tests
// =============================================================================

func TestNewCircuitBreaker(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)
	if cb == nil {
		t.Fatal("expected circuit breaker, got nil")
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected initial state closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_OpenAfterFailures(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     1 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Record failures
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Errorf("expected state open after failures, got %s", cb.State())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Reset
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("expected state closed after reset, got %s", cb.State())
	}
}

func TestCircuitBreakerManager_Get(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	mgr := NewCircuitBreakerManager(config)

	cb1 := mgr.Get("target-1")
	cb2 := mgr.Get("target-1")
	cb3 := mgr.Get("target-2")

	if cb1 != cb2 {
		t.Error("expected same instance for same target")
	}

	if cb1 == cb3 {
		t.Error("expected different instances for different targets")
	}
}

// =============================================================================
// Rate Limiter Tests
// =============================================================================

func TestNewRateLimiter(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 10,
		Burst:          5,
	}

	rl := NewRateLimiter(config)
	if rl == nil {
		t.Fatal("expected rate limiter, got nil")
	}
}

// =============================================================================
// Config Tests
// =============================================================================

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if !config.Enabled {
		t.Error("expected gateway enabled by default")
	}

	// BasePath may be empty by default, that's okay

	if config.HealthCheck.Interval == 0 {
		t.Error("expected default health check interval")
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
		Methods: []string{"GET"},
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

func TestWithDiscoveryEnabled(t *testing.T) {
	config := DefaultConfig()
	WithDiscoveryEnabled(true)(&config)

	if !config.Discovery.Enabled {
		t.Error("expected discovery enabled")
	}
}

func TestWithOpenAPIEnabled(t *testing.T) {
	config := DefaultConfig()
	WithOpenAPIEnabled(true)(&config)

	if !config.OpenAPI.Enabled {
		t.Error("expected OpenAPI enabled")
	}
}

func TestWithDashboardEnabled(t *testing.T) {
	config := DefaultConfig()
	WithDashboardEnabled(true)(&config)

	if !config.Dashboard.Enabled {
		t.Error("expected dashboard enabled")
	}
}

// =============================================================================
// Types Tests
// =============================================================================

func TestCircuitState_Values(t *testing.T) {
	states := []CircuitState{CircuitClosed, CircuitOpen, CircuitHalfOpen}

	for _, state := range states {
		if string(state) == "" {
			t.Errorf("circuit state %v has empty string value", state)
		}
	}
}

func TestRouteProtocol_Values(t *testing.T) {
	protocols := []RouteProtocol{
		ProtocolHTTP,
		ProtocolWebSocket,
		ProtocolSSE,
		ProtocolGRPC,
	}

	for _, protocol := range protocols {
		if string(protocol) == "" {
			t.Errorf("protocol %v has empty string value", protocol)
		}
	}
}

func TestLoadBalanceStrategy_Values(t *testing.T) {
	strategies := []LoadBalanceStrategy{
		LBRoundRobin,
		LBWeightedRoundRobin,
		LBRandom,
		LBLeastConnections,
	}

	for _, strategy := range strategies {
		if string(strategy) == "" {
			t.Errorf("strategy %v has empty string value", strategy)
		}
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkRouteManager_AddRoute(b *testing.B) {
	rm := NewRouteManager()

	route := &Route{
		ID:       "test-route",
		Path:     "/api/test",
		Protocol: ProtocolHTTP,
		Targets:  []*Target{{ID: "t1", URL: "http://localhost:8080"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		route.ID = string(rune(i))
		_ = rm.AddRoute(route)
	}
}

func BenchmarkCircuitBreaker_Allow(b *testing.B) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Allow()
	}
}
