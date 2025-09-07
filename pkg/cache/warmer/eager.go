package warming

import (
	"context"
	"fmt"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// EagerWarmer implements eager cache warming strategy
type EagerWarmer struct {
	cacheManager cachecore.CacheManager
	logger       common.Logger
	metrics      common.Metrics
	config       *EagerWarmingConfig
}

// EagerWarmingConfig contains configuration for eager warming
type EagerWarmingConfig struct {
	MaxConcurrency    int           `yaml:"max_concurrency" json:"max_concurrency" default:"10"`
	BatchSize         int           `yaml:"batch_size" json:"batch_size" default:"100"`
	Timeout           time.Duration `yaml:"timeout" json:"timeout" default:"30s"`
	RetryAttempts     int           `yaml:"retry_attempts" json:"retry_attempts" default:"3"`
	RetryDelay        time.Duration `yaml:"retry_delay" json:"retry_delay" default:"1s"`
	MaxItems          int64         `yaml:"max_items" json:"max_items" default:"0"` // 0 = no limit
	ProgressReporting bool          `yaml:"progress_reporting" json:"progress_reporting" default:"true"`
	FailureThreshold  float64       `yaml:"failure_threshold" json:"failure_threshold" default:"0.1"` // 10%
	EnableMetrics     bool          `yaml:"enable_metrics" json:"enable_metrics" default:"true"`
	WarmingOrder      WarmingOrder  `yaml:"warming_order" json:"warming_order" default:"sequential"`
	ChunkStrategy     ChunkStrategy `yaml:"chunk_strategy" json:"chunk_strategy" default:"size_based"`
}

// WarmingOrder defines the order in which data sources are warmed
type WarmingOrder string

const (
	WarmingOrderSequential WarmingOrder = "sequential"
	WarmingOrderParallel   WarmingOrder = "parallel"
	WarmingOrderPriority   WarmingOrder = "priority"
)

// ChunkStrategy defines how data is chunked for warming
type ChunkStrategy string

const (
	ChunkStrategyNone       ChunkStrategy = "none"
	ChunkStrategySizeBased  ChunkStrategy = "size_based"
	ChunkStrategyTimeBased  ChunkStrategy = "time_based"
	ChunkStrategyMemorySize ChunkStrategy = "memory_size"
)

// EagerWarmingOperation represents an active eager warming operation
type EagerWarmingOperation struct {
	ID           string                    `json:"id"`
	CacheName    string                    `json:"cache_name"`
	Config       EagerWarmingConfig        `json:"config"`
	DataSources  []cachecore.DataSource    `json:"-"`
	Stats        EagerWarmingStats         `json:"stats"`
	WorkerPool   *EagerWorkerPool          `json:"-"`
	Context      context.Context           `json:"-"`
	Cancel       context.CancelFunc        `json:"-"`
	StartedAt    time.Time                 `json:"started_at"`
	UpdatedAt    time.Time                 `json:"updated_at"`
	CompletedAt  *time.Time                `json:"completed_at,omitempty"`
	Status       EagerWarmingStatus        `json:"status"`
	progressChan chan EagerWarmingProgress `json:"-"`
	errorChan    chan error                `json:"-"`
}

// EagerWarmingStats provides detailed statistics for eager warming
type EagerWarmingStats struct {
	TotalItems        int64            `json:"total_items"`
	ProcessedItems    int64            `json:"processed_items"`
	SuccessfulItems   int64            `json:"successful_items"`
	FailedItems       int64            `json:"failed_items"`
	SkippedItems      int64            `json:"skipped_items"`
	Progress          float64          `json:"progress"`
	Throughput        float64          `json:"throughput"` // items per second
	AverageLatency    time.Duration    `json:"average_latency"`
	TotalSize         int64            `json:"total_size"`
	ProcessedSize     int64            `json:"processed_size"`
	ErrorRate         float64          `json:"error_rate"`
	RetryCount        int64            `json:"retry_count"`
	WorkersActive     int              `json:"workers_active"`
	QueueSize         int              `json:"queue_size"`
	DataSourceStats   map[string]int64 `json:"data_source_stats"`
	LastError         string           `json:"last_error,omitempty"`
	EstimatedTimeLeft time.Duration    `json:"estimated_time_left"`
}

// EagerWarmingStatus represents the status of eager warming
type EagerWarmingStatus string

const (
	EagerWarmingStatusIdle      EagerWarmingStatus = "idle"
	EagerWarmingStatusRunning   EagerWarmingStatus = "running"
	EagerWarmingStatusCompleted EagerWarmingStatus = "completed"
	EagerWarmingStatusFailed    EagerWarmingStatus = "failed"
	EagerWarmingStatusCancelled EagerWarmingStatus = "cancelled"
	EagerWarmingStatusPaused    EagerWarmingStatus = "paused"
)

// EagerWarmingProgress represents progress updates
type EagerWarmingProgress struct {
	ItemsProcessed int64         `json:"items_processed"`
	TotalItems     int64         `json:"total_items"`
	Progress       float64       `json:"progress"`
	Throughput     float64       `json:"throughput"`
	TimeElapsed    time.Duration `json:"time_elapsed"`
	EstimatedLeft  time.Duration `json:"estimated_left"`
	CurrentBatch   int           `json:"current_batch"`
	TotalBatches   int           `json:"total_batches"`
}

// EagerWorkerPool manages worker goroutines for eager warming
type EagerWorkerPool struct {
	workers       []*EagerWorker
	workChan      chan cachecore.WarmItem
	resultChan    chan EagerWarmingResult
	doneChan      chan struct{}
	config        *EagerWarmingConfig
	cache         cachecore.Cache
	logger        common.Logger
	metrics       common.Metrics
	mu            sync.RWMutex
	activeWorkers int
}

// EagerWorker represents a single worker goroutine
type EagerWorker struct {
	id         int
	workChan   chan cachecore.WarmItem
	resultChan chan EagerWarmingResult
	doneChan   chan struct{}
	cache      cachecore.Cache
	config     *EagerWarmingConfig
	logger     common.Logger
	metrics    common.Metrics
	stats      EagerWorkerStats
}

// EagerWorkerStats contains statistics for a single worker
type EagerWorkerStats struct {
	ItemsProcessed int64         `json:"items_processed"`
	ItemsSucceeded int64         `json:"items_succeeded"`
	ItemsFailed    int64         `json:"items_failed"`
	TotalLatency   time.Duration `json:"total_latency"`
	AverageLatency time.Duration `json:"average_latency"`
	LastActivity   time.Time     `json:"last_activity"`
	ErrorCount     int64         `json:"error_count"`
	RetryCount     int64         `json:"retry_count"`
}

// EagerWarmingResult represents the result of warming a single item
type EagerWarmingResult struct {
	Item      cachecore.WarmItem `json:"item"`
	Success   bool               `json:"success"`
	Error     error              `json:"error,omitempty"`
	Duration  time.Duration      `json:"duration"`
	WorkerID  int                `json:"worker_id"`
	Timestamp time.Time          `json:"timestamp"`
	Retries   int                `json:"retries"`
	Size      int64              `json:"size"`
}

// NewEagerWarmer creates a new eager warming strategy
func NewEagerWarmer(cacheManager cachecore.CacheManager, logger common.Logger, metrics common.Metrics, config *EagerWarmingConfig) *EagerWarmer {
	if config == nil {
		config = &EagerWarmingConfig{
			MaxConcurrency:    10,
			BatchSize:         100,
			Timeout:           30 * time.Second,
			RetryAttempts:     3,
			RetryDelay:        time.Second,
			ProgressReporting: true,
			FailureThreshold:  0.1,
			EnableMetrics:     true,
			WarmingOrder:      WarmingOrderSequential,
			ChunkStrategy:     ChunkStrategySizeBased,
		}
	}

	return &EagerWarmer{
		cacheManager: cacheManager,
		logger:       logger,
		metrics:      metrics,
		config:       config,
	}
}

// ExecuteWarming executes eager warming for a cache
func (ew *EagerWarmer) ExecuteWarming(ctx context.Context, cacheName string, dataSources []cachecore.DataSource, config cachecore.WarmConfig) (*EagerWarmingOperation, error) {
	// Get cache instance
	cache, err := ew.cacheManager.GetCache(cacheName)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache %s: %w", cacheName, err)
	}

	// Create operation context
	operationCtx, cancel := context.WithCancel(ctx)
	if config.Timeout > 0 {
		operationCtx, cancel = context.WithTimeout(ctx, config.Timeout)
	}

	// Create operation
	operation := &EagerWarmingOperation{
		ID:          fmt.Sprintf("eager_%s_%d", cacheName, time.Now().UnixNano()),
		CacheName:   cacheName,
		Config:      *ew.config,
		DataSources: dataSources,
		Context:     operationCtx,
		Cancel:      cancel,
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Status:      EagerWarmingStatusRunning,
		Stats: EagerWarmingStats{
			DataSourceStats: make(map[string]int64),
		},
		progressChan: make(chan EagerWarmingProgress, 100),
		errorChan:    make(chan error, 100),
	}

	// Apply configuration overrides
	if config.Concurrency > 0 {
		operation.Config.MaxConcurrency = config.Concurrency
	}
	if config.BatchSize > 0 {
		operation.Config.BatchSize = config.BatchSize
	}

	// Calculate total items
	var totalItems int64
	for _, source := range dataSources {
		count, err := source.GetDataCount(operationCtx, config.Filter)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to get count from %s: %w", source.Name(), err)
		}
		totalItems += count
		operation.Stats.DataSourceStats[source.Name()] = count
	}

	if config.MaxItems > 0 && totalItems > config.MaxItems {
		totalItems = config.MaxItems
	}

	operation.Stats.TotalItems = totalItems

	// Create worker pool
	operation.WorkerPool = ew.createWorkerPool(cache, operation.Config)

	// Start the warming process
	go ew.executeEagerWarming(operation, dataSources, config)

	return operation, nil
}

