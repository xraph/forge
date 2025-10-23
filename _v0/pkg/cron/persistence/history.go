package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cron "github.com/xraph/forge/v0/pkg/cron/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// HistoryManager manages job execution history
type HistoryManager struct {
	store  *DatabaseStore
	logger logger.Logger
}

// NewHistoryManager creates a new history manager
func NewHistoryManager(store *DatabaseStore, logger logger.Logger) *HistoryManager {
	return &HistoryManager{
		store:  store,
		logger: logger,
	}
}

// CreateExecution creates a new job execution record
func (hm *HistoryManager) CreateExecution(ctx context.Context, execution *cron.JobExecution) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (
			id, job_id, node_id, status, start_time, end_time, duration_ms,
			output, error, attempt, metadata, tags, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
	`, hm.store.config.ExecutionsTable)

	metadataJSON, err := json.Marshal(execution.Metadata)
	if err != nil {
		return NewStoreError("create_execution", "failed to marshal metadata", err)
	}

	tagsJSON, err := json.Marshal(execution.Tags)
	if err != nil {
		return NewStoreError("create_execution", "failed to marshal tags", err)
	}

	var durationMs *int64
	if execution.Duration > 0 {
		ms := int64(execution.Duration.Nanoseconds() / 1000000)
		durationMs = &ms
	}

	var startTime, endTime *time.Time
	if !execution.StartTime.IsZero() {
		startTime = &execution.StartTime
	}
	if !execution.EndTime.IsZero() {
		endTime = &execution.EndTime
	}

	_, err = hm.store.db.DB().(*sql.DB).ExecContext(ctx, query,
		execution.ID,
		execution.JobID,
		execution.NodeID,
		execution.Status,
		startTime,
		endTime,
		durationMs,
		execution.Output,
		execution.Error,
		execution.Attempt,
		metadataJSON,
		tagsJSON,
		execution.CreatedAt,
		execution.UpdatedAt,
	)

	if err != nil {
		return NewStoreError("create_execution", "failed to insert execution", err)
	}

	hm.store.metrics.ExecutionsCreated++
	hm.store.metrics.QueriesExecuted++

	if hm.logger != nil {
		hm.logger.Info("execution created in database",
			logger.String("execution_id", execution.ID),
			logger.String("job_id", execution.JobID),
			logger.String("node_id", execution.NodeID),
			logger.String("status", string(execution.Status)),
		)
	}

	return nil
}

// GetExecution retrieves a job execution by ID
func (hm *HistoryManager) GetExecution(ctx context.Context, executionID string) (*cron.JobExecution, error) {
	query := fmt.Sprintf(`
		SELECT id, job_id, node_id, status, start_time, end_time, duration_ms,
			   output, error, attempt, metadata, tags, created_at, updated_at
		FROM %s WHERE id = $1
	`, hm.store.config.ExecutionsTable)

	row := hm.store.db.DB().(*sql.DB).QueryRowContext(ctx, query, executionID)

	execution, err := hm.scanExecution(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrExecutionNotFound
		}
		return nil, NewStoreError("get_execution", "failed to scan execution", err)
	}

	hm.store.metrics.QueriesExecuted++
	return execution, nil
}

// GetExecutions retrieves executions based on filter criteria
func (hm *HistoryManager) GetExecutions(ctx context.Context, filter *cron.ExecutionFilter) ([]*cron.JobExecution, error) {
	query, args := hm.buildExecutionQuery(filter)

	rows, err := hm.store.db.DB().(*sql.DB).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, NewStoreError("get_executions", "failed to execute query", err)
	}
	defer rows.Close()

	var executions []*cron.JobExecution
	for rows.Next() {
		execution, err := hm.scanExecution(rows)
		if err != nil {
			return nil, NewStoreError("get_executions", "failed to scan execution", err)
		}
		executions = append(executions, execution)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_executions", "row iteration error", err)
	}

	hm.store.metrics.QueriesExecuted++
	return executions, nil
}

// UpdateExecution updates an existing job execution
func (hm *HistoryManager) UpdateExecution(ctx context.Context, execution *cron.JobExecution) error {
	query := fmt.Sprintf(`
		UPDATE %s SET
			status = $2, start_time = $3, end_time = $4, duration_ms = $5,
			output = $6, error = $7, attempt = $8, metadata = $9, tags = $10, updated_at = $11
		WHERE id = $1
	`, hm.store.config.ExecutionsTable)

	metadataJSON, err := json.Marshal(execution.Metadata)
	if err != nil {
		return NewStoreError("update_execution", "failed to marshal metadata", err)
	}

	tagsJSON, err := json.Marshal(execution.Tags)
	if err != nil {
		return NewStoreError("update_execution", "failed to marshal tags", err)
	}

	var durationMs *int64
	if execution.Duration > 0 {
		ms := int64(execution.Duration.Nanoseconds() / 1000000)
		durationMs = &ms
	}

	var startTime, endTime *time.Time
	if !execution.StartTime.IsZero() {
		startTime = &execution.StartTime
	}
	if !execution.EndTime.IsZero() {
		endTime = &execution.EndTime
	}

	execution.UpdatedAt = time.Now()

	result, err := hm.store.db.DB().(*sql.DB).ExecContext(ctx, query,
		execution.ID,
		execution.Status,
		startTime,
		endTime,
		durationMs,
		execution.Output,
		execution.Error,
		execution.Attempt,
		metadataJSON,
		tagsJSON,
		execution.UpdatedAt,
	)

	if err != nil {
		return NewStoreError("update_execution", "failed to update execution", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("update_execution", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrExecutionNotFound
	}

	hm.store.metrics.ExecutionsUpdated++
	hm.store.metrics.QueriesExecuted++

	if hm.logger != nil {
		hm.logger.Info("execution updated in database",
			logger.String("execution_id", execution.ID),
			logger.String("job_id", execution.JobID),
			logger.String("status", string(execution.Status)),
		)
	}

	return nil
}

// DeleteExecution deletes a job execution by ID
func (hm *HistoryManager) DeleteExecution(ctx context.Context, executionID string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = $1", hm.store.config.ExecutionsTable)

	result, err := hm.store.db.DB().(*sql.DB).ExecContext(ctx, query, executionID)
	if err != nil {
		return NewStoreError("delete_execution", "failed to delete execution", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("delete_execution", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrExecutionNotFound
	}

	hm.store.metrics.QueriesExecuted++

	if hm.logger != nil {
		hm.logger.Info("execution deleted from database",
			logger.String("execution_id", executionID),
		)
	}

	return nil
}

// GetExecutionsByJob retrieves executions for a specific job
func (hm *HistoryManager) GetExecutionsByJob(ctx context.Context, jobID string, limit int) ([]*cron.JobExecution, error) {
	query := fmt.Sprintf(`
		SELECT id, job_id, node_id, status, start_time, end_time, duration_ms,
			   output, error, attempt, metadata, tags, created_at, updated_at
		FROM %s
		WHERE job_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, hm.store.config.ExecutionsTable)

	rows, err := hm.store.db.DB().(*sql.DB).QueryContext(ctx, query, jobID, limit)
	if err != nil {
		return nil, NewStoreError("get_executions_by_job", "failed to execute query", err)
	}
	defer rows.Close()

	var executions []*cron.JobExecution
	for rows.Next() {
		execution, err := hm.scanExecution(rows)
		if err != nil {
			return nil, NewStoreError("get_executions_by_job", "failed to scan execution", err)
		}
		executions = append(executions, execution)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_executions_by_job", "row iteration error", err)
	}

	hm.store.metrics.QueriesExecuted++
	return executions, nil
}

