package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	cron "github.com/xraph/forge/pkg/cron/core"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// DatabaseStore implements the Store interface using a database backend
type DatabaseStore struct {
	db      database.Connection
	config  *StoreConfig
	logger  common.Logger
	metrics *StoreMetrics
}

// NewDatabaseStore creates a new database-backed store
func NewDatabaseStore(db database.Connection, config *StoreConfig, logger common.Logger) (Store, error) {
	if err := ValidateStoreConfig(config); err != nil {
		return nil, err
	}

	store := &DatabaseStore{
		db:      db,
		config:  config,
		logger:  logger,
		metrics: &StoreMetrics{LastResetTime: time.Now()},
	}

	return store, nil
}

// Migrate creates the necessary database tables and indexes
func (ds *DatabaseStore) Migrate(ctx context.Context) error {
	queries := []string{
		ds.createJobsTableQuery(),
		ds.createExecutionsTableQuery(),
		ds.createEventsTableQuery(),
		ds.createLocksTableQuery(),
		ds.createJobsIndexesQuery(),
		ds.createExecutionsIndexesQuery(),
		ds.createEventsIndexesQuery(),
		ds.createLocksIndexesQuery(),
	}

	return ds.db.Transaction(ctx, func(tx interface{}) error {
		sqlTx, ok := tx.(*sql.Tx)
		if !ok {
			return fmt.Errorf("invalid transaction type")
		}

		for _, query := range queries {
			if _, err := sqlTx.ExecContext(ctx, query); err != nil {
				return NewStoreError("migrate", "failed to execute migration query", err)
			}
		}

		return nil
	})
}

