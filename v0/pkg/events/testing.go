package events

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/events/schema"
	"github.com/xraph/forge/pkg/logger"
)

// TestEventBuilder helps build test events with a fluent API
type TestEventBuilder struct {
	event *Event
}

// NewTestEvent creates a new test event builder
func NewTestEvent(eventType, aggregateID string) *TestEventBuilder {
	return &TestEventBuilder{
		event: NewEvent(eventType, aggregateID, map[string]interface{}{}),
	}
}

// WithID sets the event ID
func (teb *TestEventBuilder) WithID(id string) *TestEventBuilder {
	teb.event.ID = id
	return teb
}

// WithData sets the event data
func (teb *TestEventBuilder) WithData(data interface{}) *TestEventBuilder {
	teb.event.Data = data
	return teb
}

// WithVersion sets the event version
func (teb *TestEventBuilder) WithVersion(version int) *TestEventBuilder {
	teb.event.Version = version
	return teb
}

// WithTimestamp sets the event timestamp
func (teb *TestEventBuilder) WithTimestamp(timestamp time.Time) *TestEventBuilder {
	teb.event.Timestamp = timestamp
	return teb
}

// WithSource sets the event source
func (teb *TestEventBuilder) WithSource(source string) *TestEventBuilder {
	teb.event.Source = source
	return teb
}

// WithCorrelationID sets the correlation ID
func (teb *TestEventBuilder) WithCorrelationID(correlationID string) *TestEventBuilder {
	teb.event.CorrelationID = correlationID
	return teb
}

// WithCausationID sets the causation ID
func (teb *TestEventBuilder) WithCausationID(causationID string) *TestEventBuilder {
	teb.event.CausationID = causationID
	return teb
}

// WithMetadata adds metadata to the event
func (teb *TestEventBuilder) WithMetadata(key string, value interface{}) *TestEventBuilder {
	if teb.event.Metadata == nil {
		teb.event.Metadata = make(map[string]interface{})
	}
	teb.event.Metadata[key] = value
	return teb
}

// Build returns the built event
func (teb *TestEventBuilder) Build() *Event {
	return teb.event
}

// TestEventStore implements EventStore for testing purposes
type TestEventStore struct {
	events      []*Event
	snapshots   []*Snapshot
	eventsByID  map[string]*Event
	eventsByAgg map[string][]*Event
	mu          sync.RWMutex
	failOnSave  bool
	failOnGet   bool
	saveDelay   time.Duration
	getDelay    time.Duration
	metrics     *TestStoreMetrics
}

// TestStoreMetrics tracks test store operations
type TestStoreMetrics struct {
	SaveEventCalls      int
	SaveEventsCalls     int
	GetEventCalls       int
	GetEventsCalls      int
	GetEventsByAggCalls int
	CreateSnapshotCalls int
	GetSnapshotCalls    int
	HealthCheckCalls    int
	mu                  sync.RWMutex
}

// NewTestEventStore creates a new test event store
func NewTestEventStore() *TestEventStore {
	return &TestEventStore{
		events:      make([]*Event, 0),
		snapshots:   make([]*Snapshot, 0),
		eventsByID:  make(map[string]*Event),
		eventsByAgg: make(map[string][]*Event),
		metrics:     &TestStoreMetrics{},
	}
}

// WithFailOnSave configures the store to fail on save operations
func (tes *TestEventStore) WithFailOnSave(fail bool) *TestEventStore {
	tes.failOnSave = fail
	return tes
}

// WithFailOnGet configures the store to fail on get operations
func (tes *TestEventStore) WithFailOnGet(fail bool) *TestEventStore {
	tes.failOnGet = fail
	return tes
}

// WithSaveDelay adds delay to save operations
func (tes *TestEventStore) WithSaveDelay(delay time.Duration) *TestEventStore {
	tes.saveDelay = delay
	return tes
}

// WithGetDelay adds delay to get operations
func (tes *TestEventStore) WithGetDelay(delay time.Duration) *TestEventStore {
	tes.getDelay = delay
	return tes
}

