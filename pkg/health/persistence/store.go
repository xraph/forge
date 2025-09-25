package persistence

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/pkg/common"
	health "github.com/xraph/forge/pkg/health/core"
	"github.com/xraph/forge/pkg/logger"
)

// HealthStore defines the interface for health history storage
type HealthStore interface {
	// Store operations
	StoreResult(ctx context.Context, result *health.HealthResult) error
	StoreReport(ctx context.Context, report *health.HealthReport) error

	// Query operations
	GetResult(ctx context.Context, checkName string, timestamp time.Time) (*health.HealthResult, error)
	GetResults(ctx context.Context, checkName string, from, to time.Time) ([]*health.HealthResult, error)
	GetReport(ctx context.Context, timestamp time.Time) (*health.HealthReport, error)
	GetReports(ctx context.Context, from, to time.Time) ([]*health.HealthReport, error)

	// Aggregate operations
	GetHealthHistory(ctx context.Context, checkName string, from, to time.Time, interval time.Duration) ([]*HealthHistoryPoint, error)
	GetHealthTrend(ctx context.Context, checkName string, duration time.Duration) (*HealthTrend, error)
	GetHealthStatistics(ctx context.Context, checkName string, from, to time.Time) (*HealthStatistics, error)

	// Maintenance operations
	Cleanup(ctx context.Context, before time.Time) error
	GetSize(ctx context.Context) (int64, error)
	Close() error
}

