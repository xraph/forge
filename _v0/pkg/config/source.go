package config

import (
	"github.com/xraph/forge/v0/pkg/config/core"
)

// ConfigSource represents a source of configuration data
type ConfigSource = core.ConfigSource

// ConfigSourceOptions contains options for creating a configuration source
type ConfigSourceOptions = core.ConfigSourceOptions

// ValidationOptions contains validation configuration
type ValidationOptions = core.ValidationOptions

// ValidationRule represents a custom validation rule
type ValidationRule = core.ValidationRule

// SourceMetadata contains metadata about a configuration source
type SourceMetadata = core.SourceMetadata

// ChangeType represents the type of configuration change
type ChangeType = core.ChangeType

const (
	ChangeTypeSet    ChangeType = core.ChangeTypeSet
	ChangeTypeUpdate ChangeType = core.ChangeTypeUpdate
	ChangeTypeDelete ChangeType = core.ChangeTypeDelete
	ChangeTypeReload ChangeType = core.ChangeTypeReload
)

// ConfigChange represents a configuration change event
type ConfigChange = core.ConfigChange

// ConfigSourceFactory creates configuration sources
type ConfigSourceFactory = core.ConfigSourceFactory

// SourceConfig contains common configuration for all sources
type SourceConfig = core.SourceConfig

// ValidationConfig contains validation configuration for sources
type ValidationConfig = core.ValidationConfig

// SourceRegistry manages registered configuration sources
type SourceRegistry = core.SourceRegistry

// SourceEvent represents an event from a configuration source
type SourceEvent = core.SourceEvent

// SourceEventHandler handles events from configuration sources
type SourceEventHandler = core.SourceEventHandler

// WatchContext contains context for watching configuration changes
type WatchContext = core.WatchContext
