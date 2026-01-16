package events

import (
	"time"

	"github.com/xraph/forge/extensions/events/core"
)

// DI container keys for events extension services.
const (
	// ServiceKey is the DI key for the event service.
	ServiceKey = "events"
	// EventBusKey is the DI key for the event bus.
	EventBusKey = "eventBus"
	// EventStoreKey is the DI key for the event store.
	EventStoreKey = "eventStore"
	// HandlerRegistryKey is the DI key for the handler registry.
	HandlerRegistryKey = "eventHandlerRegistry"
)

// Config defines the configuration for the events extension.
type Config struct {
	// Event Bus configuration
	Bus BusConfig `json:"bus" yaml:"bus"`

	// Event Store configuration
	Store StoreConfig `json:"store" yaml:"store"`

	// Message Brokers
	Brokers []BrokerConfig `json:"brokers" yaml:"brokers"`

	// Metrics
	Metrics MetricsConfig `json:"metrics" yaml:"metrics"`
}

// BusConfig defines configuration for the event bus.
type BusConfig struct {
	DefaultBroker     string        `json:"default_broker"     yaml:"default_broker"`
	MaxRetries        int           `json:"max_retries"        yaml:"max_retries"`
	RetryDelay        time.Duration `json:"retry_delay"        yaml:"retry_delay"`
	EnableMetrics     bool          `json:"enable_metrics"     yaml:"enable_metrics"`
	EnableTracing     bool          `json:"enable_tracing"     yaml:"enable_tracing"`
	BufferSize        int           `json:"buffer_size"        yaml:"buffer_size"`
	WorkerCount       int           `json:"worker_count"       yaml:"worker_count"`
	ProcessingTimeout time.Duration `json:"processing_timeout" yaml:"processing_timeout"`
}

// StoreConfig defines configuration for the event store.
type StoreConfig struct {
	Type     string `json:"type"     yaml:"type"`     // memory, postgres, mongodb
	Database string `json:"database" yaml:"database"` // Database connection name (from database extension)
	Table    string `json:"table"    yaml:"table"`
}

// BrokerConfig defines configuration for message brokers.
type BrokerConfig struct {
	Name     string         `json:"name"     yaml:"name"`
	Type     string         `json:"type"     yaml:"type"` // memory, nats, redis
	Enabled  bool           `json:"enabled"  yaml:"enabled"`
	Priority int            `json:"priority" yaml:"priority"`
	Config   map[string]any `json:"config"   yaml:"config"`
}

// MetricsConfig defines configuration for event metrics.
type MetricsConfig struct {
	Enabled          bool          `json:"enabled"            yaml:"enabled"`
	PublishInterval  time.Duration `json:"publish_interval"   yaml:"publish_interval"`
	EnablePerType    bool          `json:"enable_per_type"    yaml:"enable_per_type"`
	EnablePerHandler bool          `json:"enable_per_handler" yaml:"enable_per_handler"`
}

// DefaultConfig returns default configuration.
func DefaultConfig() Config {
	return Config{
		Bus: BusConfig{
			DefaultBroker:     "memory",
			MaxRetries:        3,
			RetryDelay:        time.Second * 5,
			EnableMetrics:     true,
			EnableTracing:     false,
			BufferSize:        1000,
			WorkerCount:       10,
			ProcessingTimeout: time.Second * 30,
		},
		Store: StoreConfig{
			Type:     "memory",
			Database: "",
			Table:    "events",
		},
		Brokers: []BrokerConfig{
			{
				Name:     "memory",
				Type:     "memory",
				Enabled:  true,
				Priority: 1,
				Config:   make(map[string]any),
			},
		},
		Metrics: MetricsConfig{
			Enabled:          true,
			PublishInterval:  time.Second * 30,
			EnablePerType:    true,
			EnablePerHandler: true,
		},
	}
}

// ToCoreStoreConfig converts to core.EventStoreConfig.
func (c *StoreConfig) ToCoreStoreConfig() *core.EventStoreConfig {
	return &core.EventStoreConfig{
		Type:           c.Type,
		ConnectionName: c.Database,
		Database:       "events",
		EventsTable:    c.Table,
		SnapshotsTable: c.Table + "_snapshots",
		MaxConnections: 10,
		ConnTimeout:    time.Second * 30,
		ReadTimeout:    time.Second * 10,
		WriteTimeout:   time.Second * 10,
		EnableMetrics:  true,
		EnableTracing:  false,
	}
}
