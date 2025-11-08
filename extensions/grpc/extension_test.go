package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
	"google.golang.org/grpc"
)

func TestNewExtension(t *testing.T) {
	ext := NewExtension()
	if ext == nil {
		t.Fatal("expected non-nil extension")
	}

	if ext.Name() != "grpc" {
		t.Errorf("expected name 'grpc', got '%s'", ext.Name())
	}

	if ext.Version() != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", ext.Version())
	}
}

func TestNewExtensionWithConfig(t *testing.T) {
	config := Config{
		Address: ":50051",
	}

	ext := NewExtensionWithConfig(config)
	if ext == nil {
		t.Fatal("expected non-nil extension")
	}
}

func TestNewExtensionWithOptions(t *testing.T) {
	ext := NewExtension(
		WithAddress(":9090"),
		WithReflection(true),
		WithHealthCheck(true),
	)

	if ext == nil {
		t.Fatal("expected non-nil extension")
	}
}

func TestExtensionRegister(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension()

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Verify grpc service is registered
	srv, err := forge.Resolve[GRPC](app.Container(), "grpc")
	if err != nil {
		t.Fatalf("failed to resolve grpc service: %v", err)
	}

	if srv == nil {
		t.Fatal("expected non-nil grpc service")
	}
}

func TestExtensionRegisterInvalidConfig(t *testing.T) {
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	config := Config{
		Address:        "", // Invalid
		MaxRecvMsgSize: -1, // Invalid
		MaxSendMsgSize: -1, // Invalid
	}
	ext := NewExtensionWithConfig(config)

	err := ext.(*Extension).Register(app)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestExtensionRegisterWithTLS(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	config := Config{
		Address:     ":50051",
		EnableTLS:   true,
		TLSCertFile: "", // Missing cert
		TLSKeyFile:  "", // Missing key
	}
	ext := NewExtensionWithConfig(config)

	err := ext.(*Extension).Register(app)
	if err == nil {
		t.Fatal("expected error for TLS without cert files")
	}
}

func TestExtensionStart(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithAddress("127.0.0.1:0")) // Use random port

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		err := ext.Start(ctx)
		if err != nil {
			t.Logf("start error: %v", err)
		}
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Stop the server
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	_ = ext.Stop(stopCtx)
}

func TestExtensionStop(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithAddress("127.0.0.1:0"))

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = ext.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	err = ext.Stop(stopCtx)
	if err != nil {
		t.Fatalf("failed to stop extension: %v", err)
	}
}

func TestExtensionHealth(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithAddress("127.0.0.1:0"))

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = ext.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	err = ext.Health(context.Background())
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	// Cleanup
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	_ = ext.Stop(stopCtx)
}

func TestExtensionHealthNotStarted(t *testing.T) {
	ext := NewExtension()

	ctx := context.Background()
	err := ext.Health(ctx)
	if err == nil {
		t.Fatal("expected error for health check on non-started extension")
	}
}

func TestExtensionLifecycle(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithAddress("127.0.0.1:0"))

	// Register
	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	// Start
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = ext.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Health check
	err = ext.Health(context.Background())
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	// Get grpc instance
	srv := ext.(*Extension).GRPC()
	if srv == nil {
		t.Fatal("expected non-nil grpc instance")
	}

	// Stop
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	err = ext.Stop(stopCtx)
	if err != nil {
		t.Fatalf("failed to stop: %v", err)
	}
}

func TestExtensionDependencies(t *testing.T) {
	ext := NewExtension()
	deps := ext.Dependencies()
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(deps))
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Address != ":50051" {
		t.Errorf("expected address ':50051', got '%s'", config.Address)
	}

	if !config.EnableHealthCheck {
		t.Error("expected health check enabled by default")
	}

	if !config.EnableReflection {
		t.Error("expected reflection enabled by default")
	}

	if !config.EnableMetrics {
		t.Error("expected metrics enabled by default")
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	config := DefaultConfig()
	err := config.Validate()
	if err != nil {
		t.Fatalf("valid config should not error: %v", err)
	}
}

func TestConfigValidate_MissingAddress(t *testing.T) {
	config := DefaultConfig()
	config.Address = ""
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestConfigValidate_InvalidMaxRecvMsgSize(t *testing.T) {
	config := DefaultConfig()
	config.MaxRecvMsgSize = -1
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_recv_msg_size < 0")
	}
}

