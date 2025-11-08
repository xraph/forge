package stores

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// MemoryEventStore implements EventStore interface using in-memory storage.
type MemoryEventStore struct {
	events               map[string]*core.Event    // eventID -> Event
	snapshots            map[string]*core.Snapshot // snapshotID -> Snapshot
	eventsByAggregate    map[string][]*core.Event  // aggregateID -> Events
	eventsByType         map[string][]*core.Event  // eventType -> Events
	aggregateVersions    map[string]int            // aggregateID -> latest version
	snapshotsByAggregate map[string]*core.Snapshot // aggregateID -> latest snapshot
	logger               forge.Logger
	metrics              forge.Metrics
	mu                   sync.RWMutex
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore(logger forge.Logger, metrics forge.Metrics) core.EventStore {
	return &MemoryEventStore{
		events:               make(map[string]*core.Event),
		snapshots:            make(map[string]*core.Snapshot),
		eventsByAggregate:    make(map[string][]*core.Event),
		eventsByType:         make(map[string][]*core.Event),
		aggregateVersions:    make(map[string]int),
		snapshotsByAggregate: make(map[string]*core.Snapshot),
		logger:               logger,
		metrics:              metrics,
	}
}

// SaveEvent saves a single event.
func (mes *MemoryEventStore) SaveEvent(ctx context.Context, event *core.Event) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	mes.mu.Lock()
	defer mes.mu.Unlock()

	start := time.Now()

	// Check for duplicate event ID
	if _, exists := mes.events[event.ID]; exists {
		return fmt.Errorf("event with ID %s already exists", event.ID)
	}

	// Clone event to avoid external modifications
	eventCopy := event.Clone()

	// Store event
	mes.events[event.ID] = eventCopy

	// Index by aggregate
	if mes.eventsByAggregate[event.AggregateID] == nil {
		mes.eventsByAggregate[event.AggregateID] = make([]*core.Event, 0)
	}

	mes.eventsByAggregate[event.AggregateID] = append(mes.eventsByAggregate[event.AggregateID], eventCopy)

	// Index by type
	if mes.eventsByType[event.Type] == nil {
		mes.eventsByType[event.Type] = make([]*core.Event, 0)
	}

	mes.eventsByType[event.Type] = append(mes.eventsByType[event.Type], eventCopy)

	// Update aggregate version
	mes.aggregateVersions[event.AggregateID] = event.Version

	// Record metrics
	if mes.metrics != nil {
		duration := time.Since(start)

		mes.metrics.Counter("forge.events.store.events_saved", "store", "memory").Inc()
		mes.metrics.Histogram("forge.events.store.save_duration", "store", "memory").Observe(duration.Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("event saved to memory store", forge.F("event_id", event.ID), forge.F("event_type", event.Type), forge.F("aggregate_id", event.AggregateID))
	}

	return nil
}

// SaveEvents saves multiple events atomically.
func (mes *MemoryEventStore) SaveEvents(ctx context.Context, events []*core.Event) error {
	if len(events) == 0 {
		return nil
	}

	mes.mu.Lock()
	defer mes.mu.Unlock()

	start := time.Now()

	// Validate all events first
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("invalid event %s: %w", event.ID, err)
		}
		// Check for duplicates
		if _, exists := mes.events[event.ID]; exists {
			return fmt.Errorf("event with ID %s already exists", event.ID)
		}
	}

	// Save all events
	for _, event := range events {
		eventCopy := event.Clone()
		mes.events[event.ID] = eventCopy

		if mes.eventsByAggregate[event.AggregateID] == nil {
			mes.eventsByAggregate[event.AggregateID] = make([]*core.Event, 0)
		}

		mes.eventsByAggregate[event.AggregateID] = append(mes.eventsByAggregate[event.AggregateID], eventCopy)

		if mes.eventsByType[event.Type] == nil {
			mes.eventsByType[event.Type] = make([]*core.Event, 0)
		}

		mes.eventsByType[event.Type] = append(mes.eventsByType[event.Type], eventCopy)

		mes.aggregateVersions[event.AggregateID] = event.Version
	}

	// Record metrics
	if mes.metrics != nil {
		duration := time.Since(start)

		mes.metrics.Counter("forge.events.store.events_saved", "store", "memory").Add(float64(len(events)))
		mes.metrics.Histogram("forge.events.store.batch_save_duration", "store", "memory").Observe(duration.Seconds())
	}

	return nil
}

// GetEvent retrieves a single event by ID.
func (mes *MemoryEventStore) GetEvent(ctx context.Context, eventID string) (*core.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	event, exists := mes.events[eventID]
	if !exists {
		return nil, fmt.Errorf("event %s not found", eventID)
	}

	return event.Clone(), nil
}

