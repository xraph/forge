package core

import (
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// ConfigSource represents a source of configuration data
type ConfigSource = common.ConfigSource

// ConfigSourceOptions contains options for creating a configuration source
type ConfigSourceOptions struct {
	Name            string
	Priority        int
	WatchInterval   time.Duration
	EnvironmentVars map[string]string
	SecretKeys      []string
	Validation      ValidationOptions
}

// ValidationOptions contains validation configuration
type ValidationOptions struct {
	Enabled     bool
	Schema      interface{}
	Required    []string
	Constraints map[string]interface{}
	CustomRules []ValidationRule
}

// ValidationRule represents a custom validation rule
type ValidationRule interface {
	Name() string
	Validate(key string, value interface{}) error
	AppliesTo(key string) bool
}

// SourceMetadata contains metadata about a configuration source
type SourceMetadata = common.SourceMetadata

// ChangeType represents the type of configuration change
type ChangeType = common.ChangeType

const (
	ChangeTypeSet    = common.ChangeTypeSet
	ChangeTypeUpdate = common.ChangeTypeUpdate
	ChangeTypeDelete = common.ChangeTypeDelete
	ChangeTypeReload = common.ChangeTypeReload
)

// ConfigChange represents a configuration change event
type ConfigChange = common.ConfigChange

// ConfigSourceFactory creates configuration sources
type ConfigSourceFactory interface {
	// CreateFileSource creates a file-based configuration source
	CreateFileSource(path string, options ConfigSourceOptions) (ConfigSource, error)

	// CreateEnvSource creates an environment variable configuration source
	CreateEnvSource(prefix string, options ConfigSourceOptions) (ConfigSource, error)

	// CreateMemorySource creates an in-memory configuration source
	CreateMemorySource(data map[string]interface{}, options ConfigSourceOptions) (ConfigSource, error)

	// RegisterCustomSource registers a custom configuration source
	RegisterCustomSource(name string, factory func(ConfigSourceOptions) (ConfigSource, error)) error
}

// SourceConfig contains common configuration for all sources
type SourceConfig struct {
	Name          string            `yaml:"name" json:"name"`
	Type          string            `yaml:"type" json:"type"`
	Priority      int               `yaml:"priority" json:"priority"`
	WatchEnabled  bool              `yaml:"watch_enabled" json:"watch_enabled"`
	WatchInterval time.Duration     `yaml:"watch_interval" json:"watch_interval"`
	RetryAttempts int               `yaml:"retry_attempts" json:"retry_attempts"`
	RetryDelay    time.Duration     `yaml:"retry_delay" json:"retry_delay"`
	Properties    map[string]string `yaml:"properties" json:"properties"`
	Validation    ValidationConfig  `yaml:"validation" json:"validation"`
}

// ValidationConfig contains validation configuration for sources
type ValidationConfig struct {
	Enabled     bool                   `yaml:"enabled" json:"enabled"`
	Required    []string               `yaml:"required" json:"required"`
	Schema      map[string]interface{} `yaml:"schema" json:"schema"`
	Constraints map[string]interface{} `yaml:"constraints" json:"constraints"`
}

// SourceRegistry manages registered configuration sources
type SourceRegistry interface {
	// RegisterSource registers a configuration source
	RegisterSource(source ConfigSource) error

	// UnregisterSource unregisters a configuration source
	UnregisterSource(name string) error

	// GetSource retrieves a configuration source by name
	GetSource(name string) (ConfigSource, error)

	// GetSources returns all registered sources ordered by priority
	GetSources() []ConfigSource

	// GetSourceMetadata returns metadata for a source
	GetSourceMetadata(name string) (*SourceMetadata, error)

	// GetAllMetadata returns metadata for all sources
	GetAllMetadata() map[string]*SourceMetadata
}

// SourceEvent represents an event from a configuration source
type SourceEvent struct {
	SourceName string                 `json:"source_name"`
	EventType  string                 `json:"event_type"`
	Data       map[string]interface{} `json:"data"`
	Timestamp  time.Time              `json:"timestamp"`
	Error      error                  `json:"error,omitempty"`
}

// SourceEventHandler handles events from configuration sources
type SourceEventHandler interface {
	HandleEvent(event SourceEvent) error
}

// WatchContext contains context for watching configuration changes
type WatchContext struct {
	Source      ConfigSource
	Interval    time.Duration
	LastCheck   time.Time
	ChangeCount int64
	ErrorCount  int64
	Active      bool
}
