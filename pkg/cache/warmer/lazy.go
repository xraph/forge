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

// LazyWarmer implements lazy cache warming strategy
type LazyWarmer struct {
	cacheManager  cachecore.CacheManager
	logger        common.Logger
	metrics       common.Metrics
	config        *LazyWarmingConfig
	operations    map[string]*LazyWarmingOperation
	accessTracker *AccessTracker
	mu            sync.RWMutex
}

// LazyWarmingConfig contains configuration for lazy warming
type LazyWarmingConfig struct {
	AccessThreshold       int           `yaml:"access_threshold" json:"access_threshold" default:"3"`         // Minimum accesses before warming
	TimeWindow            time.Duration `yaml:"time_window" json:"time_window" default:"1h"`                  // Time window for access tracking
	WarmingDelay          time.Duration `yaml:"warming_delay" json:"warming_delay" default:"5m"`              // Delay before warming after threshold
	MaxConcurrency        int           `yaml:"max_concurrency" json:"max_concurrency" default:"5"`           // Lower than eager
	BatchSize             int           `yaml:"batch_size" json:"batch_size" default:"50"`                    // Smaller batches
	PredictionWindow      time.Duration `yaml:"prediction_window" json:"prediction_window" default:"30m"`     // How far ahead to predict
	WarmingProbability    float64       `yaml:"warming_probability" json:"warming_probability" default:"0.7"` // Probability threshold for warming
	MaxPendingItems       int           `yaml:"max_pending_items" json:"max_pending_items" default:"1000"`    // Max items in warming queue
	EnablePredictive      bool          `yaml:"enable_predictive" json:"enable_predictive" default:"true"`    // Enable predictive warming
	AccessPatternAnalysis bool          `yaml:"access_pattern_analysis" json:"access_pattern_analysis" default:"true"`
	CleanupInterval       time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" default:"1h"` // Cleanup old tracking data
	MemoryLimit           int64         `yaml:"memory_limit" json:"memory_limit" default:"104857600"`  // 100MB limit for tracking
}

// LazyWarmingOperation represents a lazy warming operation
type LazyWarmingOperation struct {
	ID               string                  `json:"id"`
	CacheName        string                  `json:"cache_name"`
	Config           LazyWarmingConfig       `json:"config"`
	Stats            LazyWarmingStats        `json:"stats"`
	PendingItems     map[string]*PendingItem `json:"-"`
	WarmingQueue     chan WarmingRequest     `json:"-"`
	Context          context.Context         `json:"-"`
	Cancel           context.CancelFunc      `json:"-"`
	StartedAt        time.Time               `json:"started_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	Status           LazyWarmingStatus       `json:"status"`
	AccessTracker    *AccessTracker          `json:"-"`
	PredictionEngine *LazyPredictionEngine   `json:"-"`
	Workers          []*LazyWorker           `json:"-"`
	mu               sync.RWMutex            `json:"-"`
}

// LazyWarmingStats provides statistics for lazy warming
type LazyWarmingStats struct {
	AccessCount          int64         `json:"access_count"`
	ThresholdReached     int64         `json:"threshold_reached"`
	ItemsWarmed          int64         `json:"items_warmed"`
	ItemsPending         int64         `json:"items_pending"`
	PredictionsGenerated int64         `json:"predictions_generated"`
	PredictionAccuracy   float64       `json:"prediction_accuracy"`
	CacheHitImprovement  float64       `json:"cache_hit_improvement"`
	AverageWarmingTime   time.Duration `json:"average_warming_time"`
	MemoryUsage          int64         `json:"memory_usage"`
	PatternCount         int           `json:"pattern_count"`
	LastCleanup          time.Time     `json:"last_cleanup"`
	QueueLength          int           `json:"queue_length"`
	WorkersActive        int           `json:"workers_active"`
	ErrorRate            float64       `json:"error_rate"`
}

// LazyWarmingStatus represents the status of lazy warming
type LazyWarmingStatus string

const (
	LazyWarmingStatusIdle    LazyWarmingStatus = "idle"
	LazyWarmingStatusActive  LazyWarmingStatus = "active"
	LazyWarmingStatusPaused  LazyWarmingStatus = "paused"
	LazyWarmingStatusStopped LazyWarmingStatus = "stopped"
	LazyWarmingStatusError   LazyWarmingStatus = "error"
)

// PendingItem represents an item pending warming
type PendingItem struct {
	Key              string        `json:"key"`
	AccessCount      int           `json:"access_count"`
	FirstAccess      time.Time     `json:"first_access"`
	LastAccess       time.Time     `json:"last_access"`
	WarmingScheduled time.Time     `json:"warming_scheduled"`
	Priority         int           `json:"priority"`
	PredictionScore  float64       `json:"prediction_score"`
	Pattern          AccessPattern `json:"pattern"`
	Source           string        `json:"source"`
}

// WarmingRequest represents a request to warm an item
type WarmingRequest struct {
	Key       string                 `json:"key"`
	Value     interface{}            `json:"value"`
	TTL       time.Duration          `json:"ttl"`
	Priority  int                    `json:"priority"`
	Source    string                 `json:"source"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
}

