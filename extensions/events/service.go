package events

import (
	"context"
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
	config          Config
	bus             core.EventBus
	store           core.EventStore
	handlerRegistry *core.HandlerRegistry
	logger          forge.Logger
	metrics         forge.Metrics
	started         bool
	mu              sync.RWMutex
}

// NewEventService creates a new event service.
func NewEventService(config Config, logger forge.Logger, metrics forge.Metrics) *EventService {
	return &EventService{
		config:          config,
		logger:          logger,
		metrics:         metrics,
		handlerRegistry: core.NewHandlerRegistry(logger, metrics),
	}
}

// Start starts the event service.
// This method is idempotent - calling it multiple times is safe.
func (es *EventService) Start(ctx context.Context) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.started {
		return nil // Idempotent: already started
	}

	if es.logger != nil {
		es.logger.Info("starting event service")
	}

	// Initialize event store
	if err := es.initializeEventStore(ctx); err != nil {
		return fmt.Errorf("failed to initialize event store: %w", err)
	}

	// Initialize event bus
	if err := es.initializeEventBus(ctx); err != nil {
		return fmt.Errorf("failed to initialize event bus: %w", err)
	}

	es.started = true

	if es.logger != nil {
		es.logger.Info("event service started successfully")
	}

	if es.metrics != nil {
		es.metrics.Counter("forge.events.service_started").Inc()
	}

	return nil
}

// Stop stops the event service.
func (es *EventService) Stop(ctx context.Context) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if !es.started {
		return nil
	}

	if es.logger != nil {
		es.logger.Info("stopping event service")
	}

	// Stop bus
	if es.bus != nil {
		if err := es.bus.Stop(ctx); err != nil {
			if es.logger != nil {
				es.logger.Error("failed to stop event bus", forge.F("error", err))
			}
		}
	}

	// Close store
	if es.store != nil {
		if err := es.store.Close(ctx); err != nil {
			if es.logger != nil {
				es.logger.Error("failed to close event store", forge.F("error", err))
			}
		}
	}

	es.started = false

	if es.logger != nil {
		es.logger.Info("event service stopped")
	}

	if es.metrics != nil {
		es.metrics.Counter("forge.events.service_stopped").Inc()
	}

	return nil
}

// HealthCheck checks the health of the event service.
func (es *EventService) HealthCheck(ctx context.Context) error {
	es.mu.RLock()
	defer es.mu.RUnlock()

	if !es.started {
		return errors.New("event service not started")
	}

	// Check bus health
	if es.bus != nil {
		if err := es.bus.HealthCheck(ctx); err != nil {
			return fmt.Errorf("event bus unhealthy: %w", err)
		}
	}

	// Check store health
	if es.store != nil {
		if err := es.store.HealthCheck(ctx); err != nil {
			return fmt.Errorf("event store unhealthy: %w", err)
		}
	}

	return nil
}

// GetEventBus returns the event bus.
func (es *EventService) GetEventBus() core.EventBus {
	return es.bus
}

// GetEventStore returns the event store.
func (es *EventService) GetEventStore() core.EventStore {
	return es.store
}

// GetHandlerRegistry returns the handler registry.
func (es *EventService) GetHandlerRegistry() *core.HandlerRegistry {
	return es.handlerRegistry
}

// initializeEventStore initializes the event store.
func (es *EventService) initializeEventStore(ctx context.Context) error {
	switch es.config.Store.Type {
	case "memory":
		es.store = stores.NewMemoryEventStore(es.logger, es.metrics)
	default:
		return fmt.Errorf("unsupported event store type: %s", es.config.Store.Type)
	}

	if es.logger != nil {
		es.logger.Info("event store initialized", forge.F("type", es.config.Store.Type))
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
			if es.logger != nil {
				es.logger.Warn("unsupported broker type", forge.F("type", brokerConfig.Type), forge.F("name", brokerConfig.Name))
			}

			continue
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
