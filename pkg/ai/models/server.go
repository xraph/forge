package models

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ModelServer interface defines the contract for model servers
type ModelServer interface {
	// Model management
	LoadModel(ctx context.Context, config ModelConfig) (Model, error)
	UnloadModel(ctx context.Context, modelID string) error
	GetModel(modelID string) (Model, error)
	GetModels() []Model

	// Prediction operations
	Predict(ctx context.Context, modelID string, input ModelInput) (ModelOutput, error)
	BatchPredict(ctx context.Context, modelID string, inputs []ModelInput) ([]ModelOutput, error)

	// Server management
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetStats() ModelServerStats
	GetHealth() ModelServerHealth

	// Configuration
	SetConfig(config ModelServerConfig) error
	GetConfig() ModelServerConfig
}

// ModelServerConfig contains configuration for the model server
type ModelServerConfig struct {
	MaxModels           int                     `json:"max_models"`
	DefaultTimeout      time.Duration           `json:"default_timeout"`
	MaxConcurrency      int                     `json:"max_concurrency"`
	EnableBatching      bool                    `json:"enable_batching"`
	BatchSize           int                     `json:"batch_size"`
	BatchTimeout        time.Duration           `json:"batch_timeout"`
	EnableCaching       bool                    `json:"enable_caching"`
	CacheSize           int                     `json:"cache_size"`
	CacheTTL            time.Duration           `json:"cache_ttl"`
	EnableLoadBalance   bool                    `json:"enable_load_balance"`
	HealthCheckInterval time.Duration           `json:"health_check_interval"`
	Logger              common.Logger           `json:"-"`
	Metrics             common.Metrics          `json:"-"`
	Adapters            map[string]ModelAdapter `json:"-"`
}

// ModelAdapter interface for different ML frameworks
type ModelAdapter interface {
	Name() string
	Framework() MLFramework
	SupportsModel(config ModelConfig) bool
	CreateModel(config ModelConfig) (Model, error)
	ValidateConfig(config ModelConfig) error
}

// ModelServerStats contains statistics about the model server
type ModelServerStats struct {
	TotalModels      int                   `json:"total_models"`
	LoadedModels     int                   `json:"loaded_models"`
	TotalPredictions int64                 `json:"total_predictions"`
	TotalErrors      int64                 `json:"total_errors"`
	AverageLatency   time.Duration         `json:"average_latency"`
	ErrorRate        float64               `json:"error_rate"`
	ThroughputPerSec float64               `json:"throughput_per_sec"`
	MemoryUsage      int64                 `json:"memory_usage"`
	CPUUsage         float64               `json:"cpu_usage"`
	StartTime        time.Time             `json:"start_time"`
	Uptime           time.Duration         `json:"uptime"`
	ModelStats       map[string]ModelStats `json:"model_stats"`
}

// ModelStats contains statistics about a specific model
type ModelStats struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Status           ModelStatus   `json:"status"`
	LoadedAt         time.Time     `json:"loaded_at"`
	TotalPredictions int64         `json:"total_predictions"`
	TotalErrors      int64         `json:"total_errors"`
	AverageLatency   time.Duration `json:"average_latency"`
	ErrorRate        float64       `json:"error_rate"`
	LastPrediction   time.Time     `json:"last_prediction"`
	MemoryUsage      int64         `json:"memory_usage"`
	IsHealthy        bool          `json:"is_healthy"`
}

// ModelServerHealth represents the health status of the model server
type ModelServerHealth struct {
	Status      ModelHealthStatus      `json:"status"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details"`
	CheckedAt   time.Time              `json:"checked_at"`
	LastHealthy time.Time              `json:"last_healthy"`
	Uptime      time.Duration          `json:"uptime"`
	ModelHealth map[string]ModelHealth `json:"model_health"`
}

