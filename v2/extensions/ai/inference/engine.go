package inference

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/ai/models"
	"github.com/xraph/forge/v2/internal/logger"
)

// InferenceHealthStatus represents the health status of the inference engine
type InferenceHealthStatus string

const (
	InferenceHealthStatusHealthy   InferenceHealthStatus = "healthy"
	InferenceHealthStatusUnhealthy InferenceHealthStatus = "unhealthy"
	InferenceHealthStatusDegraded  InferenceHealthStatus = "degraded"
	InferenceHealthStatusUnknown   InferenceHealthStatus = "unknown"
)

// InferenceEngine handles high-performance ML inference with batching and caching
type InferenceEngine struct {
	config    InferenceConfig
	models    map[string]models.Model
	pipeline  *InferencePipeline
	batcher   *RequestBatcher
	cache     *InferenceCache
	scaler    *InferenceScaler
	workers   []*InferenceWorker
	pool      *ObjectPool // Object pool for performance optimization
	stats     InferenceStats
	health    InferenceHealth
	started   bool
	mu        sync.RWMutex
	logger    logger.Logger
	metrics   forge.Metrics
	shutdownC chan struct{}
	wg        sync.WaitGroup
}

// InferenceConfig contains configuration for the inference engine
type InferenceConfig struct {
	Workers          int           `yaml:"workers" default:"4"`
	BatchSize        int           `yaml:"batch_size" default:"10"`
	BatchTimeout     time.Duration `yaml:"batch_timeout" default:"100ms"`
	CacheSize        int           `yaml:"cache_size" default:"1000"`
	CacheTTL         time.Duration `yaml:"cache_ttl" default:"1h"`
	MaxQueueSize     int           `yaml:"max_queue_size" default:"10000"`
	RequestTimeout   time.Duration `yaml:"request_timeout" default:"30s"`
	EnableBatching   bool          `yaml:"enable_batching" default:"true"`
	EnableCaching    bool          `yaml:"enable_caching" default:"true"`
	EnableScaling    bool          `yaml:"enable_scaling" default:"true"`
	ScalingThreshold float64       `yaml:"scaling_threshold" default:"0.8"`
	MaxWorkers       int           `yaml:"max_workers" default:"20"`
	MinWorkers       int           `yaml:"min_workers" default:"2"`
	Logger           logger.Logger `yaml:"-"`
	Metrics          forge.Metrics `yaml:"-"`
}

// InferenceStats contains statistics about the inference engine
type InferenceStats struct {
	RequestsReceived  int64         `json:"requests_received"`
	RequestsProcessed int64         `json:"requests_processed"`
	RequestsError     int64         `json:"requests_error"`
	TotalInferences   int64         `json:"total_inferences"`
	BatchesProcessed  int64         `json:"batches_processed"`
	CacheHits         int64         `json:"cache_hits"`
	CacheMisses       int64         `json:"cache_misses"`
	AverageLatency    time.Duration `json:"average_latency"`
	AverageBatchSize  float64       `json:"average_batch_size"`
	ErrorRate         float64       `json:"error_rate"`
	CacheHitRate      float64       `json:"cache_hit_rate"`
	ThroughputPerSec  float64       `json:"throughput_per_sec"`
	ActiveWorkers     int           `json:"active_workers"`
	QueueSize         int           `json:"queue_size"`
	LastUpdated       time.Time     `json:"last_updated"`
}

// InferenceHealth represents the health status of the inference engine
type InferenceHealth struct {
	Status      InferenceHealthStatus  `json:"status"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details"`
	CheckedAt   time.Time              `json:"checked_at"`
	LastHealthy time.Time              `json:"last_healthy"`
}

// InferenceRequest represents a request for inference
type InferenceRequest struct {
	ID        string                  `json:"id"`
	ModelID   string                  `json:"model_id"`
	Input     models.ModelInput       `json:"input"`
	Options   InferenceOptions        `json:"options"`
	Context   map[string]interface{}  `json:"context"`
	Timeout   time.Duration           `json:"timeout"`
	Timestamp time.Time               `json:"timestamp"`
	Priority  int                     `json:"priority"`
	Callback  func(InferenceResponse) `json:"-"`
}

