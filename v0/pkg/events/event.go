package events

import (
	"github.com/xraph/forge/pkg/events/core"
)

// Event represents a domain event in the system
type Event = core.Event

// NewEvent creates a new event with required fields
var NewEvent = core.NewEvent

// EventEnvelope wraps an event with delivery metadata
type EventEnvelope = core.EventEnvelope

// NewEventEnvelope creates a new event envelope
var NewEventEnvelope = core.NewEventEnvelope

// EventFilter defines a filter for events
type EventFilter func(event *Event) bool

// TypeFilter creates a filter for specific event types
var TypeFilter = core.TypeFilter

// AggregateFilter creates a filter for specific aggregate IDs
var AggregateFilter = core.AggregateFilter

// SourceFilter creates a filter for specific event sources
var SourceFilter = core.SourceFilter

// CombineFilters combines multiple filters with AND logic
var CombineFilters = core.CombineFilters

// EventCollection represents a collection of events
type EventCollection = core.EventCollection

// NewEventCollection creates a new event collection
var NewEventCollection = core.NewEventCollection