// CreateJob creates a new job in the database
func (ds *DatabaseStore) CreateJob(ctx context.Context, job *cron.Job) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (
			id, name, schedule, config, enabled, max_retries, timeout_seconds,
			singleton, distributed, tags, dependencies, priority, status,
			next_run, last_run, run_count, failure_count, success_count,
			leader_node, assigned_node, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22
		)
	`, ds.config.JobsTable)

	configJSON, err := json.Marshal(job.Definition.Config)
	if err != nil {
		return NewStoreError("create_job", "failed to marshal config", err)
	}

	tagsJSON, err := json.Marshal(job.Definition.Tags)
	if err != nil {
		return NewStoreError("create_job", "failed to marshal tags", err)
	}

	dependenciesJSON, err := json.Marshal(job.Definition.Dependencies)
	if err != nil {
		return NewStoreError("create_job", "failed to marshal dependencies", err)
	}

	_, err = ds.db.DB().(*sql.DB).ExecContext(ctx, query,
		job.Definition.ID,
		job.Definition.Name,
		job.Definition.Schedule,
		configJSON,
		job.Definition.Enabled,
		job.Definition.MaxRetries,
		int(job.Definition.Timeout.Seconds()),
		job.Definition.Singleton,
		job.Definition.Distributed,
		tagsJSON,
		dependenciesJSON,
		job.Definition.Priority,
		job.Status,
		job.NextRun,
		job.LastRun,
		job.RunCount,
		job.FailureCount,
		job.SuccessCount,
		job.LeaderNode,
		job.AssignedNode,
		job.CreatedAt,
		job.UpdatedAt,
	)

	if err != nil {
		return NewStoreError("create_job", "failed to insert job", err)
	}

	ds.metrics.JobsCreated++
	ds.metrics.QueriesExecuted++

	if ds.logger != nil {
		ds.logger.Info("job created in database",
			logger.String("job_id", job.Definition.ID),
			logger.String("job_name", job.Definition.Name),
		)
	}

	return nil
}

// GetJob retrieves a job by ID
func (ds *DatabaseStore) GetJob(ctx context.Context, jobID string) (*cron.Job, error) {
	query := fmt.Sprintf(`
		SELECT id, name, schedule, config, enabled, max_retries, timeout_seconds,
			   singleton, distributed, tags, dependencies, priority, status,
			   next_run, last_run, run_count, failure_count, success_count,
			   leader_node, assigned_node, created_at, updated_at
		FROM %s WHERE id = $1
	`, ds.config.JobsTable)

	row := ds.db.DB().(*sql.DB).QueryRowContext(ctx, query, jobID)

	job, err := ds.scanJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrJobNotFound
		}
		return nil, NewStoreError("get_job", "failed to scan job", err)
	}

	ds.metrics.QueriesExecuted++
	return job, nil
}

// GetJobs retrieves jobs based on filter criteria
func (ds *DatabaseStore) GetJobs(ctx context.Context, filter *cron.JobFilter) ([]*cron.Job, error) {
	query, args := ds.buildJobQuery(filter)

	rows, err := ds.db.DB().(*sql.DB).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, NewStoreError("get_jobs", "failed to execute query", err)
	}
	defer rows.Close()

	var jobs []*cron.Job
	for rows.Next() {
		job, err := ds.scanJob(rows)
		if err != nil {
			return nil, NewStoreError("get_jobs", "failed to scan job", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_jobs", "row iteration error", err)
	}

	ds.metrics.QueriesExecuted++
	return jobs, nil
}

// UpdateJob updates an existing job
func (ds *DatabaseStore) UpdateJob(ctx context.Context, job *cron.Job) error {
	query := fmt.Sprintf(`
		UPDATE %s SET
			name = $2, schedule = $3, config = $4, enabled = $5, max_retries = $6,
			timeout_seconds = $7, singleton = $8, distributed = $9, tags = $10,
			dependencies = $11, priority = $12, status = $13, next_run = $14,
			last_run = $15, run_count = $16, failure_count = $17, success_count = $18,
			leader_node = $19, assigned_node = $20, updated_at = $21
		WHERE id = $1
	`, ds.config.JobsTable)

	configJSON, err := json.Marshal(job.Definition.Config)
	if err != nil {
		return NewStoreError("update_job", "failed to marshal config", err)
	}

	tagsJSON, err := json.Marshal(job.Definition.Tags)
	if err != nil {
		return NewStoreError("update_job", "failed to marshal tags", err)
	}

	dependenciesJSON, err := json.Marshal(job.Definition.Dependencies)
	if err != nil {
		return NewStoreError("update_job", "failed to marshal dependencies", err)
	}

	job.UpdatedAt = time.Now()

	result, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query,
		job.Definition.ID,
		job.Definition.Name,
		job.Definition.Schedule,
		configJSON,
		job.Definition.Enabled,
		job.Definition.MaxRetries,
		int(job.Definition.Timeout.Seconds()),
		job.Definition.Singleton,
		job.Definition.Distributed,
		tagsJSON,
		dependenciesJSON,
		job.Definition.Priority,
		job.Status,
		job.NextRun,
		job.LastRun,
		job.RunCount,
		job.FailureCount,
		job.SuccessCount,
		job.LeaderNode,
		job.AssignedNode,
		job.UpdatedAt,
	)

	if err != nil {
		return NewStoreError("update_job", "failed to update job", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("update_job", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	ds.metrics.JobsUpdated++
	ds.metrics.QueriesExecuted++

	if ds.logger != nil {
		ds.logger.Info("job updated in database",
			logger.String("job_id", job.Definition.ID),
			logger.String("job_name", job.Definition.Name),
		)
	}

	return nil
}

// DeleteJob deletes a job by ID
func (ds *DatabaseStore) DeleteJob(ctx context.Context, jobID string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = $1", ds.config.JobsTable)

	result, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query, jobID)
	if err != nil {
		return NewStoreError("delete_job", "failed to delete job", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("delete_job", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	ds.metrics.JobsDeleted++
	ds.metrics.QueriesExecuted++

	if ds.logger != nil {
		ds.logger.Info("job deleted from database",
			logger.String("job_id", jobID),
		)
	}

	return nil
}

// SetJobStatus updates the status of a job
func (ds *DatabaseStore) SetJobStatus(ctx context.Context, jobID string, status cron.JobStatus) error {
	query := fmt.Sprintf(`
		UPDATE %s SET status = $2, updated_at = $3 WHERE id = $1
	`, ds.config.JobsTable)

	result, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query, jobID, status, time.Now())
	if err != nil {
		return NewStoreError("set_job_status", "failed to update job status", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("set_job_status", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	ds.metrics.JobsUpdated++
	ds.metrics.QueriesExecuted++

	return nil
}

// GetJobsByStatus retrieves jobs by status
func (ds *DatabaseStore) GetJobsByStatus(ctx context.Context, status cron.JobStatus) ([]*cron.Job, error) {
	filter := &cron.JobFilter{
		Statuses: []cron.JobStatus{status},
	}
	return ds.GetJobs(ctx, filter)
}

// GetJobsByNode retrieves jobs assigned to a specific node
func (ds *DatabaseStore) GetJobsByNode(ctx context.Context, nodeID string) ([]*cron.Job, error) {
	query := fmt.Sprintf(`
		SELECT id, name, schedule, config, enabled, max_retries, timeout_seconds,
			   singleton, distributed, tags, dependencies, priority, status,
			   next_run, last_run, run_count, failure_count, success_count,
			   leader_node, assigned_node, created_at, updated_at
		FROM %s WHERE assigned_node = $1
	`, ds.config.JobsTable)

	rows, err := ds.db.DB().(*sql.DB).QueryContext(ctx, query, nodeID)
	if err != nil {
		return nil, NewStoreError("get_jobs_by_node", "failed to execute query", err)
	}
	defer rows.Close()

	var jobs []*cron.Job
	for rows.Next() {
		job, err := ds.scanJob(rows)
		if err != nil {
			return nil, NewStoreError("get_jobs_by_node", "failed to scan job", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_jobs_by_node", "row iteration error", err)
	}

	ds.metrics.QueriesExecuted++
	return jobs, nil
}

// GetJobsDueForExecution retrieves jobs that are due for execution
func (ds *DatabaseStore) GetJobsDueForExecution(ctx context.Context, before time.Time) ([]*cron.Job, error) {
	query := fmt.Sprintf(`
		SELECT id, name, schedule, config, enabled, max_retries, timeout_seconds,
			   singleton, distributed, tags, dependencies, priority, status,
			   next_run, last_run, run_count, failure_count, success_count,
			   leader_node, assigned_node, created_at, updated_at
		FROM %s 
		WHERE enabled = true AND status = 'active' AND next_run <= $1
		ORDER BY priority DESC, next_run ASC
	`, ds.config.JobsTable)

	rows, err := ds.db.DB().(*sql.DB).QueryContext(ctx, query, before)
	if err != nil {
		return nil, NewStoreError("get_jobs_due_for_execution", "failed to execute query", err)
	}
	defer rows.Close()

	var jobs []*cron.Job
	for rows.Next() {
		job, err := ds.scanJob(rows)
		if err != nil {
			return nil, NewStoreError("get_jobs_due_for_execution", "failed to scan job", err)
		}
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, NewStoreError("get_jobs_due_for_execution", "row iteration error", err)
	}

	ds.metrics.QueriesExecuted++
	return jobs, nil
}

// UpdateJobNextRun updates the next run time for a job
func (ds *DatabaseStore) UpdateJobNextRun(ctx context.Context, jobID string, nextRun time.Time) error {
	query := fmt.Sprintf(`
		UPDATE %s SET next_run = $2, updated_at = $3 WHERE id = $1
	`, ds.config.JobsTable)

	result, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query, jobID, nextRun, time.Now())
	if err != nil {
		return NewStoreError("update_job_next_run", "failed to update next run time", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("update_job_next_run", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrJobNotFound
	}

	ds.metrics.JobsUpdated++
	ds.metrics.QueriesExecuted++

	return nil
}

// LockJob creates a lock for a job
func (ds *DatabaseStore) LockJob(ctx context.Context, jobID string, nodeID string, lockDuration time.Duration) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (job_id, node_id, locked_at, expires_at, metadata)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (job_id) DO UPDATE SET
			node_id = $2,
			locked_at = $3,
			expires_at = $4,
			metadata = $5
		WHERE %s.expires_at < $3
	`, ds.config.LocksTable, ds.config.LocksTable)

	now := time.Now()
	expiresAt := now.Add(lockDuration)
	metadata := map[string]interface{}{
		"node_id":    nodeID,
		"locked_at":  now,
		"expires_at": expiresAt,
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return NewStoreError("lock_job", "failed to marshal metadata", err)
	}

	result, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query,
		jobID, nodeID, now, expiresAt, metadataJSON)
	if err != nil {
		return NewStoreError("lock_job", "failed to create lock", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("lock_job", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrJobLocked
	}

	ds.metrics.QueriesExecuted++
	return nil
}

// UnlockJob removes a lock for a job
func (ds *DatabaseStore) UnlockJob(ctx context.Context, jobID string, nodeID string) error {
	query := fmt.Sprintf(`
		DELETE FROM %s WHERE job_id = $1 AND node_id = $2
	`, ds.config.LocksTable)

	result, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query, jobID, nodeID)
	if err != nil {
		return NewStoreError("unlock_job", "failed to remove lock", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("unlock_job", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrJobNotLocked
	}

	ds.metrics.QueriesExecuted++
	return nil
}

// RefreshJobLock refreshes the expiration time of a job lock
func (ds *DatabaseStore) RefreshJobLock(ctx context.Context, jobID string, nodeID string, lockDuration time.Duration) error {
	query := fmt.Sprintf(`
		UPDATE %s SET expires_at = $3 WHERE job_id = $1 AND node_id = $2
	`, ds.config.LocksTable)

	expiresAt := time.Now().Add(lockDuration)
	result, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query, jobID, nodeID, expiresAt)
	if err != nil {
		return NewStoreError("refresh_job_lock", "failed to refresh lock", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("refresh_job_lock", "failed to get rows affected", err)
	}

	if rowsAffected == 0 {
		return ErrJobNotLocked
	}

	ds.metrics.QueriesExecuted++
	return nil
}

// IsJobLocked checks if a job is locked
func (ds *DatabaseStore) IsJobLocked(ctx context.Context, jobID string) (bool, string, error) {
	query := fmt.Sprintf(`
		SELECT node_id FROM %s WHERE job_id = $1 AND expires_at > $2
	`, ds.config.LocksTable)

	var nodeID string
	err := ds.db.DB().(*sql.DB).QueryRowContext(ctx, query, jobID, time.Now()).Scan(&nodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, "", nil
		}
		return false, "", NewStoreError("is_job_locked", "failed to check lock", err)
	}

	ds.metrics.QueriesExecuted++
	return true, nodeID, nil
}

// CleanupExpiredLocks removes expired locks
func (ds *DatabaseStore) CleanupExpiredLocks(ctx context.Context) error {
	query := fmt.Sprintf(`
		DELETE FROM %s WHERE expires_at < $1
	`, ds.config.LocksTable)

	_, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query, time.Now())
	if err != nil {
		return NewStoreError("cleanup_expired_locks", "failed to cleanup locks", err)
	}

	ds.metrics.QueriesExecuted++
	return nil
}

