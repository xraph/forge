package inference

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/models"
	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/logger"
)

// InferencePipeline handles optimized inference processing.
type InferencePipeline struct {
	config         InferencePipelineConfig
	stages         []PipelineStage
	preprocessors  []Preprocessor
	postprocessors []Postprocessor
	interceptors   []PipelineInterceptor
	requestQueue   chan InferenceRequest
	responseQueue  chan InferenceResponse
	workerPool     *WorkerPool
	stats          PipelineStats
	started        bool
	mu             sync.RWMutex
	logger         logger.Logger
	metrics        forge.Metrics
	shutdownC      chan struct{}
	wg             sync.WaitGroup
}

// InferencePipelineConfig contains configuration for the inference pipeline.
type InferencePipelineConfig struct {
	MaxConcurrency     int           `default:"10"   yaml:"max_concurrency"`
	QueueSize          int           `default:"1000" yaml:"queue_size"`
	Timeout            time.Duration `default:"30s"  yaml:"timeout"`
	EnableOptimization bool          `default:"true" yaml:"enable_optimization"`
	OptimizationLevel  int           `default:"2"    yaml:"optimization_level"`
	BatchingEnabled    bool          `default:"true" yaml:"batching_enabled"`
	CachingEnabled     bool          `default:"true" yaml:"caching_enabled"`
	Logger             logger.Logger `yaml:"-"`
	Metrics            forge.Metrics `yaml:"-"`
}

// PipelineStage represents a stage in the inference pipeline.
type PipelineStage interface {
	Name() string
	Process(ctx context.Context, request InferenceRequest) (InferenceRequest, error)
	Initialize(ctx context.Context, config map[string]any) error
	GetStats() StageStats
}

// Preprocessor handles input preprocessing.
type Preprocessor interface {
	Name() string
	Process(ctx context.Context, input models.ModelInput) (models.ModelInput, error)
	SupportedTypes() []models.ModelType
}

// Postprocessor handles output postprocessing.
type Postprocessor interface {
	Name() string
	Process(ctx context.Context, output models.ModelOutput) (models.ModelOutput, error)
	SupportedTypes() []models.ModelType
}

// PipelineInterceptor can intercept and modify requests/responses.
type PipelineInterceptor interface {
	Name() string
	BeforeInference(ctx context.Context, request *InferenceRequest) error
	AfterInference(ctx context.Context, request *InferenceRequest, response *InferenceResponse) error
}

// WorkerPool manages a pool of inference workers.
type WorkerPool struct {
	workers     []*PipelineWorker
	requestChan chan InferenceRequest
	maxWorkers  int
	activeCount int
	mu          sync.RWMutex
}

// PipelineWorker processes inference requests in the pipeline.
type PipelineWorker struct {
	id       string
	pipeline *InferencePipeline
	stats    WorkerStats
	active   bool
	mu       sync.RWMutex
}

// PipelineStats contains statistics for the inference pipeline.
type PipelineStats struct {
	RequestsReceived  int64                 `json:"requests_received"`
	RequestsProcessed int64                 `json:"requests_processed"`
	RequestsError     int64                 `json:"requests_error"`
	AverageLatency    time.Duration         `json:"average_latency"`
	TotalLatency      time.Duration         `json:"total_latency"`
	QueueSize         int                   `json:"queue_size"`
	ActiveWorkers     int                   `json:"active_workers"`
	StagesStats       map[string]StageStats `json:"stages_stats"`
	LastUpdated       time.Time             `json:"last_updated"`
}

// StageStats contains statistics for a pipeline stage.
type StageStats struct {
	Name              string        `json:"name"`
	RequestsProcessed int64         `json:"requests_processed"`
	RequestsError     int64         `json:"requests_error"`
	AverageLatency    time.Duration `json:"average_latency"`
	TotalLatency      time.Duration `json:"total_latency"`
	ErrorRate         float64       `json:"error_rate"`
	LastUpdated       time.Time     `json:"last_updated"`
}

// WorkerStats contains statistics for a pipeline worker.
type WorkerStats struct {
	WorkerID          string        `json:"worker_id"`
	RequestsProcessed int64         `json:"requests_processed"`
	RequestsError     int64         `json:"requests_error"`
	AverageLatency    time.Duration `json:"average_latency"`
	TotalLatency      time.Duration `json:"total_latency"`
	IsActive          bool          `json:"is_active"`
	LastProcessed     time.Time     `json:"last_processed"`
}

