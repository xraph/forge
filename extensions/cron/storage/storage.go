package storage

// Package storage provides storage implementations for the cron extension.
// Storage implementations use the core.Storage interface to avoid import cycles.

import (
	cron "github.com/xraph/forge/extensions/cron"
	"github.com/xraph/forge/extensions/cron/core"
)

// Verify implementations satisfy the core.Storage interface
var (
	_ core.Storage = (*MemoryStorage)(nil)
	_ core.Storage = (*DatabaseStorage)(nil)
	_ core.Storage = (*RedisStorage)(nil)
)

// Helper functions for type assertions

// ToJob converts interface{} to *cron.Job
func ToJob(v interface{}) *cron.Job {
	if v == nil {
		return nil
	}
	if job, ok := v.(*cron.Job); ok {
		return job
	}
	return nil
}

// ToJobUpdate converts interface{} to *cron.JobUpdate
func ToJobUpdate(v interface{}) *cron.JobUpdate {
	if v == nil {
		return nil
	}
	if update, ok := v.(*cron.JobUpdate); ok {
		return update
	}
	return nil
}

// ToJobExecution converts interface{} to *cron.JobExecution
func ToJobExecution(v interface{}) *cron.JobExecution {
	if v == nil {
		return nil
	}
	if exec, ok := v.(*cron.JobExecution); ok {
		return exec
	}
	return nil
}

// ToExecutionFilter converts interface{} to *cron.ExecutionFilter
func ToExecutionFilter(v interface{}) *cron.ExecutionFilter {
	if v == nil {
		return nil
	}
	if filter, ok := v.(*cron.ExecutionFilter); ok {
		return filter
	}
	return nil
}

// ToJobStats converts interface{} to *cron.JobStats
func ToJobStats(v interface{}) *cron.JobStats {
	if v == nil {
		return nil
	}
	if stats, ok := v.(*cron.JobStats); ok {
		return stats
	}
	return nil
}

// ToJobs converts []interface{} to []*cron.Job
func ToJobs(v []interface{}) []*cron.Job {
	if v == nil {
		return nil
	}
	jobs := make([]*cron.Job, 0, len(v))
	for _, item := range v {
		if job := ToJob(item); job != nil {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

// ToJobExecutions converts []interface{} to []*cron.JobExecution
func ToJobExecutions(v []interface{}) []*cron.JobExecution {
	if v == nil {
		return nil
	}
	execs := make([]*cron.JobExecution, 0, len(v))
	for _, item := range v {
		if exec := ToJobExecution(item); exec != nil {
			execs = append(execs, exec)
		}
	}
	return execs
}