// CleanupOldJobs removes old jobs
func (ds *DatabaseStore) CleanupOldJobs(ctx context.Context, before time.Time) error {
	query := fmt.Sprintf(`
		DELETE FROM %s WHERE status = 'completed' AND updated_at < $1
	`, ds.config.JobsTable)

	_, err := ds.db.DB().(*sql.DB).ExecContext(ctx, query, before)
	if err != nil {
		return NewStoreError("cleanup_old_jobs", "failed to cleanup jobs", err)
	}

	ds.metrics.QueriesExecuted++
	return nil
}

// Helper methods

// scanJob scans a database row into a Job struct
func (ds *DatabaseStore) scanJob(scanner interface{}) (*cron.Job, error) {
	var job cron.Job
	var definition cron.JobDefinition
	var configJSON, tagsJSON, dependenciesJSON []byte
	var timeoutSeconds int

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
		&definition.ID,
		&definition.Name,
		&definition.Schedule,
		&configJSON,
		&definition.Enabled,
		&definition.MaxRetries,
		&timeoutSeconds,
		&definition.Singleton,
		&definition.Distributed,
		&tagsJSON,
		&dependenciesJSON,
		&definition.Priority,
		&job.Status,
		&job.NextRun,
		&job.LastRun,
		&job.RunCount,
		&job.FailureCount,
		&job.SuccessCount,
		&job.LeaderNode,
		&job.AssignedNode,
		&job.CreatedAt,
		&job.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(configJSON, &definition.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := json.Unmarshal(tagsJSON, &definition.Tags); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tags: %w", err)
	}

	if err := json.Unmarshal(dependenciesJSON, &definition.Dependencies); err != nil {
		return nil, fmt.Errorf("failed to unmarshal dependencies: %w", err)
	}

	definition.Timeout = time.Duration(timeoutSeconds) * time.Second
	job.Definition = &definition

	return &job, nil
}

