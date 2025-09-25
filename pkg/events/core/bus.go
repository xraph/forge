package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// EventBus defines the interface for the event bus
type EventBus interface {
	common.Service

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

// EventBusConfig defines configuration for the event bus
type EventBusConfig struct {
	DefaultBroker     string        `yaml:"default_broker" json:"default_broker"`
	MaxRetries        int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay        time.Duration `yaml:"retry_delay" json:"retry_delay"`
	EnableMetrics     bool          `yaml:"enable_metrics" json:"enable_metrics"`
	EnableTracing     bool          `yaml:"enable_tracing" json:"enable_tracing"`
	BufferSize        int           `yaml:"buffer_size" json:"buffer_size"`
	WorkerCount       int           `yaml:"worker_count" json:"worker_count"`
	ProcessingTimeout time.Duration `yaml:"processing_timeout" json:"processing_timeout"`
}

// EventBusImpl implements EventBus
type EventBusImpl struct {
	name            string
	brokers         map[string]MessageBroker
	defaultBroker   string
	store           EventStore
	handlerRegistry *HandlerRegistry
	config          EventBusConfig
	logger          common.Logger
	metrics         common.Metrics
	workers         []*EventWorker
	eventQueue      chan *EventEnvelope
	started         bool
	stopping        bool
	mu              sync.RWMutex
	wg              sync.WaitGroup
}

// EventBusOptions defines configuration for EventBusImpl
type EventBusOptions struct {
	Store           EventStore
	HandlerRegistry *HandlerRegistry
	Logger          common.Logger
	Metrics         common.Metrics
	Config          EventBusConfig
}

// NewEventBus creates a new event bus
func NewEventBus(config EventBusOptions) (EventBus, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("event store is required")
	}
	if config.HandlerRegistry == nil {
		return nil, fmt.Errorf("handler registry is required")
	}

	eventQueue := make(chan *EventEnvelope, config.Config.BufferSize)

	bus := &EventBusImpl{
		name:            "event-bus",
		brokers:         make(map[string]MessageBroker),
		store:           config.Store,
		handlerRegistry: config.HandlerRegistry,
		config:          config.Config,
		logger:          config.Logger,
		metrics:         config.Metrics,
		eventQueue:      eventQueue,
		workers:         make([]*EventWorker, 0),
	}

	// Create workers
	for i := 0; i < config.Config.WorkerCount; i++ {
		worker := NewEventWorker(i, eventQueue, bus.processEvent, config.Logger, config.Metrics)
		bus.workers = append(bus.workers, worker)
	}

	return bus, nil
}

// Name implements core.Service
func (eb *EventBusImpl) Name() string {
	return eb.name
}

// Dependencies implements core.Service
func (eb *EventBusImpl) Dependencies() []string {
	return []string{"event-store", "handler-registry"}
}

// OnStart implements core.Service
func (eb *EventBusImpl) OnStart(ctx context.Context) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.started {
		return common.ErrServiceAlreadyExists("event-bus")
	}

	if eb.logger != nil {
		eb.logger.Info("starting event bus",
			logger.String("service", eb.name),
			logger.Int("workers", len(eb.workers)),
			logger.Int("buffer_size", eb.config.BufferSize),
		)
	}

	// OnStart all registered brokers
	for name, broker := range eb.brokers {
		if err := broker.Connect(ctx, nil); err != nil {
			return common.ErrServiceStartFailed(eb.name, fmt.Errorf("failed to start broker %s: %w", name, err))
		}

		if eb.logger != nil {
			eb.logger.Info("broker started",
				logger.String("broker", name),
			)
		}
	}

	// OnStart workers
	for _, worker := range eb.workers {
		eb.wg.Add(1)
		go func(w *EventWorker) {
			defer eb.wg.Done()
			w.Start(ctx)
		}(worker)
	}

	eb.started = true

	if eb.logger != nil {
		eb.logger.Info("event bus started successfully")
	}

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.bus_started").Inc()
	}

	return nil
}

