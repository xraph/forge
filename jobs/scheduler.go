package jobs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/xraph/forge/logger"
)

// scheduler implements the Scheduler interface
type scheduler struct {
	config     SchedulerConfig
	processor  Processor
	storage    ScheduleStorage
	logger     logger.Logger
	cronParser cron.Parser

	mu        sync.RWMutex
	schedules map[string]*scheduleEntry
	running   map[string]*runningExecution
	started   bool
	ctx       context.Context
	cancel    context.CancelFunc
	ticker    *time.Ticker

	// Statistics
	stats      *SchedulerStats
	executions []ExecutionHistory
	execMu     sync.RWMutex
}

// scheduleEntry represents an internal schedule entry
type scheduleEntry struct {
	Schedule
	cronSchedule cron.Schedule
	nextRun      time.Time
	lastRun      *time.Time
	isRunning    bool
	runCount     int64
	errorCount   int64
	totalRunTime time.Duration
}

// runningExecution represents a currently running scheduled execution
type runningExecution struct {
	ScheduleID string
	JobID      string
	StartedAt  time.Time
	cancel     context.CancelFunc
}

// SchedulerStats represents scheduler statistics
type SchedulerStats struct {
	TotalSchedules    int           `json:"total_schedules"`
	ActiveSchedules   int           `json:"active_schedules"`
	RunningExecutions int           `json:"running_executions"`
	TotalExecutions   int64         `json:"total_executions"`
	SuccessfulRuns    int64         `json:"successful_runs"`
	FailedRuns        int64         `json:"failed_runs"`
	AverageRunTime    time.Duration `json:"average_run_time"`
	NextExecution     *time.Time    `json:"next_execution,omitempty"`
	LastExecution     *time.Time    `json:"last_execution,omitempty"`
	OverdueSchedules  []string      `json:"overdue_schedules,omitempty"`
	Uptime            time.Duration `json:"uptime"`
	LastUpdated       time.Time     `json:"last_updated"`
}

// ScheduleStorage interface for persisting schedules
type ScheduleStorage interface {
	SaveSchedule(ctx context.Context, schedule Schedule) error
	GetSchedule(ctx context.Context, scheduleID string) (*Schedule, error)
	UpdateSchedule(ctx context.Context, schedule Schedule) error
	DeleteSchedule(ctx context.Context, scheduleID string) error
	ListSchedules(ctx context.Context) ([]Schedule, error)
	SaveExecution(ctx context.Context, execution ExecutionHistory) error
	GetExecutionHistory(ctx context.Context, scheduleID string, limit int) ([]ExecutionHistory, error)
	CleanupExecutions(ctx context.Context, olderThan time.Time) error
}

// NewScheduler creates a new job scheduler
func NewScheduler(config SchedulerConfig, processor Processor, storage ScheduleStorage, logger logger.Logger) Scheduler {
	return &scheduler{
		config:     config,
		processor:  processor,
		storage:    storage,
		logger:     logger.Named("scheduler"),
		cronParser: cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
		schedules:  make(map[string]*scheduleEntry),
		running:    make(map[string]*runningExecution),
		stats:      &SchedulerStats{},
		executions: make([]ExecutionHistory, 0),
	}
}

// Schedule creates or updates a schedule
func (s *scheduler) Schedule(ctx context.Context, schedule Schedule) error {
	if !s.isStarted() {
		return fmt.Errorf("scheduler not started")
	}

	// Validate schedule
	if err := s.validateSchedule(schedule); err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}

	// Parse cron expression
	cronSchedule, err := s.cronParser.Parse(schedule.CronExpression)
	if err != nil {
		return fmt.Errorf("invalid cron expression %s: %w", schedule.CronExpression, err)
	}

	// Set timestamps
	now := time.Now()
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = now
	}
	schedule.UpdatedAt = now

	// Calculate next run time
	var nextRun time.Time
	if schedule.StartTime != nil && schedule.StartTime.After(now) {
		nextRun = cronSchedule.Next(*schedule.StartTime)
	} else {
		nextRun = cronSchedule.Next(now)
	}
	schedule.NextRun = &nextRun

	// Save to storage
	if err := s.storage.SaveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	// Create schedule entry
	entry := &scheduleEntry{
		Schedule:     schedule,
		cronSchedule: cronSchedule,
		nextRun:      nextRun,
	}

	s.mu.Lock()
	s.schedules[schedule.ID] = entry
	s.mu.Unlock()

	s.logger.Info("Schedule created",
		logger.String("schedule_id", schedule.ID),
		logger.String("name", schedule.Name),
		logger.String("cron", schedule.CronExpression),
		logger.Time("next_run", nextRun),
		logger.Bool("enabled", schedule.Enabled),
	)

	return nil
}

