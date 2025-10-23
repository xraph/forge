package inference

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// InferenceScaler handles auto-scaling of inference workers
type InferenceScaler struct {
	config        InferenceScalerConfig
	workers       map[string]*InferenceWorker
	metrics       *ScalingMetrics
	strategies    []ScalingStrategy
	currentScale  int
	targetScale   int
	lastScaleTime time.Time
	stats         ScalerStats
	started       bool
	mu            sync.RWMutex
	logger        common.Logger
	metricsCol    common.Metrics
	shutdownC     chan struct{}
	wg            sync.WaitGroup
}

// InferenceScalerConfig contains configuration for inference scaling
type InferenceScalerConfig struct {
	MinWorkers       int               `yaml:"min_workers" default:"2"`
	MaxWorkers       int               `yaml:"max_workers" default:"20"`
	ScalingThreshold float64           `yaml:"scaling_threshold" default:"0.8"`
	ScalingCooldown  time.Duration     `yaml:"scaling_cooldown" default:"2m"`
	MetricsInterval  time.Duration     `yaml:"metrics_interval" default:"30s"`
	ScalingInterval  time.Duration     `yaml:"scaling_interval" default:"1m"`
	ScaleUpFactor    float64           `yaml:"scale_up_factor" default:"1.5"`
	ScaleDownFactor  float64           `yaml:"scale_down_factor" default:"0.7"`
	Strategy         string            `yaml:"strategy" default:"cpu_memory"`
	CustomStrategies []ScalingStrategy `yaml:"-"`
	ResourceLimits   ResourceLimits    `yaml:"resource_limits"`
	Logger           common.Logger     `yaml:"-"`
	Metrics          common.Metrics    `yaml:"-"`
}

// ResourceLimits defines resource limits for scaling
type ResourceLimits struct {
	MaxCPU     float64 `yaml:"max_cpu" default:"8.0"`
	MaxMemory  int64   `yaml:"max_memory" default:"8589934592"` // 8GB
	MaxGPU     int     `yaml:"max_gpu" default:"1"`
	MaxNetwork int64   `yaml:"max_network" default:"1073741824"` // 1GB/s
}

// ScalingMetrics contains metrics used for scaling decisions
type ScalingMetrics struct {
	CPUUsage          float64       `json:"cpu_usage"`
	MemoryUsage       float64       `json:"memory_usage"`
	GPUUsage          float64       `json:"gpu_usage"`
	NetworkUsage      float64       `json:"network_usage"`
	QueueSize         int           `json:"queue_size"`
	AverageLatency    time.Duration `json:"average_latency"`
	ThroughputPerSec  float64       `json:"throughput_per_sec"`
	ErrorRate         float64       `json:"error_rate"`
	ActiveWorkers     int           `json:"active_workers"`
	PendingRequests   int           `json:"pending_requests"`
	CompletedRequests int64         `json:"completed_requests"`
	LastUpdated       time.Time     `json:"last_updated"`
}

// ScalerStats contains statistics for the scaler
type ScalerStats struct {
	ScaleUpEvents       int64              `json:"scale_up_events"`
	ScaleDownEvents     int64              `json:"scale_down_events"`
	CurrentWorkers      int                `json:"current_workers"`
	TargetWorkers       int                `json:"target_workers"`
	MinWorkers          int                `json:"min_workers"`
	MaxWorkers          int                `json:"max_workers"`
	LastScaleTime       time.Time          `json:"last_scale_time"`
	TotalScaleTime      time.Duration      `json:"total_scale_time"`
	AverageScaleTime    time.Duration      `json:"average_scale_time"`
	ScalingDecisions    int64              `json:"scaling_decisions"`
	ResourceUtilization map[string]float64 `json:"resource_utilization"`
	LastUpdated         time.Time          `json:"last_updated"`
}

// ScalingStrategy defines a strategy for scaling decisions
type ScalingStrategy interface {
	Name() string
	ShouldScale(metrics *ScalingMetrics, config InferenceScalerConfig) ScalingDecision
	Priority() int
	Initialize(ctx context.Context, config map[string]interface{}) error
}

// ScalingDecision represents a scaling decision
type ScalingDecision struct {
	Action      ScalingAction          `json:"action"`
	TargetScale int                    `json:"target_scale"`
	Confidence  float64                `json:"confidence"`
	Reason      string                 `json:"reason"`
	Metrics     map[string]interface{} `json:"metrics"`
	Strategy    string                 `json:"strategy"`
	Priority    int                    `json:"priority"`
	Timestamp   time.Time              `json:"timestamp"`
}

