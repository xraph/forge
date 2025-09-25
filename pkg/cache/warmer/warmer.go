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

// DefaultCacheWarmer implements the CacheWarmer interface
type DefaultCacheWarmer struct {
	cacheManager    cachecore.CacheManager
	dataSources     map[string]cachecore.DataSource
	operations      map[string]*cachecore.WarmingOperation
	scheduledWarmer *ScheduledWarmer
	eagerWarmer     *EagerWarmer
	lazyWarmer      *LazyWarmer
	logger          common.Logger
	metrics         common.Metrics
	mu              sync.RWMutex
	config          *WarmerConfig
}

// WarmerConfig contains configuration for the cache warmer
type WarmerConfig struct {
	MaxConcurrentOperations int           `yaml:"max_concurrent_operations" json:"max_concurrent_operations" default:"5"`
	DefaultTimeout          time.Duration `yaml:"default_timeout" json:"default_timeout" default:"30m"`
	DefaultConcurrency      int           `yaml:"default_concurrency" json:"default_concurrency" default:"10"`
	DefaultBatchSize        int           `yaml:"default_batch_size" json:"default_batch_size" default:"100"`
	DefaultRetryAttempts    int           `yaml:"default_retry_attempts" json:"default_retry_attempts" default:"3"`
	DefaultRetryDelay       time.Duration `yaml:"default_retry_delay" json:"default_retry_delay" default:"5s"`
	EnableMetrics           bool          `yaml:"enable_metrics" json:"enable_metrics" default:"true"`
	EnableScheduling        bool          `yaml:"enable_scheduling" json:"enable_scheduling" default:"true"`
	EnableLazyWarming       bool          `yaml:"enable_lazy_warming" json:"enable_lazy_warming" default:"false"`
	CleanupInterval         time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" default:"1h"`

	// Strategy-specific configurations
	EagerConfig     *EagerWarmingConfig     `yaml:"eager_config" json:"eager_config"`
	LazyConfig      *LazyWarmingConfig      `yaml:"lazy_config" json:"lazy_config"`
	ScheduledConfig *ScheduledWarmingConfig `yaml:"scheduled_config" json:"scheduled_config"`
}

// NewCacheWarmer creates a new cache warmer
func NewCacheWarmer(cacheManager cachecore.CacheManager, logger common.Logger, metrics common.Metrics, config *WarmerConfig) *DefaultCacheWarmer {
	if config == nil {
		config = &WarmerConfig{
			MaxConcurrentOperations: 5,
			DefaultTimeout:          30 * time.Minute,
			DefaultConcurrency:      10,
			DefaultBatchSize:        100,
			DefaultRetryAttempts:    3,
			DefaultRetryDelay:       5 * time.Second,
			EnableMetrics:           true,
			EnableScheduling:        true,
			EnableLazyWarming:       false,
			CleanupInterval:         time.Hour,
		}
	}

	warmer := &DefaultCacheWarmer{
		cacheManager: cacheManager,
		dataSources:  make(map[string]cachecore.DataSource),
		operations:   make(map[string]*cachecore.WarmingOperation),
		logger:       logger,
		metrics:      metrics,
		config:       config,
	}

	// Initialize warming strategies
	warmer.eagerWarmer = NewEagerWarmer(cacheManager, logger, metrics, config.EagerConfig)

	if config.EnableLazyWarming {
		warmer.lazyWarmer = NewLazyWarmer(cacheManager, logger, metrics, config.LazyConfig)
	}

	if config.EnableScheduling {
		warmer.scheduledWarmer = NewScheduledWarmer(cacheManager, logger, metrics, config.ScheduledConfig)
	}

	// OnStart cleanup goroutine
	go warmer.cleanupWorker()

	return warmer
}

// Start starts the cache warmer and its components
func (w *DefaultCacheWarmer) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// OnStart scheduled warmer if enabled
	if w.scheduledWarmer != nil {
		if err := w.scheduledWarmer.Start(ctx); err != nil {
			return fmt.Errorf("failed to start scheduled warmer: %w", err)
		}
	}

	if w.logger != nil {
		w.logger.Info("cache warmer started")
	}

	return nil
}

