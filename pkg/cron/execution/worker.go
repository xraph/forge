package execution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	cron "github.com/xraph/forge/pkg/cron/core"
	"github.com/xraph/forge/pkg/logger"
)

// WorkerPool manages a pool of workers for job execution
type WorkerPool struct {
	size        int
	workers     []*Worker
	jobQueue    chan *ExecutionContext
	stopChannel chan struct{}
	wg          sync.WaitGroup
	mu          sync.RWMutex
	started     bool

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// Worker represents a worker that executes jobs
type Worker struct {
	id          int
	pool        *WorkerPool
	jobQueue    chan *ExecutionContext
	stopChannel chan struct{}
	wg          sync.WaitGroup
	currentJob  *ExecutionContext
	mu          sync.RWMutex
	started     bool

	// Statistics
	jobsExecuted  int64
	jobsSucceeded int64
	jobsFailed    int64
	totalDuration time.Duration
	startTime     time.Time
	lastJobTime   time.Time

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(size int, logger common.Logger, metrics common.Metrics) *WorkerPool {
	if size <= 0 {
		size = 1
	}

	pool := &WorkerPool{
		size:        size,
		workers:     make([]*Worker, size),
		jobQueue:    make(chan *ExecutionContext, size*2), // Buffer for better performance
		stopChannel: make(chan struct{}),
		logger:      logger,
		metrics:     metrics,
	}

	// Create workers
	for i := 0; i < size; i++ {
		pool.workers[i] = &Worker{
			id:          i,
			pool:        pool,
			jobQueue:    pool.jobQueue,
			stopChannel: make(chan struct{}),
			logger:      logger,
			metrics:     metrics,
		}
	}

	return pool
}

// Start starts the worker pool
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.started {
		return common.ErrLifecycleError("start", fmt.Errorf("worker pool already started"))
	}

	// OnStart all workers
	for i, worker := range wp.workers {
		if err := worker.Start(ctx); err != nil {
			// OnStop already started workers
			for j := 0; j < i; j++ {
				wp.workers[j].Stop(ctx)
			}
			return common.ErrServiceStartFailed("worker-pool", err)
		}
	}

	wp.started = true

	if wp.logger != nil {
		wp.logger.Info("worker pool started",
			logger.Int("size", wp.size),
		)
	}

	if wp.metrics != nil {
		wp.metrics.Counter("forge.cron.worker_pool_started").Inc()
		wp.metrics.Gauge("forge.cron.worker_pool_size").Set(float64(wp.size))
	}

	return nil
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop(ctx context.Context) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("worker pool not started"))
	}

	if wp.logger != nil {
		wp.logger.Info("stopping worker pool")
	}

	// Signal stop to all workers
	close(wp.stopChannel)

	// OnStop all workers
	for _, worker := range wp.workers {
		worker.Stop(ctx)
	}

	// Close job queue
	close(wp.jobQueue)

	wp.started = false

	if wp.logger != nil {
		wp.logger.Info("worker pool stopped")
	}

	if wp.metrics != nil {
		wp.metrics.Counter("forge.cron.worker_pool_stopped").Inc()
	}

	return nil
}

// Submit submits a job for execution
func (wp *WorkerPool) Submit(execCtx *ExecutionContext) error {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.started {
		return common.ErrLifecycleError("submit", fmt.Errorf("worker pool not started"))
	}

	select {
	case wp.jobQueue <- execCtx:
		if wp.metrics != nil {
			wp.metrics.Counter("forge.cron.jobs_submitted").Inc()
		}
		return nil
	default:
		return common.ErrContainerError("submit", fmt.Errorf("job queue full"))
	}
}

// GetStats returns worker pool statistics
func (wp *WorkerPool) GetStats() *WorkerStats {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	stats := &WorkerStats{
		Size:          wp.size,
		Started:       wp.started,
		QueueLength:   len(wp.jobQueue),
		QueueCapacity: cap(wp.jobQueue),
		Workers:       make([]WorkerInfo, len(wp.workers)),
		LastUpdate:    time.Now(),
	}

	// Aggregate worker stats
	for i, worker := range wp.workers {
		workerInfo := worker.GetInfo()
		stats.Workers[i] = workerInfo
		stats.TotalJobsExecuted += workerInfo.JobsExecuted
		stats.TotalJobsSucceeded += workerInfo.JobsSucceeded
		stats.TotalJobsFailed += workerInfo.JobsFailed
		stats.TotalDuration += workerInfo.TotalDuration

		if workerInfo.CurrentJob != nil {
			stats.ActiveWorkers++
		}
	}

	// Calculate averages
	if stats.TotalJobsExecuted > 0 {
		stats.AverageJobDuration = stats.TotalDuration / time.Duration(stats.TotalJobsExecuted)
		stats.SuccessRate = float64(stats.TotalJobsSucceeded) / float64(stats.TotalJobsExecuted)
	}

	return stats
}

