package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// processor implements the Processor interface
type processor struct {
	config   Config
	storage  Storage
	queues   map[string]Queue
	handlers map[string]JobHandler
	workers  map[string]*worker
	eventBus EventBus
	logger   logger.Logger

	mu      sync.RWMutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Statistics
	stats   *Stats
	statsAt time.Time
	statsMu sync.RWMutex

	// Configuration
	concurrency     int
	tickInterval    time.Duration
	shutdownTimeout time.Duration
}

// NewProcessor creates a new job processor
func NewProcessor(config Config, storage Storage, logger logger.Logger) (Processor, error) {
	return &processor{
		config:          config,
		storage:         storage,
		queues:          make(map[string]Queue),
		handlers:        make(map[string]JobHandler),
		workers:         make(map[string]*worker),
		logger:          logger.Named("processor"),
		concurrency:     config.Concurrency,
		tickInterval:    1 * time.Second,
		shutdownTimeout: 30 * time.Second,
		stats:           &Stats{},
		statsAt:         time.Now(),
	}, nil
}

// Enqueue adds a job to the queue
func (p *processor) Enqueue(ctx context.Context, job Job) error {
	if !p.isStarted() {
		return ErrProcessorNotStarted
	}

	// Set default values
	if job.ID == "" {
		job.ID = generateJobID()
	}
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.Status == "" {
		job.Status = JobStatusPending
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()

	// Validate job
	if err := p.validateJob(job); err != nil {
		return fmt.Errorf("invalid job: %w", err)
	}

	// Save job to storage
	if err := p.storage.SaveJob(ctx, job); err != nil {
		p.logger.Error("Failed to save job to storage",
			logger.String("job_id", job.ID),
			logger.String("job_type", job.Type),
			logger.Error(err),
		)
		return fmt.Errorf("failed to save job: %w", err)
	}

	// Get or create queue
	queue, err := p.getOrCreateQueue(job.Queue)
	if err != nil {
		return fmt.Errorf("failed to get queue %s: %w", job.Queue, err)
	}

	// Check if job should be delayed
	if job.RunAt != nil && job.RunAt.After(time.Now()) {
		job.Status = JobStatusScheduled
		if err := p.storage.UpdateJob(ctx, job); err != nil {
			p.logger.Error("Failed to update scheduled job",
				logger.String("job_id", job.ID),
				logger.Error(err),
			)
		}
		p.logger.Debug("Job scheduled for later execution",
			logger.String("job_id", job.ID),
			logger.Time("run_at", *job.RunAt),
		)
		return nil
	}

	// Add job to queue
	job.Status = JobStatusQueued
	if err := queue.Push(ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Update job status
	if err := p.storage.UpdateJob(ctx, job); err != nil {
		p.logger.Error("Failed to update job status to queued",
			logger.String("job_id", job.ID),
			logger.Error(err),
		)
	}

	// Publish event
	p.publishEvent(ctx, Event{
		Type:      EventJobEnqueued,
		JobID:     job.ID,
		Queue:     job.Queue,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"job_type": job.Type,
			"priority": job.Priority,
		},
	})

	p.logger.Debug("Job enqueued successfully",
		logger.String("job_id", job.ID),
		logger.String("job_type", job.Type),
		logger.String("queue", job.Queue),
	)

	return nil
}

// EnqueueIn adds a job to be processed after a delay
func (p *processor) EnqueueIn(ctx context.Context, job Job, delay time.Duration) error {
	job.RunAt = &[]time.Time{time.Now().Add(delay)}[0]
	return p.Enqueue(ctx, job)
}

// EnqueueAt adds a job to be processed at a specific time
func (p *processor) EnqueueAt(ctx context.Context, job Job, at time.Time) error {
	job.RunAt = &at
	return p.Enqueue(ctx, job)
}

// EnqueueBatch adds multiple jobs to the queue
func (p *processor) EnqueueBatch(ctx context.Context, jobs []Job) error {
	if !p.isStarted() {
		return ErrProcessorNotStarted
	}

	for i := range jobs {
		if err := p.Enqueue(ctx, jobs[i]); err != nil {
			return fmt.Errorf("failed to enqueue job %d: %w", i, err)
		}
	}

	return nil
}

// Process starts processing jobs from all queues
func (p *processor) Process(ctx context.Context) error {
	if !p.isStarted() {
		return ErrProcessorNotStarted
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.processLoop(ctx)
	}()

	return nil
}

// ProcessQueue starts processing jobs from a specific queue
func (p *processor) ProcessQueue(ctx context.Context, queueName string) error {
	if !p.isStarted() {
		return ErrProcessorNotStarted
	}

	queue, exists := p.getQueue(queueName)
	if !exists {
		return ErrQueueNotFound
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.processQueueLoop(ctx, queueName, queue)
	}()

	return nil
}

// RegisterHandler registers a job handler for a specific job type
func (p *processor) RegisterHandler(jobType string, handler JobHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.handlers[jobType]; exists {
		return fmt.Errorf("handler for job type %s already exists", jobType)
	}

	p.handlers[jobType] = handler
	p.logger.Debug("Job handler registered",
		logger.String("job_type", jobType),
	)

	return nil
}

// UnregisterHandler removes a job handler
func (p *processor) UnregisterHandler(jobType string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.handlers[jobType]; !exists {
		return ErrHandlerNotFound
	}

	delete(p.handlers, jobType)
	p.logger.Debug("Job handler unregistered",
		logger.String("job_type", jobType),
	)

	return nil
}

// CreateQueue creates a new queue with the given configuration
func (p *processor) CreateQueue(name string, config QueueConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.queues[name]; exists {
		return fmt.Errorf("queue %s already exists", name)
	}

	queue := NewMemoryQueue(name, config, p.logger)
	p.queues[name] = queue

	p.logger.Info("Queue created",
		logger.String("queue", name),
		logger.Int("workers", config.Workers),
	)

	return nil
}

// DeleteQueue removes a queue
func (p *processor) DeleteQueue(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	queue, exists := p.queues[name]
	if !exists {
		return ErrQueueNotFound
	}

	// Stop workers for this queue
	for workerID, worker := range p.workers {
		if worker.queue == name {
			worker.Stop(context.Background())
			delete(p.workers, workerID)
		}
	}

	// Close and remove queue
	queue.Close()
	delete(p.queues, name)

	p.logger.Info("Queue deleted", logger.String("queue", name))
	return nil
}

// PauseQueue pauses job processing for a queue
func (p *processor) PauseQueue(name string) error {
	queue, exists := p.getQueue(name)
	if !exists {
		return ErrQueueNotFound
	}

	if err := queue.Pause(context.Background()); err != nil {
		return err
	}

	p.publishEvent(context.Background(), Event{
		Type:      EventQueuePaused,
		Queue:     name,
		Timestamp: time.Now(),
	})

	p.logger.Info("Queue paused", logger.String("queue", name))
	return nil
}

// ResumeQueue resumes job processing for a queue
func (p *processor) ResumeQueue(name string) error {
	queue, exists := p.getQueue(name)
	if !exists {
		return ErrQueueNotFound
	}

	if err := queue.Resume(context.Background()); err != nil {
		return err
	}

	p.publishEvent(context.Background(), Event{
		Type:      EventQueueResumed,
		Queue:     name,
		Timestamp: time.Now(),
	})

	p.logger.Info("Queue resumed", logger.String("queue", name))
	return nil
}

// PurgeQueue removes all jobs from a queue
func (p *processor) PurgeQueue(name string) error {
	queue, exists := p.getQueue(name)
	if !exists {
		return ErrQueueNotFound
	}

	if err := queue.Clear(context.Background()); err != nil {
		return err
	}

	p.logger.Info("Queue purged", logger.String("queue", name))
	return nil
}

// GetJob retrieves job information
func (p *processor) GetJob(ctx context.Context, jobID string) (*JobInfo, error) {
	job, err := p.storage.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	jobInfo := &JobInfo{
		Job: *job,
	}

	// Add additional information if available
	// TODO: Add execution history, queue position, etc.

	return jobInfo, nil
}

// CancelJob cancels a job
func (p *processor) CancelJob(ctx context.Context, jobID string) error {
	job, err := p.storage.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	if job.Status == JobStatusRunning {
		// TODO: Signal running workers to cancel the job
	}

	job.Status = JobStatusCancelled
	job.UpdatedAt = time.Now()

	if err := p.storage.UpdateJob(ctx, *job); err != nil {
		return err
	}

	p.publishEvent(ctx, Event{
		Type:      EventJobCancelled,
		JobID:     jobID,
		Queue:     job.Queue,
		Timestamp: time.Now(),
	})

	p.logger.Info("Job cancelled",
		logger.String("job_id", jobID),
		logger.String("job_type", job.Type),
	)

	return nil
}

// RetryJob retries a failed job
func (p *processor) RetryJob(ctx context.Context, jobID string) error {
	job, err := p.storage.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	if job.Status != JobStatusFailed {
		return fmt.Errorf("job %s is not in failed status", jobID)
	}

	// Reset job for retry
	job.Status = JobStatusPending
	job.Attempts = 0
	job.LastError = ""
	job.UpdatedAt = time.Now()
	job.StartedAt = nil
	job.CompletedAt = nil
	job.FailedAt = nil

	if err := p.storage.UpdateJob(ctx, *job); err != nil {
		return err
	}

	// Re-enqueue the job
	return p.Enqueue(ctx, *job)
}

// DeleteJob removes a job from storage
func (p *processor) DeleteJob(ctx context.Context, jobID string) error {
	job, err := p.storage.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	if job.Status == JobStatusRunning {
		return fmt.Errorf("cannot delete running job %s", jobID)
	}

	if err := p.storage.DeleteJob(ctx, jobID); err != nil {
		return err
	}

	p.logger.Debug("Job deleted",
		logger.String("job_id", jobID),
		logger.String("job_type", job.Type),
	)

	return nil
}

// GetStats returns processor statistics
func (p *processor) GetStats(ctx context.Context) (*Stats, error) {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()

	// Update stats
	stats := &Stats{
		TotalWorkers:    len(p.workers),
		ActiveWorkers:   p.countActiveWorkers(),
		Queues:          make(map[string]*QueueStats),
		ProcessingRate:  p.calculateProcessingRate(),
		AverageWaitTime: p.calculateAverageWaitTime(),
		Uptime:          time.Since(p.statsAt),
		LastUpdated:     time.Now(),
	}

	// Get storage stats
	if storageStats, err := p.storage.GetStats(ctx); err == nil {
		stats.TotalJobs = storageStats.TotalJobs
		stats.CompletedJobs = storageStats.CompletedJobs
		stats.FailedJobs = storageStats.FailedJobs
	}

	// Get queue stats
	for name, queue := range p.queues {
		if queueStats, err := queue.Stats(ctx); err == nil {
			stats.Queues[name] = queueStats
			stats.QueuedJobs += queueStats.PendingJobs
			stats.RunningJobs += queueStats.RunningJobs
		}
	}

	return stats, nil
}

// GetQueueStats returns statistics for a specific queue
func (p *processor) GetQueueStats(ctx context.Context, queueName string) (*QueueStats, error) {
	queue, exists := p.getQueue(queueName)
	if !exists {
		return nil, ErrQueueNotFound
	}

	return queue.Stats(ctx)
}

// Start starts the job processor
func (p *processor) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		p.logger.Info("Job processor started",
			logger.Int("workers", len(p.workers)),             // Fixed: show actual count
			logger.Strings("queues", getQueueNames(p.queues)), // Fixed: show queue names
			logger.Int("concurrency", p.concurrency),          // Fixed: show actual value
		)
		return nil
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true
	p.statsAt = time.Now()

	// Create default queue if it doesn't exist
	if _, exists := p.queues["default"]; !exists {
		defaultConfig := QueueConfig{
			Workers:  p.concurrency,
			Priority: 0,
			Timeout:  30 * time.Second,
		}
		p.queues["default"] = NewMemoryQueue("default", defaultConfig, p.logger)
	}

	// Create queues from configuration
	for name, config := range p.config.Queues {
		if _, exists := p.queues[name]; !exists {
			p.queues[name] = NewMemoryQueue(name, config, p.logger)
		}
	}

	// Start workers for each queue
	for queueName, queue := range p.queues {
		config := p.config.Queues[queueName]
		workerCount := config.Workers
		if workerCount == 0 {
			workerCount = 1
		}

		for i := 0; i < workerCount; i++ {
			workerID := fmt.Sprintf("%s-worker-%d", queueName, i)
			wkr := newWorker(workerID, queueName, queue, p.storage, p.logger)
			wkr.handlers = p.handlers
			wkr.eventBus = p.eventBus

			p.workers[workerID] = wkr

			p.wg.Add(1)
			go func(w *worker) {
				defer p.wg.Done()
				w.Start(p.ctx)
			}(wkr)
		}
	}

	// Start scheduled job processor
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.processScheduledJobs(p.ctx)
	}()

	// Start statistics updater
	if p.config.Monitoring.Enabled {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.updateStats(p.ctx)
		}()
	}

	p.logger.Info("Job processor started",
		logger.Int("workers", len(p.workers)),
		logger.Int("queues", len(p.queues)),
		logger.Int("concurrency", p.concurrency),
	)

	return nil
}