// ScalingAction represents the type of scaling action
type ScalingAction string

const (
	ScalingActionNone      ScalingAction = "none"
	ScalingActionScaleUp   ScalingAction = "scale_up"
	ScalingActionScaleDown ScalingAction = "scale_down"
)

// InferenceWorker represents an inference worker
type InferenceWorker struct {
	id         string
	config     InferenceWorkerConfig
	status     WorkerStatus
	metrics    WorkerMetrics
	startTime  time.Time
	lastActive time.Time
	mu         sync.RWMutex
}

// InferenceWorkerConfig contains configuration for an inference worker
type InferenceWorkerConfig struct {
	ID       string
	Engine   *InferenceEngine
	Pipeline *InferencePipeline
	Logger   common.Logger
	Metrics  common.Metrics
}

// WorkerStatus represents the status of a worker
type WorkerStatus string

const (
	WorkerStatusIdle     WorkerStatus = "idle"
	WorkerStatusBusy     WorkerStatus = "busy"
	WorkerStatusStarting WorkerStatus = "starting"
	WorkerStatusStopping WorkerStatus = "stopping"
	WorkerStatusStopped  WorkerStatus = "stopped"
	WorkerStatusError    WorkerStatus = "error"
)

// WorkerMetrics contains metrics for a worker
type WorkerMetrics struct {
	RequestsProcessed int64         `json:"requests_processed"`
	RequestsError     int64         `json:"requests_error"`
	AverageLatency    time.Duration `json:"average_latency"`
	TotalLatency      time.Duration `json:"total_latency"`
	CPUUsage          float64       `json:"cpu_usage"`
	MemoryUsage       float64       `json:"memory_usage"`
	IsActive          bool          `json:"is_active"`
	LastProcessed     time.Time     `json:"last_processed"`
}

// NewInferenceScaler creates a new inference scaler
func NewInferenceScaler(config InferenceScalerConfig) (*InferenceScaler, error) {
	if config.MinWorkers <= 0 {
		config.MinWorkers = 2
	}
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = 20
	}
	if config.ScalingThreshold <= 0 {
		config.ScalingThreshold = 0.8
	}
	if config.ScalingCooldown == 0 {
		config.ScalingCooldown = 2 * time.Minute
	}
	if config.MetricsInterval == 0 {
		config.MetricsInterval = 30 * time.Second
	}
	if config.ScalingInterval == 0 {
		config.ScalingInterval = time.Minute
	}
	if config.ScaleUpFactor <= 0 {
		config.ScaleUpFactor = 1.5
	}
	if config.ScaleDownFactor <= 0 {
		config.ScaleDownFactor = 0.7
	}

	scaler := &InferenceScaler{
		config:        config,
		workers:       make(map[string]*InferenceWorker),
		metrics:       &ScalingMetrics{},
		strategies:    make([]ScalingStrategy, 0),
		currentScale:  config.MinWorkers,
		targetScale:   config.MinWorkers,
		lastScaleTime: time.Now(),
		stats:         ScalerStats{ResourceUtilization: make(map[string]float64)},
		logger:        config.Logger,
		metricsCol:    config.Metrics,
		shutdownC:     make(chan struct{}),
	}

	// Initialize default strategies
	if err := scaler.initializeDefaultStrategies(); err != nil {
		return nil, fmt.Errorf("failed to initialize default strategies: %w", err)
	}

	// Add custom strategies
	for _, strategy := range config.CustomStrategies {
		scaler.strategies = append(scaler.strategies, strategy)
	}

	return scaler, nil
}

// Start starts the inference scaler
func (s *InferenceScaler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("inference scaler already started")
	}

	// Initialize strategies
	for _, strategy := range s.strategies {
		if err := strategy.Initialize(ctx, map[string]interface{}{}); err != nil {
			return fmt.Errorf("failed to initialize strategy %s: %w", strategy.Name(), err)
		}
	}

	// Start metrics collection
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runMetricsCollection(ctx)
	}()

	// Start scaling loop
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runScalingLoop(ctx)
	}()

	// Start stats collection
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runStatsCollection(ctx)
	}()

	s.started = true

	if s.logger != nil {
		s.logger.Info("inference scaler started",
			logger.Int("min_workers", s.config.MinWorkers),
			logger.Int("max_workers", s.config.MaxWorkers),
			logger.Float64("scaling_threshold", s.config.ScalingThreshold),
			logger.Duration("scaling_cooldown", s.config.ScalingCooldown),
			logger.Int("strategies", len(s.strategies)),
		)
	}

	if s.metricsCol != nil {
		s.metricsCol.Counter("forge.ai.inference_scaler_started").Inc()
	}

	return nil
}

