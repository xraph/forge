package kafka

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for Kafka functionality.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension
	config Config
	// No longer storing client - Vessel manages it
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

	// Register KafkaService constructor with Vessel using vessel.WithAliases for backward compatibility
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*KafkaService, error) {
		return NewKafkaService(finalConfig, logger, metrics)
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register kafka service: %w", err)
	}

	e.Logger().Info("kafka extension registered",
		forge.F("brokers", finalConfig.Brokers),
		forge.F("client_id", finalConfig.ClientID),
	)

	return nil
}

// Start marks the extension as started.
// The actual client is started by Vessel calling KafkaService.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// The actual client is stopped by Vessel calling KafkaService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through KafkaService.Health().
func (e *Extension) Health(ctx context.Context) error {
	return nil
}