// Stop stops the job processor gracefully
func (p *processor) Stop(ctx context.Context) error {
	return p.shutdown(ctx, false)
}

// Shutdown shuts down the processor immediately
func (p *processor) Shutdown(ctx context.Context) error {
	return p.shutdown(ctx, true)
}

// Health checks the health of the processor
func (p *processor) Health(ctx context.Context) error {
	if !p.isStarted() {
		return fmt.Errorf("processor not started")
	}

	// Check storage health
	if _, err := p.storage.GetStats(ctx); err != nil {
		return fmt.Errorf("storage unhealthy: %w", err)
	}

	// Check queue health
	for name, queue := range p.queues {
		if _, err := queue.Stats(ctx); err != nil {
			return fmt.Errorf("queue %s unhealthy: %w", name, err)
		}
	}

	return nil
}

// Private methods

func (p *processor) isStarted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}

func (p *processor) getQueue(name string) (Queue, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	queue, exists := p.queues[name]
	return queue, exists
}

func (p *processor) getOrCreateQueue(name string) (Queue, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if queue, exists := p.queues[name]; exists {
		return queue, nil
	}

	// Create queue with default configuration
	config := QueueConfig{
		Workers:  1,
		Priority: 0,
		Timeout:  30 * time.Second,
	}

	queue := NewMemoryQueue(name, config, p.logger)
	p.queues[name] = queue

	return queue, nil
}

