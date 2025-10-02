package persistence

import (
	"context"
	"fmt"
	"time"

	cron "github.com/xraph/forge/pkg/cron/core"
)

// JobStore defines the interface for job persistence
type JobStore interface {
	// Job management
	CreateJob(ctx context.Context, job *cron.Job) error
	GetJob(ctx context.Context, jobID string) (*cron.Job, error)
	GetJobs(ctx context.Context, filter *cron.JobFilter) ([]*cron.Job, error)
	UpdateJob(ctx context.Context, job *cron.Job) error
	DeleteJob(ctx context.Context, jobID string) error

	// Job status operations
	SetJobStatus(ctx context.Context, jobID string, status cron.JobStatus) error
	GetJobsByStatus(ctx context.Context, status cron.JobStatus) ([]*cron.Job, error)
	GetJobsByNode(ctx context.Context, nodeID string) ([]*cron.Job, error)

	// Job scheduling
	GetJobsDueForExecution(ctx context.Context, before time.Time) ([]*cron.Job, error)
	UpdateJobNextRun(ctx context.Context, jobID string, nextRun time.Time) error

	// Job statistics
	GetJobStats(ctx context.Context, jobID string) (*cron.JobStats, error)
	GetJobsStats(ctx context.Context, filter *cron.JobFilter) ([]*cron.JobStats, error)

	// Job locking for distributed execution
	LockJob(ctx context.Context, jobID string, nodeID string, lockDuration time.Duration) error
	UnlockJob(ctx context.Context, jobID string, nodeID string) error
	RefreshJobLock(ctx context.Context, jobID string, nodeID string, lockDuration time.Duration) error
	IsJobLocked(ctx context.Context, jobID string) (bool, string, error)

	// Cleanup operations
	CleanupExpiredLocks(ctx context.Context) error
	CleanupOldJobs(ctx context.Context, before time.Time) error
}

// ExecutionStore defines the interface for job execution persistence
type ExecutionStore interface {
	// Execution management
	CreateExecution(ctx context.Context, execution *cron.JobExecution) error
	GetExecution(ctx context.Context, executionID string) (*cron.JobExecution, error)
	GetExecutions(ctx context.Context, filter *cron.ExecutionFilter) ([]*cron.JobExecution, error)
	UpdateExecution(ctx context.Context, execution *cron.JobExecution) error
	DeleteExecution(ctx context.Context, executionID string) error

	// Execution queries
	GetExecutionsByJob(ctx context.Context, jobID string, limit int) ([]*cron.JobExecution, error)
	GetExecutionsByNode(ctx context.Context, nodeID string, limit int) ([]*cron.JobExecution, error)
	GetExecutionsByStatus(ctx context.Context, status cron.ExecutionStatus, limit int) ([]*cron.JobExecution, error)
	GetRunningExecutions(ctx context.Context) ([]*cron.JobExecution, error)

	// Execution statistics
	GetExecutionStats(ctx context.Context, jobID string) (*ExecutionStats, error)
	GetExecutionsStatsForPeriod(ctx context.Context, start, end time.Time) (*ExecutionStats, error)

	// Cleanup operations
	CleanupOldExecutions(ctx context.Context, before time.Time) error
	CleanupExecutionsByJob(ctx context.Context, jobID string, keepLast int) error
}

// EventStore defines the interface for job event persistence
type EventStore interface {
	// Event management
	CreateEvent(ctx context.Context, event *cron.JobEvent) error
	GetEvents(ctx context.Context, filter *EventFilter) ([]*cron.JobEvent, error)
	GetEventsByJob(ctx context.Context, jobID string, limit int) ([]*cron.JobEvent, error)
	GetEventsByNode(ctx context.Context, nodeID string, limit int) ([]*cron.JobEvent, error)

	// Event queries
	GetEventsByType(ctx context.Context, eventType cron.JobEventType, limit int) ([]*cron.JobEvent, error)
	GetEventsForPeriod(ctx context.Context, start, end time.Time) ([]*cron.JobEvent, error)

	// Cleanup operations
	CleanupOldEvents(ctx context.Context, before time.Time) error
}