// GetExecutionsByNode retrieves executions for a specific node
func (hm *HistoryManager) GetExecutionsByNode(ctx context.Context, nodeID string, limit int) ([]*cron.JobExecution, error) {
	query := fmt.Sprintf(`
		SELECT id, job_id, node_id, status, start_time, end_time, duration_ms,
			   output, error, attempt, metadata, tags, created_at, updated_at
		FROM %s
		WHERE node_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, hm.store.config.ExecutionsTable)

	rows, err := hm.store.db.DB().(*sql.DB).QueryContext(ctx, query, nodeID, limit)
	if err != nil {
		return nil, NewStoreError("get_executions_by_node", "failed to execute query", err)
	}
	defer rows.Close()

	var executions []*cron.JobExecution
	for rows.Next() {
		execution, err := hm.scanExecution(rows)
		if err != nil {
			return nil, NewStoreError("get_executions_by_node", "failed to scan execution", err)
		}
		executions = append(executions, execution)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_executions_by_node", "row iteration error", err)
	}

	hm.store.metrics.QueriesExecuted++
	return executions, nil
}

// GetExecutionsByStatus retrieves executions by status
func (hm *HistoryManager) GetExecutionsByStatus(ctx context.Context, status cron.ExecutionStatus, limit int) ([]*cron.JobExecution, error) {
	query := fmt.Sprintf(`
		SELECT id, job_id, node_id, status, start_time, end_time, duration_ms,
			   output, error, attempt, metadata, tags, created_at, updated_at
		FROM %s
		WHERE status = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, hm.store.config.ExecutionsTable)

	rows, err := hm.store.db.DB().(*sql.DB).QueryContext(ctx, query, status, limit)
	if err != nil {
		return nil, NewStoreError("get_executions_by_status", "failed to execute query", err)
	}
	defer rows.Close()

	var executions []*cron.JobExecution
	for rows.Next() {
		execution, err := hm.scanExecution(rows)
		if err != nil {
			return nil, NewStoreError("get_executions_by_status", "failed to scan execution", err)
		}
		executions = append(executions, execution)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_executions_by_status", "row iteration error", err)
	}

	hm.store.metrics.QueriesExecuted++
	return executions, nil
}