// InferenceResponse represents a response from inference
type InferenceResponse struct {
	ID         string                 `json:"id"`
	ModelID    string                 `json:"model_id"`
	Output     models.ModelOutput     `json:"output"`
	Latency    time.Duration          `json:"latency"`
	Confidence float64                `json:"confidence"`
	ModelInfo  models.ModelInfo       `json:"model_info"`
	Metadata   map[string]interface{} `json:"metadata"`
	Error      error                  `json:"error,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	FromCache  bool                   `json:"from_cache"`
	BatchSize  int                    `json:"batch_size"`
	WorkerID   string                 `json:"worker_id"`
}

// InferenceOptions contains options for inference
type InferenceOptions struct {
	Temperature   *float64               `json:"temperature,omitempty"`
	MaxTokens     *int                   `json:"max_tokens,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	TopK          *int                   `json:"top_k,omitempty"`
	BatchSize     int                    `json:"batch_size"`
	Timeout       time.Duration          `json:"timeout"`
	ReturnProbs   bool                   `json:"return_probs"`
	Streaming     bool                   `json:"streaming"`
	UseCache      bool                   `json:"use_cache"`
	CacheTTL      time.Duration          `json:"cache_ttl"`
	CustomOptions map[string]interface{} `json:"custom_options"`
}

// NewInferenceEngine creates a new inference engine
func NewInferenceEngine(config InferenceConfig) (*InferenceEngine, error) {
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 100 * time.Millisecond
	}
	if config.CacheSize <= 0 {
		config.CacheSize = 1000
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = time.Hour
	}
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 10000
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}
	if config.ScalingThreshold <= 0 {
		config.ScalingThreshold = 0.8
	}
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = 20
	}
	if config.MinWorkers <= 0 {
		config.MinWorkers = 2
	}

	engine := &InferenceEngine{
		config:    config,
		models:    make(map[string]models.Model),
		pool:      NewObjectPool(1000, 5*time.Minute), // Initialize object pool
		stats:     InferenceStats{},
		health:    InferenceHealth{Status: InferenceHealthStatusUnknown},
		logger:    config.Logger,
		metrics:   config.Metrics,
		shutdownC: make(chan struct{}),
	}

	// Initialize pipeline
	pipeline, err := NewInferencePipeline(InferencePipelineConfig{
		MaxConcurrency: config.Workers,
		Timeout:        config.RequestTimeout,
		Logger:         config.Logger,
		Metrics:        config.Metrics,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create inference pipeline: %w", err)
	}
	engine.pipeline = pipeline

	// Initialize batcher if enabled
	if config.EnableBatching {
		batcher, err := NewRequestBatcher(RequestBatcherConfig{
			BatchSize:    config.BatchSize,
			BatchTimeout: config.BatchTimeout,
			MaxQueueSize: config.MaxQueueSize,
			Logger:       config.Logger,
			Metrics:      config.Metrics,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create request batcher: %w", err)
		}
		engine.batcher = batcher
	}

	// Initialize cache if enabled
	if config.EnableCaching {
		cache, err := NewInferenceCache(InferenceCacheConfig{
			Size:    config.CacheSize,
			TTL:     config.CacheTTL,
			Logger:  config.Logger,
			Metrics: config.Metrics,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create inference cache: %w", err)
		}
		engine.cache = cache
	}

	// Initialize scaler if enabled
	if config.EnableScaling {
		scaler, err := NewInferenceScaler(InferenceScalerConfig{
			MinWorkers:       config.MinWorkers,
			MaxWorkers:       config.MaxWorkers,
			ScalingThreshold: config.ScalingThreshold,
			Logger:           config.Logger,
			Metrics:          config.Metrics,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create inference scaler: %w", err)
		}
		engine.scaler = scaler
	}

	return engine, nil
}

// Initialize initializes the inference engine
func (e *InferenceEngine) Initialize(ctx context.Context, config InferenceConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.config = config
	e.logger = config.Logger
	e.metrics = config.Metrics

	if e.logger != nil {
		e.logger.Info("inference engine initialized",
			logger.Int("workers", config.Workers),
			logger.Int("batch_size", config.BatchSize),
			logger.Bool("batching_enabled", config.EnableBatching),
			logger.Bool("caching_enabled", config.EnableCaching),
			logger.Bool("scaling_enabled", config.EnableScaling),
		)
	}

	return nil
}

// Start starts the inference engine
func (e *InferenceEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return fmt.Errorf("inference engine already started")
	}

	// Start pipeline
	if err := e.pipeline.Start(ctx); err != nil {
		return fmt.Errorf("failed to start inference pipeline: %w", err)
	}

	// Start batcher if enabled
	if e.batcher != nil {
		if err := e.batcher.Start(ctx); err != nil {
			return fmt.Errorf("failed to start request batcher: %w", err)
		}
	}

	// Start cache if enabled
	if e.cache != nil {
		if err := e.cache.Start(ctx); err != nil {
			return fmt.Errorf("failed to start inference cache: %w", err)
		}
	}

	// Start scaler if enabled
	if e.scaler != nil {
		if err := e.scaler.Start(ctx); err != nil {
			return fmt.Errorf("failed to start inference scaler: %w", err)
		}
	}

	// Start workers
	e.workers = make([]*InferenceWorker, e.config.Workers)
	for i := 0; i < e.config.Workers; i++ {
		worker := NewInferenceWorker(InferenceWorkerConfig{
			ID:       fmt.Sprintf("worker-%d", i),
			Engine:   e,
			Pipeline: e.pipeline,
			Logger:   e.logger,
			Metrics:  e.metrics,
		})
		e.workers[i] = worker

		e.wg.Add(1)
		go func(w *InferenceWorker) {
			defer e.wg.Done()
			w.Run(ctx)
		}(worker)
	}

	// Start stats collection
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runStatsCollection(ctx)
	}()

	e.started = true
	e.health.Status = InferenceHealthStatusHealthy
	e.health.Message = "Inference engine started successfully"
	e.health.LastHealthy = time.Now()

	if e.logger != nil {
		e.logger.Info("inference engine started",
			logger.Int("workers", len(e.workers)),
		)
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.inference_engine_started").Inc()
	}

	return nil
}

// Stop stops the inference engine
func (e *InferenceEngine) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started {
		return fmt.Errorf("inference engine not started")
	}

	// Signal shutdown
	close(e.shutdownC)

	// Wait for workers to finish
	e.wg.Wait()

	// Stop scaler
	if e.scaler != nil {
		if err := e.scaler.Stop(ctx); err != nil {
			if e.logger != nil {
				e.logger.Error("failed to stop inference scaler", logger.Error(err))
			}
		}
	}

	// Stop cache
	if e.cache != nil {
		if err := e.cache.Stop(ctx); err != nil {
			if e.logger != nil {
				e.logger.Error("failed to stop inference cache", logger.Error(err))
			}
		}
	}

	// Stop batcher
	if e.batcher != nil {
		if err := e.batcher.Stop(ctx); err != nil {
			if e.logger != nil {
				e.logger.Error("failed to stop request batcher", logger.Error(err))
			}
		}
	}

	// Stop pipeline
	if err := e.pipeline.Stop(ctx); err != nil {
		if e.logger != nil {
			e.logger.Error("failed to stop inference pipeline", logger.Error(err))
		}
	}

	e.started = false
	e.health.Status = InferenceHealthStatusUnknown
	e.health.Message = "Inference engine stopped"

	if e.logger != nil {
		e.logger.Info("inference engine stopped")
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.inference_engine_stopped").Inc()
	}

	return nil
}