// Store combines all storage interfaces
type Store interface {
	JobStore
	ExecutionStore
	EventStore

	// Transaction support
	BeginTx(ctx context.Context) (Transaction, error)

	// Health and maintenance
	HealthCheck(ctx context.Context) error
	GetStats(ctx context.Context) (*StoreStats, error)
	Migrate(ctx context.Context) error
	Close() error
}

// Transaction defines the interface for transactional operations
type Transaction interface {
	JobStore
	ExecutionStore
	EventStore

	Commit() error
	Rollback() error
}

// StoreConfig defines configuration for the store
type StoreConfig struct {
	// Database configuration
	DatabaseURL    string        `json:"database_url" yaml:"database_url"`
	MaxConnections int           `json:"max_connections" yaml:"max_connections"`
	MaxIdleTime    time.Duration `json:"max_idle_time" yaml:"max_idle_time"`
	MaxLifetime    time.Duration `json:"max_lifetime" yaml:"max_lifetime"`
	ConnectTimeout time.Duration `json:"connect_timeout" yaml:"connect_timeout"`

	// Table configuration
	JobsTable       string `json:"jobs_table" yaml:"jobs_table"`
	ExecutionsTable string `json:"executions_table" yaml:"executions_table"`
	EventsTable     string `json:"events_table" yaml:"events_table"`
	LocksTable      string `json:"locks_table" yaml:"locks_table"`

	// Cleanup configuration
	CleanupInterval          time.Duration `json:"cleanup_interval" yaml:"cleanup_interval"`
	JobRetentionPeriod       time.Duration `json:"job_retention_period" yaml:"job_retention_period"`
	ExecutionRetentionPeriod time.Duration `json:"execution_retention_period" yaml:"execution_retention_period"`
	EventRetentionPeriod     time.Duration `json:"event_retention_period" yaml:"event_retention_period"`
	LockTimeout              time.Duration `json:"lock_timeout" yaml:"lock_timeout"`

	// Performance configuration
	BatchSize           int  `json:"batch_size" yaml:"batch_size"`
	EnablePreparedStmts bool `json:"enable_prepared_stmts" yaml:"enable_prepared_stmts"`
	EnableQueryCache    bool `json:"enable_query_cache" yaml:"enable_query_cache"`
}

// ExecutionStats contains statistics about job executions
type ExecutionStats struct {
	TotalExecutions      int64         `json:"total_executions"`
	SuccessfulExecutions int64         `json:"successful_executions"`
	FailedExecutions     int64         `json:"failed_executions"`
	CancelledExecutions  int64         `json:"cancelled_executions"`
	TimeoutExecutions    int64         `json:"timeout_executions"`
	AverageExecutionTime time.Duration `json:"average_execution_time"`
	MinExecutionTime     time.Duration `json:"min_execution_time"`
	MaxExecutionTime     time.Duration `json:"max_execution_time"`
	LastExecutionTime    time.Time     `json:"last_execution_time"`
	SuccessRate          float64       `json:"success_rate"`
	FailureRate          float64       `json:"failure_rate"`
}

// EventFilter defines filter criteria for querying events
type EventFilter struct {
	JobIDs    []string            `json:"job_ids,omitempty"`
	NodeIDs   []string            `json:"node_ids,omitempty"`
	Types     []cron.JobEventType `json:"types,omitempty"`
	StartTime *time.Time          `json:"start_time,omitempty"`
	EndTime   *time.Time          `json:"end_time,omitempty"`
	Limit     int                 `json:"limit,omitempty"`
	Offset    int                 `json:"offset,omitempty"`
	SortBy    string              `json:"sort_by,omitempty"`
	SortOrder string              `json:"sort_order,omitempty"`
}

// StoreStats contains statistics about the store
type StoreStats struct {
	TotalJobs         int64         `json:"total_jobs"`
	ActiveJobs        int64         `json:"active_jobs"`
	TotalExecutions   int64         `json:"total_executions"`
	TotalEvents       int64         `json:"total_events"`
	DatabaseSize      int64         `json:"database_size"`
	ConnectionsActive int           `json:"connections_active"`
	ConnectionsIdle   int           `json:"connections_idle"`
	QueriesExecuted   int64         `json:"queries_executed"`
	AverageQueryTime  time.Duration `json:"average_query_time"`
	LastCleanup       time.Time     `json:"last_cleanup"`
	LastMigration     time.Time     `json:"last_migration"`
	LastResetTime     time.Time     `json:"last_reset_time"`
}

