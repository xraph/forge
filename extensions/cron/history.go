package cron

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// HistoryTracker manages job execution history with automatic cleanup.
type HistoryTracker struct {
	config  Config
	storage Storage
	logger  forge.Logger

	// Cleanup goroutine
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Stats cache
	statsCache     map[string]*JobStats
	statsCacheMu   sync.RWMutex
	lastStatsCache time.Time
}

// NewHistoryTracker creates a new history tracker.
func NewHistoryTracker(config Config, storage Storage, logger forge.Logger) *HistoryTracker {
	ctx, cancel := context.WithCancel(context.Background())

	return &HistoryTracker{
		config:     config,
		storage:    storage,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		statsCache: make(map[string]*JobStats),
	}
}

// Start starts the history tracker and its cleanup routine.
func (h *HistoryTracker) Start(ctx context.Context) error {
	h.logger.Info("starting history tracker",
		forge.F("retention_days", h.config.HistoryRetentionDays),
		forge.F("max_records", h.config.MaxHistoryRecords),
	)

	// Start cleanup routine
	h.wg.Add(1)
	go h.cleanupRoutine()

	return nil
}

// Stop stops the history tracker.
func (h *HistoryTracker) Stop(ctx context.Context) error {
	h.logger.Info("stopping history tracker")

	h.cancel()
	h.wg.Wait()

	return nil
}

// cleanupRoutine periodically cleans up old execution history.
func (h *HistoryTracker) cleanupRoutine() {
	defer h.wg.Done()

	// Run cleanup immediately on start
	h.cleanup()

	// Run cleanup every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.cleanup()
		}
	}
}

// cleanup removes old execution records based on retention policy.
func (h *HistoryTracker) cleanup() {
	h.logger.Debug("running history cleanup")

	// Calculate cutoff time
	if h.config.HistoryRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -h.config.HistoryRetentionDays)

		count, err := h.storage.DeleteExecutionsBefore(h.ctx, cutoff)
		if err != nil {
			h.logger.Error("failed to clean up old executions",
				forge.F("error", err),
			)
			return
		}

		if count > 0 {
			h.logger.Info("cleaned up old executions",
				forge.F("count", count),
				forge.F("cutoff", cutoff),
			)
		}
	}

	// TODO: Implement max records cleanup if needed
	// This would require more complex logic to keep the most recent N records per job
}

// GetExecutions retrieves execution history with filtering.
func (h *HistoryTracker) GetExecutions(ctx context.Context, filter *ExecutionFilter) ([]*JobExecution, error) {
	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}
	return toJobExecutions(result), nil
}

// GetExecution retrieves a single execution by ID.
func (h *HistoryTracker) GetExecution(ctx context.Context, executionID string) (*JobExecution, error) {
	result, err := h.storage.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	return toJobExecution(result), nil
}

// GetJobExecutions retrieves executions for a specific job.
func (h *HistoryTracker) GetJobExecutions(ctx context.Context, jobID string, limit int) ([]*JobExecution, error) {
	filter := &ExecutionFilter{
		JobID: jobID,
		Limit: limit,
	}
	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}
	return toJobExecutions(result), nil
}

// GetRecentExecutions retrieves the most recent executions across all jobs.
func (h *HistoryTracker) GetRecentExecutions(ctx context.Context, limit int) ([]*JobExecution, error) {
	filter := &ExecutionFilter{
		Limit: limit,
	}
	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}
	return toJobExecutions(result), nil
}

// GetFailedExecutions retrieves failed executions.
func (h *HistoryTracker) GetFailedExecutions(ctx context.Context, limit int) ([]*JobExecution, error) {
	filter := &ExecutionFilter{
		Status: []ExecutionStatus{ExecutionStatusFailed, ExecutionStatusTimeout},
		Limit:  limit,
	}
	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}
	return toJobExecutions(result), nil
}

// GetJobStats retrieves aggregated statistics for a job.
// Results are cached for 1 minute to reduce storage queries.
func (h *HistoryTracker) GetJobStats(ctx context.Context, jobID string) (*JobStats, error) {
	// Check cache
	h.statsCacheMu.RLock()
	if stats, exists := h.statsCache[jobID]; exists {
		if time.Since(h.lastStatsCache) < 1*time.Minute {
			h.statsCacheMu.RUnlock()
			return stats, nil
		}
	}
	h.statsCacheMu.RUnlock()

	// Fetch from storage
	result, err := h.storage.GetJobStats(ctx, jobID)
	if err != nil {
		return nil, err
	}

	stats := toJobStats(result)
	if stats == nil {
		return nil, ErrInvalidData
	}

	// Update cache
	h.statsCacheMu.Lock()
	h.statsCache[jobID] = stats
	h.lastStatsCache = time.Now()
	h.statsCacheMu.Unlock()

	return stats, nil
}

// GetAllJobStats retrieves statistics for all jobs.
func (h *HistoryTracker) GetAllJobStats(ctx context.Context) (map[string]*JobStats, error) {
	// Get all jobs
	result, err := h.storage.ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	jobs := toJobs(result)

	stats := make(map[string]*JobStats)
	for _, job := range jobs {
		if job == nil {
			continue
		}
		jobStats, err := h.GetJobStats(ctx, job.ID)
		if err != nil {
			h.logger.Error("failed to get job stats",
				forge.String("job_id", job.ID),
				forge.Error(err),
			)
			continue
		}
		stats[job.ID] = jobStats
	}

	return stats, nil
}

// GetExecutionCount returns the total number of executions matching the filter.
func (h *HistoryTracker) GetExecutionCount(ctx context.Context, filter *ExecutionFilter) (int64, error) {
	return h.storage.GetExecutionCount(ctx, filter)
}

