package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// memoryStorage implements Storage interface using in-memory storage
type memoryStorage struct {
	jobs      map[string]Job
	queues    map[string][]string // queue name -> job IDs
	mu        sync.RWMutex
	logger    logger.Logger
	stats     StorageStats
	cleanupAt time.Time
}

// NewMemoryStorage creates a new in-memory job storage
func NewMemoryStorage(logger logger.Logger) Storage {
	return &memoryStorage{
		jobs:      make(map[string]Job),
		queues:    make(map[string][]string),
		logger:    logger.Named("memory-storage"),
		stats:     StorageStats{Backend: "memory"},
		cleanupAt: time.Now(),
	}
}

func (s *memoryStorage) SaveJob(ctx context.Context, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[job.ID] = job
	s.updateStatsLocked(job, "save")

	s.logger.Debug("Job saved",
		logger.String("job_id", job.ID),
		logger.String("job_type", job.Type),
		logger.String("status", string(job.Status)),
	)

	return nil
}

func (s *memoryStorage) GetJob(ctx context.Context, jobID string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return nil, ErrJobNotFound
	}

	// Return a copy to prevent external modifications
	jobCopy := job
	return &jobCopy, nil
}

func (s *memoryStorage) UpdateJob(ctx context.Context, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[job.ID]; !exists {
		return ErrJobNotFound
	}

	oldJob := s.jobs[job.ID]
	s.jobs[job.ID] = job
	s.updateStatsLocked(job, "update")

	s.logger.Debug("Job updated",
		logger.String("job_id", job.ID),
		logger.String("old_status", string(oldJob.Status)),
		logger.String("new_status", string(job.Status)),
	)

	return nil
}

func (s *memoryStorage) DeleteJob(ctx context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	delete(s.jobs, jobID)

	// Remove from queue if present
	if queueJobs, queueExists := s.queues[job.Queue]; queueExists {
		for i, id := range queueJobs {
			if id == jobID {
				s.queues[job.Queue] = append(queueJobs[:i], queueJobs[i+1:]...)
				break
			}
		}
	}

	s.updateStatsLocked(job, "delete")

	s.logger.Debug("Job deleted",
		logger.String("job_id", jobID),
		logger.String("job_type", job.Type),
	)

	return nil
}

func (s *memoryStorage) GetJobsByStatus(ctx context.Context, status JobStatus, limit int) ([]Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]Job, 0)
	for _, job := range s.jobs {
		if job.Status == status {
			jobs = append(jobs, job)
		}
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}

	// Sort by creation time (oldest first)
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})

	return jobs, nil
}

func (s *memoryStorage) GetJobsByQueue(ctx context.Context, queueName string, limit int) ([]Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobIDs, exists := s.queues[queueName]
	if !exists {
		return []Job{}, nil
	}

	jobs := make([]Job, 0, len(jobIDs))
	for i, jobID := range jobIDs {
		if limit > 0 && i >= limit {
			break
		}
		if job, exists := s.jobs[jobID]; exists {
			jobs = append(jobs, job)
		}
	}

	return jobs, nil
}

func (s *memoryStorage) GetExpiredJobs(ctx context.Context) ([]Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	expiredJobs := make([]Job, 0)

	for _, job := range s.jobs {
		// Check if job has timed out
		if job.Status == JobStatusRunning && job.StartedAt != nil {
			elapsed := now.Sub(*job.StartedAt)
			if elapsed > job.Timeout {
				expiredJobs = append(expiredJobs, job)
			}
		}
	}

	return expiredJobs, nil
}

func (s *memoryStorage) GetRetryableJobs(ctx context.Context) ([]Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	retryableJobs := make([]Job, 0)

	for _, job := range s.jobs {
		// Check if job is ready for retry
		if job.Status == JobStatusRetrying && job.RunAt != nil && job.RunAt.Before(now) {
			retryableJobs = append(retryableJobs, job)
		}
	}

	return retryableJobs, nil
}

