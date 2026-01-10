package storage

import (
	"context"
	"sort"
	"sync"
	"time"

	cron "github.com/xraph/forge/extensions/cron"
	"github.com/xraph/forge/extensions/cron/core"
)

func init() {
	// Register the memory storage factory
	core.RegisterStorageFactory("memory", func(configInterface interface{}) (core.Storage, error) {
		return NewMemoryStorage(), nil
	})
}

// MemoryStorage implements Storage using in-memory maps.
// This implementation is suitable for development and testing but does not persist data.
type MemoryStorage struct {
	jobs       map[string]*cron.Job
	executions map[string]*cron.JobExecution
	locks      map[string]*lockInfo
	mu         sync.RWMutex
	connected  bool
}

type lockInfo struct {
	acquiredAt time.Time
	ttl        time.Duration
}

// NewMemoryStorage creates a new in-memory storage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		jobs:       make(map[string]*cron.Job),
		executions: make(map[string]*cron.JobExecution),
		locks:      make(map[string]*lockInfo),
		connected:  false,
	}
}

// Connect initializes the storage (no-op for memory).
func (s *MemoryStorage) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = true

	return nil
}

// Disconnect closes the storage (no-op for memory).
func (s *MemoryStorage) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false

	return nil
}

// Ping checks if the storage is available.
func (s *MemoryStorage) Ping(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.connected {
		return cron.ErrStorageError
	}

	return nil
}

// SaveJob saves or updates a job.
func (s *MemoryStorage) SaveJob(ctx context.Context, jobInterface interface{}) error {
	job := ToJob(jobInterface)
	if job == nil {
		return cron.ErrInvalidConfig
	}

	if job.ID == "" {
		return cron.ErrInvalidConfig
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a copy to avoid external modifications
	jobCopy := *job
	s.jobs[job.ID] = &jobCopy

	return nil
}

// GetJob retrieves a job by ID.
func (s *MemoryStorage) GetJob(ctx context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[id]
	if !exists {
		return nil, cron.ErrJobNotFound
	}

	// Return a copy to avoid external modifications
	jobCopy := *job

	return &jobCopy, nil
}

// ListJobs retrieves all jobs.
func (s *MemoryStorage) ListJobs(ctx context.Context) ([]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*cron.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobCopy := *job
		jobs = append(jobs, &jobCopy)
	}

	// Sort by ID for consistent ordering
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	// Convert to []interface{}
	result := make([]interface{}, len(jobs))
	for i, job := range jobs {
		result[i] = job
	}

	return result, nil
}

// UpdateJob updates a job with the given changes.
func (s *MemoryStorage) UpdateJob(ctx context.Context, id string, updateInterface interface{}) error {
	update := ToJobUpdate(updateInterface)
	if update == nil {
		return cron.ErrInvalidConfig
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[id]
	if !exists {
		return cron.ErrJobNotFound
	}

	// Apply updates
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
func (s *MemoryStorage) DeleteJob(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[id]; !exists {
		return cron.ErrJobNotFound
	}

	delete(s.jobs, id)

	return nil
}

// SaveExecution saves a job execution.
func (s *MemoryStorage) SaveExecution(ctx context.Context, execInterface interface{}) error {
	exec := ToJobExecution(execInterface)
	if exec == nil {
		return cron.ErrInvalidConfig
	}

	if exec.ID == "" {
		return cron.ErrInvalidConfig
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a copy
	execCopy := *exec
	s.executions[exec.ID] = &execCopy

	return nil
}

// GetExecution retrieves an execution by ID.
func (s *MemoryStorage) GetExecution(ctx context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, exists := s.executions[id]
	if !exists {
		return nil, cron.ErrExecutionNotFound
	}

	// Return a copy
	execCopy := *exec

	return &execCopy, nil
}

// ListExecutions retrieves executions matching the filter.
func (s *MemoryStorage) ListExecutions(ctx context.Context, filterInterface interface{}) ([]interface{}, error) {
	filter := ToExecutionFilter(filterInterface)

	s.mu.RLock()
	defer s.mu.RUnlock()

	executions := make([]*cron.JobExecution, 0)

	for _, exec := range s.executions {
		// Apply filters
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

	// Sort by started time (most recent first)
	sort.Slice(executions, func(i, j int) bool {
		return executions[i].StartedAt.After(executions[j].StartedAt)
	})

	// Apply offset and limit
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

	// Convert to []interface{}
	result := make([]interface{}, len(executions))
	for i, exec := range executions {
		result[i] = exec
	}

	return result, nil
}

// DeleteExecution removes an execution.
func (s *MemoryStorage) DeleteExecution(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.executions[id]; !exists {
		return cron.ErrExecutionNotFound
	}

	delete(s.executions, id)

	return nil
}

// DeleteExecutionsBefore removes executions older than the given time.
func (s *MemoryStorage) DeleteExecutionsBefore(ctx context.Context, before time.Time) (int64, error) {
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
func (s *MemoryStorage) DeleteExecutionsByJob(ctx context.Context, jobID string) (int64, error) {
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
func (s *MemoryStorage) GetJobStats(ctx context.Context, jobID string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return nil, cron.ErrJobNotFound
	}

	stats := &cron.JobStats{
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
		case cron.ExecutionStatusSuccess:
			stats.SuccessCount++
			if stats.LastSuccess == nil || exec.CompletedAt.After(*stats.LastSuccess) {
				stats.LastSuccess = exec.CompletedAt
			}
		case cron.ExecutionStatusFailed:
			stats.FailureCount++
			if stats.LastFailure == nil || exec.CompletedAt.After(*stats.LastFailure) {
				stats.LastFailure = exec.CompletedAt
			}
		case cron.ExecutionStatusCancelled:
			stats.CancelCount++
		case cron.ExecutionStatusTimeout:
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
func (s *MemoryStorage) GetExecutionCount(ctx context.Context, filterInterface interface{}) (int64, error) {
	filter := ToExecutionFilter(filterInterface)

	s.mu.RLock()
	defer s.mu.RUnlock()

	count := int64(0)

	for _, exec := range s.executions {
		// Apply filters
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
// For memory storage, this is a simple mutex-based implementation.
func (s *MemoryStorage) AcquireLock(ctx context.Context, jobID string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if lock exists and is still valid
	if lock, exists := s.locks[jobID]; exists {
		if time.Since(lock.acquiredAt) < lock.ttl {
			return false, nil // Lock is still held
		}
		// Lock expired, can be acquired
	}

	// Acquire lock
	s.locks[jobID] = &lockInfo{
		acquiredAt: time.Now(),
		ttl:        ttl,
	}

	return true, nil
}

// ReleaseLock releases a distributed lock.
func (s *MemoryStorage) ReleaseLock(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.locks, jobID)

	return nil
}

// RefreshLock extends the TTL of an existing lock.
func (s *MemoryStorage) RefreshLock(ctx context.Context, jobID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lock, exists := s.locks[jobID]
	if !exists {
		return cron.ErrLockAcquisitionFailed
	}

	// Check if lock expired
	if time.Since(lock.acquiredAt) >= lock.ttl {
		delete(s.locks, jobID)

		return cron.ErrLockAcquisitionFailed
	}

	// Refresh lock
	lock.acquiredAt = time.Now()
	lock.ttl = ttl

	return nil
}