// buildJobQuery builds a query string and arguments for job filtering
func (ds *DatabaseStore) buildJobQuery(filter *cron.JobFilter) (string, []interface{}) {
	baseQuery := fmt.Sprintf(`
		SELECT id, name, schedule, config, enabled, max_retries, timeout_seconds,
			   singleton, distributed, tags, dependencies, priority, status,
			   next_run, last_run, run_count, failure_count, success_count,
			   leader_node, assigned_node, created_at, updated_at
		FROM %s
	`, ds.config.JobsTable)

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

		if len(filter.Names) > 0 {
			placeholders := make([]string, len(filter.Names))
			for i, name := range filter.Names {
				placeholders[i] = fmt.Sprintf("$%d", argIndex)
				args = append(args, name)
				argIndex++
			}
			conditions = append(conditions, fmt.Sprintf("name IN (%s)", strings.Join(placeholders, ",")))
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

		if filter.Enabled != nil {
			conditions = append(conditions, fmt.Sprintf("enabled = $%d", argIndex))
			args = append(args, *filter.Enabled)
			argIndex++
		}

		if filter.Singleton != nil {
			conditions = append(conditions, fmt.Sprintf("singleton = $%d", argIndex))
			args = append(args, *filter.Singleton)
			argIndex++
		}

		if filter.Distributed != nil {
			conditions = append(conditions, fmt.Sprintf("distributed = $%d", argIndex))
			args = append(args, *filter.Distributed)
			argIndex++
		}

		if filter.CreatedAfter != nil {
			conditions = append(conditions, fmt.Sprintf("created_at > $%d", argIndex))
			args = append(args, *filter.CreatedAfter)
			argIndex++
		}

		if filter.CreatedBefore != nil {
			conditions = append(conditions, fmt.Sprintf("created_at < $%d", argIndex))
			args = append(args, *filter.CreatedBefore)
			argIndex++
		}

		if filter.UpdatedAfter != nil {
			conditions = append(conditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedAfter)
			argIndex++
		}

		if filter.UpdatedBefore != nil {
			conditions = append(conditions, fmt.Sprintf("updated_at < $%d", argIndex))
			args = append(args, *filter.UpdatedBefore)
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

// Database table creation queries
func (ds *DatabaseStore) createJobsTableQuery() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			schedule VARCHAR(255) NOT NULL,
			config JSONB NOT NULL DEFAULT '{}',
			enabled BOOLEAN NOT NULL DEFAULT true,
			max_retries INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
			singleton BOOLEAN NOT NULL DEFAULT false,
			distributed BOOLEAN NOT NULL DEFAULT true,
			tags JSONB NOT NULL DEFAULT '{}',
			dependencies JSONB NOT NULL DEFAULT '[]',
			priority INTEGER NOT NULL DEFAULT 0,
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			next_run TIMESTAMP WITH TIME ZONE,
			last_run TIMESTAMP WITH TIME ZONE,
			run_count BIGINT NOT NULL DEFAULT 0,
			failure_count BIGINT NOT NULL DEFAULT 0,
			success_count BIGINT NOT NULL DEFAULT 0,
			leader_node VARCHAR(255),
			assigned_node VARCHAR(255),
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`, ds.config.JobsTable)
}

func (ds *DatabaseStore) createExecutionsTableQuery() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			job_id VARCHAR(255) NOT NULL,
			node_id VARCHAR(255) NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			start_time TIMESTAMP WITH TIME ZONE,
			end_time TIMESTAMP WITH TIME ZONE,
			duration_ms BIGINT,
			output TEXT,
			error TEXT,
			attempt INTEGER NOT NULL DEFAULT 1,
			metadata JSONB NOT NULL DEFAULT '{}',
			tags JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			FOREIGN KEY (job_id) REFERENCES %s(id) ON DELETE CASCADE
		)
	`, ds.config.ExecutionsTable, ds.config.JobsTable)
}

func (ds *DatabaseStore) createEventsTableQuery() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id SERIAL PRIMARY KEY,
			type VARCHAR(100) NOT NULL,
			job_id VARCHAR(255) NOT NULL,
			node_id VARCHAR(255) NOT NULL,
			status VARCHAR(50),
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			metadata JSONB NOT NULL DEFAULT '{}',
			error TEXT,
			FOREIGN KEY (job_id) REFERENCES %s(id) ON DELETE CASCADE
		)
	`, ds.config.EventsTable, ds.config.JobsTable)
}

func (ds *DatabaseStore) createLocksTableQuery() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			job_id VARCHAR(255) PRIMARY KEY,
			node_id VARCHAR(255) NOT NULL,
			locked_at TIMESTAMP WITH TIME ZONE NOT NULL,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			metadata JSONB NOT NULL DEFAULT '{}',
			FOREIGN KEY (job_id) REFERENCES %s(id) ON DELETE CASCADE
		)
	`, ds.config.LocksTable, ds.config.JobsTable)
}