// OnStop implements core.Service
func (eb *EventBusImpl) OnStop(ctx context.Context) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.started {
		return nil
	}

	if eb.logger != nil {
		eb.logger.Info("stopping event bus")
	}

	eb.stopping = true

	// Close event queue to signal workers to stop
	close(eb.eventQueue)

	// Wait for workers to finish processing
	eb.wg.Wait()

	// OnStop all brokers
	for name, broker := range eb.brokers {
		if err := broker.Close(ctx); err != nil {
			if eb.logger != nil {
				eb.logger.Error("failed to stop broker",
					logger.String("broker", name),
					logger.Error(err),
				)
			}
		}
	}

	eb.started = false
	eb.stopping = false

	if eb.logger != nil {
		eb.logger.Info("event bus stopped")
	}

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.bus_stopped").Inc()
	}

	return nil
}

// OnHealthCheck implements core.Service
func (eb *EventBusImpl) OnHealthCheck(ctx context.Context) error {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if !eb.started {
		return common.ErrHealthCheckFailed("event-bus", fmt.Errorf("service not started"))
	}

	// Check all brokers
	for name, broker := range eb.brokers {
		if err := broker.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("event-bus", fmt.Errorf("broker %s unhealthy: %w", name, err))
		}
	}

	// Check if workers are alive (simplified check)
	if eb.stopping {
		return common.ErrHealthCheckFailed("event-bus", fmt.Errorf("service is stopping"))
	}

	return nil
}

// Publish implements EventBus
func (eb *EventBusImpl) Publish(ctx context.Context, event *Event) error {
	if !eb.started {
		return fmt.Errorf("event bus not started")
	}

	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	start := time.Now()

	// Save event to store first
	if eb.store != nil {
		if err := eb.store.SaveEvent(ctx, event); err != nil {
			if eb.logger != nil {
				eb.logger.Error("failed to save event to store",
					logger.String("event_id", event.ID),
					logger.String("event_type", event.Type),
					logger.Error(err),
				)
			}
			if eb.metrics != nil {
				eb.metrics.Counter("forge.events.publish_store_errors").Inc()
			}
			return fmt.Errorf("failed to save event: %w", err)
		}
	}

	// Publish to default broker or all brokers
	if eb.defaultBroker != "" {
		return eb.PublishTo(ctx, eb.defaultBroker, event)
	}

	// Publish to all brokers
	var lastErr error
	published := false

	eb.mu.RLock()
	brokers := make(map[string]MessageBroker)
	for name, broker := range eb.brokers {
		brokers[name] = broker
	}
	eb.mu.RUnlock()

	for name, broker := range brokers {
		if err := broker.Publish(ctx, event.Type, *event); err != nil {
			lastErr = err
			if eb.logger != nil {
				eb.logger.Error("failed to publish to broker",
					logger.String("broker", name),
					logger.String("event_id", event.ID),
					logger.String("event_type", event.Type),
					logger.Error(err),
				)
			}
			if eb.metrics != nil {
				eb.metrics.Counter("forge.events.publish_broker_errors", "broker", name).Inc()
			}
		} else {
			published = true
		}
	}

	// Record metrics
	if eb.metrics != nil {
		duration := time.Since(start)
		eb.metrics.Histogram("forge.events.publish_duration").Observe(duration.Seconds())
		eb.metrics.Counter("forge.events.published_total", "event_type", event.Type).Inc()

		if published {
			eb.metrics.Counter("forge.events.publish_success").Inc()
		} else {
			eb.metrics.Counter("forge.events.publish_failures").Inc()
		}
	}

	if !published && lastErr != nil {
		return fmt.Errorf("failed to publish to any broker: %w", lastErr)
	}

	return nil
}

