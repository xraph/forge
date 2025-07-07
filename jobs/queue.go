package jobs

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// memoryQueue implements Queue interface using in-memory storage
type memoryQueue struct {
	name     string
	config   QueueConfig
	logger   logger.Logger
	mu       sync.RWMutex
	jobs     *PriorityQueue
	deadJobs []Job
	paused   bool
	stats    QueueStats
	closed   bool
}

// NewMemoryQueue creates a new in-memory queue
func NewMemoryQueue(name string, config QueueConfig, l logger.Logger) Queue {
	return &memoryQueue{
		name:     name,
		config:   config,
		logger:   l.Named("queue").With(logger.String("queue", name)),
		jobs:     NewPriorityQueue(),
		deadJobs: make([]Job, 0),
		stats: QueueStats{
			Name:         name,
			LastActivity: time.Now(),
		},
	}
}

func (q *memoryQueue) Name() string {
	return q.name
}

func (q *memoryQueue) Size(ctx context.Context) (int64, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return 0, fmt.Errorf("queue is closed")
	}

	return int64(q.jobs.Len()), nil
}

func (q *memoryQueue) Push(ctx context.Context, job Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	if q.config.MaxSize > 0 && int64(q.jobs.Len()) >= q.config.MaxSize {
		return ErrQueueFull
	}

	// Create priority job wrapper
	pJob := &PriorityJob{
		Job:        job,
		Priority:   job.Priority,
		EnqueuedAt: time.Now(),
	}

	heap.Push(q.jobs, pJob)
	q.stats.PendingJobs++
	q.stats.LastActivity = time.Now()

	q.logger.Debug("Job pushed to queue",
		logger.String("job_id", job.ID),
		logger.String("job_type", job.Type),
		logger.Int("priority", job.Priority),
		logger.Int64("queue_size", int64(q.jobs.Len())),
	)

	return nil
}

func (q *memoryQueue) Pop(ctx context.Context) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	if q.paused {
		return nil, ErrQueuePaused
	}

	if q.jobs.Len() == 0 {
		return nil, nil // No jobs available
	}

	pJob := heap.Pop(q.jobs).(*PriorityJob)
	q.stats.PendingJobs--
	q.stats.LastActivity = time.Now()

	q.logger.Debug("Job popped from queue",
		logger.String("job_id", pJob.Job.ID),
		logger.String("job_type", pJob.Job.Type),
		logger.Duration("wait_time", time.Since(pJob.EnqueuedAt)),
		logger.Int64("queue_size", int64(q.jobs.Len())),
	)

	return &pJob.Job, nil
}

func (q *memoryQueue) Peek(ctx context.Context) (*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	if q.jobs.Len() == 0 {
		return nil, nil // No jobs available
	}

	pJob := (*q.jobs)[0]
	return &pJob.Job, nil
}

func (q *memoryQueue) PushBatch(ctx context.Context, jobs []Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	if q.config.MaxSize > 0 && int64(q.jobs.Len()+len(jobs)) > q.config.MaxSize {
		return ErrQueueFull
	}

	for _, job := range jobs {
		pJob := &PriorityJob{
			Job:        job,
			Priority:   job.Priority,
			EnqueuedAt: time.Now(),
		}
		heap.Push(q.jobs, pJob)
		q.stats.PendingJobs++
	}

	q.stats.LastActivity = time.Now()

	q.logger.Debug("Batch pushed to queue",
		logger.Int("job_count", len(jobs)),
		logger.Int64("queue_size", int64(q.jobs.Len())),
	)

	return nil
}

func (q *memoryQueue) PopBatch(ctx context.Context, count int) ([]Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	if q.paused {
		return nil, ErrQueuePaused
	}

	available := q.jobs.Len()
	if count > available {
		count = available
	}

	jobs := make([]Job, 0, count)
	for i := 0; i < count; i++ {
		if q.jobs.Len() == 0 {
			break
		}

		pJob := heap.Pop(q.jobs).(*PriorityJob)
		jobs = append(jobs, pJob.Job)
		q.stats.PendingJobs--
	}

	q.stats.LastActivity = time.Now()

	q.logger.Debug("Batch popped from queue",
		logger.Int("job_count", len(jobs)),
		logger.Int64("queue_size", int64(q.jobs.Len())),
	)

	return jobs, nil
}

