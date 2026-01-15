package config

import (
	"fmt"

	"github.com/xraph/confy"
	"github.com/xraph/confy/sources"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

const (
	ManagerKey = "forge:config:manager"
)

type (
	ConfigSource        = confy.ConfigSource
	ConfigSourceOptions = confy.ConfigSourceOptions
	ValidationOptions   = confy.ValidationOptions
	ValidationRule      = confy.ValidationRule
	SourceMetadata      = confy.SourceMetadata
	ChangeType          = confy.ChangeType
	ConfigChange        = confy.ConfigChange
	ConfigSourceFactory = confy.ConfigSourceFactory
	SourceConfig        = confy.SourceConfig
	ValidationConfig    = confy.ValidationConfig
	SourceRegistry      = confy.SourceRegistry
	SourceEvent         = confy.SourceEvent
	SourceEventHandler  = confy.SourceEventHandler
	WatchContext        = confy.WatchContext
	ConfigManager       = confy.Confy
	Confy               = confy.Confy
)

const (
	ChangeTypeSet            = confy.ChangeTypeSet
	ChangeTypeUpdate         = confy.ChangeTypeUpdate
	ChangeTypeDelete         = confy.ChangeTypeDelete
	ChangeTypeReload         = confy.ChangeTypeReload
	ValidationModePermissive = confy.ValidationModePermissive
	ValidationModeStrict     = confy.ValidationModeStrict
	ValidationModeLoose      = confy.ValidationModeLoose
)

// ExportFormatPrometheus   = config.ExportFormatPrometheus
// ExportFormatJSON         = config.ExportFormatJSON
// ExportFormatInflux       = config.ExportFormatInflux
// ExportFormatStatsD       = config.ExportFormatStatsD
// MetricTypeCounter        = config.MetricTypeCounter
// MetricTypeGauge          = config.MetricTypeGauge
// MetricTypeHistogram      = config.MetricTypeHistogram
// MetricTypeTimer          = config.MetricTypeTimer
// SecretsManager           = config.SecretsManager
// SecretProvider           = config.SecretProvider
// SecretsConfig            = config.SecretsConfig
// ManagerConfig            = config.ManagerConfig
// Watcher                  = config.Watcher
// WatcherConfig            = config.WatcherConfig
// NewManager               = config.NewManager
// NewSourceRegistry        = config.NewSourceRegistry
// NewValidator             = config.NewValidator
// NewWatcher               = config.NewWatcher
// NewSecretsManager        = config.NewSecretsManager

func NewSecretsManager(conf confy.SecretsConfig) confy.SecretsManager {
	return confy.NewSecretsManager(conf)
}

func NewManager(conf confy.Config) confy.Confy {
	return confy.NewFromConfig(conf)
}

func NewSourceRegistry(logger logger.Logger) confy.SourceRegistry {
	return confy.NewSourceRegistry(logger)
}

func NewValidator(conf confy.ValidatorConfig) *confy.Validator {
	return confy.NewValidator(conf)
}

func NewWatcher(conf confy.WatcherConfig) *confy.Watcher {
	return confy.NewWatcher(conf)
}

func NewConsulSource(prefix string, options sources.ConsulSourceOptions) (confy.ConfigSource, error) {
	return sources.NewConsulSource(prefix, options)
}

func NewEnvSource(prefix string, options sources.EnvSourceOptions) (confy.ConfigSource, error) {
	return sources.NewEnvSource(prefix, options)
}

func NewFileSource(path string, options sources.FileSourceOptions) (confy.ConfigSource, error) {
	return sources.NewFileSource(path, options)
}

func NewK8sSource(options sources.K8sSourceOptions) (confy.ConfigSource, error) {
	return sources.NewK8sSource(options)
}

func NewSourceFactory(logger logger.Logger, errorHandler shared.ErrorHandler) *sources.FileSourceFactory {
	return sources.NewFileSourceFactory(logger, errorHandler)
}

func NewEnvSourceFactory(logger logger.Logger, errorHandler shared.ErrorHandler) *sources.EnvSourceFactory {
	return sources.NewEnvSourceFactory(logger, errorHandler)
}

func NewK8sSourceFactory(logger logger.Logger, errorHandler shared.ErrorHandler) *sources.K8sSourceFactory {
	return sources.NewK8sSourceFactory(logger, errorHandler)
}

// GetConfigManager resolves the config manager from the container
// This is a convenience function for resolving the config manager service
// Uses ManagerKey constant (defined in manager.go) which equals shared.ConfigKey.
func GetConfigManager(container shared.Container) (confy.Confy, error) {
	return GetConfy(container)
}

// GetConfigManager resolves the config manager from the container
// This is a convenience function for resolving the config manager service
// Uses ManagerKey constant (defined in manager.go) which equals shared.ConfigKey.
func GetConfy(container shared.Container) (confy.Confy, error) {
	cm, err := container.Resolve(ManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config manager: %w", err)
	}

	configManager, ok := cm.(confy.Confy)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not ConfigManager, got %T", cm)
	}

	return configManager, nil
}
