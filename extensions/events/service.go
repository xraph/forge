package events

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/events/brokers"
	"github.com/xraph/forge/extensions/events/core"
	"github.com/xraph/forge/extensions/events/stores"
)

// EventService provides event-driven architecture capabilities.
type EventService struct {
	storeProvided   bool
	factoryProvided bool
	storeFactory    EventStoreFactory
	injectedStore   core.EventStore
	ownsStore       bool
	lifecycleMu     sync.Mutex
	config          Config
	bus             core.EventBus
	store           core.EventStore
	handlerRegistry *core.HandlerRegistry
	logger          forge.Logger
	metrics         forge.Metrics
	started         bool
	stopping        bool
	mu              sync.RWMutex
}

// EventStoreFactory constructs a store owned by the event service. It is called
// again after a completed shutdown so each run receives a usable store.
type EventStoreFactory func(context.Context, StoreConfig) (core.EventStore, error)

// EventServiceOption configures event service dependencies.
type EventServiceOption func(*EventService)

// WithEventStore borrows a store owned by the caller. The service checks its
// health on startup but never closes it, including on failed startup.
func WithEventStore(store core.EventStore) EventServiceOption {
	return func(service *EventService) {
		service.storeProvided, service.factoryProvided = true, false
		service.injectedStore = store
		service.storeFactory = nil
	}
}

// WithEventStoreFactory constructs an owned store for each service run.
func WithEventStoreFactory(factory EventStoreFactory) EventServiceOption {
	return func(service *EventService) {
		service.factoryProvided, service.storeProvided = true, false
		service.storeFactory = factory
		service.injectedStore = nil
	}
}

// NewEventService creates a new event service.
func NewEventService(config Config, logger forge.Logger, metrics forge.Metrics, options ...EventServiceOption) *EventService {
	service := &EventService{
		config:          config,
		logger:          logger,
		metrics:         metrics,
		handlerRegistry: core.NewHandlerRegistry(logger, metrics),
	}

	for _, option := range options {
		if option != nil {
			option(service)
		}
	}

	return service
}