func (q *memoryQueue) Pause(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	q.paused = true
	q.stats.IsPaused = true

	q.logger.Info("Queue paused")
	return nil
}

func (q *memoryQueue) Resume(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	q.paused = false
	q.stats.IsPaused = false

	q.logger.Info("Queue resumed")
	return nil
}

func (q *memoryQueue) Clear(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	clearedCount := q.jobs.Len()
	q.jobs = NewPriorityQueue()
	q.stats.PendingJobs = 0
	q.stats.LastActivity = time.Now()

	q.logger.Info("Queue cleared", logger.Int("jobs_removed", clearedCount))
	return nil
}

func (q *memoryQueue) IsPaused(ctx context.Context) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.paused
}

func (q *memoryQueue) GetDeadJobs(ctx context.Context) ([]Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	// Return a copy of dead jobs
	deadJobs := make([]Job, len(q.deadJobs))
	copy(deadJobs, q.deadJobs)

	return deadJobs, nil
}

func (q *memoryQueue) RequeueDeadJob(ctx context.Context, jobID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	// Find and remove job from dead letter queue
	for i, job := range q.deadJobs {
		if job.ID == jobID {
			// Remove from dead jobs
			q.deadJobs = append(q.deadJobs[:i], q.deadJobs[i+1:]...)
			q.stats.DeadLetterJobs--

			// Reset job status and add back to main queue
			job.Status = JobStatusQueued
			job.Attempts = 0
			job.LastError = ""
			job.UpdatedAt = time.Now()

			pJob := &PriorityJob{
				Job:        job,
				Priority:   job.Priority,
				EnqueuedAt: time.Now(),
			}
			heap.Push(q.jobs, pJob)
			q.stats.PendingJobs++

			q.logger.Info("Dead job requeued",
				logger.String("job_id", jobID),
				logger.String("job_type", job.Type),
			)

			return nil
		}
	}

	return ErrJobNotFound
}

func (q *memoryQueue) PurgeDeadJobs(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return fmt.Errorf("queue is closed")
	}

	purgedCount := len(q.deadJobs)
	q.deadJobs = make([]Job, 0)
	q.stats.DeadLetterJobs = 0

	q.logger.Info("Dead jobs purged", logger.Int("jobs_removed", purgedCount))
	return nil
}

func (q *memoryQueue) Stats(ctx context.Context) (*QueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}

	// Create a copy of stats
	stats := q.stats
	stats.Size = int64(q.jobs.Len())

	return &stats, nil
}

func (q *memoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true
	q.jobs = NewPriorityQueue()
	q.deadJobs = nil

	q.logger.Info("Queue closed")
	return nil
}

// AddToDeadLetter adds a job to the dead letter queue
func (q *memoryQueue) AddToDeadLetter(job Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}

	job.Status = JobStatusDeadLetter
	job.UpdatedAt = time.Now()
	q.deadJobs = append(q.deadJobs, job)
	q.stats.DeadLetterJobs++

	q.logger.Warn("Job moved to dead letter queue",
		logger.String("job_id", job.ID),
		logger.String("job_type", job.Type),
		logger.String("error", job.LastError),
	)
}

// UpdateStats updates queue statistics
func (q *memoryQueue) UpdateStats(stat string, value int64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	switch stat {
	case "completed":
		q.stats.CompletedJobs += value
	case "failed":
		q.stats.FailedJobs += value
	case "running":
		q.stats.RunningJobs += value
	}

	q.stats.LastActivity = time.Now()
}

// PriorityJob wraps a job with priority information for the priority queue
type PriorityJob struct {
	Job        Job
	Priority   int
	EnqueuedAt time.Time
	Index      int // Index in the heap
}

// PriorityQueue implements a priority queue for jobs
type PriorityQueue []*PriorityJob

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue() *PriorityQueue {
	pq := make(PriorityQueue, 0)
	heap.Init(&pq)
	return &pq
}

func (pq PriorityQueue) Len() int {
	return len(pq)
}

