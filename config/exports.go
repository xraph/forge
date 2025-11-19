package config

import (
	"github.com/xraph/forge/internal/config"
	"github.com/xraph/forge/internal/config/sources"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

type (
	ConfigSource        = config.ConfigSource
	ConfigSourceOptions = config.ConfigSourceOptions
	ValidationOptions   = config.ValidationOptions
	ValidationRule      = config.ValidationRule
	SourceMetadata      = config.SourceMetadata
	ChangeType          = config.ChangeType
	ConfigChange        = config.ConfigChange
	ConfigSourceFactory = config.ConfigSourceFactory
	SourceConfig        = config.SourceConfig
	ValidationConfig    = config.ValidationConfig
	SourceRegistry      = config.SourceRegistry
	SourceEvent         = config.SourceEvent
	SourceEventHandler  = config.SourceEventHandler
	WatchContext        = config.WatchContext
	ConfigManager       = config.ConfigManager
)

const (
	ChangeTypeSet            = config.ChangeTypeSet
	ChangeTypeUpdate         = config.ChangeTypeUpdate
	ChangeTypeDelete         = config.ChangeTypeDelete
	ChangeTypeReload         = config.ChangeTypeReload
	ValidationModePermissive = config.ValidationModePermissive
	ValidationModeStrict     = config.ValidationModeStrict
	ValidationModeLoose      = config.ValidationModeLoose
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

func NewSecretsManager(conf config.SecretsConfig) config.SecretsManager {
	return config.NewSecretsManager(conf)
}

func NewManager(conf config.ManagerConfig) config.ConfigManager {
	return config.NewManager(conf)
}

func NewSourceRegistry(logger logger.Logger) config.SourceRegistry {
	return config.NewSourceRegistry(logger)
}

func NewValidator(conf config.ValidatorConfig) *config.Validator {
	return config.NewValidator(conf)
}

func NewWatcher(conf config.WatcherConfig) *config.Watcher {
	return config.NewWatcher(conf)
}

func NewConsulSource(prefix string, options sources.ConsulSourceOptions) (config.ConfigSource, error) {
	return sources.NewConsulSource(prefix, options)
}

func NewEnvSource(prefix string, options sources.EnvSourceOptions) (config.ConfigSource, error) {
	return sources.NewEnvSource(prefix, options)
}

func NewFileSource(path string, options sources.FileSourceOptions) (config.ConfigSource, error) {
	return sources.NewFileSource(path, options)
}

func NewK8sSource(options sources.K8sSourceOptions) (config.ConfigSource, error) {
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
