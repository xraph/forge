package sdk

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// BatchProcessor handles efficient bulk request processing
type BatchProcessor struct {
	llmManager  LLMManager
	logger      forge.Logger
	metrics     forge.Metrics
	
	// Configuration
	maxBatchSize    int
	maxWaitTime     time.Duration
	maxConcurrency  int
	enableBatching  bool
	
	// Internal state
	queue           []BatchRequest
	mu              sync.Mutex
	processingTimer *time.Timer
}

// BatchConfig configures batch processing
type BatchConfig struct {
	MaxBatchSize   int           // Maximum requests per batch
	MaxWaitTime    time.Duration // Maximum time to wait before processing
	MaxConcurrency int           // Maximum concurrent batches
	EnableBatching bool          // Enable/disable batching
}

// BatchRequest represents a single request in a batch
type BatchRequest struct {
	ID      string
	Prompt  string
	Model   string
	Options map[string]interface{}
	Result  chan<- *Result
	Error   chan<- error
}

// BatchResult contains results from batch processing
type BatchResult struct {
	Successful int
	Failed     int
	TotalTime  time.Duration
	Results    []*Result
	Errors     []error
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(
	llmManager LLMManager,
	logger forge.Logger,
	metrics forge.Metrics,
	config BatchConfig,
) *BatchProcessor {
	if config.MaxBatchSize == 0 {
		config.MaxBatchSize = 10
	}
	if config.MaxWaitTime == 0 {
		config.MaxWaitTime = 100 * time.Millisecond
	}
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 5
	}

	return &BatchProcessor{
		llmManager:     llmManager,
		logger:         logger,
		metrics:        metrics,
		maxBatchSize:   config.MaxBatchSize,
		maxWaitTime:    config.MaxWaitTime,
		maxConcurrency: config.MaxConcurrency,
		enableBatching: config.EnableBatching,
		queue:          make([]BatchRequest, 0, config.MaxBatchSize),
	}
}

// Submit submits a request for batch processing
func (bp *BatchProcessor) Submit(ctx context.Context, req BatchRequest) error {
	if !bp.enableBatching {
		// Process immediately if batching disabled
		return bp.processImmediate(ctx, req)
	}

	bp.mu.Lock()
	defer bp.mu.Unlock()

	bp.queue = append(bp.queue, req)

	// Process if batch is full
	if len(bp.queue) >= bp.maxBatchSize {
		go bp.processBatch()
		bp.queue = make([]BatchRequest, 0, bp.maxBatchSize)
		if bp.processingTimer != nil {
			bp.processingTimer.Stop()
		}
		return nil
	}

	// Start/reset timer
	if bp.processingTimer == nil {
		bp.processingTimer = time.AfterFunc(bp.maxWaitTime, func() {
			bp.processBatch()
		})
	}

	return nil
}

// ProcessBatch processes all requests in a batch
func (bp *BatchProcessor) ProcessBatch(ctx context.Context, requests []BatchRequest) *BatchResult {
	start := time.Now()
	result := &BatchResult{
		Results: make([]*Result, 0, len(requests)),
		Errors:  make([]error, 0),
	}

	// Use worker pool for concurrency
	semaphore := make(chan struct{}, bp.maxConcurrency)
	var wg sync.WaitGroup

	for _, req := range requests {
		wg.Add(1)
		semaphore <- struct{}{} // Acquire

		go func(r BatchRequest) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release

			// Process individual request
			builder := NewGenerateBuilder(ctx, bp.llmManager, bp.logger, bp.metrics)
			builder.WithPrompt(r.Prompt)
			if r.Model != "" {
				builder.WithModel(r.Model)
			}

			res, err := builder.Execute()
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, err)
				if r.Error != nil {
					r.Error <- err
				}
			} else {
				result.Successful++
				result.Results = append(result.Results, res)
				if r.Result != nil {
					r.Result <- res
				}
			}
		}(req)
	}

	wg.Wait()
	result.TotalTime = time.Since(start)

	if bp.metrics != nil {
		bp.metrics.Counter("ai.sdk.batch.processed",
			"status", "success",
		).Add(float64(result.Successful))
		bp.metrics.Counter("ai.sdk.batch.processed",
			"status", "failed",
		).Add(float64(result.Failed))
		bp.metrics.Histogram("ai.sdk.batch.duration").Observe(result.TotalTime.Seconds())
	}

	return result
}

func (bp *BatchProcessor) processBatch() {
	bp.mu.Lock()
	if len(bp.queue) == 0 {
		bp.mu.Unlock()
		return
	}

	batch := make([]BatchRequest, len(bp.queue))
	copy(batch, bp.queue)
	bp.queue = make([]BatchRequest, 0, bp.maxBatchSize)
	bp.processingTimer = nil
	bp.mu.Unlock()

	if bp.logger != nil {
		bp.logger.Info("processing batch",
			F("size", len(batch)),
		)
	}

	ctx := context.Background()
	bp.ProcessBatch(ctx, batch)
}

func (bp *BatchProcessor) processImmediate(ctx context.Context, req BatchRequest) error {
	builder := NewGenerateBuilder(ctx, bp.llmManager, bp.logger, bp.metrics)
	builder.WithPrompt(req.Prompt)
	if req.Model != "" {
		builder.WithModel(req.Model)
	}

	res, err := builder.Execute()
	if err != nil {
		if req.Error != nil {
			req.Error <- err
		}
		return err
	}

	if req.Result != nil {
		req.Result <- res
	}
	return nil
}

// Flush processes all pending requests immediately
func (bp *BatchProcessor) Flush(ctx context.Context) error {
	bp.mu.Lock()
	if len(bp.queue) == 0 {
		bp.mu.Unlock()
		return nil
	}

	batch := make([]BatchRequest, len(bp.queue))
	copy(batch, bp.queue)
	bp.queue = make([]BatchRequest, 0, bp.maxBatchSize)
	bp.mu.Unlock()

	bp.ProcessBatch(ctx, batch)
	return nil
}

// GetStats returns batch processing statistics
func (bp *BatchProcessor) GetStats() BatchStats {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	return BatchStats{
		QueueSize: len(bp.queue),
	}
}

// BatchStats contains batch processing statistics
type BatchStats struct {
	QueueSize int
}

