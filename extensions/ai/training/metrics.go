package training

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/logger"
)

// TrainingMetrics contains comprehensive training metrics.
type TrainingMetrics struct {
	JobID     string         `json:"job_id"`
	StartTime time.Time      `json:"start_time"`
	EndTime   time.Time      `json:"end_time"`
	Duration  time.Duration  `json:"duration"`
	Status    TrainingStatus `json:"status"`

	// Training Progress
	Epoch       int     `json:"epoch"`
	TotalEpochs int     `json:"total_epochs"`
	Step        int64   `json:"step"`
	TotalSteps  int64   `json:"total_steps"`
	Progress    float64 `json:"progress"`

	// Loss Metrics
	TrainingLoss   float64     `json:"training_loss"`
	ValidationLoss float64     `json:"validation_loss"`
	LossHistory    []LossPoint `json:"loss_history"`
	BestLoss       float64     `json:"best_loss"`
	BestEpoch      int         `json:"best_epoch"`

	// Performance Metrics
	Accuracy  float64 `json:"accuracy"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1Score   float64 `json:"f1_score"`
	AUC       float64 `json:"auc"`

	// Model-specific metrics
	CustomMetrics map[string]float64               `json:"custom_metrics"`
	ClassMetrics  map[string]ClassificationMetrics `json:"class_metrics,omitempty"`

	// Resource Utilization
	ResourceUsage ResourceUsageMetrics `json:"resource_usage"`

	// Learning Metrics
	LearningRate float64 `json:"learning_rate"`
	GradientNorm float64 `json:"gradient_norm"`
	WeightNorm   float64 `json:"weight_norm"`

	// Data Metrics
	BatchesProcessed int64   `json:"batches_processed"`
	SamplesProcessed int64   `json:"samples_processed"`
	DataThroughput   float64 `json:"data_throughput"` // samples/sec

	// Early Stopping
	EarlyStopping EarlyStoppingMetrics `json:"early_stopping"`

	// Checkpointing
	Checkpoints []CheckpointInfo `json:"checkpoints"`

	// Last updated timestamp
	LastUpdated time.Time `json:"last_updated"`
}

// LossPoint represents a point in loss history.
type LossPoint struct {
	Epoch          int       `json:"epoch"`
	Step           int64     `json:"step"`
	TrainingLoss   float64   `json:"training_loss"`
	ValidationLoss float64   `json:"validation_loss"`
	Timestamp      time.Time `json:"timestamp"`
}

// ClassificationMetrics contains per-class metrics.
type ClassificationMetrics struct {
	ClassName      string  `json:"class_name"`
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	F1Score        float64 `json:"f1_score"`
	Support        int     `json:"support"`
	TruePositives  int     `json:"true_positives"`
	FalsePositives int     `json:"false_positives"`
	TrueNegatives  int     `json:"true_negatives"`
	FalseNegatives int     `json:"false_negatives"`
}

// ResourceUsageMetrics contains resource utilization metrics.
type ResourceUsageMetrics struct {
	CPUUsage       float64   `json:"cpu_usage"`
	MemoryUsage    int64     `json:"memory_usage"`
	MemoryTotal    int64     `json:"memory_total"`
	GPUUsage       float64   `json:"gpu_usage"`
	GPUMemory      int64     `json:"gpu_memory"`
	GPUMemoryTotal int64     `json:"gpu_memory_total"`
	NetworkIO      NetworkIO `json:"network_io"`
	DiskIO         DiskIO    `json:"disk_io"`
	PowerUsage     float64   `json:"power_usage"`
	Temperature    float64   `json:"temperature"`
}

// NetworkIO contains network I/O metrics.
type NetworkIO struct {
	BytesSent       int64 `json:"bytes_sent"`
	BytesReceived   int64 `json:"bytes_received"`
	PacketsSent     int64 `json:"packets_sent"`
	PacketsReceived int64 `json:"packets_received"`
}

// DiskIO contains disk I/O metrics.
type DiskIO struct {
	ReadBytes  int64 `json:"read_bytes"`
	WriteBytes int64 `json:"write_bytes"`
	ReadOps    int64 `json:"read_ops"`
	WriteOps   int64 `json:"write_ops"`
}

// EarlyStoppingMetrics contains early stopping metrics.
type EarlyStoppingMetrics struct {
	Enabled        bool      `json:"enabled"`
	Metric         string    `json:"metric"`
	BestValue      float64   `json:"best_value"`
	BestEpoch      int       `json:"best_epoch"`
	PatienceCount  int       `json:"patience_count"`
	MaxPatience    int       `json:"max_patience"`
	Stopped        bool      `json:"stopped"`
	StoppedAt      time.Time `json:"stopped_at,omitempty"`
	Improvement    float64   `json:"improvement"`
	MinImprovement float64   `json:"min_improvement"`
}

// CheckpointInfo contains checkpoint information.
type CheckpointInfo struct {
	ID        string                 `json:"id"`
	Epoch     int                    `json:"epoch"`
	Step      int64                  `json:"step"`
	Loss      float64                `json:"loss"`
	Metrics   map[string]float64     `json:"metrics"`
	Path      string                 `json:"path"`
	Size      int64                  `json:"size"`
	CreatedAt time.Time              `json:"created_at"`
	Metadata  map[string]interface{} `json:"metadata"`
	IsBest    bool                   `json:"is_best"`
}

// MetricsCollector defines the interface for collecting training metrics.
type MetricsCollector interface {
	// Metrics collection
	RecordMetric(jobID string, name string, value float64, epoch int, step int64) error
	RecordLoss(jobID string, trainingLoss, validationLoss float64, epoch int, step int64) error
	RecordCustomMetric(jobID string, name string, value float64, metadata map[string]interface{}) error

	// Resource monitoring
	RecordResourceUsage(jobID string, usage ResourceUsageMetrics) error

	// Classification metrics
	RecordClassificationMetrics(jobID string, predictions, labels []int, classNames []string) error
	RecordConfusionMatrix(jobID string, matrix [][]int, classNames []string) error

	// Regression metrics
	RecordRegressionMetrics(jobID string, predictions, targets []float64) error

	// Model metrics
	RecordModelMetrics(jobID string, weights map[string]float64, gradients map[string]float64) error

	// Retrieval
	GetMetrics(jobID string) (TrainingMetrics, error)
	GetMetricsHistory(jobID string, fromTime, toTime time.Time) ([]TrainingMetrics, error)
	GetLossHistory(jobID string) ([]LossPoint, error)

	// Aggregation
	GetAggregatedMetrics(jobIDs []string) (AggregatedMetrics, error)

	// Export
	ExportMetrics(jobID string, format string) ([]byte, error)
}

// AggregatedMetrics contains aggregated metrics across multiple jobs.
type AggregatedMetrics struct {
	JobCount        int                      `json:"job_count"`
	SuccessRate     float64                  `json:"success_rate"`
	AverageAccuracy float64                  `json:"average_accuracy"`
	AverageLoss     float64                  `json:"average_loss"`
	AverageDuration time.Duration            `json:"average_duration"`
	BestAccuracy    float64                  `json:"best_accuracy"`
	BestLoss        float64                  `json:"best_loss"`
	MetricsSummary  map[string]MetricSummary `json:"metrics_summary"`
	TimeRange       TimeRange                `json:"time_range"`
}

// MetricSummary contains summary statistics for a metric.
type MetricSummary struct {
	Name        string             `json:"name"`
	Count       int                `json:"count"`
	Mean        float64            `json:"mean"`
	Median      float64            `json:"median"`
	StdDev      float64            `json:"std_dev"`
	Min         float64            `json:"min"`
	Max         float64            `json:"max"`
	Percentiles map[string]float64 `json:"percentiles"`
	LastValue   float64            `json:"last_value"`
	LastUpdate  time.Time          `json:"last_update"`
}

// TimeRange represents a time range.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// MetricsEvaluator defines the interface for evaluating model performance.
type MetricsEvaluator interface {
	// Classification evaluation
	EvaluateClassification(predictions, labels []int, classNames []string) (ClassificationEvaluation, error)
	EvaluateMultilabelClassification(predictions, labels [][]int, classNames []string) (MultilabelEvaluation, error)

	// Regression evaluation
	EvaluateRegression(predictions, targets []float64) (RegressionEvaluation, error)
	EvaluateMultiOutputRegression(predictions, targets [][]float64) (MultiOutputRegressionEvaluation, error)

	// Ranking evaluation
	EvaluateRanking(predictions [][]float64, relevance [][]int) (RankingEvaluation, error)

	// Time series evaluation
	EvaluateTimeSeries(predictions, targets []float64, timestamps []time.Time) (TimeSeriesEvaluation, error)

	// Custom evaluation
	EvaluateCustom(predictions, targets interface{}, evaluator func(interface{}, interface{}) (map[string]float64, error)) (CustomEvaluation, error)
}

// ClassificationEvaluation contains classification evaluation results.
type ClassificationEvaluation struct {
	Accuracy             float64                          `json:"accuracy"`
	MacroAvgPrecision    float64                          `json:"macro_avg_precision"`
	MacroAvgRecall       float64                          `json:"macro_avg_recall"`
	MacroAvgF1           float64                          `json:"macro_avg_f1"`
	WeightedAvgPrecision float64                          `json:"weighted_avg_precision"`
	WeightedAvgRecall    float64                          `json:"weighted_avg_recall"`
	WeightedAvgF1        float64                          `json:"weighted_avg_f1"`
	ConfusionMatrix      [][]int                          `json:"confusion_matrix"`
	ClassificationReport map[string]ClassificationMetrics `json:"classification_report"`
	ROCCurve             []ROCPoint                       `json:"roc_curve,omitempty"`
	PRCurve              []PRPoint                        `json:"pr_curve,omitempty"`
	AUC                  float64                          `json:"auc"`
	LogLoss              float64                          `json:"log_loss"`
}

// MultilabelEvaluation contains multilabel classification evaluation results.
type MultilabelEvaluation struct {
	SubsetAccuracy    float64                 `json:"subset_accuracy"`
	HammingLoss       float64                 `json:"hamming_loss"`
	MacroAvgPrecision float64                 `json:"macro_avg_precision"`
	MacroAvgRecall    float64                 `json:"macro_avg_recall"`
	MacroAvgF1        float64                 `json:"macro_avg_f1"`
	MicroAvgPrecision float64                 `json:"micro_avg_precision"`
	MicroAvgRecall    float64                 `json:"micro_avg_recall"`
	MicroAvgF1        float64                 `json:"micro_avg_f1"`
	LabelMetrics      map[string]LabelMetrics `json:"label_metrics"`
}

// LabelMetrics contains per-label metrics for multilabel classification.
type LabelMetrics struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1Score   float64 `json:"f1_score"`
	Support   int     `json:"support"`
}

// RegressionEvaluation contains regression evaluation results.
type RegressionEvaluation struct {
	MAE               float64 `json:"mae"`  // Mean Absolute Error
	MSE               float64 `json:"mse"`  // Mean Squared Error
	RMSE              float64 `json:"rmse"` // Root Mean Squared Error
	R2Score           float64 `json:"r2_score"`
	MedianAE          float64 `json:"median_ae"`
	MaxError          float64 `json:"max_error"`
	MAPE              float64 `json:"mape"` // Mean Absolute Percentage Error
	ExplainedVariance float64 `json:"explained_variance"`
}

// MultiOutputRegressionEvaluation contains multi-output regression evaluation results.
type MultiOutputRegressionEvaluation struct {
	OverallMetrics RegressionEvaluation            `json:"overall_metrics"`
	OutputMetrics  map[string]RegressionEvaluation `json:"output_metrics"`
}

// RankingEvaluation contains ranking evaluation results.
type RankingEvaluation struct {
	NDCG      map[int]float64 `json:"ndcg"`      // Normalized Discounted Cumulative Gain
	MAP       float64         `json:"map"`       // Mean Average Precision
	MRR       float64         `json:"mrr"`       // Mean Reciprocal Rank
	Precision map[int]float64 `json:"precision"` // Precision at K
	Recall    map[int]float64 `json:"recall"`    // Recall at K
	HitRate   map[int]float64 `json:"hit_rate"`  // Hit Rate at K
}

// TimeSeriesEvaluation contains time series evaluation results.
type TimeSeriesEvaluation struct {
	RegressionMetrics RegressionEvaluation `json:"regression_metrics"`
	SMAPE             float64              `json:"smape"` // Symmetric Mean Absolute Percentage Error
	WAPE              float64              `json:"wape"`  // Weighted Absolute Percentage Error
	Bias              float64              `json:"bias"`
	Seasonality       SeasonalityMetrics   `json:"seasonality"`
	Trend             TrendMetrics         `json:"trend"`
}

// SeasonalityMetrics contains seasonality analysis metrics.
type SeasonalityMetrics struct {
	Detected      bool                 `json:"detected"`
	Strength      float64              `json:"strength"`
	Period        int                  `json:"period"`
	Decomposition map[string][]float64 `json:"decomposition"`
}

// TrendMetrics contains trend analysis metrics.
type TrendMetrics struct {
	Detected  bool    `json:"detected"`
	Strength  float64 `json:"strength"`
	Direction string  `json:"direction"` // increasing, decreasing, stable
	Slope     float64 `json:"slope"`
}

// CustomEvaluation contains custom evaluation results.
type CustomEvaluation struct {
	Metrics  map[string]float64     `json:"metrics"`
	Metadata map[string]interface{} `json:"metadata"`
}

// ROCPoint represents a point on the ROC curve.
type ROCPoint struct {
	FPR       float64 `json:"fpr"` // False Positive Rate
	TPR       float64 `json:"tpr"` // True Positive Rate
	Threshold float64 `json:"threshold"`
}

// PRPoint represents a point on the Precision-Recall curve.
type PRPoint struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	Threshold float64 `json:"threshold"`
}

// MetricsCollectorImpl implements the MetricsCollector interface.
type MetricsCollectorImpl struct {
	metrics   map[string]*TrainingMetrics
	history   map[string][]TrainingMetrics
	evaluator MetricsEvaluator
	logger    logger.Logger
	mu        sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(logger logger.Logger) MetricsCollector {
	return &MetricsCollectorImpl{
		metrics:   make(map[string]*TrainingMetrics),
		history:   make(map[string][]TrainingMetrics),
		evaluator: NewMetricsEvaluator(logger),
		logger:    logger,
	}
}

// RecordMetric records a generic metric.
func (mc *MetricsCollectorImpl) RecordMetric(jobID string, name string, value float64, epoch int, step int64) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics := mc.getOrCreateMetrics(jobID)

	if metrics.CustomMetrics == nil {
		metrics.CustomMetrics = make(map[string]float64)
	}

	metrics.CustomMetrics[name] = value
	metrics.Epoch = epoch
	metrics.Step = step
	metrics.LastUpdated = time.Now()

	// Calculate progress
	if metrics.TotalEpochs > 0 {
		metrics.Progress = float64(epoch) / float64(metrics.TotalEpochs)
	}

	mc.logger.Debug("metric recorded",
		logger.String("job_id", jobID),
		logger.String("metric", name),
		logger.Float64("value", value),
		logger.Int("epoch", epoch),
		logger.Int64("step", step),
	)

	return nil
}

// RecordLoss records training and validation loss.
func (mc *MetricsCollectorImpl) RecordLoss(jobID string, trainingLoss, validationLoss float64, epoch int, step int64) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics := mc.getOrCreateMetrics(jobID)

	metrics.TrainingLoss = trainingLoss
	metrics.ValidationLoss = validationLoss
	metrics.Epoch = epoch
	metrics.Step = step
	metrics.LastUpdated = time.Now()

	// Update loss history
	lossPoint := LossPoint{
		Epoch:          epoch,
		Step:           step,
		TrainingLoss:   trainingLoss,
		ValidationLoss: validationLoss,
		Timestamp:      time.Now(),
	}
	metrics.LossHistory = append(metrics.LossHistory, lossPoint)

	// Update best loss
	if validationLoss < metrics.BestLoss || metrics.BestLoss == 0 {
		metrics.BestLoss = validationLoss
		metrics.BestEpoch = epoch
	}

	mc.logger.Debug("loss recorded",
		logger.String("job_id", jobID),
		logger.Float64("training_loss", trainingLoss),
		logger.Float64("validation_loss", validationLoss),
		logger.Int("epoch", epoch),
	)

	return nil
}

// RecordCustomMetric records a custom metric with metadata.
func (mc *MetricsCollectorImpl) RecordCustomMetric(jobID string, name string, value float64, metadata map[string]interface{}) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics := mc.getOrCreateMetrics(jobID)

	if metrics.CustomMetrics == nil {
		metrics.CustomMetrics = make(map[string]float64)
	}

	metrics.CustomMetrics[name] = value
	metrics.LastUpdated = time.Now()

	mc.logger.Debug("custom metric recorded",
		logger.String("job_id", jobID),
		logger.String("metric", name),
		logger.Float64("value", value),
		logger.Any("metadata", metadata),
	)

	return nil
}

// RecordResourceUsage records resource usage metrics.
func (mc *MetricsCollectorImpl) RecordResourceUsage(jobID string, usage ResourceUsageMetrics) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics := mc.getOrCreateMetrics(jobID)
	metrics.ResourceUsage = usage
	metrics.LastUpdated = time.Now()

	return nil
}

// RecordClassificationMetrics records classification metrics.
func (mc *MetricsCollectorImpl) RecordClassificationMetrics(jobID string, predictions, labels []int, classNames []string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Evaluate classification performance
	evaluation, err := mc.evaluator.EvaluateClassification(predictions, labels, classNames)
	if err != nil {
		return fmt.Errorf("failed to evaluate classification: %w", err)
	}

	metrics := mc.getOrCreateMetrics(jobID)
	metrics.Accuracy = evaluation.Accuracy
	metrics.Precision = evaluation.MacroAvgPrecision
	metrics.Recall = evaluation.MacroAvgRecall
	metrics.F1Score = evaluation.MacroAvgF1
	metrics.AUC = evaluation.AUC
	metrics.ClassMetrics = evaluation.ClassificationReport
	metrics.LastUpdated = time.Now()

	mc.logger.Debug("classification metrics recorded",
		logger.String("job_id", jobID),
		logger.Float64("accuracy", evaluation.Accuracy),
		logger.Float64("f1_score", evaluation.MacroAvgF1),
	)

	return nil
}

// RecordConfusionMatrix records confusion matrix.
func (mc *MetricsCollectorImpl) RecordConfusionMatrix(jobID string, matrix [][]int, classNames []string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Store confusion matrix in custom metrics as a flattened representation
	metrics := mc.getOrCreateMetrics(jobID)
	if metrics.CustomMetrics == nil {
		metrics.CustomMetrics = make(map[string]float64)
	}

	// Store matrix dimensions
	metrics.CustomMetrics["confusion_matrix_size"] = float64(len(matrix))

	// Store matrix values (simplified representation)
	for i, row := range matrix {
		for j, val := range row {
			key := fmt.Sprintf("confusion_matrix_%d_%d", i, j)
			metrics.CustomMetrics[key] = float64(val)
		}
	}

	metrics.LastUpdated = time.Now()

	return nil
}

// RecordRegressionMetrics records regression metrics.
func (mc *MetricsCollectorImpl) RecordRegressionMetrics(jobID string, predictions, targets []float64) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Evaluate regression performance
	evaluation, err := mc.evaluator.EvaluateRegression(predictions, targets)
	if err != nil {
		return fmt.Errorf("failed to evaluate regression: %w", err)
	}

	metrics := mc.getOrCreateMetrics(jobID)
	if metrics.CustomMetrics == nil {
		metrics.CustomMetrics = make(map[string]float64)
	}

	metrics.CustomMetrics["mae"] = evaluation.MAE
	metrics.CustomMetrics["mse"] = evaluation.MSE
	metrics.CustomMetrics["rmse"] = evaluation.RMSE
	metrics.CustomMetrics["r2_score"] = evaluation.R2Score
	metrics.LastUpdated = time.Now()

	mc.logger.Debug("regression metrics recorded",
		logger.String("job_id", jobID),
		logger.Float64("mae", evaluation.MAE),
		logger.Float64("r2_score", evaluation.R2Score),
	)

	return nil
}

// RecordModelMetrics records model-specific metrics.
func (mc *MetricsCollectorImpl) RecordModelMetrics(jobID string, weights map[string]float64, gradients map[string]float64) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics := mc.getOrCreateMetrics(jobID)

	// Calculate weight norm
	var weightNormSquared float64
	for _, weight := range weights {
		weightNormSquared += weight * weight
	}

	metrics.WeightNorm = math.Sqrt(weightNormSquared)

	// Calculate gradient norm
	var gradNormSquared float64
	for _, grad := range gradients {
		gradNormSquared += grad * grad
	}

	metrics.GradientNorm = math.Sqrt(gradNormSquared)

	metrics.LastUpdated = time.Now()

	return nil
}

// GetMetrics returns metrics for a job.
func (mc *MetricsCollectorImpl) GetMetrics(jobID string) (TrainingMetrics, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metrics, exists := mc.metrics[jobID]
	if !exists {
		return TrainingMetrics{}, errors.ErrServiceNotFound(jobID)
	}

	return *metrics, nil
}

// GetMetricsHistory returns metrics history for a job.
func (mc *MetricsCollectorImpl) GetMetricsHistory(jobID string, fromTime, toTime time.Time) ([]TrainingMetrics, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	history, exists := mc.history[jobID]
	if !exists {
		return []TrainingMetrics{}, nil
	}

	var filtered []TrainingMetrics

	for _, metric := range history {
		if metric.LastUpdated.After(fromTime) && metric.LastUpdated.Before(toTime) {
			filtered = append(filtered, metric)
		}
	}

	return filtered, nil
}

// GetLossHistory returns loss history for a job.
func (mc *MetricsCollectorImpl) GetLossHistory(jobID string) ([]LossPoint, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metrics, exists := mc.metrics[jobID]
	if !exists {
		return nil, errors.ErrServiceNotFound(jobID)
	}

	return metrics.LossHistory, nil
}

// GetAggregatedMetrics returns aggregated metrics across multiple jobs.
func (mc *MetricsCollectorImpl) GetAggregatedMetrics(jobIDs []string) (AggregatedMetrics, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var (
		allMetrics         []TrainingMetrics
		startTime, endTime time.Time
	)

	for _, jobID := range jobIDs {
		if metrics, exists := mc.metrics[jobID]; exists {
			allMetrics = append(allMetrics, *metrics)

			if startTime.IsZero() || metrics.StartTime.Before(startTime) {
				startTime = metrics.StartTime
			}

			if endTime.IsZero() || metrics.EndTime.After(endTime) {
				endTime = metrics.EndTime
			}
		}
	}

	if len(allMetrics) == 0 {
		return AggregatedMetrics{}, errors.New("no metrics found for provided job IDs")
	}

	// Calculate aggregated metrics
	aggregated := AggregatedMetrics{
		JobCount:       len(allMetrics),
		MetricsSummary: make(map[string]MetricSummary),
		TimeRange: TimeRange{
			Start: startTime,
			End:   endTime,
		},
	}

	// Calculate success rate
	successCount := 0

	var (
		totalAccuracy, totalLoss, totalDuration float64
		bestAccuracy, bestLoss                  float64 = -1, math.Inf(1)
	)

	for _, metrics := range allMetrics {
		if metrics.Status == TrainingStatusCompleted {
			successCount++
		}

		totalAccuracy += metrics.Accuracy
		totalLoss += metrics.ValidationLoss
		totalDuration += float64(metrics.Duration)

		if metrics.Accuracy > bestAccuracy {
			bestAccuracy = metrics.Accuracy
		}

		if metrics.ValidationLoss < bestLoss {
			bestLoss = metrics.ValidationLoss
		}
	}

	aggregated.SuccessRate = float64(successCount) / float64(len(allMetrics))
	aggregated.AverageAccuracy = totalAccuracy / float64(len(allMetrics))
	aggregated.AverageLoss = totalLoss / float64(len(allMetrics))
	aggregated.AverageDuration = time.Duration(totalDuration / float64(len(allMetrics)))
	aggregated.BestAccuracy = bestAccuracy
	aggregated.BestLoss = bestLoss

	return aggregated, nil
}

// ExportMetrics exports metrics in the specified format.
func (mc *MetricsCollectorImpl) ExportMetrics(jobID string, format string) ([]byte, error) {
	metrics, err := mc.GetMetrics(jobID)
	if err != nil {
		return nil, err
	}

	switch format {
	case "json":
		return mc.exportJSON(metrics)
	case "csv":
		return mc.exportCSV(metrics)
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

// Helper methods

func (mc *MetricsCollectorImpl) getOrCreateMetrics(jobID string) *TrainingMetrics {
	if metrics, exists := mc.metrics[jobID]; exists {
		return metrics
	}

	metrics := &TrainingMetrics{
		JobID:         jobID,
		StartTime:     time.Now(),
		Status:        TrainingStatusPending,
		CustomMetrics: make(map[string]float64),
		ClassMetrics:  make(map[string]ClassificationMetrics),
		LossHistory:   make([]LossPoint, 0),
		Checkpoints:   make([]CheckpointInfo, 0),
		EarlyStopping: EarlyStoppingMetrics{},
		ResourceUsage: ResourceUsageMetrics{},
	}

	mc.metrics[jobID] = metrics

	return metrics
}

func (mc *MetricsCollectorImpl) exportJSON(metrics TrainingMetrics) ([]byte, error) {
	// Implementation would use encoding/json
	return []byte("{}"), nil // Placeholder
}

func (mc *MetricsCollectorImpl) exportCSV(metrics TrainingMetrics) ([]byte, error) {
	// Implementation would create CSV format
	return []byte(""), nil // Placeholder
}

// MetricsEvaluatorImpl implements the MetricsEvaluator interface.
type MetricsEvaluatorImpl struct {
	logger logger.Logger
}

// NewMetricsEvaluator creates a new metrics evaluator.
func NewMetricsEvaluator(logger logger.Logger) MetricsEvaluator {
	return &MetricsEvaluatorImpl{
		logger: logger,
	}
}

// EvaluateClassification evaluates classification performance.
func (me *MetricsEvaluatorImpl) EvaluateClassification(predictions, labels []int, classNames []string) (ClassificationEvaluation, error) {
	if len(predictions) != len(labels) {
		return ClassificationEvaluation{}, errors.New("predictions and labels must have same length")
	}

	numClasses := len(classNames)

	confusionMatrix := make([][]int, numClasses)
	for i := range confusionMatrix {
		confusionMatrix[i] = make([]int, numClasses)
	}

	// Build confusion matrix
	correct := 0

	for i := range len(predictions) {
		pred := predictions[i]
		label := labels[i]

		if pred >= 0 && pred < numClasses && label >= 0 && label < numClasses {
			confusionMatrix[label][pred]++
			if pred == label {
				correct++
			}
		}
	}

	// Calculate overall accuracy
	accuracy := float64(correct) / float64(len(predictions))

	// Calculate per-class metrics
	classReport := make(map[string]ClassificationMetrics)

	var macroPrec, macroRec, macroF1 float64

	for i, className := range classNames {
		tp := confusionMatrix[i][i]
		fn := 0
		fp := 0
		tn := 0

		for j := range numClasses {
			if j != i {
				fn += confusionMatrix[i][j] // False negatives
				fp += confusionMatrix[j][i] // False positives
			}
		}

		// Calculate total negatives
		total := len(predictions)
		tn = total - tp - fn - fp

		precision := float64(tp) / float64(tp+fp)
		recall := float64(tp) / float64(tp+fn)
		f1 := 2 * precision * recall / (precision + recall)

		if math.IsNaN(precision) {
			precision = 0
		}

		if math.IsNaN(recall) {
			recall = 0
		}

		if math.IsNaN(f1) {
			f1 = 0
		}

		classReport[className] = ClassificationMetrics{
			ClassName:      className,
			Precision:      precision,
			Recall:         recall,
			F1Score:        f1,
			Support:        tp + fn,
			TruePositives:  tp,
			FalsePositives: fp,
			TrueNegatives:  tn,
			FalseNegatives: fn,
		}

		macroPrec += precision
		macroRec += recall
		macroF1 += f1
	}

	// Calculate macro averages
	macroPrec /= float64(numClasses)
	macroRec /= float64(numClasses)
	macroF1 /= float64(numClasses)

	return ClassificationEvaluation{
		Accuracy:             accuracy,
		MacroAvgPrecision:    macroPrec,
		MacroAvgRecall:       macroRec,
		MacroAvgF1:           macroF1,
		ConfusionMatrix:      confusionMatrix,
		ClassificationReport: classReport,
		AUC:                  0, // Would need probability scores to calculate
		LogLoss:              0, // Would need probability scores to calculate
	}, nil
}

// EvaluateMultilabelClassification evaluates multilabel classification.
func (me *MetricsEvaluatorImpl) EvaluateMultilabelClassification(predictions, labels [][]int, classNames []string) (MultilabelEvaluation, error) {
	// Implementation for multilabel classification
	return MultilabelEvaluation{}, nil // Placeholder
}

// EvaluateRegression evaluates regression performance.
func (me *MetricsEvaluatorImpl) EvaluateRegression(predictions, targets []float64) (RegressionEvaluation, error) {
	if len(predictions) != len(targets) {
		return RegressionEvaluation{}, errors.New("predictions and targets must have same length")
	}

	n := float64(len(predictions))

	var (
		sumSquaredError, sumAbsError, sumTargets, sumSquaredTargets float64
		maxError                                                    float64
		absErrors                                                   []float64
	)

	for i := range len(predictions) {
		pred := predictions[i]
		target := targets[i]

		error := pred - target
		absError := math.Abs(error)
		sqError := error * error

		sumSquaredError += sqError
		sumAbsError += absError
		sumTargets += target
		sumSquaredTargets += target * target

		if absError > maxError {
			maxError = absError
		}

		absErrors = append(absErrors, absError)
	}

	// Calculate metrics
	mae := sumAbsError / n
	mse := sumSquaredError / n
	rmse := math.Sqrt(mse)

	// Calculate R2 score
	meanTarget := sumTargets / n

	var totalSumSquares float64

	for _, target := range targets {
		diff := target - meanTarget
		totalSumSquares += diff * diff
	}

	r2 := 1.0 - (sumSquaredError / totalSumSquares)

	// Calculate median absolute error
	sort.Float64s(absErrors)

	var medianAE float64
	if len(absErrors)%2 == 0 {
		medianAE = (absErrors[len(absErrors)/2-1] + absErrors[len(absErrors)/2]) / 2
	} else {
		medianAE = absErrors[len(absErrors)/2]
	}

	// Calculate MAPE
	var sumPercentageError float64

	validCount := 0

	for i := range len(predictions) {
		if targets[i] != 0 {
			percentageError := math.Abs((targets[i] - predictions[i]) / targets[i])
			sumPercentageError += percentageError
			validCount++
		}
	}

	var mape float64
	if validCount > 0 {
		mape = (sumPercentageError / float64(validCount)) * 100
	}

	// Calculate explained variance
	var predVariance float64

	meanPred := sumTargets / n // Using target mean as approximation
	for _, pred := range predictions {
		diff := pred - meanPred
		predVariance += diff * diff
	}

	predVariance /= n

	targetVariance := (sumSquaredTargets / n) - (meanTarget * meanTarget)

	var explainedVariance float64
	if targetVariance != 0 {
		explainedVariance = 1.0 - (mse / targetVariance)
	}

	return RegressionEvaluation{
		MAE:               mae,
		MSE:               mse,
		RMSE:              rmse,
		R2Score:           r2,
		MedianAE:          medianAE,
		MaxError:          maxError,
		MAPE:              mape,
		ExplainedVariance: explainedVariance,
	}, nil
}

// EvaluateMultiOutputRegression evaluates multi-output regression.
func (me *MetricsEvaluatorImpl) EvaluateMultiOutputRegression(predictions, targets [][]float64) (MultiOutputRegressionEvaluation, error) {
	// Implementation for multi-output regression
	return MultiOutputRegressionEvaluation{}, nil // Placeholder
}

// EvaluateRanking evaluates ranking performance.
func (me *MetricsEvaluatorImpl) EvaluateRanking(predictions [][]float64, relevance [][]int) (RankingEvaluation, error) {
	// Implementation for ranking evaluation
	return RankingEvaluation{}, nil // Placeholder
}

// EvaluateTimeSeries evaluates time series performance.
func (me *MetricsEvaluatorImpl) EvaluateTimeSeries(predictions, targets []float64, timestamps []time.Time) (TimeSeriesEvaluation, error) {
	// Start with regression metrics
	regEval, err := me.EvaluateRegression(predictions, targets)
	if err != nil {
		return TimeSeriesEvaluation{}, err
	}

	// Calculate time series specific metrics
	var sumAbsPercentageError, sumActual float64

	validCount := 0

	for i := range len(predictions) {
		if targets[i] != 0 {
			absPercentageError := math.Abs(targets[i] - predictions[i])
			sumAbsPercentageError += absPercentageError
			sumActual += math.Abs(targets[i])
			validCount++
		}
	}

	var smape, wape float64
	if validCount > 0 {
		// SMAPE (Symmetric Mean Absolute Percentage Error)
		smape = (sumAbsPercentageError / float64(validCount)) * 100 / 2

		// WAPE (Weighted Absolute Percentage Error)
		if sumActual != 0 {
			wape = (sumAbsPercentageError / sumActual) * 100
		}
	}

	// Calculate bias
	var sumError float64
	for i := range len(predictions) {
		sumError += predictions[i] - targets[i]
	}

	bias := sumError / float64(len(predictions))

	return TimeSeriesEvaluation{
		RegressionMetrics: regEval,
		SMAPE:             smape,
		WAPE:              wape,
		Bias:              bias,
		Seasonality:       SeasonalityMetrics{}, // Would require more complex analysis
		Trend:             TrendMetrics{},       // Would require more complex analysis
	}, nil
}

// EvaluateCustom evaluates using a custom evaluator function.
func (me *MetricsEvaluatorImpl) EvaluateCustom(predictions, targets interface{}, evaluator func(interface{}, interface{}) (map[string]float64, error)) (CustomEvaluation, error) {
	metrics, err := evaluator(predictions, targets)
	if err != nil {
		return CustomEvaluation{}, err
	}

	return CustomEvaluation{
		Metrics:  metrics,
		Metadata: make(map[string]interface{}),
	}, nil
}