// GetRunningExecutions retrieves all currently running executions
func (hm *HistoryManager) GetRunningExecutions(ctx context.Context) ([]*cron.JobExecution, error) {
	return hm.GetExecutionsByStatus(ctx, cron.ExecutionStatusRunning, 1000)
}

// GetExecutionStats retrieves execution statistics for a specific job
func (hm *HistoryManager) GetExecutionStats(ctx context.Context, jobID string) (*ExecutionStats, error) {
	query := fmt.Sprintf(`
		SELECT 
			COUNT(*) as total_executions,
			COUNT(CASE WHEN status = 'success' THEN 1 END) as successful_executions,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_executions,
			COUNT(CASE WHEN status = 'cancelled' THEN 1 END) as cancelled_executions,
			COUNT(CASE WHEN status = 'timeout' THEN 1 END) as timeout_executions,
			AVG(duration_ms) as avg_duration_ms,
			MIN(duration_ms) as min_duration_ms,
			MAX(duration_ms) as max_duration_ms,
			MAX(created_at) as last_execution_time
		FROM %s
		WHERE job_id = $1 AND duration_ms IS NOT NULL
	`, hm.store.config.ExecutionsTable)

	row := hm.store.db.DB().(*sql.DB).QueryRowContext(ctx, query, jobID)

	var stats ExecutionStats
	var avgDurationMs, minDurationMs, maxDurationMs sql.NullFloat64
	var lastExecution sql.NullTime

	err := row.Scan(
		&stats.TotalExecutions,
		&stats.SuccessfulExecutions,
		&stats.FailedExecutions,
		&stats.CancelledExecutions,
		&stats.TimeoutExecutions,
		&avgDurationMs,
		&minDurationMs,
		&maxDurationMs,
		&lastExecution,
	)

	if err != nil {
		return nil, NewStoreError("get_execution_stats", "failed to scan stats", err)
	}

	if avgDurationMs.Valid {
		stats.AverageExecutionTime = time.Duration(avgDurationMs.Float64 * 1000000) // Convert ms to ns
	}
	if minDurationMs.Valid {
		stats.MinExecutionTime = time.Duration(minDurationMs.Float64 * 1000000)
	}
	if maxDurationMs.Valid {
		stats.MaxExecutionTime = time.Duration(maxDurationMs.Float64 * 1000000)
	}
	if lastExecution.Valid {
		stats.LastExecutionTime = lastExecution.Time
	}

	// Calculate rates
	if stats.TotalExecutions > 0 {
		stats.SuccessRate = float64(stats.SuccessfulExecutions) / float64(stats.TotalExecutions)
		stats.FailureRate = float64(stats.FailedExecutions) / float64(stats.TotalExecutions)
	}

	hm.store.metrics.QueriesExecuted++
	return &stats, nil
}

