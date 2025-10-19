package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/events/brokers/memory"
	"github.com/xraph/forge/pkg/events/brokers/nats"
	"github.com/xraph/forge/pkg/events/brokers/redis"
	eventscore "github.com/xraph/forge/pkg/events/core"
	"github.com/xraph/forge/pkg/events/stores"
	"github.com/xraph/forge/pkg/logger"
)

// EventService implements common.Service for DI integration
type EventService struct {
	name            string
	bus             EventBus
	store           EventStore
	dbManager       common.DatabaseManager
	handlerRegistry *HandlerRegistry
	config          *EventServiceConfig
	logger          common.Logger
	metrics         common.Metrics
	started         bool
	mu              sync.RWMutex
}

// EventServiceConfig defines configuration for the event service
type EventServiceConfig struct {
	Bus     EventBusConfig     `yaml:"bus" json:"bus"`
	Store   EventStoreConfig   `yaml:"store" json:"store"`
	Brokers []BrokerConfig     `yaml:"brokers" json:"brokers"`
	Metrics EventMetricsConfig `yaml:"metrics" json:"metrics"`
}

// EventBusConfig defines configuration for the event bus
type EventBusConfig = eventscore.EventBusConfig

// BrokerConfig defines configuration for message brokers
type BrokerConfig struct {
	Name     string                 `yaml:"name" json:"name"`
	Type     string                 `yaml:"type" json:"type"`
	Config   map[string]interface{} `yaml:"config" json:"config"`
	Enabled  bool                   `yaml:"enabled" json:"enabled"`
	Priority int                    `yaml:"priority" json:"priority"`
}

// EventMetricsConfig defines configuration for event metrics
type EventMetricsConfig struct {
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	PublishInterval  time.Duration `yaml:"publish_interval" json:"publish_interval"`
	HistogramBuckets []float64     `yaml:"histogram_buckets" json:"histogram_buckets"`
	EnablePerType    bool          `yaml:"enable_per_type" json:"enable_per_type"`
	EnablePerHandler bool          `yaml:"enable_per_handler" json:"enable_per_handler"`
}

// DefaultEventServiceConfig returns default configuration
func DefaultEventServiceConfig() *EventServiceConfig {
	return &EventServiceConfig{
		Bus: EventBusConfig{
			DefaultBroker:     "memory",
			MaxRetries:        3,
			RetryDelay:        time.Second * 5,
			EnableMetrics:     true,
			EnableTracing:     false,
			BufferSize:        1000,
			WorkerCount:       10,
			ProcessingTimeout: time.Second * 30,
		},
		Store: *DefaultEventStoreConfig(),
		Brokers: []BrokerConfig{
			{
				Name:     "memory",
				Type:     "memory",
				Enabled:  true,
				Priority: 1,
				Config:   make(map[string]interface{}),
			},
		},
		Metrics: EventMetricsConfig{
			Enabled:          true,
			PublishInterval:  time.Second * 30,
			HistogramBuckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
			EnablePerType:    true,
			EnablePerHandler: true,
		},
	}
}

// NewEventService creates a new event service
func NewEventService(config *EventServiceConfig, dbManager common.DatabaseManager, logger common.Logger, metrics common.Metrics) *EventService {
	if config == nil {
		config = DefaultEventServiceConfig()
	}

	return &EventService{
		name:            "event-service",
		config:          config,
		logger:          logger,
		metrics:         metrics,
		dbManager:       dbManager,
		handlerRegistry: NewHandlerRegistry(logger, metrics),
	}
}

// Name implements common.Service
func (es *EventService) Name() string {
	return es.name
}

// Dependencies implements common.Service
func (es *EventService) Dependencies() []string {
	return []string{"config-manager", "logger", "metrics"}
}

