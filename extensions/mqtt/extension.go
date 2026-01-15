package mqtt

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Extension implements forge.Extension for MQTT functionality.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension
	config Config
	// No longer storing client - Vessel manages it
}

// NewExtension creates a new MQTT extension
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("mqtt", "2.0.0", "MQTT client with pub/sub support")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new MQTT extension with a complete config
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the MQTT extension with the app
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config
	finalConfig := DefaultConfig()
	if err := e.LoadConfig("mqtt", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("mqtt: failed to load required config: %w", err)
		}
		e.Logger().Warn("mqtt: using default/programmatic config", forge.F("error", err.Error()))
	}
	e.config = finalConfig

	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("mqtt config validation failed: %w", err)
	}

	// Register MQTTService constructor with Vessel
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*MQTTService, error) {
		return NewMQTTService(finalConfig, logger, metrics)
	}); err != nil {
		return fmt.Errorf("failed to register mqtt service: %w", err)
	}

	// Register backward-compatible string key
	if err := forge.RegisterSingleton(app.Container(), "mqtt", func(c forge.Container) (MQTT, error) {
		return forge.InjectType[*MQTTService](c)
	}); err != nil {
		return fmt.Errorf("failed to register mqtt interface: %w", err)
	}

	e.Logger().Info("mqtt extension registered",
		forge.F("broker", finalConfig.Broker),
		forge.F("client_id", finalConfig.ClientID),
	)

	return nil
}

// Start marks the extension as started.
// The actual client is started by Vessel calling MQTTService.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// The actual client is stopped by Vessel calling MQTTService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through MQTTService.Health().
func (e *Extension) Health(ctx context.Context) error {
	return nil
}
