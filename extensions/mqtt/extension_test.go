package mqtt

import (
	"context"
	"testing"

	"github.com/xraph/confy"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestNewExtension(t *testing.T) {
	ext := NewExtension()
	if ext == nil {
		t.Fatal("expected extension to be created")
	}

	mqttExt, ok := ext.(*Extension)
	if !ok {
		t.Fatal("expected extension to be *Extension")
	}

	if mqttExt.BaseExtension == nil {
		t.Error("expected BaseExtension to be initialized")
	}
}

func TestNewExtensionWithOptions(t *testing.T) {
	ext := NewExtension(
		WithBroker("tcp://test:1883"),
		WithClientID("test-client"),
		WithQoS(2),
	)

	mqttExt := ext.(*Extension)

	if mqttExt.config.Broker != "tcp://test:1883" {
		t.Errorf("expected broker 'tcp://test:1883', got %s", mqttExt.config.Broker)
	}

	if mqttExt.config.ClientID != "test-client" {
		t.Errorf("expected client_id 'test-client', got %s", mqttExt.config.ClientID)
	}

	if mqttExt.config.DefaultQoS != 2 {
		t.Errorf("expected default_qos 2, got %d", mqttExt.config.DefaultQoS)
	}
}

func TestNewExtensionWithConfig(t *testing.T) {
	customConfig := Config{
		Broker:       "tcp://custom:1883",
		ClientID:     "custom-client",
		DefaultQoS:   1,
		MessageStore: "memory",
	}

	ext := NewExtensionWithConfig(customConfig)
	mqttExt := ext.(*Extension)

	if mqttExt.config.Broker != "tcp://custom:1883" {
		t.Errorf("expected broker 'tcp://custom:1883', got %s", mqttExt.config.Broker)
	}
}

func TestExtensionRegister(t *testing.T) {
	testApp := createTestApp(t)
	ext := NewExtension(
		WithBroker("tcp://localhost:1883"),
		WithClientID("test-client"),
	)

	err := ext.Register(testApp)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Verify client is registered in DI
	client, err := forge.Inject[MQTT](testApp.Container())
	if err != nil {
		t.Fatalf("failed to resolve mqtt client: %v", err)
	}

	if client == nil {
		t.Error("expected client to be resolved")
	}
}

func TestExtensionRegisterInvalidConfig(t *testing.T) {
	testApp := createTestApp(t)
	ext := NewExtension(
		WithBroker(""), // Invalid: empty broker
		WithClientID("test-client"),
	)

	err := ext.Register(testApp)
	if err == nil {
		t.Error("expected error with invalid config")
	}
}

func TestExtensionRegisterRequireConfigFails(t *testing.T) {
	testApp := createTestApp(t)
	ext := NewExtension(
		WithRequireConfig(true),
	)

	err := ext.Register(testApp)
	if err == nil {
		t.Error("expected error when requiring config that doesn't exist")
	}
}

func TestExtensionClient(t *testing.T) {
	testApp := createTestApp(t)
	ext := NewExtension(
		WithBroker("tcp://localhost:1883"),
		WithClientID("test-client"),
	).(*Extension)

	err := ext.Register(testApp)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Client is managed by Vessel - resolve from container
	client, err := forge.Inject[MQTT](testApp.Container())
	if err != nil {
		t.Fatalf("failed to resolve mqtt client: %v", err)
	}
	if client == nil {
		t.Error("expected client to be returned")
	}
}

func TestExtensionHealthNotStarted(t *testing.T) {
	testApp := createTestApp(t)
	ext := NewExtension(
		WithBroker("tcp://localhost:1883"),
		WithClientID("test-client"),
	)

	err := ext.Register(testApp)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	err = ext.Health(ctx)
	if err == nil {
		t.Error("expected health check to fail when not connected")
	}
}

func TestExtensionStopNotStarted(t *testing.T) {
	testApp := createTestApp(t)
	ext := NewExtension(
		WithBroker("tcp://localhost:1883"),
		WithClientID("test-client"),
	)

	err := ext.Register(testApp)
	if err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	ctx := context.Background()
	err = ext.Stop(ctx)
	// Should not error even if not started
	if err != nil {
		t.Errorf("unexpected error stopping extension: %v", err)
	}
}

func TestExtensionName(t *testing.T) {
	ext := NewExtension()
	if ext.Name() != "mqtt" {
		t.Errorf("expected extension name 'mqtt', got %s", ext.Name())
	}
}

func TestExtensionVersion(t *testing.T) {
	ext := NewExtension()
	if ext.Version() != "2.0.0" {
		t.Errorf("expected extension version '2.0.0', got %s", ext.Version())
	}
}

func TestExtensionDescription(t *testing.T) {
	ext := NewExtension()
	if ext.Description() != "MQTT client with pub/sub support" {
		t.Errorf("expected extension description 'MQTT client with pub/sub support', got %s", ext.Description())
	}
}

// Helper function to create a test app
func createTestApp(t *testing.T) forge.App {
	t.Helper()

	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()
	cfg := confy.NewTestConfyImpl()

	testApp := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(log),
		forge.WithAppMetrics(met),
		forge.WithAppConfigManager(cfg),
	)

	return testApp
}
