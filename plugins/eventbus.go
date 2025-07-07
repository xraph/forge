package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// eventBus implements the EventBus interface
type eventBus struct {
	mu           sync.RWMutex
	handlers     map[string][]EventHandler
	subscribers  map[string][]Subscriber
	eventHistory []Event
	logger       logger.Logger
	config       EventBusConfig

	// Buffering and batching
	eventBuffer  chan Event
	batchSize    int
	batchTimeout time.Duration

	// Statistics
	stats EventBusStatistics

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// Subscriber represents an event subscriber with metadata
type Subscriber struct {
	ID       string
	Handler  EventHandler
	Filter   EventFilter
	Priority int
	Async    bool
	Metadata map[string]interface{}
}

// EventFilter defines criteria for event filtering
type EventFilter struct {
	EventTypes []string
	Plugins    []string
	Patterns   []string
	MinLevel   EventLevel
	MaxAge     time.Duration
}

// EventLevel represents event severity levels
type EventLevel int

const (
	EventLevelDebug EventLevel = iota
	EventLevelInfo
	EventLevelWarn
	EventLevelError
	EventLevelCritical
)

// EventBusConfig represents event bus configuration
type EventBusConfig struct {
	BufferSize      int           `mapstructure:"buffer_size" yaml:"buffer_size"`
	BatchSize       int           `mapstructure:"batch_size" yaml:"batch_size"`
	BatchTimeout    time.Duration `mapstructure:"batch_timeout" yaml:"batch_timeout"`
	MaxHistory      int           `mapstructure:"max_history" yaml:"max_history"`
	EnableAsync     bool          `mapstructure:"enable_async" yaml:"enable_async"`
	RetryAttempts   int           `mapstructure:"retry_attempts" yaml:"retry_attempts"`
	RetryDelay      time.Duration `mapstructure:"retry_delay" yaml:"retry_delay"`
	DeadLetterQueue bool          `mapstructure:"dead_letter_queue" yaml:"dead_letter_queue"`
}

// EventBusStatistics represents event bus statistics
type EventBusStatistics struct {
	TotalEvents     int64 `json:"total_events"`
	EventsPerSecond int64 `json:"events_per_second"`
	HandlerCount    int   `json:"handler_count"`
	SubscriberCount int   `json:"subscriber_count"`
	FailedEvents    int64 `json:"failed_events"`
	BufferSize      int   `json:"buffer_size"`
	BufferUsage     int   `json:"buffer_usage"`
}

// NewEventBus creates a new event bus
func NewEventBus() EventBus {
	return NewEventBusWithConfig(EventBusConfig{
		BufferSize:      1000,
		BatchSize:       10,
		BatchTimeout:    100 * time.Millisecond,
		MaxHistory:      1000,
		EnableAsync:     true,
		RetryAttempts:   3,
		RetryDelay:      1 * time.Second,
		DeadLetterQueue: true,
	})
}

// NewEventBusWithConfig creates a new event bus with custom configuration
func NewEventBusWithConfig(config EventBusConfig) EventBus {
	ctx, cancel := context.WithCancel(context.Background())

	eb := &eventBus{
		handlers:     make(map[string][]EventHandler),
		subscribers:  make(map[string][]Subscriber),
		eventHistory: make([]Event, 0),
		logger:       logger.GetGlobalLogger().Named("event-bus"),
		config:       config,
		eventBuffer:  make(chan Event, config.BufferSize),
		batchSize:    config.BatchSize,
		batchTimeout: config.BatchTimeout,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
	}

	// Start event processing goroutine
	go eb.processEvents()

	eb.logger.Info("Event bus initialized",
		logger.Int("buffer_size", config.BufferSize),
		logger.Int("batch_size", config.BatchSize),
		logger.Duration("batch_timeout", config.BatchTimeout),
	)

	return eb
}

// Subscribe subscribes an event handler to specific event types
func (eb *eventBus) Subscribe(eventType string, handler EventHandler) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.handlers[eventType] = append(eb.handlers[eventType], handler)

	eb.logger.Debug("Event handler subscribed",
		logger.String("event_type", eventType),
		logger.Int("handler_count", len(eb.handlers[eventType])),
	)

	return nil
}

// Unsubscribe removes an event handler from event types
func (eb *eventBus) Unsubscribe(eventType string, handler EventHandler) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	handlers := eb.handlers[eventType]
	for i, h := range handlers {
		// Simple pointer comparison (in real implementation, you'd use proper handler identification)
		if fmt.Sprintf("%p", h) == fmt.Sprintf("%p", handler) {
			eb.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			eb.logger.Debug("Event handler unsubscribed",
				logger.String("event_type", eventType),
			)
			break
		}
	}

	return nil
}

