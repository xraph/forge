package stores

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	eventscore "github.com/xraph/forge/pkg/events/core"
	"github.com/xraph/forge/pkg/logger"
)

// MemoryEventStore implements EventStore interface using in-memory storage
type MemoryEventStore struct {
	events               map[string]*eventscore.Event    // eventID -> Event
	snapshots            map[string]*eventscore.Snapshot // snapshotID -> Snapshot
	eventsByAggregate    map[string][]*eventscore.Event  // aggregateID -> Events
	eventsByType         map[string][]*eventscore.Event  // eventType -> Events
	aggregateVersions    map[string]int                  // aggregateID -> latest version
	snapshotsByAggregate map[string]*eventscore.Snapshot // aggregateID -> latest snapshot

	logger  common.Logger
	metrics common.Metrics
	mu      sync.RWMutex
	stats   *eventscore.EventStoreStats
}

// NewMemoryEventStore creates a new in-memory event store
func NewMemoryEventStore(logger common.Logger, metrics common.Metrics) eventscore.EventStore {
	return &MemoryEventStore{
		events:               make(map[string]*eventscore.Event),
		snapshots:            make(map[string]*eventscore.Snapshot),
		eventsByAggregate:    make(map[string][]*eventscore.Event),
		eventsByType:         make(map[string][]*eventscore.Event),
		aggregateVersions:    make(map[string]int),
		snapshotsByAggregate: make(map[string]*eventscore.Snapshot),
		logger:               logger,
		metrics:              metrics,
		stats: &eventscore.EventStoreStats{
			TotalEvents:     0,
			EventsByType:    make(map[string]int64),
			TotalSnapshots:  0,
			SnapshotsByType: make(map[string]int64),
			Health:          common.HealthStatusHealthy,
			ConnectionInfo:  map[string]interface{}{"type": "memory"},
			Metrics: &eventscore.EventStoreMetrics{
				EventsSaved:      0,
				EventsRead:       0,
				SnapshotsCreated: 0,
				SnapshotsRead:    0,
				Errors:           0,
			},
		},
	}
}

