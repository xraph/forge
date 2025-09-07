package persistence

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/pkg/common"
	health "github.com/xraph/forge/pkg/health/core"
	"github.com/xraph/forge/pkg/logger"
	"gorm.io/gorm"
)

// DatabaseHealthStore implements HealthStore using a database backend
type DatabaseHealthStore struct {
	db      *gorm.DB
	config  *HealthStoreConfig
	logger  common.Logger
	metrics common.Metrics
}

// HealthResultRecord represents a health result in the database
type HealthResultRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"index;not null" json:"name"`
	Status    string    `gorm:"not null" json:"status"`
	Message   string    `gorm:"type:text" json:"message"`
	Details   string    `gorm:"type:text" json:"details"`
	Timestamp time.Time `gorm:"index;not null" json:"timestamp"`
	Duration  int64     `json:"duration"` // Duration in nanoseconds
	Error     string    `gorm:"type:text" json:"error"`
	Critical  bool      `json:"critical"`
	Tags      string    `gorm:"type:text" json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for health results
func (HealthResultRecord) TableName() string {
	return "health_results"
}

// HealthReportRecord represents a health report in the database
type HealthReportRecord struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Overall     string    `gorm:"not null" json:"overall"`
	Services    string    `gorm:"type:text" json:"services"`
	Timestamp   time.Time `gorm:"index;not null" json:"timestamp"`
	Duration    int64     `json:"duration"` // Duration in nanoseconds
	Version     string    `json:"version"`
	Environment string    `json:"environment"`
	Hostname    string    `json:"hostname"`
	Uptime      int64     `json:"uptime"` // Uptime in nanoseconds
	Metadata    string    `gorm:"type:text" json:"metadata"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName returns the table name for health reports
func (HealthReportRecord) TableName() string {
	return "health_reports"
}

// NewDatabaseHealthStore creates a new database health store
func NewDatabaseHealthStore(config *HealthStoreConfig, logger common.Logger, metrics common.Metrics, container common.Container) (HealthStore, error) {
	if config == nil {
		config = DefaultHealthStoreConfig()
	}

	// Get database connection from container
	dbManager, err := container.ResolveNamed("database-manager")
	if err != nil {
		return nil, common.ErrContainerError("resolve_database", err)
	}

	// Get the database connection
	db, err := getDatabaseConnection(dbManager, config.Database)
	if err != nil {
		return nil, common.ErrContainerError("get_database", err)
	}

	store := &DatabaseHealthStore{
		db:      db,
		config:  config,
		logger:  logger,
		metrics: metrics,
	}

	// Auto-migrate tables
	if err := store.migrate(); err != nil {
		return nil, common.ErrContainerError("migrate", err)
	}

	return store, nil
}

// migrate creates the necessary database tables
func (dhs *DatabaseHealthStore) migrate() error {
	if err := dhs.db.AutoMigrate(&HealthResultRecord{}, &HealthReportRecord{}); err != nil {
		return fmt.Errorf("failed to migrate health tables: %w", err)
	}

	// Create indexes for better query performance
	if err := dhs.createIndexes(); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	if dhs.logger != nil {
		dhs.logger.Info("health database tables migrated successfully")
	}

	return nil
}

// createIndexes creates database indexes for better performance
func (dhs *DatabaseHealthStore) createIndexes() error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_health_results_name_timestamp ON health_results(name, timestamp)",
		"CREATE INDEX IF NOT EXISTS idx_health_results_status ON health_results(status)",
		"CREATE INDEX IF NOT EXISTS idx_health_results_critical ON health_results(critical)",
		"CREATE INDEX IF NOT EXISTS idx_health_reports_timestamp ON health_reports(timestamp)",
		"CREATE INDEX IF NOT EXISTS idx_health_reports_overall ON health_reports(overall)",
	}

	for _, index := range indexes {
		if err := dhs.db.Exec(index).Error; err != nil {
			if dhs.logger != nil {
				dhs.logger.Warn("failed to create index", logger.String("index", index), logger.Error(err))
			}
		}
	}

	return nil
}

// StoreResult stores a health check result
func (dhs *DatabaseHealthStore) StoreResult(ctx context.Context, result *health.HealthResult) error {
	record, err := dhs.healthResultToRecord(result)
	if err != nil {
		return fmt.Errorf("failed to convert health result: %w", err)
	}

	if err := dhs.db.WithContext(ctx).Create(record).Error; err != nil {
		if dhs.metrics != nil {
			dhs.metrics.Counter("forge.health.store_result_errors").Inc()
		}
		return fmt.Errorf("failed to store health result: %w", err)
	}

	if dhs.metrics != nil {
		dhs.metrics.Counter("forge.health.results_stored").Inc()
	}

	return nil
}

// StoreReport stores a health report
func (dhs *DatabaseHealthStore) StoreReport(ctx context.Context, report *health.HealthReport) error {
	record, err := dhs.healthReportToRecord(report)
	if err != nil {
		return fmt.Errorf("failed to convert health report: %w", err)
	}

	if err := dhs.db.WithContext(ctx).Create(record).Error; err != nil {
		if dhs.metrics != nil {
			dhs.metrics.Counter("forge.health.store_report_errors").Inc()
		}
		return fmt.Errorf("failed to store health report: %w", err)
	}

	if dhs.metrics != nil {
		dhs.metrics.Counter("forge.health.reports_stored").Inc()
	}

	return nil
}

// GetResult retrieves a specific health check result
func (dhs *DatabaseHealthStore) GetResult(ctx context.Context, checkName string, timestamp time.Time) (*health.HealthResult, error) {
	var record HealthResultRecord

	err := dhs.db.WithContext(ctx).
		Where("name = ? AND timestamp = ?", checkName, timestamp).
		First(&record).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, common.ErrServiceNotFound(fmt.Sprintf("health result for %s at %s", checkName, timestamp))
		}
		return nil, fmt.Errorf("failed to get health result: %w", err)
	}

	return dhs.recordToHealthResult(&record)
}

// GetResults retrieves health check results for a time range
func (dhs *DatabaseHealthStore) GetResults(ctx context.Context, checkName string, from, to time.Time) ([]*health.HealthResult, error) {
	var records []HealthResultRecord

	query := dhs.db.WithContext(ctx).
		Where("timestamp BETWEEN ? AND ?", from, to).
		Order("timestamp DESC")

	if checkName != "" {
		query = query.Where("name = ?", checkName)
	}

	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to get health results: %w", err)
	}

	results := make([]*health.HealthResult, len(records))
	for i, record := range records {
		result, err := dhs.recordToHealthResult(&record)
		if err != nil {
			return nil, fmt.Errorf("failed to convert record %d: %w", i, err)
		}
		results[i] = result
	}

	return results, nil
}

// GetReport retrieves a specific health report
func (dhs *DatabaseHealthStore) GetReport(ctx context.Context, timestamp time.Time) (*health.HealthReport, error) {
	var record HealthReportRecord

	err := dhs.db.WithContext(ctx).
		Where("timestamp = ?", timestamp).
		First(&record).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, common.ErrServiceNotFound(fmt.Sprintf("health report at %s", timestamp))
		}
		return nil, fmt.Errorf("failed to get health report: %w", err)
	}

	return dhs.recordToHealthReport(&record)
}

// GetReports retrieves health reports for a time range
func (dhs *DatabaseHealthStore) GetReports(ctx context.Context, from, to time.Time) ([]*health.HealthReport, error) {
	var records []HealthReportRecord

	err := dhs.db.WithContext(ctx).
		Where("timestamp BETWEEN ? AND ?", from, to).
		Order("timestamp DESC").
		Find(&records).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get health reports: %w", err)
	}

	reports := make([]*health.HealthReport, len(records))
	for i, record := range records {
		report, err := dhs.recordToHealthReport(&record)
		if err != nil {
			return nil, fmt.Errorf("failed to convert record %d: %w", i, err)
		}
		reports[i] = report
	}

	return reports, nil
}

// GetHealthHistory retrieves health history with aggregation
func (dhs *DatabaseHealthStore) GetHealthHistory(ctx context.Context, checkName string, from, to time.Time, interval time.Duration) ([]*HealthHistoryPoint, error) {
	// Build aggregation query based on interval
	var query strings.Builder
	var args []interface{}

	// Base query for aggregation
	query.WriteString(`
		SELECT 
			name,
			status,
			AVG(duration) as avg_duration,
			message,
			error,
			timestamp
		FROM health_results 
		WHERE timestamp BETWEEN ? AND ?
	`)
	args = append(args, from, to)

	if checkName != "" {
		query.WriteString(" AND name = ?")
		args = append(args, checkName)
	}

	// Group by time interval
	intervalSQL := dhs.getIntervalSQL(interval)
	query.WriteString(fmt.Sprintf(" GROUP BY name, status, %s ORDER BY timestamp", intervalSQL))

	var results []struct {
		Name        string    `json:"name"`
		Status      string    `json:"status"`
		AvgDuration float64   `json:"avg_duration"`
		Message     string    `json:"message"`
		Error       string    `json:"error"`
		Timestamp   time.Time `json:"timestamp"`
	}

	if err := dhs.db.WithContext(ctx).Raw(query.String(), args...).Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to get health history: %w", err)
	}

	// Convert to HealthHistoryPoint
	points := make([]*HealthHistoryPoint, len(results))
	for i, result := range results {
		duration := time.Duration(result.AvgDuration)
		points[i] = &HealthHistoryPoint{
			Timestamp: result.Timestamp,
			Status:    health.HealthStatus(result.Status),
			Duration:  duration,
			Message:   result.Message,
			Error:     result.Error,
		}
	}

	return points, nil
}

// GetHealthTrend analyzes health trends
func (dhs *DatabaseHealthStore) GetHealthTrend(ctx context.Context, checkName string, duration time.Duration) (*HealthTrend, error) {
	from := time.Now().Add(-duration)
	to := time.Now()

	// Get all results for the period
	results, err := dhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get results for trend analysis: %w", err)
	}

	if len(results) == 0 {
		return &HealthTrend{
			CheckName:   checkName,
			Period:      duration,
			TotalChecks: 0,
			Trend:       "unknown",
		}, nil
	}

	// Calculate trend statistics
	trend := &HealthTrend{
		CheckName:          checkName,
		Period:             duration,
		TotalChecks:        int64(len(results)),
		StatusDistribution: make(map[health.HealthStatus]int64),
	}

	var totalDuration time.Duration
	var successCount int64
	var consecutiveFailures int
	var lastStatus health.HealthStatus

	for _, result := range results {
		trend.StatusDistribution[result.Status]++
		totalDuration += result.Duration

		if result.Status == health.HealthStatusHealthy {
			successCount++
			consecutiveFailures = 0
		} else {
			if lastStatus != health.HealthStatusHealthy {
				consecutiveFailures++
			}
		}

		lastStatus = result.Status
		trend.LastCheck = result.Timestamp
	}

	trend.SuccessRate = float64(successCount) / float64(trend.TotalChecks)
	trend.ErrorRate = 1.0 - trend.SuccessRate
	trend.AvgDuration = totalDuration / time.Duration(trend.TotalChecks)
	trend.ConsecutiveFailures = consecutiveFailures

	// Determine trend direction
	if trend.SuccessRate > 0.95 {
		trend.Trend = "stable"
	} else if trend.SuccessRate > 0.80 {
		trend.Trend = "degrading"
	} else {
		trend.Trend = "unhealthy"
	}

	return trend, nil
}

// GetHealthStatistics calculates health statistics
func (dhs *DatabaseHealthStore) GetHealthStatistics(ctx context.Context, checkName string, from, to time.Time) (*HealthStatistics, error) {
	results, err := dhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get results for statistics: %w", err)
	}

	if len(results) == 0 {
		return &HealthStatistics{
			CheckName:    checkName,
			From:         from,
			To:           to,
			TotalChecks:  0,
			StatusCounts: make(map[health.HealthStatus]int64),
		}, nil
	}

	stats := &HealthStatistics{
		CheckName:       checkName,
		From:            from,
		To:              to,
		TotalChecks:     int64(len(results)),
		StatusCounts:    make(map[health.HealthStatus]int64),
		ErrorCategories: make(map[string]int64),
		MinDuration:     time.Hour,
		MaxDuration:     0,
	}

	var totalDuration time.Duration
	var uptimeStart, downtimeStart time.Time
	var isUp bool = true

	for _, result := range results {
		stats.StatusCounts[result.Status]++

		if result.Status == health.HealthStatusHealthy {
			stats.SuccessCount++
			if !isUp {
				stats.Downtime += result.Timestamp.Sub(downtimeStart)
				isUp = true
				uptimeStart = result.Timestamp
			}
		} else {
			stats.FailureCount++
			if isUp {
				stats.Uptime += result.Timestamp.Sub(uptimeStart)
				isUp = false
				downtimeStart = result.Timestamp
			}

			if result.Error != "" {
				stats.ErrorCategories[result.Error]++
			}
		}

		totalDuration += result.Duration

		if result.Duration < stats.MinDuration {
			stats.MinDuration = result.Duration
		}
		if result.Duration > stats.MaxDuration {
			stats.MaxDuration = result.Duration
		}
	}

	stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalChecks)
	stats.AvgDuration = totalDuration / time.Duration(stats.TotalChecks)

	// Calculate MTBF and MTTR
	if stats.FailureCount > 0 {
		stats.MTBF = stats.Uptime / time.Duration(stats.FailureCount)
		stats.MTTR = stats.Downtime / time.Duration(stats.FailureCount)
	}

	return stats, nil
}

// Cleanup removes old health data
func (dhs *DatabaseHealthStore) Cleanup(ctx context.Context, before time.Time) error {
	// Delete old results
	if err := dhs.db.WithContext(ctx).
		Where("timestamp < ?", before).
		Delete(&HealthResultRecord{}).Error; err != nil {
		return fmt.Errorf("failed to cleanup health results: %w", err)
	}

	// Delete old reports
	if err := dhs.db.WithContext(ctx).
		Where("timestamp < ?", before).
		Delete(&HealthReportRecord{}).Error; err != nil {
		return fmt.Errorf("failed to cleanup health reports: %w", err)
	}

	if dhs.metrics != nil {
		dhs.metrics.Counter("forge.health.cleanup_completed").Inc()
	}

	return nil
}

// GetSize returns the storage size
func (dhs *DatabaseHealthStore) GetSize(ctx context.Context) (int64, error) {
	var size int64

	// Get table sizes (implementation depends on database type)
	query := `
		SELECT 
			COALESCE(SUM(pg_total_relation_size(schemaname||'.'||tablename)), 0) as size
		FROM pg_tables 
		WHERE tablename IN ('health_results', 'health_reports')
	`

	if err := dhs.db.WithContext(ctx).Raw(query).Scan(&size).Error; err != nil {
		// Fallback to counting rows
		var resultCount, reportCount int64
		dhs.db.WithContext(ctx).Model(&HealthResultRecord{}).Count(&resultCount)
		dhs.db.WithContext(ctx).Model(&HealthReportRecord{}).Count(&reportCount)

		// Estimate size (rough calculation)
		size = (resultCount * 500) + (reportCount * 1000)
	}

	return size, nil
}

// Close closes the database connection
func (dhs *DatabaseHealthStore) Close() error {
	// GORM doesn't provide a direct Close method for the DB
	// The connection is managed by the database manager
	return nil
}

// Helper methods for conversion

func (dhs *DatabaseHealthStore) healthResultToRecord(result *health.HealthResult) (*HealthResultRecord, error) {
	details, err := json.Marshal(result.Details)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal details: %w", err)
	}

	tags, err := json.Marshal(result.Tags)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tags: %w", err)
	}

	return &HealthResultRecord{
		Name:      result.Name,
		Status:    string(result.Status),
		Message:   result.Message,
		Details:   string(details),
		Timestamp: result.Timestamp,
		Duration:  int64(result.Duration),
		Error:     result.Error,
		Critical:  result.Critical,
		Tags:      string(tags),
	}, nil
}

func (dhs *DatabaseHealthStore) recordToHealthResult(record *HealthResultRecord) (*health.HealthResult, error) {
	var details map[string]interface{}
	if err := json.Unmarshal([]byte(record.Details), &details); err != nil {
		details = make(map[string]interface{})
	}

	var tags map[string]string
	if err := json.Unmarshal([]byte(record.Tags), &tags); err != nil {
		tags = make(map[string]string)
	}

	return &health.HealthResult{
		Name:      record.Name,
		Status:    health.HealthStatus(record.Status),
		Message:   record.Message,
		Details:   details,
		Timestamp: record.Timestamp,
		Duration:  time.Duration(record.Duration),
		Error:     record.Error,
		Critical:  record.Critical,
		Tags:      tags,
	}, nil
}

func (dhs *DatabaseHealthStore) healthReportToRecord(report *health.HealthReport) (*HealthReportRecord, error) {
	services, err := json.Marshal(report.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal services: %w", err)
	}

	metadata, err := json.Marshal(report.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return &HealthReportRecord{
		Overall:     string(report.Overall),
		Services:    string(services),
		Timestamp:   report.Timestamp,
		Duration:    int64(report.Duration),
		Version:     report.Version,
		Environment: report.Environment,
		Hostname:    report.Hostname,
		Uptime:      int64(report.Uptime),
		Metadata:    string(metadata),
	}, nil
}

func (dhs *DatabaseHealthStore) recordToHealthReport(record *HealthReportRecord) (*health.HealthReport, error) {
	var services map[string]*health.HealthResult
	if err := json.Unmarshal([]byte(record.Services), &services); err != nil {
		services = make(map[string]*health.HealthResult)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(record.Metadata), &metadata); err != nil {
		metadata = make(map[string]interface{})
	}

	return &health.HealthReport{
		Overall:     health.HealthStatus(record.Overall),
		Services:    services,
		Timestamp:   record.Timestamp,
		Duration:    time.Duration(record.Duration),
		Version:     record.Version,
		Environment: record.Environment,
		Hostname:    record.Hostname,
		Uptime:      time.Duration(record.Uptime),
		Metadata:    metadata,
	}, nil
}

func (dhs *DatabaseHealthStore) getIntervalSQL(interval time.Duration) string {
	// Convert interval to PostgreSQL interval format
	switch {
	case interval >= 24*time.Hour:
		return "DATE_TRUNC('day', timestamp)"
	case interval >= time.Hour:
		return "DATE_TRUNC('hour', timestamp)"
	case interval >= time.Minute:
		return "DATE_TRUNC('minute', timestamp)"
	default:
		return "DATE_TRUNC('second', timestamp)"
	}
}

func getDatabaseConnection(dbManager interface{}, database string) (*gorm.DB, error) {
	// This is a simplified implementation
	// In reality, you would get the specific database connection from the database manager
	// For now, assume the database manager has a method to get GORM connection

	type DatabaseManager interface {
		GetGORMConnection(database string) (*gorm.DB, error)
	}

	if manager, ok := dbManager.(DatabaseManager); ok {
		return manager.GetGORMConnection(database)
	}

	return nil, fmt.Errorf("database manager does not support GORM connections")
}

// MemoryHealthStore implements HealthStore using in-memory storage
type MemoryHealthStore struct {
	results []*health.HealthResult
	reports []*health.HealthReport
	config  *HealthStoreConfig
	logger  common.Logger
	metrics common.Metrics
}

// NewMemoryHealthStore creates a new in-memory health store
func NewMemoryHealthStore(config *HealthStoreConfig, logger common.Logger, metrics common.Metrics) HealthStore {
	return &MemoryHealthStore{
		results: make([]*health.HealthResult, 0),
		reports: make([]*health.HealthReport, 0),
		config:  config,
		logger:  logger,
		metrics: metrics,
	}
}

// StoreResult stores a health result in memory
func (mhs *MemoryHealthStore) StoreResult(ctx context.Context, result *health.HealthResult) error {
	mhs.results = append(mhs.results, result)

	// Apply retention policy
	if len(mhs.results) > mhs.config.BatchSize*10 {
		cutoff := time.Now().Add(-mhs.config.MaxRetention)
		mhs.results = mhs.filterResultsByTime(mhs.results, cutoff, time.Now())
	}

	if mhs.metrics != nil {
		mhs.metrics.Counter("forge.health.results_stored").Inc()
	}

	return nil
}

// StoreReport stores a health report in memory
func (mhs *MemoryHealthStore) StoreReport(ctx context.Context, report *health.HealthReport) error {
	mhs.reports = append(mhs.reports, report)

	// Apply retention policy
	if len(mhs.reports) > mhs.config.BatchSize {
		cutoff := time.Now().Add(-mhs.config.MaxRetention)
		mhs.reports = mhs.filterReportsByTime(mhs.reports, cutoff, time.Now())
	}

	if mhs.metrics != nil {
		mhs.metrics.Counter("forge.health.reports_stored").Inc()
	}

	return nil
}

// GetResult retrieves a specific health result
func (mhs *MemoryHealthStore) GetResult(ctx context.Context, checkName string, timestamp time.Time) (*health.HealthResult, error) {
	for _, result := range mhs.results {
		if result.Name == checkName && result.Timestamp.Equal(timestamp) {
			return result, nil
		}
	}
	return nil, common.ErrServiceNotFound(fmt.Sprintf("health result for %s at %s", checkName, timestamp))
}

// GetResults retrieves health results for a time range
func (mhs *MemoryHealthStore) GetResults(ctx context.Context, checkName string, from, to time.Time) ([]*health.HealthResult, error) {
	var results []*health.HealthResult

	for _, result := range mhs.results {
		if result.Timestamp.After(from) && result.Timestamp.Before(to) {
			if checkName == "" || result.Name == checkName {
				results = append(results, result)
			}
		}
	}

	return results, nil
}

// GetReport retrieves a specific health report
func (mhs *MemoryHealthStore) GetReport(ctx context.Context, timestamp time.Time) (*health.HealthReport, error) {
	for _, report := range mhs.reports {
		if report.Timestamp.Equal(timestamp) {
			return report, nil
		}
	}
	return nil, common.ErrServiceNotFound(fmt.Sprintf("health report at %s", timestamp))
}

// GetReports retrieves health reports for a time range
func (mhs *MemoryHealthStore) GetReports(ctx context.Context, from, to time.Time) ([]*health.HealthReport, error) {
	var reports []*health.HealthReport

	for _, report := range mhs.reports {
		if report.Timestamp.After(from) && report.Timestamp.Before(to) {
			reports = append(reports, report)
		}
	}

	return reports, nil
}

// GetHealthHistory retrieves health history (simplified for memory store)
func (mhs *MemoryHealthStore) GetHealthHistory(ctx context.Context, checkName string, from, to time.Time, interval time.Duration) ([]*HealthHistoryPoint, error) {
	results, err := mhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, err
	}

	// Convert results to history points
	points := make([]*HealthHistoryPoint, len(results))
	for i, result := range results {
		points[i] = &HealthHistoryPoint{
			Timestamp: result.Timestamp,
			Status:    result.Status,
			Duration:  result.Duration,
			Message:   result.Message,
			Error:     result.Error,
			Details:   result.Details,
		}
	}

	return points, nil
}

// GetHealthTrend analyzes health trends (simplified)
func (mhs *MemoryHealthStore) GetHealthTrend(ctx context.Context, checkName string, duration time.Duration) (*HealthTrend, error) {
	from := time.Now().Add(-duration)
	to := time.Now()

	results, err := mhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return &HealthTrend{
			CheckName:   checkName,
			Period:      duration,
			TotalChecks: 0,
			Trend:       "unknown",
		}, nil
	}

	trend := &HealthTrend{
		CheckName:          checkName,
		Period:             duration,
		TotalChecks:        int64(len(results)),
		StatusDistribution: make(map[health.HealthStatus]int64),
	}

	var successCount int64
	var totalDuration time.Duration

	for _, result := range results {
		trend.StatusDistribution[result.Status]++
		totalDuration += result.Duration

		if result.Status == health.HealthStatusHealthy {
			successCount++
		}

		if result.Timestamp.After(trend.LastCheck) {
			trend.LastCheck = result.Timestamp
		}
	}

	trend.SuccessRate = float64(successCount) / float64(trend.TotalChecks)
	trend.ErrorRate = 1.0 - trend.SuccessRate
	trend.AvgDuration = totalDuration / time.Duration(trend.TotalChecks)

	if trend.SuccessRate > 0.95 {
		trend.Trend = "stable"
	} else if trend.SuccessRate > 0.80 {
		trend.Trend = "degrading"
	} else {
		trend.Trend = "unhealthy"
	}

	return trend, nil
}

// GetHealthStatistics calculates health statistics (simplified)
func (mhs *MemoryHealthStore) GetHealthStatistics(ctx context.Context, checkName string, from, to time.Time) (*HealthStatistics, error) {
	results, err := mhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return &HealthStatistics{
			CheckName:    checkName,
			From:         from,
			To:           to,
			TotalChecks:  0,
			StatusCounts: make(map[health.HealthStatus]int64),
		}, nil
	}

	stats := &HealthStatistics{
		CheckName:       checkName,
		From:            from,
		To:              to,
		TotalChecks:     int64(len(results)),
		StatusCounts:    make(map[health.HealthStatus]int64),
		ErrorCategories: make(map[string]int64),
		MinDuration:     time.Hour,
		MaxDuration:     0,
	}

	var totalDuration time.Duration

	for _, result := range results {
		stats.StatusCounts[result.Status]++
		totalDuration += result.Duration

		if result.Status == health.HealthStatusHealthy {
			stats.SuccessCount++
		} else {
			stats.FailureCount++
		}

		if result.Duration < stats.MinDuration {
			stats.MinDuration = result.Duration
		}
		if result.Duration > stats.MaxDuration {
			stats.MaxDuration = result.Duration
		}
	}

	stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalChecks)
	stats.AvgDuration = totalDuration / time.Duration(stats.TotalChecks)

	return stats, nil
}

// Cleanup removes old data
func (mhs *MemoryHealthStore) Cleanup(ctx context.Context, before time.Time) error {
	mhs.results = mhs.filterResultsByTime(mhs.results, before, time.Now())
	mhs.reports = mhs.filterReportsByTime(mhs.reports, before, time.Now())
	return nil
}

// GetSize returns the memory usage
func (mhs *MemoryHealthStore) GetSize(ctx context.Context) (int64, error) {
	// Rough estimate of memory usage
	return int64(len(mhs.results)*500 + len(mhs.reports)*1000), nil
}

// Close closes the memory store
func (mhs *MemoryHealthStore) Close() error {
	mhs.results = nil
	mhs.reports = nil
	return nil
}

// Helper methods for memory store

func (mhs *MemoryHealthStore) filterResultsByTime(results []*health.HealthResult, from, to time.Time) []*health.HealthResult {
	var filtered []*health.HealthResult
	for _, result := range results {
		if result.Timestamp.After(from) && result.Timestamp.Before(to) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

// FileHealthStore implements HealthStore using file-based storage
type FileHealthStore struct {
	config      *HealthStoreConfig
	logger      common.Logger
	metrics     common.Metrics
	resultsFile string
	reportsFile string
	mu          sync.RWMutex
}

// NewFileHealthStore creates a new file health store
func NewFileHealthStore(config *HealthStoreConfig, logger common.Logger, metrics common.Metrics) (HealthStore, error) {
	if config == nil {
		config = DefaultHealthStoreConfig()
	}

	// Set default file paths if not provided
	if config.ConnectionString == "" {
		config.ConnectionString = "./health_data"
	}

	store := &FileHealthStore{
		config:      config,
		logger:      logger,
		metrics:     metrics,
		resultsFile: config.ConnectionString + "/results.json",
		reportsFile: config.ConnectionString + "/reports.json",
	}

	// Create directory if it doesn't exist
	if err := store.ensureDirectory(); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	return store, nil
}

// ensureDirectory creates the storage directory if it doesn't exist
func (fhs *FileHealthStore) ensureDirectory() error {
	return fmt.Errorf("file system operations not implemented in this context")
}

// StoreResult stores a health result to file
func (fhs *FileHealthStore) StoreResult(ctx context.Context, result *health.HealthResult) error {
	fhs.mu.Lock()
	defer fhs.mu.Unlock()

	// Load existing results
	results, err := fhs.loadResults()
	if err != nil {
		return fmt.Errorf("failed to load existing results: %w", err)
	}

	// Add new result
	results = append(results, result)

	// Apply retention policy
	if len(results) > fhs.config.BatchSize*10 {
		cutoff := time.Now().Add(-fhs.config.MaxRetention)
		results = fhs.filterResultsByTime(results, cutoff, time.Now())
	}

	// Save results
	if err := fhs.saveResults(results); err != nil {
		return fmt.Errorf("failed to save results: %w", err)
	}

	if fhs.metrics != nil {
		fhs.metrics.Counter("forge.health.results_stored").Inc()
	}

	return nil
}

// StoreReport stores a health report to file
func (fhs *FileHealthStore) StoreReport(ctx context.Context, report *health.HealthReport) error {
	fhs.mu.Lock()
	defer fhs.mu.Unlock()

	// Load existing reports
	reports, err := fhs.loadReports()
	if err != nil {
		return fmt.Errorf("failed to load existing reports: %w", err)
	}

	// Add new report
	reports = append(reports, report)

	// Apply retention policy
	if len(reports) > fhs.config.BatchSize {
		cutoff := time.Now().Add(-fhs.config.MaxRetention)
		reports = fhs.filterReportsByTime(reports, cutoff, time.Now())
	}

	// Save reports
	if err := fhs.saveReports(reports); err != nil {
		return fmt.Errorf("failed to save reports: %w", err)
	}

	if fhs.metrics != nil {
		fhs.metrics.Counter("forge.health.reports_stored").Inc()
	}

	return nil
}

// GetResult retrieves a specific health result
func (fhs *FileHealthStore) GetResult(ctx context.Context, checkName string, timestamp time.Time) (*health.HealthResult, error) {
	fhs.mu.RLock()
	defer fhs.mu.RUnlock()

	results, err := fhs.loadResults()
	if err != nil {
		return nil, fmt.Errorf("failed to load results: %w", err)
	}

	for _, result := range results {
		if result.Name == checkName && result.Timestamp.Equal(timestamp) {
			return result, nil
		}
	}

	return nil, common.ErrServiceNotFound(fmt.Sprintf("health result for %s at %s", checkName, timestamp))
}

// GetResults retrieves health results for a time range
func (fhs *FileHealthStore) GetResults(ctx context.Context, checkName string, from, to time.Time) ([]*health.HealthResult, error) {
	fhs.mu.RLock()
	defer fhs.mu.RUnlock()

	results, err := fhs.loadResults()
	if err != nil {
		return nil, fmt.Errorf("failed to load results: %w", err)
	}

	var filtered []*health.HealthResult
	for _, result := range results {
		if result.Timestamp.After(from) && result.Timestamp.Before(to) {
			if checkName == "" || result.Name == checkName {
				filtered = append(filtered, result)
			}
		}
	}

	return filtered, nil
}

// GetReport retrieves a specific health report
func (fhs *FileHealthStore) GetReport(ctx context.Context, timestamp time.Time) (*health.HealthReport, error) {
	fhs.mu.RLock()
	defer fhs.mu.RUnlock()

	reports, err := fhs.loadReports()
	if err != nil {
		return nil, fmt.Errorf("failed to load reports: %w", err)
	}

	for _, report := range reports {
		if report.Timestamp.Equal(timestamp) {
			return report, nil
		}
	}

	return nil, common.ErrServiceNotFound(fmt.Sprintf("health report at %s", timestamp))
}

// GetReports retrieves health reports for a time range
func (fhs *FileHealthStore) GetReports(ctx context.Context, from, to time.Time) ([]*health.HealthReport, error) {
	fhs.mu.RLock()
	defer fhs.mu.RUnlock()

	reports, err := fhs.loadReports()
	if err != nil {
		return nil, fmt.Errorf("failed to load reports: %w", err)
	}

	var filtered []*health.HealthReport
	for _, report := range reports {
		if report.Timestamp.After(from) && report.Timestamp.Before(to) {
			filtered = append(filtered, report)
		}
	}

	return filtered, nil
}

// GetHealthHistory retrieves health history (simplified for file store)
func (fhs *FileHealthStore) GetHealthHistory(ctx context.Context, checkName string, from, to time.Time, interval time.Duration) ([]*HealthHistoryPoint, error) {
	results, err := fhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, err
	}

	// Convert results to history points
	points := make([]*HealthHistoryPoint, len(results))
	for i, result := range results {
		points[i] = &HealthHistoryPoint{
			Timestamp: result.Timestamp,
			Status:    result.Status,
			Duration:  result.Duration,
			Message:   result.Message,
			Error:     result.Error,
			Details:   result.Details,
		}
	}

	return points, nil
}

// GetHealthTrend analyzes health trends (simplified)
func (fhs *FileHealthStore) GetHealthTrend(ctx context.Context, checkName string, duration time.Duration) (*HealthTrend, error) {
	from := time.Now().Add(-duration)
	to := time.Now()

	results, err := fhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return &HealthTrend{
			CheckName:   checkName,
			Period:      duration,
			TotalChecks: 0,
			Trend:       "unknown",
		}, nil
	}

	trend := &HealthTrend{
		CheckName:          checkName,
		Period:             duration,
		TotalChecks:        int64(len(results)),
		StatusDistribution: make(map[health.HealthStatus]int64),
	}

	var successCount int64
	var totalDuration time.Duration

	for _, result := range results {
		trend.StatusDistribution[result.Status]++
		totalDuration += result.Duration

		if result.Status == health.HealthStatusHealthy {
			successCount++
		}

		if result.Timestamp.After(trend.LastCheck) {
			trend.LastCheck = result.Timestamp
		}
	}

	trend.SuccessRate = float64(successCount) / float64(trend.TotalChecks)
	trend.ErrorRate = 1.0 - trend.SuccessRate
	trend.AvgDuration = totalDuration / time.Duration(trend.TotalChecks)

	if trend.SuccessRate > 0.95 {
		trend.Trend = "stable"
	} else if trend.SuccessRate > 0.80 {
		trend.Trend = "degrading"
	} else {
		trend.Trend = "unhealthy"
	}

	return trend, nil
}

// GetHealthStatistics calculates health statistics (simplified)
func (fhs *FileHealthStore) GetHealthStatistics(ctx context.Context, checkName string, from, to time.Time) (*HealthStatistics, error) {
	results, err := fhs.GetResults(ctx, checkName, from, to)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return &HealthStatistics{
			CheckName:    checkName,
			From:         from,
			To:           to,
			TotalChecks:  0,
			StatusCounts: make(map[health.HealthStatus]int64),
		}, nil
	}

	stats := &HealthStatistics{
		CheckName:       checkName,
		From:            from,
		To:              to,
		TotalChecks:     int64(len(results)),
		StatusCounts:    make(map[health.HealthStatus]int64),
		ErrorCategories: make(map[string]int64),
		MinDuration:     time.Hour,
		MaxDuration:     0,
	}

	var totalDuration time.Duration

	for _, result := range results {
		stats.StatusCounts[result.Status]++
		totalDuration += result.Duration

		if result.Status == health.HealthStatusHealthy {
			stats.SuccessCount++
		} else {
			stats.FailureCount++
		}

		if result.Duration < stats.MinDuration {
			stats.MinDuration = result.Duration
		}
		if result.Duration > stats.MaxDuration {
			stats.MaxDuration = result.Duration
		}
	}

	stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalChecks)
	stats.AvgDuration = totalDuration / time.Duration(stats.TotalChecks)

	return stats, nil
}

// Cleanup removes old data
func (fhs *FileHealthStore) Cleanup(ctx context.Context, before time.Time) error {
	fhs.mu.Lock()
	defer fhs.mu.Unlock()

	// Clean up results
	results, err := fhs.loadResults()
	if err != nil {
		return fmt.Errorf("failed to load results: %w", err)
	}

	results = fhs.filterResultsByTime(results, before, time.Now())
	if err := fhs.saveResults(results); err != nil {
		return fmt.Errorf("failed to save cleaned results: %w", err)
	}

	// Clean up reports
	reports, err := fhs.loadReports()
	if err != nil {
		return fmt.Errorf("failed to load reports: %w", err)
	}

	reports = fhs.filterReportsByTime(reports, before, time.Now())
	if err := fhs.saveReports(reports); err != nil {
		return fmt.Errorf("failed to save cleaned reports: %w", err)
	}

	return nil
}

// GetSize returns the file size
func (fhs *FileHealthStore) GetSize(ctx context.Context) (int64, error) {
	// In a real implementation, this would check file sizes
	// For now, return an estimate
	return 1000, nil
}

// Close closes the file store
func (fhs *FileHealthStore) Close() error {
	return nil
}

// Helper methods for file operations

func (fhs *FileHealthStore) loadResults() ([]*health.HealthResult, error) {
	// In a real implementation, this would load from file
	// For now, return empty slice
	return make([]*health.HealthResult, 0), nil
}

func (fhs *FileHealthStore) saveResults(results []*health.HealthResult) error {
	// In a real implementation, this would save to file
	return nil
}

func (fhs *FileHealthStore) loadReports() ([]*health.HealthReport, error) {
	// In a real implementation, this would load from file
	// For now, return empty slice
	return make([]*health.HealthReport, 0), nil
}

func (fhs *FileHealthStore) saveReports(reports []*health.HealthReport) error {
	// In a real implementation, this would save to file
	return nil
}

func (fhs *FileHealthStore) filterResultsByTime(results []*health.HealthResult, from, to time.Time) []*health.HealthResult {
	var filtered []*health.HealthResult
	for _, result := range results {
		if result.Timestamp.After(from) && result.Timestamp.Before(to) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func (fhs *FileHealthStore) filterReportsByTime(reports []*health.HealthReport, from, to time.Time) []*health.HealthReport {
	var filtered []*health.HealthReport
	for _, report := range reports {
		if report.Timestamp.After(from) && report.Timestamp.Before(to) {
			filtered = append(filtered, report)
		}
	}
	return filtered
}

func (mhs *MemoryHealthStore) filterReportsByTime(reports []*health.HealthReport, from, to time.Time) []*health.HealthReport {
	var filtered []*health.HealthReport
	for _, report := range reports {
		if report.Timestamp.After(from) && report.Timestamp.Before(to) {
			filtered = append(filtered, report)
		}
	}
	return filtered
}