// GetExecutionsStatsForPeriod retrieves execution statistics for a time period
func (hm *HistoryManager) GetExecutionsStatsForPeriod(ctx context.Context, start, end time.Time) (*ExecutionStats, error) {
	query := fmt.Sprintf(`
		SELECT 
			COUNT(*) as total_executions,
			COUNT(CASE WHEN status = 'success' THEN 1 END) as successful_executions,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_executions,
			COUNT(CASE WHEN status = 'cancelled' THEN 1 END) as cancelled_executions,
			COUNT(CASE WHEN status = 'timeout' THEN 1 END) as timeout_executions,
			AVG(duration_ms) as avg_duration_ms,
			MIN(duration_ms) as min_duration_ms,
			MAX(duration_ms) as max_duration_ms,
			MAX(created_at) as last_execution_time
		FROM %s
		WHERE created_at BETWEEN $1 AND $2 AND duration_ms IS NOT NULL
	`, hm.store.config.ExecutionsTable)

	row := hm.store.db.DB().(*sql.DB).QueryRowContext(ctx, query, start, end)

	var stats ExecutionStats
	var avgDurationMs, minDurationMs, maxDurationMs sql.NullFloat64
	var lastExecution sql.NullTime

	err := row.Scan(
		&stats.TotalExecutions,
		&stats.SuccessfulExecutions,
		&stats.FailedExecutions,
		&stats.CancelledExecutions,
		&stats.TimeoutExecutions,
		&avgDurationMs,
		&minDurationMs,
		&maxDurationMs,
		&lastExecution,
	)

	if err != nil {
		return nil, NewStoreError("get_executions_stats_for_period", "failed to scan stats", err)
	}

	if avgDurationMs.Valid {
		stats.AverageExecutionTime = time.Duration(avgDurationMs.Float64 * 1000000)
	}
	if minDurationMs.Valid {
		stats.MinExecutionTime = time.Duration(minDurationMs.Float64 * 1000000)
	}
	if maxDurationMs.Valid {
		stats.MaxExecutionTime = time.Duration(maxDurationMs.Float64 * 1000000)
	}
	if lastExecution.Valid {
		stats.LastExecutionTime = lastExecution.Time
	}

	// Calculate rates
	if stats.TotalExecutions > 0 {
		stats.SuccessRate = float64(stats.SuccessfulExecutions) / float64(stats.TotalExecutions)
		stats.FailureRate = float64(stats.FailedExecutions) / float64(stats.TotalExecutions)
	}

	hm.store.metrics.QueriesExecuted++
	return &stats, nil
}

// GetExecutionTimeSeries retrieves execution time series data
func (hm *HistoryManager) GetExecutionTimeSeries(ctx context.Context, jobID string, start, end time.Time, interval time.Duration) ([]*ExecutionTimeSeriesPoint, error) {
	// Generate time buckets
	bucketDuration := interval
	if bucketDuration == 0 {
		bucketDuration = time.Hour
	}

	query := fmt.Sprintf(`
		SELECT 
			DATE_TRUNC('hour', created_at) as time_bucket,
			COUNT(*) as total_executions,
			COUNT(CASE WHEN status = 'success' THEN 1 END) as successful_executions,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_executions,
			AVG(duration_ms) as avg_duration_ms
		FROM %s
		WHERE job_id = $1 AND created_at BETWEEN $2 AND $3
		GROUP BY time_bucket
		ORDER BY time_bucket
	`, hm.store.config.ExecutionsTable)

	rows, err := hm.store.db.DB().(*sql.DB).QueryContext(ctx, query, jobID, start, end)
	if err != nil {
		return nil, NewStoreError("get_execution_time_series", "failed to execute query", err)
	}
	defer rows.Close()

	var points []*ExecutionTimeSeriesPoint
	for rows.Next() {
		var point ExecutionTimeSeriesPoint
		var avgDurationMs sql.NullFloat64

		err := rows.Scan(
			&point.Time,
			&point.TotalExecutions,
			&point.SuccessfulExecutions,
			&point.FailedExecutions,
			&avgDurationMs,
		)

		if err != nil {
			return nil, NewStoreError("get_execution_time_series", "failed to scan point", err)
		}

		if avgDurationMs.Valid {
			point.AverageExecutionTime = time.Duration(avgDurationMs.Float64 * 1000000)
		}

		if point.TotalExecutions > 0 {
			point.SuccessRate = float64(point.SuccessfulExecutions) / float64(point.TotalExecutions)
			point.FailureRate = float64(point.FailedExecutions) / float64(point.TotalExecutions)
		}

		points = append(points, &point)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_execution_time_series", "row iteration error", err)
	}

	hm.store.metrics.QueriesExecuted++
	return points, nil
}

