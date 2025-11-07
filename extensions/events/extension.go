package events

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// Extension implements forge.Extension for events
type Extension struct {
	config  Config
	service *EventService
	logger  forge.Logger
	metrics forge.Metrics
}

// NewExtension creates a new events extension
func NewExtension() forge.Extension {
	return &Extension{
		config: DefaultConfig(),
	}
}

// NewExtensionWithConfig creates a new events extension with custom config
func NewExtensionWithConfig(config Config) forge.Extension {
	return &Extension{
		config: config,
	}
}

// Name returns the extension name
func (e *Extension) Name() string {
	return "events"
}

// Version returns the extension version
func (e *Extension) Version() string {
	return "2.0.0"
}

// Description returns the extension description
func (e *Extension) Description() string {
	return "Event-driven architecture with event sourcing"
}

// Dependencies returns the extension dependencies
func (e *Extension) Dependencies() []string {
	return []string{} // Optional dependencies
}

// Register registers the extension with the app
func (e *Extension) Register(app forge.App) error {
	// Get dependencies
	var err error
	e.logger, err = forge.GetLogger(app.Container())
	if err != nil {
		return fmt.Errorf("failed to resolve logger: %w", err)
	}
	e.metrics, err = forge.GetMetrics(app.Container())
	if err != nil {
		return fmt.Errorf("failed to resolve metrics: %w", err)
	}

	// Get config from ConfigManager (bind pattern)
	configMgr, err := forge.GetConfigManager(app.Container())
	if err != nil {
		return fmt.Errorf("failed to resolve config manager: %w", err)
	}
	if err := configMgr.Bind("extensions.events", &e.config); err != nil {
		// Use defaults if not found
		e.config = DefaultConfig()
		e.logger.Warn("using default events config", forge.F("reason", "config not found"))
	}

	// Create event service
	e.service = NewEventService(e.config, e.logger, e.metrics)

	// Register services with DI
	forge.RegisterSingleton(app.Container(), "events", func(c forge.Container) (*EventService, error) {
		return e.service, nil
	})

	forge.RegisterSingleton(app.Container(), "eventBus", func(c forge.Container) (core.EventBus, error) {
		return e.service.GetEventBus(), nil
	})

	forge.RegisterSingleton(app.Container(), "eventStore", func(c forge.Container) (core.EventStore, error) {
		return e.service.GetEventStore(), nil
	})

	forge.RegisterSingleton(app.Container(), "eventHandlerRegistry", func(c forge.Container) (*core.HandlerRegistry, error) {
		return e.service.GetHandlerRegistry(), nil
	})

	e.logger.Info("events extension registered")

	return nil
}

// Start starts the extension
func (e *Extension) Start(ctx context.Context) error {
	e.logger.Info("starting events extension")
	return e.service.Start(ctx)
}

// Stop stops the extension
func (e *Extension) Stop(ctx context.Context) error {
	e.logger.Info("stopping events extension")
	return e.service.Stop(ctx)
}

// Health checks the health of the extension
func (e *Extension) Health(ctx context.Context) error {
	return e.service.HealthCheck(ctx)
}
