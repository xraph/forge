package kafka

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

	kafkaExt, ok := ext.(*Extension)
	if !ok {
		t.Fatal("expected extension to be *Extension")
	}

	if kafkaExt.BaseExtension == nil {
		t.Error("expected BaseExtension to be initialized")
	}
}

func TestNewExtensionWithOptions(t *testing.T) {
	ext := NewExtension(
		WithBrokers("broker1:9092", "broker2:9092"),
		WithClientID("test-client"),
		WithVersion("3.0.0"),
	)

	kafkaExt := ext.(*Extension)

	if len(kafkaExt.config.Brokers) != 2 {
		t.Errorf("expected 2 brokers, got %d", len(kafkaExt.config.Brokers))
	}

	if kafkaExt.config.ClientID != "test-client" {
		t.Errorf("expected client_id 'test-client', got %s", kafkaExt.config.ClientID)
	}

	if kafkaExt.config.Version != "3.0.0" {
		t.Errorf("expected version '3.0.0', got %s", kafkaExt.config.Version)
	}
}

func TestNewExtensionWithConfig(t *testing.T) {
	customConfig := Config{
		Brokers:                []string{"custom:9092"},
		ClientID:               "custom-client",
		Version:                "3.0.0",
		ProducerCompression:    "snappy",
		ProducerAcks:           "local",
		ConsumerOffsets:        "newest",
		ConsumerIsolation:      "read_uncommitted",
		ConsumerGroupRebalance: "range",
	}

	ext := NewExtensionWithConfig(customConfig)
	kafkaExt := ext.(*Extension)

	if kafkaExt.config.Brokers[0] != "custom:9092" {
		t.Errorf("expected broker 'custom:9092', got %s", kafkaExt.config.Brokers[0])
	}
}

func TestExtensionRegisterInvalidConfig(t *testing.T) {
	testApp := createTestApp(t)
	ext := NewExtension(
		WithBrokers(), // Invalid: empty brokers
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
	ext := NewExtension(
		WithBrokers("localhost:9092"),
		WithClientID("test-client"),
	).(*Extension)

	client := ext.Client()
	if client != nil {
		t.Error("expected client to be nil before registration")
	}
}

func TestExtensionName(t *testing.T) {
	ext := NewExtension()
	if ext.Name() != "kafka" {
		t.Errorf("expected extension name 'kafka', got %s", ext.Name())
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
	expected := "Kafka client with producer and consumer support"
	if ext.Description() != expected {
		t.Errorf("expected extension description '%s', got %s", expected, ext.Description())
	}
}

func TestExtensionStopNotStarted(t *testing.T) {
	ext := NewExtension(
		WithBrokers("localhost:9092"),
		WithClientID("test-client"),
	).(*Extension)

	// Create a mock client
	config := DefaultConfig()
	log := logger.NewNoopLogger()
	met := forge.NewNoOpMetrics()

	// Initialize the extension properly
	ext.SetLogger(log)
	ext.SetMetrics(met)

	ext.client = &kafkaClient{
		config:  config,
		logger:  log,
		metrics: met,
		stats:   ClientStats{},
	}

	ctx := context.Background()
	err := ext.Stop(ctx)
	// Should not error even if not started
	if err != nil {
		t.Errorf("unexpected error stopping extension: %v", err)
	}
}

func TestExtensionHealthNotInitialized(t *testing.T) {
	ext := NewExtension().(*Extension)

	ctx := context.Background()
	err := ext.Health(ctx)
	if err == nil {
		t.Error("expected health check to fail when client not initialized")
	}
}

// Helper function to create a test app
func createTestApp(t *testing.T) forge.App {
	t.Helper()

	log := logger.NewNoopLogger()

	cfg := confy.NewTestConfigManager()

	testApp := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(log),
		forge.WithAppConfigManager(cfg),
	)

	return testApp
}
