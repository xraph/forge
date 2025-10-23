package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// PipelineManager manages Redis pipeline operations for batch processing
type PipelineManager struct {
	cache         *RedisCache
	batchSize     int
	flushInterval time.Duration

	// Batching
	pendingOps []PipelineOp
	opsMu      sync.Mutex
	lastFlush  time.Time

	// Background flushing
	flushTicker *time.Ticker
	stopChan    chan struct{}
	running     bool
	runningMu   sync.Mutex

	// Statistics
	stats   *PipelineStats
	logger  common.Logger
	metrics common.Metrics
}

// PipelineOp represents a pipeline operation
type PipelineOp struct {
	Type      string
	Key       string
	Value     interface{}
	TTL       time.Duration
	Keys      []string
	Values    map[string]interface{}
	Callback  func(interface{}, error)
	CreatedAt time.Time
}

// PipelineStats tracks pipeline statistics
type PipelineStats struct {
	TotalOps        int64         `json:"total_ops"`
	BatchedOps      int64         `json:"batched_ops"`
	IndividualOps   int64         `json:"individual_ops"`
	FlushCount      int64         `json:"flush_count"`
	AvgBatchSize    float64       `json:"avg_batch_size"`
	AvgFlushLatency time.Duration `json:"avg_flush_latency"`
	LastFlush       time.Time     `json:"last_flush"`
	PendingOps      int           `json:"pending_ops"`
	mu              sync.RWMutex
}

// PipelineConfig contains pipeline configuration
type PipelineConfig struct {
	BatchSize     int           `yaml:"batch_size" json:"batch_size" default:"100"`
	FlushInterval time.Duration `yaml:"flush_interval" json:"flush_interval" default:"100ms"`
	MaxPending    int           `yaml:"max_pending" json:"max_pending" default:"1000"`
	AutoFlush     bool          `yaml:"auto_flush" json:"auto_flush" default:"true"`
}

// NewPipelineManager creates a new pipeline manager
func NewPipelineManager(cache *RedisCache, config PipelineConfig, logger common.Logger, metrics common.Metrics) *PipelineManager {
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 100 * time.Millisecond
	}

	pm := &PipelineManager{
		cache:         cache,
		batchSize:     config.BatchSize,
		flushInterval: config.FlushInterval,
		pendingOps:    make([]PipelineOp, 0, config.BatchSize),
		stopChan:      make(chan struct{}),
		stats:         &PipelineStats{},
		logger:        logger,
		metrics:       metrics,
	}

	if config.AutoFlush {
		pm.Start()
	}

	return pm
}

// Start starts the pipeline manager
func (pm *PipelineManager) Start() {
	pm.runningMu.Lock()
	defer pm.runningMu.Unlock()

	if pm.running {
		return
	}

	pm.running = true
	pm.flushTicker = time.NewTicker(pm.flushInterval)

	go pm.backgroundFlusher()

	if pm.logger != nil {
		pm.logger.Info("pipeline manager started",
			logger.String("cache", pm.cache.Name()),
			logger.Int("batch_size", pm.batchSize),
			logger.Duration("flush_interval", pm.flushInterval),
		)
	}
}

// Stop stops the pipeline manager
func (pm *PipelineManager) Stop() {
	pm.runningMu.Lock()
	defer pm.runningMu.Unlock()

	if !pm.running {
		return
	}

	pm.running = false
	close(pm.stopChan)

	if pm.flushTicker != nil {
		pm.flushTicker.Stop()
	}

	// Flush any remaining operations
	pm.flushPending()

	if pm.logger != nil {
		pm.logger.Info("pipeline manager stopped", logger.String("cache", pm.cache.Name()))
	}
}

// backgroundFlusher runs in the background to flush pending operations
func (pm *PipelineManager) backgroundFlusher() {
	defer func() {
		if r := recover(); r != nil {
			if pm.logger != nil {
				pm.logger.Error("pipeline flusher panic recovered",
					logger.String("cache", pm.cache.Name()),
					logger.Any("panic", r),
				)
			}
		}
	}()

	for {
		select {
		case <-pm.stopChan:
			return
		case <-pm.flushTicker.C:
			if pm.shouldFlush() {
				pm.flushPending()
			}
		}
	}
}

// shouldFlush determines if pending operations should be flushed
func (pm *PipelineManager) shouldFlush() bool {
	pm.opsMu.Lock()
	defer pm.opsMu.Unlock()

	// Flush if we have operations and enough time has passed
	return len(pm.pendingOps) > 0 && time.Since(pm.lastFlush) >= pm.flushInterval
}