// HealthHistoryPoint represents a point in health history
type HealthHistoryPoint struct {
	Timestamp time.Time              `json:"timestamp"`
	Status    health.HealthStatus    `json:"status"`
	Duration  time.Duration          `json:"duration"`
	Message   string                 `json:"message"`
	Error     string                 `json:"error,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// HealthTrend represents health trend analysis
type HealthTrend struct {
	CheckName           string                        `json:"check_name"`
	Period              time.Duration                 `json:"period"`
	TotalChecks         int64                         `json:"total_checks"`
	SuccessRate         float64                       `json:"success_rate"`
	AvgDuration         time.Duration                 `json:"avg_duration"`
	StatusDistribution  map[health.HealthStatus]int64 `json:"status_distribution"`
	ErrorRate           float64                       `json:"error_rate"`
	Trend               string                        `json:"trend"` // "improving", "stable", "degrading"
	LastCheck           time.Time                     `json:"last_check"`
	ConsecutiveFailures int                           `json:"consecutive_failures"`
}

// HealthStatistics represents health statistics over a period
type HealthStatistics struct {
	CheckName       string                        `json:"check_name"`
	From            time.Time                     `json:"from"`
	To              time.Time                     `json:"to"`
	TotalChecks     int64                         `json:"total_checks"`
	SuccessCount    int64                         `json:"success_count"`
	FailureCount    int64                         `json:"failure_count"`
	SuccessRate     float64                       `json:"success_rate"`
	AvgDuration     time.Duration                 `json:"avg_duration"`
	MinDuration     time.Duration                 `json:"min_duration"`
	MaxDuration     time.Duration                 `json:"max_duration"`
	StatusCounts    map[health.HealthStatus]int64 `json:"status_counts"`
	ErrorCategories map[string]int64              `json:"error_categories"`
	Uptime          time.Duration                 `json:"uptime"`
	Downtime        time.Duration                 `json:"downtime"`
	MTBF            time.Duration                 `json:"mtbf"` // Mean Time Between Failures
	MTTR            time.Duration                 `json:"mttr"` // Mean Time To Recovery
}

// HealthStoreConfig contains configuration for health store
type HealthStoreConfig struct {
	Type              string            `json:"type"`
	ConnectionString  string            `json:"connection_string"`
	Database          string            `json:"database"`
	Table             string            `json:"table"`
	MaxRetention      time.Duration     `json:"max_retention"`
	CleanupInterval   time.Duration     `json:"cleanup_interval"`
	BatchSize         int               `json:"batch_size"`
	MaxConnections    int               `json:"max_connections"`
	QueryTimeout      time.Duration     `json:"query_timeout"`
	EnableCompression bool              `json:"enable_compression"`
	EnableEncryption  bool              `json:"enable_encryption"`
	Tags              map[string]string `json:"tags"`
}

// DefaultHealthStoreConfig returns default configuration
func DefaultHealthStoreConfig() *HealthStoreConfig {
	return &HealthStoreConfig{
		Type:              "memory",
		Database:          "health",
		Table:             "health_history",
		MaxRetention:      30 * 24 * time.Hour, // 30 days
		CleanupInterval:   24 * time.Hour,      // Daily cleanup
		BatchSize:         100,
		MaxConnections:    10,
		QueryTimeout:      30 * time.Second,
		EnableCompression: true,
		EnableEncryption:  false,
		Tags:              make(map[string]string),
	}
}

// HealthStoreFactory creates health store instances
type HealthStoreFactory struct {
	logger    common.Logger
	metrics   common.Metrics
	container common.Container
}

// NewHealthStoreFactory creates a new health store factory
func NewHealthStoreFactory(logger common.Logger, metrics common.Metrics, container common.Container) *HealthStoreFactory {
	return &HealthStoreFactory{
		logger:    logger,
		metrics:   metrics,
		container: container,
	}
}

// CreateStore creates a health store based on configuration
func (f *HealthStoreFactory) CreateStore(config *HealthStoreConfig) (HealthStore, error) {
	switch config.Type {
	case "memory":
		return NewMemoryHealthStore(config, f.logger, f.metrics), nil
	case "database", "postgres", "mysql":
		return NewDatabaseHealthStore(config, f.logger, f.metrics, f.container)
	case "file":
		return NewFileHealthStore(config, f.logger, f.metrics)
	default:
		return nil, common.ErrInvalidConfig("store_type",
			fmt.Errorf("unsupported health store type: %s", config.Type))
	}
}

// HealthPersistenceService manages health data persistence
type HealthPersistenceService struct {
	store         HealthStore
	config        *HealthStoreConfig
	logger        common.Logger
	metrics       common.Metrics
	cleanupTicker *time.Ticker
	stopChan      chan struct{}
}

// NewHealthPersistenceService creates a new health persistence service
func NewHealthPersistenceService(store HealthStore, config *HealthStoreConfig, logger common.Logger, metrics common.Metrics) *HealthPersistenceService {
	return &HealthPersistenceService{
		store:    store,
		config:   config,
		logger:   logger,
		metrics:  metrics,
		stopChan: make(chan struct{}),
	}
}

// Start starts the persistence service
func (hps *HealthPersistenceService) Start(ctx context.Context) error {
	if hps.logger != nil {
		hps.logger.Info("starting health persistence service",
			logger.String("store_type", hps.config.Type),
			logger.Duration("cleanup_interval", hps.config.CleanupInterval),
			logger.Duration("max_retention", hps.config.MaxRetention),
		)
	}

	// OnStart cleanup ticker
	hps.cleanupTicker = time.NewTicker(hps.config.CleanupInterval)
	go hps.cleanupLoop()

	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.persistence_started").Inc()
	}

	return nil
}

// Stop stops the persistence service
func (hps *HealthPersistenceService) Stop(ctx context.Context) error {
	if hps.logger != nil {
		hps.logger.Info("stopping health persistence service")
	}

	// OnStop cleanup ticker
	if hps.cleanupTicker != nil {
		hps.cleanupTicker.Stop()
	}

	// Signal stop
	close(hps.stopChan)

	// Close store
	if err := hps.store.Close(); err != nil {
		if hps.logger != nil {
			hps.logger.Error("failed to close health store",
				logger.Error(err),
			)
		}
		return err
	}

	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.persistence_stopped").Inc()
	}

	return nil
}

// cleanupLoop runs periodic cleanup
func (hps *HealthPersistenceService) cleanupLoop() {
	for {
		select {
		case <-hps.cleanupTicker.C:
			hps.performCleanup()
		case <-hps.stopChan:
			return
		}
	}
}

// performCleanup performs periodic cleanup of old health data
func (hps *HealthPersistenceService) performCleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cutoff := time.Now().Add(-hps.config.MaxRetention)

	if hps.logger != nil {
		hps.logger.Debug("performing health data cleanup",
			logger.Time("cutoff", cutoff),
		)
	}

	if err := hps.store.Cleanup(ctx, cutoff); err != nil {
		if hps.logger != nil {
			hps.logger.Error("failed to cleanup health data",
				logger.Error(err),
			)
		}
		if hps.metrics != nil {
			hps.metrics.Counter("forge.health.cleanup_errors").Inc()
		}
		return
	}

	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.cleanup_completed").Inc()
	}
}

// GetStore returns the underlying health store
func (hps *HealthPersistenceService) GetStore() HealthStore {
	return hps.store
}

// StoreResult stores a health check result
func (hps *HealthPersistenceService) StoreResult(ctx context.Context, result *health.HealthResult) error {
	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.results_stored").Inc()
	}

	return hps.store.StoreResult(ctx, result)
}

// StoreReport stores a health report
func (hps *HealthPersistenceService) StoreReport(ctx context.Context, report *health.HealthReport) error {
	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.reports_stored").Inc()
	}

	return hps.store.StoreReport(ctx, report)
}

// GetHealthHistory returns health history for a check
func (hps *HealthPersistenceService) GetHealthHistory(ctx context.Context, checkName string, from, to time.Time, interval time.Duration) ([]*HealthHistoryPoint, error) {
	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.history_queries").Inc()
	}

	return hps.store.GetHealthHistory(ctx, checkName, from, to, interval)
}

// GetHealthTrend returns health trend analysis
func (hps *HealthPersistenceService) GetHealthTrend(ctx context.Context, checkName string, duration time.Duration) (*HealthTrend, error) {
	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.trend_queries").Inc()
	}

	return hps.store.GetHealthTrend(ctx, checkName, duration)
}

// GetHealthStatistics returns health statistics
func (hps *HealthPersistenceService) GetHealthStatistics(ctx context.Context, checkName string, from, to time.Time) (*HealthStatistics, error) {
	if hps.metrics != nil {
		hps.metrics.Counter("forge.health.statistics_queries").Inc()
	}

	return hps.store.GetHealthStatistics(ctx, checkName, from, to)
}

// HealthQueryBuilder provides a fluent interface for building health queries
type HealthQueryBuilder struct {
	store     HealthStore
	checkName string
	from      time.Time
	to        time.Time
	interval  time.Duration
	statuses  []health.HealthStatus
	limit     int
	offset    int
}

// NewHealthQueryBuilder creates a new health query builder
func NewHealthQueryBuilder(store HealthStore) *HealthQueryBuilder {
	return &HealthQueryBuilder{
		store: store,
		to:    time.Now(),
		from:  time.Now().Add(-24 * time.Hour), // Default to last 24 hours
	}
}

// ForCheck sets the check name to query
func (hqb *HealthQueryBuilder) ForCheck(checkName string) *HealthQueryBuilder {
	hqb.checkName = checkName
	return hqb
}

// Between sets the time range for the query
func (hqb *HealthQueryBuilder) Between(from, to time.Time) *HealthQueryBuilder {
	hqb.from = from
	hqb.to = to
	return hqb
}

// Last sets the query to cover the last duration
func (hqb *HealthQueryBuilder) Last(duration time.Duration) *HealthQueryBuilder {
	hqb.to = time.Now()
	hqb.from = hqb.to.Add(-duration)
	return hqb
}

// WithInterval sets the aggregation interval
func (hqb *HealthQueryBuilder) WithInterval(interval time.Duration) *HealthQueryBuilder {
	hqb.interval = interval
	return hqb
}

// WithStatuses filters by health statuses
func (hqb *HealthQueryBuilder) WithStatuses(statuses ...health.HealthStatus) *HealthQueryBuilder {
	hqb.statuses = statuses
	return hqb
}

// WithLimit sets the result limit
func (hqb *HealthQueryBuilder) WithLimit(limit int) *HealthQueryBuilder {
	hqb.limit = limit
	return hqb
}

// WithOffset sets the result offset
func (hqb *HealthQueryBuilder) WithOffset(offset int) *HealthQueryBuilder {
	hqb.offset = offset
	return hqb
}

// GetHistory executes the query and returns health history
func (hqb *HealthQueryBuilder) GetHistory(ctx context.Context) ([]*HealthHistoryPoint, error) {
	return hqb.store.GetHealthHistory(ctx, hqb.checkName, hqb.from, hqb.to, hqb.interval)
}

// GetTrend executes the query and returns health trend
func (hqb *HealthQueryBuilder) GetTrend(ctx context.Context) (*HealthTrend, error) {
	duration := hqb.to.Sub(hqb.from)
	return hqb.store.GetHealthTrend(ctx, hqb.checkName, duration)
}

// GetStatistics executes the query and returns health statistics
func (hqb *HealthQueryBuilder) GetStatistics(ctx context.Context) (*HealthStatistics, error) {
	return hqb.store.GetHealthStatistics(ctx, hqb.checkName, hqb.from, hqb.to)
}

// GetResults executes the query and returns health results
func (hqb *HealthQueryBuilder) GetResults(ctx context.Context) ([]*health.HealthResult, error) {
	return hqb.store.GetResults(ctx, hqb.checkName, hqb.from, hqb.to)
}

// HealthDataExporter exports health data in various formats
type HealthDataExporter struct {
	store HealthStore
}

// NewHealthDataExporter creates a new health data exporter
func NewHealthDataExporter(store HealthStore) *HealthDataExporter {
	return &HealthDataExporter{store: store}
}

// ExportFormat represents supported export formats
type ExportFormat string

const (
	ExportFormatJSON ExportFormat = "json"
	ExportFormatCSV  ExportFormat = "csv"
	ExportFormatXML  ExportFormat = "xml"
)

// ExportHealthData exports health data in the specified format
func (hde *HealthDataExporter) ExportHealthData(ctx context.Context, checkName string, from, to time.Time, format ExportFormat) ([]byte, error) {
	results, err := hde.store.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, err
	}

	switch format {
	case ExportFormatJSON:
		return json.Marshal(results)
	case ExportFormatCSV:
		return hde.exportCSV(results)
	case ExportFormatXML:
		return hde.exportXML(results)
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

// exportCSV exports health data as CSV
func (hde *HealthDataExporter) exportCSV(results []*health.HealthResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Write header
	header := []string{"timestamp", "check_name", "status", "message", "duration", "error"}
	if err := writer.Write(header); err != nil {
		return nil, err
	}

	// Write data
	for _, result := range results {
		record := []string{
			result.Timestamp.Format(time.RFC3339),
			result.Name,
			string(result.Status),
			result.Message,
			result.Duration.String(),
			func() string {
				return result.Error
			}(),
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	return buf.Bytes(), writer.Error()
}

// exportXML exports health data as XML
func (hde *HealthDataExporter) exportXML(results []*health.HealthResult) ([]byte, error) {
	type XMLResults struct {
		XMLName xml.Name               `xml:"health_results"`
		Results []*health.HealthResult `xml:"result"`
	}

	xmlResults := XMLResults{Results: results}
	return xml.MarshalIndent(xmlResults, "", "  ")
}
