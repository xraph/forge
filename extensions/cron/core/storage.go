// Package core contains shared interfaces to avoid import cycles.
package core

import (
	"context"
	"time"
)

// Storage defines the interface for persisting jobs and executions.
// Implementations must be thread-safe.
//
// This interface is defined in the core package to avoid import cycles between
// the main cron package and storage implementations. Storage implementations
// should import both core (for the interface) and cron (for the types).
type Storage interface {
	// Connection management
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error

	// Job CRUD operations
	// Note: job, update parameters are *cron.Job, *cron.JobUpdate
	// Returns are *cron.Job, []*cron.Job
	SaveJob(ctx context.Context, job any) error
	GetJob(ctx context.Context, id string) (any, error)
	ListJobs(ctx context.Context) ([]any, error)
	UpdateJob(ctx context.Context, id string, update any) error
	DeleteJob(ctx context.Context, id string) error

	// Execution history
	// Note: exec parameter is *cron.JobExecution, filter is *cron.ExecutionFilter
	// Returns are *cron.JobExecution, []*cron.JobExecution
	SaveExecution(ctx context.Context, exec any) error
	GetExecution(ctx context.Context, id string) (any, error)
	ListExecutions(ctx context.Context, filter any) ([]any, error)
	DeleteExecution(ctx context.Context, id string) error
	DeleteExecutionsBefore(ctx context.Context, before time.Time) (int64, error)
	DeleteExecutionsByJob(ctx context.Context, jobID string) (int64, error)

	// Statistics
	// Returns *cron.JobStats
	GetJobStats(ctx context.Context, jobID string) (any, error)
	GetExecutionCount(ctx context.Context, filter any) (int64, error)

	// Distributed locking (optional, only for Redis backend)
	AcquireLock(ctx context.Context, jobID string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, jobID string) error
	RefreshLock(ctx context.Context, jobID string, ttl time.Duration) error
}
