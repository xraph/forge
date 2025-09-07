package inference

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RequestBatcher handles batching of inference requests for improved efficiency
type RequestBatcher struct {
	config        RequestBatcherConfig
	requestQueue  chan InferenceRequest
	batchQueue    chan *RequestBatch
	activeBatches map[string]*RequestBatch
	stats         BatcherStats
	started       bool
	mu            sync.RWMutex
	logger        common.Logger
	metrics       common.Metrics
	shutdownC     chan struct{}
	wg            sync.WaitGroup
}

// RequestBatcherConfig contains configuration for request batching
type RequestBatcherConfig struct {
	BatchSize     int            `yaml:"batch_size" default:"10"`
	BatchTimeout  time.Duration  `yaml:"batch_timeout" default:"100ms"`
	MaxQueueSize  int            `yaml:"max_queue_size" default:"10000"`
	MaxBatchAge   time.Duration  `yaml:"max_batch_age" default:"5s"`
	Workers       int            `yaml:"workers" default:"4"`
	EnableDynamic bool           `yaml:"enable_dynamic" default:"true"`
	MinBatchSize  int            `yaml:"min_batch_size" default:"1"`
	MaxBatchSize  int            `yaml:"max_batch_size" default:"100"`
	Logger        common.Logger  `yaml:"-"`
	Metrics       common.Metrics `yaml:"-"`
}

// RequestBatch represents a batch of inference requests
type RequestBatch struct {
	ID          string
	ModelID     string
	Requests    []InferenceRequest
	Responses   []InferenceResponse
	CreatedAt   time.Time
	ProcessedAt time.Time
	Size        int
	MaxSize     int
	Timeout     time.Duration
	Status      BatchStatus
	Error       error
	Callbacks   []func([]InferenceResponse)
	mu          sync.RWMutex
}

// BatchStatus represents the status of a batch
type BatchStatus string

const (
	BatchStatusPending    BatchStatus = "pending"
	BatchStatusProcessing BatchStatus = "processing"
	BatchStatusCompleted  BatchStatus = "completed"
	BatchStatusFailed     BatchStatus = "failed"
	BatchStatusTimeout    BatchStatus = "timeout"
)

// BatcherStats contains statistics for the request batcher
type BatcherStats struct {
	BatchesCreated     int64         `json:"batches_created"`
	BatchesProcessed   int64         `json:"batches_processed"`
	BatchesTimeout     int64         `json:"batches_timeout"`
	BatchesError       int64         `json:"batches_error"`
	RequestsQueued     int64         `json:"requests_queued"`
	RequestsProcessed  int64         `json:"requests_processed"`
	AverageBatchSize   float64       `json:"average_batch_size"`
	AverageWaitTime    time.Duration `json:"average_wait_time"`
	AverageProcessTime time.Duration `json:"average_process_time"`
	QueueSize          int           `json:"queue_size"`
	ActiveBatches      int           `json:"active_batches"`
	BatchSizeHistogram map[int]int64 `json:"batch_size_histogram"`
	LastUpdated        time.Time     `json:"last_updated"`
}

// BatchingStrategy defines different batching strategies
type BatchingStrategy interface {
	Name() string
	ShouldBatch(request InferenceRequest, currentBatch *RequestBatch) bool
	OptimalBatchSize(modelID string, queueSize int) int
	BatchTimeout(modelID string, batchSize int) time.Duration
}

// DynamicBatchingStrategy implements dynamic batching based on queue size and model characteristics
type DynamicBatchingStrategy struct {
	name         string
	modelMetrics map[string]*ModelBatchingMetrics
	logger       common.Logger
	mu           sync.RWMutex
}

// ModelBatchingMetrics contains metrics for model-specific batching
type ModelBatchingMetrics struct {
	ModelID            string
	OptimalBatchSize   int
	AverageLatency     time.Duration
	ThroughputPerBatch float64
	LastOptimization   time.Time
	SampleCount        int64
	mu                 sync.RWMutex
}

