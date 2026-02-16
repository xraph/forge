package mqtt

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
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
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register mqtt service: %w", err)
	}

	// Register MQTT interface backed by the same *MQTTService singleton
	if err := forge.Provide(app.Container(), func(svc *MQTTService) MQTT {
		return svc.Client()
	}); err != nil {
		return fmt.Errorf("failed to register mqtt interface: %w", err)
	}

	e.Logger().Info("mqtt extension registered",
		forge.F("broker", finalConfig.Broker),
		forge.F("client_id", finalConfig.ClientID),
	)

	return nil
}

// Start resolves and starts the MQTT service, then marks the extension as started.
func (e *Extension) Start(ctx context.Context) error {
	svc, err := forge.Inject[*MQTTService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve mqtt service: %w", err)
	}

	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start mqtt service: %w", err)
	}

	e.MarkStarted()
	return nil
}

// Stop stops the MQTT service and marks the extension as stopped.
func (e *Extension) Stop(ctx context.Context) error {
	svc, err := forge.Inject[*MQTTService](e.App().Container())
	if err == nil {
		if stopErr := svc.Stop(ctx); stopErr != nil {
			e.Logger().Error("failed to stop mqtt service", forge.F("error", stopErr))
		}
	}

	e.MarkStopped()
	return nil
}

// Health checks the extension health.
func (e *Extension) Health(ctx context.Context) error {
	if !e.IsStarted() {
		return fmt.Errorf("mqtt extension not started")
	}

	return nil
}
