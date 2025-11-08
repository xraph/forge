package statemachine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/internal/errors"
)

// ApplyManager manages optimized log entry application.
type ApplyManager struct {
	stateMachine internal.StateMachine
	logger       forge.Logger

	// Apply pipeline
	applyPipeline chan applyTask
	resultChans   map[uint64]chan applyResult
	resultMu      sync.RWMutex

	// Configuration
	pipelineDepth int
	workerCount   int
	batchSize     int
	batchTimeout  time.Duration

	// Statistics
	totalApplied   uint64
	totalFailed    uint64
	averageLatency time.Duration
	statsMu        sync.RWMutex

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// applyTask represents a task to apply log entries.
type applyTask struct {
	entries []internal.LogEntry
	result  chan applyResult
}

// applyResult represents the result of applying log entries.
type applyResult struct {
	lastIndex uint64
	count     int
	err       error
	duration  time.Duration
}

// ApplyManagerConfig contains apply manager configuration.
type ApplyManagerConfig struct {
	PipelineDepth int
	WorkerCount   int
	BatchSize     int
	BatchTimeout  time.Duration
}

// NewApplyManager creates a new apply manager.
func NewApplyManager(
	stateMachine internal.StateMachine,
	config ApplyManagerConfig,
	logger forge.Logger,
) *ApplyManager {
	if config.PipelineDepth == 0 {
		config.PipelineDepth = 1000
	}

	if config.WorkerCount == 0 {
		config.WorkerCount = 4
	}

	if config.BatchSize == 0 {
		config.BatchSize = 100
	}

	if config.BatchTimeout == 0 {
		config.BatchTimeout = 10 * time.Millisecond
	}

	return &ApplyManager{
		stateMachine:  stateMachine,
		logger:        logger,
		applyPipeline: make(chan applyTask, config.PipelineDepth),
		resultChans:   make(map[uint64]chan applyResult),
		pipelineDepth: config.PipelineDepth,
		workerCount:   config.WorkerCount,
		batchSize:     config.BatchSize,
		batchTimeout:  config.BatchTimeout,
	}
}

// Start starts the apply manager.
func (am *ApplyManager) Start(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.started {
		return internal.ErrAlreadyStarted
	}

	am.ctx, am.cancel = context.WithCancel(ctx)
	am.started = true

	// Start worker pool
	for i := range am.workerCount {
		am.wg.Add(1)

		go am.applyWorker(i)
	}

	am.logger.Info("apply manager started",
		forge.F("workers", am.workerCount),
		forge.F("pipeline_depth", am.pipelineDepth),
		forge.F("batch_size", am.batchSize),
	)

	return nil
}

// Stop stops the apply manager.
func (am *ApplyManager) Stop(ctx context.Context) error {
	am.mu.Lock()

	if !am.started {
		am.mu.Unlock()

		return internal.ErrNotStarted
	}

	am.mu.Unlock()

	if am.cancel != nil {
		am.cancel()
	}

	// Close pipeline
	close(am.applyPipeline)

	// Wait for workers
	done := make(chan struct{})

	go func() {
		am.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		am.logger.Info("apply manager stopped")
	case <-ctx.Done():
		am.logger.Warn("apply manager stop timed out")
	}

	return nil
}

// ApplyEntries applies log entries asynchronously.
func (am *ApplyManager) ApplyEntries(entries []internal.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Create result channel
	resultChan := make(chan applyResult, 1)

	// Store result channel
	lastIndex := entries[len(entries)-1].Index

	am.resultMu.Lock()
	am.resultChans[lastIndex] = resultChan
	am.resultMu.Unlock()

	// Submit task
	task := applyTask{
		entries: entries,
		result:  resultChan,
	}

	select {
	case am.applyPipeline <- task:
		return nil
	case <-am.ctx.Done():
		return errors.New("apply manager stopped")
	default:
		return errors.New("apply pipeline full")
	}
}

// ApplyEntriesSync applies log entries synchronously.
func (am *ApplyManager) ApplyEntriesSync(entries []internal.LogEntry, timeout time.Duration) error {
	if len(entries) == 0 {
		return nil
	}

	// Submit for async processing
	if err := am.ApplyEntries(entries); err != nil {
		return err
	}

	// Wait for result
	lastIndex := entries[len(entries)-1].Index

	return am.WaitForApply(lastIndex, timeout)
}

// WaitForApply waits for entries up to index to be applied.
func (am *ApplyManager) WaitForApply(index uint64, timeout time.Duration) error {
	am.resultMu.RLock()
	resultChan, exists := am.resultChans[index]
	am.resultMu.RUnlock()

	if !exists {
		return fmt.Errorf("no pending apply for index %d", index)
	}

	// Wait for result with timeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-resultChan:
		// Clean up result channel
		am.resultMu.Lock()
		delete(am.resultChans, index)
		am.resultMu.Unlock()

		return result.err

	case <-timer.C:
		return fmt.Errorf("apply timeout for index %d", index)

	case <-am.ctx.Done():
		return errors.New("apply manager stopped")
	}
}

