package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

// Simple test service (no proto required for basic tests)
const bufSize = 1024 * 1024

var _ *bufconn.Listener // Unused but satisfies import

func TestGRPCServer_StartStop(t *testing.T) {
	// Create server
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.EnableHealthCheck = true
	config.EnableReflection = true
	config.EnableMetrics = true
	config.EnableLogging = true

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	// Start server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	if !server.IsRunning() {
		t.Error("expected server to be running")
	}

	// Verify GetServer returns non-nil
	if server.GetServer() == nil {
		t.Error("expected non-nil grpc.Server")
	}

	// Stop server
	err = server.Stop(ctx)
	if err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	// Verify server is stopped
	if server.IsRunning() {
		t.Error("expected server to be stopped")
	}
}

func TestGRPCServer_HealthCheck(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.EnableHealthCheck = true

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Get actual address
	impl := server.(*grpcServer)
	addr := impl.listener.Addr().String()

	// Create client
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Check health
	healthClient := grpc_health_v1.NewHealthClient(conn)
	healthResp, err := healthClient.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	if healthResp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING status, got %v", healthResp.Status)
	}
}

func TestGRPCServer_CustomHealthChecker(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.EnableHealthCheck = true

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	// Register custom health checker
	checker := &mockHealthChecker{healthy: true}
	server.RegisterHealthChecker("test-service", checker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Get actual address
	impl := server.(*grpcServer)
	addr := impl.listener.Addr().String()

	// Create client
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Check health for specific service
	healthClient := grpc_health_v1.NewHealthClient(conn)
	healthResp, err := healthClient.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{
		Service: "test-service",
	})
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	if healthResp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING status, got %v", healthResp.Status)
	}

	// Mark checker as unhealthy
	checker.healthy = false

	healthResp, err = healthClient.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{
		Service: "test-service",
	})
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	if healthResp.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Errorf("expected NOT_SERVING status, got %v", healthResp.Status)
	}
}

func TestGRPCServer_CustomInterceptor(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.EnableMetrics = false
	config.EnableLogging = false

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	// Add custom interceptor
	server.AddUnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	impl := server.(*grpcServer)
	if len(impl.customUnaryInterceptors) != 1 {
		t.Errorf("expected 1 custom interceptor, got %d", len(impl.customUnaryInterceptors))
	}
}

func TestGRPCServer_ServerStats(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.EnableMetrics = true

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Check initial stats
	stats := server.GetStats()
	if stats.RPCsStarted != 0 {
		t.Errorf("expected 0 RPCs started, got %d", stats.RPCsStarted)
	}

	if stats.StartTime == 0 {
		t.Error("expected start time to be set")
	}
}

func TestGRPCServer_GetServices(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.EnableHealthCheck = true
	config.EnableReflection = true

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop(ctx)

	// Get services
	services := server.GetServices()

	if len(services) == 0 {
		t.Fatal("expected at least one service")
	}

	// Should have health and reflection services
	foundHealth := false

	for _, svc := range services {
		if svc.Name == "grpc.health.v1.Health" {
			foundHealth = true
			if len(svc.Methods) == 0 {
				t.Error("expected health service to have methods")
			}
		}
	}

	if !foundHealth {
		t.Error("expected to find health service")
	}
}

func TestGRPCServer_GracefulStop(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Graceful stop should not error
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	err = server.GracefulStop(stopCtx)
	if err != nil {
		t.Fatalf("graceful stop failed: %v", err)
	}

	// Server should no longer be running
	if server.IsRunning() {
		t.Error("server should not be running after graceful stop")
	}
}

func TestGRPCServer_MessageSizeLimits(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.MaxRecvMsgSize = 100 // Very small limit
	config.EnableMetrics = false
	config.EnableLogging = false

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx, config.Address)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop(ctx)

	// Verify config was applied
	impl := server.(*grpcServer)
	if impl.config.MaxRecvMsgSize != 100 {
		t.Errorf("expected MaxRecvMsgSize 100, got %d", impl.config.MaxRecvMsgSize)
	}
}

// TestGRPCServer_NoDeadlockOnStartWithInterceptors verifies that starting the server
// with metrics, logging, and custom interceptors enabled doesn't cause a deadlock.
// This is a regression test for the deadlock bug where Start() held a write lock
// while calling buildServerOptions() which tried to acquire a read lock.
func TestGRPCServer_NoDeadlockOnStartWithInterceptors(t *testing.T) {
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"
	config.EnableMetrics = true
	config.EnableLogging = true
	config.EnableTracing = true

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	server := NewGRPCServer(config, logger, metrics)

	// Add custom interceptors before starting
	server.AddUnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	})
	server.AddStreamInterceptor(func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, ss)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This should complete without deadlock
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx, config.Address)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("failed to start server: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("start operation deadlocked - timeout exceeded")
	}

	defer server.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	// Verify server started correctly
	if !server.IsRunning() {
		t.Error("expected server to be running")
	}
}