// NewRequestBatcher creates a new request batcher
func NewRequestBatcher(config RequestBatcherConfig) (*RequestBatcher, error) {
	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 100 * time.Millisecond
	}
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 10000
	}
	if config.MaxBatchAge == 0 {
		config.MaxBatchAge = 5 * time.Second
	}
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.MinBatchSize <= 0 {
		config.MinBatchSize = 1
	}
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 100
	}

	batcher := &RequestBatcher{
		config:        config,
		requestQueue:  make(chan InferenceRequest, config.MaxQueueSize),
		batchQueue:    make(chan *RequestBatch, config.MaxQueueSize/config.BatchSize),
		activeBatches: make(map[string]*RequestBatch),
		stats:         BatcherStats{BatchSizeHistogram: make(map[int]int64)},
		logger:        config.Logger,
		metrics:       config.Metrics,
		shutdownC:     make(chan struct{}),
	}

	return batcher, nil
}

// Start starts the request batcher
func (b *RequestBatcher) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started {
		return fmt.Errorf("request batcher already started")
	}

	// Start batch creation workers
	for i := 0; i < b.config.Workers; i++ {
		b.wg.Add(1)
		go func(workerID int) {
			defer b.wg.Done()
			b.runBatchCreationWorker(ctx, workerID)
		}(i)
	}

	// Start batch processing workers
	for i := 0; i < b.config.Workers; i++ {
		b.wg.Add(1)
		go func(workerID int) {
			defer b.wg.Done()
			b.runBatchProcessingWorker(ctx, workerID)
		}(i)
	}

	// Start batch timeout monitor
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.runBatchTimeoutMonitor(ctx)
	}()

	// Start stats collection
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.runStatsCollection(ctx)
	}()

	b.started = true

	if b.logger != nil {
		b.logger.Info("request batcher started",
			logger.Int("workers", b.config.Workers),
			logger.Int("batch_size", b.config.BatchSize),
			logger.Duration("batch_timeout", b.config.BatchTimeout),
		)
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.inference_batcher_started").Inc()
	}

	return nil
}

// Stop stops the request batcher
func (b *RequestBatcher) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.started {
		return fmt.Errorf("request batcher not started")
	}

	// Signal shutdown
	close(b.shutdownC)

	// Wait for workers to finish
	b.wg.Wait()

	// Process any remaining batches
	b.processRemainingBatches(ctx)

	b.started = false

	if b.logger != nil {
		b.logger.Info("request batcher stopped")
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.inference_batcher_stopped").Inc()
	}

	return nil
}

// QueueRequest queues a request for batching
func (b *RequestBatcher) QueueRequest(ctx context.Context, request InferenceRequest) error {
	if !b.started {
		return fmt.Errorf("request batcher not started")
	}

	select {
	case b.requestQueue <- request:
		b.updateStats(func(stats *BatcherStats) {
			stats.RequestsQueued++
		})

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.inference_requests_queued").Inc()
		}

		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("request queue full")
	}
}

// ProcessBatch processes a batch of requests
func (b *RequestBatcher) ProcessBatch(ctx context.Context, requests []InferenceRequest) ([]InferenceResponse, error) {
	if len(requests) == 0 {
		return []InferenceResponse{}, nil
	}

	// Create batch
	batch := &RequestBatch{
		ID:        fmt.Sprintf("batch-%d", time.Now().UnixNano()),
		ModelID:   requests[0].ModelID,
		Requests:  requests,
		Responses: make([]InferenceResponse, len(requests)),
		CreatedAt: time.Now(),
		Size:      len(requests),
		MaxSize:   b.config.MaxBatchSize,
		Timeout:   b.config.BatchTimeout,
		Status:    BatchStatusPending,
	}

	// Process batch
	if err := b.processBatch(ctx, batch); err != nil {
		return nil, err
	}

	return batch.Responses, nil
}

// QueueSize returns the current queue size
func (b *RequestBatcher) QueueSize() int {
	return len(b.requestQueue)
}

// GetStats returns batcher statistics
func (b *RequestBatcher) GetStats() BatcherStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := b.stats
	stats.QueueSize = len(b.requestQueue)
	stats.ActiveBatches = len(b.activeBatches)
	stats.LastUpdated = time.Now()

	return stats
}