// PublishTo implements EventBus
func (eb *EventBusImpl) PublishTo(ctx context.Context, brokerName string, event *Event) error {
	if !eb.started {
		return fmt.Errorf("event bus not started")
	}

	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	eb.mu.RLock()
	broker, exists := eb.brokers[brokerName]
	eb.mu.RUnlock()

	if !exists {
		return fmt.Errorf("broker %s not found", brokerName)
	}

	start := time.Now()

	if err := broker.Publish(ctx, event.Type, *event); err != nil {
		if eb.metrics != nil {
			eb.metrics.Counter("forge.events.publish_broker_errors", "broker", brokerName).Inc()
		}
		return fmt.Errorf("failed to publish to broker %s: %w", brokerName, err)
	}

	// Record metrics
	if eb.metrics != nil {
		duration := time.Since(start)
		eb.metrics.Histogram("forge.events.publish_duration", "broker", brokerName).Observe(duration.Seconds())
		eb.metrics.Counter("forge.events.published_total", "broker", brokerName, "event_type", event.Type).Inc()
		eb.metrics.Counter("forge.events.publish_success").Inc()
	}

	if eb.logger != nil {
		eb.logger.Debug("event published to broker",
			logger.String("broker", brokerName),
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
		)
	}

	return nil
}

// Subscribe implements EventBus
func (eb *EventBusImpl) Subscribe(eventType string, handler EventHandler) error {
	if !eb.started {
		return fmt.Errorf("event bus not started")
	}

	// Register handler in the registry
	if err := eb.handlerRegistry.Register(eventType, handler); err != nil {
		return fmt.Errorf("failed to register handler: %w", err)
	}

	// Subscribe to all brokers
	eb.mu.RLock()
	brokers := make(map[string]MessageBroker)
	for name, broker := range eb.brokers {
		brokers[name] = broker
	}
	eb.mu.RUnlock()

	var lastErr error
	subscribed := false

	for name, broker := range brokers {
		if err := broker.Subscribe(context.Background(), eventType, handler); err != nil {
			lastErr = err
			if eb.logger != nil {
				eb.logger.Error("failed to subscribe to broker",
					logger.String("broker", name),
					logger.String("event_type", eventType),
					logger.String("handler", handler.Name()),
					logger.Error(err),
				)
			}
		} else {
			subscribed = true
		}
	}

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.subscriptions_total", "event_type", eventType).Inc()
	}

	if eb.logger != nil {
		eb.logger.Info("subscribed to event type",
			logger.String("event_type", eventType),
			logger.String("handler", handler.Name()),
		)
	}

	if !subscribed && lastErr != nil {
		return fmt.Errorf("failed to subscribe to any broker: %w", lastErr)
	}

	return nil
}

// Unsubscribe implements EventBus
func (eb *EventBusImpl) Unsubscribe(eventType string, handlerName string) error {
	if !eb.started {
		return fmt.Errorf("event bus not started")
	}

	// Unregister from handler registry
	if err := eb.handlerRegistry.Unregister(eventType, handlerName); err != nil {
		return fmt.Errorf("failed to unregister handler: %w", err)
	}

	// Unsubscribe from all brokers
	eb.mu.RLock()
	brokers := make(map[string]MessageBroker)
	for name, broker := range eb.brokers {
		brokers[name] = broker
	}
	eb.mu.RUnlock()

	for name, broker := range brokers {
		if err := broker.Unsubscribe(context.Background(), eventType, handlerName); err != nil {
			if eb.logger != nil {
				eb.logger.Error("failed to unsubscribe from broker",
					logger.String("broker", name),
					logger.String("event_type", eventType),
					logger.String("handler", handlerName),
					logger.Error(err),
				)
			}
		}
	}

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.unsubscriptions_total", "event_type", eventType).Inc()
	}

	if eb.logger != nil {
		eb.logger.Info("unsubscribed from event type",
			logger.String("event_type", eventType),
			logger.String("handler", handlerName),
		)
	}

	return nil
}

// RegisterBroker implements EventBus
func (eb *EventBusImpl) RegisterBroker(name string, broker MessageBroker) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if _, exists := eb.brokers[name]; exists {
		return fmt.Errorf("broker %s already registered", name)
	}

	eb.brokers[name] = broker

	if eb.logger != nil {
		eb.logger.Info("broker registered",
			logger.String("broker", name),
		)
	}

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.brokers_registered").Inc()
		eb.metrics.Gauge("forge.events.brokers_total").Set(float64(len(eb.brokers)))
	}

	return nil
}