// Stop stops the cache warmer and its components
func (w *DefaultCacheWarmer) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// OnStop scheduled warmer
	if w.scheduledWarmer != nil {
		if err := w.scheduledWarmer.Stop(); err != nil {
			if w.logger != nil {
				w.logger.Error("error stopping scheduled warmer", logger.Error(err))
			}
		}
	}

	// Cancel all active operations
	for _, operation := range w.operations {
		operation.Cancel()
	}

	if w.logger != nil {
		w.logger.Info("cache warmer stopped")
	}

	return nil
}

// WarmCache warms a cache with predefined data
func (w *DefaultCacheWarmer) WarmCache(ctx context.Context, cacheName string, config cachecore.WarmConfig) error {
	w.mu.Lock()

	// Check if already warming this cache
	if _, exists := w.operations[cacheName]; exists {
		w.mu.Unlock()
		return fmt.Errorf("cache %s is already being warmed", cacheName)
	}

	// Check concurrent operation limit
	if len(w.operations) >= w.config.MaxConcurrentOperations {
		w.mu.Unlock()
		return fmt.Errorf("maximum concurrent warming operations reached (%d)", w.config.MaxConcurrentOperations)
	}

	// Get cache instance
	cache, err := w.cacheManager.GetCache(cacheName)
	if err != nil {
		w.mu.Unlock()
		return fmt.Errorf("failed to get cache %s: %w", cacheName, err)
	}

	// Apply defaults
	w.applyDefaults(&config)

	// Create warming operation
	operationCtx, cancel := context.WithCancel(ctx)
	if config.Timeout > 0 {
		operationCtx, cancel = context.WithTimeout(ctx, config.Timeout)
	}

	operation := &cachecore.WarmingOperation{
		ID:        fmt.Sprintf("%s_%d", cacheName, time.Now().UnixNano()),
		CacheName: cacheName,
		Config:    config,
		Context:   operationCtx,
		Cancel:    cancel,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
		Stats: cachecore.WarmStats{
			CacheName:   cacheName,
			Strategy:    config.Strategy,
			Status:      cachecore.WarmStatusRunning,
			StartTime:   time.Now(),
			DataSources: config.DataSources,
		},
	}

	w.operations[cacheName] = operation
	w.mu.Unlock()

	// OnStart warming in a goroutine
	go w.executeWarming(operation, cache)

	return nil
}

// GetWarmStats returns warming statistics for a cache
func (w *DefaultCacheWarmer) GetWarmStats(cacheName string) (cachecore.WarmStats, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	operation, exists := w.operations[cacheName]
	if !exists {
		return cachecore.WarmStats{}, fmt.Errorf("no warming operation found for cache %s", cacheName)
	}

	return operation.Stats, nil
}

// ScheduleWarming schedules automatic cache warming
func (w *DefaultCacheWarmer) ScheduleWarming(cacheName string, config cachecore.WarmConfig) error {
	if !w.config.EnableScheduling || w.scheduledWarmer == nil {
		return fmt.Errorf("scheduling is not enabled")
	}

	if config.Schedule == "" {
		return fmt.Errorf("schedule expression is required")
	}

	return w.scheduledWarmer.Schedule(cacheName, config)
}

// CancelWarming cancels scheduled warming for a cache
func (w *DefaultCacheWarmer) CancelWarming(cacheName string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Cancel active operation
	if operation, exists := w.operations[cacheName]; exists {
		operation.Cancel()
		operation.Stats.Status = cachecore.WarmStatusCancelled
		endTime := time.Now()
		operation.Stats.EndTime = &endTime
		operation.UpdatedAt = time.Now()
	}

	// Cancel scheduled warming
	if w.scheduledWarmer != nil {
		if err := w.scheduledWarmer.Cancel(cacheName); err != nil {
			// Log but don't fail if no scheduled job exists
			if w.logger != nil {
				w.logger.Debug("no scheduled warming job to cancel",
					logger.String("cache_name", cacheName))
			}
		}
	}

	return nil
}