func (s *memoryStorage) EnqueueJob(ctx context.Context, queueName string, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Add job ID to queue
	if s.queues[queueName] == nil {
		s.queues[queueName] = make([]string, 0)
	}

	// Insert job based on priority
	jobIDs := s.queues[queueName]
	inserted := false

	for i, existingJobID := range jobIDs {
		if existingJob, exists := s.jobs[existingJobID]; exists {
			if job.Priority > existingJob.Priority {
				// Insert at this position
				jobIDs = append(jobIDs[:i], append([]string{job.ID}, jobIDs[i:]...)...)
				inserted = true
				break
			}
		}
	}

	if !inserted {
		// Append at the end
		jobIDs = append(jobIDs, job.ID)
	}

	s.queues[queueName] = jobIDs

	s.logger.Debug("Job enqueued",
		logger.String("job_id", job.ID),
		logger.String("queue", queueName),
		logger.Int("priority", job.Priority),
		logger.Int("queue_size", len(jobIDs)),
	)

	return nil
}

func (s *memoryStorage) DequeueJob(ctx context.Context, queueName string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobIDs, exists := s.queues[queueName]
	if !exists || len(jobIDs) == 0 {
		return nil, nil // No jobs in queue
	}

	// Get the first job (highest priority)
	jobID := jobIDs[0]
	s.queues[queueName] = jobIDs[1:]

	job, exists := s.jobs[jobID]
	if !exists {
		s.logger.Warn("Job not found when dequeuing",
			logger.String("job_id", jobID),
			logger.String("queue", queueName),
		)
		return nil, ErrJobNotFound
	}

	s.logger.Debug("Job dequeued",
		logger.String("job_id", jobID),
		logger.String("queue", queueName),
		logger.Int("remaining", len(s.queues[queueName])),
	)

	return &job, nil
}

func (s *memoryStorage) GetQueueSize(ctx context.Context, queueName string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobIDs, exists := s.queues[queueName]
	if !exists {
		return 0, nil
	}

	return int64(len(jobIDs)), nil
}

func (s *memoryStorage) GetStats(ctx context.Context) (*StorageStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Recalculate stats
	stats := StorageStats{
		Backend:     "memory",
		TotalJobs:   int64(len(s.jobs)),
		StorageSize: s.calculateStorageSize(),
		Performance: make(map[string]string),
	}

	// Count jobs by status
	for _, job := range s.jobs {
		switch job.Status {
		case JobStatusPending, JobStatusQueued, JobStatusRunning, JobStatusRetrying:
			stats.ActiveJobs++
		case JobStatusCompleted:
			stats.CompletedJobs++
		case JobStatusFailed, JobStatusCancelled:
			stats.FailedJobs++
		}
	}

	// Add performance metrics
	stats.Performance["avg_lookup_time"] = "< 1ms"
	stats.Performance["total_queues"] = fmt.Sprintf("%d", len(s.queues))

	return &stats, nil
}

func (s *memoryStorage) GetQueueStats(ctx context.Context, queueName string) (*QueueStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobIDs, exists := s.queues[queueName]
	if !exists {
		return &QueueStats{
			Name: queueName,
			Size: 0,
		}, nil
	}

	stats := &QueueStats{
		Name:         queueName,
		Size:         int64(len(jobIDs)),
		LastActivity: time.Now(),
	}

	// Count jobs by status in this queue
	for _, jobID := range jobIDs {
		if job, exists := s.jobs[jobID]; exists {
			switch job.Status {
			case JobStatusPending, JobStatusQueued:
				stats.PendingJobs++
			case JobStatusRunning:
				stats.RunningJobs++
			case JobStatusCompleted:
				stats.CompletedJobs++
			case JobStatusFailed:
				stats.FailedJobs++
			}
		}
	}

	return stats, nil
}

func (s *memoryStorage) CleanupJobs(ctx context.Context, olderThan time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	deletedCount := 0
	for jobID, job := range s.jobs {
		// Delete completed/failed jobs older than the threshold
		if (job.Status == JobStatusCompleted || job.Status == JobStatusFailed) &&
			job.UpdatedAt.Before(olderThan) {

			delete(s.jobs, jobID)

			// Remove from queue if present
			if queueJobs, queueExists := s.queues[job.Queue]; queueExists {
				for i, id := range queueJobs {
					if id == jobID {
						s.queues[job.Queue] = append(queueJobs[:i], queueJobs[i+1:]...)
						break
					}
				}
			}

			deletedCount++
		}
	}

	s.cleanupAt = time.Now()

	s.logger.Info("Jobs cleanup completed",
		logger.Int("deleted", deletedCount),
		logger.Time("older_than", olderThan),
	)

	return nil
}