// HealthCheck performs a health check on the worker pool
func (wp *WorkerPool) HealthCheck(ctx context.Context) error {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.started {
		return common.ErrHealthCheckFailed("worker-pool", fmt.Errorf("worker pool not started"))
	}

	// Check worker health
	unhealthyWorkers := 0
	for _, worker := range wp.workers {
		if err := worker.HealthCheck(ctx); err != nil {
			unhealthyWorkers++
		}
	}

	if unhealthyWorkers > len(wp.workers)/2 {
		return common.ErrHealthCheckFailed("worker-pool", fmt.Errorf("too many unhealthy workers: %d/%d", unhealthyWorkers, len(wp.workers)))
	}

	return nil
}

// Start starts the worker
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return common.ErrLifecycleError("start", fmt.Errorf("worker already started"))
	}

	w.startTime = time.Now()
	w.started = true

	// OnStart worker loop
	w.wg.Add(1)
	go w.workerLoop(ctx)

	if w.logger != nil {
		w.logger.Debug("worker started", logger.Int("worker_id", w.id))
	}

	if w.metrics != nil {
		w.metrics.Counter("forge.cron.worker_started").Inc()
	}

	return nil
}

// Stop stops the worker
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("worker not started"))
	}

	// Signal stop
	close(w.stopChannel)

	// Cancel current job if running
	if w.currentJob != nil && w.currentJob.Cancel != nil {
		w.currentJob.Cancel()
	}

	// Wait for worker loop to finish
	w.wg.Wait()

	w.started = false

	if w.logger != nil {
		w.logger.Debug("worker stopped", logger.Int("worker_id", w.id))
	}

	if w.metrics != nil {
		w.metrics.Counter("forge.cron.worker_stopped").Inc()
	}

	return nil
}

// workerLoop runs the main worker loop
func (w *Worker) workerLoop(ctx context.Context) {
	defer w.wg.Done()

	for {
		select {
		case <-w.stopChannel:
			return
		case <-ctx.Done():
			return
		case execCtx, ok := <-w.jobQueue:
			if !ok {
				return // Job queue closed
			}
			w.executeJob(execCtx)
		}
	}
}

// executeJob executes a job
func (w *Worker) executeJob(execCtx *ExecutionContext) {
	w.mu.Lock()
	w.currentJob = execCtx
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.currentJob = nil
		w.mu.Unlock()
	}()

	startTime := time.Now()
	w.lastJobTime = startTime

	// Mark execution as started
	execCtx.Execution.MarkStarted()

	if w.logger != nil {
		w.logger.Info("executing job",
			logger.Int("worker_id", w.id),
			logger.String("job_id", execCtx.Job.Definition.ID),
			logger.String("execution_id", execCtx.Execution.ID),
		)
	}

	// Create execution result
	result := &ExecutionResult{
		Success:  false,
		Output:   "",
		Error:    nil,
		Duration: 0,
		Metadata: make(map[string]interface{}),
	}

	// Execute job with timeout
	timeout := execCtx.Job.Definition.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute // Default timeout
	}

	execCtx.Context, execCtx.Cancel = context.WithTimeout(execCtx.Context, timeout)
	defer execCtx.Cancel()

	// Execute job handler
	err := w.executeWithRecovery(execCtx)

	// Calculate duration
	result.Duration = time.Since(startTime)

	// Update result
	if err != nil {
		result.Success = false
		result.Error = err

		// Check if it's a timeout error
		if execCtx.Context.Err() == context.DeadlineExceeded {
			execCtx.Execution.Status = cron.ExecutionStatusTimeout

			// Call handler timeout callback
			if execCtx.Handler != nil {
				execCtx.Handler.OnTimeout(execCtx.Context, execCtx.Job, execCtx.Execution)
			}
		}
	} else {
		result.Success = true
	}

	// Mark execution as completed
	status := cron.ExecutionStatusSuccess
	if !result.Success {
		status = cron.ExecutionStatusFailed
	}
	execCtx.Execution.MarkCompleted(status, result.Output, result.Error)

	// Update worker statistics
	w.mu.Lock()
	w.jobsExecuted++
	w.totalDuration += result.Duration
	if result.Success {
		w.jobsSucceeded++
	} else {
		w.jobsFailed++
	}
	w.mu.Unlock()

	// Notify pool of completion
	if w.pool != nil {
		// This would be called if the pool had an executor reference
		// w.pool.executor.OnExecutionComplete(execCtx.Execution.ID, result)
	}

	if w.logger != nil {
		if result.Success {
			w.logger.Info("job execution completed",
				logger.Int("worker_id", w.id),
				logger.String("job_id", execCtx.Job.Definition.ID),
				logger.String("execution_id", execCtx.Execution.ID),
				logger.Duration("duration", result.Duration),
			)
		} else {
			w.logger.Error("job execution failed",
				logger.Int("worker_id", w.id),
				logger.String("job_id", execCtx.Job.Definition.ID),
				logger.String("execution_id", execCtx.Execution.ID),
				logger.Duration("duration", result.Duration),
				logger.Error(result.Error),
			)
		}
	}

	if w.metrics != nil {
		if result.Success {
			w.metrics.Counter("forge.cron.worker_jobs_success").Inc()
		} else {
			w.metrics.Counter("forge.cron.worker_jobs_failed").Inc()
		}
		w.metrics.Histogram("forge.cron.worker_job_duration").Observe(result.Duration.Seconds())
	}
}