// GetActiveWarmings returns currently active warming operations
func (w *DefaultCacheWarmer) GetActiveWarmings() []cachecore.WarmingOperation {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var operations []cachecore.WarmingOperation
	for _, op := range w.operations {
		if op.Stats.Status == cachecore.WarmStatusRunning {
			operations = append(operations, *op)
		}
	}

	return operations
}

// RegisterDataSource registers a data source for warming
func (w *DefaultCacheWarmer) RegisterDataSource(source cachecore.DataSource) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	name := source.Name()
	if _, exists := w.dataSources[name]; exists {
		return fmt.Errorf("data source %s is already registered", name)
	}

	w.dataSources[name] = source

	if w.logger != nil {
		w.logger.Info("data source registered",
			logger.String("name", name))
	}

	return nil
}

// UnregisterDataSource unregisters a data source
func (w *DefaultCacheWarmer) UnregisterDataSource(name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.dataSources[name]; !exists {
		return fmt.Errorf("data source %s is not registered", name)
	}

	delete(w.dataSources, name)

	if w.logger != nil {
		w.logger.Info("data source unregistered",
			logger.String("name", name))
	}

	return nil
}

// GetDataSources returns all registered data sources
func (w *DefaultCacheWarmer) GetDataSources() []cachecore.DataSource {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var sources []cachecore.DataSource
	for _, source := range w.dataSources {
		sources = append(sources, source)
	}

	return sources
}

// executeWarming executes the warming operation
func (w *DefaultCacheWarmer) executeWarming(operation *cachecore.WarmingOperation, cache cachecore.Cache) {
	defer func() {
		if r := recover(); r != nil {
			w.mu.Lock()
			operation.Stats.Status = cachecore.WarmStatusFailed
			operation.Stats.LastError = fmt.Sprintf("panic: %v", r)
			endTime := time.Now()
			operation.Stats.EndTime = &endTime
			operation.Stats.Duration = time.Since(operation.Stats.StartTime)
			operation.UpdatedAt = time.Now()
			w.mu.Unlock()

			if w.logger != nil {
				w.logger.Error("warming operation panic",
					logger.String("operation_id", operation.ID),
					logger.Any("panic", r),
				)
			}
		}
	}()

	ctx := operation.Context
	config := operation.Config

	// Get data sources
	var dataSources []cachecore.DataSource
	for _, sourceName := range config.DataSources {
		w.mu.RLock()
		source, exists := w.dataSources[sourceName]
		w.mu.RUnlock()

		if !exists {
			w.mu.Lock()
			operation.Stats.Status = cachecore.WarmStatusFailed
			operation.Stats.LastError = fmt.Sprintf("data source %s not found", sourceName)
			endTime := time.Now()
			operation.Stats.EndTime = &endTime
			operation.Stats.Duration = time.Since(operation.Stats.StartTime)
			operation.UpdatedAt = time.Now()
			w.mu.Unlock()
			return
		}

		dataSources = append(dataSources, source)
	}

	// Calculate total items
	var totalItems int64
	for _, source := range dataSources {
		count, err := source.GetDataCount(ctx, config.Filter)
		if err != nil {
			if config.FailureMode == cachecore.FailureModeAbort {
				w.mu.Lock()
				operation.Stats.Status = cachecore.WarmStatusFailed
				operation.Stats.LastError = fmt.Sprintf("failed to get count from %s: %v", source.Name(), err)
				endTime := time.Now()
				operation.Stats.EndTime = &endTime
				operation.Stats.Duration = time.Since(operation.Stats.StartTime)
				operation.UpdatedAt = time.Now()
				w.mu.Unlock()
				return
			}
			continue
		}
		totalItems += count
	}

	if config.MaxItems > 0 && totalItems > config.MaxItems {
		totalItems = config.MaxItems
	}

	w.mu.Lock()
	operation.Stats.ItemsTotal = totalItems
	operation.UpdatedAt = time.Now()
	w.mu.Unlock()

	// Execute warming based on strategy
	switch config.Strategy {
	case cachecore.WarmStrategyEager:
		w.executeEagerWarming(ctx, operation, cache, dataSources)
	case cachecore.WarmStrategyLazy:
		w.executeLazyWarming(ctx, operation, cache, dataSources)
	case cachecore.WarmStrategyOnDemand:
		w.executeOnDemandWarming(ctx, operation, cache, dataSources)
	case cachecore.WarmStrategyPredictive:
		w.executePredictiveWarming(ctx, operation, cache, dataSources)
	default:
		w.executeEagerWarming(ctx, operation, cache, dataSources)
	}

	// Update final stats
	w.mu.Lock()
	if operation.Stats.Status == cachecore.WarmStatusRunning {
		operation.Stats.Status = cachecore.WarmStatusCompleted
	}
	endTime := time.Now()
	operation.Stats.EndTime = &endTime
	operation.Stats.Duration = time.Since(operation.Stats.StartTime)
	if operation.Stats.ItemsWarmed > 0 {
		operation.Stats.Throughput = float64(operation.Stats.ItemsWarmed) / operation.Stats.Duration.Seconds()
	}
	if operation.Stats.ItemsTotal > 0 {
		operation.Stats.Progress = float64(operation.Stats.ItemsWarmed) / float64(operation.Stats.ItemsTotal)
		operation.Stats.ErrorRate = float64(operation.Stats.ItemsFailed) / float64(operation.Stats.ItemsTotal)
	}
	operation.UpdatedAt = time.Now()
	w.mu.Unlock()

	if w.logger != nil {
		w.logger.Info("warming operation completed",
			logger.String("operation_id", operation.ID),
			logger.String("cache_name", operation.CacheName),
			logger.String("strategy", string(config.Strategy)),
			logger.Int64("items_warmed", operation.Stats.ItemsWarmed),
			logger.Duration("duration", operation.Stats.Duration),
		)
	}
}

