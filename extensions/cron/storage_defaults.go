package cron

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/extensions/cron/core"
)

func init() {
	// Register built-in storage factories to avoid import cycles.
	// These are registered here because the storage subpackage imports cron,
	// which would create a cycle if we tried to import storage from extension.go.

	// Register memory storage factory
	core.RegisterStorageFactory("memory", func(configInterface interface{}) (core.Storage, error) {
		return newDefaultMemoryStorage(), nil
	})
}

// defaultMemoryStorage is a simple in-memory storage implementation.
// This is registered as the default "memory" storage to avoid import cycles.
type defaultMemoryStorage struct {
	jobs       map[string]*Job
	executions map[string]*JobExecution
	locks      map[string]*defaultLockInfo
	mu         sync.RWMutex
	connected  bool
}

type defaultLockInfo struct {
	acquiredAt time.Time
	ttl        time.Duration
}

// newDefaultMemoryStorage creates a new in-memory storage.
func newDefaultMemoryStorage() *defaultMemoryStorage {
	return &defaultMemoryStorage{
		jobs:       make(map[string]*Job),
		executions: make(map[string]*JobExecution),
		locks:      make(map[string]*defaultLockInfo),
		connected:  false,
	}
}

// Connect initializes the storage.
func (s *defaultMemoryStorage) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = true

	return nil
}

// Disconnect closes the storage.
func (s *defaultMemoryStorage) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false

	return nil
}

// Ping checks if the storage is available.
func (s *defaultMemoryStorage) Ping(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.connected {
		return ErrStorageError
	}

	return nil
}