// executeEagerWarming executes the actual eager warming process
func (ew *EagerWarmer) executeEagerWarming(operation *EagerWarmingOperation, dataSources []cachecore.DataSource, config cachecore.WarmConfig) {
	defer func() {
		if r := recover(); r != nil {
			operation.Status = EagerWarmingStatusFailed
			operation.Stats.LastError = fmt.Sprintf("panic: %v", r)
			completedAt := time.Now()
			operation.CompletedAt = &completedAt

			if ew.logger != nil {
				ew.logger.Error("eager warming panic",
					logger.String("operation_id", operation.ID),
					logger.Any("panic", r),
				)
			}
		}
	}()

	ctx := operation.Context
	startTime := time.Now()

	// Start worker pool
	if err := operation.WorkerPool.Start(ctx); err != nil {
		operation.Status = EagerWarmingStatusFailed
		operation.Stats.LastError = fmt.Sprintf("failed to start worker pool: %v", err)
		return
	}

	defer operation.WorkerPool.Stop()

	// Start result collector
	go ew.collectResults(operation)

	// Process data sources based on warming order
	switch operation.Config.WarmingOrder {
	case WarmingOrderSequential:
		ew.processDataSourcesSequentially(operation, dataSources, config)
	case WarmingOrderParallel:
		ew.processDataSourcesParallel(operation, dataSources, config)
	case WarmingOrderPriority:
		ew.processDataSourcesByPriority(operation, dataSources, config)
	default:
		ew.processDataSourcesSequentially(operation, dataSources, config)
	}

	// Wait for all workers to complete
	operation.WorkerPool.WaitForCompletion()

	// Update final statistics
	operation.Status = EagerWarmingStatusCompleted
	if operation.Stats.ErrorRate > operation.Config.FailureThreshold {
		operation.Status = EagerWarmingStatusFailed
	}

	completedAt := time.Now()
	operation.CompletedAt = &completedAt
	operation.UpdatedAt = completedAt

	duration := completedAt.Sub(startTime)
	if operation.Stats.ProcessedItems > 0 {
		operation.Stats.Throughput = float64(operation.Stats.ProcessedItems) / duration.Seconds()
		operation.Stats.AverageLatency = duration / time.Duration(operation.Stats.ProcessedItems)
	}

	if operation.Stats.TotalItems > 0 {
		operation.Stats.Progress = float64(operation.Stats.ProcessedItems) / float64(operation.Stats.TotalItems)
		operation.Stats.ErrorRate = float64(operation.Stats.FailedItems) / float64(operation.Stats.TotalItems)
	}

	if ew.logger != nil {
		ew.logger.Info("eager warming completed",
			logger.String("operation_id", operation.ID),
			logger.String("cache_name", operation.CacheName),
			logger.Int64("processed_items", operation.Stats.ProcessedItems),
			logger.Int64("successful_items", operation.Stats.SuccessfulItems),
			logger.Int64("failed_items", operation.Stats.FailedItems),
			logger.Float64("throughput", operation.Stats.Throughput),
			logger.Duration("duration", duration),
		)
	}

	if ew.metrics != nil {
		ew.metrics.Counter("forge.cache.warming.eager.completed").Inc()
		ew.metrics.Histogram("forge.cache.warming.eager.duration").Observe(duration.Seconds())
		ew.metrics.Histogram("forge.cache.warming.eager.throughput").Observe(operation.Stats.Throughput)
	}
}