// runBatchCreationWorker runs a batch creation worker
func (b *RequestBatcher) runBatchCreationWorker(ctx context.Context, workerID int) {
	var currentBatch *RequestBatch
	batchTimer := time.NewTimer(b.config.BatchTimeout)
	batchTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownC:
			return
		case request := <-b.requestQueue:
			// Create new batch if needed
			if currentBatch == nil || !b.canAddToBatch(request, currentBatch) {
				// Submit current batch if it exists
				if currentBatch != nil {
					b.submitBatch(currentBatch)
				}

				// Create new batch
				currentBatch = &RequestBatch{
					ID:        fmt.Sprintf("batch-%d-%d", time.Now().UnixNano(), workerID),
					ModelID:   request.ModelID,
					Requests:  make([]InferenceRequest, 0, b.config.BatchSize),
					CreatedAt: time.Now(),
					MaxSize:   b.config.MaxBatchSize,
					Timeout:   b.config.BatchTimeout,
					Status:    BatchStatusPending,
				}

				// Reset batch timer
				batchTimer.Reset(b.config.BatchTimeout)
			}

			// Add request to batch
			currentBatch.Requests = append(currentBatch.Requests, request)
			currentBatch.Size = len(currentBatch.Requests)

			// Submit batch if it's full
			if currentBatch.Size >= b.config.BatchSize {
				b.submitBatch(currentBatch)
				currentBatch = nil
				batchTimer.Stop()
			}

		case <-batchTimer.C:
			// Submit batch on timeout
			if currentBatch != nil && len(currentBatch.Requests) > 0 {
				b.submitBatch(currentBatch)
				currentBatch = nil
			}
		}
	}
}

// runBatchProcessingWorker runs a batch processing worker
func (b *RequestBatcher) runBatchProcessingWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownC:
			return
		case batch := <-b.batchQueue:
			if err := b.processBatch(ctx, batch); err != nil {
				if b.logger != nil {
					b.logger.Error("failed to process batch",
						logger.String("batch_id", batch.ID),
						logger.Int("worker_id", workerID),
						logger.Error(err),
					)
				}
			}
		}
	}
}

// runBatchTimeoutMonitor monitors batch timeouts
func (b *RequestBatcher) runBatchTimeoutMonitor(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownC:
			return
		case <-ticker.C:
			b.checkBatchTimeouts()
		}
	}
}

// runStatsCollection runs the statistics collection loop
func (b *RequestBatcher) runStatsCollection(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.shutdownC:
			return
		case <-ticker.C:
			b.collectStats()
		}
	}
}

// canAddToBatch checks if a request can be added to the current batch
func (b *RequestBatcher) canAddToBatch(request InferenceRequest, batch *RequestBatch) bool {
	// Check if batch is full
	if batch.Size >= batch.MaxSize {
		return false
	}

	// Check if models match
	if request.ModelID != batch.ModelID {
		return false
	}

	// Check if batch is too old
	if time.Since(batch.CreatedAt) > b.config.MaxBatchAge {
		return false
	}

	return true
}

// submitBatch submits a batch for processing
func (b *RequestBatcher) submitBatch(batch *RequestBatch) {
	b.mu.Lock()
	b.activeBatches[batch.ID] = batch
	b.mu.Unlock()

	select {
	case b.batchQueue <- batch:
		b.updateStats(func(stats *BatcherStats) {
			stats.BatchesCreated++
			stats.BatchSizeHistogram[batch.Size]++
		})

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.inference_batches_created").Inc()
			b.metrics.Histogram("forge.ai.inference_batch_size").Observe(float64(batch.Size))
		}
	default:
		// Queue full, process immediately
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), b.config.BatchTimeout)
			defer cancel()
			b.processBatch(ctx, batch)
		}()
	}
}

