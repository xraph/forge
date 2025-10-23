package mqtt

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Extension implements forge.Extension for MQTT functionality
type Extension struct {
	*forge.BaseExtension
	config Config
	client MQTT
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

	client := NewMQTTClient(e.config, e.Logger(), e.Metrics())
	e.client = client

	if err := forge.RegisterSingleton(app.Container(), "mqtt", func(c forge.Container) (MQTT, error) {
		return e.client, nil
	}); err != nil {
		return fmt.Errorf("failed to register mqtt client: %w", err)
	}

	e.Logger().Info("mqtt extension registered",
		forge.F("broker", e.config.Broker),
		forge.F("client_id", e.config.ClientID),
	)

	return nil
}

// Start starts the MQTT extension
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting mqtt extension", forge.F("broker", e.config.Broker))

	if err := e.client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to mqtt broker: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("mqtt extension started")
	return nil
}

// Stop stops the MQTT extension
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping mqtt extension")

	if e.client != nil {
		if err := e.client.Disconnect(ctx); err != nil {
			e.Logger().Error("failed to disconnect from mqtt broker", forge.F("error", err))
		}
	}

	e.MarkStopped()
	e.Logger().Info("mqtt extension stopped")
	return nil
}

// Health checks if the MQTT client is healthy
func (e *Extension) Health(ctx context.Context) error {
	if e.client == nil {
		return fmt.Errorf("mqtt client not initialized")
	}

	if !e.client.IsConnected() {
		return fmt.Errorf("mqtt client not connected")
	}

	if err := e.client.Ping(ctx); err != nil {
		return fmt.Errorf("mqtt health check failed: %w", err)
	}

	return nil
}

// Client returns the MQTT client instance
func (e *Extension) Client() MQTT {
	return e.client
}