// processDataSourcesSequentially processes data sources one by one
func (ew *EagerWarmer) processDataSourcesSequentially(operation *EagerWarmingOperation, dataSources []cachecore.DataSource, config cachecore.WarmConfig) {
	ctx := operation.Context

	for _, source := range dataSources {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := ew.processDataSource(operation, source, config); err != nil {
			operation.Stats.LastError = err.Error()
			if ew.logger != nil {
				ew.logger.Error("failed to process data source",
					logger.String("source", source.Name()),
					logger.Error(err),
				)
			}
		}
	}
}

// processDataSourcesParallel processes all data sources in parallel
func (ew *EagerWarmer) processDataSourcesParallel(operation *EagerWarmingOperation, dataSources []cachecore.DataSource, config cachecore.WarmConfig) {
	ctx := operation.Context
	var wg sync.WaitGroup

	for _, source := range dataSources {
		wg.Add(1)
		go func(src cachecore.DataSource) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			default:
			}

			if err := ew.processDataSource(operation, src, config); err != nil {
				operation.Stats.LastError = err.Error()
				if ew.logger != nil {
					ew.logger.Error("failed to process data source",
						logger.String("source", src.Name()),
						logger.Error(err),
					)
				}
			}
		}(source)
	}

	wg.Wait()
}

