package monitoring

import (
	"context"
	"fmt"
	"maps"
	"math"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	ai "github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/internal/logger"
)

// AIMetricsCollector collects and analyzes AI system performance metrics.
type AIMetricsCollector struct {
	aiManager        ai.AI
	metricsCollector forge.Metrics
	logger           forge.Logger

	// Metrics storage
	agentMetrics   map[string]*AgentMetrics
	modelMetrics   map[string]*ModelMetrics
	systemMetrics  *SystemMetrics
	historicalData *HistoricalMetrics

	// Configuration
	config           AIMetricsConfig
	collectionTicker *time.Ticker

	mu      sync.RWMutex
	started bool
}

// AIMetricsConfig contains configuration for AI metrics collection.
type AIMetricsConfig struct {
	CollectionInterval     time.Duration   `default:"30s"           yaml:"collection_interval"`
	RetentionPeriod        time.Duration   `default:"24h"           yaml:"retention_period"`
	HistoricalPoints       int             `default:"1000"          yaml:"historical_points"`
	EnablePredictive       bool            `default:"true"          yaml:"enable_predictive"`
	EnableAnomalyDetection bool            `default:"true"          yaml:"enable_anomaly_detection"`
	MetricsBatchSize       int             `default:"100"           yaml:"metrics_batch_size"`
	AlertThresholds        AlertThresholds `yaml:"alert_thresholds"`
}

// AlertThresholds defines thresholds for metric-based alerting.
type AlertThresholds struct {
	ErrorRateWarning    float64       `default:"0.05" yaml:"error_rate_warning"`
	ErrorRateCritical   float64       `default:"0.15" yaml:"error_rate_critical"`
	LatencyWarning      time.Duration `default:"1s"   yaml:"latency_warning"`
	LatencyCritical     time.Duration `default:"5s"   yaml:"latency_critical"`
	ThroughputWarning   float64       `default:"100"  yaml:"throughput_warning"`
	ThroughputCritical  float64       `default:"50"   yaml:"throughput_critical"`
	CPUUsageWarning     float64       `default:"80"   yaml:"cpu_usage_warning"`
	CPUUsageCritical    float64       `default:"95"   yaml:"cpu_usage_critical"`
	MemoryUsageWarning  float64       `default:"85"   yaml:"memory_usage_warning"`
	MemoryUsageCritical float64       `default:"95"   yaml:"memory_usage_critical"`
}

// AgentMetrics contains performance metrics for an AI agent.
type AgentMetrics struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	AgentType string `json:"agent_type"`

	// Performance metrics
	RequestCount int64   `json:"request_count"`
	ErrorCount   int64   `json:"error_count"`
	SuccessCount int64   `json:"success_count"`
	ErrorRate    float64 `json:"error_rate"`

	// Latency metrics
	AverageLatency time.Duration `json:"average_latency"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	P50Latency     time.Duration `json:"p50_latency"`
	P95Latency     time.Duration `json:"p95_latency"`
	P99Latency     time.Duration `json:"p99_latency"`

	// Throughput metrics
	RequestsPerSecond float64 `json:"requests_per_second"`
	PeakRPS           float64 `json:"peak_rps"`

	// Quality metrics
	AverageConfidence float64 `json:"average_confidence"`
	AccuracyScore     float64 `json:"accuracy_score"`

	// Learning metrics
	LearningProgress float64 `json:"learning_progress"`
	FeedbackCount    int64   `json:"feedback_count"`
	PositiveFeedback int64   `json:"positive_feedback"`
	NegativeFeedback int64   `json:"negative_feedback"`

	// Resource metrics
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`

	// Time series data
	LatencyHistory    []float64 `json:"latency_history"`
	ErrorRateHistory  []float64 `json:"error_rate_history"`
	ThroughputHistory []float64 `json:"throughput_history"`

	// Anomaly detection
	Anomalies []MetricAnomaly `json:"anomalies"`

	LastUpdated time.Time `json:"last_updated"`
}

