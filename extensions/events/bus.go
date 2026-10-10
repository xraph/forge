package events

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/events/core"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/go-utils/metrics"
)

// EventBusConfig defines configuration for the event bus.
type EventBusConfig struct {
	DefaultBroker     string        `json:"default_broker"     yaml:"default_broker"`
	MaxRetries        int           `json:"max_retries"        yaml:"max_retries"`
	RetryDelay        time.Duration `json:"retry_delay"        yaml:"retry_delay"`
	EnableMetrics     bool          `json:"enable_metrics"     yaml:"enable_metrics"`
	EnableTracing     bool          `json:"enable_tracing"     yaml:"enable_tracing"`
	BufferSize        int           `json:"buffer_size"        yaml:"buffer_size"`
	WorkerCount       int           `json:"worker_count"       yaml:"worker_count"`
	ProcessingTimeout time.Duration `json:"processing_timeout" yaml:"processing_timeout"`
}

// EventBusImpl implements EventBus.
type EventBusImpl struct {
	name            string
	brokers         map[string]core.MessageBroker
	defaultBroker   string
	store           core.EventStore
	handlerRegistry *core.HandlerRegistry
	config          EventBusConfig
	logger          forge.Logger
	metrics         forge.Metrics
	workers         []*EventWorker
	eventQueue      chan *core.EventEnvelope
	started         bool
	starting        bool
	runCtx          context.Context //nolint:containedctx // A bus run owns broker and worker cancellation.
	cancel          context.CancelFunc
	shutdown        *busShutdown
	active          sync.WaitGroup
	lifecycleMu     sync.Mutex
	subscriptionMu  sync.Mutex
	pendingRemovals map[subscriptionKey]map[string]bool
	stopping        bool
	mu              sync.RWMutex
	wg              sync.WaitGroup
}

type subscriptionKey struct {
	eventType   string
	handlerName string
}

type busShutdown struct {
	done chan struct{}
	err  error
}

// nilDependency also rejects interfaces wrapping a nil pointer.
func nilDependency(value any) bool {
	if value == nil {
		return true
	}

	switch reflect.ValueOf(value).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	default:
		return false
	}
}

// EventBusOptions defines configuration for EventBusImpl.
type EventBusOptions struct {
	Store           core.EventStore
	HandlerRegistry *core.HandlerRegistry
	Logger          forge.Logger
	Metrics         forge.Metrics
	Config          EventBusConfig
}

// NewEventBus creates a new event bus.
func NewEventBus(config EventBusOptions) (core.EventBus, error) {
	if nilDependency(config.Store) {
		return nil, errors.New("event store is required")
	}

	if config.HandlerRegistry == nil {
		return nil, errors.New("handler registry is required")
	}

	if config.Config.BufferSize < 0 || config.Config.WorkerCount < 0 {
		return nil, errors.New("buffer size and worker count cannot be negative")
	}

	eventQueue := make(chan *core.EventEnvelope, config.Config.BufferSize)

	bus := &EventBusImpl{
		name:            "event-bus",
		defaultBroker:   config.Config.DefaultBroker,
		brokers:         make(map[string]core.MessageBroker),
		store:           config.Store,
		handlerRegistry: config.HandlerRegistry,
		config:          config.Config,
		logger:          config.Logger,
		metrics:         config.Metrics,
		eventQueue:      eventQueue,
		workers:         make([]*EventWorker, 0),
	}

	// Create workers
	for i := range config.Config.WorkerCount {
		worker := NewEventWorker(i, eventQueue, bus.processEvent, config.Logger, config.Metrics)
		bus.workers = append(bus.workers, worker)
	}

	return bus, nil
}

// Name implements core.Service.
func (eb *EventBusImpl) Name() string {
	return eb.name
}

// Dependencies implements core.Service.
func (eb *EventBusImpl) Dependencies() []string {
	return []string{"event-store", "handler-registry"}
}