// AddOperation adds an operation to the pipeline
func (pm *PipelineManager) AddOperation(op PipelineOp) {
	pm.opsMu.Lock()
	defer pm.opsMu.Unlock()

	op.CreatedAt = time.Now()
	pm.pendingOps = append(pm.pendingOps, op)

	pm.stats.mu.Lock()
	pm.stats.TotalOps++
	pm.stats.PendingOps = len(pm.pendingOps)
	pm.stats.mu.Unlock()

	// Flush if batch is full
	if len(pm.pendingOps) >= pm.batchSize {
		go pm.flushPending()
	}
}

// BatchSet adds multiple set operations to the pipeline
func (pm *PipelineManager) BatchSet(ctx context.Context, items map[string]cachecore.CacheItem, callback func(error)) {
	op := PipelineOp{
		Type:   "mset",
		Values: make(map[string]interface{}),
		Callback: func(result interface{}, err error) {
			if callback != nil {
				callback(err)
			}
		},
	}

	for key, item := range items {
		op.Values[key] = item.Value
	}

	pm.AddOperation(op)
}

// BatchGet adds multiple get operations to the pipeline
func (pm *PipelineManager) BatchGet(ctx context.Context, keys []string, callback func(map[string]interface{}, error)) {
	op := PipelineOp{
		Type: "mget",
		Keys: keys,
		Callback: func(result interface{}, err error) {
			if callback != nil {
				if values, ok := result.(map[string]interface{}); ok {
					callback(values, err)
				} else {
					callback(nil, err)
				}
			}
		},
	}

	pm.AddOperation(op)
}

// BatchDelete adds multiple delete operations to the pipeline
func (pm *PipelineManager) BatchDelete(ctx context.Context, keys []string, callback func(error)) {
	op := PipelineOp{
		Type: "mdel",
		Keys: keys,
		Callback: func(result interface{}, err error) {
			if callback != nil {
				callback(err)
			}
		},
	}

	pm.AddOperation(op)
}

// flushPending flushes all pending operations
func (pm *PipelineManager) flushPending() {
	pm.opsMu.Lock()
	if len(pm.pendingOps) == 0 {
		pm.opsMu.Unlock()
		return
	}

	ops := make([]PipelineOp, len(pm.pendingOps))
	copy(ops, pm.pendingOps)
	pm.pendingOps = pm.pendingOps[:0] // Clear slice but keep capacity
	pm.lastFlush = time.Now()
	pm.opsMu.Unlock()

	pm.executePipeline(ops)
}