// UnregisterBroker implements EventBus
func (eb *EventBusImpl) UnregisterBroker(name string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	broker, exists := eb.brokers[name]
	if !exists {
		return fmt.Errorf("broker %s not found", name)
	}

	// Close broker connection
	if err := broker.Close(context.Background()); err != nil {
		if eb.logger != nil {
			eb.logger.Error("failed to close broker during unregistration",
				logger.String("broker", name),
				logger.Error(err),
			)
		}
	}

	delete(eb.brokers, name)

	// Clear default broker if it was this one
	if eb.defaultBroker == name {
		eb.defaultBroker = ""
	}

	if eb.logger != nil {
		eb.logger.Info("broker unregistered",
			logger.String("broker", name),
		)
	}

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.brokers_unregistered").Inc()
		eb.metrics.Gauge("forge.events.brokers_total").Set(float64(len(eb.brokers)))
	}

	return nil
}

// GetBroker implements EventBus
func (eb *EventBusImpl) GetBroker(name string) (MessageBroker, error) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	broker, exists := eb.brokers[name]
	if !exists {
		return nil, fmt.Errorf("broker %s not found", name)
	}

	return broker, nil
}

// GetBrokers implements EventBus
func (eb *EventBusImpl) GetBrokers() map[string]MessageBroker {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	brokers := make(map[string]MessageBroker)
	for name, broker := range eb.brokers {
		brokers[name] = broker
	}

	return brokers
}

// SetDefaultBroker implements EventBus
func (eb *EventBusImpl) SetDefaultBroker(name string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if _, exists := eb.brokers[name]; !exists {
		return fmt.Errorf("broker %s not found", name)
	}

	eb.defaultBroker = name

	if eb.logger != nil {
		eb.logger.Info("default broker set",
			logger.String("broker", name),
		)
	}

	return nil
}

// GetStats implements EventBus
func (eb *EventBusImpl) GetStats() map[string]interface{} {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	stats := map[string]interface{}{
		"name":           eb.name,
		"started":        eb.started,
		"stopping":       eb.stopping,
		"brokers_count":  len(eb.brokers),
		"default_broker": eb.defaultBroker,
		"workers_count":  len(eb.workers),
		"buffer_size":    eb.config.BufferSize,
	}

	// Add broker stats
	brokerStats := make(map[string]interface{})
	for name, broker := range eb.brokers {
		brokerStats[name] = broker.GetStats()
	}
	stats["brokers"] = brokerStats

	// Add handler registry stats
	if eb.handlerRegistry != nil {
		stats["handlers"] = eb.handlerRegistry.Stats()
	}

	// Add worker stats
	workerStats := make([]map[string]interface{}, 0, len(eb.workers))
	for _, worker := range eb.workers {
		workerStats = append(workerStats, worker.GetStats())
	}
	stats["workers"] = workerStats

	return stats
}

// processEvent processes an event from the queue
func (eb *EventBusImpl) processEvent(ctx context.Context, envelope *EventEnvelope) error {
	start := time.Now()

	// Handle the event using registered handlers
	if err := eb.handlerRegistry.HandleEvent(ctx, envelope.Event); err != nil {
		if eb.logger != nil {
			eb.logger.Error("failed to process event",
				logger.String("event_id", envelope.Event.ID),
				logger.String("event_type", envelope.Event.Type),
				logger.Error(err),
			)
		}

		if eb.metrics != nil {
			eb.metrics.Counter("forge.events.processing_errors", "event_type", envelope.Event.Type).Inc()
		}

		return err
	}

	// Record metrics
	if eb.metrics != nil {
		duration := time.Since(start)
		eb.metrics.Histogram("forge.events.processing_duration", "event_type", envelope.Event.Type).Observe(duration.Seconds())
		eb.metrics.Counter("forge.events.processed_total", "event_type", envelope.Event.Type).Inc()
	}

	if eb.logger != nil {
		eb.logger.Debug("event processed successfully",
			logger.String("event_id", envelope.Event.ID),
			logger.String("event_type", envelope.Event.Type),
			logger.Duration("duration", time.Since(start)),
		)
	}

	return nil
}