// DefaultModelServer implements the ModelServer interface
type DefaultModelServer struct {
	config    ModelServerConfig
	models    map[string]Model
	adapters  map[string]ModelAdapter
	started   bool
	startTime time.Time
	logger    common.Logger
	metrics   common.Metrics
	mu        sync.RWMutex

	// Statistics
	totalPredictions int64
	totalErrors      int64
	totalLatency     time.Duration

	// Concurrency control
	semaphore chan struct{}

	// Health monitoring
	lastHealthCheck time.Time
	healthStatus    ModelHealthStatus

	// Background tasks
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewModelServer creates a new model server
func NewModelServer(config ModelServerConfig) (ModelServer, error) {
	if config.MaxModels <= 0 {
		config.MaxModels = 10
	}
	if config.DefaultTimeout <= 0 {
		config.DefaultTimeout = 30 * time.Second
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 100
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}
	if config.BatchTimeout <= 0 {
		config.BatchTimeout = 100 * time.Millisecond
	}
	if config.HealthCheckInterval <= 0 {
		config.HealthCheckInterval = 60 * time.Second
	}

	server := &DefaultModelServer{
		config:       config,
		models:       make(map[string]Model),
		adapters:     make(map[string]ModelAdapter),
		logger:       config.Logger,
		metrics:      config.Metrics,
		semaphore:    make(chan struct{}, config.MaxConcurrency),
		stopChan:     make(chan struct{}),
		healthStatus: ModelHealthStatusUnknown,
	}

	// Register built-in adapters
	server.registerDefaultAdapters()

	// Register custom adapters
	for name, adapter := range config.Adapters {
		server.adapters[name] = adapter
	}

	return server, nil
}

// Start starts the model server
func (s *DefaultModelServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("model server already started")
	}

	s.started = true
	s.startTime = time.Now()
	s.healthStatus = ModelHealthStatusHealthy

	// Start background health monitoring
	s.wg.Add(1)
	go s.healthMonitor()

	if s.logger != nil {
		s.logger.Info("model server started",
			logger.Int("max_models", s.config.MaxModels),
			logger.Int("max_concurrency", s.config.MaxConcurrency),
			logger.Bool("batching_enabled", s.config.EnableBatching),
			logger.Bool("caching_enabled", s.config.EnableCaching),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.model_server_started").Inc()
	}

	return nil
}

// Stop stops the model server
func (s *DefaultModelServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return fmt.Errorf("model server not started")
	}

	// Stop background tasks
	close(s.stopChan)
	s.wg.Wait()

	// Unload all models
	for modelID := range s.models {
		if err := s.unloadModelInternal(ctx, modelID); err != nil {
			if s.logger != nil {
				s.logger.Error("failed to unload model during shutdown",
					logger.String("model_id", modelID),
					logger.Error(err),
				)
			}
		}
	}

	s.started = false
	s.healthStatus = ModelHealthStatusUnknown

	if s.logger != nil {
		s.logger.Info("model server stopped",
			logger.Duration("uptime", time.Since(s.startTime)),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.model_server_stopped").Inc()
	}

	return nil
}

// LoadModel loads a model
func (s *DefaultModelServer) LoadModel(ctx context.Context, config ModelConfig) (Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil, fmt.Errorf("model server not started")
	}

	// Check if model already exists
	if _, exists := s.models[config.ID]; exists {
		return nil, fmt.Errorf("model %s already loaded", config.ID)
	}

	// Check max models limit
	if len(s.models) >= s.config.MaxModels {
		return nil, fmt.Errorf("maximum number of models (%d) reached", s.config.MaxModels)
	}

	// Find appropriate adapter
	adapter, err := s.findAdapter(config)
	if err != nil {
		return nil, fmt.Errorf("failed to find adapter for model: %w", err)
	}

	// Validate configuration
	if err := adapter.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid model configuration: %w", err)
	}

	// Create model
	model, err := adapter.CreateModel(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create model: %w", err)
	}

	// Load the model
	if err := model.Load(ctx); err != nil {
		return nil, fmt.Errorf("failed to load model: %w", err)
	}

	s.models[config.ID] = model

	if s.logger != nil {
		s.logger.Info("model loaded successfully",
			logger.String("model_id", config.ID),
			logger.String("model_name", config.Name),
			logger.String("framework", string(config.Framework)),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.models_loaded").Inc()
		s.metrics.Gauge("forge.ai.active_models").Set(float64(len(s.models)))
	}

	return model, nil
}