// SaveEvent implements EventStore
func (tes *TestEventStore) SaveEvent(ctx context.Context, event *Event) error {
	tes.metrics.mu.Lock()
	tes.metrics.SaveEventCalls++
	tes.metrics.mu.Unlock()

	if tes.saveDelay > 0 {
		time.Sleep(tes.saveDelay)
	}

	if tes.failOnSave {
		return fmt.Errorf("test store configured to fail on save")
	}

	tes.mu.Lock()
	defer tes.mu.Unlock()

	tes.events = append(tes.events, event)
	tes.eventsByID[event.ID] = event

	if tes.eventsByAgg[event.AggregateID] == nil {
		tes.eventsByAgg[event.AggregateID] = make([]*Event, 0)
	}
	tes.eventsByAgg[event.AggregateID] = append(tes.eventsByAgg[event.AggregateID], event)

	return nil
}

// SaveEvents implements EventStore
func (tes *TestEventStore) SaveEvents(ctx context.Context, events []*Event) error {
	tes.metrics.mu.Lock()
	tes.metrics.SaveEventsCalls++
	tes.metrics.mu.Unlock()

	if tes.saveDelay > 0 {
		time.Sleep(tes.saveDelay)
	}

	if tes.failOnSave {
		return fmt.Errorf("test store configured to fail on save")
	}

	for _, event := range events {
		if err := tes.SaveEvent(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

// GetEvent implements EventStore
func (tes *TestEventStore) GetEvent(ctx context.Context, eventID string) (*Event, error) {
	tes.metrics.mu.Lock()
	tes.metrics.GetEventCalls++
	tes.metrics.mu.Unlock()

	if tes.getDelay > 0 {
		time.Sleep(tes.getDelay)
	}

	if tes.failOnGet {
		return nil, fmt.Errorf("test store configured to fail on get")
	}

	tes.mu.RLock()
	defer tes.mu.RUnlock()

	if event, exists := tes.eventsByID[eventID]; exists {
		return event, nil
	}

	return nil, fmt.Errorf("event not found: %s", eventID)
}

// GetEvents implements EventStore
func (tes *TestEventStore) GetEvents(ctx context.Context, criteria EventCriteria) (*EventCollection, error) {
	tes.metrics.mu.Lock()
	tes.metrics.GetEventsCalls++
	tes.metrics.mu.Unlock()

	if tes.getDelay > 0 {
		time.Sleep(tes.getDelay)
	}

	if tes.failOnGet {
		return nil, fmt.Errorf("test store configured to fail on get")
	}

	tes.mu.RLock()
	defer tes.mu.RUnlock()

	var filteredEvents []Event

	for _, event := range tes.events {
		if tes.matchesCriteria(event, criteria) {
			filteredEvents = append(filteredEvents, *event)
		}
	}

	// Apply offset and limit
	total := len(filteredEvents)
	start := int(criteria.Offset)
	end := start + criteria.Limit

	if start > total {
		filteredEvents = []Event{}
	} else {
		if end > total {
			end = total
		}
		filteredEvents = filteredEvents[start:end]
	}

	return NewEventCollection(filteredEvents, total, criteria.Offset, criteria.Limit), nil
}

// GetEventsByAggregate implements EventStore
func (tes *TestEventStore) GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*Event, error) {
	tes.metrics.mu.Lock()
	tes.metrics.GetEventsByAggCalls++
	tes.metrics.mu.Unlock()

	if tes.getDelay > 0 {
		time.Sleep(tes.getDelay)
	}

	if tes.failOnGet {
		return nil, fmt.Errorf("test store configured to fail on get")
	}

	tes.mu.RLock()
	defer tes.mu.RUnlock()

	events, exists := tes.eventsByAgg[aggregateID]
	if !exists {
		return []*Event{}, nil
	}

	var filteredEvents []*Event
	for _, event := range events {
		if event.Version >= fromVersion {
			filteredEvents = append(filteredEvents, event)
		}
	}

	return filteredEvents, nil
}

// CreateSnapshot implements EventStore
func (tes *TestEventStore) CreateSnapshot(ctx context.Context, snapshot *Snapshot) error {
	tes.metrics.mu.Lock()
	tes.metrics.CreateSnapshotCalls++
	tes.metrics.mu.Unlock()

	if tes.failOnSave {
		return fmt.Errorf("test store configured to fail on save")
	}

	tes.mu.Lock()
	defer tes.mu.Unlock()

	tes.snapshots = append(tes.snapshots, snapshot)
	return nil
}

// GetSnapshot implements EventStore
func (tes *TestEventStore) GetSnapshot(ctx context.Context, aggregateID string) (*Snapshot, error) {
	tes.metrics.mu.Lock()
	tes.metrics.GetSnapshotCalls++
	tes.metrics.mu.Unlock()

	if tes.failOnGet {
		return nil, fmt.Errorf("test store configured to fail on get")
	}

	tes.mu.RLock()
	defer tes.mu.RUnlock()

	var latestSnapshot *Snapshot
	for _, snapshot := range tes.snapshots {
		if snapshot.AggregateID == aggregateID {
			if latestSnapshot == nil || snapshot.Version > latestSnapshot.Version {
				latestSnapshot = snapshot
			}
		}
	}

	if latestSnapshot == nil {
		return nil, fmt.Errorf("snapshot not found for aggregate: %s", aggregateID)
	}

	return latestSnapshot, nil
}

// Helper methods for TestEventStore
func (tes *TestEventStore) matchesCriteria(event *Event, criteria EventCriteria) bool {
	// Check event types
	if len(criteria.EventTypes) > 0 {
		found := false
		for _, eventType := range criteria.EventTypes {
			if event.Type == eventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check aggregate IDs
	if len(criteria.AggregateIDs) > 0 {
		found := false
		for _, aggregateID := range criteria.AggregateIDs {
			if event.AggregateID == aggregateID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check time range
	if criteria.StartTime != nil && event.Timestamp.Before(*criteria.StartTime) {
		return false
	}
	if criteria.EndTime != nil && event.Timestamp.After(*criteria.EndTime) {
		return false
	}

	return true
}

// GetEventsByType implements EventStore
func (tes *TestEventStore) GetEventsByType(ctx context.Context, eventType string, limit int, offset int64) ([]*Event, error) {
	criteria := EventCriteria{
		EventTypes: []string{eventType},
		Limit:      limit,
		Offset:     offset,
	}

	collection, err := tes.GetEvents(ctx, criteria)
	if err != nil {
		return nil, err
	}

	var events []*Event
	for _, event := range collection.Events {
		eventCopy := event
		events = append(events, &eventCopy)
	}

	return events, nil
}

// GetEventsSince implements EventStore
func (tes *TestEventStore) GetEventsSince(ctx context.Context, since time.Time, limit int, offset int64) ([]*Event, error) {
	criteria := EventCriteria{
		StartTime: &since,
		Limit:     limit,
		Offset:    offset,
	}

	collection, err := tes.GetEvents(ctx, criteria)
	if err != nil {
		return nil, err
	}

	var events []*Event
	for _, event := range collection.Events {
		eventCopy := event
		events = append(events, &eventCopy)
	}

	return events, nil
}

// GetEventsInRange implements EventStore
func (tes *TestEventStore) GetEventsInRange(ctx context.Context, start, end time.Time, limit int, offset int64) ([]*Event, error) {
	criteria := EventCriteria{
		StartTime: &start,
		EndTime:   &end,
		Limit:     limit,
		Offset:    offset,
	}

	collection, err := tes.GetEvents(ctx, criteria)
	if err != nil {
		return nil, err
	}

	var events []*Event
	for _, event := range collection.Events {
		eventCopy := event
		events = append(events, &eventCopy)
	}

	return events, nil
}

// DeleteEvent implements EventStore
func (tes *TestEventStore) DeleteEvent(ctx context.Context, eventID string) error {
	tes.mu.Lock()
	defer tes.mu.Unlock()

	delete(tes.eventsByID, eventID)

	// Remove from events slice
	for i, event := range tes.events {
		if event.ID == eventID {
			tes.events = append(tes.events[:i], tes.events[i+1:]...)
			break
		}
	}

	return nil
}

// DeleteEventsByAggregate implements EventStore
func (tes *TestEventStore) DeleteEventsByAggregate(ctx context.Context, aggregateID string) error {
	tes.mu.Lock()
	defer tes.mu.Unlock()

	// Remove from eventsByAgg
	events := tes.eventsByAgg[aggregateID]
	delete(tes.eventsByAgg, aggregateID)

	// Remove from eventsByID and events slice
	for _, event := range events {
		delete(tes.eventsByID, event.ID)

		for i, e := range tes.events {
			if e.ID == event.ID {
				tes.events = append(tes.events[:i], tes.events[i+1:]...)
				break
			}
		}
	}

	return nil
}

// GetLastEvent implements EventStore
func (tes *TestEventStore) GetLastEvent(ctx context.Context, aggregateID string) (*Event, error) {
	events, err := tes.GetEventsByAggregate(ctx, aggregateID, 0)
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no events found for aggregate: %s", aggregateID)
	}

	return events[len(events)-1], nil
}

// GetEventCount implements EventStore
func (tes *TestEventStore) GetEventCount(ctx context.Context) (int64, error) {
	tes.mu.RLock()
	defer tes.mu.RUnlock()
	return int64(len(tes.events)), nil
}

// GetEventCountByType implements EventStore
func (tes *TestEventStore) GetEventCountByType(ctx context.Context, eventType string) (int64, error) {
	tes.mu.RLock()
	defer tes.mu.RUnlock()

	count := int64(0)
	for _, event := range tes.events {
		if event.Type == eventType {
			count++
		}
	}

	return count, nil
}

// DeleteSnapshot implements EventStore
func (tes *TestEventStore) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	tes.mu.Lock()
	defer tes.mu.Unlock()

	for i, snapshot := range tes.snapshots {
		if snapshot.ID == snapshotID {
			tes.snapshots = append(tes.snapshots[:i], tes.snapshots[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("snapshot not found: %s", snapshotID)
}

// Close implements EventStore
func (tes *TestEventStore) Close(ctx context.Context) error {
	return nil
}

// HealthCheck implements EventStore
func (tes *TestEventStore) HealthCheck(ctx context.Context) error {
	tes.metrics.mu.Lock()
	tes.metrics.HealthCheckCalls++
	tes.metrics.mu.Unlock()

	return nil
}

// GetMetrics returns test store metrics
func (tes *TestEventStore) GetMetrics() *TestStoreMetrics {
	return tes.metrics
}

// Reset clears all data from the test store
func (tes *TestEventStore) Reset() {
	tes.mu.Lock()
	defer tes.mu.Unlock()

	tes.events = make([]*Event, 0)
	tes.snapshots = make([]*Snapshot, 0)
	tes.eventsByID = make(map[string]*Event)
	tes.eventsByAgg = make(map[string][]*Event)
	tes.metrics = &TestStoreMetrics{}
}

// GetAllEvents returns all events in the store (for testing)
func (tes *TestEventStore) GetAllEvents() []*Event {
	tes.mu.RLock()
	defer tes.mu.RUnlock()

	events := make([]*Event, len(tes.events))
	copy(events, tes.events)
	return events
}

// MockMessageBroker implements MessageBroker for testing
type MockMessageBroker struct {
	name             string
	connected        bool
	published        []*Event
	subscriptions    map[string][]EventHandler
	publishError     error
	subscribeError   error
	connectError     error
	healthCheckError error
	publishDelay     time.Duration
	mu               sync.RWMutex
	metrics          *MockBrokerMetrics
}

// MockBrokerMetrics tracks mock broker operations
type MockBrokerMetrics struct {
	ConnectCalls     int
	PublishCalls     int
	SubscribeCalls   int
	UnsubscribeCalls int
	CloseCalls       int
	HealthCheckCalls int
	mu               sync.RWMutex
}

// NewMockMessageBroker creates a new mock message broker
func NewMockMessageBroker(name string) *MockMessageBroker {
	return &MockMessageBroker{
		name:          name,
		published:     make([]*Event, 0),
		subscriptions: make(map[string][]EventHandler),
		metrics:       &MockBrokerMetrics{},
	}
}

// WithConnectError configures the broker to fail on connect
func (mmb *MockMessageBroker) WithConnectError(err error) *MockMessageBroker {
	mmb.connectError = err
	return mmb
}

// WithPublishError configures the broker to fail on publish
func (mmb *MockMessageBroker) WithPublishError(err error) *MockMessageBroker {
	mmb.publishError = err
	return mmb
}

// WithSubscribeError configures the broker to fail on subscribe
func (mmb *MockMessageBroker) WithSubscribeError(err error) *MockMessageBroker {
	mmb.subscribeError = err
	return mmb
}

// WithHealthCheckError configures the broker to fail on health check
func (mmb *MockMessageBroker) WithHealthCheckError(err error) *MockMessageBroker {
	mmb.healthCheckError = err
	return mmb
}

// WithPublishDelay adds delay to publish operations
func (mmb *MockMessageBroker) WithPublishDelay(delay time.Duration) *MockMessageBroker {
	mmb.publishDelay = delay
	return mmb
}

// Connect implements MessageBroker
func (mmb *MockMessageBroker) Connect(ctx context.Context, config interface{}) error {
	mmb.metrics.mu.Lock()
	mmb.metrics.ConnectCalls++
	mmb.metrics.mu.Unlock()

	if mmb.connectError != nil {
		return mmb.connectError
	}

	mmb.mu.Lock()
	mmb.connected = true
	mmb.mu.Unlock()

	return nil
}

// Publish implements MessageBroker
func (mmb *MockMessageBroker) Publish(ctx context.Context, topic string, event Event) error {
	mmb.metrics.mu.Lock()
	mmb.metrics.PublishCalls++
	mmb.metrics.mu.Unlock()

	if mmb.publishDelay > 0 {
		time.Sleep(mmb.publishDelay)
	}

	if mmb.publishError != nil {
		return mmb.publishError
	}

	mmb.mu.Lock()
	defer mmb.mu.Unlock()

	if !mmb.connected {
		return fmt.Errorf("broker not connected")
	}

	mmb.published = append(mmb.published, &event)

	// Trigger handlers for this topic
	if handlers, exists := mmb.subscriptions[topic]; exists {
		for _, handler := range handlers {
			go func(h EventHandler, e *Event) {
				h.Handle(ctx, e)
			}(handler, &event)
		}
	}

	return nil
}

// Subscribe implements MessageBroker
func (mmb *MockMessageBroker) Subscribe(ctx context.Context, topic string, handler EventHandler) error {
	mmb.metrics.mu.Lock()
	mmb.metrics.SubscribeCalls++
	mmb.metrics.mu.Unlock()

	if mmb.subscribeError != nil {
		return mmb.subscribeError
	}

	mmb.mu.Lock()
	defer mmb.mu.Unlock()

	if !mmb.connected {
		return fmt.Errorf("broker not connected")
	}

	if mmb.subscriptions[topic] == nil {
		mmb.subscriptions[topic] = make([]EventHandler, 0)
	}
	mmb.subscriptions[topic] = append(mmb.subscriptions[topic], handler)

	return nil
}

// Unsubscribe implements MessageBroker
func (mmb *MockMessageBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	mmb.metrics.mu.Lock()
	mmb.metrics.UnsubscribeCalls++
	mmb.metrics.mu.Unlock()

	mmb.mu.Lock()
	defer mmb.mu.Unlock()

	if handlers, exists := mmb.subscriptions[topic]; exists {
		for i, handler := range handlers {
			if handler.Name() == handlerName {
				mmb.subscriptions[topic] = append(handlers[:i], handlers[i+1:]...)
				break
			}
		}
	}

	return nil
}

// Close implements MessageBroker
func (mmb *MockMessageBroker) Close(ctx context.Context) error {
	mmb.metrics.mu.Lock()
	mmb.metrics.CloseCalls++
	mmb.metrics.mu.Unlock()

	mmb.mu.Lock()
	mmb.connected = false
	mmb.mu.Unlock()

	return nil
}

// HealthCheck implements MessageBroker
func (mmb *MockMessageBroker) HealthCheck(ctx context.Context) error {
	mmb.metrics.mu.Lock()
	mmb.metrics.HealthCheckCalls++
	mmb.metrics.mu.Unlock()

	if mmb.healthCheckError != nil {
		return mmb.healthCheckError
	}

	return nil
}

// GetStats implements MessageBroker
func (mmb *MockMessageBroker) GetStats() map[string]interface{} {
	mmb.mu.RLock()
	defer mmb.mu.RUnlock()

	return map[string]interface{}{
		"name":               mmb.name,
		"connected":          mmb.connected,
		"published_count":    len(mmb.published),
		"subscription_count": len(mmb.subscriptions),
	}
}

// GetPublishedEvents returns all published events
func (mmb *MockMessageBroker) GetPublishedEvents() []*Event {
	mmb.mu.RLock()
	defer mmb.mu.RUnlock()

	events := make([]*Event, len(mmb.published))
	copy(events, mmb.published)
	return events
}

// GetSubscriptions returns all subscriptions
func (mmb *MockMessageBroker) GetSubscriptions() map[string][]EventHandler {
	mmb.mu.RLock()
	defer mmb.mu.RUnlock()

	subs := make(map[string][]EventHandler)
	for topic, handlers := range mmb.subscriptions {
		subs[topic] = make([]EventHandler, len(handlers))
		copy(subs[topic], handlers)
	}
	return subs
}

// GetMetrics returns mock broker metrics
func (mmb *MockMessageBroker) GetMetrics() *MockBrokerMetrics {
	return mmb.metrics
}

// Reset clears all data from the mock broker
func (mmb *MockMessageBroker) Reset() {
	mmb.mu.Lock()
	defer mmb.mu.Unlock()

	mmb.published = make([]*Event, 0)
	mmb.subscriptions = make(map[string][]EventHandler)
	mmb.metrics = &MockBrokerMetrics{}
}

// TestEventHandler is a simple event handler for testing
type TestEventHandler struct {
	name          string
	handled       []*Event
	handleError   error
	handleDelay   time.Duration
	eventTypes    map[string]bool
	mu            sync.RWMutex
	callCount     int
	lastHandledAt time.Time
}

// NewTestEventHandler creates a new test event handler
func NewTestEventHandler(name string, eventTypes ...string) *TestEventHandler {
	typeMap := make(map[string]bool)
	for _, eventType := range eventTypes {
		typeMap[eventType] = true
	}

	return &TestEventHandler{
		name:       name,
		handled:    make([]*Event, 0),
		eventTypes: typeMap,
	}
}

// WithHandleError configures the handler to fail
func (teh *TestEventHandler) WithHandleError(err error) *TestEventHandler {
	teh.handleError = err
	return teh
}

// WithHandleDelay adds delay to handle operations
func (teh *TestEventHandler) WithHandleDelay(delay time.Duration) *TestEventHandler {
	teh.handleDelay = delay
	return teh
}

// Handle implements EventHandler
func (teh *TestEventHandler) Handle(ctx context.Context, event *Event) error {
	teh.mu.Lock()
	defer teh.mu.Unlock()

	teh.callCount++
	teh.lastHandledAt = time.Now()

	if teh.handleDelay > 0 {
		time.Sleep(teh.handleDelay)
	}

	if teh.handleError != nil {
		return teh.handleError
	}

	teh.handled = append(teh.handled, event)
	return nil
}

// CanHandle implements EventHandler
func (teh *TestEventHandler) CanHandle(event *Event) bool {
	if len(teh.eventTypes) == 0 {
		return true // Handle all events if no types specified
	}
	return teh.eventTypes[event.Type]
}

// Name implements EventHandler
func (teh *TestEventHandler) Name() string {
	return teh.name
}

// GetHandledEvents returns all handled events
func (teh *TestEventHandler) GetHandledEvents() []*Event {
	teh.mu.RLock()
	defer teh.mu.RUnlock()

	events := make([]*Event, len(teh.handled))
	copy(events, teh.handled)
	return events
}

// GetCallCount returns the number of times Handle was called
func (teh *TestEventHandler) GetCallCount() int {
	teh.mu.RLock()
	defer teh.mu.RUnlock()
	return teh.callCount
}

// GetLastHandledAt returns when Handle was last called
func (teh *TestEventHandler) GetLastHandledAt() time.Time {
	teh.mu.RLock()
	defer teh.mu.RUnlock()
	return teh.lastHandledAt
}

// Reset clears all handled events and resets counters
func (teh *TestEventHandler) Reset() {
	teh.mu.Lock()
	defer teh.mu.Unlock()

	teh.handled = make([]*Event, 0)
	teh.callCount = 0
	teh.lastHandledAt = time.Time{}
}

// EventAssertions provides assertion helpers for testing events
type EventAssertions struct {
	t *testing.T
}

// NewEventAssertions creates a new event assertions helper
func NewEventAssertions(t *testing.T) *EventAssertions {
	return &EventAssertions{t: t}
}

// AssertEventStored checks if an event was stored
func (ea *EventAssertions) AssertEventStored(store *TestEventStore, eventID string) {
	_, err := store.GetEvent(context.Background(), eventID)
	if err != nil {
		ea.t.Errorf("Expected event %s to be stored, but it was not found: %v", eventID, err)
	}
}

// AssertEventNotStored checks if an event was not stored
func (ea *EventAssertions) AssertEventNotStored(store *TestEventStore, eventID string) {
	_, err := store.GetEvent(context.Background(), eventID)
	if err == nil {
		ea.t.Errorf("Expected event %s to not be stored, but it was found", eventID)
	}
}

// AssertEventPublished checks if an event was published to a broker
func (ea *EventAssertions) AssertEventPublished(broker *MockMessageBroker, eventID string) {
	published := broker.GetPublishedEvents()
	for _, event := range published {
		if event.ID == eventID {
			return
		}
	}
	ea.t.Errorf("Expected event %s to be published, but it was not found", eventID)
}

// AssertEventHandled checks if an event was handled by a handler
func (ea *EventAssertions) AssertEventHandled(handler *TestEventHandler, eventID string) {
	handled := handler.GetHandledEvents()
	for _, event := range handled {
		if event.ID == eventID {
			return
		}
	}
	ea.t.Errorf("Expected event %s to be handled, but it was not found", eventID)
}

// AssertEventCount checks the number of events in a store
func (ea *EventAssertions) AssertEventCount(store *TestEventStore, expected int) {
	actual := len(store.GetAllEvents())
	if actual != expected {
		ea.t.Errorf("Expected %d events in store, but found %d", expected, actual)
	}
}

// AssertHandlerCallCount checks the number of times a handler was called
func (ea *EventAssertions) AssertHandlerCallCount(handler *TestEventHandler, expected int) {
	actual := handler.GetCallCount()
	if actual != expected {
		ea.t.Errorf("Expected handler to be called %d times, but it was called %d times", expected, actual)
	}
}

// AssertEventData checks if an event has the expected data
func (ea *EventAssertions) AssertEventData(event *Event, expected interface{}) {
	if event.Data != expected {
		ea.t.Errorf("Expected event data to be %v, but got %v", expected, event.Data)
	}
}

// AssertEventType checks if an event has the expected type
func (ea *EventAssertions) AssertEventType(event *Event, expected string) {
	if event.Type != expected {
		ea.t.Errorf("Expected event type to be %s, but got %s", expected, event.Type)
	}
}

// AssertEventAggregateID checks if an event has the expected aggregate ID
func (ea *EventAssertions) AssertEventAggregateID(event *Event, expected string) {
	if event.AggregateID != expected {
		ea.t.Errorf("Expected event aggregate ID to be %s, but got %s", expected, event.AggregateID)
	}
}

// EventTestSuite provides a comprehensive testing suite for events
type EventTestSuite struct {
	Store      *TestEventStore
	Broker     *MockMessageBroker
	Handler    *TestEventHandler
	Registry   *HandlerRegistry
	Assertions *EventAssertions
	Logger     common.Logger
	Metrics    common.Metrics
}

// NewEventTestSuite creates a new event test suite
func NewEventTestSuite(t *testing.T) *EventTestSuite {
	store := NewTestEventStore()
	broker := NewMockMessageBroker("test-broker")
	handler := NewTestEventHandler("test-handler")
	logger := logger.NewNoopLogger()
	metrics := &testMetrics{}
	registry := NewHandlerRegistry(logger, metrics)
	assertions := NewEventAssertions(t)

	return &EventTestSuite{
		Store:      store,
		Broker:     broker,
		Handler:    handler,
		Registry:   registry,
		Assertions: assertions,
		Logger:     logger,
		Metrics:    metrics,
	}
}

// Setup initializes the test suite
func (ets *EventTestSuite) Setup() error {
	if err := ets.Broker.Connect(context.Background(), nil); err != nil {
		return fmt.Errorf("failed to connect broker: %w", err)
	}

	if err := ets.Registry.Register("test.event", ets.Handler); err != nil {
		return fmt.Errorf("failed to register handler: %w", err)
	}

	return nil
}

// Teardown cleans up the test suite
func (ets *EventTestSuite) Teardown() error {
	ets.Store.Reset()
	ets.Broker.Reset()
	ets.Handler.Reset()

	return ets.Broker.Close(context.Background())
}

// CreateTestSchema creates a test schema
func CreateTestSchema(name string, version int) *schema.Schema {
	return &schema.Schema{
		ID:          fmt.Sprintf("%s-v%d-%s", name, version, uuid.New().String()),
		Name:        name,
		Version:     version,
		Type:        "json-schema",
		Title:       fmt.Sprintf("%s Schema", name),
		Description: fmt.Sprintf("Test schema for %s events", name),
		Properties: map[string]*schema.Property{
			"id": {
				Type:        "string",
				Description: "Event identifier",
			},
			"data": {
				Type:        "object",
				Description: "Event data",
			},
		},
		Required:  []string{"id", "data"},
		Metadata:  make(map[string]interface{}),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// Simple test implementations for logger and metrics

type testMetrics struct{}

func (tm *testMetrics) Counter(name string, tags ...string) common.Counter { return &testCounter{} }
func (tm *testMetrics) Gauge(name string, tags ...string) common.Gauge     { return &testGauge{} }
func (tm *testMetrics) Histogram(name string, tags ...string) common.Histogram {
	return &testHistogram{}
}
func (tm *testMetrics) Timer(name string, tags ...string) common.Timer { return &testTimer{} }

func (tm *testMetrics) Name() string {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Dependencies() []string {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Start(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Stop(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) OnHealthCheck(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) RegisterCollector(collector common.CustomCollector) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) UnregisterCollector(name string) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetCollectors() []common.CustomCollector {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetMetrics() map[string]interface{} {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetMetricsByType(metricType common.MetricType) map[string]interface{} {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetMetricsByTag(tagKey, tagValue string) map[string]interface{} {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Export(format common.ExportFormat) ([]byte, error) {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) ExportToFile(format common.ExportFormat, filename string) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) Reset() error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) ResetMetric(name string) error {
	// TODO implement me
	panic("implement me")
}

func (tm *testMetrics) GetStats() common.CollectorStats {
	// TODO implement me
	panic("implement me")
}

type testCounter struct{}

func (tc *testCounter) Dec() {
	return
}

func (tc *testCounter) Get() float64 {
	return 0
}

func (tc *testCounter) Reset() {
}

func (tc *testCounter) Inc()              {}
func (tc *testCounter) Add(value float64) {}

type testGauge struct{}

func (tg *testGauge) Get() float64 {
	return 0
}

func (tg *testGauge) Reset() {
}

func (tg *testGauge) Set(value float64) {}
func (tg *testGauge) Inc()              {}
func (tg *testGauge) Dec()              {}
func (tg *testGauge) Add(value float64) {}

type testHistogram struct{}

func (th *testHistogram) GetBuckets() map[float64]uint64 {
	return map[float64]uint64{}
}

func (th *testHistogram) GetCount() uint64 {
	return 0
}

func (th *testHistogram) GetSum() float64 {
	return 0
}

func (th *testHistogram) GetMean() float64 {
	return 0
}

func (th *testHistogram) GetPercentile(percentile float64) float64 {
	return 0
}

func (th *testHistogram) Reset() {
}

func (th *testHistogram) Observe(value float64) {}

type testTimer struct{}

func (tt *testTimer) GetCount() uint64 {
	return 0
}

func (tt *testTimer) GetMean() time.Duration {
	return 0
}

func (tt *testTimer) GetPercentile(percentile float64) time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) GetMin() time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) GetMax() time.Duration {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) Reset() {
	// TODO implement me
	panic("implement me")
}

func (tt *testTimer) Record(duration time.Duration) {}
func (tt *testTimer) Time() func()                  { return func() {} }