// OnStart implements common.Service
func (es *EventService) Start(ctx context.Context) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.started {
		return common.ErrServiceAlreadyExists("event-service")
	}

	if es.logger != nil {
		es.logger.Info("starting event service",
			logger.String("service", es.name),
		)
	}

	// Initialize event store
	if err := es.initializeEventStore(ctx); err != nil {
		return common.ErrServiceStartFailed(es.name, fmt.Errorf("failed to initialize event store: %w", err))
	}

	// Initialize event bus
	if err := es.initializeEventBus(ctx); err != nil {
		return common.ErrServiceStartFailed(es.name, fmt.Errorf("failed to initialize event bus: %w", err))
	}

	// Start metrics collection if enabled
	if es.config.Metrics.Enabled && es.metrics != nil {
		go es.startMetricsCollection(ctx)
	}

	es.started = true

	if es.logger != nil {
		es.logger.Info("event service started successfully",
			logger.String("service", es.name),
		)
	}

	if es.metrics != nil {
		es.metrics.Counter("forge.events.service_started").Inc()
	}

	return nil
}

// OnStop implements common.Service
func (es *EventService) Stop(ctx context.Context) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if !es.started {
		return nil
	}

	if es.logger != nil {
		es.logger.Info("stopping event service",
			logger.String("service", es.name),
		)
	}

	// Stop event bus
	if es.bus != nil {
		if err := es.bus.Stop(ctx); err != nil {
			if es.logger != nil {
				es.logger.Error("failed to stop event bus",
					logger.Error(err),
				)
			}
		}
	}

	// Close event store
	if es.store != nil {
		if err := es.store.Close(ctx); err != nil {
			if es.logger != nil {
				es.logger.Error("failed to close event store",
					logger.Error(err),
				)
			}
		}
	}

	es.started = false

	if es.logger != nil {
		es.logger.Info("event service stopped",
			logger.String("service", es.name),
		)
	}

	if es.metrics != nil {
		es.metrics.Counter("forge.events.service_stopped").Inc()
	}

	return nil
}

// OnHealthCheck implements common.Service
func (es *EventService) OnHealthCheck(ctx context.Context) error {
	es.mu.RLock()
	defer es.mu.RUnlock()

	if !es.started {
		return common.ErrHealthCheckFailed(es.name, fmt.Errorf("service not started"))
	}

	// Check event bus health
	if es.bus != nil {
		if err := es.bus.OnHealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed(es.name, fmt.Errorf("event bus unhealthy: %w", err))
		}
	}

	// Check event store health
	if es.store != nil {
		if err := es.store.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed(es.name, fmt.Errorf("event store unhealthy: %w", err))
		}
	}

	return nil
}

// initializeEventStore initializes the event store
func (es *EventService) initializeEventStore(ctx context.Context) error {
	switch es.config.Store.Type {
	case "memory":
		es.store = stores.NewMemoryEventStore(es.logger, es.metrics)
	case "postgres":
		store, err := stores.NewPostgresEventStore(&es.config.Store, es.logger, es.metrics, es.dbManager)
		if err != nil {
			return fmt.Errorf("failed to create postgres event store: %w", err)
		}
		es.store = store
	case "mongodb":
		store, err := stores.NewMongoEventStore(&es.config.Store, es.logger, es.metrics, es.dbManager)
		if err != nil {
			return fmt.Errorf("failed to create mongodb event store: %w", err)
		}
		es.store = store
	default:
		return fmt.Errorf("unsupported event store type: %s", es.config.Store.Type)
	}

	if es.logger != nil {
		es.logger.Info("event store initialized",
			logger.String("type", es.config.Store.Type),
		)
	}

	return nil
}

// initializeEventBus initializes the event bus
func (es *EventService) initializeEventBus(ctx context.Context) error {
	busConfig := EventBusOptions{
		Store:           es.store,
		HandlerRegistry: es.handlerRegistry,
		Logger:          es.logger,
		Metrics:         es.metrics,
		Config:          es.config.Bus,
	}

	bus, err := NewEventBus(busConfig)
	if err != nil {
		return fmt.Errorf("failed to create event bus: %w", err)
	}

	// Register brokers
	for _, brokerConfig := range es.config.Brokers {
		if !brokerConfig.Enabled {
			continue
		}

		broker, err := es.createBroker(brokerConfig)
		if err != nil {
			return fmt.Errorf("failed to create broker %s: %w", brokerConfig.Name, err)
		}

		if err := bus.RegisterBroker(brokerConfig.Name, broker); err != nil {
			return fmt.Errorf("failed to register broker %s: %w", brokerConfig.Name, err)
		}

		if es.logger != nil {
			es.logger.Info("broker registered",
				logger.String("name", brokerConfig.Name),
				logger.String("type", brokerConfig.Type),
			)
		}
	}

	// Start event bus
	if err := bus.Start(ctx); err != nil {
		return fmt.Errorf("failed to start event bus: %w", err)
	}

	es.bus = bus

	if es.logger != nil {
		es.logger.Info("event bus initialized")
	}

	return nil
}