// executeEagerWarming executes eager warming strategy
func (w *DefaultCacheWarmer) executeEagerWarming(ctx context.Context, operation *cachecore.WarmingOperation, cache cachecore.Cache, dataSources []cachecore.DataSource) {
	if w.eagerWarmer == nil {
		w.mu.Lock()
		operation.Stats.Status = cachecore.WarmStatusFailed
		operation.Stats.LastError = "eager warmer not initialized"
		w.mu.Unlock()
		return
	}

	// Execute using eager warmer
	eagerOperation, err := w.eagerWarmer.ExecuteWarming(ctx, operation.CacheName, dataSources, operation.Config)
	if err != nil {
		w.mu.Lock()
		operation.Stats.Status = cachecore.WarmStatusFailed
		operation.Stats.LastError = fmt.Sprintf("eager warming failed: %v", err)
		w.mu.Unlock()
		return
	}

	// Monitor progress
	go w.monitorEagerOperation(operation, eagerOperation)

	// Wait for completion
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
			if eagerOperation.Status == EagerWarmingStatusCompleted ||
				eagerOperation.Status == EagerWarmingStatusFailed ||
				eagerOperation.Status == EagerWarmingStatusCancelled {

				// Update final stats
				w.mu.Lock()
				operation.Stats.ItemsWarmed = eagerOperation.Stats.SuccessfulItems
				operation.Stats.ItemsFailed = eagerOperation.Stats.FailedItems
				operation.Stats.Progress = eagerOperation.Stats.Progress
				operation.Stats.Throughput = eagerOperation.Stats.Throughput
				operation.Stats.ErrorRate = eagerOperation.Stats.ErrorRate
				if eagerOperation.Status == EagerWarmingStatusFailed {
					operation.Stats.Status = cachecore.WarmStatusFailed
					operation.Stats.LastError = eagerOperation.Stats.LastError
				}
				w.mu.Unlock()

				return
			}
		}
	}
}