// applyWorker processes apply tasks.
func (am *ApplyManager) applyWorker(workerID int) {
	defer am.wg.Done()

	am.logger.Debug("apply worker started",
		forge.F("worker_id", workerID),
	)

	for {
		select {
		case <-am.ctx.Done():
			return

		case task, ok := <-am.applyPipeline:
			if !ok {
				return
			}

			// Process task
			result := am.processApplyTask(task)

			// Send result
			select {
			case task.result <- result:
			case <-am.ctx.Done():
				return
			}
		}
	}
}

// processApplyTask processes an apply task.
func (am *ApplyManager) processApplyTask(task applyTask) applyResult {
	startTime := time.Now()

	result := applyResult{
		count: len(task.entries),
	}

	// Apply entries in batches
	for i := 0; i < len(task.entries); i += am.batchSize {
		end := min(i+am.batchSize, len(task.entries))

		batch := task.entries[i:end]

		// Apply batch
		for _, entry := range batch {
			if err := am.stateMachine.Apply(entry); err != nil {
				result.err = fmt.Errorf("failed to apply entry %d: %w", entry.Index, err)
				am.logger.Error("apply failed",
					forge.F("index", entry.Index),
					forge.F("error", err),
				)

				am.statsMu.Lock()
				am.totalFailed++
				am.statsMu.Unlock()

				return result
			}

			result.lastIndex = entry.Index
		}
	}

	result.duration = time.Since(startTime)

	// Update statistics
	am.statsMu.Lock()
	am.totalApplied += uint64(result.count)

	// Update running average latency
	if am.averageLatency == 0 {
		am.averageLatency = result.duration
	} else {
		am.averageLatency = (am.averageLatency + result.duration) / 2
	}

	am.statsMu.Unlock()

	am.logger.Debug("applied entries",
		forge.F("count", result.count),
		forge.F("last_index", result.lastIndex),
		forge.F("duration_ms", result.duration.Milliseconds()),
	)

	return result
}

// ApplyBatch applies a batch of entries with optimizations.
func (am *ApplyManager) ApplyBatch(entries []internal.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Split into smaller batches for parallel processing
	batchCount := (len(entries) + am.batchSize - 1) / am.batchSize
	resultChans := make([]chan applyResult, batchCount)

	for i := range batchCount {
		start := i * am.batchSize

		end := min(start+am.batchSize, len(entries))

		batch := entries[start:end]
		resultChan := make(chan applyResult, 1)
		resultChans[i] = resultChan

		// Submit batch
		task := applyTask{
			entries: batch,
			result:  resultChan,
		}

		select {
		case am.applyPipeline <- task:
		case <-am.ctx.Done():
			return errors.New("apply manager stopped")
		default:
			return errors.New("apply pipeline full")
		}
	}

	// Wait for all batches to complete
	for i, resultChan := range resultChans {
		select {
		case result := <-resultChan:
			if result.err != nil {
				return fmt.Errorf("batch %d failed: %w", i, result.err)
			}

		case <-time.After(30 * time.Second):
			return fmt.Errorf("batch %d timeout", i)

		case <-am.ctx.Done():
			return errors.New("apply manager stopped")
		}
	}

	return nil
}

// ApplyWithRetry applies entries with retry logic.
func (am *ApplyManager) ApplyWithRetry(entries []internal.LogEntry, maxRetries int) error {
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			am.logger.Warn("retrying apply",
				forge.F("attempt", attempt),
				forge.F("entries", len(entries)),
			)

			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			time.Sleep(backoff)
		}

		err = am.ApplyEntriesSync(entries, 30*time.Second)
		if err == nil {
			return nil
		}
	}

	am.logger.Error("apply failed after retries",
		forge.F("attempts", maxRetries+1),
		forge.F("error", err),
	)

	return err
}

// GetStats returns apply manager statistics.
func (am *ApplyManager) GetStats() map[string]any {
	am.statsMu.RLock()
	defer am.statsMu.RUnlock()

	am.resultMu.RLock()
	pendingCount := len(am.resultChans)
	am.resultMu.RUnlock()

	pipelineSize := len(am.applyPipeline)

	stats := map[string]any{
		"total_applied":      am.totalApplied,
		"total_failed":       am.totalFailed,
		"average_latency_ms": am.averageLatency.Milliseconds(),
		"pending_count":      pendingCount,
		"pipeline_size":      pipelineSize,
		"pipeline_capacity":  am.pipelineDepth,
		"worker_count":       am.workerCount,
	}

	if am.totalApplied > 0 {
		successRate := float64(am.totalApplied) / float64(am.totalApplied+am.totalFailed)
		stats["success_rate"] = successRate
	}

	return stats
}

// GetPipelineUtilization returns pipeline utilization percentage.
func (am *ApplyManager) GetPipelineUtilization() float64 {
	size := len(am.applyPipeline)

	return float64(size) / float64(am.pipelineDepth) * 100.0
}

// IsPipelineFull returns true if the pipeline is full.
func (am *ApplyManager) IsPipelineFull() bool {
	return len(am.applyPipeline) >= am.pipelineDepth
}

// GetPendingCount returns the number of pending applies.
func (am *ApplyManager) GetPendingCount() int {
	am.resultMu.RLock()
	defer am.resultMu.RUnlock()

	return len(am.resultChans)
}

// FlushPending waits for all pending applies to complete.
func (am *ApplyManager) FlushPending(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		am.resultMu.RLock()
		pending := len(am.resultChans)
		am.resultMu.RUnlock()

		if pending == 0 {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("flush timeout with %d pending", pending)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