// GetEvents retrieves events by various criteria.
func (mes *MemoryEventStore) GetEvents(ctx context.Context, criteria core.EventCriteria) (*core.EventCollection, error) {
	if err := criteria.Validate(); err != nil {
		return nil, fmt.Errorf("invalid criteria: %w", err)
	}

	mes.mu.RLock()
	defer mes.mu.RUnlock()

	// Get all events
	var events []*core.Event
	for _, event := range mes.events {
		// Apply filters
		if len(criteria.EventTypes) > 0 {
			found := slices.Contains(criteria.EventTypes, event.Type)

			if !found {
				continue
			}
		}

		if len(criteria.AggregateIDs) > 0 {
			found := slices.Contains(criteria.AggregateIDs, event.AggregateID)

			if !found {
				continue
			}
		}

		if criteria.StartTime != nil && event.Timestamp.Before(*criteria.StartTime) {
			continue
		}

		if criteria.EndTime != nil && event.Timestamp.After(*criteria.EndTime) {
			continue
		}

		events = append(events, event)
	}

	// Sort
	sort.Slice(events, func(i, j int) bool {
		if criteria.SortOrder == "desc" {
			return events[i].Timestamp.After(events[j].Timestamp)
		}

		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Apply pagination
	total := len(events)
	if int(criteria.Offset) >= len(events) {
		events = []*core.Event{}
	} else {
		start := criteria.Offset

		end := min(start+int64(criteria.Limit), int64(len(events)))

		events = events[start:end]
	}

	// Convert to Event (not *Event)
	eventList := make([]core.Event, len(events))
	for i, e := range events {
		eventList[i] = *e
	}

	return core.NewEventCollection(eventList, total, criteria.Offset, criteria.Limit), nil
}

// GetEventsByAggregate retrieves all events for a specific aggregate.
func (mes *MemoryEventStore) GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*core.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	events, exists := mes.eventsByAggregate[aggregateID]
	if !exists {
		return []*core.Event{}, nil
	}

	// Filter by version
	var filtered []*core.Event

	for _, event := range events {
		if event.Version >= fromVersion {
			filtered = append(filtered, event.Clone())
		}
	}

	return filtered, nil
}

// GetEventsByType retrieves events of a specific type.
func (mes *MemoryEventStore) GetEventsByType(ctx context.Context, eventType string, limit int, offset int64) ([]*core.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	events, exists := mes.eventsByType[eventType]
	if !exists {
		return []*core.Event{}, nil
	}

	// Apply pagination
	if int(offset) >= len(events) {
		return []*core.Event{}, nil
	}

	start := offset

	end := min(start+int64(limit), int64(len(events)))

	result := make([]*core.Event, end-start)
	for i, event := range events[start:end] {
		result[i] = event.Clone()
	}

	return result, nil
}

// GetEventsSince retrieves events since a specific timestamp.
func (mes *MemoryEventStore) GetEventsSince(ctx context.Context, since time.Time, limit int, offset int64) ([]*core.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	var events []*core.Event
	for _, event := range mes.events {
		if event.Timestamp.After(since) {
			events = append(events, event)
		}
	}

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Apply pagination
	if int(offset) >= len(events) {
		return []*core.Event{}, nil
	}

	start := offset

	end := min(start+int64(limit), int64(len(events)))

	result := make([]*core.Event, end-start)
	for i, event := range events[start:end] {
		result[i] = event.Clone()
	}

	return result, nil
}

// GetEventsInRange retrieves events within a time range.
func (mes *MemoryEventStore) GetEventsInRange(ctx context.Context, start, end time.Time, limit int, offset int64) ([]*core.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	var events []*core.Event
	for _, event := range mes.events {
		if event.Timestamp.After(start) && event.Timestamp.Before(end) {
			events = append(events, event)
		}
	}

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Apply pagination
	if int(offset) >= len(events) {
		return []*core.Event{}, nil
	}

	startIdx := offset

	endIdx := min(startIdx+int64(limit), int64(len(events)))

	result := make([]*core.Event, endIdx-startIdx)
	for i, event := range events[startIdx:endIdx] {
		result[i] = event.Clone()
	}

	return result, nil
}

// GetLastEvent gets the last event for an aggregate.
func (mes *MemoryEventStore) GetLastEvent(ctx context.Context, aggregateID string) (*core.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	events, exists := mes.eventsByAggregate[aggregateID]
	if !exists || len(events) == 0 {
		return nil, fmt.Errorf("no events found for aggregate %s", aggregateID)
	}

	return events[len(events)-1].Clone(), nil
}

// GetEventCount gets the total count of events.
func (mes *MemoryEventStore) GetEventCount(ctx context.Context) (int64, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	return int64(len(mes.events)), nil
}

// GetEventCountByType gets the count of events by type.
func (mes *MemoryEventStore) GetEventCountByType(ctx context.Context, eventType string) (int64, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	events, exists := mes.eventsByType[eventType]
	if !exists {
		return 0, nil
	}

	return int64(len(events)), nil
}

// CreateSnapshot creates a snapshot of an aggregate's state.
func (mes *MemoryEventStore) CreateSnapshot(ctx context.Context, snapshot *core.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("invalid snapshot: %w", err)
	}

	mes.mu.Lock()
	defer mes.mu.Unlock()

	mes.snapshots[snapshot.ID] = snapshot
	mes.snapshotsByAggregate[snapshot.AggregateID] = snapshot

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.snapshots_created", "store", "memory").Inc()
	}

	return nil
}

