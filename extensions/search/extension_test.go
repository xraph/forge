package search

import (
	"context"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestNewExtension(t *testing.T) {
	ext := NewExtension()
	if ext == nil {
		t.Fatal("expected non-nil extension")
	}

	if ext.Name() != "search" {
		t.Errorf("expected name 'search', got '%s'", ext.Name())
	}

	if ext.Version() != "2.0.0" {
		t.Errorf("expected version '2.0.0', got '%s'", ext.Version())
	}
}

func TestNewExtensionWithConfig(t *testing.T) {
	config := Config{
		Driver: "inmemory",
	}

	ext := NewExtensionWithConfig(config)
	if ext == nil {
		t.Fatal("expected non-nil extension")
	}
}

func TestNewExtensionWithOptions(t *testing.T) {
	ext := NewExtension(
		WithDriver("elasticsearch"),
		WithURL("http://localhost:9200"),
		WithDefaultLimit(50),
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

	ext := NewExtension(WithDriver("inmemory"))

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Verify search service is registered
	search, err := forge.Resolve[Search](app.Container(), "search")
	if err != nil {
		t.Fatalf("failed to resolve search service: %v", err)
	}

	if search == nil {
		t.Fatal("expected non-nil search service")
	}
}

func TestExtensionRegisterInvalidDriver(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithDriver("invalid"))

	err := ext.(*Extension).Register(app)
	if err == nil {
		t.Fatal("expected error for invalid driver")
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
		Driver:             "elasticsearch",
		URL:                "", // Missing required URL
		MaxConnections:     0,  // Invalid
		MaxIdleConnections: 0,
		ConnectTimeout:     0, // Invalid
	}
	ext := NewExtensionWithConfig(config)

	err := ext.(*Extension).Register(app)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestExtensionStart(t *testing.T) {

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithDriver("inmemory"))

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

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithDriver("inmemory"))

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

	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	ext := NewExtension(WithDriver("inmemory"))

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
	ext := NewExtension(WithDriver("inmemory"))

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

	ext := NewExtension(WithDriver("inmemory"))

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

	// Get search instance
	search := ext.(*Extension).Search()
	if search == nil {
		t.Fatal("expected non-nil search instance")
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

func TestConfigOption_WithDriver(t *testing.T) {
	config := DefaultConfig()
	WithDriver("elasticsearch")(&config)

	if config.Driver != "elasticsearch" {
		t.Errorf("expected driver 'elasticsearch', got '%s'", config.Driver)
	}
}

func TestConfigOption_WithURL(t *testing.T) {
	config := DefaultConfig()
	WithURL("http://localhost:9200")(&config)

	if config.URL != "http://localhost:9200" {
		t.Errorf("expected url 'http://localhost:9200', got '%s'", config.URL)
	}
}

func TestConfigOption_WithHosts(t *testing.T) {
	config := DefaultConfig()
	hosts := []string{"host1", "host2"}
	WithHosts(hosts...)(&config)

	if len(config.Hosts) != 2 {
		t.Errorf("expected 2 hosts, got %d", len(config.Hosts))
	}
}

func TestConfigOption_WithAuth(t *testing.T) {
	config := DefaultConfig()
	WithAuth("user", "pass")(&config)

	if config.Username != "user" || config.Password != "pass" {
		t.Error("auth not set correctly")
	}
}

func TestConfigOption_WithAPIKey(t *testing.T) {
	config := DefaultConfig()
	WithAPIKey("key123")(&config)

	if config.APIKey != "key123" {
		t.Errorf("expected api_key 'key123', got '%s'", config.APIKey)
	}
}

func TestConfigOption_WithMaxConnections(t *testing.T) {
	config := DefaultConfig()
	WithMaxConnections(20)(&config)

	if config.MaxConnections != 20 {
		t.Errorf("expected max_connections 20, got %d", config.MaxConnections)
	}
}

func TestConfigOption_WithDefaultLimit(t *testing.T) {
	config := DefaultConfig()
	WithDefaultLimit(50)(&config)

	if config.DefaultLimit != 50 {
		t.Errorf("expected default_limit 50, got %d", config.DefaultLimit)
	}
}

func TestConfigOption_WithMaxLimit(t *testing.T) {
	config := DefaultConfig()
	WithMaxLimit(200)(&config)

	if config.MaxLimit != 200 {
		t.Errorf("expected max_limit 200, got %d", config.MaxLimit)
	}
}

func TestConfigOption_WithMetrics(t *testing.T) {
	config := DefaultConfig()
	WithMetrics(false)(&config)

	if config.EnableMetrics {
		t.Error("expected metrics disabled")
	}
}

func TestConfigOption_WithTracing(t *testing.T) {
	config := DefaultConfig()
	WithTracing(false)(&config)

	if config.EnableTracing {
		t.Error("expected tracing disabled")
	}
}

func TestConfigOption_WithRequireConfig(t *testing.T) {
	config := DefaultConfig()
	WithRequireConfig(true)(&config)

	if !config.RequireConfig {
		t.Error("expected require_config enabled")
	}
}

func TestConfigOption_WithConfig(t *testing.T) {
	originalConfig := Config{
		Driver: "custom",
	}
	config := DefaultConfig()
	WithConfig(originalConfig)(&config)

	if config.Driver != "custom" {
		t.Errorf("expected driver 'custom', got '%s'", config.Driver)
	}
}