// JobLock represents a lock on a job
type JobLock struct {
	JobID     string                 `json:"job_id"`
	NodeID    string                 `json:"node_id"`
	LockedAt  time.Time              `json:"locked_at"`
	ExpiresAt time.Time              `json:"expires_at"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// StoreError represents a store-specific error
type StoreError struct {
	Operation string `json:"operation"`
	Message   string `json:"message"`
	Cause     error  `json:"cause,omitempty"`
}

// Error implements the error interface
func (se *StoreError) Error() string {
	if se.Cause != nil {
		return fmt.Sprintf("store error in %s: %s: %v", se.Operation, se.Message, se.Cause)
	}
	return fmt.Sprintf("store error in %s: %s", se.Operation, se.Message)
}

// Unwrap returns the underlying error
func (se *StoreError) Unwrap() error {
	return se.Cause
}

// NewStoreError creates a new store error
func NewStoreError(operation, message string, cause error) *StoreError {
	return &StoreError{
		Operation: operation,
		Message:   message,
		Cause:     cause,
	}
}

// Common store errors
var (
	ErrJobNotFound       = NewStoreError("get_job", "job not found", nil)
	ErrJobAlreadyExists  = NewStoreError("create_job", "job already exists", nil)
	ErrJobLocked         = NewStoreError("lock_job", "job is already locked", nil)
	ErrJobNotLocked      = NewStoreError("unlock_job", "job is not locked", nil)
	ErrExecutionNotFound = NewStoreError("get_execution", "execution not found", nil)
	ErrInvalidFilter     = NewStoreError("query", "invalid filter", nil)
	ErrTransactionFailed = NewStoreError("transaction", "transaction failed", nil)
	ErrMigrationFailed   = NewStoreError("migration", "migration failed", nil)
	ErrConnectionFailed  = NewStoreError("connection", "connection failed", nil)
)

// StoreFactory defines the interface for creating stores
type StoreFactory interface {
	CreateStore(config *StoreConfig) (Store, error)
	GetStoreType() string
}

// DefaultStoreConfig returns default configuration for the store
func DefaultStoreConfig() *StoreConfig {
	return &StoreConfig{
		MaxConnections:           10,
		MaxIdleTime:              30 * time.Minute,
		MaxLifetime:              1 * time.Hour,
		ConnectTimeout:           5 * time.Second,
		JobsTable:                "cron_jobs",
		ExecutionsTable:          "cron_executions",
		EventsTable:              "cron_events",
		LocksTable:               "cron_locks",
		CleanupInterval:          1 * time.Hour,
		JobRetentionPeriod:       30 * 24 * time.Hour, // 30 days
		ExecutionRetentionPeriod: 7 * 24 * time.Hour,  // 7 days
		EventRetentionPeriod:     24 * time.Hour,      // 1 day
		LockTimeout:              5 * time.Minute,
		BatchSize:                100,
		EnablePreparedStmts:      true,
		EnableQueryCache:         true,
	}
}

// ValidateStoreConfig validates store configuration
func ValidateStoreConfig(config *StoreConfig) error {
	if config.DatabaseURL == "" {
		return NewStoreError("validate_config", "database URL is required", nil)
	}

	if config.MaxConnections <= 0 {
		return NewStoreError("validate_config", "max connections must be positive", nil)
	}

	if config.MaxIdleTime <= 0 {
		return NewStoreError("validate_config", "max idle time must be positive", nil)
	}

	if config.MaxLifetime <= 0 {
		return NewStoreError("validate_config", "max lifetime must be positive", nil)
	}

	if config.ConnectTimeout <= 0 {
		return NewStoreError("validate_config", "connect timeout must be positive", nil)
	}

	if config.JobsTable == "" {
		return NewStoreError("validate_config", "jobs table name is required", nil)
	}

	if config.ExecutionsTable == "" {
		return NewStoreError("validate_config", "executions table name is required", nil)
	}

	if config.EventsTable == "" {
		return NewStoreError("validate_config", "events table name is required", nil)
	}

	if config.LocksTable == "" {
		return NewStoreError("validate_config", "locks table name is required", nil)
	}

	if config.BatchSize <= 0 {
		return NewStoreError("validate_config", "batch size must be positive", nil)
	}

	return nil
}

// StoreMetrics contains metrics about store operations
type StoreMetrics struct {
	JobsCreated            int64         `json:"jobs_created"`
	JobsUpdated            int64         `json:"jobs_updated"`
	JobsDeleted            int64         `json:"jobs_deleted"`
	ExecutionsCreated      int64         `json:"executions_created"`
	ExecutionsUpdated      int64         `json:"executions_updated"`
	EventsCreated          int64         `json:"events_created"`
	QueriesExecuted        int64         `json:"queries_executed"`
	TransactionsStarted    int64         `json:"transactions_started"`
	TransactionsCommitted  int64         `json:"transactions_committed"`
	TransactionsRolledBack int64         `json:"transactions_rolled_back"`
	AverageQueryTime       time.Duration `json:"average_query_time"`
	LastResetTime          time.Time     `json:"last_reset_time"`
}

// ResetMetrics resets all metrics
func (sm *StoreMetrics) ResetMetrics() {
	sm.JobsCreated = 0
	sm.JobsUpdated = 0
	sm.JobsDeleted = 0
	sm.ExecutionsCreated = 0
	sm.ExecutionsUpdated = 0
	sm.EventsCreated = 0
	sm.QueriesExecuted = 0
	sm.TransactionsStarted = 0
	sm.TransactionsCommitted = 0
	sm.TransactionsRolledBack = 0
	sm.AverageQueryTime = 0
	sm.LastResetTime = time.Now()
}

// StoreObserver defines the interface for store event observers
type StoreObserver interface {
	OnJobCreated(ctx context.Context, job *cron.Job) error
	OnJobUpdated(ctx context.Context, job *cron.Job) error
	OnJobDeleted(ctx context.Context, jobID string) error
	OnExecutionCreated(ctx context.Context, execution *cron.JobExecution) error
	OnExecutionUpdated(ctx context.Context, execution *cron.JobExecution) error
	OnEventCreated(ctx context.Context, event *cron.JobEvent) error
}

// ObservableStore wraps a store with observation capabilities
type ObservableStore struct {
	store     Store
	observers []StoreObserver
}

// NewObservableStore creates a new observable store
func NewObservableStore(store Store) *ObservableStore {
	return &ObservableStore{
		store:     store,
		observers: make([]StoreObserver, 0),
	}
}

// AddObserver adds an observer to the store
func (os *ObservableStore) AddObserver(observer StoreObserver) {
	os.observers = append(os.observers, observer)
}

// RemoveObserver removes an observer from the store
func (os *ObservableStore) RemoveObserver(observer StoreObserver) {
	for i, obs := range os.observers {
		if obs == observer {
			os.observers = append(os.observers[:i], os.observers[i+1:]...)
			break
		}
	}
}

// notifyJobCreated notifies observers about job creation
func (os *ObservableStore) notifyJobCreated(ctx context.Context, job *cron.Job) {
	for _, observer := range os.observers {
		observer.OnJobCreated(ctx, job)
	}
}

// notifyJobUpdated notifies observers about job updates
func (os *ObservableStore) notifyJobUpdated(ctx context.Context, job *cron.Job) {
	for _, observer := range os.observers {
		observer.OnJobUpdated(ctx, job)
	}
}

// notifyJobDeleted notifies observers about job deletion
func (os *ObservableStore) notifyJobDeleted(ctx context.Context, jobID string) {
	for _, observer := range os.observers {
		observer.OnJobDeleted(ctx, jobID)
	}
}

// notifyExecutionCreated notifies observers about execution creation
func (os *ObservableStore) notifyExecutionCreated(ctx context.Context, execution *cron.JobExecution) {
	for _, observer := range os.observers {
		observer.OnExecutionCreated(ctx, execution)
	}
}

// notifyExecutionUpdated notifies observers about execution updates
func (os *ObservableStore) notifyExecutionUpdated(ctx context.Context, execution *cron.JobExecution) {
	for _, observer := range os.observers {
		observer.OnExecutionUpdated(ctx, execution)
	}
}

// notifyEventCreated notifies observers about event creation
func (os *ObservableStore) notifyEventCreated(ctx context.Context, event *cron.JobEvent) {
	for _, observer := range os.observers {
		observer.OnEventCreated(ctx, event)
	}
}