// AccessTracker tracks cache access patterns
type AccessTracker struct {
	accesses    map[string]*AccessInfo
	patterns    map[string]*AccessPattern
	mu          sync.RWMutex
	config      *LazyWarmingConfig
	logger      common.Logger
	lastCleanup time.Time
}

// AccessInfo contains information about cache accesses
type AccessInfo struct {
	Key         string      `json:"key"`
	Count       int         `json:"count"`
	FirstAccess time.Time   `json:"first_access"`
	LastAccess  time.Time   `json:"last_access"`
	Accesses    []time.Time `json:"accesses"`
	Pattern     string      `json:"pattern"`
}

// AccessPattern represents an identified access pattern
type AccessPattern struct {
	Pattern     string        `json:"pattern"`
	Frequency   time.Duration `json:"frequency"`
	Confidence  float64       `json:"confidence"`
	NextAccess  time.Time     `json:"next_access"`
	Description string        `json:"description"`
}

// LazyPredictionEngine predicts when items should be warmed
type LazyPredictionEngine struct {
	patterns map[string]*AccessPattern
	config   *LazyWarmingConfig
	logger   common.Logger
	mu       sync.RWMutex
}

// LazyWorker processes lazy warming requests
type LazyWorker struct {
	id        int
	operation *LazyWarmingOperation
	cache     cachecore.Cache
	workChan  chan WarmingRequest
	stopChan  chan struct{}
	logger    common.Logger
	metrics   common.Metrics
	stats     LazyWorkerStats
}

// LazyWorkerStats contains statistics for a lazy worker
type LazyWorkerStats struct {
	ItemsProcessed int64         `json:"items_processed"`
	ItemsSucceeded int64         `json:"items_succeeded"`
	ItemsFailed    int64         `json:"items_failed"`
	AverageLatency time.Duration `json:"average_latency"`
	LastActivity   time.Time     `json:"last_activity"`
	TotalLatency   time.Duration `json:"total_latency"`
}

// NewLazyWarmer creates a new lazy warming strategy
func NewLazyWarmer(cacheManager cachecore.CacheManager, logger common.Logger, metrics common.Metrics, config *LazyWarmingConfig) *LazyWarmer {
	if config == nil {
		config = &LazyWarmingConfig{
			AccessThreshold:       3,
			TimeWindow:            time.Hour,
			WarmingDelay:          5 * time.Minute,
			MaxConcurrency:        5,
			BatchSize:             50,
			PredictionWindow:      30 * time.Minute,
			WarmingProbability:    0.7,
			MaxPendingItems:       1000,
			EnablePredictive:      true,
			AccessPatternAnalysis: true,
			CleanupInterval:       time.Hour,
			MemoryLimit:           100 * 1024 * 1024, // 100MB
		}
	}

	lw := &LazyWarmer{
		cacheManager: cacheManager,
		logger:       logger,
		metrics:      metrics,
		config:       config,
		operations:   make(map[string]*LazyWarmingOperation),
	}

	lw.accessTracker = NewAccessTracker(config, logger)

	return lw
}

