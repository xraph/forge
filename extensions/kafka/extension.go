package kafka

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Extension implements forge.Extension for Kafka functionality
type Extension struct {
	*forge.BaseExtension
	config Config
	client Kafka
}

// NewExtension creates a new Kafka extension
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("kafka", "2.0.0", "Kafka client with producer and consumer support")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new Kafka extension with a complete config
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the Kafka extension with the app
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config
	finalConfig := DefaultConfig()
	if err := e.LoadConfig("kafka", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("kafka: failed to load required config: %w", err)
		}
		e.Logger().Warn("kafka: using default/programmatic config", forge.F("error", err.Error()))
	}
	e.config = finalConfig

	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("kafka config validation failed: %w", err)
	}

	client, err := NewKafkaClient(e.config, e.Logger(), e.Metrics())
	if err != nil {
		return fmt.Errorf("failed to create kafka client: %w", err)
	}
	e.client = client

	if err := forge.RegisterSingleton(app.Container(), "kafka", func(c forge.Container) (Kafka, error) {
		return e.client, nil
	}); err != nil {
		return fmt.Errorf("failed to register kafka client: %w", err)
	}

	e.Logger().Info("kafka extension registered",
		forge.F("brokers", e.config.Brokers),
		forge.F("client_id", e.config.ClientID),
	)

	return nil
}

// Start starts the Kafka extension
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting kafka extension", forge.F("brokers", e.config.Brokers))

	// Kafka client is created during Register, just verify it's healthy
	if err := e.client.Ping(ctx); err != nil {
		return fmt.Errorf("kafka client not healthy: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("kafka extension started")
	return nil
}

// Stop stops the Kafka extension
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping kafka extension")

	if e.client != nil {
		if err := e.client.Close(); err != nil {
			e.Logger().Error("failed to close kafka client", forge.F("error", err))
		}
	}

	e.MarkStopped()
	e.Logger().Info("kafka extension stopped")
	return nil
}

// Health checks if the Kafka client is healthy
func (e *Extension) Health(ctx context.Context) error {
	if e.client == nil {
		return fmt.Errorf("kafka client not initialized")
	}

	if err := e.client.Ping(ctx); err != nil {
		return fmt.Errorf("kafka health check failed: %w", err)
	}

	return nil
}

// Client returns the Kafka client instance
func (e *Extension) Client() Kafka {
	return e.client
}