// processDataSourcesByPriority processes data sources ordered by priority
func (ew *EagerWarmer) processDataSourcesByPriority(operation *EagerWarmingOperation, dataSources []cachecore.DataSource, config cachecore.WarmConfig) {
	// For now, this is the same as sequential, but could be enhanced with priority metadata
	ew.processDataSourcesSequentially(operation, dataSources, config)
}

// processDataSource processes a single data source
func (ew *EagerWarmer) processDataSource(operation *EagerWarmingOperation, source cachecore.DataSource, config cachecore.WarmConfig) error {
	ctx := operation.Context
	var offset int64 = 0
	batchSize := int64(operation.Config.BatchSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check max items limit
		if config.MaxItems > 0 && operation.Stats.ProcessedItems >= config.MaxItems {
			break
		}

		// Adjust batch size for max items limit
		if config.MaxItems > 0 && operation.Stats.ProcessedItems+batchSize > config.MaxItems {
			batchSize = config.MaxItems - operation.Stats.ProcessedItems
		}

		// Get batch of items
		items, err := source.GetDataBatch(ctx, config.Filter, offset, batchSize)
		if err != nil {
			return fmt.Errorf("failed to get batch from %s: %w", source.Name(), err)
		}

		if len(items) == 0 {
			break
		}

		// Send items to worker pool
		for _, item := range items {
			select {
			case operation.WorkerPool.workChan <- item:
				// Item sent successfully
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		offset += batchSize
	}

	return nil
}

// collectResults collects results from workers
func (ew *EagerWarmer) collectResults(operation *EagerWarmingOperation) {
	ctx := operation.Context
	lastProgressReport := time.Now()
	progressInterval := 5 * time.Second

	for {
		select {
		case result := <-operation.WorkerPool.resultChan:
			operation.Stats.ProcessedItems++
			operation.Stats.ProcessedSize += result.Size

			if result.Success {
				operation.Stats.SuccessfulItems++
			} else {
				operation.Stats.FailedItems++
				if result.Error != nil {
					operation.Stats.LastError = result.Error.Error()
				}
			}

			// Update progress reporting
			if operation.Config.ProgressReporting && time.Since(lastProgressReport) >= progressInterval {
				ew.reportProgress(operation)
				lastProgressReport = time.Now()
			}

		case <-ctx.Done():
			return
		}

		// Check if we've processed all items
		if operation.Stats.ProcessedItems >= operation.Stats.TotalItems {
			return
		}
	}
}

// reportProgress reports warming progress
func (ew *EagerWarmer) reportProgress(operation *EagerWarmingOperation) {
	now := time.Now()
	elapsed := now.Sub(operation.StartedAt)

	var progress float64
	var estimatedLeft time.Duration

	if operation.Stats.TotalItems > 0 {
		progress = float64(operation.Stats.ProcessedItems) / float64(operation.Stats.TotalItems)
		if progress > 0 {
			totalEstimated := time.Duration(float64(elapsed) / progress)
			estimatedLeft = totalEstimated - elapsed
		}
	}

	var throughput float64
	if elapsed.Seconds() > 0 {
		throughput = float64(operation.Stats.ProcessedItems) / elapsed.Seconds()
	}

	progressReport := EagerWarmingProgress{
		ItemsProcessed: operation.Stats.ProcessedItems,
		TotalItems:     operation.Stats.TotalItems,
		Progress:       progress,
		Throughput:     throughput,
		TimeElapsed:    elapsed,
		EstimatedLeft:  estimatedLeft,
	}

	operation.Stats.Progress = progress
	operation.Stats.Throughput = throughput
	operation.Stats.EstimatedTimeLeft = estimatedLeft
	operation.UpdatedAt = now

	// Send progress update
	select {
	case operation.progressChan <- progressReport:
	default:
		// Channel is full, skip this update
	}

	if ew.metrics != nil && operation.Config.EnableMetrics {
		ew.metrics.Gauge("forge.cache.warming.eager.progress", "cache", operation.CacheName).Set(progress)
		ew.metrics.Gauge("forge.cache.warming.eager.throughput", "cache", operation.CacheName).Set(throughput)
		ew.metrics.Gauge("forge.cache.warming.eager.items_processed", "cache", operation.CacheName).Set(float64(operation.Stats.ProcessedItems))
	}
}

// createWorkerPool creates a worker pool for eager warming
func (ew *EagerWarmer) createWorkerPool(cache cachecore.Cache, config EagerWarmingConfig) *EagerWorkerPool {
	workChan := make(chan cachecore.WarmItem, config.BatchSize*2)
	resultChan := make(chan EagerWarmingResult, config.BatchSize*2)
	doneChan := make(chan struct{})

	pool := &EagerWorkerPool{
		workers:    make([]*EagerWorker, config.MaxConcurrency),
		workChan:   workChan,
		resultChan: resultChan,
		doneChan:   doneChan,
		config:     &config,
		cache:      cache,
		logger:     ew.logger,
		metrics:    ew.metrics,
	}

	// Create workers
	for i := 0; i < config.MaxConcurrency; i++ {
		pool.workers[i] = &EagerWorker{
			id:         i,
			workChan:   workChan,
			resultChan: resultChan,
			doneChan:   doneChan,
			cache:      cache,
			config:     &config,
			logger:     ew.logger,
			metrics:    ew.metrics,
		}
	}

	return pool
}

// Start starts the worker pool
func (ewp *EagerWorkerPool) Start(ctx context.Context) error {
	ewp.mu.Lock()
	defer ewp.mu.Unlock()

	for _, worker := range ewp.workers {
		go worker.start(ctx)
		ewp.activeWorkers++
	}

	return nil
}

// Stop stops the worker pool
func (ewp *EagerWorkerPool) Stop() {
	close(ewp.workChan)
	close(ewp.doneChan)
}

// WaitForCompletion waits for all workers to complete
func (ewp *EagerWorkerPool) WaitForCompletion() {
	// Wait for all work items to be processed
	// This is a simplified implementation
	for ewp.activeWorkers > 0 {
		time.Sleep(100 * time.Millisecond)
	}
}

// start starts a worker
func (ew *EagerWorker) start(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if ew.logger != nil {
				ew.logger.Error("eager worker panic",
					logger.Int("worker_id", ew.id),
					logger.Any("panic", r),
				)
			}
		}
	}()

	for {
		select {
		case item, ok := <-ew.workChan:
			if !ok {
				return
			}
			ew.processItem(ctx, item)

		case <-ew.doneChan:
			return

		case <-ctx.Done():
			return
		}
	}
}