func TestConfigValidate_InvalidMaxSendMsgSize(t *testing.T) {
	config := DefaultConfig()
	config.MaxSendMsgSize = -1
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_send_msg_size < 0")
	}
}

func TestConfigValidate_TLSMissingCert(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSCertFile = ""
	config.TLSKeyFile = ""
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for TLS without cert files")
	}
}

func TestConfigValidate_ClientAuthMissingCA(t *testing.T) {
	config := DefaultConfig()
	config.EnableTLS = true
	config.TLSCertFile = "cert.pem"
	config.TLSKeyFile = "key.pem"
	config.ClientAuth = true
	config.TLSCAFile = ""
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for client auth without CA file")
	}
}

func TestConfigOptions_MultipleOptions(t *testing.T) {
	config := DefaultConfig()

	opts := []ConfigOption{
		WithAddress(":9090"),
		WithMaxMessageSize(8 * 1024 * 1024),
		WithMaxConcurrentStreams(100),
		WithTLS("cert.pem", "key.pem", "ca.pem"),
		WithClientAuth(true),
		WithHealthCheck(false),
		WithReflection(false),
		WithMetrics(false),
		WithTracing(false),
	}

	for _, opt := range opts {
		opt(&config)
	}

	if config.Address != ":9090" {
		t.Error("address not set")
	}
	if config.MaxRecvMsgSize != 8*1024*1024 {
		t.Error("max message size not set")
	}
	if config.MaxConcurrentStreams != 100 {
		t.Error("max concurrent streams not set")
	}
	if !config.EnableTLS {
		t.Error("TLS should be enabled")
	}
	if config.TLSCertFile != "cert.pem" {
		t.Error("cert file not set")
	}
	if !config.ClientAuth {
		t.Error("client auth should be enabled")
	}
	if config.EnableHealthCheck {
		t.Error("health check should be disabled")
	}
	if config.EnableReflection {
		t.Error("reflection should be disabled")
	}
	if config.EnableMetrics {
		t.Error("metrics should be disabled")
	}
	if config.EnableTracing {
		t.Error("tracing should be disabled")
	}
}

func TestConfigOption_WithConfig(t *testing.T) {
	originalConfig := Config{
		Address: ":8080",
	}
	config := DefaultConfig()
	WithConfig(originalConfig)(&config)
	if config.Address != ":8080" {
		t.Errorf("expected address ':8080', got '%s'", config.Address)
	}
}

func TestConfigOption_WithRequireConfig(t *testing.T) {
	config := DefaultConfig()
	WithRequireConfig(true)(&config)
	if !config.RequireConfig {
		t.Error("expected require_config enabled")
	}
}

// Mock health checker for testing
type mockHealthChecker struct {
	healthy bool
}

func (m *mockHealthChecker) Check(ctx context.Context) error {
	if !m.healthy {
		return ErrHealthCheckFailed
	}
	return nil
}

func TestGRPCServer_RegisterHealthChecker(t *testing.T) {
	server := NewGRPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*grpcServer)

	checker := &mockHealthChecker{healthy: true}
	server.RegisterHealthChecker("test-service", checker)

	// Verify checker was registered
	if len(server.healthCheckers) != 1 {
		t.Errorf("expected 1 health checker, got %d", len(server.healthCheckers))
	}
}

func TestGRPCServer_AddInterceptors(t *testing.T) {
	server := NewGRPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*grpcServer)

	// Add unary interceptor
	unaryInt := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
	server.AddUnaryInterceptor(unaryInt)

	// Add stream interceptor
	streamInt := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, ss)
	}
	server.AddStreamInterceptor(streamInt)

	// These should not error
}

func TestGRPCServer_IsRunning(t *testing.T) {
	server := NewGRPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*grpcServer)

	if server.IsRunning() {
		t.Error("expected server not running")
	}
}

func TestGRPCServer_GetServer(t *testing.T) {
	server := NewGRPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*grpcServer)

	if server.GetServer() != nil {
		t.Error("expected nil server before initialization")
	}
}

func TestGRPCServer_Ping(t *testing.T) {
	server := NewGRPCServer(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics()).(*grpcServer)

	err := server.Ping(context.Background())
	if err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}