// NewInferencePipeline creates a new inference pipeline.
func NewInferencePipeline(config InferencePipelineConfig) (*InferencePipeline, error) {
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 10
	}

	if config.QueueSize <= 0 {
		config.QueueSize = 1000
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	pipeline := &InferencePipeline{
		config:         config,
		stages:         make([]PipelineStage, 0),
		preprocessors:  make([]Preprocessor, 0),
		postprocessors: make([]Postprocessor, 0),
		interceptors:   make([]PipelineInterceptor, 0),
		requestQueue:   make(chan InferenceRequest, config.QueueSize),
		responseQueue:  make(chan InferenceResponse, config.QueueSize),
		stats:          PipelineStats{StagesStats: make(map[string]StageStats)},
		logger:         config.Logger,
		metrics:        config.Metrics,
		shutdownC:      make(chan struct{}),
	}

	// Create worker pool
	workerPool := &WorkerPool{
		workers:     make([]*PipelineWorker, 0),
		requestChan: pipeline.requestQueue,
		maxWorkers:  config.MaxConcurrency,
	}
	pipeline.workerPool = workerPool

	// Initialize default stages
	if err := pipeline.initializeDefaultStages(); err != nil {
		return nil, fmt.Errorf("failed to initialize default stages: %w", err)
	}

	return pipeline, nil
}

// Start starts the inference pipeline.
func (p *InferencePipeline) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return errors.New("inference pipeline already started")
	}

	// Initialize all stages
	for _, stage := range p.stages {
		if err := stage.Initialize(ctx, map[string]any{}); err != nil {
			return fmt.Errorf("failed to initialize stage %s: %w", stage.Name(), err)
		}
	}

	// Start worker pool
	for i := range p.config.MaxConcurrency {
		worker := &PipelineWorker{
			id:       fmt.Sprintf("pipeline-worker-%d", i),
			pipeline: p,
			active:   true,
		}
		p.workerPool.workers = append(p.workerPool.workers, worker)

		p.wg.Add(1)

		go func(w *PipelineWorker) {
			defer p.wg.Done()

			w.run(ctx)
		}(worker)
	}

	// Start stats collection

	p.wg.Go(func() {

		p.runStatsCollection(ctx)
	})

	p.started = true

	if p.logger != nil {
		p.logger.Info("inference pipeline started",
			logger.Int("max_concurrency", p.config.MaxConcurrency),
			logger.Int("queue_size", p.config.QueueSize),
			logger.Int("stages", len(p.stages)),
			logger.Int("preprocessors", len(p.preprocessors)),
			logger.Int("postprocessors", len(p.postprocessors)),
		)
	}

	if p.metrics != nil {
		p.metrics.Counter("forge.ai.inference_pipeline_started").Inc()
	}

	return nil
}

// Stop stops the inference pipeline.
func (p *InferencePipeline) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return errors.New("inference pipeline not started")
	}

	// Signal shutdown
	close(p.shutdownC)

	// Wait for workers to finish
	p.wg.Wait()

	p.started = false

	if p.logger != nil {
		p.logger.Info("inference pipeline stopped")
	}

	if p.metrics != nil {
		p.metrics.Counter("forge.ai.inference_pipeline_stopped").Inc()
	}

	return nil
}

// Process processes an inference request through the pipeline.
func (p *InferencePipeline) Process(ctx context.Context, request InferenceRequest) (InferenceResponse, error) {
	if !p.started {
		return InferenceResponse{}, errors.New("inference pipeline not started")
	}

	startTime := time.Now()

	// Update request count
	p.updateStats(func(stats *PipelineStats) {
		stats.RequestsReceived++
	})

	// Run interceptors before inference
	for _, interceptor := range p.interceptors {
		if err := interceptor.BeforeInference(ctx, &request); err != nil {
			return InferenceResponse{}, fmt.Errorf("interceptor %s failed: %w", interceptor.Name(), err)
		}
	}

	// Process through stages
	processedRequest := request

	for _, stage := range p.stages {
		stageStart := time.Now()

		var err error

		processedRequest, err = stage.Process(ctx, processedRequest)
		if err != nil {
			p.updateStats(func(stats *PipelineStats) {
				stats.RequestsError++
			})

			return InferenceResponse{}, fmt.Errorf("stage %s failed: %w", stage.Name(), err)
		}

		// Update stage stats
		stageLatency := time.Since(stageStart)
		p.updateStageStats(stage.Name(), stageLatency, nil)
	}

	// Perform actual inference
	response, err := p.performInference(ctx, processedRequest)
	if err != nil {
		p.updateStats(func(stats *PipelineStats) {
			stats.RequestsError++
		})

		return InferenceResponse{}, fmt.Errorf("inference failed: %w", err)
	}

	// Run interceptors after inference
	for _, interceptor := range p.interceptors {
		if err := interceptor.AfterInference(ctx, &processedRequest, &response); err != nil {
			if p.logger != nil {
				p.logger.Warn("interceptor after inference failed",
					logger.String("interceptor", interceptor.Name()),
					logger.Error(err),
				)
			}
		}
	}

	// Update stats
	latency := time.Since(startTime)

	p.updateStats(func(stats *PipelineStats) {
		stats.RequestsProcessed++
		stats.TotalLatency += latency
		stats.AverageLatency = stats.TotalLatency / time.Duration(stats.RequestsProcessed)
	})

	if p.metrics != nil {
		p.metrics.Counter("forge.ai.inference_pipeline_processed").Inc()
		p.metrics.Histogram("forge.ai.inference_pipeline_latency").Observe(latency.Seconds())
	}

	return response, nil
}