// processBatch processes a single batch
func (b *RequestBatcher) processBatch(ctx context.Context, batch *RequestBatch) error {
	startTime := time.Now()

	batch.mu.Lock()
	batch.Status = BatchStatusProcessing
	batch.ProcessedAt = startTime
	batch.mu.Unlock()

	// Process each request in the batch
	responses := make([]InferenceResponse, len(batch.Requests))
	for i, request := range batch.Requests {
		// This is a simplified implementation
		// In practice, this would use the actual model inference
		response := InferenceResponse{
			ID:         request.ID,
			ModelID:    request.ModelID,
			Output:     request.Input, // Echo for now
			Latency:    time.Since(request.Timestamp),
			Confidence: 0.95,
			Timestamp:  time.Now(),
			FromCache:  false,
			BatchSize:  batch.Size,
			WorkerID:   fmt.Sprintf("batcher-%s", batch.ID),
		}
		responses[i] = response
	}

	// Update batch
	batch.mu.Lock()
	batch.Responses = responses
	batch.Status = BatchStatusCompleted
	batch.mu.Unlock()

	// Execute callbacks
	for _, callback := range batch.Callbacks {
		go callback(responses)
	}

	// Update statistics
	processTime := time.Since(startTime)
	waitTime := batch.ProcessedAt.Sub(batch.CreatedAt)

	b.updateStats(func(stats *BatcherStats) {
		stats.BatchesProcessed++
		stats.RequestsProcessed += int64(batch.Size)
		stats.AverageProcessTime = (stats.AverageProcessTime*time.Duration(stats.BatchesProcessed-1) + processTime) / time.Duration(stats.BatchesProcessed)
		stats.AverageWaitTime = (stats.AverageWaitTime*time.Duration(stats.BatchesProcessed-1) + waitTime) / time.Duration(stats.BatchesProcessed)

		if stats.BatchesProcessed > 0 {
			stats.AverageBatchSize = float64(stats.RequestsProcessed) / float64(stats.BatchesProcessed)
		}
	})

	// Remove from active batches
	b.mu.Lock()
	delete(b.activeBatches, batch.ID)
	b.mu.Unlock()

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.inference_batches_processed").Inc()
		b.metrics.Histogram("forge.ai.inference_batch_process_time").Observe(processTime.Seconds())
		b.metrics.Histogram("forge.ai.inference_batch_wait_time").Observe(waitTime.Seconds())
	}

	if b.logger != nil {
		b.logger.Debug("batch processed",
			logger.String("batch_id", batch.ID),
			logger.Int("batch_size", batch.Size),
			logger.Duration("process_time", processTime),
			logger.Duration("wait_time", waitTime),
		)
	}

	return nil
}

// checkBatchTimeouts checks for and handles batch timeouts
func (b *RequestBatcher) checkBatchTimeouts() {
	b.mu.RLock()
	timeoutBatches := make([]*RequestBatch, 0)
	for _, batch := range b.activeBatches {
		if batch.Status == BatchStatusPending && time.Since(batch.CreatedAt) > b.config.MaxBatchAge {
			timeoutBatches = append(timeoutBatches, batch)
		}
	}
	b.mu.RUnlock()

	for _, batch := range timeoutBatches {
		batch.mu.Lock()
		batch.Status = BatchStatusTimeout
		batch.mu.Unlock()

		b.updateStats(func(stats *BatcherStats) {
			stats.BatchesTimeout++
		})

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.inference_batches_timeout").Inc()
		}

		if b.logger != nil {
			b.logger.Warn("batch timeout",
				logger.String("batch_id", batch.ID),
				logger.Duration("age", time.Since(batch.CreatedAt)),
			)
		}
	}
}

// processRemainingBatches processes any remaining batches during shutdown
func (b *RequestBatcher) processRemainingBatches(ctx context.Context) {
	// Process remaining batches in queue
	for {
		select {
		case batch := <-b.batchQueue:
			if err := b.processBatch(ctx, batch); err != nil {
				if b.logger != nil {
					b.logger.Error("failed to process remaining batch",
						logger.String("batch_id", batch.ID),
						logger.Error(err),
					)
				}
			}
		default:
			return
		}
	}
}

// updateStats updates batcher statistics
func (b *RequestBatcher) updateStats(fn func(*BatcherStats)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(&b.stats)
}

// collectStats collects and updates statistics
func (b *RequestBatcher) collectStats() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Update queue size
	b.stats.QueueSize = len(b.requestQueue)

	// Update active batches
	b.stats.ActiveBatches = len(b.activeBatches)

	b.stats.LastUpdated = time.Now()
}

// Dynamic batching strategy implementation

