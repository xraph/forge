package events

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// EventHandler defines the interface for handling events
type EventHandler = core.EventHandler

// EventHandlerFunc is a function adapter for EventHandler
type EventHandlerFunc = core.EventHandlerFunc

// TypedEventHandler is a handler for specific event types
type TypedEventHandler = core.TypedEventHandler

// NewTypedEventHandler creates a new typed event handler
func NewTypedEventHandler(name string, eventTypes []string, handler EventHandlerFunc) *TypedEventHandler {
	return core.NewTypedEventHandler(name, eventTypes, handler)
}

// HandlerMiddleware defines middleware for event handlers
type HandlerMiddleware = core.HandlerMiddleware

// LoggingMiddleware creates logging middleware for handlers
func LoggingMiddleware(l forge.Logger) HandlerMiddleware {
	return core.LoggingMiddleware(l)
}

// MetricsMiddleware creates metrics middleware for handlers
func MetricsMiddleware(metrics forge.Metrics) HandlerMiddleware {
	return core.MetricsMiddleware(metrics)
}

// ValidationMiddleware creates validation middleware for handlers
func ValidationMiddleware() HandlerMiddleware {
	return core.ValidationMiddleware()
}

// RetryPolicy defines retry behavior for event handlers
type RetryPolicy = core.RetryPolicy

// NewRetryPolicy creates a new retry policy
func NewRetryPolicy(maxRetries int, initialDelay time.Duration) *RetryPolicy {
	return core.NewRetryPolicy(maxRetries, initialDelay)
}

// HandlerRegistry manages event handlers
type HandlerRegistry = core.HandlerRegistry

// NewHandlerRegistry creates a new handler registry
func NewHandlerRegistry(logger forge.Logger, metrics forge.Metrics) *HandlerRegistry {
	return core.NewHandlerRegistry(logger, metrics)
}

// DomainEventHandler is a specialized handler for domain events
type DomainEventHandler = core.DomainEventHandler

// NewDomainEventHandler creates a new domain event handler
func NewDomainEventHandler(name, aggregateType string, eventTypes []string, handler EventHandlerFunc) *DomainEventHandler {
	return core.NewDomainEventHandler(name, aggregateType, eventTypes, handler)
}

// ReflectionEventHandler uses reflection to call methods based on event type
type ReflectionEventHandler = core.ReflectionEventHandler

// NewReflectionEventHandler creates a new reflection-based event handler
func NewReflectionEventHandler(name string, target interface{}) *ReflectionEventHandler {
	return core.NewReflectionEventHandler(name, target)
}

// EventStore defines the interface for persisting and retrieving events
type EventStore = core.EventStore

// EventCriteria defines criteria for querying events
type EventCriteria = core.EventCriteria

// NewEventCriteria creates a new event criteria
func NewEventCriteria() *EventCriteria {
	return core.NewEventCriteria()
}

// Snapshot represents a point-in-time snapshot of aggregate state
type Snapshot = core.Snapshot

// NewSnapshot creates a new snapshot
func NewSnapshot(aggregateID, snapshotType string, data interface{}, version int) *Snapshot {
	return core.NewSnapshot(aggregateID, snapshotType, data, version)
}

// EventStoreConfig defines configuration for event stores
type EventStoreConfig = core.EventStoreConfig

// DefaultEventStoreConfig returns default configuration
func DefaultEventStoreConfig() *EventStoreConfig {
	return core.DefaultEventStoreConfig()
}

// EventStream represents a stream of events
type EventStream = core.EventStream

// EventStreamHandler handles events from an event stream
type EventStreamHandler = core.EventStreamHandler

// EventStreamConfig defines configuration for event streams
type EventStreamConfig = core.EventStreamConfig

// DefaultEventStreamConfig returns default stream configuration
func DefaultEventStreamConfig() *EventStreamConfig {
	return core.DefaultEventStreamConfig()
}

// EventStoreMetrics defines metrics for event stores
type EventStoreMetrics = core.EventStoreMetrics

// EventStoreStats represents statistics for an event store
type EventStoreStats = core.EventStoreStats

// EventProjection represents a projection of events
type EventProjection = core.EventProjection

// ProjectionManager manages event projections
type ProjectionManager = core.ProjectionManager

// EventStoreTransaction represents a transaction in the event store
type EventStoreTransaction = core.EventStoreTransaction

// TransactionalEventStore extends EventStore with transaction support
type TransactionalEventStore = core.TransactionalEventStore

// EventStoreMigration represents a migration for the event store
type EventStoreMigration = core.EventStoreMigration

// EventStoreMigrator manages event store migrations
type EventStoreMigrator = core.EventStoreMigrator