// AddStage adds a stage to the pipeline.
func (p *InferencePipeline) AddStage(stage PipelineStage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return errors.New("cannot add stage to running pipeline")
	}

	p.stages = append(p.stages, stage)
	p.stats.StagesStats[stage.Name()] = StageStats{Name: stage.Name()}

	if p.logger != nil {
		p.logger.Info("stage added to pipeline",
			logger.String("stage", stage.Name()),
		)
	}

	return nil
}

// AddPreprocessor adds a preprocessor to the pipeline.
func (p *InferencePipeline) AddPreprocessor(preprocessor Preprocessor) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return errors.New("cannot add preprocessor to running pipeline")
	}

	p.preprocessors = append(p.preprocessors, preprocessor)

	if p.logger != nil {
		p.logger.Info("preprocessor added to pipeline",
			logger.String("preprocessor", preprocessor.Name()),
		)
	}

	return nil
}

// AddPostprocessor adds a postprocessor to the pipeline.
func (p *InferencePipeline) AddPostprocessor(postprocessor Postprocessor) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return errors.New("cannot add postprocessor to running pipeline")
	}

	p.postprocessors = append(p.postprocessors, postprocessor)

	if p.logger != nil {
		p.logger.Info("postprocessor added to pipeline",
			logger.String("postprocessor", postprocessor.Name()),
		)
	}

	return nil
}

// AddInterceptor adds an interceptor to the pipeline.
func (p *InferencePipeline) AddInterceptor(interceptor PipelineInterceptor) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return errors.New("cannot add interceptor to running pipeline")
	}

	p.interceptors = append(p.interceptors, interceptor)

	if p.logger != nil {
		p.logger.Info("interceptor added to pipeline",
			logger.String("interceptor", interceptor.Name()),
		)
	}

	return nil
}

// GetStats returns pipeline statistics.
func (p *InferencePipeline) GetStats() PipelineStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := p.stats
	stats.QueueSize = len(p.requestQueue)
	stats.ActiveWorkers = len(p.workerPool.workers)
	stats.LastUpdated = time.Now()

	return stats
}

// performInference performs the actual inference.
func (p *InferencePipeline) performInference(ctx context.Context, request InferenceRequest) (InferenceResponse, error) {
	// This is a simplified implementation - in practice, this would delegate to the actual model
	// For now, we'll create a mock response
	response := InferenceResponse{
		ID:         request.ID,
		ModelID:    request.ModelID,
		Output:     models.ModelOutput{Metadata: map[string]any{"result": "processed"}},
		Latency:    time.Since(request.Timestamp),
		Confidence: 0.95,
		ModelInfo: models.ModelInfo{
			ID:      request.ModelID,
			Name:    request.ModelID,
			Version: "1.0.0",
		},
		Metadata:  map[string]any{"pipeline": "inference"},
		Timestamp: time.Now(),
		FromCache: false,
	}

	return response, nil
}

// initializeDefaultStages initializes default pipeline stages.
func (p *InferencePipeline) initializeDefaultStages() error {
	// Add default preprocessing stage
	preprocessingStage := &PreprocessingStage{
		name:          "preprocessing",
		preprocessors: p.preprocessors,
		logger:        p.logger,
	}
	p.stages = append(p.stages, preprocessingStage)

	// Add default inference stage
	inferenceStage := &InferenceStage{
		name:   "inference",
		logger: p.logger,
	}
	p.stages = append(p.stages, inferenceStage)

	// Add default postprocessing stage
	postprocessingStage := &PostprocessingStage{
		name:           "postprocessing",
		postprocessors: p.postprocessors,
		logger:         p.logger,
	}
	p.stages = append(p.stages, postprocessingStage)

	return nil
}

// updateStats updates pipeline statistics.
func (p *InferencePipeline) updateStats(fn func(*PipelineStats)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	fn(&p.stats)
}

