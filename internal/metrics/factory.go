package metrics

import (
	"time"

	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

const MetricsKey = shared.MetricsKey
const CollectorKey = shared.MetricsCollectorKey

// EndpointConfig contains configuration for metrics endpoints
type EndpointConfig struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	PrefixPath    string        `yaml:"prefix_path" json:"prefix_path"`
	MetricsPath   string        `yaml:"metrics_path" json:"metrics_path"`
	HealthPath    string        `yaml:"health_path" json:"health_path"`
	StatsPath     string        `yaml:"stats_path" json:"stats_path"`
	EnableCORS    bool          `yaml:"enable_cors" json:"enable_cors"`
	RequireAuth   bool          `yaml:"require_auth" json:"require_auth"`
	CacheDuration time.Duration `yaml:"cache_duration" json:"cache_duration"`
}

// ExporterConfig contains configuration for exporters
type ExporterConfig = shared.MetricsExporterConfig[map[string]interface{}]

// RegisterMetricsCollector registers the metrics collector directly with DI container
func RegisterMetricsCollector(container shared.Container, config *CollectorConfig) error {
	loggerFn, err := container.Resolve(shared.LoggerKey)
	if err != nil {
		return err
	}
	logger, ok := loggerFn.(logger.Logger)
	if !ok {
		return errors.ErrServiceNotFound(shared.LoggerKey)
	}

	col := New(config, logger)

	return container.Register(col.Name(), func(c shared.Container) (any, error) {
		return col, nil
	}, shared.Singleton(), shared.WithDependencies(shared.ConfigKey))
}

// GetMetricsInstance retrieves the metrics collector from DI container
func GetMetricsInstance(container shared.Container) (shared.Metrics, error) {
	collector, err := container.Resolve(CollectorKey)
	if err != nil {
		return nil, err
	}

	if metricsCollector, ok := collector.(shared.Metrics); ok {
		return metricsCollector, nil
	}

	return nil, errors.ErrServiceNotFound(CollectorKey)
}