// UnloadModel unloads a model
func (s *DefaultModelServer) UnloadModel(ctx context.Context, modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.unloadModelInternal(ctx, modelID)
}

// unloadModelInternal unloads a model (internal method, assumes lock is held)
func (s *DefaultModelServer) unloadModelInternal(ctx context.Context, modelID string) error {
	model, exists := s.models[modelID]
	if !exists {
		return fmt.Errorf("model %s not found", modelID)
	}

	// Unload the model
	if err := model.Unload(ctx); err != nil {
		return fmt.Errorf("failed to unload model: %w", err)
	}

	delete(s.models, modelID)

	if s.logger != nil {
		s.logger.Info("model unloaded successfully",
			logger.String("model_id", modelID),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.models_unloaded").Inc()
		s.metrics.Gauge("forge.ai.active_models").Set(float64(len(s.models)))
	}

	return nil
}

// GetModel retrieves a model by ID
func (s *DefaultModelServer) GetModel(modelID string) (Model, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	model, exists := s.models[modelID]
	if !exists {
		return nil, fmt.Errorf("model %s not found", modelID)
	}

	return model, nil
}

// GetModels returns all loaded models
func (s *DefaultModelServer) GetModels() []Model {
	s.mu.RLock()
	defer s.mu.RUnlock()

	models := make([]Model, 0, len(s.models))
	for _, model := range s.models {
		models = append(models, model)
	}

	return models
}

// Predict performs prediction using a specific model
func (s *DefaultModelServer) Predict(ctx context.Context, modelID string, input ModelInput) (ModelOutput, error) {
	// Acquire semaphore for concurrency control
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		return ModelOutput{}, ctx.Err()
	}

	startTime := time.Now()

	// Get model
	model, err := s.GetModel(modelID)
	if err != nil {
		s.updateErrorCount()
		return ModelOutput{}, err
	}

	// Perform prediction
	output, err := model.Predict(ctx, input)
	if err != nil {
		s.updateErrorCount()
		return ModelOutput{}, err
	}

	// Update statistics
	s.updateStats(time.Since(startTime))

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.predictions_total", "model_id", modelID).Inc()
		s.metrics.Histogram("forge.ai.prediction_duration", "model_id", modelID).Observe(time.Since(startTime).Seconds())
	}

	return output, nil
}

// BatchPredict performs batch prediction using a specific model
func (s *DefaultModelServer) BatchPredict(ctx context.Context, modelID string, inputs []ModelInput) ([]ModelOutput, error) {
	if len(inputs) == 0 {
		return []ModelOutput{}, nil
	}

	// Acquire semaphore for concurrency control
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	startTime := time.Now()

	// Get model
	model, err := s.GetModel(modelID)
	if err != nil {
		s.updateErrorCount()
		return nil, err
	}

	// Perform batch prediction
	outputs, err := model.BatchPredict(ctx, inputs)
	if err != nil {
		s.updateErrorCount()
		return nil, err
	}

	// Update statistics
	s.updateStats(time.Since(startTime))

	if s.metrics != nil {
		s.metrics.Counter("forge.ai.batch_predictions_total", "model_id", modelID).Inc()
		s.metrics.Histogram("forge.ai.batch_prediction_duration", "model_id", modelID).Observe(time.Since(startTime).Seconds())
	}

	return outputs, nil
}

