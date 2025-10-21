package graphql

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/v2"
)

func TestNewExtension(t *testing.T) {
	ext := NewExtension()
	if ext == nil {
		t.Fatal("expected non-nil extension")
	}

	if ext.Name() != "graphql" {
		t.Errorf("expected name 'graphql', got '%s'", ext.Name())
	}

	if ext.Version() != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", ext.Version())
	}
}

func TestNewExtensionWithConfig(t *testing.T) {
	config := Config{
		Endpoint: "/graphql",
	}

	ext := NewExtensionWithConfig(config)
	if ext == nil {
		t.Fatal("expected non-nil extension")
	}
}

func TestNewExtensionWithOptions(t *testing.T) {
	ext := NewExtension(
		WithEndpoint("/api/graphql"),
		WithPlayground(true),
		WithIntrospection(true),
	)

	if ext == nil {
		t.Fatal("expected non-nil extension")
	}
}

func TestExtensionRegister(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension()

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Verify graphql service is registered
	gql, err := forge.Resolve[GraphQL](app.Container(), "graphql")
	if err != nil {
		t.Fatalf("failed to resolve graphql service: %v", err)
	}

	if gql == nil {
		t.Fatal("expected non-nil graphql service")
	}
}

func TestExtensionRegisterInvalidConfig(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	config := Config{
		Endpoint:      "", // Invalid
		MaxDepth:      0,  // Invalid
		QueryTimeout:  0,  // Invalid
		MaxComplexity: -1, // Invalid
	}
	ext := NewExtensionWithConfig(config)

	err := ext.(*Extension).Register(app)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestExtensionStart(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension()

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	err = ext.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	if !ext.(*Extension).IsStarted() {
		t.Error("extension should be marked as started")
	}
}

func TestExtensionStop(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension()

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	err = ext.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	err = ext.Stop(ctx)
	if err != nil {
		t.Fatalf("failed to stop extension: %v", err)
	}

	if ext.(*Extension).IsStarted() {
		t.Error("extension should not be marked as started after stop")
	}
}

func TestExtensionHealth(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension()

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	err = ext.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	err = ext.Health(ctx)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
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
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension()

	// Register
	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	// Start
	ctx := context.Background()
	err = ext.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Health check
	err = ext.Health(ctx)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	// Get graphql instance
	gql := ext.(*Extension).GraphQL()
	if gql == nil {
		t.Fatal("expected non-nil graphql instance")
	}

	// Stop
	err = ext.Stop(ctx)
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

	if config.Endpoint != "/graphql" {
		t.Errorf("expected endpoint '/graphql', got '%s'", config.Endpoint)
	}

	if !config.EnablePlayground {
		t.Error("expected playground enabled by default")
	}

	if !config.EnableIntrospection {
		t.Error("expected introspection enabled by default")
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

func TestConfigValidate_MissingEndpoint(t *testing.T) {
	config := DefaultConfig()
	config.Endpoint = ""
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestConfigValidate_InvalidMaxComplexity(t *testing.T) {
	config := DefaultConfig()
	config.MaxComplexity = -1
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_complexity < 0")
	}
}

func TestConfigValidate_InvalidMaxDepth(t *testing.T) {
	config := DefaultConfig()
	config.MaxDepth = 0
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_depth < 1")
	}
}

func TestConfigValidate_InvalidQueryTimeout(t *testing.T) {
	config := DefaultConfig()
	config.QueryTimeout = 0
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for query_timeout = 0")
	}
}

func TestConfigValidate_InvalidDataLoaderBatchSize(t *testing.T) {
	config := DefaultConfig()
	config.DataLoaderBatchSize = 0
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for dataloader_batch_size = 0")
	}
}

func TestConfigValidate_InvalidMaxUploadSize(t *testing.T) {
	config := DefaultConfig()
	config.MaxUploadSize = -1
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for max_upload_size < 0")
	}
}

func TestConfigOptions_MultipleOptions(t *testing.T) {
	config := DefaultConfig()

	opts := []ConfigOption{
		WithEndpoint("/api/graphql"),
		WithPlayground(false),
		WithIntrospection(false),
		WithMaxComplexity(500),
		WithMaxDepth(10),
		WithTimeout(60 * time.Second),
		WithDataLoader(false),
		WithQueryCache(false, 1*time.Minute),
		WithCORS("http://localhost:3000"),
		WithMetrics(false),
		WithTracing(false),
	}

	for _, opt := range opts {
		opt(&config)
	}

	if config.Endpoint != "/api/graphql" {
		t.Error("endpoint not set")
	}
	if config.EnablePlayground {
		t.Error("playground should be disabled")
	}
	if config.EnableIntrospection {
		t.Error("introspection should be disabled")
	}
	if config.MaxComplexity != 500 {
		t.Error("max_complexity not set")
	}
	if config.MaxDepth != 10 {
		t.Error("max_depth not set")
	}
	if config.QueryTimeout != 60*time.Second {
		t.Error("timeout not set")
	}
	if config.EnableDataLoader {
		t.Error("dataloader should be disabled")
	}
	if config.EnableQueryCache {
		t.Error("query cache should be disabled")
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
		Endpoint: "/custom",
	}
	config := DefaultConfig()
	WithConfig(originalConfig)(&config)
	if config.Endpoint != "/custom" {
		t.Errorf("expected endpoint '/custom', got '%s'", config.Endpoint)
	}
}

func TestConfigOption_WithRequireConfig(t *testing.T) {
	config := DefaultConfig()
	WithRequireConfig(true)(&config)
	if !config.RequireConfig {
		t.Error("expected require_config enabled")
	}
}
