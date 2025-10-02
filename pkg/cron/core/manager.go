package core

import (
	"context"
)

// Manager defines the interface for cron management operations
type Manager interface {

	// Name returns the name of the manager or job.
	Name() string

	// Dependencies returns a list of job dependencies required for execution.
	Dependencies() []string

	// OnStart initializes the manager or a specific component, allowing it to prepare for operation within the given context.
	OnStart(ctx context.Context) error

	// OnStop is called to stop the manager gracefully, releasing any resources. Returns an error if the stop process fails.
	OnStop(ctx context.Context) error

	// OnHealthCheck performs a health check on the cron manager and returns an error if any issues are detected.
	OnHealthCheck(ctx context.Context) error

	// RegisterJob registers a new cron job with the provided job definition. It returns an error if the registration fails.
	RegisterJob(jobDef JobDefinition) error

	// UnregisterJob removes a registered job by its ID from the scheduling system. Returns an error if the operation fails.
	UnregisterJob(jobID string) error

	// GetJob retrieves the details of a job using its unique identifier. Returns the job or an error if not found.
	GetJob(jobID string) (*Job, error)

	// GetJobs retrieves a list of all registered cron jobs, including their definitions and current statuses.
	GetJobs() []Job

	// TriggerJob triggers the execution of a job by its ID, returning an error if the job cannot be executed.
	TriggerJob(ctx context.Context, jobID string) error

	// GetJobHistory retrieves the execution history for the specified job ID, limited to the given number of records.
	GetJobHistory(jobID string, limit int) ([]JobExecution, error)

	// GetStats retrieves the overall statistics of the cron manager, including job counts, execution rates, and cluster status.
	GetStats() CronStats

	// RegisterHandler registers a handler for a specific job ID. The handler will execute the job's logic during its run.
	RegisterHandler(jobID string, handler JobHandler) error

	// UnregisterHandler removes a previously registered job handler by its job ID.
	UnregisterHandler(jobID string) error

	IsRunning() bool
}
