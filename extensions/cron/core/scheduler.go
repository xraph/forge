package core

import "context"

// Scheduler defines the interface for job schedulers.
//
// This interface is defined in the core package to avoid import cycles between
// the main cron package and scheduler implementations.
type Scheduler interface {
	// Lifecycle methods
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
	IsLeader() bool // For distributed schedulers

	// Job management
	// Note: job parameter is *cron.Job, returns []*cron.Job
	AddJob(job interface{}) error
	RemoveJob(jobID string) error
	UpdateJob(job interface{}) error
	ListJobs() ([]interface{}, error)
	GetJob(jobID string) (interface{}, error)

	// Manual execution
	TriggerJob(ctx context.Context, jobID string) (string, error)
}