// Stop stops the inference scaler
func (s *InferenceScaler) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return fmt.Errorf("inference scaler not started")
	}

	// Signal shutdown
	close(s.shutdownC)

	// Wait for routines to finish
	s.wg.Wait()

	// Stop all workers
	for _, worker := range s.workers {
		worker.stop(ctx)
	}

	s.started = false

	if s.logger != nil {
		s.logger.Info("inference scaler stopped")
	}

	if s.metricsCol != nil {
		s.metricsCol.Counter("forge.ai.inference_scaler_stopped").Inc()
	}

	return nil
}

// Scale scales the inference workers to the target number
func (s *InferenceScaler) Scale(ctx context.Context, targetWorkers int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return fmt.Errorf("inference scaler not started")
	}

	// Validate target
	if targetWorkers < s.config.MinWorkers {
		targetWorkers = s.config.MinWorkers
	}
	if targetWorkers > s.config.MaxWorkers {
		targetWorkers = s.config.MaxWorkers
	}

	// Check cooldown
	if time.Since(s.lastScaleTime) < s.config.ScalingCooldown {
		return fmt.Errorf("scaling cooldown not yet elapsed")
	}

	currentWorkers := len(s.workers)
	if targetWorkers == currentWorkers {
		return nil
	}

	startTime := time.Now()
	var err error

	if targetWorkers > currentWorkers {
		// Scale up
		err = s.scaleUp(ctx, targetWorkers-currentWorkers)
		if err == nil {
			s.stats.ScaleUpEvents++
		}
	} else {
		// Scale down
		err = s.scaleDown(ctx, currentWorkers-targetWorkers)
		if err == nil {
			s.stats.ScaleDownEvents++
		}
	}

	if err != nil {
		return err
	}

	// Update scaling statistics
	scaleTime := time.Since(startTime)
	s.stats.TotalScaleTime += scaleTime
	s.stats.ScalingDecisions++
	s.stats.AverageScaleTime = s.stats.TotalScaleTime / time.Duration(s.stats.ScalingDecisions)
	s.stats.LastScaleTime = time.Now()
	s.lastScaleTime = time.Now()
	s.currentScale = targetWorkers

	if s.logger != nil {
		s.logger.Info("scaling completed",
			logger.Int("previous_workers", currentWorkers),
			logger.Int("target_workers", targetWorkers),
			logger.Duration("scale_time", scaleTime),
		)
	}

	if s.metricsCol != nil {
		s.metricsCol.Counter("forge.ai.inference_scaling_events").Inc()
		s.metricsCol.Histogram("forge.ai.inference_scaling_time").Observe(scaleTime.Seconds())
		s.metricsCol.Gauge("forge.ai.inference_active_workers").Set(float64(targetWorkers))
	}

	return nil
}

// GetStats returns scaler statistics
func (s *InferenceScaler) GetStats() ScalerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := s.stats
	stats.CurrentWorkers = len(s.workers)
	stats.TargetWorkers = s.targetScale
	stats.MinWorkers = s.config.MinWorkers
	stats.MaxWorkers = s.config.MaxWorkers
	stats.LastUpdated = time.Now()

	return stats
}

// GetMetrics returns current scaling metrics
func (s *InferenceScaler) GetMetrics() *ScalingMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.metrics
}

// scaleUp adds workers to the pool
func (s *InferenceScaler) scaleUp(ctx context.Context, count int) error {
	for i := 0; i < count; i++ {
		workerID := fmt.Sprintf("worker-%d-%d", time.Now().UnixNano(), i)
		worker := NewInferenceWorker(InferenceWorkerConfig{
			ID:      workerID,
			Logger:  s.logger,
			Metrics: s.metricsCol,
		})

		if err := worker.start(ctx); err != nil {
			return fmt.Errorf("failed to start worker %s: %w", workerID, err)
		}

		s.workers[workerID] = worker
	}

	return nil
}