// Unschedule removes a schedule
func (s *scheduler) Unschedule(ctx context.Context, scheduleID string) error {
	if !s.isStarted() {
		return fmt.Errorf("scheduler not started")
	}

	s.mu.Lock()
	entry, exists := s.schedules[scheduleID]
	if !exists {
		s.mu.Unlock()
		return ErrScheduleNotFound
	}

	// Cancel if currently running
	if running, isRunning := s.running[scheduleID]; isRunning {
		running.cancel()
		delete(s.running, scheduleID)
	}

	delete(s.schedules, scheduleID)
	s.mu.Unlock()

	// Delete from storage
	if err := s.storage.DeleteSchedule(ctx, scheduleID); err != nil {
		s.logger.Error("Failed to delete schedule from storage",
			logger.String("schedule_id", scheduleID),
			logger.Error(err),
		)
	}

	s.logger.Info("Schedule removed",
		logger.String("schedule_id", scheduleID),
		logger.String("name", entry.Name),
	)

	return nil
}

// UpdateSchedule updates an existing schedule
func (s *scheduler) UpdateSchedule(ctx context.Context, scheduleID string, schedule Schedule) error {
	if !s.isStarted() {
		return fmt.Errorf("scheduler not started")
	}

	s.mu.Lock()
	entry, exists := s.schedules[scheduleID]
	if !exists {
		s.mu.Unlock()
		return ErrScheduleNotFound
	}

	// Preserve some fields from existing schedule
	schedule.ID = scheduleID
	schedule.CreatedAt = entry.CreatedAt
	schedule.RunCount = entry.RunCount
	schedule.ErrorCount = entry.ErrorCount
	s.mu.Unlock()

	// Re-create the schedule (this will validate and update)
	return s.Schedule(ctx, schedule)
}

// EnableSchedule enables a schedule
func (s *scheduler) EnableSchedule(ctx context.Context, scheduleID string) error {
	return s.setScheduleEnabled(ctx, scheduleID, true)
}

// DisableSchedule disables a schedule
func (s *scheduler) DisableSchedule(ctx context.Context, scheduleID string) error {
	return s.setScheduleEnabled(ctx, scheduleID, false)
}

// GetSchedule retrieves a schedule by ID
func (s *scheduler) GetSchedule(ctx context.Context, scheduleID string) (*ScheduleInfo, error) {
	s.mu.RLock()
	entry, exists := s.schedules[scheduleID]
	s.mu.RUnlock()

	if !exists {
		return nil, ErrScheduleNotFound
	}

	// Get recent executions
	executions, err := s.storage.GetExecutionHistory(ctx, scheduleID, 10)
	if err != nil {
		s.logger.Error("Failed to get execution history",
			logger.String("schedule_id", scheduleID),
			logger.Error(err),
		)
		executions = []ExecutionHistory{}
	}

	// Calculate next few executions
	nextExecutions := s.calculateNextExecutions(entry.cronSchedule, entry.Schedule, 5)

	scheduleInfo := &ScheduleInfo{
		Schedule:       entry.Schedule,
		NextExecutions: nextExecutions,
		RecentRuns:     executions,
		IsRunning:      entry.isRunning,
	}

	return scheduleInfo, nil
}