// StartLazyWarming starts lazy warming for a cache
func (lw *LazyWarmer) StartLazyWarming(ctx context.Context, cacheName string, dataSources []cachecore.DataSource, config cachecore.WarmConfig) (*LazyWarmingOperation, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	// Check if already running for this cache
	if _, exists := lw.operations[cacheName]; exists {
		return nil, fmt.Errorf("lazy warming already active for cache %s", cacheName)
	}

	// Get cache instance
	cache, err := lw.cacheManager.GetCache(cacheName)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache %s: %w", cacheName, err)
	}

	// Create operation context
	operationCtx, cancel := context.WithCancel(ctx)

	// Create operation
	operation := &LazyWarmingOperation{
		ID:           fmt.Sprintf("lazy_%s_%d", cacheName, time.Now().UnixNano()),
		CacheName:    cacheName,
		Config:       *lw.config,
		PendingItems: make(map[string]*PendingItem),
		WarmingQueue: make(chan WarmingRequest, lw.config.MaxPendingItems),
		Context:      operationCtx,
		Cancel:       cancel,
		StartedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Status:       LazyWarmingStatusActive,
		Stats: LazyWarmingStats{
			LastCleanup: time.Now(),
		},
	}

	// Initialize access tracker for this operation
	operation.AccessTracker = lw.accessTracker

	// Initialize prediction engine if enabled
	if lw.config.EnablePredictive {
		operation.PredictionEngine = NewLazyPredictionEngine(lw.config, lw.logger)
	}

	// Create workers
	operation.Workers = make([]*LazyWorker, lw.config.MaxConcurrency)
	for i := 0; i < lw.config.MaxConcurrency; i++ {
		worker := &LazyWorker{
			id:        i,
			operation: operation,
			cache:     cache,
			workChan:  operation.WarmingQueue,
			stopChan:  make(chan struct{}),
			logger:    lw.logger,
			metrics:   lw.metrics,
		}
		operation.Workers[i] = worker
		go worker.start()
		operation.Stats.WorkersActive++
	}

	// OnStart the monitoring goroutine
	go lw.monitorOperation(operation)

	// OnStart cleanup goroutine
	go lw.cleanupRoutine(operation)

	lw.operations[cacheName] = operation

	if lw.logger != nil {
		lw.logger.Info("lazy warming started",
			logger.String("cache_name", cacheName),
			logger.String("operation_id", operation.ID),
		)
	}

	if lw.metrics != nil {
		lw.metrics.Counter("forge.cache.warming.lazy.started").Inc()
	}

	return operation, nil
}

// StopLazyWarming stops lazy warming for a cache
func (lw *LazyWarmer) StopLazyWarming(cacheName string) error {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	operation, exists := lw.operations[cacheName]
	if !exists {
		return fmt.Errorf("no lazy warming operation found for cache %s", cacheName)
	}

	// Cancel operation
	operation.Cancel()
	operation.Status = LazyWarmingStatusStopped

	// OnStop workers
	for _, worker := range operation.Workers {
		close(worker.stopChan)
	}

	delete(lw.operations, cacheName)

	if lw.logger != nil {
		lw.logger.Info("lazy warming stopped",
			logger.String("cache_name", cacheName),
			logger.String("operation_id", operation.ID),
		)
	}

	if lw.metrics != nil {
		lw.metrics.Counter("forge.cache.warming.lazy.stopped").Inc()
	}

	return nil
}

