package events

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// Extension implements forge.Extension for events.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing service - Vessel manages it
}

// NewExtension creates a new events extension.
func NewExtension() forge.Extension {
	base := forge.NewBaseExtension("events", "2.0.0", "Event-driven architecture with event sourcing")

	return &Extension{
		BaseExtension: base,
		config:        DefaultConfig(),
	}
}

// NewExtensionWithConfig creates a new events extension with custom config.
func NewExtensionWithConfig(config Config) forge.Extension {
	base := forge.NewBaseExtension("events", "2.0.0", "Event-driven architecture with event sourcing")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// Register registers the extension with the app.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config
	cfg := e.config
	cm := e.App().Config()
	
	if cm != nil && cm.IsSet("extensions.events") {
		if err := cm.Bind("extensions.events", &cfg); err != nil {
			e.Logger().Warn("failed to bind events config", forge.F("error", err))
		} else {
			e.config = cfg
		}
	}

	// Register EventService constructor with Vessel
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*EventService, error) {
		return NewEventService(e.config, logger, metrics), nil
	}); err != nil {
		return fmt.Errorf("failed to register event service: %w", err)
	}

	// Register backward-compatible string keys
	if err := forge.RegisterSingleton(app.Container(), "events", func(c forge.Container) (*EventService, error) {
		return forge.InjectType[*EventService](c)
	}); err != nil {
		return fmt.Errorf("failed to register events key: %w", err)
	}

	if err := forge.RegisterSingleton(app.Container(), "eventBus", func(c forge.Container) (core.EventBus, error) {
		svc, err := forge.InjectType[*EventService](c)
		if err != nil {
			return nil, err
		}
		return svc.GetEventBus(), nil
	}); err != nil {
		return fmt.Errorf("failed to register event bus key: %w", err)
	}

	if err := forge.RegisterSingleton(app.Container(), "eventStore", func(c forge.Container) (core.EventStore, error) {
		svc, err := forge.InjectType[*EventService](c)
		if err != nil {
			return nil, err
		}
		return svc.GetEventStore(), nil
	}); err != nil {
		return fmt.Errorf("failed to register event store key: %w", err)
	}

	if err := forge.RegisterSingleton(app.Container(), "eventHandlerRegistry", func(c forge.Container) (*core.HandlerRegistry, error) {
		svc, err := forge.InjectType[*EventService](c)
		if err != nil {
			return nil, err
		}
		return svc.GetHandlerRegistry(), nil
	}); err != nil {
		return fmt.Errorf("failed to register handler registry key: %w", err)
	}

	e.Logger().Info("events extension registered")

	return nil
}

// Start marks the extension as started.
// EventService lifecycle is managed by Vessel.
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// EventService lifecycle is managed by Vessel.
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the health of the extension.
// Service health is managed by Vessel.
func (e *Extension) Health(ctx context.Context) error {
	return nil
}