// GetExecutionsByStatus retrieves executions with a specific status.
func (h *HistoryTracker) GetExecutionsByStatus(ctx context.Context, status ExecutionStatus, limit int) ([]*JobExecution, error) {
	filter := &ExecutionFilter{
		Status: []ExecutionStatus{status},
		Limit:  limit,
	}
	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}
	return toJobExecutions(result), nil
}

// GetExecutionsByDateRange retrieves executions within a date range.
func (h *HistoryTracker) GetExecutionsByDateRange(ctx context.Context, after, before time.Time, limit int) ([]*JobExecution, error) {
	filter := &ExecutionFilter{
		After:  &after,
		Before: &before,
		Limit:  limit,
	}
	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}
	return toJobExecutions(result), nil
}

// DeleteOldExecutions manually triggers cleanup of old executions.
func (h *HistoryTracker) DeleteOldExecutions(ctx context.Context, before time.Time) (int64, error) {
	count, err := h.storage.DeleteExecutionsBefore(ctx, before)
	if err != nil {
		return 0, err
	}

	h.logger.Info("manually deleted old executions",
		forge.F("count", count),
		forge.F("before", before),
	)

	// Invalidate stats cache
	h.invalidateStatsCache()

	return count, nil
}

// DeleteJobExecutions deletes all executions for a specific job.
func (h *HistoryTracker) DeleteJobExecutions(ctx context.Context, jobID string) (int64, error) {
	count, err := h.storage.DeleteExecutionsByJob(ctx, jobID)
	if err != nil {
		return 0, err
	}

	h.logger.Info("deleted job executions",
		forge.F("job_id", jobID),
		forge.F("count", count),
	)

	// Invalidate stats cache
	h.invalidateStatsCache()

	return count, nil
}

// invalidateStatsCache clears the statistics cache.
func (h *HistoryTracker) invalidateStatsCache() {
	h.statsCacheMu.Lock()
	defer h.statsCacheMu.Unlock()

	h.statsCache = make(map[string]*JobStats)
}

// GetSuccessRate calculates the success rate for a job.
func (h *HistoryTracker) GetSuccessRate(ctx context.Context, jobID string) (float64, error) {
	stats, err := h.GetJobStats(ctx, jobID)
	if err != nil {
		return 0, err
	}

	return stats.SuccessRate, nil
}

// GetAverageDuration calculates the average execution duration for a job.
func (h *HistoryTracker) GetAverageDuration(ctx context.Context, jobID string) (time.Duration, error) {
	stats, err := h.GetJobStats(ctx, jobID)
	if err != nil {
		return 0, err
	}

	return stats.AverageDuration, nil
}

// GetLastExecution retrieves the most recent execution for a job.
func (h *HistoryTracker) GetLastExecution(ctx context.Context, jobID string) (*JobExecution, error) {
	filter := &ExecutionFilter{
		JobID: jobID,
		Limit: 1,
	}

	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}

	executions := toJobExecutions(result)
	if len(executions) == 0 {
		return nil, ErrExecutionNotFound
	}

	return executions[0], nil
}

// GetLastSuccessfulExecution retrieves the most recent successful execution for a job.
func (h *HistoryTracker) GetLastSuccessfulExecution(ctx context.Context, jobID string) (*JobExecution, error) {
	filter := &ExecutionFilter{
		JobID:  jobID,
		Status: []ExecutionStatus{ExecutionStatusSuccess},
		Limit:  1,
	}

	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}

	executions := toJobExecutions(result)
	if len(executions) == 0 {
		return nil, ErrExecutionNotFound
	}

	return executions[0], nil
}

// GetExecutionTrend calculates execution trends over time.
func (h *HistoryTracker) GetExecutionTrend(ctx context.Context, jobID string, duration time.Duration) (map[string]int, error) {
	now := time.Now()
	after := now.Add(-duration)

	filter := &ExecutionFilter{
		JobID: jobID,
		After: &after,
	}

	result, err := h.storage.ListExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}

	executions := toJobExecutions(result)

	// Group by status
	trend := make(map[string]int)
	for _, exec := range executions {
		if exec != nil {
			trend[string(exec.Status)]++
		}
	}

	return trend, nil
}

// Helper functions for type assertions

// toJob converts interface{} to *Job
func toJob(v interface{}) *Job {
	if v == nil {
		return nil
	}
	if job, ok := v.(*Job); ok {
		return job
	}
	return nil
}

// toJobExecution converts interface{} to *JobExecution
func toJobExecution(v interface{}) *JobExecution {
	if v == nil {
		return nil
	}
	if exec, ok := v.(*JobExecution); ok {
		return exec
	}
	return nil
}

// toJobStats converts interface{} to *JobStats
func toJobStats(v interface{}) *JobStats {
	if v == nil {
		return nil
	}
	if stats, ok := v.(*JobStats); ok {
		return stats
	}
	return nil
}

// toJobs converts []interface{} to []*Job
func toJobs(v []interface{}) []*Job {
	if v == nil {
		return nil
	}
	jobs := make([]*Job, 0, len(v))
	for _, item := range v {
		if job := toJob(item); job != nil {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

// toJobExecutions converts []interface{} to []*JobExecution
func toJobExecutions(v []interface{}) []*JobExecution {
	if v == nil {
		return nil
	}
	execs := make([]*JobExecution, 0, len(v))
	for _, item := range v {
		if exec := toJobExecution(item); exec != nil {
			execs = append(execs, exec)
		}
	}
	return execs
}
