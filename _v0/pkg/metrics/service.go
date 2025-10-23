package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/metrics/collectors"
)

// =============================================================================
// SERVICE DEFINITION
// =============================================================================

// Service implements the metrics service for DI integration
type Service struct {
	name      string
	collector MetricsCollector
	config    *ServiceConfig
	logger    logger.Logger
	started   bool
	startTime time.Time
}

// ServiceConfig contains configuration for the metrics service
type ServiceConfig struct {
	CollectorConfig *CollectorConfig          `yaml:"collector" json:"collector"`
	AutoRegister    bool                      `yaml:"auto_register" json:"auto_register"`
	EnableEndpoints bool                      `yaml:"enable_endpoints" json:"enable_endpoints"`
	EndpointConfig  *EndpointConfig           `yaml:"endpoints" json:"endpoints"`
	Exporters       map[string]ExporterConfig `yaml:"exporters" json:"exporters"`
}

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
type ExporterConfig struct {
	Enabled  bool                   `yaml:"enabled" json:"enabled"`
	Interval time.Duration          `yaml:"interval" json:"interval"`
	Config   map[string]interface{} `yaml:"config" json:"config"`
}

// DefaultServiceConfig returns default service configuration
func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		CollectorConfig: DefaultCollectorConfig(),
		AutoRegister:    true,
		EnableEndpoints: true,
		EndpointConfig: &EndpointConfig{
			Enabled:       true,
			PrefixPath:    "/metrics",
			MetricsPath:   "",
			HealthPath:    "/health",
			StatsPath:     "/stats",
			EnableCORS:    true,
			RequireAuth:   false,
			CacheDuration: time.Second * 30,
		},
		Exporters: make(map[string]ExporterConfig),
	}
}

// NewService creates a new metrics service
func NewService(config *ServiceConfig, logger logger.Logger) *Service {
	if config == nil {
		config = DefaultServiceConfig()
	}

	return &Service{
		name:      MetricsKey,
		collector: NewCollector(config.CollectorConfig, logger),
		config:    config,
		logger:    logger,
	}
}

// =============================================================================
// SERVICE LIFECYCLE IMPLEMENTATION
// =============================================================================

// Name returns the service name
func (s *Service) Name() string {
	return s.name
}

// Dependencies returns the service dependencies
func (s *Service) Dependencies() []string {
	return []string{common.ConfigKey}
}

// OnStart starts the metrics service
func (s *Service) Start(ctx context.Context) error {
	if s.started {
		return common.ErrServiceAlreadyExists(s.name)
	}

	s.started = true
	s.startTime = time.Now()

	// Start the metrics collector
	if err := s.collector.Start(ctx); err != nil {
		s.started = false
		return common.ErrServiceStartFailed(s.name, err)
	}

	// Register built-in collectors if auto-register is enabled
	if s.config.AutoRegister {
		if err := s.registerBuiltinCollectors(); err != nil {
			s.logger.Error("failed to register built-in collectors", logger.Error(err))
			// Don't fail startup for this
		}
	}

	// Start exporters
	if err := s.startExporters(ctx); err != nil {
		s.logger.Error("failed to start exporters", logger.Error(err))
		// Don't fail startup for this
	}

	if s.logger != nil {
		s.logger.Info("metrics service started",
			logger.String("name", s.name),
			logger.Bool("auto_register", s.config.AutoRegister),
			logger.Bool("enable_endpoints", s.config.EnableEndpoints),
			logger.Int("exporters", len(s.config.Exporters)),
		)
	}

	return nil
}

// OnStop stops the metrics service
func (s *Service) Stop(ctx context.Context) error {
	if !s.started {
		return common.ErrServiceNotFound(s.name)
	}

	s.started = false

	// Stop exporters
	if err := s.stopExporters(ctx); err != nil {
		s.logger.Error("failed to stop exporters", logger.Error(err))
	}

	// Stop the metrics collector
	if err := s.collector.Stop(ctx); err != nil {
		s.logger.Error("failed to stop metrics collector", logger.Error(err))
	}

	if s.logger != nil {
		s.logger.Info("metrics service stopped",
			logger.String("name", s.name),
			logger.Duration("uptime", time.Since(s.startTime)),
		)
	}

	return nil
}

// OnHealthCheck performs health check
func (s *Service) OnHealthCheck(ctx context.Context) error {
	if !s.started {
		return common.ErrHealthCheckFailed(s.name, fmt.Errorf("metrics service not started"))
	}

	// Check collector health
	if err := s.collector.OnHealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed(s.name, err)
	}

	// Check if metrics are being collected
	stats := s.collector.GetStats()
	if stats.MetricsCreated == 0 {
		return common.ErrHealthCheckFailed(s.name, fmt.Errorf("no metrics created"))
	}

	// Check if collection is working
	if !stats.LastCollectionTime.IsZero() {
		timeSinceLastCollection := time.Since(stats.LastCollectionTime)
		if timeSinceLastCollection > time.Minute*5 {
			return common.ErrHealthCheckFailed(s.name, fmt.Errorf("metrics collection stalled"))
		}
	}

	return nil
}

