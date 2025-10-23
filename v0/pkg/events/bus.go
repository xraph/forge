package events

import (
	"context"

	"github.com/xraph/forge/pkg/common"
	eventscore "github.com/xraph/forge/pkg/events/core"
)

// EventBus defines the interface for the event bus
type EventBus = eventscore.EventBus

// MessageBroker defines the interface for message brokers
type MessageBroker = eventscore.MessageBroker

// EventBusOptions defines configuration for EventBusImpl
type EventBusOptions = eventscore.EventBusOptions

// NewEventBus creates a new event bus
func NewEventBus(config EventBusOptions) (EventBus, error) {
	return eventscore.NewEventBus(config)
}

// EventWorker processes events from the queue
type EventWorker = eventscore.EventWorker

// WorkerStats contains worker statistics
type WorkerStats = eventscore.WorkerStats

// NewEventWorker creates a new event worker
func NewEventWorker(id int, eventQueue <-chan *EventEnvelope, processor func(context.Context, *EventEnvelope) error, logger common.Logger, metrics common.Metrics) *EventWorker {
	return eventscore.NewEventWorker(id, eventQueue, processor, logger, metrics)
}