// ListSchedules returns all schedules
func (s *scheduler) ListSchedules(ctx context.Context) ([]ScheduleInfo, error) {
	s.mu.RLock()
	entries := make([]*scheduleEntry, 0, len(s.schedules))
	for _, entry := range s.schedules {
		entries = append(entries, entry)
	}
	s.mu.RUnlock()

	schedules := make([]ScheduleInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := s.GetSchedule(ctx, entry.ID)
		if err != nil {
			s.logger.Error("Failed to get schedule info",
				logger.String("schedule_id", entry.ID),
				logger.Error(err),
			)
			continue
		}
		schedules = append(schedules, *info)
	}

	return schedules, nil
}

// GetNextRun returns the next run time for a schedule
func (s *scheduler) GetNextRun(ctx context.Context, scheduleID string) (*time.Time, error) {
	s.mu.RLock()
	entry, exists := s.schedules[scheduleID]
	s.mu.RUnlock()

	if !exists {
		return nil, ErrScheduleNotFound
	}

	return &entry.nextRun, nil
}

// GetExecutionHistory returns execution history for a schedule
func (s *scheduler) GetExecutionHistory(ctx context.Context, scheduleID string, limit int) ([]ExecutionHistory, error) {
	return s.storage.GetExecutionHistory(ctx, scheduleID, limit)
}

// Start starts the scheduler
func (s *scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("scheduler already started")
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.started = true

	// Load existing schedules from storage
	if err := s.loadSchedules(ctx); err != nil {
		s.logger.Error("Failed to load schedules from storage", logger.Error(err))
		// Don't fail startup, continue with empty schedules
	}

	// Start the scheduling loop
	s.ticker = time.NewTicker(s.config.TickInterval)
	go s.schedulingLoop()

	// Start cleanup routine
	if s.config.HistoryRetention > 0 {
		go s.cleanupLoop()
	}

	s.logger.Info("Scheduler started",
		logger.Duration("tick_interval", s.config.TickInterval),
		logger.Int("max_concurrent", s.config.MaxConcurrent),
		logger.Int("loaded_schedules", len(s.schedules)),
	)

	return nil
}

// Stop stops the scheduler
func (s *scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.started = false

	// Stop ticker
	if s.ticker != nil {
		s.ticker.Stop()
	}

	// Cancel all running executions
	for scheduleID, execution := range s.running {
		execution.cancel()
		s.logger.Info("Cancelled running execution",
			logger.String("schedule_id", scheduleID),
			logger.String("job_id", execution.JobID),
		)
	}

	// Cancel context
	if s.cancel != nil {
		s.cancel()
	}

	s.logger.Info("Scheduler stopped")
	return nil
}

// Health checks scheduler health
func (s *scheduler) Health(ctx context.Context) error {
	if !s.isStarted() {
		return fmt.Errorf("scheduler not started")
	}

	// Check if any schedules are severely overdue
	s.mu.RLock()
	overdue := 0
	for _, entry := range s.schedules {
		if entry.Enabled && time.Since(entry.nextRun) > 5*time.Minute {
			overdue++
		}
	}
	s.mu.RUnlock()

	if overdue > 0 {
		return fmt.Errorf("%d schedules are overdue", overdue)
	}

	return nil
}

// Private methods

func (s *scheduler) isStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

func (s *scheduler) validateSchedule(schedule Schedule) error {
	if schedule.Name == "" {
		return fmt.Errorf("schedule name is required")
	}

	if schedule.JobType == "" {
		return fmt.Errorf("job type is required")
	}

	if schedule.CronExpression == "" {
		return fmt.Errorf("cron expression is required")
	}

	if schedule.Queue == "" {
		schedule.Queue = "default"
	}

	// Validate cron expression
	if _, err := s.cronParser.Parse(schedule.CronExpression); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	// Validate timezone if specified
	if schedule.Timezone != "" {
		if _, err := time.LoadLocation(schedule.Timezone); err != nil {
			return fmt.Errorf("invalid timezone: %w", err)
		}
	}

	// Validate time range
	if schedule.StartTime != nil && schedule.EndTime != nil {
		if schedule.EndTime.Before(*schedule.StartTime) {
			return fmt.Errorf("end time must be after start time")
		}
	}

	return nil
}