// RecordAccess records a cache access for lazy warming consideration
func (lw *LazyWarmer) RecordAccess(cacheName, key string) {
	lw.mu.RLock()
	operation, exists := lw.operations[cacheName]
	lw.mu.RUnlock()

	if !exists {
		return
	}

	// Record access in tracker
	operation.AccessTracker.RecordAccess(key)

	// Update operation stats
	operation.mu.Lock()
	operation.Stats.AccessCount++
	operation.UpdatedAt = time.Now()
	operation.mu.Unlock()

	// Check if item should be considered for warming
	lw.evaluateForWarming(operation, key)

	if lw.metrics != nil {
		lw.metrics.Counter("forge.cache.warming.lazy.access_recorded", "cache", cacheName).Inc()
	}
}

// evaluateForWarming evaluates if an item should be warmed
func (lw *LazyWarmer) evaluateForWarming(operation *LazyWarmingOperation, key string) {
	accessInfo := operation.AccessTracker.GetAccessInfo(key)
	if accessInfo == nil {
		return
	}

	// Check if access threshold is reached
	if accessInfo.Count >= operation.Config.AccessThreshold {
		operation.mu.Lock()

		// Check if already pending
		if _, exists := operation.PendingItems[key]; !exists {
			// Check if we're under the pending items limit
			if len(operation.PendingItems) < operation.Config.MaxPendingItems {
				pendingItem := &PendingItem{
					Key:              key,
					AccessCount:      accessInfo.Count,
					FirstAccess:      accessInfo.FirstAccess,
					LastAccess:       accessInfo.LastAccess,
					WarmingScheduled: time.Now().Add(operation.Config.WarmingDelay),
					Priority:         lw.calculatePriority(accessInfo),
					Source:           "threshold",
				}

				// Get prediction score if predictive warming is enabled
				if operation.Config.EnablePredictive && operation.PredictionEngine != nil {
					score := operation.PredictionEngine.GetPredictionScore(key, accessInfo)
					pendingItem.PredictionScore = score

					if score >= operation.Config.WarmingProbability {
						pendingItem.Priority++
						pendingItem.Source = "predictive"
					}
				}

				operation.PendingItems[key] = pendingItem
				operation.Stats.ThresholdReached++
				operation.Stats.ItemsPending++
			}
		}

		operation.mu.Unlock()
	}
}

// calculatePriority calculates warming priority for an item
func (lw *LazyWarmer) calculatePriority(accessInfo *AccessInfo) int {
	priority := 0

	// Base priority on access frequency
	timeSinceFirst := time.Since(accessInfo.FirstAccess)
	if timeSinceFirst > 0 {
		frequency := float64(accessInfo.Count) / timeSinceFirst.Hours()
		if frequency > 1 {
			priority += 2
		} else if frequency > 0.5 {
			priority += 1
		}
	}

	// Boost priority for recent accesses
	if time.Since(accessInfo.LastAccess) < 10*time.Minute {
		priority++
	}

	return priority
}

// monitorOperation monitors a lazy warming operation
func (lw *LazyWarmer) monitorOperation(operation *LazyWarmingOperation) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lw.processScheduledItems(operation)
			lw.generatePredictiveItems(operation)
			lw.updateOperationStats(operation)

		case <-operation.Context.Done():
			return
		}
	}
}

// processScheduledItems processes items scheduled for warming
func (lw *LazyWarmer) processScheduledItems(operation *LazyWarmingOperation) {
	operation.mu.Lock()
	now := time.Now()

	for key, item := range operation.PendingItems {
		if now.After(item.WarmingScheduled) {
			// Create warming request
			request := WarmingRequest{
				Key:       key,
				Priority:  item.Priority,
				Source:    item.Source,
				Timestamp: now,
				Metadata: map[string]interface{}{
					"access_count":      item.AccessCount,
					"prediction_score":  item.PredictionScore,
					"warming_scheduled": item.WarmingScheduled,
				},
			}

			// Try to send to warming queue
			select {
			case operation.WarmingQueue <- request:
				delete(operation.PendingItems, key)
				operation.Stats.ItemsPending--
			default:
				// Queue is full, skip for now
			}
		}
	}

	operation.mu.Unlock()
}