// monitorEagerOperation monitors an eager warming operation
func (w *DefaultCacheWarmer) monitorEagerOperation(operation *cachecore.WarmingOperation, eagerOperation *EagerWarmingOperation) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.mu.Lock()
			operation.Stats.ItemsWarmed = eagerOperation.Stats.SuccessfulItems
			operation.Stats.ItemsFailed = eagerOperation.Stats.FailedItems
			operation.Stats.Progress = eagerOperation.Stats.Progress
			operation.Stats.Throughput = eagerOperation.Stats.Throughput
			operation.UpdatedAt = time.Now()
			w.mu.Unlock()

		case <-operation.Context.Done():
			return
		}
	}
}

// executeLazyWarming executes lazy warming strategy
func (w *DefaultCacheWarmer) executeLazyWarming(ctx context.Context, operation *cachecore.WarmingOperation, cache cachecore.Cache, dataSources []cachecore.DataSource) {
	if w.lazyWarmer == nil {
		// Fallback to reduced eager warming
		operation.Config.Concurrency = 1
		operation.Config.BatchSize = 10
		w.executeEagerWarming(ctx, operation, cache, dataSources)
		return
	}

	// OnStart lazy warming
	lazyOperation, err := w.lazyWarmer.StartLazyWarming(ctx, operation.CacheName, dataSources, operation.Config)
	if err != nil {
		w.mu.Lock()
		operation.Stats.Status = cachecore.WarmStatusFailed
		operation.Stats.LastError = fmt.Sprintf("lazy warming failed: %v", err)
		w.mu.Unlock()
		return
	}

	// Monitor lazy warming (simplified)
	w.mu.Lock()
	operation.Stats.Status = cachecore.WarmStatusRunning
	w.mu.Unlock()

	// Lazy warming continues in the background
	// This is just a placeholder implementation
	_ = lazyOperation
}

// executeOnDemandWarming executes on-demand warming strategy
func (w *DefaultCacheWarmer) executeOnDemandWarming(ctx context.Context, operation *cachecore.WarmingOperation, cache cachecore.Cache, dataSources []cachecore.DataSource) {
	// On-demand warming would be triggered by cache misses
	// For now, implement as reduced eager warming
	operation.Config.Concurrency = 2
	operation.Config.BatchSize = 20
	w.executeEagerWarming(ctx, operation, cache, dataSources)
}

// executePredictiveWarming executes predictive warming strategy
func (w *DefaultCacheWarmer) executePredictiveWarming(ctx context.Context, operation *cachecore.WarmingOperation, cache cachecore.Cache, dataSources []cachecore.DataSource) {
	// Predictive warming would use ML/analytics to predict what to warm
	// For now, implement as standard eager warming
	w.executeEagerWarming(ctx, operation, cache, dataSources)
}

// applyDefaults applies default configuration values
func (w *DefaultCacheWarmer) applyDefaults(config *cachecore.WarmConfig) {
	if config.Concurrency == 0 {
		config.Concurrency = w.config.DefaultConcurrency
	}
	if config.BatchSize == 0 {
		config.BatchSize = w.config.DefaultBatchSize
	}
	if config.Timeout == 0 {
		config.Timeout = w.config.DefaultTimeout
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = w.config.DefaultRetryAttempts
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = w.config.DefaultRetryDelay
	}
	if config.FailureMode == "" {
		config.FailureMode = cachecore.FailureModeLog
	}
}

// cleanupWorker removes completed operations periodically
func (w *DefaultCacheWarmer) cleanupWorker() {
	ticker := time.NewTicker(w.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		w.mu.Lock()
		now := time.Now()
		for cacheName, operation := range w.operations {
			// Remove operations that completed more than an hour ago
			if operation.Stats.Status != cachecore.WarmStatusRunning &&
				operation.Stats.EndTime != nil &&
				now.Sub(*operation.Stats.EndTime) > time.Hour {
				delete(w.operations, cacheName)
			}
		}
		w.mu.Unlock()
	}
}