// AddModel adds a model to the inference engine
func (e *InferenceEngine) AddModel(model models.Model) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.models[model.ID()]; exists {
		return fmt.Errorf("model %s already exists", model.ID())
	}

	e.models[model.ID()] = model

	if e.logger != nil {
		e.logger.Info("model added to inference engine",
			logger.String("model_id", model.ID()),
			logger.String("model_name", model.Name()),
			logger.String("model_type", string(model.Type())),
		)
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.inference_models_added").Inc()
		e.metrics.Gauge("forge.ai.inference_models_total").Set(float64(len(e.models)))
	}

	return nil
}

// RemoveModel removes a model from the inference engine
func (e *InferenceEngine) RemoveModel(modelID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.models[modelID]; !exists {
		return fmt.Errorf("model %s not found", modelID)
	}

	delete(e.models, modelID)

	if e.logger != nil {
		e.logger.Info("model removed from inference engine",
			logger.String("model_id", modelID),
		)
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.inference_models_removed").Inc()
		e.metrics.Gauge("forge.ai.inference_models_total").Set(float64(len(e.models)))
	}

	return nil
}

// Infer performs inference on a single request
func (e *InferenceEngine) Infer(ctx context.Context, request InferenceRequest) (InferenceResponse, error) {
	if !e.started {
		return InferenceResponse{}, fmt.Errorf("inference engine not started")
	}

	startTime := time.Now()
	request.Timestamp = startTime

	// Update request count
	e.updateStats(func(stats *InferenceStats) {
		stats.RequestsReceived++
	})

	// Check cache first if enabled
	if e.cache != nil && request.Options.UseCache {
		if response, found := e.cache.Get(request); found {
			e.updateStats(func(stats *InferenceStats) {
				stats.CacheHits++
			})

			if e.metrics != nil {
				e.metrics.Counter("forge.ai.inference_cache_hits").Inc()
			}

			return response, nil
		}
		e.updateStats(func(stats *InferenceStats) {
			stats.CacheMisses++
		})
	}

	// Process through pipeline
	response, err := e.pipeline.Process(ctx, request)
	if err != nil {
		e.updateStats(func(stats *InferenceStats) {
			stats.RequestsError++
		})

		if e.metrics != nil {
			e.metrics.Counter("forge.ai.inference_requests_error").Inc()
		}

		// Use object pool for response
		response := e.pool.GetResponse()
		response.ID = request.ID
		response.ModelID = request.ModelID
		response.Error = err
		response.Timestamp = time.Now()

		// Return response and put it back to pool after use
		defer e.pool.PutResponse(response)
		return *response, err
	}

	// Update cache if enabled
	if e.cache != nil && request.Options.UseCache && err == nil {
		e.cache.Set(request, response)
	}

	// Update stats
	latency := time.Since(startTime)
	e.updateStats(func(stats *InferenceStats) {
		stats.RequestsProcessed++
		stats.TotalInferences++
		stats.AverageLatency = (stats.AverageLatency*time.Duration(stats.RequestsProcessed-1) + latency) / time.Duration(stats.RequestsProcessed)
	})

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.inference_requests_processed").Inc()
		e.metrics.Histogram("forge.ai.inference_latency").Observe(latency.Seconds())
	}

	return response, nil
}