func (pq PriorityQueue) Less(i, j int) bool {
	// Higher priority first, then FIFO for same priority
	if pq[i].Priority == pq[j].Priority {
		return pq[i].EnqueuedAt.Before(pq[j].EnqueuedAt)
	}
	return pq[i].Priority > pq[j].Priority
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*PriorityJob)
	item.Index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.Index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

// RedisQueue implements Queue interface using Redis
type redisQueue struct {
	name   string
	config QueueConfig
	logger logger.Logger
	// TODO: Add Redis client
	// client   redis.Client
}

// NewRedisQueue creates a new Redis-based queue
func NewRedisQueue(name string, config QueueConfig, l logger.Logger) Queue {
	// TODO: Implement Redis queue
	return &redisQueue{
		name:   name,
		config: config,
		logger: l.Named("redis-queue").With(logger.String("queue", name)),
	}
}

// Placeholder implementations for RedisQueue (to be implemented)
func (q *redisQueue) Name() string { return q.name }
func (q *redisQueue) Size(ctx context.Context) (int64, error) {
	return 0, fmt.Errorf("not implemented")
}
func (q *redisQueue) Push(ctx context.Context, job Job) error { return fmt.Errorf("not implemented") }
func (q *redisQueue) Pop(ctx context.Context) (*Job, error) {
	return nil, fmt.Errorf("not implemented")
}
func (q *redisQueue) Peek(ctx context.Context) (*Job, error) {
	return nil, fmt.Errorf("not implemented")
}
func (q *redisQueue) PushBatch(ctx context.Context, jobs []Job) error {
	return fmt.Errorf("not implemented")
}
func (q *redisQueue) PopBatch(ctx context.Context, count int) ([]Job, error) {
	return nil, fmt.Errorf("not implemented")
}
func (q *redisQueue) Pause(ctx context.Context) error   { return fmt.Errorf("not implemented") }
func (q *redisQueue) Resume(ctx context.Context) error  { return fmt.Errorf("not implemented") }
func (q *redisQueue) Clear(ctx context.Context) error   { return fmt.Errorf("not implemented") }
func (q *redisQueue) IsPaused(ctx context.Context) bool { return false }
func (q *redisQueue) GetDeadJobs(ctx context.Context) ([]Job, error) {
	return nil, fmt.Errorf("not implemented")
}
func (q *redisQueue) RequeueDeadJob(ctx context.Context, jobID string) error {
	return fmt.Errorf("not implemented")
}
func (q *redisQueue) PurgeDeadJobs(ctx context.Context) error { return fmt.Errorf("not implemented") }
func (q *redisQueue) Stats(ctx context.Context) (*QueueStats, error) {
	return nil, fmt.Errorf("not implemented")
}
func (q *redisQueue) Close() error { return nil }

// QueueFactory creates queues based on configuration
type QueueFactory struct {
	logger logger.Logger
}

// NewQueueFactory creates a new queue factory
func NewQueueFactory(logger logger.Logger) *QueueFactory {
	return &QueueFactory{
		logger: logger,
	}
}

// CreateQueue creates a queue based on the storage type
func (f *QueueFactory) CreateQueue(name string, config QueueConfig, storageType string) Queue {
	switch storageType {
	case "memory":
		return NewMemoryQueue(name, config, f.logger)
	case "redis":
		return NewRedisQueue(name, config, f.logger)
	default:
		f.logger.Warn("Unknown queue storage type, using memory",
			logger.String("storage_type", storageType),
			logger.String("queue", name),
		)
		return NewMemoryQueue(name, config, f.logger)
	}
}

// QueueManager manages multiple queues
type QueueManager struct {
	queues  map[string]Queue
	factory *QueueFactory
	logger  logger.Logger
	mu      sync.RWMutex
}

// NewQueueManager creates a new queue manager
func NewQueueManager(logger logger.Logger) *QueueManager {
	return &QueueManager{
		queues:  make(map[string]Queue),
		factory: NewQueueFactory(logger),
		logger:  logger.Named("queue-manager"),
	}
}