// generatePredictiveItems generates items for predictive warming
func (lw *LazyWarmer) generatePredictiveItems(operation *LazyWarmingOperation) {
	if !operation.Config.EnablePredictive || operation.PredictionEngine == nil {
		return
	}

	predictions := operation.PredictionEngine.GeneratePredictions(operation.Config.PredictionWindow)

	operation.mu.Lock()
	for _, prediction := range predictions {
		if _, exists := operation.PendingItems[prediction.Key]; !exists {
			if len(operation.PendingItems) < operation.Config.MaxPendingItems {
				pendingItem := &PendingItem{
					Key:              prediction.Key,
					WarmingScheduled: prediction.ScheduledTime,
					Priority:         prediction.Priority,
					PredictionScore:  prediction.Score,
					Source:           "predictive",
				}

				operation.PendingItems[prediction.Key] = pendingItem
				operation.Stats.ItemsPending++
				operation.Stats.PredictionsGenerated++
			}
		}
	}
	operation.mu.Unlock()
}

// updateOperationStats updates operation statistics
func (lw *LazyWarmer) updateOperationStats(operation *LazyWarmingOperation) {
	operation.mu.Lock()

	operation.Stats.QueueLength = len(operation.WarmingQueue)
	operation.Stats.PatternCount = len(operation.AccessTracker.patterns)
	operation.UpdatedAt = time.Now()

	// Calculate memory usage
	operation.Stats.MemoryUsage = lw.calculateMemoryUsage(operation)

	operation.mu.Unlock()

	if lw.metrics != nil {
		lw.metrics.Gauge("forge.cache.warming.lazy.pending_items", "cache", operation.CacheName).Set(float64(operation.Stats.ItemsPending))
		lw.metrics.Gauge("forge.cache.warming.lazy.queue_length", "cache", operation.CacheName).Set(float64(operation.Stats.QueueLength))
		lw.metrics.Gauge("forge.cache.warming.lazy.memory_usage", "cache", operation.CacheName).Set(float64(operation.Stats.MemoryUsage))
	}
}

// calculateMemoryUsage calculates approximate memory usage
func (lw *LazyWarmer) calculateMemoryUsage(operation *LazyWarmingOperation) int64 {
	// Simplified memory calculation
	var usage int64

	// Pending items
	usage += int64(len(operation.PendingItems)) * 200 // Approximate bytes per pending item

	// Access tracker
	usage += int64(len(operation.AccessTracker.accesses)) * 150 // Approximate bytes per access info

	// Patterns
	usage += int64(len(operation.AccessTracker.patterns)) * 100 // Approximate bytes per pattern

	return usage
}

// cleanupRoutine performs periodic cleanup
func (lw *LazyWarmer) cleanupRoutine(operation *LazyWarmingOperation) {
	ticker := time.NewTicker(operation.Config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lw.performCleanup(operation)

		case <-operation.Context.Done():
			return
		}
	}
}

// performCleanup performs cleanup of old data
func (lw *LazyWarmer) performCleanup(operation *LazyWarmingOperation) {
	operation.AccessTracker.Cleanup()

	operation.mu.Lock()
	operation.Stats.LastCleanup = time.Now()
	operation.mu.Unlock()

	if lw.logger != nil {
		lw.logger.Debug("lazy warming cleanup completed",
			logger.String("cache_name", operation.CacheName),
		)
	}
}

// GetOperationStats returns statistics for a lazy warming operation
func (lw *LazyWarmer) GetOperationStats(cacheName string) (*LazyWarmingStats, error) {
	lw.mu.RLock()
	defer lw.mu.RUnlock()

	operation, exists := lw.operations[cacheName]
	if !exists {
		return nil, fmt.Errorf("no lazy warming operation found for cache %s", cacheName)
	}

	operation.mu.RLock()
	stats := operation.Stats
	operation.mu.RUnlock()

	return &stats, nil
}

