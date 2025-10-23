package queue

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
)

func TestNewExtension(t *testing.T) {
	ext := NewExtension()
	if ext == nil {
		t.Fatal("expected non-nil extension")
	}

	if ext.Name() != "queue" {
		t.Errorf("expected name 'queue', got '%s'", ext.Name())
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
		WithDriver("rabbitmq"),
		WithURL("amqp://localhost:5672"),
		WithPrefetch(20),
	)

	if ext == nil {
		t.Fatal("expected non-nil extension")
	}
}

func TestExtensionRegister(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension(WithDriver("inmemory"))

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Verify queue service is registered
	q, err := forge.Resolve[Queue](app.Container(), "queue")
	if err != nil {
		t.Fatalf("failed to resolve queue service: %v", err)
	}

	if q == nil {
		t.Fatal("expected non-nil queue service")
	}
}

func TestExtensionRegisterInvalidDriver(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension(WithDriver("invalid"))

	err := ext.(*Extension).Register(app)
	if err == nil {
		t.Fatal("expected error for invalid driver")
	}
}

func TestExtensionRegisterInvalidConfig(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	config := Config{
		Driver:             "rabbitmq",
		URL:                "", // Missing required URL
		MaxConnections:     0,  // Invalid
		ConnectTimeout:     0,  // Invalid
		DefaultPrefetch:    -1, // Invalid
		DefaultConcurrency: 0,  // Invalid
	}
	ext := NewExtensionWithConfig(config)

	err := ext.(*Extension).Register(app)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestExtensionStart(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
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
	app := forge.NewApp(forge.DefaultAppConfig())
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
	app := forge.NewApp(forge.DefaultAppConfig())
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
	app := forge.NewApp(forge.DefaultAppConfig())
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

	// Get queue instance
	queue := ext.(*Extension).Queue()
	if queue == nil {
		t.Fatal("expected non-nil queue instance")
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
	WithDriver("rabbitmq")(&config)
	if config.Driver != "rabbitmq" {
		t.Errorf("expected driver 'rabbitmq', got '%s'", config.Driver)
	}
}

func TestConfigOption_WithURL(t *testing.T) {
	config := DefaultConfig()
	WithURL("amqp://localhost:5672")(&config)
	if config.URL != "amqp://localhost:5672" {
		t.Errorf("expected url 'amqp://localhost:5672', got '%s'", config.URL)
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

func TestConfigOption_WithVHost(t *testing.T) {
	config := DefaultConfig()
	WithVHost("/test")(&config)
	if config.VHost != "/test" {
		t.Errorf("expected vhost '/test', got '%s'", config.VHost)
	}
}

func TestConfigOption_WithMaxConnections(t *testing.T) {
	config := DefaultConfig()
	WithMaxConnections(20)(&config)
	if config.MaxConnections != 20 {
		t.Errorf("expected max_connections 20, got %d", config.MaxConnections)
	}
}

func TestConfigOption_WithPrefetch(t *testing.T) {
	config := DefaultConfig()
	WithPrefetch(50)(&config)
	if config.DefaultPrefetch != 50 {
		t.Errorf("expected prefetch 50, got %d", config.DefaultPrefetch)
	}
}

func TestConfigOption_WithConcurrency(t *testing.T) {
	config := DefaultConfig()
	WithConcurrency(10)(&config)
	if config.DefaultConcurrency != 10 {
		t.Errorf("expected concurrency 10, got %d", config.DefaultConcurrency)
	}
}

func TestConfigOption_WithTimeout(t *testing.T) {
	config := DefaultConfig()
	timeout := 60 * time.Second
	WithTimeout(timeout)(&config)
	if config.DefaultTimeout != timeout {
		t.Errorf("expected timeout %v, got %v", timeout, config.DefaultTimeout)
	}
}

func TestConfigOption_WithDeadLetter(t *testing.T) {
	config := DefaultConfig()
	WithDeadLetter(false)(&config)
	if config.EnableDeadLetter {
		t.Error("expected dead letter disabled")
	}
}

func TestConfigOption_WithPersistence(t *testing.T) {
	config := DefaultConfig()
	WithPersistence(false)(&config)
	if config.EnablePersistence {
		t.Error("expected persistence disabled")
	}
}

func TestConfigOption_WithPriority(t *testing.T) {
	config := DefaultConfig()
	WithPriority(true)(&config)
	if !config.EnablePriority {
		t.Error("expected priority enabled")
	}
}

func TestConfigOption_WithDelayed(t *testing.T) {
	config := DefaultConfig()
	WithDelayed(true)(&config)
	if !config.EnableDelayed {
		t.Error("expected delayed enabled")
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

func TestDefaultQueueOptions(t *testing.T) {
	opts := DefaultQueueOptions()
	if !opts.Durable {
		t.Error("expected durable true")
	}
	if opts.AutoDelete {
		t.Error("expected auto_delete false")
	}
}

func TestDefaultConsumeOptions(t *testing.T) {
	opts := DefaultConsumeOptions()
	if opts.AutoAck {
		t.Error("expected auto_ack false")
	}
	if opts.PrefetchCount != 10 {
		t.Errorf("expected prefetch_count 10, got %d", opts.PrefetchCount)
	}
	if opts.Concurrency != 1 {
		t.Errorf("expected concurrency 1, got %d", opts.Concurrency)
	}
	if opts.RetryStrategy.MaxRetries != 3 {
		t.Errorf("expected max_retries 3, got %d", opts.RetryStrategy.MaxRetries)
	}
}
