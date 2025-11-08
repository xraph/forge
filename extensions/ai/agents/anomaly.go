package agents

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	internalai "github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/internal/errors"
)

// AnomalyDetectionAgent detects anomalies in system behavior and performance.
type AnomalyDetectionAgent struct {
	*internalai.BaseAgent

	// Anomaly detection state
	anomalyHistory   []AnomalyEvent
	baselineModels   map[string]*BaselineModel
	detectionRules   []AnomalyRule
	sensitivityLevel SensitivityLevel

	// Detection algorithms
	statisticalDetector *StatisticalAnomalyDetector
	mlDetector          *MLAnomalyDetector
	ruleBasedDetector   *RuleBasedAnomalyDetector

	// Learning parameters
	learningWindow      time.Duration
	adaptationRate      float64
	confidenceThreshold float64
}

// AnomalyEvent represents a detected anomaly.
type AnomalyEvent struct {
	ID          string                 `json:"id"`
	Type        AnomalyType            `json:"type"`
	Severity    AnomalySeverity        `json:"severity"`
	Score       float64                `json:"score"`
	Description string                 `json:"description"`
	DataPoint   DataPoint              `json:"data_point"`
	Context     map[string]interface{} `json:"context"`
	DetectedAt  time.Time              `json:"detected_at"`
	Resolved    bool                   `json:"resolved"`
	ResolvedAt  time.Time              `json:"resolved_at,omitempty"`
	Actions     []AnomalyAction        `json:"actions"`
}

// AnomalyType represents the type of anomaly.
type AnomalyType string

const (
	AnomalyTypeStatistical AnomalyType = "statistical"
	AnomalyTypeTrend       AnomalyType = "trend"
	AnomalyTypeSpike       AnomalyType = "spike"
	AnomalyTypeDrop        AnomalyType = "drop"
	AnomalyTypePattern     AnomalyType = "pattern"
	AnomalyTypeSeasonal    AnomalyType = "seasonal"
	AnomalyTypeContextual  AnomalyType = "contextual"
	AnomalyTypeCollective  AnomalyType = "collective"
)

// AnomalySeverity represents the severity of an anomaly.
type AnomalySeverity string

const (
	AnomalySeverityLow      AnomalySeverity = "low"
	AnomalySeverityMedium   AnomalySeverity = "medium"
	AnomalySeverityHigh     AnomalySeverity = "high"
	AnomalySeverityCritical AnomalySeverity = "critical"
)

// SensitivityLevel represents the sensitivity level for anomaly detection.
type SensitivityLevel string

const (
	SensitivityLevelLow    SensitivityLevel = "low"
	SensitivityLevelMedium SensitivityLevel = "medium"
	SensitivityLevelHigh   SensitivityLevel = "high"
)

// DataPoint represents a data point for anomaly detection.
type DataPoint struct {
	Timestamp time.Time              `json:"timestamp"`
	Value     float64                `json:"value"`
	Metric    string                 `json:"metric"`
	Labels    map[string]string      `json:"labels"`
	Context   map[string]interface{} `json:"context"`
}

// BaselineModel represents a baseline model for normal behavior.
type BaselineModel struct {
	Metric          string             `json:"metric"`
	Mean            float64            `json:"mean"`
	StandardDev     float64            `json:"standard_dev"`
	Min             float64            `json:"min"`
	Max             float64            `json:"max"`
	Percentiles     map[string]float64 `json:"percentiles"`
	SampleSize      int                `json:"sample_size"`
	LastUpdated     time.Time          `json:"last_updated"`
	Confidence      float64            `json:"confidence"`
	SeasonalPattern map[string]float64 `json:"seasonal_pattern"`
}