// CreateQueue creates a new queue
func (m *QueueManager) CreateQueue(name string, config QueueConfig, storageType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.queues[name]; exists {
		return fmt.Errorf("queue %s already exists", name)
	}

	queue := m.factory.CreateQueue(name, config, storageType)
	m.queues[name] = queue

	m.logger.Info("Queue created",
		logger.String("name", name),
		logger.String("storage_type", storageType),
		logger.Int("workers", config.Workers),
	)

	return nil
}

// GetQueue retrieves a queue by name
func (m *QueueManager) GetQueue(name string) (Queue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	queue, exists := m.queues[name]
	if !exists {
		return nil, ErrQueueNotFound
	}

	return queue, nil
}

// DeleteQueue removes a queue
func (m *QueueManager) DeleteQueue(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue, exists := m.queues[name]
	if !exists {
		return ErrQueueNotFound
	}

	if err := queue.Close(); err != nil {
		return fmt.Errorf("failed to close queue: %w", err)
	}

	delete(m.queues, name)

	m.logger.Info("Queue deleted", logger.String("name", name))
	return nil
}

// ListQueues returns all queue names
func (m *QueueManager) ListQueues() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.queues))
	for name := range m.queues {
		names = append(names, name)
	}

	return names
}

// GetAllStats returns statistics for all queues
func (m *QueueManager) GetAllStats(ctx context.Context) (map[string]*QueueStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]*QueueStats)
	for name, queue := range m.queues {
		queueStats, err := queue.Stats(ctx)
		if err != nil {
			m.logger.Error("Failed to get queue stats",
				logger.String("queue", name),
				logger.Error(err),
			)
			continue
		}
		stats[name] = queueStats
	}

	return stats, nil
}

// CloseAll closes all queues
func (m *QueueManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errors []error
	for name, queue := range m.queues {
		if err := queue.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close queue %s: %w", name, err))
		}
	}

	m.queues = make(map[string]Queue)

	if len(errors) > 0 {
		return fmt.Errorf("failed to close some queues: %v", errors)
	}

	m.logger.Info("All queues closed")
	return nil
}

// Worker represents a job worker
type worker struct {
	id       string
	queue    string
	queueRef Queue
	storage  Storage
	handlers map[string]JobHandler
	eventBus EventBus
	logger   logger.Logger

	mu         sync.RWMutex
	status     WorkerStatus
	currentJob *Job
	stats      WorkerStats
	started    bool
	ctx        context.Context
	cancel     context.CancelFunc
}

// newWorker creates a new worker
func newWorker(id, queueName string, queue Queue, storage Storage, l logger.Logger) *worker {
	return &worker{
		id:       id,
		queue:    queueName,
		queueRef: queue,
		storage:  storage,
		handlers: make(map[string]JobHandler),
		logger:   l.Named("worker").With(logger.String("worker_id", id)),
		status:   WorkerStatusIdle,
		stats: WorkerStats{
			ID:        id,
			Queue:     queueName,
			Status:    WorkerStatusIdle,
			StartedAt: time.Now(),
		},
	}
}

func (w *worker) ID() string {
	return w.id
}

func (w *worker) Queue() string {
	return w.queue
}

func (w *worker) Status() WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return fmt.Errorf("worker already started")
	}

	w.ctx, w.cancel = context.WithCancel(ctx)
	w.started = true
	w.status = WorkerStatusIdle
	w.stats.StartedAt = time.Now()

	// Fixed logging with proper worker ID
	w.logger.Info("Worker started",
		logger.String("worker_id", w.id),          // Fixed: proper field name
		logger.String("queue", w.queue),           // Added: queue name
		logger.String("status", string(w.status)), // Added: status
	)

	// Start processing loop
	go w.processLoop()

	return nil
}

func (w *worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return nil
	}

	w.started = false
	w.status = WorkerStatusStopped

	if w.cancel != nil {
		w.cancel()
	}

	w.logger.Info("Worker stopped")
	return nil
}

func (w *worker) Pause() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.status == WorkerStatusProcessing {
		return fmt.Errorf("cannot pause worker while processing job")
	}

	w.status = WorkerStatusPaused
	w.logger.Info("Worker paused")
	return nil
}