func (s *memoryStorage) CleanupQueue(ctx context.Context, queueName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobIDs, exists := s.queues[queueName]
	if !exists {
		return nil
	}

	// Remove all jobs from queue and delete completed/failed ones
	deletedCount := 0
	for _, jobID := range jobIDs {
		if job, exists := s.jobs[jobID]; exists {
			if job.Status == JobStatusCompleted || job.Status == JobStatusFailed {
				delete(s.jobs, jobID)
				deletedCount++
			}
		}
	}

	// Clear the queue
	delete(s.queues, queueName)

	s.logger.Info("Queue cleanup completed",
		logger.String("queue", queueName),
		logger.Int("deleted", deletedCount),
	)

	return nil
}

func (s *memoryStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = make(map[string]Job)
	s.queues = make(map[string][]string)

	s.logger.Info("Memory storage closed")
	return nil
}

func (s *memoryStorage) updateStatsLocked(job Job, operation string) {
	// Update stats based on operation
	switch operation {
	case "save":
		// Stats updated in GetStats method
	case "update", "delete":
		// Stats updated in GetStats method
	}
}

func (s *memoryStorage) calculateStorageSize() int64 {
	// Rough estimate of memory usage
	size := int64(0)

	for _, job := range s.jobs {
		// Estimate job size
		jobBytes, _ := json.Marshal(job)
		size += int64(len(jobBytes))
	}

	for _, jobIDs := range s.queues {
		// Estimate queue size
		size += int64(len(jobIDs) * 32) // Rough estimate for string slice
	}

	return size
}

// redisStorage implements Storage interface using Redis (placeholder implementation)
type redisStorage struct {
	// TODO: Add Redis client
	// client redis.Client
	logger logger.Logger
	prefix string
}

// NewRedisStorage creates a new Redis-based job storage
func NewRedisStorage(config StorageConfig, logger logger.Logger) Storage {
	return &redisStorage{
		logger: logger.Named("redis-storage"),
		prefix: config.KeyPrefix,
	}
}