// scaleDown removes workers from the pool
func (s *InferenceScaler) scaleDown(ctx context.Context, count int) error {
	// Select workers to remove (prefer idle workers)
	workersToRemove := make([]*InferenceWorker, 0, count)

	// First, try to remove idle workers
	for _, worker := range s.workers {
		if len(workersToRemove) >= count {
			break
		}
		if worker.status == WorkerStatusIdle {
			workersToRemove = append(workersToRemove, worker)
		}
	}

	// If not enough idle workers, remove any workers
	if len(workersToRemove) < count {
		for _, worker := range s.workers {
			if len(workersToRemove) >= count {
				break
			}
			found := false
			for _, w := range workersToRemove {
				if w.id == worker.id {
					found = true
					break
				}
			}
			if !found {
				workersToRemove = append(workersToRemove, worker)
			}
		}
	}

	// Remove selected workers
	for _, worker := range workersToRemove {
		if err := worker.stop(ctx); err != nil {
			if s.logger != nil {
				s.logger.Error("failed to stop worker",
					logger.String("worker_id", worker.id),
					logger.Error(err),
				)
			}
		}
		delete(s.workers, worker.id)
	}

	return nil
}

// runMetricsCollection runs the metrics collection loop
func (s *InferenceScaler) runMetricsCollection(ctx context.Context) {
	ticker := time.NewTicker(s.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownC:
			return
		case <-ticker.C:
			s.collectMetrics()
		}
	}
}

// runScalingLoop runs the scaling decision loop
func (s *InferenceScaler) runScalingLoop(ctx context.Context) {
	ticker := time.NewTicker(s.config.ScalingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownC:
			return
		case <-ticker.C:
			s.makeScalingDecision(ctx)
		}
	}
}

// runStatsCollection runs the statistics collection loop
func (s *InferenceScaler) runStatsCollection(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownC:
			return
		case <-ticker.C:
			s.collectStats()
		}
	}
}

// collectMetrics collects scaling metrics
func (s *InferenceScaler) collectMetrics() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Collect system metrics (simplified)
	s.metrics.CPUUsage = s.collectCPUUsage()
	s.metrics.MemoryUsage = s.collectMemoryUsage()
	s.metrics.GPUUsage = s.collectGPUUsage()
	s.metrics.NetworkUsage = s.collectNetworkUsage()
	s.metrics.ActiveWorkers = len(s.workers)
	s.metrics.LastUpdated = time.Now()

	// Collect worker metrics
	totalLatency := time.Duration(0)
	activeWorkers := 0
	for _, worker := range s.workers {
		if worker.status == WorkerStatusBusy {
			activeWorkers++
			totalLatency += worker.metrics.AverageLatency
		}
	}

	if activeWorkers > 0 {
		s.metrics.AverageLatency = totalLatency / time.Duration(activeWorkers)
	}
}

// makeScalingDecision makes a scaling decision based on current metrics
func (s *InferenceScaler) makeScalingDecision(ctx context.Context) {
	s.mu.RLock()
	metrics := s.metrics
	s.mu.RUnlock()

	// Get decisions from all strategies
	decisions := make([]ScalingDecision, 0, len(s.strategies))
	for _, strategy := range s.strategies {
		decision := strategy.ShouldScale(metrics, s.config)
		decision.Strategy = strategy.Name()
		decision.Priority = strategy.Priority()
		decision.Timestamp = time.Now()
		decisions = append(decisions, decision)
	}

	// Select the best decision
	bestDecision := s.selectBestDecision(decisions)
	if bestDecision.Action == ScalingActionNone {
		return
	}

	// Execute scaling decision
	if err := s.Scale(ctx, bestDecision.TargetScale); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to execute scaling decision",
				logger.String("action", string(bestDecision.Action)),
				logger.Int("target_scale", bestDecision.TargetScale),
				logger.String("strategy", bestDecision.Strategy),
				logger.Error(err),
			)
		}
	} else {
		if s.logger != nil {
			s.logger.Info("scaling decision executed",
				logger.String("action", string(bestDecision.Action)),
				logger.Int("target_scale", bestDecision.TargetScale),
				logger.String("strategy", bestDecision.Strategy),
				logger.String("reason", bestDecision.Reason),
				logger.Float64("confidence", bestDecision.Confidence),
			)
		}
	}
}

