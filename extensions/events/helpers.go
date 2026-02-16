package events

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// Helper functions for convenient event service access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// GetEventService retrieves the EventService from the container.
// Returns error if not found or type assertion fails.
func GetEventService(c forge.Container) (*EventService, error) {
	// Try type-based resolution first
	if svc, err := forge.InjectType[*EventService](c); err == nil && svc != nil {
		return svc, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[*EventService](c)
}

// MustGetEventService retrieves the EventService from the container.
// Panics if not found or type assertion fails.
func MustGetEventService(c forge.Container) *EventService {
	svc, err := GetEventService(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get event service: %v", err))
	}
	return svc
}

// GetEventBus retrieves the EventBus from the container.
// Returns error if not found or type assertion fails.
func GetEventBus(c forge.Container) (core.EventBus, error) {
	return forge.Inject[core.EventBus](c)
}

// MustGetEventBus retrieves the EventBus from the container.
// Panics if not found or type assertion fails.
func MustGetEventBus(c forge.Container) core.EventBus {
	bus, err := GetEventBus(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get event bus: %v", err))
	}
	return bus
}

// GetEventStore retrieves the EventStore from the container.
// Returns error if not found or type assertion fails.
func GetEventStore(c forge.Container) (core.EventStore, error) {
	return forge.Inject[core.EventStore](c)
}

// MustGetEventStore retrieves the EventStore from the container.
// Panics if not found or type assertion fails.
func MustGetEventStore(c forge.Container) core.EventStore {
	store, err := GetEventStore(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get event store: %v", err))
	}
	return store
}

// GetHandlerRegistry retrieves the HandlerRegistry from the container.
// Returns error if not found or type assertion fails.
func GetHandlerRegistry(c forge.Container) (*core.HandlerRegistry, error) {
	return forge.Inject[*core.HandlerRegistry](c)
}

// MustGetHandlerRegistry retrieves the HandlerRegistry from the container.
// Panics if not found or type assertion fails.
func MustGetHandlerRegistry(c forge.Container) *core.HandlerRegistry {
	registry, err := GetHandlerRegistry(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get handler registry: %v", err))
	}
	return registry
}

// GetEventServiceFromApp retrieves the EventService from the app.
// Returns error if not found or type assertion fails.
func GetEventServiceFromApp(app forge.App) (*EventService, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetEventService(app.Container())
}

// MustGetEventServiceFromApp retrieves the EventService from the app.
// Panics if not found or type assertion fails.
func MustGetEventServiceFromApp(app forge.App) *EventService {
	if app == nil {
		panic("app is nil")
	}
	return MustGetEventService(app.Container())
}