func (w *worker) Resume() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.status != WorkerStatusPaused {
		return fmt.Errorf("worker is not paused")
	}

	w.status = WorkerStatusIdle
	w.logger.Info("Worker resumed")
	return nil
}

func (w *worker) ProcessNext(ctx context.Context) error {
	if !w.IsIdle() {
		return ErrWorkerBusy
	}

	job, err := w.queueRef.Pop(ctx)
	if err != nil {
		return err
	}

	if job == nil {
		return nil // No jobs available
	}

	return w.ProcessJob(ctx, *job)
}

func (w *worker) ProcessJob(ctx context.Context, job Job) error {
	w.mu.Lock()
	w.status = WorkerStatusProcessing
	w.currentJob = &job
	w.stats.LastActivity = time.Now()
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.status = WorkerStatusIdle
		w.currentJob = nil
		w.mu.Unlock()
	}()

	w.logger.Debug("Processing job",
		logger.String("job_id", job.ID),
		logger.String("job_type", job.Type),
	)

	startTime := time.Now()

	// Update job status to running
	job.Status = JobStatusRunning
	job.StartedAt = &startTime
	job.Attempts++
	job.ProcessedBy = w.id
	job.UpdatedAt = time.Now()

	if err := w.storage.UpdateJob(ctx, job); err != nil {
		w.logger.Error("Failed to update job status to running",
			logger.String("job_id", job.ID),
			logger.Error(err),
		)
	}

	// Publish job started event
	w.publishEvent(ctx, Event{
		Type:      EventJobStarted,
		JobID:     job.ID,
		Queue:     job.Queue,
		WorkerID:  w.id,
		Timestamp: time.Now(),
	})

	// Process the job
	result, err := w.executeJob(ctx, job)
	duration := time.Since(startTime)

	// Update statistics
	w.mu.Lock()
	w.stats.ProcessingTime += duration
	if err != nil {
		w.stats.FailedJobs++
	} else {
		w.stats.ProcessedJobs++
	}
	w.mu.Unlock()

	// Handle job completion
	if err != nil {
		w.handleJobFailure(ctx, job, err, duration)
	} else {
		w.handleJobSuccess(ctx, job, result, duration)
	}

	return nil
}

func (w *worker) CurrentJob() *Job {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.currentJob
}

func (w *worker) IsIdle() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status == WorkerStatusIdle
}

func (w *worker) IsProcessing() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status == WorkerStatusProcessing
}

func (w *worker) Stats() WorkerStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	stats := w.stats
	stats.TotalUptime = time.Since(w.stats.StartedAt)
	stats.IdleTime = stats.TotalUptime - stats.ProcessingTime

	if stats.ProcessedJobs > 0 {
		stats.AverageRunTime = stats.ProcessingTime / time.Duration(stats.ProcessedJobs)
	}

	return stats
}

func (w *worker) processLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if w.IsIdle() {
				if err := w.ProcessNext(w.ctx); err != nil && err != ErrWorkerBusy {
					w.logger.Error("Error processing next job", logger.Error(err))
				}
			}
		}
	}
}

func (w *worker) executeJob(ctx context.Context, job Job) (interface{}, error) {
	// Get handler for job type
	handler, exists := w.handlers[job.Type]
	if !exists {
		return nil, ErrHandlerNotFound
	}

	// Create context with timeout
	timeout := job.Timeout
	if timeout == 0 {
		timeout = handler.GetTimeout()
	}

	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute job
	return handler.Handle(jobCtx, job)
}

func (w *worker) handleJobSuccess(ctx context.Context, job Job, result interface{}, duration time.Duration) {
	now := time.Now()
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now

	if result != nil {
		if resultStr, ok := result.(string); ok {
			job.Result = resultStr
		} else {
			job.Result = fmt.Sprintf("%v", result)
		}
	}

	if err := w.storage.UpdateJob(ctx, job); err != nil {
		w.logger.Error("Failed to update completed job",
			logger.String("job_id", job.ID),
			logger.Error(err),
		)
	}

	// Update queue stats
	if memQueue, ok := w.queueRef.(*memoryQueue); ok {
		memQueue.UpdateStats("completed", 1)
		memQueue.UpdateStats("running", -1)
	}

	w.publishEvent(ctx, Event{
		Type:      EventJobCompleted,
		JobID:     job.ID,
		Queue:     job.Queue,
		WorkerID:  w.id,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"duration": duration.String(),
			"result":   result,
		},
	})

	w.logger.Info("Job completed successfully",
		logger.String("job_id", job.ID),
		logger.String("job_type", job.Type),
		logger.Duration("duration", duration),
	)
}