// =============================================================================
// PUBLIC METHODS
// =============================================================================

// GetCollector returns the metrics collector
func (s *Service) GetCollector() MetricsCollector {
	return s.collector
}

// Counter creates or retrieves a counter metric
func (s *Service) Counter(name string, tags ...string) Counter {
	return s.collector.Counter(name, tags...)
}

// Gauge creates or retrieves a gauge metric
func (s *Service) Gauge(name string, tags ...string) Gauge {
	return s.collector.Gauge(name, tags...)
}

// Histogram creates or retrieves a histogram metric
func (s *Service) Histogram(name string, tags ...string) Histogram {
	return s.collector.Histogram(name, tags...)
}

// Timer creates or retrieves a timer metric
func (s *Service) Timer(name string, tags ...string) Timer {
	return s.collector.Timer(name, tags...)
}

// RegisterCollector registers a custom collector
func (s *Service) RegisterCollector(collector CustomCollector) error {
	return s.collector.RegisterCollector(collector)
}

// GetMetrics returns all metrics
func (s *Service) GetMetrics() map[string]interface{} {
	return s.collector.GetMetrics()
}

// Export exports metrics in the specified format
func (s *Service) Export(format ExportFormat) ([]byte, error) {
	return s.collector.Export(format)
}

// GetStats returns service statistics
func (s *Service) GetStats() ServiceStats {
	collectorStats := s.collector.GetStats()

	return ServiceStats{
		ServiceName:      s.name,
		Started:          s.started,
		StartTime:        s.startTime,
		Uptime:           time.Since(s.startTime),
		CollectorStats:   collectorStats,
		AutoRegister:     s.config.AutoRegister,
		EndpointsEnabled: s.config.EnableEndpoints,
		ActiveExporters:  s.getActiveExporters(),
		Configuration:    s.config,
	}
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// registerBuiltinCollectors registers built-in collectors
func (s *Service) registerBuiltinCollectors() error {
	var errors []error

	// Register system collector
	if s.config.CollectorConfig.EnableSystemMetrics {
		systemCollector := collectors.NewSystemCollector()
		if err := s.collector.RegisterCollector(systemCollector); err != nil {
			errors = append(errors, fmt.Errorf("failed to register system collector: %w", err))
		}
	}

	// Register runtime collector
	if s.config.CollectorConfig.EnableRuntimeMetrics {
		runtimeCollector := collectors.NewRuntimeCollector()
		if err := s.collector.RegisterCollector(runtimeCollector); err != nil {
			errors = append(errors, fmt.Errorf("failed to register runtime collector: %w", err))
		}
	}

	// Register HTTP collector
	httpCollector := collectors.NewHTTPCollector()
	if err := s.collector.RegisterCollector(httpCollector); err != nil {
		errors = append(errors, fmt.Errorf("failed to register HTTP collector: %w", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to register %d built-in collectors: %v", len(errors), errors)
	}

	if s.logger != nil {
		s.logger.Info("built-in collectors registered",
			logger.Bool("system", s.config.CollectorConfig.EnableSystemMetrics),
			logger.Bool("runtime", s.config.CollectorConfig.EnableRuntimeMetrics),
			logger.Bool("http", true),
		)
	}

	return nil
}

// startExporters starts configured exporters
func (s *Service) startExporters(ctx context.Context) error {
	if len(s.config.Exporters) == 0 {
		return nil
	}

	for name, config := range s.config.Exporters {
		if config.Enabled {
			if err := s.startExporter(ctx, name, config); err != nil {
				s.logger.Error("failed to start exporter",
					logger.String("exporter", name),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

// startExporter starts a specific exporter
func (s *Service) startExporter(ctx context.Context, name string, config ExporterConfig) error {
	if s.logger != nil {
		s.logger.Info("starting exporter",
			logger.String("exporter", name),
			logger.Duration("interval", config.Interval),
		)
	}

	// Start exporter goroutine
	go s.exporterLoop(ctx, name, config)

	return nil
}

// exporterLoop runs the exporter loop
func (s *Service) exporterLoop(ctx context.Context, name string, config ExporterConfig) {
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.started {
				return
			}

			if err := s.performExport(name, config); err != nil {
				s.logger.Error("export failed",
					logger.String("exporter", name),
					logger.Error(err),
				)
			}
		}
	}
}

// performExport performs a single export operation
func (s *Service) performExport(name string, config ExporterConfig) error {
	// Determine export format based on exporter name
	var format ExportFormat
	switch name {
	case "prometheus":
		format = ExportFormatPrometheus
	case "json":
		format = ExportFormatJSON
	case "influx":
		format = ExportFormatInflux
	case "statsd":
		format = ExportFormatStatsD
	default:
		return fmt.Errorf("unknown exporter: %s", name)
	}

	// Export metrics
	data, err := s.collector.Export(format)
	if err != nil {
		return fmt.Errorf("failed to export metrics: %w", err)
	}

	// Process exported data based on configuration
	if err := s.processExportedData(name, config, data); err != nil {
		return fmt.Errorf("failed to process exported data: %w", err)
	}

	return nil
}

// processExportedData processes exported data
func (s *Service) processExportedData(exporterName string, config ExporterConfig, data []byte) error {
	// This is a placeholder implementation
	// In a real implementation, this would:
	// - Send data to monitoring systems
	// - Write to files
	// - Post to HTTP endpoints
	// - Send via network protocols

	if s.logger != nil {
		s.logger.Debug("exported metrics",
			logger.String("exporter", exporterName),
			logger.Int("bytes", len(data)),
		)
	}

	return nil
}

// stopExporters stops all exporters
func (s *Service) stopExporters(ctx context.Context) error {
	// Exporters are stopped by context cancellation
	return nil
}

// getActiveExporters returns the number of active exporters
func (s *Service) getActiveExporters() int {
	count := 0
	for _, config := range s.config.Exporters {
		if config.Enabled {
			count++
		}
	}
	return count
}

// =============================================================================
// SERVICE STATISTICS
// =============================================================================

// ServiceStats contains statistics about the metrics service
type ServiceStats struct {
	ServiceName      string         `json:"service_name"`
	Started          bool           `json:"started"`
	StartTime        time.Time      `json:"start_time"`
	Uptime           time.Duration  `json:"uptime"`
	CollectorStats   CollectorStats `json:"collector_stats"`
	AutoRegister     bool           `json:"auto_register"`
	EndpointsEnabled bool           `json:"endpoints_enabled"`
	ActiveExporters  int            `json:"active_exporters"`
	Configuration    *ServiceConfig `json:"configuration"`
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// RegisterMetricsService registers the metrics service with DI container
func RegisterMetricsService(container common.Container, config *ServiceConfig, logger logger.Logger) error {
	service := NewService(config, logger)

	return container.Register(common.ServiceDefinition{
		Name:         MetricsKey,
		Type:         (*common.Service)(nil),
		Constructor:  func() common.Service { return service },
		Singleton:    true,
		Dependencies: []string{common.ConfigKey},
		Tags: map[string]string{
			"type":    "metrics",
			"version": "1.0.0",
		},
	})
}

// RegisterMetricsCollector registers the metrics collector directly with DI container
func RegisterMetricsCollector(container common.Container, config *ServiceConfig, logger logger.Logger) error {
	service := NewService(config, logger)

	// Register both the service and collector
	if err := container.Register(common.ServiceDefinition{
		Name:         MetricsKey,
		Type:         (*Service)(nil),
		Instance:     service,
		Singleton:    true,
		Dependencies: []string{common.ConfigKey},
		Extensions:   map[string]any{},
	}); err != nil {
		return err
	}

	return container.Register(common.ServiceDefinition{
		Name:        CollectorKey,
		Type:        (*common.Metrics)(nil),
		Constructor: func() common.Metrics { return service.GetCollector() },
		Singleton:   true,
		// Dependencies: []string{MetricsKey},
		Extensions: map[string]any{
			"referenceNames": []string{"ForgeMetrics"},
		},
	})
}

// GetMetricsService retrieves the metrics service from DI container
func GetMetricsService(container common.Container) (*Service, error) {
	service, err := container.ResolveNamed(MetricsKey)
	if err != nil {
		return nil, err
	}

	if metricsService, ok := service.(*Service); ok {
		return metricsService, nil
	}

	return nil, common.ErrServiceNotFound(MetricsKey)
}

// GetMetricsCollector retrieves the metrics collector from DI container
func GetMetricsCollector(container common.Container) (MetricsCollector, error) {
	collector, err := container.ResolveNamed(CollectorKey)
	if err != nil {
		return nil, err
	}

	if metricsCollector, ok := collector.(MetricsCollector); ok {
		return metricsCollector, nil
	}

	return nil, common.ErrServiceNotFound(CollectorKey)
}

// =============================================================================
// CONFIGURATION HELPERS
// =============================================================================

// NewServiceConfigFromEnv creates service configuration from environment variables
func NewServiceConfigFromEnv() *ServiceConfig {
	config := DefaultServiceConfig()

	// This would typically read from environment variables
	// For now, return default configuration

	return config
}

// ValidateServiceConfig validates service configuration
func ValidateServiceConfig(config *ServiceConfig) error {
	if config == nil {
		return fmt.Errorf("service configuration is nil")
	}

	if config.CollectorConfig == nil {
		return fmt.Errorf("collector configuration is nil")
	}

	if config.EndpointConfig == nil {
		return fmt.Errorf("endpoint configuration is nil")
	}

	if config.EndpointConfig.CacheDuration < 0 {
		return fmt.Errorf("cache duration cannot be negative")
	}

	// Validate exporters
	for name, exporterConfig := range config.Exporters {
		if exporterConfig.Enabled && exporterConfig.Interval <= 0 {
			return fmt.Errorf("exporter %s: interval must be positive", name)
		}
	}

	return nil
}