func (ds *DatabaseStore) createJobsIndexesQuery() string {
	return fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_status ON %s(status);
		CREATE INDEX IF NOT EXISTS idx_%s_next_run ON %s(next_run);
		CREATE INDEX IF NOT EXISTS idx_%s_enabled ON %s(enabled);
		CREATE INDEX IF NOT EXISTS idx_%s_assigned_node ON %s(assigned_node);
		CREATE INDEX IF NOT EXISTS idx_%s_priority ON %s(priority);
		CREATE INDEX IF NOT EXISTS idx_%s_created_at ON %s(created_at);
		CREATE INDEX IF NOT EXISTS idx_%s_updated_at ON %s(updated_at);
	`,
		ds.config.JobsTable, ds.config.JobsTable,
		ds.config.JobsTable, ds.config.JobsTable,
		ds.config.JobsTable, ds.config.JobsTable,
		ds.config.JobsTable, ds.config.JobsTable,
		ds.config.JobsTable, ds.config.JobsTable,
		ds.config.JobsTable, ds.config.JobsTable,
		ds.config.JobsTable, ds.config.JobsTable,
	)
}

func (ds *DatabaseStore) createExecutionsIndexesQuery() string {
	return fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_job_id ON %s(job_id);
		CREATE INDEX IF NOT EXISTS idx_%s_node_id ON %s(node_id);
		CREATE INDEX IF NOT EXISTS idx_%s_status ON %s(status);
		CREATE INDEX IF NOT EXISTS idx_%s_start_time ON %s(start_time);
		CREATE INDEX IF NOT EXISTS idx_%s_end_time ON %s(end_time);
		CREATE INDEX IF NOT EXISTS idx_%s_created_at ON %s(created_at);
	`,
		ds.config.ExecutionsTable, ds.config.ExecutionsTable,
		ds.config.ExecutionsTable, ds.config.ExecutionsTable,
		ds.config.ExecutionsTable, ds.config.ExecutionsTable,
		ds.config.ExecutionsTable, ds.config.ExecutionsTable,
		ds.config.ExecutionsTable, ds.config.ExecutionsTable,
		ds.config.ExecutionsTable, ds.config.ExecutionsTable,
	)
}

