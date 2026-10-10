package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for events.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config  Config
	options []EventServiceOption
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
func NewExtensionWithConfig(config Config, options ...EventServiceOption) forge.Extension {
	base := forge.NewBaseExtension("events", "2.0.0", "Event-driven architecture with event sourcing")

	return &Extension{
		BaseExtension: base,
		config:        config,
		options:       options,
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
			return fmt.Errorf("failed to bind events config: %w", err)
		} else {
			e.config = cfg
		}
	}

	// Register EventService constructor with Vessel using vessel.WithAliases for backward compatibility
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*EventService, error) {
		return NewEventService(e.config, logger, metrics, e.options...), nil
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register event service: %w", err)
	}

	// Register derived services as separate constructors with dependencies
	if err := vessel.Provide(app.Container(), func(svc *EventService) (core.EventBus, error) {
		return svc.GetEventBus(), nil
	}, vessel.WithAliases(EventBusKey)); err != nil {
		return fmt.Errorf("failed to register event bus: %w", err)
	}

	if err := vessel.Provide(app.Container(), func(svc *EventService) (core.EventStore, error) {
		return svc.GetEventStore(), nil
	}, vessel.WithAliases(EventStoreKey)); err != nil {
		return fmt.Errorf("failed to register event store: %w", err)
	}

	if err := vessel.Provide(app.Container(), func(svc *EventService) (*core.HandlerRegistry, error) {
		return svc.GetHandlerRegistry(), nil
	}, vessel.WithAliases(HandlerRegistryKey)); err != nil {
		return fmt.Errorf("failed to register handler registry: %w", err)
	}

	e.Logger().Info("events extension registered")

	return nil
}

// Start resolves and starts the event service, then marks the extension as started.
func (e *Extension) Start(ctx context.Context) error {
	svc, err := forge.Inject[*EventService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve event service: %w", err)
	}

	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start event service: %w", err)
	}

	e.MarkStarted()

	return nil
}

// Stop stops the event service and marks the extension as stopped.
func (e *Extension) Stop(ctx context.Context) error {
	svc, err := forge.Inject[*EventService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve event service: %w", err)
	}

	if err := svc.Stop(ctx); err != nil {
		return err
	}

	e.MarkStopped()

	return nil
}

// Health checks the health of the extension.
func (e *Extension) Health(ctx context.Context) error {
	if !e.IsStarted() {
		return errors.New("events extension not started")
	}

	svc, err := forge.Inject[*EventService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve event service: %w", err)
	}

	return svc.HealthCheck(ctx)
}