func (p *processor) validateJob(job Job) error {
	if job.Type == "" {
		return fmt.Errorf("job type is required")
	}

	if job.Queue == "" {
		return fmt.Errorf("job queue is required")
	}

	// Check if handler exists
	p.mu.RLock()
	_, exists := p.handlers[job.Type]
	p.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no handler registered for job type: %s", job.Type)
	}

	return nil
}

func (p *processor) processLoop(ctx context.Context) {
	ticker := time.NewTicker(p.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Process each queue
			for queueName, queue := range p.queues {
				select {
				case <-ctx.Done():
					return
				default:
					p.processQueueTick(ctx, queueName, queue)
				}
			}
		}
	}
}

func (p *processor) processQueueLoop(ctx context.Context, queueName string, queue Queue) {
	ticker := time.NewTicker(p.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processQueueTick(ctx, queueName, queue)
		}
	}
}

func (p *processor) processQueueTick(ctx context.Context, queueName string, queue Queue) {
	// Check if queue is paused
	if queue.IsPaused(ctx) {
		return
	}

	// Check if we have available workers
	availableWorkers := p.getAvailableWorkers(queueName)
	if len(availableWorkers) == 0 {
		return
	}

	// Process jobs up to the number of available workers
	for i := 0; i < len(availableWorkers); i++ {
		job, err := queue.Pop(ctx)
		if err != nil || job == nil {
			break // No more jobs in queue
		}

		worker := availableWorkers[i]
		go worker.ProcessJob(ctx, *job)
	}
}