// EventWorker processes events from the queue
type EventWorker struct {
	id         int
	eventQueue <-chan *EventEnvelope
	processor  func(context.Context, *EventEnvelope) error
	logger     common.Logger
	metrics    common.Metrics
	stats      *WorkerStats
	mu         sync.RWMutex
}

// WorkerStats contains worker statistics
type WorkerStats struct {
	ID                    int           `json:"id"`
	EventsProcessed       int64         `json:"events_processed"`
	ErrorsEncountered     int64         `json:"errors_encountered"`
	TotalProcessingTime   time.Duration `json:"total_processing_time"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
	LastEventTime         *time.Time    `json:"last_event_time,omitempty"`
	IsRunning             bool          `json:"is_running"`
}

// NewEventWorker creates a new event worker
func NewEventWorker(id int, eventQueue <-chan *EventEnvelope, processor func(context.Context, *EventEnvelope) error, logger common.Logger, metrics common.Metrics) *EventWorker {
	return &EventWorker{
		id:         id,
		eventQueue: eventQueue,
		processor:  processor,
		logger:     logger,
		metrics:    metrics,
		stats: &WorkerStats{
			ID:        id,
			IsRunning: false,
		},
	}
}

// Start starts the worker
func (ew *EventWorker) Start(ctx context.Context) {
	ew.mu.Lock()
	ew.stats.IsRunning = true
	ew.mu.Unlock()

	if ew.logger != nil {
		ew.logger.Info("event worker started",
			logger.Int("worker_id", ew.id),
		)
	}

	for {
		select {
		case <-ctx.Done():
			ew.mu.Lock()
			ew.stats.IsRunning = false
			ew.mu.Unlock()
			return
		case envelope, ok := <-ew.eventQueue:
			if !ok {
				// Channel closed, worker should stop
				ew.mu.Lock()
				ew.stats.IsRunning = false
				ew.mu.Unlock()
				return
			}

			ew.processEvent(ctx, envelope)
		}
	}
}

// processEvent processes a single event
func (ew *EventWorker) processEvent(ctx context.Context, envelope *EventEnvelope) {
	start := time.Now()

	ew.mu.Lock()
	now := start
	ew.stats.LastEventTime = &now
	ew.mu.Unlock()

	err := ew.processor(ctx, envelope)

	duration := time.Since(start)

	ew.mu.Lock()
	ew.stats.EventsProcessed++
	ew.stats.TotalProcessingTime += duration
	ew.stats.AverageProcessingTime = ew.stats.TotalProcessingTime / time.Duration(ew.stats.EventsProcessed)

	if err != nil {
		ew.stats.ErrorsEncountered++
	}
	ew.mu.Unlock()

	if ew.metrics != nil {
		ew.metrics.Counter("forge.events.worker_events_processed", "worker_id", fmt.Sprintf("%d", ew.id)).Inc()
		ew.metrics.Histogram("forge.events.worker_processing_duration", "worker_id", fmt.Sprintf("%d", ew.id)).Observe(duration.Seconds())

		if err != nil {
			ew.metrics.Counter("forge.events.worker_errors", "worker_id", fmt.Sprintf("%d", ew.id)).Inc()
		}
	}

	if err != nil && ew.logger != nil {
		ew.logger.Error("worker failed to process event",
			logger.Int("worker_id", ew.id),
			logger.String("event_id", envelope.Event.ID),
			logger.String("event_type", envelope.Event.Type),
			logger.Error(err),
		)
	}
}

// GetStats returns worker statistics
func (ew *EventWorker) GetStats() map[string]interface{} {
	ew.mu.RLock()
	defer ew.mu.RUnlock()

	return map[string]interface{}{
		"id":                      ew.stats.ID,
		"events_processed":        ew.stats.EventsProcessed,
		"errors_encountered":      ew.stats.ErrorsEncountered,
		"total_processing_time":   ew.stats.TotalProcessingTime.String(),
		"average_processing_time": ew.stats.AverageProcessingTime.String(),
		"last_event_time":         ew.stats.LastEventTime,
		"is_running":              ew.stats.IsRunning,
	}
}
