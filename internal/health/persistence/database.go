package persistence

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// HealthResultRecord represents a health result in the database.
type HealthResultRecord struct {
	ID        uint      `gorm:"primaryKey"     json:"id"`
	Name      string    `gorm:"index;not null" json:"name"`
	Status    string    `gorm:"not null"       json:"status"`
	Message   string    `gorm:"type:text"      json:"message"`
	Details   string    `gorm:"type:text"      json:"details"`
	Timestamp time.Time `gorm:"index;not null" json:"timestamp"`
	Duration  int64     `json:"duration"` // Duration in nanoseconds
	Error     string    `gorm:"type:text"      json:"error"`
	Critical  bool      `json:"critical"`
	Tags      string    `gorm:"type:text"      json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the table name for health results.
func (HealthResultRecord) TableName() string {
	return "health_results"
}

// HealthReportRecord represents a health report in the database.
type HealthReportRecord struct {
	ID          uint      `gorm:"primaryKey"     json:"id"`
	Overall     string    `gorm:"not null"       json:"overall"`
	Services    string    `gorm:"type:text"      json:"services"`
	Timestamp   time.Time `gorm:"index;not null" json:"timestamp"`
	Duration    int64     `json:"duration"` // Duration in nanoseconds
	Version     string    `json:"version"`
	Environment string    `json:"environment"`
	Hostname    string    `json:"hostname"`
	Uptime      int64     `json:"uptime"` // Uptime in nanoseconds
	Metadata    string    `gorm:"type:text"      json:"metadata"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName returns the table name for health reports.
func (HealthReportRecord) TableName() string {
	return "health_reports"
}

// MemoryHealthStore implements HealthStore using in-memory storage.
type MemoryHealthStore struct {
	results []*health.HealthResult
	reports []*health.HealthReport
	config  *HealthStoreConfig
	logger  logger.Logger
	metrics shared.Metrics
}

// NewMemoryHealthStore creates a new in-memory health store.
func NewMemoryHealthStore(config *HealthStoreConfig, logger logger.Logger, metrics shared.Metrics) HealthStore {
	return &MemoryHealthStore{
		results: make([]*health.HealthResult, 0),
		reports: make([]*health.HealthReport, 0),
		config:  config,
		logger:  logger,
		metrics: metrics,
	}
}

// StoreResult stores a health result in memory.
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

// StoreReport stores a health report in memory.
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

// GetResult retrieves a specific health result.
func (mhs *MemoryHealthStore) GetResult(ctx context.Context, checkName string, timestamp time.Time) (*health.HealthResult, error) {
	for _, result := range mhs.results {
		if result.Name == checkName && result.Timestamp.Equal(timestamp) {
			return result, nil
		}
	}

	return nil, errors.ErrServiceNotFound(fmt.Sprintf("health result for %s at %s", checkName, timestamp))
}

// GetResults retrieves health results for a time range.
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

// GetReport retrieves a specific health report.
func (mhs *MemoryHealthStore) GetReport(ctx context.Context, timestamp time.Time) (*health.HealthReport, error) {
	for _, report := range mhs.reports {
		if report.Timestamp.Equal(timestamp) {
			return report, nil
		}
	}

	return nil, errors.ErrServiceNotFound(fmt.Sprintf("health report at %s", timestamp))
}

// GetReports retrieves health reports for a time range.
func (mhs *MemoryHealthStore) GetReports(ctx context.Context, from, to time.Time) ([]*health.HealthReport, error) {
	var reports []*health.HealthReport

	for _, report := range mhs.reports {
		if report.Timestamp.After(from) && report.Timestamp.Before(to) {
			reports = append(reports, report)
		}
	}

	return reports, nil
}

// GetHealthHistory retrieves health history (simplified for memory store).
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

// GetHealthTrend analyzes health trends (simplified).
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

	var (
		successCount  int64
		totalDuration time.Duration
	)

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

// GetHealthStatistics calculates health statistics (simplified).
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

// Cleanup removes old data.
func (mhs *MemoryHealthStore) Cleanup(ctx context.Context, before time.Time) error {
	mhs.results = mhs.filterResultsByTime(mhs.results, before, time.Now())
	mhs.reports = mhs.filterReportsByTime(mhs.reports, before, time.Now())

	return nil
}

// GetSize returns the memory usage.
func (mhs *MemoryHealthStore) GetSize(ctx context.Context) (int64, error) {
	// Rough estimate of memory usage
	return int64(len(mhs.results)*500 + len(mhs.reports)*1000), nil
}

// Close closes the memory store.
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

// FileHealthStore implements HealthStore using file-based storage.
type FileHealthStore struct {
	config      *HealthStoreConfig
	logger      logger.Logger
	metrics     shared.Metrics
	resultsFile string
	reportsFile string
	mu          sync.RWMutex
}

// NewFileHealthStore creates a new file health store.
func NewFileHealthStore(config *HealthStoreConfig, logger logger.Logger, metrics shared.Metrics) (HealthStore, error) {
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

// ensureDirectory creates the storage directory if it doesn't exist.
func (fhs *FileHealthStore) ensureDirectory() error {
	return errors.New("file system operations not implemented in this context")
}

// StoreResult stores a health result to file.
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

// StoreReport stores a health report to file.
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

// GetResult retrieves a specific health result.
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

	return nil, errors.ErrServiceNotFound(fmt.Sprintf("health result for %s at %s", checkName, timestamp))
}

// GetResults retrieves health results for a time range.
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

// GetReport retrieves a specific health report.
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

	return nil, errors.ErrServiceNotFound(fmt.Sprintf("health report at %s", timestamp))
}

// GetReports retrieves health reports for a time range.
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

// GetHealthHistory retrieves health history (simplified for file store).
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

// GetHealthTrend analyzes health trends (simplified).
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

	var (
		successCount  int64
		totalDuration time.Duration
	)

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

// GetHealthStatistics calculates health statistics (simplified).
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

// Cleanup removes old data.
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

// GetSize returns the file size.
func (fhs *FileHealthStore) GetSize(ctx context.Context) (int64, error) {
	// In a real implementation, this would check file sizes
	// For now, return an estimate
	return 1000, nil
}

// Close closes the file store.
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