// executePipeline executes a batch of operations using Redis pipeline
func (pm *PipelineManager) executePipeline(ops []PipelineOp) {
	if len(ops) == 0 {
		return
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Group operations by type for optimal pipelining
	sets := make(map[string]interface{})
	gets := make([]string, 0)
	deletes := make([]string, 0)
	individual := make([]PipelineOp, 0)

	for _, op := range ops {
		switch op.Type {
		case "set":
			sets[op.Key] = op.Value
		case "mset":
			for k, v := range op.Values {
				sets[k] = v
			}
		case "get":
			gets = append(gets, op.Key)
		case "mget":
			gets = append(gets, op.Keys...)
		case "del":
			deletes = append(deletes, op.Key)
		case "mdel":
			deletes = append(deletes, op.Keys...)
		default:
			individual = append(individual, op)
		}
	}

	pipe := pm.cache.client.Pipeline()

	// Add operations to pipeline
	var setCmd *redis.StatusCmd
	var getCmd *redis.SliceCmd
	var delCmd *redis.IntCmd

	if len(sets) > 0 {
		setCmd = pm.addSetsToPipeline(pipe, sets)
	}

	if len(gets) > 0 {
		getCmd = pipe.MGet(ctx, gets...)
	}

	if len(deletes) > 0 {
		delCmd = pipe.Del(ctx, deletes...)
	}

	// Add individual operations
	individualCmds := pm.addIndividualOpsToPipeline(pipe, ctx, individual)

	// Execute pipeline
	_, err := pipe.Exec(ctx)

	latency := time.Since(start)

	// Update statistics
	pm.updateStats(len(ops), latency)

	// Process results and call callbacks
	pm.processResults(ops, setCmd, getCmd, delCmd, individualCmds, err)

	if pm.logger != nil && err != nil {
		pm.logger.Error("pipeline execution failed",
			logger.String("cache", pm.cache.Name()),
			logger.Int("ops_count", len(ops)),
			logger.Duration("latency", latency),
			logger.Error(err),
		)
	}
}

// addSetsToPipeline adds set operations to the pipeline
func (pm *PipelineManager) addSetsToPipeline(pipe redis.Pipeliner, sets map[string]interface{}) *redis.StatusCmd {
	ctx := context.Background()

	// Convert to Redis format
	args := make([]interface{}, 0, len(sets)*2)
	for key, value := range sets {
		data, err := json.Marshal(value)
		if err != nil {
			continue
		}
		args = append(args, key, string(data))
	}

	if len(args) == 0 {
		return nil
	}

	return pipe.MSet(ctx, args...)
}

// addIndividualOpsToPipeline adds individual operations to the pipeline
func (pm *PipelineManager) addIndividualOpsToPipeline(pipe redis.Pipeliner, ctx context.Context, ops []PipelineOp) map[int]redis.Cmder {
	cmds := make(map[int]redis.Cmder)

	for i, op := range ops {
		switch op.Type {
		case "incr":
			if delta, ok := op.Value.(int64); ok {
				cmds[i] = pipe.IncrBy(ctx, op.Key, delta)
			}
		case "decr":
			if delta, ok := op.Value.(int64); ok {
				cmds[i] = pipe.IncrBy(ctx, op.Key, -delta)
			}
		case "expire":
			if ttl, ok := op.Value.(time.Duration); ok {
				cmds[i] = pipe.Expire(ctx, op.Key, ttl)
			}
		case "exists":
			cmds[i] = pipe.Exists(ctx, op.Key)
		}
	}

	return cmds
}

// processResults processes pipeline results and calls callbacks
func (pm *PipelineManager) processResults(
	ops []PipelineOp,
	setCmd *redis.StatusCmd,
	getCmd *redis.SliceCmd,
	delCmd *redis.IntCmd,
	individualCmds map[int]redis.Cmder,
	pipelineErr error,
) {
	// Process set results
	if setCmd != nil {
		err := setCmd.Err()
		if err == nil {
			err = pipelineErr
		}

		for _, op := range ops {
			if (op.Type == "set" || op.Type == "mset") && op.Callback != nil {
				op.Callback(nil, err)
			}
		}
	}

	// Process get results
	if getCmd != nil {
		results, err := getCmd.Result()
		if err == nil {
			err = pipelineErr
		}

		values := make(map[string]interface{})
		getKeys := make([]string, 0)

		// Collect all get keys in order
		for _, op := range ops {
			if op.Type == "get" {
				getKeys = append(getKeys, op.Key)
			} else if op.Type == "mget" {
				getKeys = append(getKeys, op.Keys...)
			}
		}

		// Parse results
		for i, result := range results {
			if i < len(getKeys) && result != nil {
				var value interface{}
				if err := json.Unmarshal([]byte(result.(string)), &value); err == nil {
					values[getKeys[i]] = value
				}
			}
		}

		// Call callbacks
		for _, op := range ops {
			if (op.Type == "get" || op.Type == "mget") && op.Callback != nil {
				op.Callback(values, err)
			}
		}
	}

	// Process delete results
	if delCmd != nil {
		err := delCmd.Err()
		if err == nil {
			err = pipelineErr
		}

		for _, op := range ops {
			if (op.Type == "del" || op.Type == "mdel") && op.Callback != nil {
				op.Callback(nil, err)
			}
		}
	}

	// Process individual operation results
	individualOpIndex := 0
	for _, op := range ops {
		if op.Type != "set" && op.Type != "mset" && op.Type != "get" && op.Type != "mget" && op.Type != "del" && op.Type != "mdel" {
			if cmd, exists := individualCmds[individualOpIndex]; exists && op.Callback != nil {
				if cmd != nil {
					var result interface{}
					var cmdErr error

					switch c := cmd.(type) {
					case *redis.StringCmd:
						result, cmdErr = c.Result()
					case *redis.IntCmd:
						result = c.Val()
						cmdErr = c.Err()
					case *redis.StatusCmd:
						result, cmdErr = c.Result()
					case *redis.SliceCmd:
						result, cmdErr = c.Result()
					// Add more cases as needed for other redis.Cmder implementations
					default:
						cmdErr = fmt.Errorf("unsupported command type: %T", cmd)
					}

					if cmdErr == nil {
						cmdErr = pipelineErr
					}

					op.Callback(result, cmdErr)
				} else {
					op.Callback(nil, fmt.Errorf("command is nil"))
				}

			}
			individualOpIndex++
		}
	}
}

// updateStats updates pipeline statistics
func (pm *PipelineManager) updateStats(opsCount int, latency time.Duration) {
	pm.stats.mu.Lock()
	defer pm.stats.mu.Unlock()

	pm.stats.BatchedOps += int64(opsCount)
	pm.stats.FlushCount++
	pm.stats.LastFlush = time.Now()

	// Update average batch size
	pm.stats.AvgBatchSize = float64(pm.stats.BatchedOps) / float64(pm.stats.FlushCount)

	// Update average flush latency
	totalLatency := time.Duration(pm.stats.FlushCount-1)*pm.stats.AvgFlushLatency + latency
	pm.stats.AvgFlushLatency = totalLatency / time.Duration(pm.stats.FlushCount)

	pm.opsMu.Lock()
	pm.stats.PendingOps = len(pm.pendingOps)
	pm.opsMu.Unlock()

	if pm.metrics != nil {
		pm.metrics.Counter("cache.pipeline.batched_ops", "cache", pm.cache.Name()).Add(float64(opsCount))
		pm.metrics.Counter("cache.pipeline.flush_count", "cache", pm.cache.Name()).Inc()
		pm.metrics.Histogram("cache.pipeline.flush_latency", "cache", pm.cache.Name()).Observe(latency.Seconds())
		pm.metrics.Gauge("cache.pipeline.avg_batch_size", "cache", pm.cache.Name()).Set(pm.stats.AvgBatchSize)
	}
}

// GetStats returns pipeline statistics
func (pm *PipelineManager) GetStats() PipelineStats {
	pm.stats.mu.RLock()
	defer pm.stats.mu.RUnlock()

	pm.opsMu.Lock()
	pendingOps := len(pm.pendingOps)
	pm.opsMu.Unlock()

	stats := *pm.stats
	stats.PendingOps = pendingOps
	return stats
}

// FlushNow forces an immediate flush of pending operations
func (pm *PipelineManager) FlushNow() {
	pm.flushPending()
}

// IsRunning returns whether the pipeline manager is running
func (pm *PipelineManager) IsRunning() bool {
	pm.runningMu.Lock()
	defer pm.runningMu.Unlock()
	return pm.running
}

// GetPendingCount returns the number of pending operations
func (pm *PipelineManager) GetPendingCount() int {
	pm.opsMu.Lock()
	defer pm.opsMu.Unlock()
	return len(pm.pendingOps)
}

// SetBatchSize updates the batch size
func (pm *PipelineManager) SetBatchSize(size int) {
	if size <= 0 {
		return
	}

	pm.opsMu.Lock()
	defer pm.opsMu.Unlock()

	pm.batchSize = size
}

// SetFlushInterval updates the flush interval
func (pm *PipelineManager) SetFlushInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}

	pm.runningMu.Lock()
	defer pm.runningMu.Unlock()

	pm.flushInterval = interval

	if pm.running && pm.flushTicker != nil {
		pm.flushTicker.Stop()
		pm.flushTicker = time.NewTicker(interval)
	}
}