func (s *scheduler) setScheduleEnabled(ctx context.Context, scheduleID string, enabled bool) error {
	s.mu.Lock()
	entry, exists := s.schedules[scheduleID]
	if !exists {
		s.mu.Unlock()
		return ErrScheduleNotFound
	}

	entry.Enabled = enabled
	entry.UpdatedAt = time.Now()

	// Cancel if currently running and being disabled
	if !enabled {
		if running, isRunning := s.running[scheduleID]; isRunning {
			running.cancel()
			delete(s.running, scheduleID)
		}
	}
	s.mu.Unlock()

	// Update in storage
	if err := s.storage.UpdateSchedule(ctx, entry.Schedule); err != nil {
		return fmt.Errorf("failed to update schedule: %w", err)
	}

	action := "enabled"
	if !enabled {
		action = "disabled"
	}

	s.logger.Info("Schedule "+action,
		logger.String("schedule_id", scheduleID),
		logger.String("name", entry.Name),
	)

	return nil
}

func (s *scheduler) loadSchedules(ctx context.Context) error {
	schedules, err := s.storage.ListSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to list schedules: %w", err)
	}

	loaded := 0
	for _, schedule := range schedules {
		cronSchedule, err := s.cronParser.Parse(schedule.CronExpression)
		if err != nil {
			s.logger.Error("Failed to parse cron expression for schedule",
				logger.String("schedule_id", schedule.ID),
				logger.String("cron", schedule.CronExpression),
				logger.Error(err),
			)
			continue
		}

		// Calculate next run time
		now := time.Now()
		var nextRun time.Time
		if schedule.StartTime != nil && schedule.StartTime.After(now) {
			nextRun = cronSchedule.Next(*schedule.StartTime)
		} else {
			nextRun = cronSchedule.Next(now)
		}

		entry := &scheduleEntry{
			Schedule:     schedule,
			cronSchedule: cronSchedule,
			nextRun:      nextRun,
			runCount:     int64(schedule.RunCount),
			errorCount:   int64(schedule.ErrorCount),
		}

		s.schedules[schedule.ID] = entry
		loaded++
	}

	s.logger.Info("Schedules loaded from storage",
		logger.Int("total", len(schedules)),
		logger.Int("loaded", loaded),
		logger.Int("failed", len(schedules)-loaded),
	)

	return nil
}

func (s *scheduler) schedulingLoop() {
	defer s.ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.ticker.C:
			s.processSchedules()
		}
	}
}

func (s *scheduler) processSchedules() {
	now := time.Now()

	s.mu.RLock()
	readySchedules := make([]*scheduleEntry, 0)

	for _, entry := range s.schedules {
		if s.shouldExecuteSchedule(entry, now) {
			readySchedules = append(readySchedules, entry)
		}
	}
	s.mu.RUnlock()

	// Sort by next run time (earliest first)
	sort.Slice(readySchedules, func(i, j int) bool {
		return readySchedules[i].nextRun.Before(readySchedules[j].nextRun)
	})

	// Execute schedules (respecting concurrency limit)
	executed := 0
	for _, entry := range readySchedules {
		if s.config.MaxConcurrent > 0 && len(s.running) >= s.config.MaxConcurrent {
			s.logger.Warn("Max concurrent executions reached, skipping schedule",
				logger.String("schedule_id", entry.ID),
				logger.String("name", entry.Name),
			)
			break
		}

		if err := s.executeSchedule(entry, now); err != nil {
			s.logger.Error("Failed to execute schedule",
				logger.String("schedule_id", entry.ID),
				logger.String("name", entry.Name),
				logger.Error(err),
			)
		} else {
			executed++
		}
	}

	if executed > 0 {
		s.logger.Debug("Processed schedules",
			logger.Int("ready", len(readySchedules)),
			logger.Int("executed", executed),
			logger.Int("running", len(s.running)),
		)
	}
}