// createBroker creates a message broker from configuration
func (es *EventService) createBroker(config BrokerConfig) (MessageBroker, error) {
	switch config.Type {
	case "memory":
		return memory.NewMemoryBroker(es.logger, es.metrics), nil
	case "nats":
		return nats.NewNATSBroker(config.Config, es.logger, es.metrics)
	case "redis":
		return redis.NewRedisBroker(config.Config, es.logger, es.metrics)
	default:
		return nil, fmt.Errorf("unsupported broker type: %s", config.Type)
	}
}

// startMetricsCollection starts collecting and publishing metrics
func (es *EventService) startMetricsCollection(ctx context.Context) {
	ticker := time.NewTicker(es.config.Metrics.PublishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			es.collectAndPublishMetrics()
		}
	}
}

// collectAndPublishMetrics collects and publishes event metrics
func (es *EventService) collectAndPublishMetrics() {
	if es.metrics == nil {
		return
	}

	// Collect handler registry stats
	if registryStats := es.handlerRegistry.Stats(); registryStats != nil {
		if totalHandlers, ok := registryStats["total_handlers"].(int); ok {
			es.metrics.Gauge("forge.events.handlers_total").Set(float64(totalHandlers))
		}
		if totalEventTypes, ok := registryStats["total_event_types"].(int); ok {
			es.metrics.Gauge("forge.events.event_types_total").Set(float64(totalEventTypes))
		}
	}

	// Collect event store stats if available
	if statsProvider, ok := es.store.(interface{ GetStats() *EventStoreStats }); ok {
		if stats := statsProvider.GetStats(); stats != nil {
			es.metrics.Gauge("forge.events.store_events_total").Set(float64(stats.TotalEvents))
			es.metrics.Gauge("forge.events.store_snapshots_total").Set(float64(stats.TotalSnapshots))

			if stats.Metrics != nil {
				es.metrics.Gauge("forge.events.store_events_saved").Set(float64(stats.Metrics.EventsSaved))
				es.metrics.Gauge("forge.events.store_events_read").Set(float64(stats.Metrics.EventsRead))
				es.metrics.Gauge("forge.events.store_errors").Set(float64(stats.Metrics.Errors))
			}
		}
	}

	// Collect event bus stats if available
	if statsProvider, ok := es.bus.(interface{ GetStats() map[string]interface{} }); ok {
		if stats := statsProvider.GetStats(); stats != nil {
			for key, value := range stats {
				if floatVal, ok := value.(float64); ok {
					es.metrics.Gauge(fmt.Sprintf("forge.events.bus_%s", key)).Set(floatVal)
				} else if intVal, ok := value.(int); ok {
					es.metrics.Gauge(fmt.Sprintf("forge.events.bus_%s", key)).Set(float64(intVal))
				} else if int64Val, ok := value.(int64); ok {
					es.metrics.Gauge(fmt.Sprintf("forge.events.bus_%s", key)).Set(float64(int64Val))
				}
			}
		}
	}
}

// GetEventBus returns the event bus instance
func (es *EventService) GetEventBus() EventBus {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.bus
}

// GetEventStore returns the event store instance
func (es *EventService) GetEventStore() EventStore {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.store
}

// GetHandlerRegistry returns the handler registry instance
func (es *EventService) GetHandlerRegistry() *HandlerRegistry {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.handlerRegistry
}

// RegisterHandler registers an event handler
func (es *EventService) RegisterHandler(eventType string, handler EventHandler) error {
	return es.handlerRegistry.Register(eventType, handler)
}

// UnregisterHandler unregisters an event handler
func (es *EventService) UnregisterHandler(eventType string, handlerName string) error {
	return es.handlerRegistry.Unregister(eventType, handlerName)
}

// PublishEvent publishes an event through the event bus
func (es *EventService) PublishEvent(ctx context.Context, event *Event) error {
	if es.bus == nil {
		return fmt.Errorf("event bus not initialized")
	}
	return es.bus.Publish(ctx, event)
}