// OptimizedRedisCache extends RedisCache with pipeline optimization
type OptimizedRedisCache struct {
	*RedisCache
	pipelineManager *PipelineManager
	enablePipeline  bool
}

// NewOptimizedRedisCache creates a Redis cache with pipeline optimization
func NewOptimizedRedisCache(name string, config *cachecore.RedisConfig, logger common.Logger, metrics common.Metrics) (*OptimizedRedisCache, error) {
	baseCache, err := NewRedisCache(name, config, logger, metrics)
	if err != nil {
		return nil, err
	}

	pipelineConfig := PipelineConfig{
		BatchSize:     config.PipelineSize,
		FlushInterval: 100 * time.Millisecond,
		AutoFlush:     config.EnablePipelining,
	}

	optimizedCache := &OptimizedRedisCache{
		RedisCache:     baseCache,
		enablePipeline: config.EnablePipelining,
	}

	if optimizedCache.enablePipeline {
		optimizedCache.pipelineManager = NewPipelineManager(baseCache, pipelineConfig, logger, metrics)
	}

	return optimizedCache, nil
}

// SetMulti uses pipeline for batch set operations
func (orc *OptimizedRedisCache) SetMulti(ctx context.Context, items map[string]cachecore.CacheItem) error {
	if !orc.enablePipeline || orc.pipelineManager == nil {
		return orc.RedisCache.SetMulti(ctx, items)
	}

	done := make(chan error, 1)

	orc.pipelineManager.BatchSet(ctx, items, func(err error) {
		done <- err
	})

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("pipeline set timeout")
	}
}

// GetMulti uses pipeline for batch get operations
func (orc *OptimizedRedisCache) GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error) {
	if !orc.enablePipeline || orc.pipelineManager == nil {
		return orc.RedisCache.GetMulti(ctx, keys)
	}

	done := make(chan struct {
		values map[string]interface{}
		err    error
	}, 1)

	orc.pipelineManager.BatchGet(ctx, keys, func(values map[string]interface{}, err error) {
		done <- struct {
			values map[string]interface{}
			err    error
		}{values, err}
	})

	select {
	case result := <-done:
		return result.values, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("pipeline get timeout")
	}
}

// Stop stops the optimized cache and pipeline manager
func (orc *OptimizedRedisCache) Stop(ctx context.Context) error {
	if orc.pipelineManager != nil {
		orc.pipelineManager.Stop()
	}
	return orc.RedisCache.Stop(ctx)
}

// GetPipelineStats returns pipeline statistics
func (orc *OptimizedRedisCache) GetPipelineStats() *PipelineStats {
	if orc.pipelineManager == nil {
		return nil
	}

	stats := orc.pipelineManager.GetStats()
	return &stats
}