func (w *worker) handleJobFailure(ctx context.Context, job Job, jobErr error, duration time.Duration) {
	now := time.Now()
	job.LastError = jobErr.Error()
	job.UpdatedAt = now

	// Get retry policy
	retryPolicy := job.RetryPolicy
	if retryPolicy == nil {
		// Get from handler
		if handler, exists := w.handlers[job.Type]; exists {
			policy := handler.GetRetryPolicy()
			retryPolicy = &policy
		} else {
			policy := DefaultRetryPolicy()
			retryPolicy = &policy
		}
	}

	// Check if we should retry
	if job.Attempts < retryPolicy.MaxRetries {
		// Calculate retry delay
		delay := w.calculateRetryDelay(job.Attempts, *retryPolicy)
		job.Status = JobStatusRetrying
		retryAt := now.Add(delay)
		job.RunAt = &retryAt

		if err := w.storage.UpdateJob(ctx, job); err != nil {
			w.logger.Error("Failed to update retrying job",
				logger.String("job_id", job.ID),
				logger.Error(err),
			)
		}

		w.publishEvent(ctx, Event{
			Type:      EventJobRetried,
			JobID:     job.ID,
			Queue:     job.Queue,
			WorkerID:  w.id,
			Timestamp: time.Now(),
			Error:     jobErr.Error(),
			Data: map[string]interface{}{
				"attempt":  job.Attempts,
				"retry_at": retryAt,
				"duration": duration.String(),
			},
		})

		w.logger.Warn("Job failed, retrying",
			logger.String("job_id", job.ID),
			logger.String("job_type", job.Type),
			logger.Int("attempt", job.Attempts),
			logger.Time("retry_at", retryAt),
			logger.Error(jobErr),
		)
	} else {
		// Exceeded max retries, mark as failed
		job.Status = JobStatusFailed
		job.FailedAt = &now

		if err := w.storage.UpdateJob(ctx, job); err != nil {
			w.logger.Error("Failed to update failed job",
				logger.String("job_id", job.ID),
				logger.Error(err),
			)
		}

		// Move to dead letter queue if enabled
		if memQueue, ok := w.queueRef.(*memoryQueue); ok {
			memQueue.AddToDeadLetter(job)
			memQueue.UpdateStats("failed", 1)
			memQueue.UpdateStats("running", -1)
		}

		w.publishEvent(ctx, Event{
			Type:      EventJobFailed,
			JobID:     job.ID,
			Queue:     job.Queue,
			WorkerID:  w.id,
			Timestamp: time.Now(),
			Error:     jobErr.Error(),
			Data: map[string]interface{}{
				"attempts": job.Attempts,
				"duration": duration.String(),
			},
		})

		w.logger.Error("Job failed permanently",
			logger.String("job_id", job.ID),
			logger.String("job_type", job.Type),
			logger.Int("attempts", job.Attempts),
			logger.Error(jobErr),
		)
	}
}

func (w *worker) calculateRetryDelay(attempt int, policy RetryPolicy) time.Duration {
	delay := policy.InitialInterval
	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * policy.Multiplier)
		if delay > policy.MaxInterval {
			delay = policy.MaxInterval
			break
		}
	}

	// Add random jitter if configured
	if policy.RandomFactor > 0 {
		jitter := time.Duration(float64(delay) * policy.RandomFactor)
		// Simple jitter - in production you'd use proper random
		delay += jitter / 2
	}

	return delay
}

func (w *worker) publishEvent(ctx context.Context, event Event) {
	if w.eventBus != nil {
		if err := w.eventBus.PublishAsync(ctx, event); err != nil {
			w.logger.Error("Failed to publish event",
				logger.String("event_type", string(event.Type)),
				logger.Error(err),
			)
		}
	}
}
