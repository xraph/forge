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

// =============================================================================
// Types Tests (unique to this file)
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