// GetStats returns model server statistics
func (s *DefaultModelServer) GetStats() ModelServerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var avgLatency time.Duration
	var errorRate float64
	var throughput float64

	if s.totalPredictions > 0 {
		avgLatency = s.totalLatency / time.Duration(s.totalPredictions)
		errorRate = float64(s.totalErrors) / float64(s.totalPredictions)

		if s.started {
			duration := time.Since(s.startTime)
			if duration > 0 {
				throughput = float64(s.totalPredictions) / duration.Seconds()
			}
		}
	}

	loadedModels := 0
	modelStats := make(map[string]ModelStats)

	for id, model := range s.models {
		if model.IsLoaded() {
			loadedModels++
		}

		metrics := model.GetMetrics()
		health := model.GetHealth()

		modelStats[id] = ModelStats{
			ID:               id,
			Name:             model.Name(),
			Status:           model.(*BaseModel).GetStatus(),
			LoadedAt:         model.(*BaseModel).loadedAt,
			TotalPredictions: metrics.TotalPredictions,
			TotalErrors:      metrics.TotalErrors,
			AverageLatency:   metrics.AverageLatency,
			ErrorRate:        metrics.ErrorRate,
			LastPrediction:   metrics.LastPrediction,
			MemoryUsage:      metrics.MemoryUsage,
			IsHealthy:        health.Status == ModelHealthStatusHealthy,
		}
	}

	return ModelServerStats{
		TotalModels:      len(s.models),
		LoadedModels:     loadedModels,
		TotalPredictions: s.totalPredictions,
		TotalErrors:      s.totalErrors,
		AverageLatency:   avgLatency,
		ErrorRate:        errorRate,
		ThroughputPerSec: throughput,
		StartTime:        s.startTime,
		Uptime:           time.Since(s.startTime),
		ModelStats:       modelStats,
	}
}

// GetHealth returns model server health
func (s *DefaultModelServer) GetHealth() ModelServerHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()

	unhealthyModels := 0
	modelHealth := make(map[string]ModelHealth)

	for id, model := range s.models {
		health := model.GetHealth()
		modelHealth[id] = health

		if health.Status != ModelHealthStatusHealthy {
			unhealthyModels++
		}
	}

	// Determine overall health
	status := s.healthStatus
	message := "Model server operating normally"

	if !s.started {
		status = ModelHealthStatusUnknown
		message = "Model server not started"
	} else if unhealthyModels > len(s.models)/2 {
		status = ModelHealthStatusUnhealthy
		message = fmt.Sprintf("Too many unhealthy models: %d/%d", unhealthyModels, len(s.models))
	} else if unhealthyModels > 0 {
		status = ModelHealthStatusDegraded
		message = fmt.Sprintf("Some models are unhealthy: %d/%d", unhealthyModels, len(s.models))
	}

	return ModelServerHealth{
		Status:      status,
		Message:     message,
		CheckedAt:   time.Now(),
		LastHealthy: s.lastHealthCheck,
		Uptime:      time.Since(s.startTime),
		ModelHealth: modelHealth,
		Details: map[string]interface{}{
			"total_models":      len(s.models),
			"unhealthy_models":  unhealthyModels,
			"total_predictions": s.totalPredictions,
			"error_rate":        float64(s.totalErrors) / float64(s.totalPredictions),
		},
	}
}

// SetConfig updates the server configuration
func (s *DefaultModelServer) SetConfig(config ModelServerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = config

	// Update semaphore if concurrency changed
	if config.MaxConcurrency != len(s.semaphore) {
		s.semaphore = make(chan struct{}, config.MaxConcurrency)
	}

	return nil
}

// GetConfig returns the server configuration
func (s *DefaultModelServer) GetConfig() ModelServerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// findAdapter finds an appropriate adapter for the model
func (s *DefaultModelServer) findAdapter(config ModelConfig) (ModelAdapter, error) {
	// Try to find adapter by framework
	for _, adapter := range s.adapters {
		if adapter.Framework() == config.Framework && adapter.SupportsModel(config) {
			return adapter, nil
		}
	}

	// Try to find any adapter that supports the model
	for _, adapter := range s.adapters {
		if adapter.SupportsModel(config) {
			return adapter, nil
		}
	}

	return nil, fmt.Errorf("no adapter found for framework %s", config.Framework)
}