// Start connects brokers and restores subscriptions.
// This method is idempotent - calling it multiple times is safe.
func (eb *EventBusImpl) Start(ctx context.Context) error {
	eb.lifecycleMu.Lock()
	defer eb.lifecycleMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	eb.mu.Lock()
	if eb.stopping {
		eb.mu.Unlock()

		return errors.New("event bus is stopping")
	}

	if eb.started {
		eb.mu.Unlock()

		return nil
	}

	if len(eb.brokers) == 0 {
		eb.mu.Unlock()

		return errors.New("event bus requires at least one broker")
	}

	if eb.defaultBroker != "" && eb.brokers[eb.defaultBroker] == nil {
		eb.mu.Unlock()

		return fmt.Errorf("default broker %s not found", eb.defaultBroker)
	}

	eb.starting = true
	brokers := make(map[string]core.MessageBroker)
	maps.Copy(brokers, eb.brokers)
	eb.mu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)

	var connected []string

	fail := func(err error) error {
		cancel()

		for _, v := range slices.Backward(connected) {
			err = stderrors.Join(err, brokers[v].Close(context.WithoutCancel(ctx)))
		}

		eb.mu.Lock()
		eb.starting = false
		eb.mu.Unlock()

		return err
	}

	names := brokerNames(brokers)
	for _, name := range names {
		// A failed connection may still have allocated broker resources.
		connected = append(connected, name)
		if err := brokers[name].Connect(runCtx, nil); err != nil {
			return fail(fmt.Errorf("failed to start broker %s: %w", name, err))
		}
	}

	for topic, handlers := range eb.handlerRegistry.GetAllHandlers() {
		for _, handler := range handlers {
			eb.mu.RLock()
			_, removing := eb.pendingRemovals[subscriptionKey{topic, handler.Name()}]
			eb.mu.RUnlock()

			if removing {
				continue
			}

			for _, name := range names {
				if err := brokers[name].Subscribe(runCtx, topic, handler); err != nil {
					return fail(fmt.Errorf("failed to restore subscription on broker %s: %w", name, err))
				}
			}
		}
	}

	if err := runCtx.Err(); err != nil {
		return fail(err)
	}

	eb.mu.Lock()
	eb.runCtx, eb.cancel = runCtx, cancel
	eb.eventQueue = make(chan *core.EventEnvelope, eb.config.BufferSize)

	eb.workers = make([]*EventWorker, 0, eb.config.WorkerCount)
	for i := range eb.config.WorkerCount {
		worker := NewEventWorker(i, eb.eventQueue, eb.processEvent, eb.logger, eb.metrics)
		eb.workers = append(eb.workers, worker)

		eb.wg.Go(func() { worker.Start(runCtx) })
	}

	eb.started, eb.starting = true, false
	eb.mu.Unlock()

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.bus_started").Inc()
	}

	return nil
}