// GetJobExecutionSummary retrieves a summary of job executions
func (hm *HistoryManager) GetJobExecutionSummary(ctx context.Context, jobID string, days int) (*JobExecutionSummary, error) {
	sinceTime := time.Now().AddDate(0, 0, -days)

	query := fmt.Sprintf(`
		SELECT 
			COUNT(*) as total_executions,
			COUNT(CASE WHEN status = 'success' THEN 1 END) as successful_executions,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_executions,
			COUNT(CASE WHEN status = 'cancelled' THEN 1 END) as cancelled_executions,
			COUNT(CASE WHEN status = 'timeout' THEN 1 END) as timeout_executions,
			AVG(duration_ms) as avg_duration_ms,
			MIN(duration_ms) as min_duration_ms,
			MAX(duration_ms) as max_duration_ms,
			MAX(created_at) as last_execution_time,
			MIN(created_at) as first_execution_time
		FROM %s
		WHERE job_id = $1 AND created_at >= $2
	`, hm.store.config.ExecutionsTable)

	row := hm.store.db.DB().(*sql.DB).QueryRowContext(ctx, query, jobID, sinceTime)

	var summary JobExecutionSummary
	var avgDurationMs, minDurationMs, maxDurationMs sql.NullFloat64
	var lastExecution, firstExecution sql.NullTime

	err := row.Scan(
		&summary.TotalExecutions,
		&summary.SuccessfulExecutions,
		&summary.FailedExecutions,
		&summary.CancelledExecutions,
		&summary.TimeoutExecutions,
		&avgDurationMs,
		&minDurationMs,
		&maxDurationMs,
		&lastExecution,
		&firstExecution,
	)

	if err != nil {
		return nil, NewStoreError("get_job_execution_summary", "failed to scan summary", err)
	}

	summary.JobID = jobID
	summary.PeriodDays = days
	summary.PeriodStart = sinceTime
	summary.PeriodEnd = time.Now()

	if avgDurationMs.Valid {
		summary.AverageExecutionTime = time.Duration(avgDurationMs.Float64 * 1000000)
	}
	if minDurationMs.Valid {
		summary.MinExecutionTime = time.Duration(minDurationMs.Float64 * 1000000)
	}
	if maxDurationMs.Valid {
		summary.MaxExecutionTime = time.Duration(maxDurationMs.Float64 * 1000000)
	}
	if lastExecution.Valid {
		summary.LastExecutionTime = lastExecution.Time
	}
	if firstExecution.Valid {
		summary.FirstExecutionTime = firstExecution.Time
	}

	// Calculate rates
	if summary.TotalExecutions > 0 {
		summary.SuccessRate = float64(summary.SuccessfulExecutions) / float64(summary.TotalExecutions)
		summary.FailureRate = float64(summary.FailedExecutions) / float64(summary.TotalExecutions)
	}

	hm.store.metrics.QueriesExecuted++
	return &summary, nil
}