// registerDefaultAdapters registers default model adapters
func (s *DefaultModelServer) registerDefaultAdapters() {
	// Register built-in adapters
	// These would be implemented in separate files
	// s.adapters["tensorflow"] = NewTensorFlowAdapter()
	// s.adapters["pytorch"] = NewPyTorchAdapter()
	// s.adapters["onnx"] = NewONNXAdapter()
	// s.adapters["scikit"] = NewScikitAdapter()
	// s.adapters["huggingface"] = NewHuggingFaceAdapter()
}

// updateStats updates server statistics
func (s *DefaultModelServer) updateStats(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalPredictions++
	s.totalLatency += latency
}

// updateErrorCount updates error count
func (s *DefaultModelServer) updateErrorCount() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalErrors++
}

// healthMonitor runs periodic health checks
func (s *DefaultModelServer) healthMonitor() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.performHealthCheck()
		case <-s.stopChan:
			return
		}
	}
}

// performHealthCheck performs a health check on all models
func (s *DefaultModelServer) performHealthCheck() {
	s.mu.RLock()
	models := make([]Model, 0, len(s.models))
	for _, model := range s.models {
		models = append(models, model)
	}
	s.mu.RUnlock()

	healthyCount := 0
	for _, model := range models {
		health := model.GetHealth()
		if health.Status == ModelHealthStatusHealthy {
			healthyCount++
		}
	}

	s.mu.Lock()
	s.lastHealthCheck = time.Now()

	if len(models) == 0 {
		s.healthStatus = ModelHealthStatusUnknown
	} else if healthyCount == len(models) {
		s.healthStatus = ModelHealthStatusHealthy
	} else if healthyCount > len(models)/2 {
		s.healthStatus = ModelHealthStatusDegraded
	} else {
		s.healthStatus = ModelHealthStatusUnhealthy
	}
	s.mu.Unlock()

	if s.metrics != nil {
		s.metrics.Gauge("forge.ai.healthy_models").Set(float64(healthyCount))
		s.metrics.Gauge("forge.ai.model_server_health").Set(float64(s.healthStatusToValue(s.healthStatus)))
	}
}

// healthStatusToValue converts health status to numeric value for metrics
func (s *DefaultModelServer) healthStatusToValue(status ModelHealthStatus) int {
	switch status {
	case ModelHealthStatusHealthy:
		return 1
	case ModelHealthStatusDegraded:
		return 2
	case ModelHealthStatusUnhealthy:
		return 3
	default:
		return 0
	}
}

// GetModelsByType returns models filtered by type
func (s *DefaultModelServer) GetModelsByType(modelType ModelType) []Model {
	s.mu.RLock()
	defer s.mu.RUnlock()

	models := make([]Model, 0)
	for _, model := range s.models {
		if model.Type() == modelType {
			models = append(models, model)
		}
	}

	return models
}

// GetModelsByFramework returns models filtered by framework
func (s *DefaultModelServer) GetModelsByFramework(framework MLFramework) []Model {
	s.mu.RLock()
	defer s.mu.RUnlock()

	models := make([]Model, 0)
	for _, model := range s.models {
		if model.Framework() == framework {
			models = append(models, model)
		}
	}

	return models
}

// GetBestModel returns the best model for a given type based on health and performance
func (s *DefaultModelServer) GetBestModel(modelType ModelType) (Model, error) {
	models := s.GetModelsByType(modelType)
	if len(models) == 0 {
		return nil, fmt.Errorf("no models found for type %s", modelType)
	}

	// Sort models by health and performance
	sort.Slice(models, func(i, j int) bool {
		healthI := models[i].GetHealth()
		healthJ := models[j].GetHealth()

		// Prefer healthy models
		if healthI.Status == ModelHealthStatusHealthy && healthJ.Status != ModelHealthStatusHealthy {
			return true
		}
		if healthI.Status != ModelHealthStatusHealthy && healthJ.Status == ModelHealthStatusHealthy {
			return false
		}

		// If both have same health status, prefer lower latency
		metricsI := models[i].GetMetrics()
		metricsJ := models[j].GetMetrics()
		return metricsI.AverageLatency < metricsJ.AverageLatency
	})

	return models[0], nil
}