func (p *processor) processScheduledJobs(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processScheduledJobsTick(ctx)
		}
	}
}

func (p *processor) processScheduledJobsTick(ctx context.Context) {
	// Get jobs that are scheduled and ready to run
	jobs, err := p.storage.GetJobsByStatus(ctx, JobStatusScheduled, 100)
	if err != nil {
		p.logger.Error("Failed to get scheduled jobs", logger.Error(err))
		return
	}

	now := time.Now()
	for _, job := range jobs {
		if job.RunAt != nil && job.RunAt.Before(now) {
			// Job is ready to run
			job.Status = JobStatusPending
			job.UpdatedAt = now

			if err := p.storage.UpdateJob(ctx, job); err != nil {
				p.logger.Error("Failed to update scheduled job",
					logger.String("job_id", job.ID),
					logger.Error(err),
				)
				continue
			}

			// Enqueue the job
			if err := p.Enqueue(ctx, job); err != nil {
				p.logger.Error("Failed to enqueue scheduled job",
					logger.String("job_id", job.ID),
					logger.Error(err),
				)
			}
		}
	}
}

func (p *processor) getAvailableWorkers(queueName string) []*worker {
	var available []*worker

	for _, worker := range p.workers {
		if worker.queue == queueName && worker.IsIdle() {
			available = append(available, worker)
		}
	}

	return available
}