// SubscribeToEvent subscribes to events of a specific type
func (es *EventService) SubscribeToEvent(eventType string, handler EventHandler) error {
	if es.bus == nil {
		return fmt.Errorf("event bus not initialized")
	}
	return es.bus.Subscribe(eventType, handler)
}

// GetServiceStats returns service statistics
func (es *EventService) GetServiceStats() map[string]interface{} {
	es.mu.RLock()
	defer es.mu.RUnlock()

	stats := map[string]interface{}{
		"name":    es.name,
		"started": es.started,
	}

	if es.handlerRegistry != nil {
		stats["handler_registry"] = es.handlerRegistry.Stats()
	}

	if statsProvider, ok := es.store.(interface{ GetStats() *EventStoreStats }); ok {
		if storeStats := statsProvider.GetStats(); storeStats != nil {
			stats["store"] = storeStats
		}
	}

	if statsProvider, ok := es.bus.(interface{ GetStats() map[string]interface{} }); ok {
		if busStats := statsProvider.GetStats(); busStats != nil {
			stats["bus"] = busStats
		}
	}

	return stats
}

// Validate validates the service configuration
func (es *EventService) Validate() error {
	if es.config == nil {
		return fmt.Errorf("event service configuration is required")
	}

	if err := es.config.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store configuration: %w", err)
	}

	if es.config.Bus.WorkerCount <= 0 {
		return fmt.Errorf("bus worker count must be positive")
	}

	if es.config.Bus.BufferSize <= 0 {
		return fmt.Errorf("bus buffer size must be positive")
	}

	if es.config.Bus.ProcessingTimeout <= 0 {
		return fmt.Errorf("bus processing timeout must be positive")
	}

	// Validate broker configurations
	for _, brokerConfig := range es.config.Brokers {
		if brokerConfig.Name == "" {
			return fmt.Errorf("broker name is required")
		}
		if brokerConfig.Type == "" {
			return fmt.Errorf("broker type is required for broker %s", brokerConfig.Name)
		}
	}

	return nil
}

// EventServiceFactory creates an EventService instance for DI
func EventServiceFactory(l common.Logger, metrics common.Metrics, configManager common.ConfigManager, dbManager common.DatabaseManager) *EventService {
	config := DefaultEventServiceConfig()

	// Bind configuration from config manager if available
	if configManager != nil {
		if err := configManager.Bind("events", config); err != nil && l != nil {
			l.Warn("failed to bind event service configuration",
				logger.Error(err),
			)
		}
	}

	return NewEventService(config, dbManager, l, metrics)
}

// RegisterEventService registers the event service with the DI container
func RegisterEventService(container common.Container) error {
	return container.Register(common.ServiceDefinition{
		Name:         "event-service",
		Type:         (*EventService)(nil),
		Constructor:  EventServiceFactory,
		Singleton:    true,
		Dependencies: []string{"logger", "metrics", "config-manager"},
	})
}

// RegisterEventBusService registers the event bus as a separate service
func RegisterEventBusService(container common.Container) error {
	return container.Register(common.ServiceDefinition{
		Name: "event-bus",
		Type: (*EventBus)(nil),
		Constructor: func(eventService *EventService) EventBus {
			return eventService.GetEventBus()
		},
		Singleton:    true,
		Dependencies: []string{"event-service"},
	})
}

// RegisterEventStoreService registers the event store as a separate service
func RegisterEventStoreService(container common.Container) error {
	return container.Register(common.ServiceDefinition{
		Name: "event-store",
		Type: (*EventStore)(nil),
		Constructor: func(eventService *EventService) EventStore {
			return eventService.GetEventStore()
		},
		Singleton:    true,
		Dependencies: []string{"event-service"},
	})
}

// RegisterAllEventServices registers all event-related services with the DI container
func RegisterAllEventServices(container common.Container) error {
	if err := RegisterEventService(container); err != nil {
		return fmt.Errorf("failed to register event service: %w", err)
	}

	if err := RegisterEventBusService(container); err != nil {
		return fmt.Errorf("failed to register event bus service: %w", err)
	}

	if err := RegisterEventStoreService(container); err != nil {
		return fmt.Errorf("failed to register event store service: %w", err)
	}

	return nil
}