// start starts a lazy worker
func (lw *LazyWorker) start() {
	for {
		select {
		case request := <-lw.workChan:
			lw.processRequest(request)

		case <-lw.stopChan:
			return
		}
	}
}

// processRequest processes a warming request
func (lw *LazyWorker) processRequest(request WarmingRequest) {
	startTime := time.Now()
	lw.stats.LastActivity = startTime

	// TODO: Implement actual warming logic
	// This would involve fetching the data and storing it in cache
	// For now, this is a placeholder

	duration := time.Since(startTime)
	lw.stats.ItemsProcessed++
	lw.stats.TotalLatency += duration
	lw.stats.AverageLatency = lw.stats.TotalLatency / time.Duration(lw.stats.ItemsProcessed)
	lw.stats.ItemsSucceeded++

	// Update operation stats
	lw.operation.mu.Lock()
	lw.operation.Stats.ItemsWarmed++
	lw.operation.Stats.AverageWarmingTime = (lw.operation.Stats.AverageWarmingTime + duration) / 2
	lw.operation.mu.Unlock()

	if lw.logger != nil {
		lw.logger.Debug("lazy warming item processed",
			logger.String("key", request.Key),
			logger.Int("worker_id", lw.id),
			logger.Duration("duration", duration),
		)
	}

	if lw.metrics != nil {
		lw.metrics.Counter("forge.cache.warming.lazy.items_warmed").Inc()
		lw.metrics.Histogram("forge.cache.warming.lazy.warming_duration").Observe(duration.Seconds())
	}
}

// NewAccessTracker creates a new access tracker
func NewAccessTracker(config *LazyWarmingConfig, logger common.Logger) *AccessTracker {
	return &AccessTracker{
		accesses:    make(map[string]*AccessInfo),
		patterns:    make(map[string]*AccessPattern),
		config:      config,
		logger:      logger,
		lastCleanup: time.Now(),
	}
}

// RecordAccess records a cache access
func (at *AccessTracker) RecordAccess(key string) {
	at.mu.Lock()
	defer at.mu.Unlock()

	now := time.Now()

	if info, exists := at.accesses[key]; exists {
		info.Count++
		info.LastAccess = now
		info.Accesses = append(info.Accesses, now)

		// Limit access history
		if len(info.Accesses) > 100 {
			info.Accesses = info.Accesses[1:]
		}
	} else {
		at.accesses[key] = &AccessInfo{
			Key:         key,
			Count:       1,
			FirstAccess: now,
			LastAccess:  now,
			Accesses:    []time.Time{now},
		}
	}

	// Analyze patterns if enabled
	if at.config.AccessPatternAnalysis {
		at.analyzePattern(key)
	}
}

// GetAccessInfo returns access information for a key
func (at *AccessTracker) GetAccessInfo(key string) *AccessInfo {
	at.mu.RLock()
	defer at.mu.RUnlock()

	return at.accesses[key]
}

// analyzePattern analyzes access patterns for a key
func (at *AccessTracker) analyzePattern(key string) {
	info := at.accesses[key]
	if len(info.Accesses) < 3 {
		return
	}

	// Simple pattern detection - could be enhanced with more sophisticated algorithms
	intervals := make([]time.Duration, len(info.Accesses)-1)
	for i := 1; i < len(info.Accesses); i++ {
		intervals[i-1] = info.Accesses[i].Sub(info.Accesses[i-1])
	}

	// Check for regular intervals
	if len(intervals) >= 2 {
		avgInterval := time.Duration(0)
		for _, interval := range intervals {
			avgInterval += interval
		}
		avgInterval /= time.Duration(len(intervals))

		// Check if intervals are relatively consistent
		variance := time.Duration(0)
		for _, interval := range intervals {
			diff := interval - avgInterval
			if diff < 0 {
				diff = -diff
			}
			variance += diff
		}
		variance /= time.Duration(len(intervals))

		if variance < avgInterval/2 { // Less than 50% variance
			pattern := &AccessPattern{
				Pattern:     "regular",
				Frequency:   avgInterval,
				Confidence:  1.0 - float64(variance)/float64(avgInterval),
				NextAccess:  info.LastAccess.Add(avgInterval),
				Description: fmt.Sprintf("Regular access every %v", avgInterval),
			}

			at.patterns[key] = pattern
			info.Pattern = "regular"
		}
	}
}