// Publish publishes an event synchronously
func (eb *eventBus) Publish(ctx context.Context, event Event) error {
	eb.logger.Debug("Publishing event",
		logger.String("type", event.Type),
		logger.String("plugin", event.Plugin),
	)

	// Update statistics
	eb.stats.TotalEvents++

	// Add to history
	eb.addToHistory(event)

	// Get handlers for this event type
	eb.mu.RLock()
	handlers := make([]EventHandler, len(eb.handlers[event.Type]))
	copy(handlers, eb.handlers[event.Type])

	// Also get wildcard handlers
	wildcardHandlers := eb.handlers["*"]
	handlers = append(handlers, wildcardHandlers...)
	eb.mu.RUnlock()

	// Process handlers
	var errors []error
	for _, handler := range handlers {
		if err := eb.executeHandler(ctx, handler, event); err != nil {
			eb.stats.FailedEvents++
			errors = append(errors, err)
			eb.logger.Error("Event handler failed",
				logger.String("event_type", event.Type),
				logger.String("plugin", event.Plugin),
				logger.Error(err),
			)
		}
	}

	// Process subscribers
	eb.processSubscribers(ctx, event)

	if len(errors) > 0 {
		return fmt.Errorf("failed to process %d handlers: %v", len(errors), errors)
	}

	return nil
}

// PublishAsync publishes an event asynchronously
func (eb *eventBus) PublishAsync(ctx context.Context, event Event) error {
	if !eb.config.EnableAsync {
		return eb.Publish(ctx, event)
	}

	select {
	case eb.eventBuffer <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Buffer full, handle based on policy
		eb.logger.Warn("Event buffer full, dropping event",
			logger.String("event_type", event.Type),
			logger.String("plugin", event.Plugin),
		)
		return fmt.Errorf("event buffer full")
	}
}

// Advanced subscription methods

// SubscribeWithFilter subscribes with event filtering
func (eb *eventBus) SubscribeWithFilter(filter EventFilter, handler EventHandler) (string, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subscriberID := eb.generateSubscriberID()
	subscriber := Subscriber{
		ID:       subscriberID,
		Handler:  handler,
		Filter:   filter,
		Priority: 0,
		Async:    false,
	}

	// Add to all matching event types
	for _, eventType := range filter.EventTypes {
		eb.subscribers[eventType] = append(eb.subscribers[eventType], subscriber)
	}

	eb.logger.Debug("Filtered subscription created",
		logger.String("subscriber_id", subscriberID),
		logger.Strings("event_types", filter.EventTypes),
	)

	return subscriberID, nil
}

// SubscribeAsync subscribes with async processing
func (eb *eventBus) SubscribeAsync(eventType string, handler EventHandler, priority int) (string, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subscriberID := eb.generateSubscriberID()
	subscriber := Subscriber{
		ID:       subscriberID,
		Handler:  handler,
		Priority: priority,
		Async:    true,
		Filter: EventFilter{
			EventTypes: []string{eventType},
		},
	}

	eb.subscribers[eventType] = append(eb.subscribers[eventType], subscriber)

	eb.logger.Debug("Async subscription created",
		logger.String("subscriber_id", subscriberID),
		logger.String("event_type", eventType),
		logger.Int("priority", priority),
	)

	return subscriberID, nil
}

// UnsubscribeByID removes a subscription by ID
func (eb *eventBus) UnsubscribeByID(subscriberID string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for eventType, subscribers := range eb.subscribers {
		for i, subscriber := range subscribers {
			if subscriber.ID == subscriberID {
				eb.subscribers[eventType] = append(subscribers[:i], subscribers[i+1:]...)
				eb.logger.Debug("Subscription removed",
					logger.String("subscriber_id", subscriberID),
					logger.String("event_type", eventType),
				)
				return nil
			}
		}
	}

	return fmt.Errorf("subscriber not found: %s", subscriberID)
}

// Event querying and history

// GetEventHistory returns recent events
func (eb *eventBus) GetEventHistory(limit int) []Event {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if limit <= 0 || limit > len(eb.eventHistory) {
		limit = len(eb.eventHistory)
	}

	// Return most recent events
	start := len(eb.eventHistory) - limit
	result := make([]Event, limit)
	copy(result, eb.eventHistory[start:])

	return result
}

// QueryEvents queries events with filters
func (eb *eventBus) QueryEvents(filter EventFilter) []Event {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	var result []Event
	for _, event := range eb.eventHistory {
		if eb.matchesFilter(event, filter) {
			result = append(result, event)
		}
	}

	return result
}

// Statistics and monitoring

// GetStatistics returns event bus statistics
func (eb *eventBus) GetStatistics() EventBusStatistics {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	stats := eb.stats
	stats.HandlerCount = eb.getTotalHandlerCount()
	stats.SubscriberCount = eb.getTotalSubscriberCount()
	stats.BufferUsage = len(eb.eventBuffer)

	return stats
}