func (p *processor) countActiveWorkers() int {
	count := 0
	for _, worker := range p.workers {
		if worker.IsProcessing() {
			count++
		}
	}
	return count
}

func (p *processor) calculateProcessingRate() float64 {
	// TODO: Implement processing rate calculation
	return 0.0
}

func (p *processor) calculateAverageWaitTime() time.Duration {
	// TODO: Implement average wait time calculation
	return 0
}

func (p *processor) updateStats(ctx context.Context) {
	ticker := time.NewTicker(p.config.Monitoring.StatsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Update internal statistics
			// This is where you would collect and aggregate stats
		}
	}
}

func (p *processor) publishEvent(ctx context.Context, event Event) {
	if p.eventBus != nil {
		if err := p.eventBus.PublishAsync(ctx, event); err != nil {
			p.logger.Error("Failed to publish event",
				logger.String("event_type", string(event.Type)),
				logger.Error(err),
			)
		}
	}
}

func (p *processor) shutdown(ctx context.Context, force bool) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}

	p.started = false
	p.mu.Unlock()

	p.logger.Info("Stopping job processor", logger.Bool("force", force))

	// Cancel context to stop all workers
	if p.cancel != nil {
		p.cancel()
	}

	// Wait for workers to finish or timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	timeout := p.shutdownTimeout
	if force {
		timeout = 5 * time.Second
	}

	select {
	case <-done:
		p.logger.Info("All workers stopped gracefully")
	case <-time.After(timeout):
		if force {
			p.logger.Warn("Force shutdown timeout reached")
		} else {
			p.logger.Warn("Graceful shutdown timeout reached")
		}
	case <-ctx.Done():
		p.logger.Warn("Shutdown cancelled by context")
	}

	// Close all queues
	for name, queue := range p.queues {
		if err := queue.Close(); err != nil {
			p.logger.Error("Failed to close queue",
				logger.String("queue", name),
				logger.Error(err),
			)
		}
	}

	// Close storage
	if err := p.storage.Close(); err != nil {
		p.logger.Error("Failed to close storage", logger.Error(err))
	}

	p.logger.Info("Job processor stopped")
	return nil
}

// Helper function to get queue names
func getQueueNames(queues map[string]Queue) []string {
	names := make([]string, 0, len(queues))
	for name := range queues {
		names = append(names, name)
	}
	return names
}