// updateStageStats updates stage statistics.
func (p *InferencePipeline) updateStageStats(stageName string, latency time.Duration, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats, exists := p.stats.StagesStats[stageName]
	if !exists {
		stats = StageStats{Name: stageName}
	}

	stats.RequestsProcessed++
	stats.TotalLatency += latency
	stats.AverageLatency = stats.TotalLatency / time.Duration(stats.RequestsProcessed)

	if err != nil {
		stats.RequestsError++
	}

	if stats.RequestsProcessed > 0 {
		stats.ErrorRate = float64(stats.RequestsError) / float64(stats.RequestsProcessed)
	}

	stats.LastUpdated = time.Now()
	p.stats.StagesStats[stageName] = stats
}

// runStatsCollection runs the statistics collection loop.
func (p *InferencePipeline) runStatsCollection(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.shutdownC:
			return
		case <-ticker.C:
			p.collectStats()
		}
	}
}

// collectStats collects and updates statistics.
func (p *InferencePipeline) collectStats() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update queue size
	p.stats.QueueSize = len(p.requestQueue)

	// Update active workers
	activeWorkers := 0

	for _, worker := range p.workerPool.workers {
		if worker.active {
			activeWorkers++
		}
	}

	p.stats.ActiveWorkers = activeWorkers

	p.stats.LastUpdated = time.Now()
}

// run runs the pipeline worker.
func (w *PipelineWorker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request, ok := <-w.pipeline.requestQueue:
			if !ok {
				return
			}

			w.processRequest(ctx, request)
		}
	}
}

// processRequest processes a single request.
func (w *PipelineWorker) processRequest(ctx context.Context, request InferenceRequest) {
	startTime := time.Now()

	w.mu.Lock()
	w.stats.RequestsProcessed++
	w.stats.LastProcessed = startTime
	w.mu.Unlock()

	// Process the request
	response, err := w.pipeline.Process(ctx, request)
	if err != nil {
		w.mu.Lock()
		w.stats.RequestsError++
		w.mu.Unlock()

		response = InferenceResponse{
			ID:        request.ID,
			ModelID:   request.ModelID,
			Error:     err,
			Timestamp: time.Now(),
		}
	}

	// Update worker stats
	latency := time.Since(startTime)

	w.mu.Lock()
	w.stats.TotalLatency += latency
	w.stats.AverageLatency = w.stats.TotalLatency / time.Duration(w.stats.RequestsProcessed)
	w.mu.Unlock()

	// Send response if callback is provided
	if request.Callback != nil {
		request.Callback(response)
	}
}

// GetStats returns worker statistics.
func (w *PipelineWorker) GetStats() WorkerStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	stats := w.stats
	stats.IsActive = w.active

	return stats
}

// Default stage implementations

// PreprocessingStage handles preprocessing.
type PreprocessingStage struct {
	name          string
	preprocessors []Preprocessor
	stats         StageStats
	logger        logger.Logger
	mu            sync.RWMutex
}

func (s *PreprocessingStage) Name() string {
	return s.name
}

func (s *PreprocessingStage) Process(ctx context.Context, request InferenceRequest) (InferenceRequest, error) {
	// Apply preprocessors
	for _, preprocessor := range s.preprocessors {
		var err error

		request.Input, err = preprocessor.Process(ctx, request.Input)
		if err != nil {
			return request, fmt.Errorf("preprocessor %s failed: %w", preprocessor.Name(), err)
		}
	}

	return request, nil
}

func (s *PreprocessingStage) Initialize(ctx context.Context, config map[string]any) error {
	s.stats = StageStats{Name: s.name}

	return nil
}

func (s *PreprocessingStage) GetStats() StageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stats
}

// InferenceStage handles the main inference.
type InferenceStage struct {
	name   string
	stats  StageStats
	logger logger.Logger
	mu     sync.RWMutex
}

func (s *InferenceStage) Name() string {
	return s.name
}

func (s *InferenceStage) Process(ctx context.Context, request InferenceRequest) (InferenceRequest, error) {
	// This is where the actual model inference would happen
	// For now, we just pass the request through
	return request, nil
}

func (s *InferenceStage) Initialize(ctx context.Context, config map[string]any) error {
	s.stats = StageStats{Name: s.name}

	return nil
}

func (s *InferenceStage) GetStats() StageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stats
}

// PostprocessingStage handles postprocessing.
type PostprocessingStage struct {
	name           string
	postprocessors []Postprocessor
	stats          StageStats
	logger         logger.Logger
	mu             sync.RWMutex
}

func (s *PostprocessingStage) Name() string {
	return s.name
}

func (s *PostprocessingStage) Process(ctx context.Context, request InferenceRequest) (InferenceRequest, error) {
	// Apply postprocessors (we would need the output here, but this is a simplified implementation)
	return request, nil
}

func (s *PostprocessingStage) Initialize(ctx context.Context, config map[string]any) error {
	s.stats = StageStats{Name: s.name}

	return nil
}

func (s *PostprocessingStage) GetStats() StageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stats
}