// EmitSystemEvent emits a system-level event
func (eb *eventBus) EmitSystemEvent(eventType string, data map[string]interface{}) error {
	event := Event{
		Type:      eventType,
		Plugin:    "system",
		Data:      data,
		Timestamp: time.Now(),
	}

	return eb.PublishAsync(context.Background(), event)
}

// Private helper methods

func (eb *eventBus) processEvents() {
	defer close(eb.done)

	batch := make([]Event, 0, eb.batchSize)
	ticker := time.NewTicker(eb.batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case event := <-eb.eventBuffer:
			batch = append(batch, event)

			if len(batch) >= eb.batchSize {
				eb.processBatch(batch)
				batch = batch[:0] // Reset batch
			}

		case <-ticker.C:
			if len(batch) > 0 {
				eb.processBatch(batch)
				batch = batch[:0] // Reset batch
			}

		case <-eb.ctx.Done():
			// Process remaining events
			if len(batch) > 0 {
				eb.processBatch(batch)
			}
			return
		}
	}
}

func (eb *eventBus) processBatch(events []Event) {
	eb.logger.Debug("Processing event batch",
		logger.Int("size", len(events)),
	)

	for _, event := range events {
		if err := eb.Publish(eb.ctx, event); err != nil {
			eb.logger.Error("Failed to process batched event",
				logger.String("event_type", event.Type),
				logger.String("plugin", event.Plugin),
				logger.Error(err),
			)
		}
	}
}

func (eb *eventBus) executeHandler(ctx context.Context, handler EventHandler, event Event) error {
	// Add timeout for handler execution
	handlerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Execute with retry logic
	var lastErr error
	for attempt := 0; attempt <= eb.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(eb.config.RetryDelay):
			case <-handlerCtx.Done():
				return handlerCtx.Err()
			}
		}

		if err := handler.Handle(handlerCtx, event); err != nil {
			lastErr = err
			eb.logger.Warn("Event handler failed, retrying",
				logger.String("event_type", event.Type),
				logger.Int("attempt", attempt+1),
				logger.Error(err),
			)
			continue
		}

		return nil // Success
	}

	return lastErr
}

func (eb *eventBus) processSubscribers(ctx context.Context, event Event) {
	eb.mu.RLock()
	subscribers := make([]Subscriber, 0)

	// Get direct subscribers
	if subs, exists := eb.subscribers[event.Type]; exists {
		subscribers = append(subscribers, subs...)
	}

	// Get wildcard subscribers
	if subs, exists := eb.subscribers["*"]; exists {
		subscribers = append(subscribers, subs...)
	}
	eb.mu.RUnlock()

	// Sort by priority (higher priority first)
	eb.sortSubscribersByPriority(subscribers)

	// Process subscribers
	for _, subscriber := range subscribers {
		if !eb.matchesFilter(event, subscriber.Filter) {
			continue
		}

		if subscriber.Async {
			go eb.executeSubscriber(ctx, subscriber, event)
		} else {
			eb.executeSubscriber(ctx, subscriber, event)
		}
	}
}

func (eb *eventBus) executeSubscriber(ctx context.Context, subscriber Subscriber, event Event) {
	defer func() {
		if r := recover(); r != nil {
			eb.logger.Error("Subscriber panicked",
				logger.String("subscriber_id", subscriber.ID),
				logger.String("event_type", event.Type),
				logger.Any("panic", r),
			)
		}
	}()

	if err := subscriber.Handler.Handle(ctx, event); err != nil {
		eb.logger.Error("Subscriber failed",
			logger.String("subscriber_id", subscriber.ID),
			logger.String("event_type", event.Type),
			logger.Error(err),
		)
	}
}