// AnomalyRule represents a rule for anomaly detection.
type AnomalyRule struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Metric    string                 `json:"metric"`
	Condition string                 `json:"condition"`
	Threshold float64                `json:"threshold"`
	Severity  AnomalySeverity        `json:"severity"`
	Action    string                 `json:"action"`
	Enabled   bool                   `json:"enabled"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// AnomalyAction represents an action to take for an anomaly.
type AnomalyAction struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Priority    int                    `json:"priority"`
	Automated   bool                   `json:"automated"`
}

// NewAnomalyDetectionAgent creates a new anomaly detection agent.
func NewAnomalyDetectionAgent(id, name string) internalai.AIAgent {
	capabilities := []internalai.Capability{
		{
			Name:        "anomaly-detection",
			Description: "Detect anomalies in system behavior and metrics",
			InputType:   reflect.TypeOf(AnomalyDetectionInput{}),
			OutputType:  reflect.TypeOf(AnomalyDetectionOutput{}),
			Metadata: map[string]interface{}{
				"category": "detection",
				"priority": "high",
			},
		},
		{
			Name:        "baseline-modeling",
			Description: "Build and maintain baseline models for normal behavior",
			InputType:   reflect.TypeOf(BaselineModelingInput{}),
			OutputType:  reflect.TypeOf(BaselineModelingOutput{}),
			Metadata: map[string]interface{}{
				"category": "modeling",
				"priority": "medium",
			},
		},
		{
			Name:        "anomaly-analysis",
			Description: "Analyze detected anomalies and provide insights",
			InputType:   reflect.TypeOf(AnomalyAnalysisInput{}),
			OutputType:  reflect.TypeOf(AnomalyAnalysisOutput{}),
			Metadata: map[string]interface{}{
				"category": "analysis",
				"priority": "medium",
			},
		},
	}

	agent := &AnomalyDetectionAgent{
		BaseAgent:           internalai.NewBaseAgent(id, name, internalai.AgentTypeAnomalyDetection, capabilities),
		anomalyHistory:      make([]AnomalyEvent, 0),
		baselineModels:      make(map[string]*BaselineModel),
		detectionRules:      make([]AnomalyRule, 0),
		sensitivityLevel:    SensitivityLevelMedium,
		statisticalDetector: NewStatisticalAnomalyDetector(),
		mlDetector:          NewMLAnomalyDetector(),
		ruleBasedDetector:   NewRuleBasedAnomalyDetector(),
		learningWindow:      24 * time.Hour,
		adaptationRate:      0.1,
		confidenceThreshold: 0.8,
	}

	// Initialize default detection rules
	agent.initializeDefaultRules()

	return agent
}

// Process processes anomaly detection requests.
func (a *AnomalyDetectionAgent) Process(ctx context.Context, input internalai.AgentInput) (internalai.AgentOutput, error) {
	// Call base implementation first
	output, err := a.BaseAgent.Process(ctx, input)
	if err != nil {
		return output, err
	}

	// Handle specific anomaly detection types
	switch input.Type {
	case "anomaly-detection":
		return a.processAnomalyDetection(ctx, input)
	case "baseline-modeling":
		return a.processBaselineModeling(ctx, input)
	case "anomaly-analysis":
		return a.processAnomalyAnalysis(ctx, input)
	default:
		return output, fmt.Errorf("unsupported anomaly detection type: %s", input.Type)
	}
}

// processAnomalyDetection processes anomaly detection requests.
func (a *AnomalyDetectionAgent) processAnomalyDetection(ctx context.Context, input internalai.AgentInput) (internalai.AgentOutput, error) {
	// Parse input
	data, ok := input.Data.(map[string]interface{})
	if !ok {
		return internalai.AgentOutput{}, errors.New("invalid input data format")
	}

	// Extract data points
	dataPoints := a.extractDataPoints(data)
	if len(dataPoints) == 0 {
		return internalai.AgentOutput{}, errors.New("no data points provided")
	}

	// Detect anomalies using multiple methods
	anomalies := make([]AnomalyEvent, 0)

	// Statistical detection
	statisticalAnomalies, err := a.statisticalDetector.DetectAnomalies(dataPoints, a.baselineModels)
	if err == nil {
		anomalies = append(anomalies, statisticalAnomalies...)
	}

	// ML-based detection
	mlAnomalies, err := a.mlDetector.DetectAnomalies(dataPoints, a.baselineModels)
	if err == nil {
		anomalies = append(anomalies, mlAnomalies...)
	}

	// Rule-based detection
	ruleAnomalies, err := a.ruleBasedDetector.DetectAnomalies(dataPoints, a.detectionRules)
	if err == nil {
		anomalies = append(anomalies, ruleAnomalies...)
	}

	// Merge and deduplicate anomalies
	mergedAnomalies := a.mergeAnomalies(anomalies)

	// Filter by sensitivity level
	filteredAnomalies := a.filterBySensitivity(mergedAnomalies)

	// Generate actions for each anomaly
	actions := make([]internalai.AgentAction, 0)

	for _, anomaly := range filteredAnomalies {
		for _, anomalyAction := range anomaly.Actions {
			action := internalai.AgentAction{
				Type:       anomalyAction.Type,
				Target:     anomaly.DataPoint.Metric,
				Parameters: anomalyAction.Parameters,
				Priority:   anomalyAction.Priority,
				Timeout:    60 * time.Second,
			}
			actions = append(actions, action)
		}
	}

	// Update anomaly history
	a.anomalyHistory = append(a.anomalyHistory, filteredAnomalies...)

	// Calculate confidence based on multiple detectors
	confidence := a.calculateDetectionConfidence(filteredAnomalies)

	return internalai.AgentOutput{
		Type:        "anomaly-detection-result",
		Data:        filteredAnomalies,
		Confidence:  confidence,
		Explanation: fmt.Sprintf("Detected %d anomalies using multi-method approach", len(filteredAnomalies)),
		Actions:     actions,
		Metadata: map[string]interface{}{
			"anomalies_count":   len(filteredAnomalies),
			"data_points_count": len(dataPoints),
			"sensitivity_level": string(a.sensitivityLevel),
			"detection_methods": []string{"statistical", "ml", "rule-based"},
		},
		Timestamp: time.Now(),
	}, nil
}

// processBaselineModeling processes baseline modeling requests.
func (a *AnomalyDetectionAgent) processBaselineModeling(ctx context.Context, input internalai.AgentInput) (internalai.AgentOutput, error) {
	// Parse input
	data, ok := input.Data.(map[string]interface{})
	if !ok {
		return internalai.AgentOutput{}, errors.New("invalid input data format")
	}

	// Extract data points
	dataPoints := a.extractDataPoints(data)
	if len(dataPoints) == 0 {
		return internalai.AgentOutput{}, errors.New("no data points provided")
	}

	// Group data points by metric
	metricGroups := make(map[string][]DataPoint)
	for _, dp := range dataPoints {
		metricGroups[dp.Metric] = append(metricGroups[dp.Metric], dp)
	}

	// Build baseline models for each metric
	updatedModels := make(map[string]*BaselineModel)

	for metric, points := range metricGroups {
		model := a.buildBaselineModel(metric, points)
		updatedModels[metric] = model
		a.baselineModels[metric] = model
	}

	// Generate baseline actions
	actions := make([]internalai.AgentAction, 0)

	for metric, model := range updatedModels {
		if model.Confidence > a.confidenceThreshold {
			action := internalai.AgentAction{
				Type:   "baseline-updated",
				Target: metric,
				Parameters: map[string]interface{}{
					"model_confidence": model.Confidence,
					"sample_size":      model.SampleSize,
				},
				Priority: 5,
				Timeout:  30 * time.Second,
			}
			actions = append(actions, action)
		}
	}

	return internalai.AgentOutput{
		Type:        "baseline-modeling-result",
		Data:        updatedModels,
		Confidence:  a.calculateModelingConfidence(updatedModels),
		Explanation: fmt.Sprintf("Updated %d baseline models", len(updatedModels)),
		Actions:     actions,
		Metadata: map[string]interface{}{
			"models_count":      len(updatedModels),
			"data_points_count": len(dataPoints),
			"learning_window":   a.learningWindow.String(),
		},
		Timestamp: time.Now(),
	}, nil
}

// processAnomalyAnalysis processes anomaly analysis requests.
func (a *AnomalyDetectionAgent) processAnomalyAnalysis(ctx context.Context, input internalai.AgentInput) (internalai.AgentOutput, error) {
	// Parse input
	data, ok := input.Data.(map[string]interface{})
	if !ok {
		return internalai.AgentOutput{}, errors.New("invalid input data format")
	}

	// Extract time range for analysis
	timeRange, err := a.extractTimeRange(data)
	if err != nil {
		return internalai.AgentOutput{}, fmt.Errorf("invalid time range: %w", err)
	}

	// Analyze anomalies within the time range
	analysis := a.analyzeAnomalies(timeRange)

	return internalai.AgentOutput{
		Type:        "anomaly-analysis-result",
		Data:        analysis,
		Confidence:  0.9, // High confidence for analysis
		Explanation: fmt.Sprintf("Analyzed %d anomalies in the specified time range", len(analysis.Anomalies)),
		Actions:     []internalai.AgentAction{}, // Analysis typically doesn't generate actions
		Metadata: map[string]interface{}{
			"time_range":      timeRange,
			"anomalies_count": len(analysis.Anomalies),
		},
		Timestamp: time.Now(),
	}, nil
}

// extractDataPoints extracts data points from input data.
func (a *AnomalyDetectionAgent) extractDataPoints(data map[string]interface{}) []DataPoint {
	dataPoints := make([]DataPoint, 0)

	// Handle different input formats
	if dataArray, ok := data["data"].([]interface{}); ok {
		for _, item := range dataArray {
			if dpMap, ok := item.(map[string]interface{}); ok {
				dp := a.convertToDataPoint(dpMap)
				dataPoints = append(dataPoints, dp)
			}
		}
	} else if pointsInterface, ok := data["points"].([]interface{}); ok {
		for _, item := range pointsInterface {
			if dpMap, ok := item.(map[string]interface{}); ok {
				dp := a.convertToDataPoint(dpMap)
				dataPoints = append(dataPoints, dp)
			}
		}
	}

	return dataPoints
}

// convertToDataPoint converts a map to a DataPoint.
func (a *AnomalyDetectionAgent) convertToDataPoint(dpMap map[string]interface{}) DataPoint {
	dp := DataPoint{
		Timestamp: time.Now(),
		Labels:    make(map[string]string),
		Context:   make(map[string]interface{}),
	}

	if timestamp, ok := dpMap["timestamp"].(time.Time); ok {
		dp.Timestamp = timestamp
	}

	if value, ok := dpMap["value"].(float64); ok {
		dp.Value = value
	}

	if metric, ok := dpMap["metric"].(string); ok {
		dp.Metric = metric
	}

	if labels, ok := dpMap["labels"].(map[string]string); ok {
		dp.Labels = labels
	}

	if context, ok := dpMap["context"].(map[string]interface{}); ok {
		dp.Context = context
	}

	return dp
}

// buildBaselineModel builds a baseline model from data points.
func (a *AnomalyDetectionAgent) buildBaselineModel(metric string, points []DataPoint) *BaselineModel {
	if len(points) == 0 {
		return nil
	}

	// Calculate basic statistics
	values := make([]float64, len(points))
	for i, point := range points {
		values[i] = point.Value
	}

	mean := a.calculateMean(values)
	stdDev := a.calculateStandardDeviation(values, mean)
	min := a.calculateMin(values)
	max := a.calculateMax(values)
	percentiles := a.calculatePercentiles(values)

	// Calculate confidence based on sample size and variance
	confidence := a.calculateModelConfidence(len(points), stdDev)

	// Build seasonal pattern (simplified)
	seasonalPattern := a.buildSeasonalPattern(points)

	return &BaselineModel{
		Metric:          metric,
		Mean:            mean,
		StandardDev:     stdDev,
		Min:             min,
		Max:             max,
		Percentiles:     percentiles,
		SampleSize:      len(points),
		LastUpdated:     time.Now(),
		Confidence:      confidence,
		SeasonalPattern: seasonalPattern,
	}
}

// mergeAnomalies merges and deduplicates anomalies from different detectors.
func (a *AnomalyDetectionAgent) mergeAnomalies(anomalies []AnomalyEvent) []AnomalyEvent {
	// Simple deduplication based on timestamp and metric
	seen := make(map[string]bool)
	merged := make([]AnomalyEvent, 0)

	for _, anomaly := range anomalies {
		key := fmt.Sprintf("%s_%s_%d", anomaly.DataPoint.Metric, anomaly.Type, anomaly.DetectedAt.Unix())
		if !seen[key] {
			seen[key] = true

			merged = append(merged, anomaly)
		}
	}

	// Sort by severity and score
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Severity != merged[j].Severity {
			return a.severityWeight(merged[i].Severity) > a.severityWeight(merged[j].Severity)
		}

		return merged[i].Score > merged[j].Score
	})

	return merged
}

// filterBySensitivity filters anomalies based on sensitivity level.
func (a *AnomalyDetectionAgent) filterBySensitivity(anomalies []AnomalyEvent) []AnomalyEvent {
	threshold := a.getSensitivityThreshold()
	filtered := make([]AnomalyEvent, 0)

	for _, anomaly := range anomalies {
		if anomaly.Score >= threshold {
			filtered = append(filtered, anomaly)
		}
	}

	return filtered
}

// getSensitivityThreshold returns the threshold based on sensitivity level.
func (a *AnomalyDetectionAgent) getSensitivityThreshold() float64 {
	switch a.sensitivityLevel {
	case SensitivityLevelLow:
		return 0.8
	case SensitivityLevelMedium:
		return 0.6
	case SensitivityLevelHigh:
		return 0.4
	default:
		return 0.6
	}
}

// severityWeight returns the weight for severity comparison.
func (a *AnomalyDetectionAgent) severityWeight(severity AnomalySeverity) int {
	switch severity {
	case AnomalySeverityCritical:
		return 4
	case AnomalySeverityHigh:
		return 3
	case AnomalySeverityMedium:
		return 2
	case AnomalySeverityLow:
		return 1
	default:
		return 0
	}
}

// calculateDetectionConfidence calculates confidence for anomaly detection.
func (a *AnomalyDetectionAgent) calculateDetectionConfidence(anomalies []AnomalyEvent) float64 {
	if len(anomalies) == 0 {
		return 0.5
	}

	totalScore := 0.0
	for _, anomaly := range anomalies {
		totalScore += anomaly.Score
	}

	averageScore := totalScore / float64(len(anomalies))

	return math.Min(0.99, averageScore)
}

// calculateModelingConfidence calculates confidence for baseline modeling.
func (a *AnomalyDetectionAgent) calculateModelingConfidence(models map[string]*BaselineModel) float64 {
	if len(models) == 0 {
		return 0.5
	}

	totalConfidence := 0.0
	for _, model := range models {
		totalConfidence += model.Confidence
	}

	return totalConfidence / float64(len(models))
}

// initializeDefaultRules initializes default anomaly detection rules.
func (a *AnomalyDetectionAgent) initializeDefaultRules() {
	a.detectionRules = []AnomalyRule{
		{
			ID:        "high-error-rate",
			Name:      "High Error Rate",
			Metric:    "error_rate",
			Condition: "greater_than",
			Threshold: 0.05,
			Severity:  AnomalySeverityHigh,
			Action:    "alert",
			Enabled:   true,
		},
		{
			ID:        "high-latency",
			Name:      "High Latency",
			Metric:    "latency",
			Condition: "greater_than",
			Threshold: 1000.0, // milliseconds
			Severity:  AnomalySeverityMedium,
			Action:    "alert",
			Enabled:   true,
		},
		{
			ID:        "low-throughput",
			Name:      "Low Throughput",
			Metric:    "throughput",
			Condition: "less_than",
			Threshold: 100.0,
			Severity:  AnomalySeverityMedium,
			Action:    "alert",
			Enabled:   true,
		},
	}
}

// Helper calculation methods

func (a *AnomalyDetectionAgent) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}

func (a *AnomalyDetectionAgent) calculateStandardDeviation(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}

	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}

	variance /= float64(len(values) - 1)

	return math.Sqrt(variance)
}

func (a *AnomalyDetectionAgent) calculateMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	min := values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
	}

	return min
}

func (a *AnomalyDetectionAgent) calculateMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	max := values[0]
	for _, v := range values {
		if v > max {
			max = v
		}
	}

	return max
}

func (a *AnomalyDetectionAgent) calculatePercentiles(values []float64) map[string]float64 {
	if len(values) == 0 {
		return make(map[string]float64)
	}

	sortedValues := make([]float64, len(values))
	copy(sortedValues, values)
	sort.Float64s(sortedValues)

	percentiles := make(map[string]float64)
	percentiles["p50"] = a.percentile(sortedValues, 0.5)
	percentiles["p95"] = a.percentile(sortedValues, 0.95)
	percentiles["p99"] = a.percentile(sortedValues, 0.99)

	return percentiles
}

func (a *AnomalyDetectionAgent) percentile(sortedValues []float64, p float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}

	index := int(p * float64(len(sortedValues)-1))
	if index >= len(sortedValues) {
		index = len(sortedValues) - 1
	}

	return sortedValues[index]
}

func (a *AnomalyDetectionAgent) calculateModelConfidence(sampleSize int, stdDev float64) float64 {
	// Simple confidence calculation based on sample size and variability
	sizeScore := math.Min(1.0, float64(sampleSize)/1000.0)
	variabilityScore := math.Max(0.0, 1.0-stdDev/10.0)

	return (sizeScore + variabilityScore) / 2.0
}

func (a *AnomalyDetectionAgent) buildSeasonalPattern(points []DataPoint) map[string]float64 {
	// Simplified seasonal pattern building
	pattern := make(map[string]float64)

	// Group by hour of day
	hourlyValues := make(map[int][]float64)

	for _, point := range points {
		hour := point.Timestamp.Hour()
		hourlyValues[hour] = append(hourlyValues[hour], point.Value)
	}

	// Calculate average for each hour
	for hour, values := range hourlyValues {
		if len(values) > 0 {
			pattern[fmt.Sprintf("hour_%d", hour)] = a.calculateMean(values)
		}
	}

	return pattern
}

func (a *AnomalyDetectionAgent) extractTimeRange(data map[string]interface{}) (TimeRange, error) {
	var timeRange TimeRange

	if startTime, ok := data["start_time"].(time.Time); ok {
		timeRange.Start = startTime
	}

	if endTime, ok := data["end_time"].(time.Time); ok {
		timeRange.End = endTime
	}

	if timeRange.Start.IsZero() || timeRange.End.IsZero() {
		return timeRange, errors.New("invalid time range")
	}

	return timeRange, nil
}

func (a *AnomalyDetectionAgent) analyzeAnomalies(timeRange TimeRange) AnomalyAnalysis {
	// Filter anomalies by time range
	filteredAnomalies := make([]AnomalyEvent, 0)

	for _, anomaly := range a.anomalyHistory {
		if anomaly.DetectedAt.After(timeRange.Start) && anomaly.DetectedAt.Before(timeRange.End) {
			filteredAnomalies = append(filteredAnomalies, anomaly)
		}
	}

	// Calculate statistics
	analysis := AnomalyAnalysis{
		TimeRange:     timeRange,
		Anomalies:     filteredAnomalies,
		TotalCount:    len(filteredAnomalies),
		SeverityStats: make(map[AnomalySeverity]int),
		TypeStats:     make(map[AnomalyType]int),
	}

	for _, anomaly := range filteredAnomalies {
		analysis.SeverityStats[anomaly.Severity]++
		analysis.TypeStats[anomaly.Type]++
	}

	return analysis
}

// Input/Output types

type AnomalyDetectionInput struct {
	Data    []DataPoint            `json:"data"`
	Options map[string]interface{} `json:"options"`
}

type AnomalyDetectionOutput struct {
	Anomalies  []AnomalyEvent `json:"anomalies"`
	Confidence float64        `json:"confidence"`
	Statistics AnomalyStats   `json:"statistics"`
}

type BaselineModelingInput struct {
	Data    []DataPoint            `json:"data"`
	Options map[string]interface{} `json:"options"`
}

type BaselineModelingOutput struct {
	Models     map[string]*BaselineModel `json:"models"`
	Confidence float64                   `json:"confidence"`
}

type AnomalyAnalysisInput struct {
	StartTime time.Time              `json:"start_time"`
	EndTime   time.Time              `json:"end_time"`
	Options   map[string]interface{} `json:"options"`
}

type AnomalyAnalysisOutput struct {
	Analysis AnomalyAnalysis `json:"analysis"`
}

// Supporting types

type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type AnomalyStats struct {
	TotalCount    int                     `json:"total_count"`
	SeverityStats map[AnomalySeverity]int `json:"severity_stats"`
	TypeStats     map[AnomalyType]int     `json:"type_stats"`
	TimeRange     TimeRange               `json:"time_range"`
}

type AnomalyAnalysis struct {
	TimeRange     TimeRange               `json:"time_range"`
	Anomalies     []AnomalyEvent          `json:"anomalies"`
	TotalCount    int                     `json:"total_count"`
	SeverityStats map[AnomalySeverity]int `json:"severity_stats"`
	TypeStats     map[AnomalyType]int     `json:"type_stats"`
	Trends        []AnomalyTrend          `json:"trends"`
	Patterns      []AnomalyPattern        `json:"patterns"`
}

type AnomalyTrend struct {
	Metric    string  `json:"metric"`
	Direction string  `json:"direction"`
	Strength  float64 `json:"strength"`
}

type AnomalyPattern struct {
	Type        string  `json:"type"`
	Frequency   string  `json:"frequency"`
	Confidence  float64 `json:"confidence"`
	Description string  `json:"description"`
}

// Placeholder implementations for detectors

type StatisticalAnomalyDetector struct{}

func NewStatisticalAnomalyDetector() *StatisticalAnomalyDetector {
	return &StatisticalAnomalyDetector{}
}

func (sad *StatisticalAnomalyDetector) DetectAnomalies(dataPoints []DataPoint, models map[string]*BaselineModel) ([]AnomalyEvent, error) {
	anomalies := make([]AnomalyEvent, 0)

	for _, dp := range dataPoints {
		if model, exists := models[dp.Metric]; exists {
			// Z-score based detection
			if model.StandardDev > 0 {
				zScore := math.Abs(dp.Value-model.Mean) / model.StandardDev
				if zScore > 3.0 { // 3-sigma rule
					anomaly := AnomalyEvent{
						ID:          fmt.Sprintf("stat_%d", time.Now().UnixNano()),
						Type:        AnomalyTypeStatistical,
						Severity:    sad.calculateSeverityFromScore(zScore),
						Score:       zScore / 3.0, // Normalize to 0-1
						Description: fmt.Sprintf("Statistical anomaly detected: Z-score = %.2f", zScore),
						DataPoint:   dp,
						DetectedAt:  time.Now(),
						Actions:     []AnomalyAction{},
					}
					anomalies = append(anomalies, anomaly)
				}
			}
		}
	}

	return anomalies, nil
}

func (sad *StatisticalAnomalyDetector) calculateSeverityFromScore(score float64) AnomalySeverity {
	if score > 5.0 {
		return AnomalySeverityCritical
	} else if score > 4.0 {
		return AnomalySeverityHigh
	} else if score > 3.0 {
		return AnomalySeverityMedium
	} else {
		return AnomalySeverityLow
	}
}

type MLAnomalyDetector struct{}

func NewMLAnomalyDetector() *MLAnomalyDetector {
	return &MLAnomalyDetector{}
}

func (mlad *MLAnomalyDetector) DetectAnomalies(dataPoints []DataPoint, models map[string]*BaselineModel) ([]AnomalyEvent, error) {
	// Placeholder ML-based detection
	return make([]AnomalyEvent, 0), nil
}

type RuleBasedAnomalyDetector struct{}

func NewRuleBasedAnomalyDetector() *RuleBasedAnomalyDetector {
	return &RuleBasedAnomalyDetector{}
}

func (rbad *RuleBasedAnomalyDetector) DetectAnomalies(dataPoints []DataPoint, rules []AnomalyRule) ([]AnomalyEvent, error) {
	anomalies := make([]AnomalyEvent, 0)

	for _, dp := range dataPoints {
		for _, rule := range rules {
			if !rule.Enabled || rule.Metric != dp.Metric {
				continue
			}

			violated := false

			switch rule.Condition {
			case "greater_than":
				violated = dp.Value > rule.Threshold
			case "less_than":
				violated = dp.Value < rule.Threshold
			case "equals":
				violated = dp.Value == rule.Threshold
			}

			if violated {
				anomaly := AnomalyEvent{
					ID:          fmt.Sprintf("rule_%s_%d", rule.ID, time.Now().UnixNano()),
					Type:        AnomalyTypePattern,
					Severity:    rule.Severity,
					Score:       0.8, // Fixed score for rule-based detection
					Description: "Rule violation: " + rule.Name,
					DataPoint:   dp,
					DetectedAt:  time.Now(),
					Actions:     []AnomalyAction{},
				}
				anomalies = append(anomalies, anomaly)
			}
		}
	}

	return anomalies, nil
}
