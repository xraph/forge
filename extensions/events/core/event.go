package core

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge/errors"
)

// Event represents a domain event in the system.
type Event struct {
	// ID is the unique identifier for the event
	ID string `json:"id"`

	// Type identifies the type of event (e.g., "user.created", "order.completed")
	Type string `json:"type"`

	// AggregateID identifies the aggregate root that produced this event
	AggregateID string `json:"aggregate_id"`

	// Data contains the event payload
	Data any `json:"data"`

	// Metadata contains additional event information
	Metadata map[string]any `json:"metadata"`

	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`

	// Version for event versioning and schema evolution
	Version int `json:"version"`

	// Source indicates where the event originated
	Source string `json:"source,omitempty"`

	// CorrelationID for request correlation
	CorrelationID string `json:"correlation_id,omitempty"`

	// CausationID for event causation tracking
	CausationID string `json:"causation_id,omitempty"`
}

// NewEvent creates a new event with required fields.
func NewEvent(eventType, aggregateID string, data any) *Event {
	return &Event{
		ID:          uuid.New().String(),
		Type:        eventType,
		AggregateID: aggregateID,
		Data:        data,
		Metadata:    make(map[string]any),
		Timestamp:   time.Now().UTC(),
		Version:     1,
	}
}

// WithVersion sets the event version for schema evolution.
func (e *Event) WithVersion(version int) *Event {
	e.Version = version

	return e
}

// WithSource sets the event source.
func (e *Event) WithSource(source string) *Event {
	e.Source = source

	return e
}

// WithCorrelationID sets the correlation ID for request tracking.
func (e *Event) WithCorrelationID(correlationID string) *Event {
	e.CorrelationID = correlationID

	return e
}

// WithCausationID sets the causation ID for event causation tracking.
func (e *Event) WithCausationID(causationID string) *Event {
	e.CausationID = causationID

	return e
}

// WithMetadata adds metadata to the event.
func (e *Event) WithMetadata(key string, value any) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]any)
	}

	e.Metadata[key] = value

	return e
}

// GetMetadata retrieves metadata by key.
func (e *Event) GetMetadata(key string) (any, bool) {
	if e.Metadata == nil {
		return nil, false
	}

	value, exists := e.Metadata[key]

	return value, exists
}

// GetMetadataString retrieves string metadata by key.
func (e *Event) GetMetadataString(key string) string {
	if value, exists := e.GetMetadata(key); exists {
		if str, ok := value.(string); ok {
			return str
		}
	}

	return ""
}

// Validate validates the event structure.
func (e *Event) Validate() error {
	if e.ID == "" {
		return errors.New("event ID is required")
	}

	if e.Type == "" {
		return errors.New("event type is required")
	}

	if e.AggregateID == "" {
		return errors.New("aggregate ID is required")
	}

	if e.Data == nil {
		return errors.New("event data is required")
	}

	if e.Timestamp.IsZero() {
		return errors.New("event timestamp is required")
	}

	if e.Version <= 0 {
		return errors.New("event version must be positive")
	}

	return nil
}

// MarshalJSON customizes JSON marshaling.
func (e *Event) MarshalJSON() ([]byte, error) {
	type EventAlias Event

	return json.Marshal(&struct {
		*EventAlias

		Timestamp string `json:"timestamp"`
	}{
		EventAlias: (*EventAlias)(e),
		Timestamp:  e.Timestamp.Format(time.RFC3339Nano),
	})
}

// UnmarshalJSON customizes JSON unmarshaling.
func (e *Event) UnmarshalJSON(data []byte) error {
	type EventAlias Event

	aux := &struct {
		*EventAlias

		Timestamp string `json:"timestamp"`
	}{
		EventAlias: (*EventAlias)(e),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	timestamp, err := time.Parse(time.RFC3339Nano, aux.Timestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}

	e.Timestamp = timestamp

	return nil
}

// Clone creates a deep copy of the event.
func (e *Event) Clone() *Event {
	clone := &Event{
		ID:            e.ID,
		Type:          e.Type,
		AggregateID:   e.AggregateID,
		Data:          e.Data, // Note: this is a shallow copy for performance
		Timestamp:     e.Timestamp,
		Version:       e.Version,
		Source:        e.Source,
		CorrelationID: e.CorrelationID,
		CausationID:   e.CausationID,
	}

	// Deep copy metadata
	if e.Metadata != nil {
		clone.Metadata = make(map[string]any)
		maps.Copy(clone.Metadata, e.Metadata)
	}

	return clone
}

// String returns a string representation of the event.
func (e *Event) String() string {
	return fmt.Sprintf("Event{ID: %s, Type: %s, AggregateID: %s, Timestamp: %s}",
		e.ID, e.Type, e.AggregateID, e.Timestamp.Format(time.RFC3339))
}

// EventEnvelope wraps an event with delivery metadata.
type EventEnvelope struct {
	Event     *Event            `json:"event"`
	Topic     string            `json:"topic"`
	Partition int               `json:"partition,omitempty"`
	Offset    int64             `json:"offset,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Retry     int               `json:"retry,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// NewEventEnvelope creates a new event envelope.
func NewEventEnvelope(event *Event, topic string) *EventEnvelope {
	return &EventEnvelope{
		Event:    event,
		Topic:    topic,
		Headers:  make(map[string]string),
		Metadata: make(map[string]any),
	}
}

// WithHeader adds a header to the envelope.
func (ee *EventEnvelope) WithHeader(key, value string) *EventEnvelope {
	if ee.Headers == nil {
		ee.Headers = make(map[string]string)
	}

	ee.Headers[key] = value

	return ee
}

// WithPartition sets the partition for the envelope.
func (ee *EventEnvelope) WithPartition(partition int) *EventEnvelope {
	ee.Partition = partition

	return ee
}

// WithOffset sets the offset for the envelope.
func (ee *EventEnvelope) WithOffset(offset int64) *EventEnvelope {
	ee.Offset = offset

	return ee
}

// EventFilter defines a filter for events.
type EventFilter func(event *Event) bool

// TypeFilter creates a filter for specific event types.
func TypeFilter(eventTypes ...string) EventFilter {
	typeMap := make(map[string]bool)
	for _, eventType := range eventTypes {
		typeMap[eventType] = true
	}

	return func(event *Event) bool {
		return typeMap[event.Type]
	}
}

// AggregateFilter creates a filter for specific aggregate IDs.
func AggregateFilter(aggregateIDs ...string) EventFilter {
	idMap := make(map[string]bool)
	for _, id := range aggregateIDs {
		idMap[id] = true
	}

	return func(event *Event) bool {
		return idMap[event.AggregateID]
	}
}

// SourceFilter creates a filter for specific event sources.
func SourceFilter(sources ...string) EventFilter {
	sourceMap := make(map[string]bool)
	for _, source := range sources {
		sourceMap[source] = true
	}

	return func(event *Event) bool {
		return sourceMap[event.Source]
	}
}

// CombineFilters combines multiple filters with AND logic.
func CombineFilters(filters ...EventFilter) EventFilter {
	return func(event *Event) bool {
		for _, filter := range filters {
			if !filter(event) {
				return false
			}
		}

		return true
	}
}

// EventCollection represents a collection of events.
type EventCollection struct {
	Events []Event `json:"events"`
	Total  int     `json:"total"`
	Offset int64   `json:"offset"`
	Limit  int     `json:"limit"`
}

// NewEventCollection creates a new event collection.
func NewEventCollection(events []Event, total int, offset int64, limit int) *EventCollection {
	return &EventCollection{
		Events: events,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	}
}

// Add adds an event to the collection.
func (ec *EventCollection) Add(event Event) {
	ec.Events = append(ec.Events, event)
	ec.Total++
}

// Filter filters events in the collection.
func (ec *EventCollection) Filter(filter EventFilter) *EventCollection {
	var filtered []Event

	for _, event := range ec.Events {
		if filter(&event) {
			filtered = append(filtered, event)
		}
	}

	return &EventCollection{
		Events: filtered,
		Total:  len(filtered),
		Offset: ec.Offset,
		Limit:  ec.Limit,
	}
}

// SortByTimestamp sorts events by timestamp.
func (ec *EventCollection) SortByTimestamp(ascending bool) {
	if ascending {
		for i := range len(ec.Events) - 1 {
			for j := i + 1; j < len(ec.Events); j++ {
				if ec.Events[i].Timestamp.After(ec.Events[j].Timestamp) {
					ec.Events[i], ec.Events[j] = ec.Events[j], ec.Events[i]
				}
			}
		}
	} else {
		for i := range len(ec.Events) - 1 {
			for j := i + 1; j < len(ec.Events); j++ {
				if ec.Events[i].Timestamp.Before(ec.Events[j].Timestamp) {
					ec.Events[i], ec.Events[j] = ec.Events[j], ec.Events[i]
				}
			}
		}
	}
}