// Start starts the event service.
// This method is idempotent - calling it multiple times is safe.
func (es *EventService) Start(ctx context.Context) error {
	es.lifecycleMu.Lock()
	defer es.lifecycleMu.Unlock()

	es.mu.RLock()
	started, stopping, existingBus := es.started, es.stopping, es.bus
	es.mu.RUnlock()

	if stopping {
		return errors.New("event service is stopping")
	}

	if started {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := es.config.Validate(); err != nil {
		return err
	}
	// Build dependencies without holding the service lock across broker callbacks.
	candidate := &EventService{
		config: es.config, logger: es.logger, metrics: es.metrics,
		handlerRegistry: es.handlerRegistry, injectedStore: es.injectedStore,
		storeFactory: es.storeFactory, storeProvided: es.storeProvided, factoryProvided: es.factoryProvided,
	}
	if err := candidate.initializeEventStore(ctx); err != nil {
		return fmt.Errorf("failed to initialize event store: %w", err)
	}

	var startErr error

	if existingBus != nil {
		bus, ok := existingBus.(*EventBusImpl)
		if !ok {
			startErr = errors.New("unexpected bus type")
		} else {
			bus.mu.Lock()
			if bus.started || bus.stopping {
				startErr = errors.New("event bus has not completed shutdown")
			} else {
				bus.store = candidate.store
			}
			bus.mu.Unlock()

			if startErr == nil {
				startErr = bus.Start(ctx)
				candidate.bus = bus
			}
		}
	} else {
		startErr = candidate.initializeEventBus(ctx)
	}

	if startErr != nil {
		if candidate.ownsStore {
			startErr = stderrors.Join(startErr, candidate.store.Close(context.WithoutCancel(ctx)))
		}

		return fmt.Errorf("failed to initialize event bus: %w", startErr)
	}

	es.mu.Lock()
	es.bus, es.store, es.ownsStore, es.started = candidate.bus, candidate.store, candidate.ownsStore, true
	es.mu.Unlock()

	if es.metrics != nil {
		es.metrics.Counter("forge.events.service_started").Inc()
	}

	return nil
}

// Stop stops the event service.
func (es *EventService) Stop(ctx context.Context) error {
	es.lifecycleMu.Lock()
	defer es.lifecycleMu.Unlock()

	es.mu.Lock()

	started, bus, store, ownsStore := es.started, es.bus, es.store, es.ownsStore
	if started {
		es.stopping = true
	}
	es.mu.Unlock()

	if !started {
		return nil
	}

	var stopErr error
	if bus != nil {
		stopErr = bus.Stop(ctx)
		// A timed-out shutdown continues in the bus. Keep the store available until
		// a later Stop confirms that all admitted operations have finished.
		if ctx.Err() != nil {
			return stderrors.Join(stopErr, ctx.Err())
		}
	}

	if store != nil && ownsStore {
		stopErr = stderrors.Join(stopErr, store.Close(ctx))
	}

	es.mu.Lock()

	es.started, es.stopping = false, false
	if ownsStore {
		es.store = nil
	}
	es.mu.Unlock()

	if es.metrics != nil {
		es.metrics.Counter("forge.events.service_stopped").Inc()
	}

	return stopErr
}

// HealthCheck checks the health of the event service.
func (es *EventService) HealthCheck(ctx context.Context) error {
	es.mu.RLock()
	started, stopping, bus, store := es.started, es.stopping, es.bus, es.store
	es.mu.RUnlock()

	if !started || stopping {
		return errors.New("event service not running")
	}

	// Check bus health
	if bus != nil {
		if err := bus.HealthCheck(ctx); err != nil {
			return fmt.Errorf("event bus unhealthy: %w", err)
		}
	}

	// Check store health
	if store != nil {
		if err := store.HealthCheck(ctx); err != nil {
			return fmt.Errorf("event store unhealthy: %w", err)
		}
	}

	return nil
}

// GetEventBus returns the event bus.
func (es *EventService) GetEventBus() core.EventBus {
	es.mu.RLock()
	defer es.mu.RUnlock()

	return es.bus
}

// GetEventStore returns the event store.
func (es *EventService) GetEventStore() core.EventStore {
	es.mu.RLock()
	defer es.mu.RUnlock()

	return es.store
}

// GetHandlerRegistry returns the handler registry.
func (es *EventService) GetHandlerRegistry() *core.HandlerRegistry {
	return es.handlerRegistry
}

// initializeEventStore initializes the event store.
func (es *EventService) initializeEventStore(ctx context.Context) error {
	es.ownsStore = false
	switch {
	case es.storeProvided:
		if nilDependency(es.injectedStore) {
			return errors.New("injected event store is nil")
		}

		es.store = es.injectedStore
	case es.factoryProvided:
		if es.storeFactory == nil {
			return errors.New("event store factory is nil")
		}

		store, err := es.storeFactory(ctx, es.config.Store)
		if err != nil {
			return err
		}

		if nilDependency(store) {
			return errors.New("event store factory returned nil")
		}

		es.store, es.ownsStore = store, true
	case es.config.Store.Type == "memory":
		es.store, es.ownsStore = stores.NewMemoryEventStore(es.logger, es.metrics), true
	default:
		return fmt.Errorf("event store type %s requires an injected store or factory", es.config.Store.Type)
	}

	if err := es.store.HealthCheck(ctx); err != nil {
		if es.ownsStore {
			err = stderrors.Join(err, es.store.Close(context.WithoutCancel(ctx)))
			es.store = nil
		}

		return fmt.Errorf("event store unhealthy: %w", err)
	}

	return nil
}

// initializeEventBus initializes the event bus.
func (es *EventService) initializeEventBus(ctx context.Context) error {
	// Convert BusConfig to EventBusConfig
	busConfig := EventBusOptions{
		Store:           es.store,
		HandlerRegistry: es.handlerRegistry,
		Logger:          es.logger,
		Metrics:         es.metrics,
		Config: EventBusConfig{
			DefaultBroker:     es.config.Bus.DefaultBroker,
			MaxRetries:        es.config.Bus.MaxRetries,
			RetryDelay:        es.config.Bus.RetryDelay,
			EnableMetrics:     es.config.Bus.EnableMetrics,
			EnableTracing:     es.config.Bus.EnableTracing,
			BufferSize:        es.config.Bus.BufferSize,
			WorkerCount:       es.config.Bus.WorkerCount,
			ProcessingTimeout: es.config.Bus.ProcessingTimeout,
		},
	}

	busInstance, err := NewEventBus(busConfig)
	if err != nil {
		return fmt.Errorf("failed to create event bus: %w", err)
	}

	bus, ok := busInstance.(*EventBusImpl)
	if !ok {
		return errors.New("unexpected bus type")
	}

	// Set default broker
	bus.defaultBroker = es.config.Bus.DefaultBroker

	// Initialize brokers
	for _, brokerConfig := range es.config.Brokers {
		if !brokerConfig.Enabled {
			continue
		}

		var (
			broker core.MessageBroker
			err    error
		)

		switch brokerConfig.Type {
		case "memory":
			broker = brokers.NewMemoryBroker(es.logger, es.metrics)
		case "nats":
			broker, err = brokers.NewNATSBroker(brokerConfig.Config, es.logger, es.metrics)
			if err != nil {
				return fmt.Errorf("failed to create NATS broker %s: %w", brokerConfig.Name, err)
			}
		case "redis":
			broker, err = brokers.NewRedisBroker(brokerConfig.Config, es.logger, es.metrics)
			if err != nil {
				return fmt.Errorf("failed to create Redis broker %s: %w", brokerConfig.Name, err)
			}
		default:
			return fmt.Errorf("unsupported broker type %s for %s", brokerConfig.Type, brokerConfig.Name)
		}

		// Register broker (will be connected when bus.Start() is called)
		if err := bus.RegisterBroker(brokerConfig.Name, broker); err != nil {
			return fmt.Errorf("failed to register broker %s: %w", brokerConfig.Name, err)
		}

		if es.logger != nil {
			es.logger.Info("broker registered", forge.F("name", brokerConfig.Name), forge.F("type", brokerConfig.Type))
		}
	}

	// Start the bus
	if err := bus.Start(ctx); err != nil {
		return fmt.Errorf("failed to start event bus: %w", err)
	}

	es.bus = bus

	if es.logger != nil {
		es.logger.Info("event bus initialized")
	}

	return nil
}
