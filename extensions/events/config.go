package events

import (
	"time"

	"github.com/xraph/forge/extensions/events/core"
)

// Config defines the configuration for the events extension
type Config struct {
	// Event Bus configuration
	Bus BusConfig `yaml:"bus" json:"bus"`

	// Event Store configuration
	Store StoreConfig `yaml:"store" json:"store"`

	// Message Brokers
	Brokers []BrokerConfig `yaml:"brokers" json:"brokers"`

	// Metrics
	Metrics MetricsConfig `yaml:"metrics" json:"metrics"`
}

// BusConfig defines configuration for the event bus
type BusConfig struct {
	DefaultBroker     string        `yaml:"default_broker" json:"default_broker"`
	MaxRetries        int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay        time.Duration `yaml:"retry_delay" json:"retry_delay"`
	EnableMetrics     bool          `yaml:"enable_metrics" json:"enable_metrics"`
	EnableTracing     bool          `yaml:"enable_tracing" json:"enable_tracing"`
	BufferSize        int           `yaml:"buffer_size" json:"buffer_size"`
	WorkerCount       int           `yaml:"worker_count" json:"worker_count"`
	ProcessingTimeout time.Duration `yaml:"processing_timeout" json:"processing_timeout"`
}

// StoreConfig defines configuration for the event store
type StoreConfig struct {
	Type     string `yaml:"type" json:"type"`         // memory, postgres, mongodb
	Database string `yaml:"database" json:"database"` // Database connection name (from database extension)
	Table    string `yaml:"table" json:"table"`
}

// BrokerConfig defines configuration for message brokers
type BrokerConfig struct {
	Name     string                 `yaml:"name" json:"name"`
	Type     string                 `yaml:"type" json:"type"` // memory, nats, redis
	Enabled  bool                   `yaml:"enabled" json:"enabled"`
	Priority int                    `yaml:"priority" json:"priority"`
	Config   map[string]interface{} `yaml:"config" json:"config"`
}

// MetricsConfig defines configuration for event metrics
type MetricsConfig struct {
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	PublishInterval  time.Duration `yaml:"publish_interval" json:"publish_interval"`
	EnablePerType    bool          `yaml:"enable_per_type" json:"enable_per_type"`
	EnablePerHandler bool          `yaml:"enable_per_handler" json:"enable_per_handler"`
}

// DefaultConfig returns default configuration
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
				Config:   make(map[string]interface{}),
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

// ToCoreStoreConfig converts to core.EventStoreConfig
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