// SaveEvent saves a single event
func (mes *MemoryEventStore) SaveEvent(ctx context.Context, event *eventscore.Event) error {
	if err := event.Validate(); err != nil {
		mes.recordError()
		return fmt.Errorf("invalid event: %w", err)
	}

	mes.mu.Lock()
	defer mes.mu.Unlock()

	start := time.Now()

	// Check for duplicate event ID
	if _, exists := mes.events[event.ID]; exists {
		mes.recordError()
		return fmt.Errorf("event with ID %s already exists", event.ID)
	}

	// Clone event to avoid external modifications
	eventCopy := event.Clone()

	// Store event
	mes.events[event.ID] = eventCopy

	// Index by aggregate
	if mes.eventsByAggregate[event.AggregateID] == nil {
		mes.eventsByAggregate[event.AggregateID] = make([]*eventscore.Event, 0)
	}
	mes.eventsByAggregate[event.AggregateID] = append(mes.eventsByAggregate[event.AggregateID], eventCopy)

	// Index by type
	if mes.eventsByType[event.Type] == nil {
		mes.eventsByType[event.Type] = make([]*eventscore.Event, 0)
	}
	mes.eventsByType[event.Type] = append(mes.eventsByType[event.Type], eventCopy)

	// Update aggregate version
	mes.aggregateVersions[event.AggregateID] = event.Version

	// Update statistics
	mes.stats.TotalEvents++
	mes.stats.EventsByType[event.Type]++
	mes.stats.Metrics.EventsSaved++

	// Update oldest/newest timestamps
	if mes.stats.OldestEvent == nil || event.Timestamp.Before(*mes.stats.OldestEvent) {
		mes.stats.OldestEvent = &event.Timestamp
	}
	if mes.stats.NewestEvent == nil || event.Timestamp.After(*mes.stats.NewestEvent) {
		mes.stats.NewestEvent = &event.Timestamp
	}

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.AverageWriteTime = (mes.stats.Metrics.AverageWriteTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_saved", "store", "memory").Inc()
		mes.metrics.Histogram("forge.events.store.save_duration", "store", "memory").Observe(duration.Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("event saved to memory store",
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
			logger.String("aggregate_id", event.AggregateID),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// SaveEvents saves multiple events atomically
func (mes *MemoryEventStore) SaveEvents(ctx context.Context, events []*eventscore.Event) error {
	if len(events) == 0 {
		return nil
	}

	mes.mu.Lock()
	defer mes.mu.Unlock()

	start := time.Now()

	// Validate all events first
	for _, event := range events {
		if err := event.Validate(); err != nil {
			mes.recordError()
			return fmt.Errorf("invalid event %s: %w", event.ID, err)
		}

		// Check for duplicates
		if _, exists := mes.events[event.ID]; exists {
			mes.recordError()
			return fmt.Errorf("event with ID %s already exists", event.ID)
		}
	}

	// Save all events
	for _, event := range events {
		eventCopy := event.Clone()

		// Store event
		mes.events[event.ID] = eventCopy

		// Index by aggregate
		if mes.eventsByAggregate[event.AggregateID] == nil {
			mes.eventsByAggregate[event.AggregateID] = make([]*eventscore.Event, 0)
		}
		mes.eventsByAggregate[event.AggregateID] = append(mes.eventsByAggregate[event.AggregateID], eventCopy)

		// Index by type
		if mes.eventsByType[event.Type] == nil {
			mes.eventsByType[event.Type] = make([]*eventscore.Event, 0)
		}
		mes.eventsByType[event.Type] = append(mes.eventsByType[event.Type], eventCopy)

		// Update aggregate version
		mes.aggregateVersions[event.AggregateID] = event.Version

		// Update statistics
		mes.stats.TotalEvents++
		mes.stats.EventsByType[event.Type]++

		// Update oldest/newest timestamps
		if mes.stats.OldestEvent == nil || event.Timestamp.Before(*mes.stats.OldestEvent) {
			mes.stats.OldestEvent = &event.Timestamp
		}
		if mes.stats.NewestEvent == nil || event.Timestamp.After(*mes.stats.NewestEvent) {
			mes.stats.NewestEvent = &event.Timestamp
		}
	}

	mes.stats.Metrics.EventsSaved += int64(len(events))

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.AverageWriteTime = (mes.stats.Metrics.AverageWriteTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_saved", "store", "memory").Add(float64(len(events)))
		mes.metrics.Histogram("forge.events.store.batch_save_duration", "store", "memory").Observe(duration.Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("events saved to memory store",
			logger.Int("count", len(events)),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// GetEvent retrieves a single event by ID
func (mes *MemoryEventStore) GetEvent(ctx context.Context, eventID string) (*eventscore.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	start := time.Now()

	event, exists := mes.events[eventID]
	if !exists {
		return nil, fmt.Errorf("event with ID %s not found", eventID)
	}

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.EventsRead++
	mes.stats.Metrics.AverageReadTime = (mes.stats.Metrics.AverageReadTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "memory").Inc()
		mes.metrics.Histogram("forge.events.store.read_duration", "store", "memory").Observe(duration.Seconds())
	}

	// Return a clone to avoid external modifications
	return event.Clone(), nil
}

// GetEvents retrieves events by various criteria
func (mes *MemoryEventStore) GetEvents(ctx context.Context, criteria eventscore.EventCriteria) (*eventscore.EventCollection, error) {
	if err := criteria.Validate(); err != nil {
		mes.recordError()
		return nil, fmt.Errorf("invalid criteria: %w", err)
	}

	mes.mu.RLock()
	defer mes.mu.RUnlock()

	start := time.Now()

	var events []*eventscore.Event

	// Collect events based on criteria
	if len(criteria.EventTypes) > 0 {
		// Filter by event types
		for _, eventType := range criteria.EventTypes {
			if typeEvents, exists := mes.eventsByType[eventType]; exists {
				events = append(events, typeEvents...)
			}
		}
	} else if len(criteria.AggregateIDs) > 0 {
		// Filter by aggregate IDs
		for _, aggregateID := range criteria.AggregateIDs {
			if aggEvents, exists := mes.eventsByAggregate[aggregateID]; exists {
				events = append(events, aggEvents...)
			}
		}
	} else {
		// Get all events
		for _, event := range mes.events {
			events = append(events, event)
		}
	}

	// Apply additional filters
	events = mes.applyFilters(events, criteria)

	// Sort events
	mes.sortEvents(events, criteria.SortBy, criteria.SortOrder)

	// Apply pagination
	total := len(events)
	offset := int(criteria.Offset)
	limit := criteria.Limit

	if offset >= total {
		events = []*eventscore.Event{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		events = events[offset:end]
	}

	// Clone events to avoid external modifications
	clonedEvents := make([]eventscore.Event, len(events))
	for i, event := range events {
		clonedEvents[i] = *event.Clone()
	}

	collection := eventscore.NewEventCollection(clonedEvents, total, criteria.Offset, criteria.Limit)

	// Record metrics
	duration := time.Since(start)
	mes.stats.Metrics.EventsRead += int64(len(events))
	mes.stats.Metrics.AverageReadTime = (mes.stats.Metrics.AverageReadTime + duration) / 2

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "memory").Add(float64(len(events)))
		mes.metrics.Histogram("forge.events.store.query_duration", "store", "memory").Observe(duration.Seconds())
	}

	return collection, nil
}

// GetEventsByAggregate retrieves all events for a specific aggregate
func (mes *MemoryEventStore) GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*eventscore.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	aggEvents, exists := mes.eventsByAggregate[aggregateID]
	if !exists {
		return []*eventscore.Event{}, nil
	}

	var events []*eventscore.Event
	for _, event := range aggEvents {
		if event.Version >= fromVersion {
			events = append(events, event.Clone())
		}
	}

	// Sort by version
	sort.Slice(events, func(i, j int) bool {
		return events[i].Version < events[j].Version
	})

	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "memory").Add(float64(len(events)))
	}

	return events, nil
}

// GetEventsByType retrieves events of a specific type
func (mes *MemoryEventStore) GetEventsByType(ctx context.Context, eventType string, limit int, offset int64) ([]*eventscore.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	typeEvents, exists := mes.eventsByType[eventType]
	if !exists {
		return []*eventscore.Event{}, nil
	}

	// Apply pagination
	total := len(typeEvents)
	start := int(offset)
	if start >= total {
		return []*eventscore.Event{}, nil
	}

	end := start + limit
	if end > total {
		end = total
	}

	events := make([]*eventscore.Event, end-start)
	for i, event := range typeEvents[start:end] {
		events[i] = event.Clone()
	}

	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "memory").Add(float64(len(events)))
	}

	return events, nil
}

// GetEventsSince retrieves events since a specific timestamp
func (mes *MemoryEventStore) GetEventsSince(ctx context.Context, since time.Time, limit int, offset int64) ([]*eventscore.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	var events []*eventscore.Event
	for _, event := range mes.events {
		if event.Timestamp.After(since) || event.Timestamp.Equal(since) {
			events = append(events, event.Clone())
		}
	}

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Apply pagination
	total := len(events)
	start := int(offset)
	if start >= total {
		return []*eventscore.Event{}, nil
	}

	end := start + limit
	if end > total {
		end = total
	}

	events = events[start:end]

	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "memory").Add(float64(len(events)))
	}

	return events, nil
}