func (ds *DatabaseStore) createEventsIndexesQuery() string {
	return fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_job_id ON %s(job_id);
		CREATE INDEX IF NOT EXISTS idx_%s_node_id ON %s(node_id);
		CREATE INDEX IF NOT EXISTS idx_%s_type ON %s(type);
		CREATE INDEX IF NOT EXISTS idx_%s_timestamp ON %s(timestamp);
	`,
		ds.config.EventsTable, ds.config.EventsTable,
		ds.config.EventsTable, ds.config.EventsTable,
		ds.config.EventsTable, ds.config.EventsTable,
		ds.config.EventsTable, ds.config.EventsTable,
	)
}

func (ds *DatabaseStore) createLocksIndexesQuery() string {
	return fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_expires_at ON %s(expires_at);
		CREATE INDEX IF NOT EXISTS idx_%s_node_id ON %s(node_id);
	`,
		ds.config.LocksTable, ds.config.LocksTable,
		ds.config.LocksTable, ds.config.LocksTable,
	)
}

// Continue with remaining Store interface methods...
// (Implementation would continue with ExecutionStore and EventStore methods)

// HealthCheck checks the health of the database connection
func (ds *DatabaseStore) HealthCheck(ctx context.Context) error {
	return ds.db.OnHealthCheck(ctx)
}