// BatchInfer performs inference on multiple requests
func (e *InferenceEngine) BatchInfer(ctx context.Context, requests []InferenceRequest) ([]InferenceResponse, error) {
	if !e.started {
		return nil, fmt.Errorf("inference engine not started")
	}

	if len(requests) == 0 {
		return []InferenceResponse{}, nil
	}

	// If batching is disabled, process requests individually
	if e.batcher == nil {
		responses := make([]InferenceResponse, len(requests))
		for i, request := range requests {
			response, err := e.Infer(ctx, request)
			if err != nil {
				response = InferenceResponse{
					ID:        request.ID,
					ModelID:   request.ModelID,
					Error:     err,
					Timestamp: time.Now(),
				}
			}
			responses[i] = response
		}
		return responses, nil
	}

	// Process through batcher
	return e.batcher.ProcessBatch(ctx, requests)
}

// Scale scales the inference engine workers
func (e *InferenceEngine) Scale(ctx context.Context, replicas int) error {
	if e.scaler == nil {
		return fmt.Errorf("scaling not enabled")
	}

	return e.scaler.Scale(ctx, replicas)
}

// GetStats returns inference engine statistics
func (e *InferenceEngine) GetStats() InferenceStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := e.stats
	stats.ActiveWorkers = len(e.workers)
	stats.LastUpdated = time.Now()

	// Calculate rates
	if stats.RequestsReceived > 0 {
		stats.ErrorRate = float64(stats.RequestsError) / float64(stats.RequestsReceived)
	}
	if stats.CacheHits+stats.CacheMisses > 0 {
		stats.CacheHitRate = float64(stats.CacheHits) / float64(stats.CacheHits+stats.CacheMisses)
	}
	if stats.BatchesProcessed > 0 {
		stats.AverageBatchSize = float64(stats.RequestsProcessed) / float64(stats.BatchesProcessed)
	}

	return stats
}

// GetHealth returns the health status of the inference engine
func (e *InferenceEngine) GetHealth() InferenceHealth {
	e.mu.RLock()
	defer e.mu.RUnlock()

	health := e.health
	health.CheckedAt = time.Now()
	health.Details = map[string]interface{}{
		"started":        e.started,
		"active_workers": len(e.workers),
		"models_loaded":  len(e.models),
		"error_rate":     e.stats.ErrorRate,
	}

	// Update health status based on current state
	if !e.started {
		health.Status = InferenceHealthStatusUnknown
		health.Message = "Inference engine not started"
	} else if e.stats.ErrorRate > 0.5 {
		health.Status = InferenceHealthStatusUnhealthy
		health.Message = "High error rate detected"
	} else if e.stats.ErrorRate > 0.1 {
		health.Status = InferenceHealthStatusDegraded
		health.Message = "Elevated error rate"
	} else {
		health.Status = InferenceHealthStatusHealthy
		health.Message = "Inference engine operating normally"
		health.LastHealthy = time.Now()
	}

	return health
}

// updateStats updates the inference engine statistics
func (e *InferenceEngine) updateStats(fn func(*InferenceStats)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn(&e.stats)
}

// runStatsCollection runs the statistics collection loop
func (e *InferenceEngine) runStatsCollection(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.shutdownC:
			return
		case <-ticker.C:
			e.collectStats()
		}
	}
}

// collectStats collects and updates statistics
func (e *InferenceEngine) collectStats() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Update throughput
	if e.stats.RequestsProcessed > 0 {
		duration := time.Since(e.stats.LastUpdated)
		if duration > 0 {
			e.stats.ThroughputPerSec = float64(e.stats.RequestsProcessed) / duration.Seconds()
		}
	}

	// Update queue size if batcher is enabled
	if e.batcher != nil {
		e.stats.QueueSize = e.batcher.QueueSize()
	}

	e.stats.LastUpdated = time.Now()
}

// GetPoolStats returns object pool statistics
func (e *InferenceEngine) GetPoolStats() PoolStats {
	return e.pool.GetStats()
}

// ResetPoolStats resets object pool statistics
func (e *InferenceEngine) ResetPoolStats() {
	e.pool.ResetStats()
}