// executeWithRecovery executes a job with panic recovery
func (w *Worker) executeWithRecovery(execCtx *ExecutionContext) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("job panicked: %v", r)

			if w.logger != nil {
				w.logger.Error("job panicked",
					logger.Int("worker_id", w.id),
					logger.String("job_id", execCtx.Job.Definition.ID),
					logger.String("execution_id", execCtx.Execution.ID),
					logger.Any("panic", r),
				)
			}

			if w.metrics != nil {
				w.metrics.Counter("forge.cron.worker_panics").Inc()
			}
		}
	}()

	// Execute the job handler
	return execCtx.Handler.Execute(execCtx.Context, execCtx.Job)
}

// GetInfo returns worker information
func (w *Worker) GetInfo() WorkerInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	info := WorkerInfo{
		ID:            w.id,
		Started:       w.started,
		JobsExecuted:  w.jobsExecuted,
		JobsSucceeded: w.jobsSucceeded,
		JobsFailed:    w.jobsFailed,
		TotalDuration: w.totalDuration,
		StartTime:     w.startTime,
		LastJobTime:   w.lastJobTime,
	}

	if w.currentJob != nil {
		info.CurrentJob = &CurrentJobInfo{
			JobID:       w.currentJob.Job.Definition.ID,
			ExecutionID: w.currentJob.Execution.ID,
			StartTime:   w.currentJob.StartTime,
			Duration:    time.Since(w.currentJob.StartTime),
		}
	}

	// Calculate averages
	if w.jobsExecuted > 0 {
		info.AverageJobDuration = w.totalDuration / time.Duration(w.jobsExecuted)
		info.SuccessRate = float64(w.jobsSucceeded) / float64(w.jobsExecuted)
	}

	return info
}

// HealthCheck performs a health check on the worker
func (w *Worker) HealthCheck(ctx context.Context) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.started {
		return common.ErrHealthCheckFailed("worker", fmt.Errorf("worker %d not started", w.id))
	}

	// Check for stuck jobs
	if w.currentJob != nil {
		jobDuration := time.Since(w.currentJob.StartTime)
		maxDuration := w.currentJob.Job.Definition.Timeout
		if maxDuration <= 0 {
			maxDuration = 1 * time.Hour // Default max duration
		}

		if jobDuration > maxDuration*2 { // Allow some buffer
			return common.ErrHealthCheckFailed("worker", fmt.Errorf("worker %d has stuck job running for %v", w.id, jobDuration))
		}
	}

	return nil
}

// WorkerStats contains worker pool statistics
type WorkerStats struct {
	Size               int           `json:"size"`
	Started            bool          `json:"started"`
	ActiveWorkers      int           `json:"active_workers"`
	QueueLength        int           `json:"queue_length"`
	QueueCapacity      int           `json:"queue_capacity"`
	TotalJobsExecuted  int64         `json:"total_jobs_executed"`
	TotalJobsSucceeded int64         `json:"total_jobs_succeeded"`
	TotalJobsFailed    int64         `json:"total_jobs_failed"`
	TotalDuration      time.Duration `json:"total_duration"`
	AverageJobDuration time.Duration `json:"average_job_duration"`
	SuccessRate        float64       `json:"success_rate"`
	Workers            []WorkerInfo  `json:"workers"`
	LastUpdate         time.Time     `json:"last_update"`
}

// WorkerInfo contains information about a worker
type WorkerInfo struct {
	ID                 int             `json:"id"`
	Started            bool            `json:"started"`
	JobsExecuted       int64           `json:"jobs_executed"`
	JobsSucceeded      int64           `json:"jobs_succeeded"`
	JobsFailed         int64           `json:"jobs_failed"`
	TotalDuration      time.Duration   `json:"total_duration"`
	AverageJobDuration time.Duration   `json:"average_job_duration"`
	SuccessRate        float64         `json:"success_rate"`
	StartTime          time.Time       `json:"start_time"`
	LastJobTime        time.Time       `json:"last_job_time"`
	CurrentJob         *CurrentJobInfo `json:"current_job,omitempty"`
}

// CurrentJobInfo contains information about the currently executing job
type CurrentJobInfo struct {
	JobID       string        `json:"job_id"`
	ExecutionID string        `json:"execution_id"`
	StartTime   time.Time     `json:"start_time"`
	Duration    time.Duration `json:"duration"`
}
