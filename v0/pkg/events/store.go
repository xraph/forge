package events

import (
	"github.com/xraph/forge/pkg/events/core"
)

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