// NewDynamicBatchingStrategy creates a new dynamic batching strategy
func NewDynamicBatchingStrategy(logger common.Logger) *DynamicBatchingStrategy {
	return &DynamicBatchingStrategy{
		name:         "dynamic",
		modelMetrics: make(map[string]*ModelBatchingMetrics),
		logger:       logger,
	}
}

// Name returns the strategy name
func (s *DynamicBatchingStrategy) Name() string {
	return s.name
}

// ShouldBatch determines if a request should be batched
func (s *DynamicBatchingStrategy) ShouldBatch(request InferenceRequest, currentBatch *RequestBatch) bool {
	// Always batch requests for the same model
	return request.ModelID == currentBatch.ModelID
}

// OptimalBatchSize determines the optimal batch size for a model
func (s *DynamicBatchingStrategy) OptimalBatchSize(modelID string, queueSize int) int {
	s.mu.RLock()
	metrics, exists := s.modelMetrics[modelID]
	s.mu.RUnlock()

	if !exists {
		// Default batch size for new models
		return 10
	}

	metrics.mu.RLock()
	optimalSize := metrics.OptimalBatchSize
	metrics.mu.RUnlock()

	// Adjust based on queue size
	if queueSize > optimalSize*2 {
		return optimalSize * 2
	}
	if queueSize < optimalSize/2 {
		return optimalSize / 2
	}

	return optimalSize
}

// BatchTimeout determines the optimal timeout for a batch
func (s *DynamicBatchingStrategy) BatchTimeout(modelID string, batchSize int) time.Duration {
	s.mu.RLock()
	metrics, exists := s.modelMetrics[modelID]
	s.mu.RUnlock()

	if !exists {
		// Default timeout for new models
		return 100 * time.Millisecond
	}

	metrics.mu.RLock()
	avgLatency := metrics.AverageLatency
	metrics.mu.RUnlock()

	// Timeout should be proportional to expected latency
	timeout := avgLatency / 10
	if timeout < 50*time.Millisecond {
		timeout = 50 * time.Millisecond
	}
	if timeout > 500*time.Millisecond {
		timeout = 500 * time.Millisecond
	}

	return timeout
}

// UpdateModelMetrics updates metrics for a model
func (s *DynamicBatchingStrategy) UpdateModelMetrics(modelID string, batchSize int, latency time.Duration, throughput float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	metrics, exists := s.modelMetrics[modelID]
	if !exists {
		metrics = &ModelBatchingMetrics{
			ModelID:            modelID,
			OptimalBatchSize:   batchSize,
			AverageLatency:     latency,
			ThroughputPerBatch: throughput,
			LastOptimization:   time.Now(),
		}
		s.modelMetrics[modelID] = metrics
		return
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	// Update metrics with exponential moving average
	alpha := 0.1
	metrics.AverageLatency = time.Duration(float64(metrics.AverageLatency)*(1-alpha) + float64(latency)*alpha)
	metrics.ThroughputPerBatch = metrics.ThroughputPerBatch*(1-alpha) + throughput*alpha
	metrics.SampleCount++

	// Optimize batch size periodically
	if time.Since(metrics.LastOptimization) > 5*time.Minute && metrics.SampleCount >= 100 {
		s.optimizeBatchSize(metrics)
		metrics.LastOptimization = time.Now()
		metrics.SampleCount = 0
	}
}

// optimizeBatchSize optimizes the batch size for a model
func (s *DynamicBatchingStrategy) optimizeBatchSize(metrics *ModelBatchingMetrics) {
	// Simple optimization: increase batch size if throughput is good, decrease if latency is high
	if metrics.ThroughputPerBatch > 100 && metrics.AverageLatency < 100*time.Millisecond {
		metrics.OptimalBatchSize = int(float64(metrics.OptimalBatchSize) * 1.2)
		if metrics.OptimalBatchSize > 100 {
			metrics.OptimalBatchSize = 100
		}
	} else if metrics.AverageLatency > 500*time.Millisecond {
		metrics.OptimalBatchSize = int(float64(metrics.OptimalBatchSize) * 0.8)
		if metrics.OptimalBatchSize < 1 {
			metrics.OptimalBatchSize = 1
		}
	}
}