// GetEventsInRange retrieves events within a time range
func (mes *MemoryEventStore) GetEventsInRange(ctx context.Context, start, end time.Time, limit int, offset int64) ([]*eventscore.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	var events []*eventscore.Event
	for _, event := range mes.events {
		if (event.Timestamp.After(start) || event.Timestamp.Equal(start)) &&
			(event.Timestamp.Before(end) || event.Timestamp.Equal(end)) {
			events = append(events, event.Clone())
		}
	}

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Apply pagination
	total := len(events)
	startIdx := int(offset)
	if startIdx >= total {
		return []*eventscore.Event{}, nil
	}

	endIdx := startIdx + limit
	if endIdx > total {
		endIdx = total
	}

	events = events[startIdx:endIdx]

	mes.stats.Metrics.EventsRead += int64(len(events))

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "memory").Add(float64(len(events)))
	}

	return events, nil
}

// DeleteEvent soft deletes an event by ID
func (mes *MemoryEventStore) DeleteEvent(ctx context.Context, eventID string) error {
	mes.mu.Lock()
	defer mes.mu.Unlock()

	event, exists := mes.events[eventID]
	if !exists {
		return fmt.Errorf("event with ID %s not found", eventID)
	}

	// Mark as deleted by adding metadata
	event.WithMetadata("_deleted", true)
	event.WithMetadata("_deleted_at", time.Now())

	if mes.logger != nil {
		mes.logger.Debug("event marked as deleted",
			logger.String("event_id", eventID),
		)
	}

	return nil
}