// processItem processes a single warming item
func (ew *EagerWorker) processItem(ctx context.Context, item cachecore.WarmItem) {
	startTime := time.Now()
	ew.stats.LastActivity = startTime

	var result EagerWarmingResult
	var err error

	// Apply configuration overrides
	ttl := item.TTL
	if ttl == 0 && ew.config.Timeout > 0 {
		ttl = ew.config.Timeout
	}

	// Retry logic
	for attempt := 0; attempt <= ew.config.RetryAttempts; attempt++ {
		err = ew.cache.Set(ctx, item.Key, item.Value, ttl)
		if err == nil {
			break
		}

		if attempt < ew.config.RetryAttempts {
			time.Sleep(ew.config.RetryDelay)
			ew.stats.RetryCount++
		}
	}

	duration := time.Since(startTime)
	ew.stats.ItemsProcessed++
	ew.stats.TotalLatency += duration
	ew.stats.AverageLatency = ew.stats.TotalLatency / time.Duration(ew.stats.ItemsProcessed)

	if err == nil {
		ew.stats.ItemsSucceeded++
		result = EagerWarmingResult{
			Item:      item,
			Success:   true,
			Duration:  duration,
			WorkerID:  ew.id,
			Timestamp: startTime,
			Retries:   ew.config.RetryAttempts - (ew.config.RetryAttempts - len(fmt.Sprintf("%d", ew.stats.RetryCount))),
			Size:      item.Size,
		}
	} else {
		ew.stats.ItemsFailed++
		ew.stats.ErrorCount++
		result = EagerWarmingResult{
			Item:      item,
			Success:   false,
			Error:     err,
			Duration:  duration,
			WorkerID:  ew.id,
			Timestamp: startTime,
			Size:      item.Size,
		}
	}

	// Send result
	select {
	case ew.resultChan <- result:
	default:
		// Channel is full, drop the result
	}

	if ew.metrics != nil {
		ew.metrics.Counter("forge.cache.warming.eager.items_processed", "worker", fmt.Sprintf("%d", ew.id)).Inc()
		ew.metrics.Histogram("forge.cache.warming.eager.item_duration", "worker", fmt.Sprintf("%d", ew.id)).Observe(duration.Seconds())

		if err == nil {
			ew.metrics.Counter("forge.cache.warming.eager.items_succeeded", "worker", fmt.Sprintf("%d", ew.id)).Inc()
		} else {
			ew.metrics.Counter("forge.cache.warming.eager.items_failed", "worker", fmt.Sprintf("%d", ew.id)).Inc()
		}
	}
}