func (s *scheduler) shouldExecuteSchedule(entry *scheduleEntry, now time.Time) bool {
	// Check if enabled
	if !entry.Enabled {
		return false
	}

	// Check if already running
	if entry.isRunning {
		return false
	}

	// Check if it's time to run
	if now.Before(entry.nextRun) {
		return false
	}

	// Check start/end time constraints
	if entry.StartTime != nil && now.Before(*entry.StartTime) {
		return false
	}

	if entry.EndTime != nil && now.After(*entry.EndTime) {
		return false
	}

	// Check max runs constraint
	if entry.MaxRuns != nil && entry.runCount >= int64(*entry.MaxRuns) {
		return false
	}

	return true
}

func (s *scheduler) executeSchedule(entry *scheduleEntry, now time.Time) error {
	// Create job from schedule
	job := NewJob(entry.JobType, entry.JobPayload)
	job.Queue = entry.Queue
	job.ScheduleID = entry.ID

	if entry.Timeout > 0 {
		job.Timeout = entry.Timeout
	}

	if entry.RetryPolicy != nil {
		job.RetryPolicy = entry.RetryPolicy
	}

	// Add schedule metadata
	if job.Metadata == nil {
		job.Metadata = make(map[string]interface{})
	}
	job.Metadata["schedule_id"] = entry.ID
	job.Metadata["schedule_name"] = entry.Name
	job.Metadata["scheduled_at"] = now
	job.Metadata["execution_count"] = entry.runCount + 1

	// Enqueue the job
	if err := s.processor.Enqueue(s.ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue scheduled job: %w", err)
	}

	// Update schedule state
	s.mu.Lock()
	entry.isRunning = true
	entry.runCount++
	entry.lastRun = &now
	entry.LastRun = &now
	entry.RunCount = int(entry.runCount)

	// Calculate next run time
	nextRun := entry.cronSchedule.Next(now)
	entry.nextRun = nextRun
	entry.NextRun = &nextRun

	// Track running execution
	execCtx, cancel := context.WithCancel(s.ctx)
	s.running[entry.ID] = &runningExecution{
		ScheduleID: entry.ID,
		JobID:      job.ID,
		StartedAt:  now,
		cancel:     cancel,
	}
	s.mu.Unlock()

	// Update schedule in storage
	if err := s.storage.UpdateSchedule(s.ctx, entry.Schedule); err != nil {
		s.logger.Error("Failed to update schedule in storage",
			logger.String("schedule_id", entry.ID),
			logger.Error(err),
		)
	}

	// Start monitoring this execution
	go s.monitorExecution(execCtx, entry.ID, job.ID, now)

	s.logger.Info("Schedule executed",
		logger.String("schedule_id", entry.ID),
		logger.String("schedule_name", entry.Name),
		logger.String("job_id", job.ID),
		logger.Time("next_run", nextRun),
		logger.Int64("run_count", entry.runCount),
	)

	return nil
}

func (s *scheduler) monitorExecution(ctx context.Context, scheduleID, jobID string, startedAt time.Time) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check job status
			if jobInfo, err := s.processor.GetJob(ctx, jobID); err == nil {
				switch jobInfo.Status {
				case JobStatusCompleted, JobStatusFailed, JobStatusCancelled:
					s.handleExecutionComplete(scheduleID, jobID, startedAt, jobInfo.Job)
					return
				}
			}
		}
	}
}