// selectBestDecision selects the best scaling decision from multiple strategies
func (s *InferenceScaler) selectBestDecision(decisions []ScalingDecision) ScalingDecision {
	if len(decisions) == 0 {
		return ScalingDecision{Action: ScalingActionNone}
	}

	// Sort by priority and confidence
	bestDecision := decisions[0]
	for _, decision := range decisions[1:] {
		if decision.Priority > bestDecision.Priority ||
			(decision.Priority == bestDecision.Priority && decision.Confidence > bestDecision.Confidence) {
			bestDecision = decision
		}
	}

	return bestDecision
}

// collectCPUUsage collects CPU usage metrics
func (s *InferenceScaler) collectCPUUsage() float64 {
	// Simplified CPU usage collection
	return 0.5 // 50% usage
}

// collectMemoryUsage collects memory usage metrics
func (s *InferenceScaler) collectMemoryUsage() float64 {
	// Simplified memory usage collection
	return 0.6 // 60% usage
}

// collectGPUUsage collects GPU usage metrics
func (s *InferenceScaler) collectGPUUsage() float64 {
	// Simplified GPU usage collection
	return 0.7 // 70% usage
}

// collectNetworkUsage collects network usage metrics
func (s *InferenceScaler) collectNetworkUsage() float64 {
	// Simplified network usage collection
	return 0.3 // 30% usage
}

// collectStats collects and updates statistics
func (s *InferenceScaler) collectStats() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stats.CurrentWorkers = len(s.workers)
	s.stats.ResourceUtilization["cpu"] = s.metrics.CPUUsage
	s.stats.ResourceUtilization["memory"] = s.metrics.MemoryUsage
	s.stats.ResourceUtilization["gpu"] = s.metrics.GPUUsage
	s.stats.ResourceUtilization["network"] = s.metrics.NetworkUsage
	s.stats.LastUpdated = time.Now()
}

// initializeDefaultStrategies initializes default scaling strategies
func (s *InferenceScaler) initializeDefaultStrategies() error {
	// CPU/Memory based strategy
	cpuMemoryStrategy := &CPUMemoryScalingStrategy{
		name:     "cpu_memory",
		priority: 100,
		logger:   s.logger,
	}
	s.strategies = append(s.strategies, cpuMemoryStrategy)

	// Queue-based strategy
	queueStrategy := &QueueBasedScalingStrategy{
		name:     "queue",
		priority: 90,
		logger:   s.logger,
	}
	s.strategies = append(s.strategies, queueStrategy)

	// Latency-based strategy
	latencyStrategy := &LatencyBasedScalingStrategy{
		name:     "latency",
		priority: 80,
		logger:   s.logger,
	}
	s.strategies = append(s.strategies, latencyStrategy)

	return nil
}

// NewInferenceWorker creates a new inference worker
func NewInferenceWorker(config InferenceWorkerConfig) *InferenceWorker {
	return &InferenceWorker{
		id:         config.ID,
		config:     config,
		status:     WorkerStatusIdle,
		metrics:    WorkerMetrics{},
		startTime:  time.Now(),
		lastActive: time.Now(),
	}
}

// start starts the inference worker
func (w *InferenceWorker) start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.status != WorkerStatusIdle && w.status != WorkerStatusStopped {
		return fmt.Errorf("worker %s already started", w.id)
	}

	w.status = WorkerStatusStarting
	w.startTime = time.Now()

	// Simulate worker startup
	time.Sleep(100 * time.Millisecond)

	w.status = WorkerStatusIdle
	w.metrics.IsActive = true

	return nil
}

// stop stops the inference worker
func (w *InferenceWorker) stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.status == WorkerStatusStopped {
		return nil
	}

	w.status = WorkerStatusStopping

	// Simulate worker shutdown
	time.Sleep(100 * time.Millisecond)

	w.status = WorkerStatusStopped
	w.metrics.IsActive = false

	return nil
}

// Run runs the inference worker
func (w *InferenceWorker) Run(ctx context.Context) {
	// Simplified worker run loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Process work
			time.Sleep(time.Millisecond)
			w.lastActive = time.Now()
		}
	}
}

// Default scaling strategies

// CPUMemoryScalingStrategy scales based on CPU and memory usage
type CPUMemoryScalingStrategy struct {
	name     string
	priority int
	logger   common.Logger
}

func (s *CPUMemoryScalingStrategy) Name() string {
	return s.name
}