// SaveJob saves or updates a job.
func (s *defaultMemoryStorage) SaveJob(ctx context.Context, jobInterface interface{}) error {
	job, ok := jobInterface.(*Job)
	if !ok || job == nil {
		return ErrInvalidConfig
	}

	if job.ID == "" {
		return ErrInvalidConfig
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	jobCopy := *job
	s.jobs[job.ID] = &jobCopy

	return nil
}

// GetJob retrieves a job by ID.
func (s *defaultMemoryStorage) GetJob(ctx context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[id]
	if !exists {
		return nil, ErrJobNotFound
	}

	jobCopy := *job

	return &jobCopy, nil
}

// ListJobs retrieves all jobs.
func (s *defaultMemoryStorage) ListJobs(ctx context.Context) ([]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobCopy := *job
		jobs = append(jobs, &jobCopy)
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	result := make([]interface{}, len(jobs))
	for i, job := range jobs {
		result[i] = job
	}

	return result, nil
}

// UpdateJob updates a job with the given changes.
func (s *defaultMemoryStorage) UpdateJob(ctx context.Context, id string, updateInterface interface{}) error {
	update, ok := updateInterface.(*JobUpdate)
	if !ok || update == nil {
		return ErrInvalidConfig
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[id]
	if !exists {
		return ErrJobNotFound
	}

	if update.Name != nil {
		job.Name = *update.Name
	}

	if update.Schedule != nil {
		job.Schedule = *update.Schedule
	}

	if update.HandlerName != nil {
		job.HandlerName = *update.HandlerName
	}

	if update.Command != nil {
		job.Command = *update.Command
	}

	if update.Args != nil {
		job.Args = update.Args
	}

	if update.Env != nil {
		job.Env = update.Env
	}

	if update.WorkingDir != nil {
		job.WorkingDir = *update.WorkingDir
	}

	if update.Payload != nil {
		job.Payload = update.Payload
	}

	if update.Enabled != nil {
		job.Enabled = *update.Enabled
	}

	if update.MaxRetries != nil {
		job.MaxRetries = *update.MaxRetries
	}

	if update.Timeout != nil {
		job.Timeout = *update.Timeout
	}

	if update.Metadata != nil {
		job.Metadata = update.Metadata
	}

	if update.Tags != nil {
		job.Tags = update.Tags
	}

	job.UpdatedAt = time.Now()

	return nil
}

// DeleteJob removes a job.
func (s *defaultMemoryStorage) DeleteJob(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[id]; !exists {
		return ErrJobNotFound
	}

	delete(s.jobs, id)

	return nil
}

// SaveExecution saves a job execution.
func (s *defaultMemoryStorage) SaveExecution(ctx context.Context, execInterface interface{}) error {
	exec, ok := execInterface.(*JobExecution)
	if !ok || exec == nil {
		return ErrInvalidConfig
	}

	if exec.ID == "" {
		return ErrInvalidConfig
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	execCopy := *exec
	s.executions[exec.ID] = &execCopy

	return nil
}

// GetExecution retrieves an execution by ID.
func (s *defaultMemoryStorage) GetExecution(ctx context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, exists := s.executions[id]
	if !exists {
		return nil, ErrExecutionNotFound
	}

	execCopy := *exec

	return &execCopy, nil
}

// ListExecutions retrieves executions matching the filter.
func (s *defaultMemoryStorage) ListExecutions(ctx context.Context, filterInterface interface{}) ([]interface{}, error) {
	filter, _ := filterInterface.(*ExecutionFilter)

	s.mu.RLock()
	defer s.mu.RUnlock()

	executions := make([]*JobExecution, 0)

	for _, exec := range s.executions {
		if filter != nil {
			if filter.JobID != "" && exec.JobID != filter.JobID {
				continue
			}

			if len(filter.Status) > 0 {
				matchStatus := false

				for _, status := range filter.Status {
					if exec.Status == status {
						matchStatus = true

						break
					}
				}

				if !matchStatus {
					continue
				}
			}

			if filter.After != nil && exec.StartedAt.Before(*filter.After) {
				continue
			}

			if filter.Before != nil && exec.StartedAt.After(*filter.Before) {
				continue
			}

			if filter.NodeID != "" && exec.NodeID != filter.NodeID {
				continue
			}
		}

		execCopy := *exec
		executions = append(executions, &execCopy)
	}

	sort.Slice(executions, func(i, j int) bool {
		return executions[i].StartedAt.After(executions[j].StartedAt)
	})

	if filter != nil {
		if filter.Offset > 0 {
			if filter.Offset >= len(executions) {
				return []interface{}{}, nil
			}

			executions = executions[filter.Offset:]
		}

		if filter.Limit > 0 && filter.Limit < len(executions) {
			executions = executions[:filter.Limit]
		}
	}

	result := make([]interface{}, len(executions))
	for i, exec := range executions {
		result[i] = exec
	}

	return result, nil
}

// DeleteExecution removes an execution.
func (s *defaultMemoryStorage) DeleteExecution(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.executions[id]; !exists {
		return ErrExecutionNotFound
	}

	delete(s.executions, id)

	return nil
}

// DeleteExecutionsBefore removes executions older than the given time.
func (s *defaultMemoryStorage) DeleteExecutionsBefore(ctx context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := int64(0)

	for id, exec := range s.executions {
		if exec.StartedAt.Before(before) {
			delete(s.executions, id)

			count++
		}
	}

	return count, nil
}

// DeleteExecutionsByJob removes all executions for a job.
func (s *defaultMemoryStorage) DeleteExecutionsByJob(ctx context.Context, jobID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := int64(0)

	for id, exec := range s.executions {
		if exec.JobID == jobID {
			delete(s.executions, id)

			count++
		}
	}

	return count, nil
}

// GetJobStats calculates statistics for a job.
func (s *defaultMemoryStorage) GetJobStats(ctx context.Context, jobID string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return nil, ErrJobNotFound
	}

	stats := &JobStats{
		JobID:   jobID,
		JobName: job.Name,
	}

	var (
		totalDuration time.Duration
		minDuration   time.Duration
		maxDuration   time.Duration
	)

	for _, exec := range s.executions {
		if exec.JobID != jobID {
			continue
		}

		stats.TotalExecutions++

		switch exec.Status {
		case ExecutionStatusSuccess:
			stats.SuccessCount++
			if stats.LastSuccess == nil || exec.CompletedAt.After(*stats.LastSuccess) {
				stats.LastSuccess = exec.CompletedAt
			}
		case ExecutionStatusFailed:
			stats.FailureCount++
			if stats.LastFailure == nil || exec.CompletedAt.After(*stats.LastFailure) {
				stats.LastFailure = exec.CompletedAt
			}
		case ExecutionStatusCancelled:
			stats.CancelCount++
		case ExecutionStatusTimeout:
			stats.TimeoutCount++
		}

		if exec.CompletedAt != nil {
			if stats.LastExecution == nil || exec.CompletedAt.After(*stats.LastExecution) {
				stats.LastExecution = exec.CompletedAt
			}
		}

		if exec.Duration > 0 {
			totalDuration += exec.Duration
			if minDuration == 0 || exec.Duration < minDuration {
				minDuration = exec.Duration
			}

			if exec.Duration > maxDuration {
				maxDuration = exec.Duration
			}
		}
	}

	if stats.TotalExecutions > 0 {
		stats.AverageDuration = totalDuration / time.Duration(stats.TotalExecutions)
		stats.MinDuration = minDuration
		stats.MaxDuration = maxDuration
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalExecutions) * 100.0
	}

	stats.NextExecution = job.NextExecutionAt

	return stats, nil
}

// GetExecutionCount returns the number of executions matching the filter.
func (s *defaultMemoryStorage) GetExecutionCount(ctx context.Context, filterInterface interface{}) (int64, error) {
	filter, _ := filterInterface.(*ExecutionFilter)

	s.mu.RLock()
	defer s.mu.RUnlock()

	count := int64(0)

	for _, exec := range s.executions {
		if filter != nil {
			if filter.JobID != "" && exec.JobID != filter.JobID {
				continue
			}

			if len(filter.Status) > 0 {
				matchStatus := false

				for _, status := range filter.Status {
					if exec.Status == status {
						matchStatus = true

						break
					}
				}

				if !matchStatus {
					continue
				}
			}

			if filter.After != nil && exec.StartedAt.Before(*filter.After) {
				continue
			}

			if filter.Before != nil && exec.StartedAt.After(*filter.Before) {
				continue
			}

			if filter.NodeID != "" && exec.NodeID != filter.NodeID {
				continue
			}
		}

		count++
	}

	return count, nil
}

// AcquireLock attempts to acquire a distributed lock.
func (s *defaultMemoryStorage) AcquireLock(ctx context.Context, jobID string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if lock, exists := s.locks[jobID]; exists {
		if time.Since(lock.acquiredAt) < lock.ttl {
			return false, nil
		}
	}

	s.locks[jobID] = &defaultLockInfo{
		acquiredAt: time.Now(),
		ttl:        ttl,
	}

	return true, nil
}

// ReleaseLock releases a distributed lock.
func (s *defaultMemoryStorage) ReleaseLock(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.locks, jobID)

	return nil
}

// RefreshLock extends the TTL of an existing lock.
func (s *defaultMemoryStorage) RefreshLock(ctx context.Context, jobID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lock, exists := s.locks[jobID]
	if !exists {
		return ErrLockAcquisitionFailed
	}

	if time.Since(lock.acquiredAt) >= lock.ttl {
		delete(s.locks, jobID)

		return ErrLockAcquisitionFailed
	}

	lock.acquiredAt = time.Now()
	lock.ttl = ttl

	return nil
}