// GetSnapshot retrieves the latest snapshot for an aggregate.
func (mes *MemoryEventStore) GetSnapshot(ctx context.Context, aggregateID string) (*core.Snapshot, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	snapshot, exists := mes.snapshotsByAggregate[aggregateID]
	if !exists {
		return nil, fmt.Errorf("no snapshot found for aggregate %s", aggregateID)
	}

	return snapshot, nil
}

// Close closes the event store connection.
func (mes *MemoryEventStore) Close(ctx context.Context) error {
	return nil
}

// HealthCheck checks if the event store is healthy.
func (mes *MemoryEventStore) HealthCheck(ctx context.Context) error {
	return nil
}

func (mes *MemoryEventStore) DeleteEvent(ctx context.Context, eventID string) error {
	mes.mu.Lock()
	defer mes.mu.Unlock()

	event, exists := mes.events[eventID]
	if !exists {
		return fmt.Errorf("event %s not found", eventID)
	}

	// Remove from events map
	delete(mes.events, eventID)

	// Remove from eventsByAggregate
	eventsAgg := mes.eventsByAggregate[event.AggregateID]
	for i, e := range eventsAgg {
		if e.ID == eventID {
			mes.eventsByAggregate[event.AggregateID] = append(eventsAgg[:i], eventsAgg[i+1:]...)

			break
		}
	}

	// Remove from eventsByType
	eventsType := mes.eventsByType[event.Type]
	for i, e := range eventsType {
		if e.ID == eventID {
			mes.eventsByType[event.Type] = append(eventsType[:i], eventsType[i+1:]...)

			break
		}
	}

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_deleted", "store", "memory").Inc()
	}

	if mes.logger != nil {
		mes.logger.Debug("event deleted from memory store", forge.F("event_id", eventID))
	}

	return nil
}

func (mes *MemoryEventStore) DeleteEventsByAggregate(ctx context.Context, aggregateID string) error {
	mes.mu.Lock()
	defer mes.mu.Unlock()

	events, exists := mes.eventsByAggregate[aggregateID]
	if !exists || len(events) == 0 {
		return nil
	}

	// Remove each event
	for _, event := range events {
		delete(mes.events, event.ID)

		// Remove from eventsByType
		eventsType := mes.eventsByType[event.Type]
		for i, e := range eventsType {
			if e.ID == event.ID {
				mes.eventsByType[event.Type] = append(eventsType[:i], eventsType[i+1:]...)

				break
			}
		}
	}

	// Remove aggregate entries
	delete(mes.eventsByAggregate, aggregateID)
	delete(mes.aggregateVersions, aggregateID)

	count := len(events)
	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_deleted", "store", "memory").Add(float64(count))
	}

	if mes.logger != nil {
		mes.logger.Debug("events deleted by aggregate from memory store", forge.F("aggregate_id", aggregateID), forge.F("count", count))
	}

	return nil
}

func (mes *MemoryEventStore) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	mes.mu.Lock()
	defer mes.mu.Unlock()

	snapshot, exists := mes.snapshots[snapshotID]
	if !exists {
		return fmt.Errorf("snapshot %s not found", snapshotID)
	}

	// Remove from snapshots map
	delete(mes.snapshots, snapshotID)

	// Remove from snapshotsByAggregate if this is the latest
	if mes.snapshotsByAggregate[snapshot.AggregateID] != nil && mes.snapshotsByAggregate[snapshot.AggregateID].ID == snapshotID {
		delete(mes.snapshotsByAggregate, snapshot.AggregateID)
	}

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.snapshots_deleted", "store", "memory").Inc()
	}

	if mes.logger != nil {
		mes.logger.Debug("snapshot deleted from memory store", forge.F("snapshot_id", snapshotID))
	}

	return nil
}