func (s *scheduler) handleExecutionComplete(scheduleID, jobID string, startedAt time.Time, job Job) {
	now := time.Now()
	duration := now.Sub(startedAt)

	s.mu.Lock()
	entry, exists := s.schedules[scheduleID]
	if exists {
		entry.isRunning = false
		entry.totalRunTime += duration

		if job.Status == JobStatusFailed {
			entry.errorCount++
			entry.ErrorCount = int(entry.errorCount)
			entry.LastError = job.LastError
		}
	}

	// Remove from running executions
	delete(s.running, scheduleID)
	s.mu.Unlock()

	// Create execution history record
	execution := ExecutionHistory{
		ScheduleID:  scheduleID,
		JobID:       jobID,
		StartedAt:   startedAt,
		CompletedAt: &now,
		Duration:    duration,
		Status:      job.Status,
		Error:       job.LastError,
		Result:      job.Result,
		Metadata: map[string]interface{}{
			"attempts": job.Attempts,
		},
	}

	// Save execution history
	if err := s.storage.SaveExecution(s.ctx, execution); err != nil {
		s.logger.Error("Failed to save execution history",
			logger.String("schedule_id", scheduleID),
			logger.String("job_id", jobID),
			logger.Error(err),
		)
	}

	// Update internal execution history
	s.execMu.Lock()
	s.executions = append(s.executions, execution)
	// Keep only recent executions in memory
	if len(s.executions) > 1000 {
		s.executions = s.executions[len(s.executions)-1000:]
	}
	s.execMu.Unlock()

	s.logger.Info("Schedule execution completed",
		logger.String("schedule_id", scheduleID),
		logger.String("job_id", jobID),
		logger.String("status", string(job.Status)),
		logger.Duration("duration", duration),
	)

	// Update schedule in storage
	if exists {
		if err := s.storage.UpdateSchedule(s.ctx, entry.Schedule); err != nil {
			s.logger.Error("Failed to update schedule after execution",
				logger.String("schedule_id", scheduleID),
				logger.Error(err),
			)
		}
	}
}

func (s *scheduler) calculateNextExecutions(cronSchedule cron.Schedule, schedule Schedule, count int) []time.Time {
	executions := make([]time.Time, 0, count)

	now := time.Now()
	next := now
	if schedule.StartTime != nil && schedule.StartTime.After(now) {
		next = *schedule.StartTime
	}

	for i := 0; i < count; i++ {
		next = cronSchedule.Next(next)

		// Check end time constraint
		if schedule.EndTime != nil && next.After(*schedule.EndTime) {
			break
		}

		executions = append(executions, next)
	}

	return executions
}

func (s *scheduler) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour) // Run daily
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.performCleanup()
		}
	}
}

func (s *scheduler) performCleanup() {
	if s.config.HistoryRetention <= 0 {
		return
	}

	cutoff := time.Now().Add(-s.config.HistoryRetention)

	if err := s.storage.CleanupExecutions(s.ctx, cutoff); err != nil {
		s.logger.Error("Failed to cleanup execution history", logger.Error(err))
	} else {
		s.logger.Info("Cleaned up execution history",
			logger.Time("cutoff", cutoff),
			logger.Duration("retention", s.config.HistoryRetention),
		)
	}

	// Cleanup in-memory executions
	s.execMu.Lock()
	cleaned := 0
	for i, execution := range s.executions {
		if execution.StartedAt.Before(cutoff) {
			cleaned++
		} else {
			s.executions = s.executions[i:]
			break
		}
	}
	if cleaned == len(s.executions) {
		s.executions = s.executions[:0]
	}
	s.execMu.Unlock()

	if cleaned > 0 {
		s.logger.Debug("Cleaned up in-memory executions",
			logger.Int("cleaned", cleaned),
		)
	}
}

// Memory-based schedule storage implementation
type memoryScheduleStorage struct {
	schedules  map[string]Schedule
	executions map[string][]ExecutionHistory
	mu         sync.RWMutex
	logger     logger.Logger
}

// NewMemoryScheduleStorage creates a new in-memory schedule storage
func NewMemoryScheduleStorage(logger logger.Logger) ScheduleStorage {
	return &memoryScheduleStorage{
		schedules:  make(map[string]Schedule),
		executions: make(map[string][]ExecutionHistory),
		logger:     logger.Named("memory-schedule-storage"),
	}
}

func (s *memoryScheduleStorage) SaveSchedule(ctx context.Context, schedule Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.schedules[schedule.ID] = schedule
	return nil
}

func (s *memoryScheduleStorage) GetSchedule(ctx context.Context, scheduleID string) (*Schedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	schedule, exists := s.schedules[scheduleID]
	if !exists {
		return nil, ErrScheduleNotFound
	}

	return &schedule, nil
}

func (s *memoryScheduleStorage) UpdateSchedule(ctx context.Context, schedule Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.schedules[schedule.ID]; !exists {
		return ErrScheduleNotFound
	}

	s.schedules[schedule.ID] = schedule
	return nil
}