// DeleteEventsByAggregate soft deletes all events for an aggregate
func (mes *MemoryEventStore) DeleteEventsByAggregate(ctx context.Context, aggregateID string) error {
	mes.mu.Lock()
	defer mes.mu.Unlock()

	events, exists := mes.eventsByAggregate[aggregateID]
	if !exists {
		return fmt.Errorf("no events found for aggregate %s", aggregateID)
	}

	deleteTime := time.Now()
	for _, event := range events {
		event.WithMetadata("_deleted", true)
		event.WithMetadata("_deleted_at", deleteTime)
	}

	if mes.logger != nil {
		mes.logger.Debug("events marked as deleted for aggregate",
			logger.String("aggregate_id", aggregateID),
			logger.Int("count", len(events)),
		)
	}

	return nil
}

// GetLastEvent gets the last event for an aggregate
func (mes *MemoryEventStore) GetLastEvent(ctx context.Context, aggregateID string) (*eventscore.Event, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	events, exists := mes.eventsByAggregate[aggregateID]
	if !exists || len(events) == 0 {
		return nil, fmt.Errorf("no events found for aggregate %s", aggregateID)
	}

	// Find event with highest version
	var lastEvent *eventscore.Event
	for _, event := range events {
		if lastEvent == nil || event.Version > lastEvent.Version {
			lastEvent = event
		}
	}

	mes.stats.Metrics.EventsRead++

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.events_read", "store", "memory").Inc()
	}

	return lastEvent.Clone(), nil
}

// GetEventCount gets the total count of events
func (mes *MemoryEventStore) GetEventCount(ctx context.Context) (int64, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	return mes.stats.TotalEvents, nil
}

// GetEventCountByType gets the count of events by type
func (mes *MemoryEventStore) GetEventCountByType(ctx context.Context, eventType string) (int64, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	count, exists := mes.stats.EventsByType[eventType]
	if !exists {
		return 0, nil
	}

	return count, nil
}

// CreateSnapshot creates a snapshot of an aggregate's state
func (mes *MemoryEventStore) CreateSnapshot(ctx context.Context, snapshot *eventscore.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		mes.recordError()
		return fmt.Errorf("invalid snapshot: %w", err)
	}

	mes.mu.Lock()
	defer mes.mu.Unlock()

	start := time.Now()

	// Store snapshot
	mes.snapshots[snapshot.ID] = snapshot
	mes.snapshotsByAggregate[snapshot.AggregateID] = snapshot

	// Update statistics
	mes.stats.TotalSnapshots++
	mes.stats.SnapshotsByType[snapshot.Type]++
	mes.stats.Metrics.SnapshotsCreated++

	// Record metrics
	duration := time.Since(start)

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.snapshots_created", "store", "memory").Inc()
		mes.metrics.Histogram("forge.events.store.snapshot_save_duration", "store", "memory").Observe(duration.Seconds())
	}

	if mes.logger != nil {
		mes.logger.Debug("snapshot created",
			logger.String("snapshot_id", snapshot.ID),
			logger.String("aggregate_id", snapshot.AggregateID),
			logger.String("type", snapshot.Type),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// GetSnapshot retrieves the latest snapshot for an aggregate
func (mes *MemoryEventStore) GetSnapshot(ctx context.Context, aggregateID string) (*eventscore.Snapshot, error) {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	snapshot, exists := mes.snapshotsByAggregate[aggregateID]
	if !exists {
		return nil, fmt.Errorf("no snapshot found for aggregate %s", aggregateID)
	}

	mes.stats.Metrics.SnapshotsRead++

	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.snapshots_read", "store", "memory").Inc()
	}

	return snapshot, nil
}

// DeleteSnapshot deletes a snapshot
func (mes *MemoryEventStore) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	mes.mu.Lock()
	defer mes.mu.Unlock()

	snapshot, exists := mes.snapshots[snapshotID]
	if !exists {
		return fmt.Errorf("snapshot with ID %s not found", snapshotID)
	}

	delete(mes.snapshots, snapshotID)
	delete(mes.snapshotsByAggregate, snapshot.AggregateID)

	mes.stats.TotalSnapshots--
	mes.stats.SnapshotsByType[snapshot.Type]--

	if mes.logger != nil {
		mes.logger.Debug("snapshot deleted",
			logger.String("snapshot_id", snapshotID),
		)
	}

	return nil
}

