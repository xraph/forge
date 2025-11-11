package metrics

import (
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

const MetricsKey = shared.MetricsKey
const CollectorKey = shared.MetricsCollectorKey

// EndpointConfig contains configuration for metrics endpoints.
type EndpointConfig struct {
	Enabled       bool          `json:"enabled"        yaml:"enabled"`
	PrefixPath    string        `json:"prefix_path"    yaml:"prefix_path"`
	MetricsPath   string        `json:"metrics_path"   yaml:"metrics_path"`
	HealthPath    string        `json:"health_path"    yaml:"health_path"`
	StatsPath     string        `json:"stats_path"     yaml:"stats_path"`
	EnableCORS    bool          `json:"enable_cors"    yaml:"enable_cors"`
	RequireAuth   bool          `json:"require_auth"   yaml:"require_auth"`
	CacheDuration time.Duration `json:"cache_duration" yaml:"cache_duration"`
}

// ExporterConfig contains configuration for exporters.
type ExporterConfig = shared.MetricsExporterConfig[map[string]any]

// RegisterMetricsCollector registers the metrics collector directly with DI container.
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

// GetMetricsInstance retrieves the metrics collector from DI container.
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