func (s *memoryScheduleStorage) DeleteSchedule(ctx context.Context, scheduleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.schedules[scheduleID]; !exists {
		return ErrScheduleNotFound
	}

	delete(s.schedules, scheduleID)
	delete(s.executions, scheduleID)
	return nil
}

func (s *memoryScheduleStorage) ListSchedules(ctx context.Context) ([]Schedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	schedules := make([]Schedule, 0, len(s.schedules))
	for _, schedule := range s.schedules {
		schedules = append(schedules, schedule)
	}

	return schedules, nil
}

func (s *memoryScheduleStorage) SaveExecution(ctx context.Context, execution ExecutionHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	executions := s.executions[execution.ScheduleID]
	executions = append(executions, execution)

	// Keep only recent executions
	if len(executions) > 100 {
		executions = executions[len(executions)-100:]
	}

	s.executions[execution.ScheduleID] = executions
	return nil
}

func (s *memoryScheduleStorage) GetExecutionHistory(ctx context.Context, scheduleID string, limit int) ([]ExecutionHistory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	executions := s.executions[scheduleID]
	if len(executions) == 0 {
		return []ExecutionHistory{}, nil
	}

	// Return most recent executions
	start := 0
	if len(executions) > limit {
		start = len(executions) - limit
	}

	result := make([]ExecutionHistory, len(executions)-start)
	copy(result, executions[start:])

	// Reverse to get most recent first
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

func (s *memoryScheduleStorage) CleanupExecutions(ctx context.Context, olderThan time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for scheduleID, executions := range s.executions {
		cleaned := 0
		for i, execution := range executions {
			if execution.StartedAt.Before(olderThan) {
				cleaned++
			} else {
				s.executions[scheduleID] = executions[i:]
				break
			}
		}

		if cleaned == len(executions) {
			s.executions[scheduleID] = []ExecutionHistory{}
		}
	}

	return nil
}

// CronExpressionValidator validates cron expressions
type CronExpressionValidator struct {
	parser cron.Parser
}

// NewCronExpressionValidator creates a new cron expression validator
func NewCronExpressionValidator() *CronExpressionValidator {
	return &CronExpressionValidator{
		parser: cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
	}
}

// Validate validates a cron expression
func (v *CronExpressionValidator) Validate(expression string) error {
	_, err := v.parser.Parse(expression)
	return err
}

// ParseAndGetNext parses a cron expression and returns the next execution time
func (v *CronExpressionValidator) ParseAndGetNext(expression string, from time.Time) (time.Time, error) {
	schedule, err := v.parser.Parse(expression)
	if err != nil {
		return time.Time{}, err
	}

	return schedule.Next(from), nil
}

// GetNextExecutions returns the next N execution times for a cron expression
func (v *CronExpressionValidator) GetNextExecutions(expression string, from time.Time, count int) ([]time.Time, error) {
	schedule, err := v.parser.Parse(expression)
	if err != nil {
		return nil, err
	}

	executions := make([]time.Time, 0, count)
	next := from

	for i := 0; i < count; i++ {
		next = schedule.Next(next)
		executions = append(executions, next)
	}

	return executions, nil
}

// Common cron expressions
var CommonCronExpressions = map[string]string{
	"every_minute":    "0 * * * * *",
	"every_5_minutes": "0 */5 * * * *",
	"every_hour":      "0 0 * * * *",
	"every_day":       "0 0 0 * * *",
	"every_week":      "0 0 0 * * 0",
	"every_month":     "0 0 0 1 * *",
	"every_year":      "0 0 0 1 1 *",
	"workdays_9am":    "0 0 9 * * 1-5",
	"weekends_10am":   "0 0 10 * * 6,0",
}

// GetCommonCronExpression returns a common cron expression by name
func GetCommonCronExpression(name string) (string, bool) {
	expr, exists := CommonCronExpressions[name]
	return expr, exists
}

// ListCommonCronExpressions returns all available common cron expressions
func ListCommonCronExpressions() map[string]string {
	result := make(map[string]string)
	for name, expr := range CommonCronExpressions {
		result[name] = expr
	}
	return result
}