// Cleanup removes old access data
func (at *AccessTracker) Cleanup() {
	at.mu.Lock()
	defer at.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-at.config.TimeWindow)

	// Remove old accesses
	for key, info := range at.accesses {
		if info.LastAccess.Before(cutoff) {
			delete(at.accesses, key)
			delete(at.patterns, key)
		}
	}

	at.lastCleanup = now
}

// NewLazyPredictionEngine creates a new lazy prediction engine
func NewLazyPredictionEngine(config *LazyWarmingConfig, logger common.Logger) *LazyPredictionEngine {
	return &LazyPredictionEngine{
		patterns: make(map[string]*AccessPattern),
		config:   config,
		logger:   logger,
	}
}

// GetPredictionScore returns a prediction score for warming an item
func (lpe *LazyPredictionEngine) GetPredictionScore(key string, accessInfo *AccessInfo) float64 {
	lpe.mu.RLock()
	defer lpe.mu.RUnlock()

	pattern, exists := lpe.patterns[key]
	if !exists {
		// No pattern, use basic scoring
		return lpe.calculateBasicScore(accessInfo)
	}

	// Use pattern-based scoring
	return pattern.Confidence
}

// calculateBasicScore calculates a basic prediction score
func (lpe *LazyPredictionEngine) calculateBasicScore(accessInfo *AccessInfo) float64 {
	// Simple scoring based on access frequency
	timeSinceFirst := time.Since(accessInfo.FirstAccess)
	if timeSinceFirst.Hours() == 0 {
		return 0.5 // Default score for very recent items
	}

	frequency := float64(accessInfo.Count) / timeSinceFirst.Hours()
	score := frequency / 10.0 // Normalize to 0-1 range

	if score > 1.0 {
		score = 1.0
	}

	return score
}

// Prediction represents a warming prediction
type Prediction struct {
	Key           string    `json:"key"`
	Score         float64   `json:"score"`
	ScheduledTime time.Time `json:"scheduled_time"`
	Priority      int       `json:"priority"`
	Reason        string    `json:"reason"`
}

// GeneratePredictions generates warming predictions for the next time window
func (lpe *LazyPredictionEngine) GeneratePredictions(window time.Duration) []Prediction {
	lpe.mu.RLock()
	defer lpe.mu.RUnlock()

	var predictions []Prediction
	now := time.Now()
	cutoff := now.Add(window)

	for key, pattern := range lpe.patterns {
		if pattern.NextAccess.After(now) && pattern.NextAccess.Before(cutoff) {
			if pattern.Confidence >= lpe.config.WarmingProbability {
				prediction := Prediction{
					Key:           key,
					Score:         pattern.Confidence,
					ScheduledTime: pattern.NextAccess.Add(-lpe.config.WarmingDelay),
					Priority:      lpe.calculatePredictionPriority(pattern),
					Reason:        pattern.Description,
				}
				predictions = append(predictions, prediction)
			}
		}
	}

	return predictions
}

// calculatePredictionPriority calculates priority for a prediction
func (lpe *LazyPredictionEngine) calculatePredictionPriority(pattern *AccessPattern) int {
	priority := 1

	if pattern.Confidence > 0.8 {
		priority += 2
	} else if pattern.Confidence > 0.6 {
		priority += 1
	}

	if pattern.Frequency < time.Hour {
		priority += 1
	}

	return priority
}