// GetStats returns statistics about the store
func (ds *DatabaseStore) GetStats(ctx context.Context) (*StoreStats, error) {
	stats := &StoreStats{
		QueriesExecuted:  ds.metrics.QueriesExecuted,
		AverageQueryTime: ds.metrics.AverageQueryTime,
		LastResetTime:    ds.metrics.LastResetTime,
	}

	// Get counts from database
	queries := []struct {
		query string
		field *int64
	}{
		{fmt.Sprintf("SELECT COUNT(*) FROM %s", ds.config.JobsTable), &stats.TotalJobs},
		{fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE status = 'active'", ds.config.JobsTable), &stats.ActiveJobs},
		{fmt.Sprintf("SELECT COUNT(*) FROM %s", ds.config.ExecutionsTable), &stats.TotalExecutions},
		{fmt.Sprintf("SELECT COUNT(*) FROM %s", ds.config.EventsTable), &stats.TotalEvents},
	}

	for _, q := range queries {
		err := ds.db.DB().(*sql.DB).QueryRowContext(ctx, q.query).Scan(q.field)
		if err != nil {
			return nil, NewStoreError("get_stats", "failed to get count", err)
		}
	}

	return stats, nil
}

// BeginTx begins a transaction
func (ds *DatabaseStore) BeginTx(ctx context.Context) (Transaction, error) {
	// This would be implemented to return a transaction wrapper
	return nil, fmt.Errorf("transaction support not implemented yet")
}

// Close closes the database connection
func (ds *DatabaseStore) Close() error {
	return ds.db.Close(context.Background())
}

// Additional methods for ExecutionStore and EventStore interfaces would be implemented here...
// Following the same pattern as the JobStore methods