// Stop cancels admitted operations and waits for broker shutdown.
func (eb *EventBusImpl) Stop(ctx context.Context) error {
	eb.lifecycleMu.Lock()
	eb.mu.Lock()
	if !eb.started && !eb.stopping {
		var stopErr error
		if eb.shutdown != nil {
			stopErr = eb.shutdown.err
		}
		eb.mu.Unlock()
		eb.lifecycleMu.Unlock()

		return stopErr
	}

	if !eb.stopping {
		eb.stopping = true
		eb.shutdown = &busShutdown{done: make(chan struct{})}

		eb.cancel()
		go eb.finishStop(context.WithoutCancel(ctx))
	}

	shutdown := eb.shutdown
	eb.mu.Unlock()
	eb.lifecycleMu.Unlock()

	select {
	case <-shutdown.done:
		return shutdown.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (eb *EventBusImpl) finishStop(ctx context.Context) {
	// Admission is closed before Wait, so no operation can race Add with Wait.
	eb.active.Wait()
	eb.wg.Wait()
	eb.mu.RLock()

	brokers := make(map[string]core.MessageBroker)
	maps.Copy(brokers, eb.brokers)
	eb.mu.RUnlock()

	var stopErr error

	for _, name := range brokerNames(brokers) {
		if err := brokers[name].Close(ctx); err != nil {
			stopErr = stderrors.Join(stopErr, fmt.Errorf("failed to close broker %s: %w", name, err))
		} else {
			stopErr = stderrors.Join(stopErr, eb.finishBrokerRemovals(name))
		}
	}

	eb.mu.Lock()
	eb.started, eb.stopping = false, false
	eb.shutdown.err = stopErr
	close(eb.shutdown.done)
	eb.mu.Unlock()

	if eb.metrics != nil {
		eb.metrics.Counter("forge.events.bus_stopped").Inc()
	}
}

func brokerNames(brokers map[string]core.MessageBroker) []string {
	names := make([]string, 0, len(brokers))
	for name := range brokers {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// beginOperation admits work before shutdown and captures a consistent route set.
func (eb *EventBusImpl) beginOperation(ctx context.Context) (context.Context, func(), map[string]core.MessageBroker, string, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.started || eb.stopping {
		return nil, nil, nil, "", errors.New("event bus not running")
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, nil, "", err
	}

	if err := eb.runCtx.Err(); err != nil {
		return nil, nil, nil, "", err
	}

	opCtx, cancel := context.WithCancel(ctx)
	unlink := context.AfterFunc(eb.runCtx, cancel)
	eb.active.Add(1)

	brokers := make(map[string]core.MessageBroker)
	maps.Copy(brokers, eb.brokers)

	done := func() { unlink(); cancel(); eb.active.Done() }

	return opCtx, done, brokers, eb.defaultBroker, nil
}

// HealthCheck implements core.EventBus.
func (eb *EventBusImpl) HealthCheck(ctx context.Context) error {
	opCtx, done, brokers, _, err := eb.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer done()

	eb.mu.RLock()
	pending := len(eb.pendingRemovals)
	eb.mu.RUnlock()

	if pending > 0 {
		return fmt.Errorf("%d subscription removals remain incomplete", pending)
	}

	for _, name := range brokerNames(brokers) {
		if err := brokers[name].HealthCheck(opCtx); err != nil {
			return fmt.Errorf("broker %s unhealthy: %w", name, err)
		}
	}

	return nil
}

// Publish implements EventBus.
func (eb *EventBusImpl) Publish(ctx context.Context, event *core.Event) error {
	opCtx, done, brokers, defaultBroker, err := eb.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer done()

	if event == nil {
		return errors.New("event is required")
	}

	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	if defaultBroker != "" {
		broker, exists := brokers[defaultBroker]
		if !exists {
			return fmt.Errorf("default broker %s not found", defaultBroker)
		}

		brokers = map[string]core.MessageBroker{defaultBroker: broker}
	}

	if len(brokers) == 0 {
		return errors.New("no brokers available for publishing")
	}

	if err := eb.store.SaveEvent(opCtx, event); err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

	return eb.publishToBrokers(opCtx, brokers, event)
}

// PublishTo implements EventBus.
func (eb *EventBusImpl) PublishTo(ctx context.Context, brokerName string, event *core.Event) error {
	opCtx, done, brokers, _, err := eb.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer done()

	if event == nil {
		return errors.New("event is required")
	}

	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	broker, exists := brokers[brokerName]
	if !exists {
		return fmt.Errorf("broker %s not found", brokerName)
	}

	return eb.publishToBrokers(opCtx, map[string]core.MessageBroker{brokerName: broker}, event)
}

func (eb *EventBusImpl) publishToBrokers(ctx context.Context, brokers map[string]core.MessageBroker, event *core.Event) error {
	start := time.Now()

	var publishErr error

	for _, name := range brokerNames(brokers) {
		err := ctx.Err()
		if err == nil {
			err = brokers[name].Publish(ctx, event.Type, *event)
		}

		if err != nil {
			publishErr = stderrors.Join(publishErr, fmt.Errorf("failed to publish to broker %s: %w", name, err))
			if eb.metrics != nil {
				eb.metrics.Counter("forge.events.publish_broker_errors", metrics.WithLabel("broker", name)).Inc()
			}
		}
	}

	if eb.metrics != nil {
		eb.metrics.Histogram("forge.events.publish_duration").Observe(time.Since(start).Seconds())

		if publishErr == nil {
			eb.metrics.Counter("forge.events.published_total", metrics.WithLabel("event_type", event.Type)).Inc()
			eb.metrics.Counter("forge.events.publish_success").Inc()
		} else {
			eb.metrics.Counter("forge.events.publish_failures").Inc()
		}
	}

	return publishErr
}

// Subscribe implements EventBus.
func (eb *EventBusImpl) Subscribe(eventType string, handler core.EventHandler) error {
	_, done, brokers, _, err := eb.beginOperation(context.Background())
	if err != nil {
		return err
	}
	defer done()

	if nilDependency(handler) {
		return errors.New("handler is required")
	}

	if len(brokers) == 0 {
		return errors.New("no brokers available for subscribing")
	}

	eb.subscriptionMu.Lock()
	defer eb.subscriptionMu.Unlock()

	for _, registered := range eb.handlerRegistry.GetHandlers(eventType) {
		if handler.Name() != "anonymous-handler" && registered.Name() == handler.Name() {
			return fmt.Errorf("handler %s already subscribed to %s", handler.Name(), eventType)
		}
	}

	eb.mu.RLock()
	runCtx := eb.runCtx
	eb.mu.RUnlock()

	var subscribed []string

	for _, name := range brokerNames(brokers) {
		if err := brokers[name].Subscribe(runCtx, eventType, handler); err != nil {
			result := fmt.Errorf("failed to subscribe to broker %s: %w", name, err)
			for _, previous := range subscribed {
				result = stderrors.Join(result, brokers[previous].Unsubscribe(context.WithoutCancel(runCtx), eventType, handler.Name()))
			}

			return result
		}

		subscribed = append(subscribed, name)
	}

	return eb.handlerRegistry.Register(eventType, handler)
}

// Unsubscribe implements EventBus.
func (eb *EventBusImpl) Unsubscribe(eventType string, handlerName string) error {
	opCtx, done, brokers, _, err := eb.beginOperation(context.Background())
	if err != nil {
		return err
	}
	defer done()

	eb.subscriptionMu.Lock()
	defer eb.subscriptionMu.Unlock()

	registered := false

	for _, handler := range eb.handlerRegistry.GetHandlers(eventType) {
		if handler.Name() == handlerName {
			registered = true

			break
		}
	}

	if !registered {
		return fmt.Errorf("handler %s is not registered for %s", handlerName, eventType)
	}

	key := subscriptionKey{eventType, handlerName}

	eb.mu.Lock()
	if eb.pendingRemovals == nil {
		eb.pendingRemovals = make(map[subscriptionKey]map[string]bool)
	}

	pending, exists := eb.pendingRemovals[key]
	if !exists {
		pending = make(map[string]bool, len(brokers))
		for name := range brokers {
			pending[name] = true
		}

		eb.pendingRemovals[key] = pending
	}

	names := make([]string, 0, len(pending))
	for name := range pending {
		names = append(names, name)
	}
	eb.mu.Unlock()
	sort.Strings(names)

	var result error

	for _, name := range names {
		if err := brokers[name].Unsubscribe(opCtx, eventType, handlerName); err != nil {
			result = stderrors.Join(result, fmt.Errorf("failed to unsubscribe from broker %s: %w", name, err))
		} else {
			eb.mu.Lock()
			delete(pending, name)
			eb.mu.Unlock()
		}
	}

	if result != nil {
		return result
	}

	if err := eb.handlerRegistry.Unregister(eventType, handlerName); err != nil {
		return err
	}

	eb.mu.Lock()
	delete(eb.pendingRemovals, key)
	eb.mu.Unlock()

	return nil
}

// finishBrokerRemovals completes pending intent after a successful broker Close.
// Shutdown has drained all subscription operations before calling this helper.
func (eb *EventBusImpl) finishBrokerRemovals(brokerName string) error {
	eb.mu.Lock()

	var complete []subscriptionKey

	for key, pending := range eb.pendingRemovals {
		delete(pending, brokerName)

		if len(pending) == 0 {
			complete = append(complete, key)
		}
	}
	eb.mu.Unlock()

	var result error

	for _, key := range complete {
		if err := eb.handlerRegistry.Unregister(key.eventType, key.handlerName); err != nil {
			result = stderrors.Join(result, err)

			continue
		}

		eb.mu.Lock()
		delete(eb.pendingRemovals, key)
		eb.mu.Unlock()
	}

	return result
}

// RegisterBroker implements EventBus.
func (eb *EventBusImpl) RegisterBroker(name string, broker core.MessageBroker) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.started || eb.starting || eb.stopping {
		return errors.New("stop the event bus before changing brokers")
	}

	if name == "" || nilDependency(broker) {
		return errors.New("broker name and instance are required")
	}

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

// UnregisterBroker implements EventBus.
func (eb *EventBusImpl) UnregisterBroker(name string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.started || eb.starting || eb.stopping {
		return errors.New("stop the event bus before changing brokers")
	}

	if _, exists := eb.brokers[name]; !exists {
		return fmt.Errorf("broker %s not found", name)
	}

	for _, pending := range eb.pendingRemovals {
		if pending[name] {
			return fmt.Errorf("broker %s has incomplete subscription removals", name)
		}
	}

	delete(eb.brokers, name)

	if eb.defaultBroker == name {
		eb.defaultBroker = ""
	}

	return nil
}

// GetBroker implements EventBus.
func (eb *EventBusImpl) GetBroker(name string) (core.MessageBroker, error) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	broker, exists := eb.brokers[name]
	if !exists {
		return nil, fmt.Errorf("broker %s not found", name)
	}

	return broker, nil
}

// GetBrokers implements EventBus.
func (eb *EventBusImpl) GetBrokers() map[string]core.MessageBroker {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	brokers := make(map[string]core.MessageBroker)
	maps.Copy(brokers, eb.brokers)

	return brokers
}

// SetDefaultBroker implements EventBus.
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

// GetStats implements EventBus.
func (eb *EventBusImpl) GetStats() map[string]any {
	eb.mu.RLock()
	stats := map[string]any{
		"name": eb.name, "started": eb.started, "stopping": eb.stopping,
		"brokers_count": len(eb.brokers), "default_broker": eb.defaultBroker,
		"workers_count": len(eb.workers), "buffer_size": eb.config.BufferSize,
		"pending_subscription_removals": len(eb.pendingRemovals),
	}
	brokers := make(map[string]core.MessageBroker)
	maps.Copy(brokers, eb.brokers)
	workers := append([]*EventWorker(nil), eb.workers...)
	eb.mu.RUnlock()

	brokerStats := make(map[string]any)
	for name, broker := range brokers {
		brokerStats[name] = broker.GetStats()
	}

	stats["brokers"] = brokerStats
	stats["handlers"] = eb.handlerRegistry.Stats()

	workerStats := make([]map[string]any, 0, len(workers))
	for _, worker := range workers {
		workerStats = append(workerStats, worker.GetStats())
	}

	stats["workers"] = workerStats

	return stats
}

// processEvent processes an event from the queue.
func (eb *EventBusImpl) processEvent(ctx context.Context, envelope *core.EventEnvelope) error {
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
			eb.metrics.Counter("forge.events.processing_errors", metrics.WithLabel("event_type", envelope.Event.Type)).Inc()
		}

		return err
	}

	// Record metrics
	if eb.metrics != nil {
		duration := time.Since(start)
		eb.metrics.Histogram("forge.events.processing_duration", metrics.WithLabel("event_type", envelope.Event.Type)).Observe(duration.Seconds())
		eb.metrics.Counter("forge.events.processed_total", metrics.WithLabel("event_type", envelope.Event.Type)).Inc()
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

// EventWorker processes events from the queue.
type EventWorker struct {
	id         int
	eventQueue <-chan *core.EventEnvelope
	processor  func(context.Context, *core.EventEnvelope) error
	logger     forge.Logger
	metrics    forge.Metrics
	stats      *WorkerStats
	mu         sync.RWMutex
}

// WorkerStats contains worker statistics.
type WorkerStats struct {
	ID                    int           `json:"id"`
	EventsProcessed       int64         `json:"events_processed"`
	ErrorsEncountered     int64         `json:"errors_encountered"`
	TotalProcessingTime   time.Duration `json:"total_processing_time"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
	LastEventTime         *time.Time    `json:"last_event_time,omitempty"`
	IsRunning             bool          `json:"is_running"`
}

// NewEventWorker creates a new event worker.
func NewEventWorker(id int, eventQueue <-chan *core.EventEnvelope, processor func(context.Context, *core.EventEnvelope) error, logger forge.Logger, metrics forge.Metrics) *EventWorker {
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

// Start starts the worker.
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

// processEvent processes a single event.
func (ew *EventWorker) processEvent(ctx context.Context, envelope *core.EventEnvelope) {
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
		ew.metrics.Counter("forge.events.worker_events_processed", metrics.WithLabel("worker_id", strconv.Itoa(ew.id))).Inc()
		ew.metrics.Histogram("forge.events.worker_processing_duration", metrics.WithLabel("worker_id", strconv.Itoa(ew.id))).Observe(duration.Seconds())

		if err != nil {
			ew.metrics.Counter("forge.events.worker_errors", metrics.WithLabel("worker_id", strconv.Itoa(ew.id))).Inc()
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

// GetStats returns worker statistics.
func (ew *EventWorker) GetStats() map[string]any {
	ew.mu.RLock()
	defer ew.mu.RUnlock()

	return map[string]any{
		"id":                      ew.stats.ID,
		"events_processed":        ew.stats.EventsProcessed,
		"errors_encountered":      ew.stats.ErrorsEncountered,
		"total_processing_time":   ew.stats.TotalProcessingTime.String(),
		"average_processing_time": ew.stats.AverageProcessingTime.String(),
		"last_event_time":         ew.stats.LastEventTime,
		"is_running":              ew.stats.IsRunning,
	}
}