// ModelMetrics contains performance metrics for ML models.
type ModelMetrics struct {
	ModelID   string `json:"model_id"`
	ModelName string `json:"model_name"`
	ModelType string `json:"model_type"`
	Framework string `json:"framework"`
	Version   string `json:"version"`

	// Inference metrics
	InferenceCount   int64         `json:"inference_count"`
	InferenceErrors  int64         `json:"inference_errors"`
	InferenceLatency time.Duration `json:"inference_latency"`
	BatchSize        float64       `json:"batch_size"`

	// Accuracy metrics
	Accuracy  float64 `json:"accuracy"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1Score   float64 `json:"f1_score"`

	// Resource metrics
	ModelSize       int64         `json:"model_size"`
	LoadTime        time.Duration `json:"load_time"`
	MemoryFootprint int64         `json:"memory_footprint"`
	GPUUtilization  float64       `json:"gpu_utilization"`

	// Performance trends
	AccuracyTrend string `json:"accuracy_trend"` // "improving", "stable", "degrading"
	LatencyTrend  string `json:"latency_trend"`

	LastUpdated time.Time `json:"last_updated"`
}

// SystemMetrics contains system-wide AI performance metrics.
type SystemMetrics struct {
	// Overall performance
	TotalRequests     int64         `json:"total_requests"`
	TotalErrors       int64         `json:"total_errors"`
	OverallErrorRate  float64       `json:"overall_error_rate"`
	OverallLatency    time.Duration `json:"overall_latency"`
	OverallThroughput float64       `json:"overall_throughput"`

	// Agent metrics
	TotalAgents   int `json:"total_agents"`
	ActiveAgents  int `json:"active_agents"`
	HealthyAgents int `json:"healthy_agents"`

	// Model metrics
	TotalModels  int `json:"total_models"`
	LoadedModels int `json:"loaded_models"`

	// Resource utilization
	SystemCPUUsage    float64 `json:"system_cpu_usage"`
	SystemMemoryUsage float64 `json:"system_memory_usage"`
	SystemDiskUsage   float64 `json:"system_disk_usage"`
	GPUCount          int     `json:"gpu_count"`
	GPUUtilization    float64 `json:"gpu_utilization"`

	// Capacity metrics
	CurrentCapacity     float64 `json:"current_capacity"`
	MaxCapacity         float64 `json:"max_capacity"`
	CapacityUtilization float64 `json:"capacity_utilization"`

	// Quality metrics
	OverallAccuracy   float64 `json:"overall_accuracy"`
	OverallConfidence float64 `json:"overall_confidence"`

	LastUpdated time.Time `json:"last_updated"`
}

// HistoricalMetrics stores historical performance data for analysis.
type HistoricalMetrics struct {
	DataPoints  []MetricDataPoint     `json:"data_points"`
	Trends      map[string]Trend      `json:"trends"`
	Predictions map[string]Prediction `json:"predictions"`
	maxPoints   int
	mu          sync.RWMutex
}

// MetricDataPoint represents a single point in time with multiple metrics.
type MetricDataPoint struct {
	Timestamp      time.Time      `json:"timestamp"`
	ErrorRate      float64        `json:"error_rate"`
	AverageLatency float64        `json:"average_latency"`
	Throughput     float64        `json:"throughput"`
	CPUUsage       float64        `json:"cpu_usage"`
	MemoryUsage    float64        `json:"memory_usage"`
	ActiveAgents   int            `json:"active_agents"`
	Metadata       map[string]any `json:"metadata"`
}

// Trend represents a trend in metric data.
type Trend struct {
	MetricName string         `json:"metric_name"`
	Direction  TrendDirection `json:"direction"`
	Slope      float64        `json:"slope"`
	Confidence float64        `json:"confidence"`
	StartTime  time.Time      `json:"start_time"`
	EndTime    time.Time      `json:"end_time"`
	DataPoints int            `json:"data_points"`
}

// TrendDirection represents the direction of a trend.
type TrendDirection string

const (
	TrendDirectionIncreasing TrendDirection = "increasing"
	TrendDirectionDecreasing TrendDirection = "decreasing"
	TrendDirectionStable     TrendDirection = "stable"
	TrendDirectionVolatile   TrendDirection = "volatile"
)

// Prediction represents a predicted future value.
type Prediction struct {
	MetricName       string        `json:"metric_name"`
	PredictedValue   float64       `json:"predicted_value"`
	Confidence       float64       `json:"confidence"`
	TimeHorizon      time.Duration `json:"time_horizon"`
	PredictionMethod string        `json:"prediction_method"`
	CreatedAt        time.Time     `json:"created_at"`
}

// MetricAnomaly represents an anomaly detected in metrics.
type MetricAnomaly struct {
	ID            string          `json:"id"`
	MetricName    string          `json:"metric_name"`
	AnomalyType   AnomalyType     `json:"anomaly_type"`
	Severity      AnomalySeverity `json:"severity"`
	DetectedAt    time.Time       `json:"detected_at"`
	ActualValue   float64         `json:"actual_value"`
	ExpectedValue float64         `json:"expected_value"`
	Deviation     float64         `json:"deviation"`
	Description   string          `json:"description"`
	Resolved      bool            `json:"resolved"`
	ResolvedAt    *time.Time      `json:"resolved_at,omitempty"`
}

// AnomalyType defines types of anomalies.
type AnomalyType string

const (
	AnomalyTypeSpike       AnomalyType = "spike"
	AnomalyTypeDrop        AnomalyType = "drop"
	AnomalyTypeFluctuation AnomalyType = "fluctuation"
	AnomalyTypeTrend       AnomalyType = "trend"
	AnomalyTypePattern     AnomalyType = "pattern"
)

// AnomalySeverity defines anomaly severity levels.
type AnomalySeverity string

const (
	AnomalySeverityLow      AnomalySeverity = "low"
	AnomalySeverityMedium   AnomalySeverity = "medium"
	AnomalySeverityHigh     AnomalySeverity = "high"
	AnomalySeverityCritical AnomalySeverity = "critical"
)

// AIMetricsReport contains comprehensive AI metrics analysis.
type AIMetricsReport struct {
	SystemMetrics      *SystemMetrics              `json:"system_metrics"`
	AgentMetrics       map[string]*AgentMetrics    `json:"agent_metrics"`
	ModelMetrics       map[string]*ModelMetrics    `json:"model_metrics"`
	HistoricalData     *HistoricalMetrics          `json:"historical_data"`
	Trends             map[string]Trend            `json:"trends"`
	Predictions        map[string]Prediction       `json:"predictions"`
	Anomalies          []MetricAnomaly             `json:"anomalies"`
	PerformanceSummary *PerformanceSummary         `json:"performance_summary"`
	Recommendations    []PerformanceRecommendation `json:"recommendations"`
	LastUpdated        time.Time                   `json:"last_updated"`
	ReportDuration     time.Duration               `json:"report_duration"`
}

// PerformanceSummary provides high-level performance insights.
type PerformanceSummary struct {
	OverallHealth     string            `json:"overall_health"`
	PerformanceScore  float64           `json:"performance_score"`
	EfficiencyScore   float64           `json:"efficiency_score"`
	QualityScore      float64           `json:"quality_score"`
	KeyInsights       []string          `json:"key_insights"`
	CriticalIssues    []string          `json:"critical_issues"`
	ImprovementAreas  []string          `json:"improvement_areas"`
	PerformanceTrends map[string]string `json:"performance_trends"`
}

// PerformanceRecommendation provides actionable performance recommendations.
type PerformanceRecommendation struct {
	ID                   string                 `json:"id"`
	Category             string                 `json:"category"`
	Priority             RecommendationPriority `json:"priority"`
	Title                string                 `json:"title"`
	Description          string                 `json:"description"`
	ExpectedImpact       string                 `json:"expected_impact"`
	ImplementationEffort string                 `json:"implementation_effort"`
	Actions              []string               `json:"actions"`
	MetricTargets        map[string]float64     `json:"metric_targets"`
	Deadline             *time.Time             `json:"deadline,omitempty"`
}

// NewAIMetricsCollector creates a new AI metrics collector.
func NewAIMetricsCollector(aiManager ai.AI, metricsCollector forge.Metrics, logger forge.Logger, config AIMetricsConfig) *AIMetricsCollector {
	return &AIMetricsCollector{
		aiManager:        aiManager,
		metricsCollector: metricsCollector,
		logger:           logger,
		config:           config,
		agentMetrics:     make(map[string]*AgentMetrics),
		modelMetrics:     make(map[string]*ModelMetrics),
		systemMetrics:    &SystemMetrics{},
		historicalData: &HistoricalMetrics{
			DataPoints:  make([]MetricDataPoint, 0, config.HistoricalPoints),
			Trends:      make(map[string]Trend),
			Predictions: make(map[string]Prediction),
			maxPoints:   config.HistoricalPoints,
		},
	}
}

// Start begins metrics collection.
func (c *AIMetricsCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return errors.New("AI metrics collector already started")
	}

	c.collectionTicker = time.NewTicker(c.config.CollectionInterval)
	c.started = true

	// Start collection goroutine
	go c.collectMetrics(ctx)

	if c.logger != nil {
		c.logger.Info("AI metrics collector started",
			logger.Duration("collection_interval", c.config.CollectionInterval),
			logger.Bool("predictive_enabled", c.config.EnablePredictive),
			logger.Bool("anomaly_detection_enabled", c.config.EnableAnomalyDetection),
		)
	}

	return nil
}

// Stop stops metrics collection.
func (c *AIMetricsCollector) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return errors.New("AI metrics collector not started")
	}

	if c.collectionTicker != nil {
		c.collectionTicker.Stop()
	}

	c.started = false

	if c.logger != nil {
		c.logger.Info("AI metrics collector stopped")
	}

	return nil
}

// GetMetricsReport returns comprehensive AI metrics report.
func (c *AIMetricsCollector) GetMetricsReport(ctx context.Context) (*AIMetricsReport, error) {
	startTime := time.Now()

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Collect current metrics
	c.collectCurrentMetrics()

	// Analyze trends
	trends := c.analyzeTrends()

	// Generate predictions if enabled
	var predictions map[string]Prediction
	if c.config.EnablePredictive {
		predictions = c.generatePredictions()
	}

	// Detect anomalies if enabled
	var anomalies []MetricAnomaly
	if c.config.EnableAnomalyDetection {
		anomalies = c.detectAnomalies()
	}

	// Generate performance summary
	performanceSummary := c.generatePerformanceSummary()

	// Generate recommendations
	recommendations := c.generatePerformanceRecommendations()

	report := &AIMetricsReport{
		SystemMetrics:      c.systemMetrics,
		AgentMetrics:       c.copyAgentMetrics(),
		ModelMetrics:       c.copyModelMetrics(),
		HistoricalData:     c.copyHistoricalData(),
		Trends:             trends,
		Predictions:        predictions,
		Anomalies:          anomalies,
		PerformanceSummary: performanceSummary,
		Recommendations:    recommendations,
		LastUpdated:        time.Now(),
		ReportDuration:     time.Since(startTime),
	}

	// Record metrics about the report generation
	if c.metricsCollector != nil {
		c.metricsCollector.Counter("forge.ai.metrics.reports_generated").Inc()
		c.metricsCollector.Histogram("forge.ai.metrics.report_duration").Observe(time.Since(startTime).Seconds())
		c.metricsCollector.Gauge("forge.ai.metrics.system_performance_score").Set(performanceSummary.PerformanceScore)
	}

	return report, nil
}

// collectMetrics runs the periodic metrics collection.
func (c *AIMetricsCollector) collectMetrics(ctx context.Context) {
	defer c.collectionTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.collectionTicker.C:
			c.mu.Lock()
			c.collectCurrentMetrics()
			c.updateHistoricalData()
			c.mu.Unlock()
		}
	}
}

// collectCurrentMetrics collects current performance metrics.
func (c *AIMetricsCollector) collectCurrentMetrics() {
	// Collect system-wide metrics
	c.collectSystemMetrics()

	// Collect agent metrics
	c.collectAgentMetrics()

	// Collect model metrics
	c.collectModelMetrics()
}

// collectSystemMetrics collects system-wide metrics.
func (c *AIMetricsCollector) collectSystemMetrics() {
	stats := c.aiManager.GetStats()

	inferenceRequests := getInt64FromMap(stats, "InferenceRequests")
	errorRate := getFloat64FromMap(stats, "ErrorRate")

	c.systemMetrics.TotalRequests = inferenceRequests
	c.systemMetrics.TotalErrors = int64(errorRate * float64(inferenceRequests))
	c.systemMetrics.OverallErrorRate = errorRate
	c.systemMetrics.OverallLatency = getDurationFromMap(stats, "AverageLatency")
	c.systemMetrics.OverallThroughput = float64(inferenceRequests) / time.Since(time.Now().Add(-c.config.CollectionInterval)).Seconds()

	c.systemMetrics.TotalAgents = getIntFromMap(stats, "TotalAgents")
	c.systemMetrics.ActiveAgents = getIntFromMap(stats, "ActiveAgents")

	// Simplified healthy agents calculation
	c.systemMetrics.HealthyAgents = getIntFromMap(stats, "ActiveAgents")

	c.systemMetrics.TotalModels = getIntFromMap(stats, "ModelsLoaded")
	c.systemMetrics.LoadedModels = getIntFromMap(stats, "ModelsLoaded")

	// Resource utilization would be collected from system monitoring
	c.systemMetrics.SystemCPUUsage = 0.0    // Would integrate with system metrics
	c.systemMetrics.SystemMemoryUsage = 0.0 // Would integrate with system metrics
	c.systemMetrics.SystemDiskUsage = 0.0   // Would integrate with system metrics

	// Capacity metrics
	c.systemMetrics.CurrentCapacity = float64(getIntFromMap(stats, "ActiveAgents"))

	c.systemMetrics.MaxCapacity = float64(getIntFromMap(stats, "TotalAgents"))
	if c.systemMetrics.MaxCapacity > 0 {
		c.systemMetrics.CapacityUtilization = c.systemMetrics.CurrentCapacity / c.systemMetrics.MaxCapacity
	}

	c.systemMetrics.LastUpdated = time.Now()

	// Record system metrics
	if c.metricsCollector != nil {
		c.metricsCollector.Gauge("forge.ai.system.error_rate").Set(c.systemMetrics.OverallErrorRate)
		c.metricsCollector.Gauge("forge.ai.system.latency").Set(c.systemMetrics.OverallLatency.Seconds())
		c.metricsCollector.Gauge("forge.ai.system.throughput").Set(c.systemMetrics.OverallThroughput)
		c.metricsCollector.Gauge("forge.ai.system.active_agents").Set(float64(c.systemMetrics.ActiveAgents))
		c.metricsCollector.Gauge("forge.ai.system.healthy_agents").Set(float64(c.systemMetrics.HealthyAgents))
		c.metricsCollector.Gauge("forge.ai.system.capacity_utilization").Set(c.systemMetrics.CapacityUtilization)
	}
}

// collectAgentMetrics collects metrics for all agents.
func (c *AIMetricsCollector) collectAgentMetrics() {
	agents := c.aiManager.GetAgents()

	for _, agent := range agents {
		agentID := agent.ID()
		stats := agent.GetStats()
		_ = agent.GetHealth() // unused for now

		// Get or create agent metrics
		agentMetrics, exists := c.agentMetrics[agentID]
		if !exists {
			agentMetrics = &AgentMetrics{
				AgentID:           agentID,
				AgentName:         agent.Name(),
				AgentType:         string(agent.Type()),
				LatencyHistory:    make([]float64, 0, 100),
				ErrorRateHistory:  make([]float64, 0, 100),
				ThroughputHistory: make([]float64, 0, 100),
				Anomalies:         make([]MetricAnomaly, 0),
			}
			c.agentMetrics[agentID] = agentMetrics
		}

		// Update metrics
		agentMetrics.RequestCount = stats.TotalProcessed
		agentMetrics.ErrorCount = stats.TotalErrors
		agentMetrics.SuccessCount = stats.TotalProcessed - stats.TotalErrors
		agentMetrics.ErrorRate = stats.ErrorRate
		agentMetrics.AverageLatency = stats.AverageLatency
		agentMetrics.AverageConfidence = stats.Confidence
		agentMetrics.AccuracyScore = stats.LearningMetrics.AccuracyScore
		agentMetrics.FeedbackCount = stats.LearningMetrics.TotalFeedback
		agentMetrics.PositiveFeedback = stats.LearningMetrics.PositiveFeedback
		agentMetrics.NegativeFeedback = stats.LearningMetrics.NegativeFeedback

		// Calculate throughput
		if c.config.CollectionInterval > 0 {
			agentMetrics.RequestsPerSecond = float64(stats.TotalProcessed) / time.Since(time.Now().Add(-c.config.CollectionInterval)).Seconds()
		}

		// Update history (keep last 100 points)
		agentMetrics.LatencyHistory = append(agentMetrics.LatencyHistory, stats.AverageLatency.Seconds())
		if len(agentMetrics.LatencyHistory) > 100 {
			agentMetrics.LatencyHistory = agentMetrics.LatencyHistory[1:]
		}

		agentMetrics.ErrorRateHistory = append(agentMetrics.ErrorRateHistory, stats.ErrorRate)
		if len(agentMetrics.ErrorRateHistory) > 100 {
			agentMetrics.ErrorRateHistory = agentMetrics.ErrorRateHistory[1:]
		}

		agentMetrics.ThroughputHistory = append(agentMetrics.ThroughputHistory, agentMetrics.RequestsPerSecond)
		if len(agentMetrics.ThroughputHistory) > 100 {
			agentMetrics.ThroughputHistory = agentMetrics.ThroughputHistory[1:]
		}

		// Calculate percentiles from history
		if len(agentMetrics.LatencyHistory) > 0 {
			agentMetrics.P50Latency = time.Duration(c.calculatePercentile(agentMetrics.LatencyHistory, 0.5)) * time.Second
			agentMetrics.P95Latency = time.Duration(c.calculatePercentile(agentMetrics.LatencyHistory, 0.95)) * time.Second
			agentMetrics.P99Latency = time.Duration(c.calculatePercentile(agentMetrics.LatencyHistory, 0.99)) * time.Second
		}

		agentMetrics.LastUpdated = time.Now()

		// Record agent metrics
		if c.metricsCollector != nil {
			c.metricsCollector.Counter("forge.ai.agent.requests_total", "agent_id", agentID).Add(float64(stats.TotalProcessed))
			c.metricsCollector.Counter("forge.ai.agent.errors_total", "agent_id", agentID).Add(float64(stats.TotalErrors))
			c.metricsCollector.Gauge("forge.ai.agent.error_rate", "agent_id", agentID).Set(stats.ErrorRate)
			c.metricsCollector.Gauge("forge.ai.agent.latency", "agent_id", agentID).Set(stats.AverageLatency.Seconds())
			c.metricsCollector.Gauge("forge.ai.agent.confidence", "agent_id", agentID).Set(stats.Confidence)
			c.metricsCollector.Gauge("forge.ai.agent.throughput", "agent_id", agentID).Set(agentMetrics.RequestsPerSecond)
		}
	}
}

// collectModelMetrics collects metrics for all models.
func (c *AIMetricsCollector) collectModelMetrics() {
	modelServer := c.aiManager.GetModelServer()
	if modelServer == nil {
		return
	}

	// modelServer is interface{} for now, so we can't call methods
	// TODO: Implement when model server interface is properly defined
	// models := modelServer.GetModels()
	// for _, model := range models {
	// 	modelID := model.ID()
	// 	modelMetrics := model.GetMetrics()
	// 	_ = model.GetHealth() // unused for now
	// 	...
	// }
}

// Helper methods for metrics analysis

// calculatePercentile calculates the percentile value from a slice of floats.
func (c *AIMetricsCollector) calculatePercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// Simple percentile calculation (could be improved with proper sorting)
	sorted := make([]float64, len(values))
	copy(sorted, values)

	// Basic sorting for percentile calculation
	for i := range len(sorted) - 1 {
		for j := range len(sorted) - i - 1 {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	index := percentile * float64(len(sorted)-1)
	if index == float64(int(index)) {
		return sorted[int(index)]
	}

	lower := sorted[int(index)]
	upper := sorted[int(index)+1]

	return lower + (upper-lower)*(index-float64(int(index)))
}

// updateHistoricalData updates the historical metrics data.
func (c *AIMetricsCollector) updateHistoricalData() {
	c.historicalData.mu.Lock()
	defer c.historicalData.mu.Unlock()

	dataPoint := MetricDataPoint{
		Timestamp:      time.Now(),
		ErrorRate:      c.systemMetrics.OverallErrorRate,
		AverageLatency: c.systemMetrics.OverallLatency.Seconds(),
		Throughput:     c.systemMetrics.OverallThroughput,
		CPUUsage:       c.systemMetrics.SystemCPUUsage,
		MemoryUsage:    c.systemMetrics.SystemMemoryUsage,
		ActiveAgents:   c.systemMetrics.ActiveAgents,
		Metadata:       make(map[string]any),
	}

	c.historicalData.DataPoints = append(c.historicalData.DataPoints, dataPoint)

	// Keep only the configured number of points
	if len(c.historicalData.DataPoints) > c.historicalData.maxPoints {
		c.historicalData.DataPoints = c.historicalData.DataPoints[len(c.historicalData.DataPoints)-c.historicalData.maxPoints:]
	}
}

// Additional methods for trend analysis, predictions, anomaly detection, etc. would be implemented here...

// Placeholder methods for the report generation functionality.
func (c *AIMetricsCollector) analyzeTrends() map[string]Trend {
	// Implement trend analysis logic
	return make(map[string]Trend)
}

func (c *AIMetricsCollector) generatePredictions() map[string]Prediction {
	// Implement prediction logic
	return make(map[string]Prediction)
}

func (c *AIMetricsCollector) detectAnomalies() []MetricAnomaly {
	// Implement anomaly detection logic
	return make([]MetricAnomaly, 0)
}

func (c *AIMetricsCollector) generatePerformanceSummary() *PerformanceSummary {
	// Calculate performance scores
	errorScore := 1.0 - c.systemMetrics.OverallErrorRate
	latencyScore := 1.0 / (1.0 + c.systemMetrics.OverallLatency.Seconds())
	throughputScore := math.Min(c.systemMetrics.OverallThroughput/1000.0, 1.0) // Normalize to 1000 RPS
	healthScore := float64(c.systemMetrics.HealthyAgents) / float64(c.systemMetrics.TotalAgents)

	performanceScore := (errorScore + latencyScore + throughputScore + healthScore) / 4.0

	return &PerformanceSummary{
		OverallHealth:     c.determineOverallHealth(performanceScore),
		PerformanceScore:  performanceScore,
		EfficiencyScore:   c.systemMetrics.CapacityUtilization,
		QualityScore:      c.systemMetrics.OverallAccuracy,
		KeyInsights:       c.generateKeyInsights(),
		CriticalIssues:    c.identifyCriticalIssues(),
		ImprovementAreas:  c.identifyImprovementAreas(),
		PerformanceTrends: c.summarizePerformanceTrends(),
	}
}

func (c *AIMetricsCollector) generatePerformanceRecommendations() []PerformanceRecommendation {
	// Implement recommendation generation logic
	return make([]PerformanceRecommendation, 0)
}

// Helper methods for copying data structures to avoid race conditions.
func (c *AIMetricsCollector) copyAgentMetrics() map[string]*AgentMetrics {
	result := make(map[string]*AgentMetrics)
	// Deep copy would be implemented here
	maps.Copy(result, c.agentMetrics)

	return result
}

func (c *AIMetricsCollector) copyModelMetrics() map[string]*ModelMetrics {
	result := make(map[string]*ModelMetrics)
	// Deep copy would be implemented here
	maps.Copy(result, c.modelMetrics)

	return result
}

func (c *AIMetricsCollector) copyHistoricalData() *HistoricalMetrics {
	// Deep copy would be implemented here
	return c.historicalData
}

// Additional helper methods.
func (c *AIMetricsCollector) determineOverallHealth(score float64) string {
	if score >= 0.8 {
		return "excellent"
	} else if score >= 0.6 {
		return "good"
	} else if score >= 0.4 {
		return "fair"
	} else if score >= 0.2 {
		return "poor"
	}

	return "critical"
}

func (c *AIMetricsCollector) generateKeyInsights() []string {
	var insights []string

	if c.systemMetrics.OverallErrorRate < 0.01 {
		insights = append(insights, "System maintains excellent reliability with <1% error rate")
	}

	if c.systemMetrics.CapacityUtilization > 0.9 {
		insights = append(insights, "High capacity utilization - consider scaling")
	}

	if float64(c.systemMetrics.HealthyAgents)/float64(c.systemMetrics.TotalAgents) > 0.95 {
		insights = append(insights, "Excellent agent health with >95% healthy agents")
	}

	return insights
}

func (c *AIMetricsCollector) identifyCriticalIssues() []string {
	var issues []string

	if c.systemMetrics.OverallErrorRate > c.config.AlertThresholds.ErrorRateCritical {
		issues = append(issues, fmt.Sprintf("Critical error rate: %.2f%%", c.systemMetrics.OverallErrorRate*100))
	}

	if c.systemMetrics.OverallLatency > c.config.AlertThresholds.LatencyCritical {
		issues = append(issues, fmt.Sprintf("Critical latency: %v", c.systemMetrics.OverallLatency))
	}

	unhealthyAgents := c.systemMetrics.TotalAgents - c.systemMetrics.HealthyAgents
	if unhealthyAgents > c.systemMetrics.TotalAgents/2 {
		issues = append(issues, fmt.Sprintf("Majority of agents unhealthy: %d/%d", unhealthyAgents, c.systemMetrics.TotalAgents))
	}

	return issues
}

func (c *AIMetricsCollector) identifyImprovementAreas() []string {
	var areas []string

	if c.systemMetrics.OverallErrorRate > 0.05 {
		areas = append(areas, "Error rate optimization")
	}

	if c.systemMetrics.OverallLatency > time.Second {
		areas = append(areas, "Latency optimization")
	}

	if c.systemMetrics.CapacityUtilization < 0.5 {
		areas = append(areas, "Resource utilization optimization")
	}

	return areas
}

func (c *AIMetricsCollector) summarizePerformanceTrends() map[string]string {
	// This would analyze historical data to determine trends
	return map[string]string{
		"error_rate": "stable",
		"latency":    "improving",
		"throughput": "increasing",
		"capacity":   "stable",
	}
}
