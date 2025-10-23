package core

import (
	"context"
)

// EventBus defines the interface for the event bus
type EventBus interface {
	// Publish publishes an event to all registered brokers
	Publish(ctx context.Context, event *Event) error

	// PublishTo publishes an event to a specific broker
	PublishTo(ctx context.Context, brokerName string, event *Event) error

	// Subscribe subscribes to events of a specific type
	Subscribe(eventType string, handler EventHandler) error

	// Unsubscribe unsubscribes from events of a specific type
	Unsubscribe(eventType string, handlerName string) error

	// RegisterBroker registers a message broker
	RegisterBroker(name string, broker MessageBroker) error

	// UnregisterBroker unregisters a message broker
	UnregisterBroker(name string) error

	// GetBroker returns a broker by name
	GetBroker(name string) (MessageBroker, error)

	// GetBrokers returns all registered brokers
	GetBrokers() map[string]MessageBroker

	// SetDefaultBroker sets the default broker for publishing
	SetDefaultBroker(name string) error

	// GetStats returns event bus statistics
	GetStats() map[string]interface{}

	// Start starts the event bus
	Start(ctx context.Context) error

	// Stop stops the event bus
	Stop(ctx context.Context) error

	// HealthCheck checks the health of the event bus
	HealthCheck(ctx context.Context) error
}

// MessageBroker defines the interface for message brokers
type MessageBroker interface {
	// Connect connects to the message broker
	Connect(ctx context.Context, config interface{}) error

	// Publish publishes an event to a topic
	Publish(ctx context.Context, topic string, event Event) error

	// Subscribe subscribes to a topic
	Subscribe(ctx context.Context, topic string, handler EventHandler) error

	// Unsubscribe unsubscribes from a topic
	Unsubscribe(ctx context.Context, topic string, handlerName string) error

	// Close closes the connection to the message broker
	Close(ctx context.Context) error

	// HealthCheck checks if the broker is healthy
	HealthCheck(ctx context.Context) error

	// GetStats returns broker statistics
	GetStats() map[string]interface{}
}