// CleanupOldExecutions removes old execution records
func (hm *HistoryManager) CleanupOldExecutions(ctx context.Context, before time.Time) error {
	query := fmt.Sprintf(`
		DELETE FROM %s WHERE created_at < $1
	`, hm.store.config.ExecutionsTable)

	result, err := hm.store.db.DB().(*sql.DB).ExecContext(ctx, query, before)
	if err != nil {
		return NewStoreError("cleanup_old_executions", "failed to cleanup executions", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("cleanup_old_executions", "failed to get rows affected", err)
	}

	if hm.logger != nil {
		hm.logger.Info("cleaned up old executions",
			logger.Int64("rows_deleted", rowsAffected),
			logger.Time("before", before),
		)
	}

	hm.store.metrics.QueriesExecuted++
	return nil
}

// CleanupExecutionsByJob removes old execution records for a specific job
func (hm *HistoryManager) CleanupExecutionsByJob(ctx context.Context, jobID string, keepLast int) error {
	// First get the execution IDs to keep
	keepQuery := fmt.Sprintf(`
		SELECT id FROM %s 
		WHERE job_id = $1 
		ORDER BY created_at DESC 
		LIMIT $2
	`, hm.store.config.ExecutionsTable)

	rows, err := hm.store.db.DB().(*sql.DB).QueryContext(ctx, keepQuery, jobID, keepLast)
	if err != nil {
		return NewStoreError("cleanup_executions_by_job", "failed to get executions to keep", err)
	}
	defer rows.Close()

	var keepIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return NewStoreError("cleanup_executions_by_job", "failed to scan execution ID", err)
		}
		keepIDs = append(keepIDs, id)
	}

	if len(keepIDs) == 0 {
		return nil // No executions to clean up
	}

	// Delete executions not in the keep list
	placeholders := make([]string, len(keepIDs))
	args := make([]interface{}, len(keepIDs)+1)
	args[0] = jobID
	for i, id := range keepIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	deleteQuery := fmt.Sprintf(`
		DELETE FROM %s 
		WHERE job_id = $1 AND id NOT IN (%s)
	`, hm.store.config.ExecutionsTable, strings.Join(placeholders, ","))

	result, err := hm.store.db.DB().(*sql.DB).ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return NewStoreError("cleanup_executions_by_job", "failed to delete executions", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("cleanup_executions_by_job", "failed to get rows affected", err)
	}

	if hm.logger != nil {
		hm.logger.Info("cleaned up executions by job",
			logger.String("job_id", jobID),
			logger.Int64("rows_deleted", rowsAffected),
			logger.Int("kept_executions", len(keepIDs)),
		)
	}

	hm.store.metrics.QueriesExecuted += 2 // Two queries executed
	return nil
}

// Helper methods