// Placeholder implementations for RedisStorage
func (s *redisStorage) SaveJob(ctx context.Context, job Job) error {
	return fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetJob(ctx context.Context, jobID string) (*Job, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) UpdateJob(ctx context.Context, job Job) error {
	return fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) DeleteJob(ctx context.Context, jobID string) error {
	return fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetJobsByStatus(ctx context.Context, status JobStatus, limit int) ([]Job, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetJobsByQueue(ctx context.Context, queueName string, limit int) ([]Job, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetExpiredJobs(ctx context.Context) ([]Job, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetRetryableJobs(ctx context.Context) ([]Job, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) EnqueueJob(ctx context.Context, queueName string, job Job) error {
	return fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) DequeueJob(ctx context.Context, queueName string) (*Job, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetQueueSize(ctx context.Context, queueName string) (int64, error) {
	return 0, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetStats(ctx context.Context) (*StorageStats, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) GetQueueStats(ctx context.Context, queueName string) (*QueueStats, error) {
	return nil, fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) CleanupJobs(ctx context.Context, olderThan time.Time) error {
	return fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) CleanupQueue(ctx context.Context, queueName string) error {
	return fmt.Errorf("Redis storage not implemented")
}

func (s *redisStorage) Close() error {
	return nil
}

// StorageFactory creates storage instances based on configuration
type StorageFactory struct {
	logger logger.Logger
}

// NewStorageFactory creates a new storage factory
func NewStorageFactory(logger logger.Logger) *StorageFactory {
	return &StorageFactory{
		logger: logger,
	}
}

// CreateStorage creates a storage instance based on the configuration
func (f *StorageFactory) CreateStorage(config StorageConfig) (Storage, error) {
	switch config.Type {
	case "memory":
		return NewMemoryStorage(f.logger), nil
	case "redis":
		return NewRedisStorage(config, f.logger), nil
	case "postgres":
		return NewPostgresStorage(config, f.logger)
	case "mysql":
		return NewMySQLStorage(config, f.logger)
	default:
		f.logger.Warn("Unknown storage type, using memory",
			logger.String("storage_type", config.Type),
		)
		return NewMemoryStorage(f.logger), nil
	}
}

// SQL-based storage implementations (placeholder)

// NewPostgresStorage creates a new PostgreSQL-based job storage
func NewPostgresStorage(config StorageConfig, logger logger.Logger) (Storage, error) {
	// TODO: Implement PostgreSQL storage
	return nil, fmt.Errorf("PostgreSQL storage not implemented")
}

// NewMySQLStorage creates a new MySQL-based job storage
func NewMySQLStorage(config StorageConfig, logger logger.Logger) (Storage, error) {
	// TODO: Implement MySQL storage
	return nil, fmt.Errorf("MySQL storage not implemented")
}

// Event bus implementation for job events
type eventBus struct {
	handlers map[EventType][]EventHandler
	mu       sync.RWMutex
	logger   logger.Logger
	async    chan Event
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewEventBus creates a new event bus for job events
func NewEventBus(logger logger.Logger) EventBus {
	ctx, cancel := context.WithCancel(context.Background())

	bus := &eventBus{
		handlers: make(map[EventType][]EventHandler),
		logger:   logger.Named("event-bus"),
		async:    make(chan Event, 1000), // Buffer for async events
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start async event processor
	bus.wg.Add(1)
	go bus.processAsyncEvents()

	return bus
}

func (e *eventBus) Subscribe(eventType EventType, handler EventHandler) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.handlers[eventType] == nil {
		e.handlers[eventType] = make([]EventHandler, 0)
	}

	e.handlers[eventType] = append(e.handlers[eventType], handler)

	e.logger.Debug("Event handler subscribed",
		logger.String("event_type", string(eventType)),
	)

	return nil
}

func (e *eventBus) Unsubscribe(eventType EventType, handler EventHandler) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	handlers := e.handlers[eventType]
	for i, h := range handlers {
		if h == handler {
			e.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}

	e.logger.Debug("Event handler unsubscribed",
		logger.String("event_type", string(eventType)),
	)

	return nil
}

func (e *eventBus) Publish(ctx context.Context, event Event) error {
	e.mu.RLock()
	handlers := e.handlers[event.Type]
	e.mu.RUnlock()

	if len(handlers) == 0 {
		return nil
	}

	// Execute handlers synchronously
	for _, handler := range handlers {
		if handler.CanHandle(event.Type) {
			if err := handler.HandleEvent(ctx, event); err != nil {
				e.logger.Error("Event handler failed",
					logger.String("event_type", string(event.Type)),
					logger.String("job_id", event.JobID),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (e *eventBus) PublishAsync(ctx context.Context, event Event) error {
	select {
	case e.async <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		e.logger.Warn("Async event queue full, dropping event",
			logger.String("event_type", string(event.Type)),
			logger.String("job_id", event.JobID),
		)
		return fmt.Errorf("event queue full")
	}
}

func (e *eventBus) processAsyncEvents() {
	defer e.wg.Done()

	for {
		select {
		case <-e.ctx.Done():
			return
		case event := <-e.async:
			if err := e.Publish(e.ctx, event); err != nil {
				e.logger.Error("Failed to process async event",
					logger.String("event_type", string(event.Type)),
					logger.Error(err),
				)
			}
		}
	}
}

func (e *eventBus) Close() error {
	e.cancel()

	// Close async channel
	close(e.async)

	// Wait for processor to finish
	e.wg.Wait()

	e.logger.Info("Event bus closed")
	return nil
}

// JobEventHandler is a basic implementation of EventHandler
type JobEventHandler struct {
	logger      logger.Logger
	handleTypes []EventType
	onEvent     func(context.Context, Event) error
}

// NewJobEventHandler creates a new job event handler
func NewJobEventHandler(logger logger.Logger, handleTypes []EventType, onEvent func(context.Context, Event) error) *JobEventHandler {
	return &JobEventHandler{
		logger:      logger,
		handleTypes: handleTypes,
		onEvent:     onEvent,
	}
}

func (h *JobEventHandler) HandleEvent(ctx context.Context, event Event) error {
	h.logger.Debug("Handling job event",
		logger.String("event_type", string(event.Type)),
		logger.String("job_id", event.JobID),
	)

	if h.onEvent != nil {
		return h.onEvent(ctx, event)
	}

	return nil
}

func (h *JobEventHandler) CanHandle(eventType EventType) bool {
	for _, t := range h.handleTypes {
		if t == eventType {
			return true
		}
	}
	return false
}

// MetricsEventHandler collects metrics from job events
type MetricsEventHandler struct {
	logger    logger.Logger
	jobCounts map[EventType]int64
	mu        sync.RWMutex
}

// NewMetricsEventHandler creates a new metrics event handler
func NewMetricsEventHandler(logger logger.Logger) *MetricsEventHandler {
	return &MetricsEventHandler{
		logger:    logger,
		jobCounts: make(map[EventType]int64),
	}
}

func (h *MetricsEventHandler) HandleEvent(ctx context.Context, event Event) error {
	h.mu.Lock()
	h.jobCounts[event.Type]++
	h.mu.Unlock()

	// Log interesting events
	switch event.Type {
	case EventJobFailed:
		h.logger.Warn("Job failed",
			logger.String("job_id", event.JobID),
			logger.String("queue", event.Queue),
			logger.String("error", event.Error),
		)
	case EventJobCompleted:
		if duration, ok := event.Data["duration"]; ok {
			h.logger.Info("Job completed",
				logger.String("job_id", event.JobID),
				logger.String("queue", event.Queue),
				logger.Any("duration", duration),
			)
		}
	}

	return nil
}

func (h *MetricsEventHandler) CanHandle(eventType EventType) bool {
	// Handle all job events
	return true
}

func (h *MetricsEventHandler) GetCounts() map[EventType]int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	counts := make(map[EventType]int64)
	for eventType, count := range h.jobCounts {
		counts[eventType] = count
	}

	return counts
}

func (h *MetricsEventHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.jobCounts = make(map[EventType]int64)
}

// Default configurations

// DefaultConfig returns a default job system configuration
func DefaultConfig() Config {
	return Config{
		Enabled:     true,
		Backend:     "memory",
		Concurrency: 10,
		Queues: map[string]QueueConfig{
			"default": {
				Workers:  5,
				Priority: 0,
				MaxSize:  1000,
				Timeout:  30 * time.Second,
			},
			"critical": {
				Workers:  3,
				Priority: 10,
				MaxSize:  500,
				Timeout:  60 * time.Second,
			},
			"background": {
				Workers:  2,
				Priority: -5,
				MaxSize:  2000,
				Timeout:  300 * time.Second,
			},
		},
		Storage: StorageConfig{
			Type:       "memory",
			CleanupTTL: 24 * time.Hour,
		},
		Scheduler: SchedulerConfig{
			Enabled:          true,
			TickInterval:     10 * time.Second,
			MaxConcurrent:    50,
			LockTimeout:      5 * time.Minute,
			HistoryRetention: 30 * 24 * time.Hour, // 30 days
		},
		RetryPolicy: DefaultRetryPolicy(),
		Monitoring: MonitoringConfig{
			Enabled:        true,
			MetricsEnabled: true,
			StatsInterval:  30 * time.Second,
			HealthCheck:    true,
		},
	}
}

// DefaultStorageConfig returns a default storage configuration
func DefaultStorageConfig() StorageConfig {
	return StorageConfig{
		Type:       "memory",
		Database:   "forge_jobs",
		TableName:  "jobs",
		KeyPrefix:  "forge:jobs:",
		TTL:        24 * time.Hour,
		CleanupTTL: 7 * 24 * time.Hour,
		Options:    make(map[string]string),
	}
}

// DefaultSchedulerConfig returns a default scheduler configuration
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		Enabled:          true,
		TickInterval:     10 * time.Second,
		MaxConcurrent:    50,
		LockTimeout:      5 * time.Minute,
		HistoryRetention: 30 * 24 * time.Hour,
	}
}

// DefaultQueueConfig returns a default queue configuration
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		Workers:     5,
		Priority:    0,
		MaxSize:     1000,
		Timeout:     30 * time.Second,
		RetryPolicy: &[]RetryPolicy{DefaultRetryPolicy()}[0],
		DeadLetter:  true,
	}
}