func (eb *eventBus) matchesFilter(event Event, filter EventFilter) bool {
	// Check event types
	if len(filter.EventTypes) > 0 {
		found := false
		for _, eventType := range filter.EventTypes {
			if eventType == "*" || eventType == event.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check plugins
	if len(filter.Plugins) > 0 {
		found := false
		for _, plugin := range filter.Plugins {
			if plugin == "*" || plugin == event.Plugin {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check age
	if filter.MaxAge > 0 {
		if time.Since(event.Timestamp) > filter.MaxAge {
			return false
		}
	}

	return true
}

func (eb *eventBus) addToHistory(event Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.eventHistory = append(eb.eventHistory, event)

	// Trim history if too large
	if len(eb.eventHistory) > eb.config.MaxHistory {
		eb.eventHistory = eb.eventHistory[1:]
	}
}

func (eb *eventBus) generateSubscriberID() string {
	return fmt.Sprintf("sub_%d_%d", time.Now().UnixNano(), len(eb.subscribers))
}

func (eb *eventBus) getTotalHandlerCount() int {
	count := 0
	for _, handlers := range eb.handlers {
		count += len(handlers)
	}
	return count
}

func (eb *eventBus) getTotalSubscriberCount() int {
	count := 0
	for _, subscribers := range eb.subscribers {
		count += len(subscribers)
	}
	return count
}

func (eb *eventBus) sortSubscribersByPriority(subscribers []Subscriber) {
	// Simple bubble sort by priority (higher priority first)
	for i := 0; i < len(subscribers)-1; i++ {
		for j := 0; j < len(subscribers)-i-1; j++ {
			if subscribers[j].Priority < subscribers[j+1].Priority {
				subscribers[j], subscribers[j+1] = subscribers[j+1], subscribers[j]
			}
		}
	}
}

// Shutdown gracefully shuts down the event bus
func (eb *eventBus) Shutdown(ctx context.Context) error {
	eb.logger.Info("Shutting down event bus")

	eb.cancel()

	select {
	case <-eb.done:
		eb.logger.Info("Event bus shut down successfully")
		return nil
	case <-ctx.Done():
		eb.logger.Warn("Event bus shutdown timed out")
		return ctx.Err()
	}
}

// Standard event types
const (
	EventTypePluginRegistered   = "plugin.registered"
	EventTypePluginUnregistered = "plugin.unregistered"
	EventTypePluginEnabled      = "plugin.enabled"
	EventTypePluginDisabled     = "plugin.disabled"
	EventTypePluginLoaded       = "plugin.loaded"
	EventTypePluginUnloaded     = "plugin.unloaded"
	EventTypePluginStarted      = "plugin.started"
	EventTypePluginStopped      = "plugin.stopped"
	EventTypePluginError        = "plugin.error"
	EventTypePluginReloaded     = "plugin.reloaded"
	EventTypeConfigChanged      = "config.changed"
	EventTypeViolationDetected  = "security.violation"
	EventTypeResourceWarning    = "resource.warning"
	EventTypeResourceLimit      = "resource.limit"
)

// Helper functions for creating common events

// NewPluginEvent creates a plugin-related event
func NewPluginEvent(eventType, pluginName string, data map[string]interface{}) Event {
	if data == nil {
		data = make(map[string]interface{})
	}

	return Event{
		Type:      eventType,
		Plugin:    pluginName,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// NewErrorEvent creates an error event
func NewErrorEvent(pluginName string, err error, data map[string]interface{}) Event {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["error"] = err.Error()

	return Event{
		Type:      EventTypePluginError,
		Plugin:    pluginName,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// NewConfigEvent creates a configuration change event
func NewConfigEvent(pluginName string, oldConfig, newConfig map[string]interface{}) Event {
	return Event{
		Type:   EventTypeConfigChanged,
		Plugin: pluginName,
		Data: map[string]interface{}{
			"old_config": oldConfig,
			"new_config": newConfig,
		},
		Timestamp: time.Now(),
	}
}

// LoggingEventHandler creates an event handler that logs events
type LoggingEventHandler struct {
	logger logger.Logger
	level  EventLevel
}

func NewLoggingEventHandler(level EventLevel) *LoggingEventHandler {
	return &LoggingEventHandler{
		logger: logger.GetGlobalLogger().Named("event-handler"),
		level:  level,
	}
}

func (h *LoggingEventHandler) Handle(ctx context.Context, event Event) error {
	switch h.level {
	case EventLevelDebug:
		h.logger.Debug("Event received",
			logger.String("type", event.Type),
			logger.String("plugin", event.Plugin),
			logger.Any("data", event.Data),
		)
	case EventLevelInfo:
		h.logger.Info("Event received",
			logger.String("type", event.Type),
			logger.String("plugin", event.Plugin),
		)
	case EventLevelWarn:
		h.logger.Warn("Event received",
			logger.String("type", event.Type),
			logger.String("plugin", event.Plugin),
		)
	case EventLevelError:
		h.logger.Error("Event received",
			logger.String("type", event.Type),
			logger.String("plugin", event.Plugin),
		)
	}

	return nil
}

// MetricsEventHandler creates an event handler that tracks metrics
type MetricsEventHandler struct {
	eventCounts map[string]int64
	mu          sync.RWMutex
}

func NewMetricsEventHandler() *MetricsEventHandler {
	return &MetricsEventHandler{
		eventCounts: make(map[string]int64),
	}
}

func (h *MetricsEventHandler) Handle(ctx context.Context, event Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.eventCounts[event.Type]++
	h.eventCounts["total"]++

	return nil
}

func (h *MetricsEventHandler) GetCounts() map[string]int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]int64)
	for k, v := range h.eventCounts {
		result[k] = v
	}

	return result
}