// scanExecution scans a database row into a JobExecution struct
func (hm *HistoryManager) scanExecution(scanner interface{}) (*cron.JobExecution, error) {
	var execution cron.JobExecution
	var metadataJSON, tagsJSON []byte
	var durationMs sql.NullInt64
	var startTime, endTime sql.NullTime
	var output, errorMsg sql.NullString

	var scanner_func func(dest ...interface{}) error
	switch s := scanner.(type) {
	case *sql.Row:
		scanner_func = s.Scan
	case *sql.Rows:
		scanner_func = s.Scan
	default:
		return nil, fmt.Errorf("invalid scanner type")
	}

	err := scanner_func(
		&execution.ID,
		&execution.JobID,
		&execution.NodeID,
		&execution.Status,
		&startTime,
		&endTime,
		&durationMs,
		&output,
		&errorMsg,
		&execution.Attempt,
		&metadataJSON,
		&tagsJSON,
		&execution.CreatedAt,
		&execution.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// Handle nullable fields
	if startTime.Valid {
		execution.StartTime = startTime.Time
	}
	if endTime.Valid {
		execution.EndTime = endTime.Time
	}
	if durationMs.Valid {
		execution.Duration = time.Duration(durationMs.Int64) * time.Millisecond
	}
	if output.Valid {
		execution.Output = output.String
	}
	if errorMsg.Valid {
		execution.Error = errorMsg.String
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(metadataJSON, &execution.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	if err := json.Unmarshal(tagsJSON, &execution.Tags); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
	}

	return &execution, nil
}

// buildExecutionQuery builds a query string and arguments for execution filtering
func (hm *HistoryManager) buildExecutionQuery(filter *cron.ExecutionFilter) (string, []interface{}) {
	baseQuery := fmt.Sprintf(`
		SELECT id, job_id, node_id, status, start_time, end_time, duration_ms,
			   output, error, attempt, metadata, tags, created_at, updated_at
		FROM %s
	`, hm.store.config.ExecutionsTable)

	var conditions []string
	var args []interface{}
	argIndex := 1

	if filter != nil {
		if len(filter.IDs) > 0 {
			placeholders := make([]string, len(filter.IDs))
			for i, id := range filter.IDs {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, id)
				argIndex++
			}
			conditions = append(conditions, fmt.Sprintf("id IN (%s)", strings.Join(placeholders, ",")))
		}

		if len(filter.JobIDs) > 0 {
			placeholders := make([]string, len(filter.JobIDs))
			for i, jobID := range filter.JobIDs {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, jobID)
				argIndex++
			}
			conditions = append(conditions, fmt.Sprintf("job_id IN (%s)", strings.Join(placeholders, ",")))
		}

		if len(filter.NodeIDs) > 0 {
			placeholders := make([]string, len(filter.NodeIDs))
			for i, nodeID := range filter.NodeIDs {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, nodeID)
				argIndex++
			}
			conditions = append(conditions, fmt.Sprintf("node_id IN (%s)", strings.Join(placeholders, ",")))
		}

		if len(filter.Statuses) > 0 {
			placeholders := make([]string, len(filter.Statuses))
			for i, status := range filter.Statuses {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, status)
				argIndex++
			}
			conditions = append(conditions, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
		}

		if filter.StartedAfter != nil {
			conditions = append(conditions, fmt.Sprintf("start_time > $%d", argIndex))
			args = append(args, *filter.StartedAfter)
			argIndex++
		}

		if filter.StartedBefore != nil {
			conditions = append(conditions, fmt.Sprintf("start_time < $%d", argIndex))
			args = append(args, *filter.StartedBefore)
			argIndex++
		}

		if filter.EndedAfter != nil {
			conditions = append(conditions, fmt.Sprintf("end_time > $%d", argIndex))
			args = append(args, *filter.EndedAfter)
			argIndex++
		}

		if filter.EndedBefore != nil {
			conditions = append(conditions, fmt.Sprintf("end_time < $%d", argIndex))
			args = append(args, *filter.EndedBefore)
			argIndex++
		}
	}

	query := baseQuery
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Add ordering
	if filter != nil && filter.SortBy != "" {
		order := "ASC"
		if filter.SortOrder == "DESC" {
			order = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", filter.SortBy, order)
	} else {
		query += " ORDER BY created_at DESC"
	}

	// Add limit and offset
	if filter != nil {
		if filter.Limit > 0 {
			query += fmt.Sprintf(" LIMIT $%d", argIndex)
			args = append(args, filter.Limit)
			argIndex++
		}

		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", argIndex)
			args = append(args, filter.Offset)
			argIndex++
		}
	}

	return query, args
}

// Additional types for history data

// ExecutionTimeSeriesPoint represents a point in execution time series
type ExecutionTimeSeriesPoint struct {
	Time                 time.Time     `json:"time"`
	TotalExecutions      int64         `json:"total_executions"`
	SuccessfulExecutions int64         `json:"successful_executions"`
	FailedExecutions     int64         `json:"failed_executions"`
	AverageExecutionTime time.Duration `json:"average_execution_time"`
	SuccessRate          float64       `json:"success_rate"`
	FailureRate          float64       `json:"failure_rate"`
}

// JobExecutionSummary represents a summary of job executions
type JobExecutionSummary struct {
	JobID                string        `json:"job_id"`
	PeriodDays           int           `json:"period_days"`
	PeriodStart          time.Time     `json:"period_start"`
	PeriodEnd            time.Time     `json:"period_end"`
	TotalExecutions      int64         `json:"total_executions"`
	SuccessfulExecutions int64         `json:"successful_executions"`
	FailedExecutions     int64         `json:"failed_executions"`
	CancelledExecutions  int64         `json:"cancelled_executions"`
	TimeoutExecutions    int64         `json:"timeout_executions"`
	SuccessRate          float64       `json:"success_rate"`
	FailureRate          float64       `json:"failure_rate"`
	AverageExecutionTime time.Duration `json:"average_execution_time"`
	MinExecutionTime     time.Duration `json:"min_execution_time"`
	MaxExecutionTime     time.Duration `json:"max_execution_time"`
	FirstExecutionTime   time.Time     `json:"first_execution_time"`
	LastExecutionTime    time.Time     `json:"last_execution_time"`
}