// Close closes the event store connection
func (mes *MemoryEventStore) Close(ctx context.Context) error {
	mes.mu.Lock()
	defer mes.mu.Unlock()

	// Clear all data
	mes.events = make(map[string]*eventscore.Event)
	mes.snapshots = make(map[string]*eventscore.Snapshot)
	mes.eventsByAggregate = make(map[string][]*eventscore.Event)
	mes.eventsByType = make(map[string][]*eventscore.Event)
	mes.aggregateVersions = make(map[string]int)
	mes.snapshotsByAggregate = make(map[string]*eventscore.Snapshot)

	// Reset statistics
	mes.stats = &eventscore.EventStoreStats{
		TotalEvents:     0,
		EventsByType:    make(map[string]int64),
		TotalSnapshots:  0,
		SnapshotsByType: make(map[string]int64),
		Health:          common.HealthStatusHealthy,
		ConnectionInfo:  map[string]interface{}{"type": "memory"},
		Metrics: &eventscore.EventStoreMetrics{
			EventsSaved:      0,
			EventsRead:       0,
			SnapshotsCreated: 0,
			SnapshotsRead:    0,
			Errors:           0,
		},
	}

	if mes.logger != nil {
		mes.logger.Info("memory event store closed")
	}

	return nil
}

// HealthCheck checks if the event store is healthy
func (mes *MemoryEventStore) HealthCheck(ctx context.Context) error {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	// Memory store is always healthy if accessible
	mes.stats.Health = common.HealthStatusHealthy
	return nil
}

// GetStats returns event store statistics
func (mes *MemoryEventStore) GetStats() *eventscore.EventStoreStats {
	mes.mu.RLock()
	defer mes.mu.RUnlock()

	// Create a copy to avoid external modifications
	statsCopy := *mes.stats
	statsCopy.EventsByType = make(map[string]int64)
	statsCopy.SnapshotsByType = make(map[string]int64)
	statsCopy.ConnectionInfo = make(map[string]interface{})

	for k, v := range mes.stats.EventsByType {
		statsCopy.EventsByType[k] = v
	}
	for k, v := range mes.stats.SnapshotsByType {
		statsCopy.SnapshotsByType[k] = v
	}
	for k, v := range mes.stats.ConnectionInfo {
		statsCopy.ConnectionInfo[k] = v
	}

	// Copy metrics
	if mes.stats.Metrics != nil {
		metricsCopy := *mes.stats.Metrics
		statsCopy.Metrics = &metricsCopy
	}

	return &statsCopy
}

// Helper methods

// applyFilters applies additional filters to events
func (mes *MemoryEventStore) applyFilters(events []*eventscore.Event, criteria eventscore.EventCriteria) []*eventscore.Event {
	var filtered []*eventscore.Event

	for _, event := range events {
		// Skip deleted events
		if deleted, exists := event.GetMetadata("_deleted"); exists && deleted == true {
			continue
		}

		// Filter by time range
		if criteria.StartTime != nil && event.Timestamp.Before(*criteria.StartTime) {
			continue
		}
		if criteria.EndTime != nil && event.Timestamp.After(*criteria.EndTime) {
			continue
		}

		// Filter by sources
		if len(criteria.Sources) > 0 {
			found := false
			for _, source := range criteria.Sources {
				if event.Source == source {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Filter by metadata
		if len(criteria.Metadata) > 0 {
			match := true
			for key, value := range criteria.Metadata {
				if eventValue, exists := event.GetMetadata(key); !exists || eventValue != value {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}

		filtered = append(filtered, event)
	}

	return filtered
}

// sortEvents sorts events based on criteria
func (mes *MemoryEventStore) sortEvents(events []*eventscore.Event, sortBy, sortOrder string) {
	if sortBy == "" {
		sortBy = "timestamp"
	}
	if sortOrder == "" {
		sortOrder = "asc"
	}

	ascending := sortOrder == "asc"

	sort.Slice(events, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "timestamp":
			less = events[i].Timestamp.Before(events[j].Timestamp)
		case "type":
			less = events[i].Type < events[j].Type
		case "aggregate_id":
			less = events[i].AggregateID < events[j].AggregateID
		case "version":
			less = events[i].Version < events[j].Version
		default:
			less = events[i].Timestamp.Before(events[j].Timestamp)
		}

		if ascending {
			return less
		}
		return !less
	})
}

// recordError increments the error count
func (mes *MemoryEventStore) recordError() {
	mes.stats.Metrics.Errors++
	if mes.metrics != nil {
		mes.metrics.Counter("forge.events.store.errors", "store", "memory").Inc()
	}
}