func (s *CPUMemoryScalingStrategy) Priority() int {
	return s.priority
}

func (s *CPUMemoryScalingStrategy) Initialize(ctx context.Context, config map[string]interface{}) error {
	return nil
}

func (s *CPUMemoryScalingStrategy) ShouldScale(metrics *ScalingMetrics, config InferenceScalerConfig) ScalingDecision {
	avgUsage := (metrics.CPUUsage + metrics.MemoryUsage) / 2

	if avgUsage > config.ScalingThreshold {
		return ScalingDecision{
			Action:      ScalingActionScaleUp,
			TargetScale: int(float64(metrics.ActiveWorkers) * config.ScaleUpFactor),
			Confidence:  avgUsage,
			Reason:      fmt.Sprintf("High CPU/Memory usage: %.2f%%", avgUsage*100),
		}
	}

	if avgUsage < config.ScalingThreshold*0.5 {
		return ScalingDecision{
			Action:      ScalingActionScaleDown,
			TargetScale: int(float64(metrics.ActiveWorkers) * config.ScaleDownFactor),
			Confidence:  1.0 - avgUsage,
			Reason:      fmt.Sprintf("Low CPU/Memory usage: %.2f%%", avgUsage*100),
		}
	}

	return ScalingDecision{Action: ScalingActionNone}
}

// QueueBasedScalingStrategy scales based on queue size
type QueueBasedScalingStrategy struct {
	name     string
	priority int
	logger   common.Logger
}

func (s *QueueBasedScalingStrategy) Name() string {
	return s.name
}

func (s *QueueBasedScalingStrategy) Priority() int {
	return s.priority
}

func (s *QueueBasedScalingStrategy) Initialize(ctx context.Context, config map[string]interface{}) error {
	return nil
}

func (s *QueueBasedScalingStrategy) ShouldScale(metrics *ScalingMetrics, config InferenceScalerConfig) ScalingDecision {
	queueRatio := float64(metrics.QueueSize) / float64(metrics.ActiveWorkers)

	if queueRatio > 10 {
		return ScalingDecision{
			Action:      ScalingActionScaleUp,
			TargetScale: metrics.ActiveWorkers + int(queueRatio/5),
			Confidence:  0.8,
			Reason:      fmt.Sprintf("High queue size: %d requests, %d workers", metrics.QueueSize, metrics.ActiveWorkers),
		}
	}

	if queueRatio < 1 && metrics.ActiveWorkers > config.MinWorkers {
		return ScalingDecision{
			Action:      ScalingActionScaleDown,
			TargetScale: metrics.ActiveWorkers - 1,
			Confidence:  0.6,
			Reason:      fmt.Sprintf("Low queue size: %d requests, %d workers", metrics.QueueSize, metrics.ActiveWorkers),
		}
	}

	return ScalingDecision{Action: ScalingActionNone}
}

// LatencyBasedScalingStrategy scales based on latency
type LatencyBasedScalingStrategy struct {
	name     string
	priority int
	logger   common.Logger
}

func (s *LatencyBasedScalingStrategy) Name() string {
	return s.name
}

func (s *LatencyBasedScalingStrategy) Priority() int {
	return s.priority
}

func (s *LatencyBasedScalingStrategy) Initialize(ctx context.Context, config map[string]interface{}) error {
	return nil
}

func (s *LatencyBasedScalingStrategy) ShouldScale(metrics *ScalingMetrics, config InferenceScalerConfig) ScalingDecision {
	targetLatency := 500 * time.Millisecond

	if metrics.AverageLatency > targetLatency {
		return ScalingDecision{
			Action:      ScalingActionScaleUp,
			TargetScale: int(float64(metrics.ActiveWorkers) * 1.2),
			Confidence:  0.7,
			Reason:      fmt.Sprintf("High latency: %v > %v", metrics.AverageLatency, targetLatency),
		}
	}

	if metrics.AverageLatency < targetLatency/2 && metrics.ActiveWorkers > config.MinWorkers {
		return ScalingDecision{
			Action:      ScalingActionScaleDown,
			TargetScale: int(float64(metrics.ActiveWorkers) * 0.9),
			Confidence:  0.5,
			Reason:      fmt.Sprintf("Low latency: %v < %v", metrics.AverageLatency, targetLatency/2),
		}
	}

	return ScalingDecision{Action: ScalingActionNone}
}
