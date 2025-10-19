package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/ai/models"
	"github.com/xraph/forge/pkg/ai/training"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/pluginengine/common"
)

// TrainingPlugin defines the interface for training plugins
type TrainingPlugin interface {
	common.PluginEngine

	// Training capabilities
	CreateTrainer(config training.TrainerConfig) (training.ModelTrainer, error)
	GetSupportedModelTypes() []models.ModelType
	GetTrainingCapabilities() []TrainingCapability

	// Pipeline management
	CreateTrainingPipeline(config training.PipelineConfig) (training.TrainingPipeline, error)
	GetTrainingPipelines() []training.TrainingPipeline
	DeleteTrainingPipeline(pipelineID string) error

	// Training operations
	StartTraining(ctx context.Context, request TrainingRequest) (TrainingJob, error)
	StopTraining(ctx context.Context, jobID string) error
	GetTrainingJob(jobID string) (TrainingJob, error)
	ListTrainingJobs() []TrainingJob

	// Dataset management
	PrepareDataset(ctx context.Context, config DatasetConfig) (Dataset, error)
	ValidateDataset(ctx context.Context, dataset Dataset) error
	GetSupportedDatasetTypes() []DatasetType

	// Model lifecycle
	SaveTrainedModel(ctx context.Context, jobID string, path string) (TrainedModel, error)
	LoadTrainedModel(ctx context.Context, path string) (TrainedModel, error)
	ExportModel(ctx context.Context, jobID string, format ExportFormat) ([]byte, error)

	// Training monitoring and optimization
	GetTrainingMetrics(jobID string) (TrainingMetrics, error)
	GetTrainingRecommendations(ctx context.Context, jobID string) ([]TrainingRecommendation, error)
	OptimizeHyperparameters(ctx context.Context, config HyperparameterOptimizationConfig) (OptimizationResult, error)

	// Distributed training
	CreateDistributedTraining(ctx context.Context, config DistributedTrainingConfig) (DistributedTrainingJob, error)
	GetDistributedTrainingStatus(jobID string) (DistributedTrainingStatus, error)

	// Plugin-specific extensions
	GetCustomTrainers() []CustomTrainer
	RegisterCustomTrainer(trainer CustomTrainer) error
}

// TrainingCapability represents a specific training capability
type TrainingCapability struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	ModelTypes    []models.ModelType     `json:"model_types"`
	TrainingTypes []TrainingType         `json:"training_types"`
	DatasetTypes  []DatasetType          `json:"dataset_types"`
	Features      []string               `json:"features"`
	Optimizers    []string               `json:"optimizers"`
	LossFunctions []string               `json:"loss_functions"`
	Requirements  ResourceRequirements   `json:"requirements"`
	Performance   PerformanceMetrics     `json:"performance"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// TrainingType defines the type of training supported
type TrainingType string

const (
	TrainingTypeSupervised       TrainingType = "supervised"
	TrainingTypeUnsupervised     TrainingType = "unsupervised"
	TrainingTypeReinforcement    TrainingType = "reinforcement"
	TrainingTypeSelfSupervised   TrainingType = "self_supervised"
	TrainingTypeTransferLearning TrainingType = "transfer_learning"
	TrainingTypeFederated        TrainingType = "federated"
	TrainingTypeDistributed      TrainingType = "distributed"
	TrainingTypeIncremental      TrainingType = "incremental"
)

// DatasetType defines the type of dataset supported
type DatasetType string

const (
	DatasetTypeText       DatasetType = "text"
	DatasetTypeImage      DatasetType = "image"
	DatasetTypeAudio      DatasetType = "audio"
	DatasetTypeVideo      DatasetType = "video"
	DatasetTypeTabular    DatasetType = "tabular"
	DatasetTypeTimeSeries DatasetType = "time_series"
	DatasetTypeGraph      DatasetType = "graph"
	DatasetTypeMultimodal DatasetType = "multimodal"
	DatasetTypeCustom     DatasetType = "custom"
)

// ExportFormat defines model export formats
type ExportFormat string

const (
	ExportFormatONNX       ExportFormat = "onnx"
	ExportFormatTensorFlow ExportFormat = "tensorflow"
	ExportFormatPyTorch    ExportFormat = "pytorch"
	ExportFormatTFLite     ExportFormat = "tflite"
	ExportFormatCoreML     ExportFormat = "coreml"
	ExportFormatOpenVINO   ExportFormat = "openvino"
	ExportFormatTensorRT   ExportFormat = "tensorrt"
	ExportFormatCustom     ExportFormat = "custom"
)

// TrainingRequest represents a training request specific to plugins
type TrainingRequest struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	ModelType        string                 `json:"model_type"`
	TrainingType     TrainingType           `json:"training_type"`
	ModelConfig      models.ModelConfig     `json:"model_config"`
	TrainingConfig   TrainingConfig         `json:"training_config"`
	DatasetConfig    DatasetConfig          `json:"dataset_config"`
	HyperParameters  map[string]interface{} `json:"hyperparameters"`
	ValidationConfig ValidationConfig       `json:"validation_config"`
	CallbackConfig   CallbackConfig         `json:"callback_config"`
	ResourceConfig   ResourceConfig         `json:"resources"`
	Tags             map[string]string      `json:"tags"`
	Priority         int                    `json:"priority"`
	Metadata         map[string]interface{} `json:"metadata"`
}

// TrainingConfig contains training configuration specific to plugins
type TrainingConfig struct {
	Epochs             int                    `json:"epochs"`
	BatchSize          int                    `json:"batch_size"`
	LearningRate       float64                `json:"learning_rate"`
	Optimizer          string                 `json:"optimizer"`
	LossFunction       string                 `json:"loss_function"`
	OptimizerConfig    map[string]interface{} `json:"optimizer_config"`
	LossConfig         map[string]interface{} `json:"loss_config"`
	Regularization     RegularizationConfig   `json:"regularization"`
	EarlyStopping      EarlyStoppingConfig    `json:"early_stopping"`
	LRScheduler        LRSchedulerConfig      `json:"lr_scheduler"`
	Checkpoints        CheckpointConfig       `json:"checkpoints"`
	AugmentationConfig AugmentationConfig     `json:"augmentation"`
	MixedPrecision     bool                   `json:"mixed_precision"`
	GradientClipping   GradientClippingConfig `json:"gradient_clipping"`
	Environment        map[string]string      `json:"environment"`
	CustomConfig       map[string]interface{} `json:"custom_config"`
}

// DatasetConfig contains dataset configuration for plugins
type DatasetConfig struct {
	Type          DatasetType            `json:"type"`
	Source        DatasetSource          `json:"source"`
	Format        string                 `json:"format"`
	SplitConfig   DataSplitConfig        `json:"split_config"`
	Preprocessing PreprocessingConfig    `json:"preprocessing"`
	Validation    DataValidationConfig   `json:"validation"`
	Caching       CachingConfig          `json:"caching"`
	Streaming     bool                   `json:"streaming"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// DatasetSource defines the source of training data
type DatasetSource struct {
	Type        string                 `json:"type"` // file, url, database, s3, gcs
	Location    string                 `json:"location"`
	Credentials map[string]interface{} `json:"credentials,omitempty"`
	Format      string                 `json:"format"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// DataSplitConfig contains data splitting configuration
type DataSplitConfig struct {
	TrainRatio      float64 `json:"train_ratio"`
	ValidationRatio float64 `json:"validation_ratio"`
	TestRatio       float64 `json:"test_ratio"`
	StratifyBy      string  `json:"stratify_by,omitempty"`
	RandomSeed      int     `json:"random_seed"`
	ShuffleData     bool    `json:"shuffle_data"`
}

// PreprocessingConfig contains data preprocessing configuration
type PreprocessingConfig struct {
	Steps         []PreprocessingStep    `json:"steps"`
	Normalization NormalizationConfig    `json:"normalization"`
	Tokenization  TokenizationConfig     `json:"tokenization,omitempty"`
	Encoding      EncodingConfig         `json:"encoding,omitempty"`
	Custom        map[string]interface{} `json:"custom"`
}

// PreprocessingStep defines a preprocessing step
type PreprocessingStep struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Parameters map[string]interface{} `json:"parameters"`
	Enabled    bool                   `json:"enabled"`
}

// NormalizationConfig contains normalization configuration
type NormalizationConfig struct {
	Type       string  `json:"type"` // min-max, z-score, unit-vector
	Min        float64 `json:"min,omitempty"`
	Max        float64 `json:"max,omitempty"`
	Mean       float64 `json:"mean,omitempty"`
	Std        float64 `json:"std,omitempty"`
	Axis       int     `json:"axis,omitempty"`
	PerChannel bool    `json:"per_channel"`
}

// TokenizationConfig contains tokenization configuration
type TokenizationConfig struct {
	Tokenizer     string                 `json:"tokenizer"`
	VocabSize     int                    `json:"vocab_size"`
	MaxLength     int                    `json:"max_length"`
	PaddingToken  string                 `json:"padding_token"`
	UnknownToken  string                 `json:"unknown_token"`
	SpecialTokens map[string]string      `json:"special_tokens"`
	Parameters    map[string]interface{} `json:"parameters"`
}

// EncodingConfig contains encoding configuration
type EncodingConfig struct {
	Type          string                 `json:"type"`
	Categories    []string               `json:"categories,omitempty"`
	HandleUnknown string                 `json:"handle_unknown"`
	Parameters    map[string]interface{} `json:"parameters"`
}

// DataValidationConfig contains data validation configuration
type DataValidationConfig struct {
	Rules             []ValidationRule       `json:"rules"`
	SchemaValidation  bool                   `json:"schema_validation"`
	QualityChecks     []QualityCheck         `json:"quality_checks"`
	Sampling          SamplingConfig         `json:"sampling"`
	CustomValidations map[string]interface{} `json:"custom_validations"`
}

// ValidationRule defines a data validation rule
type ValidationRule struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Field      string                 `json:"field"`
	Condition  string                 `json:"condition"`
	Parameters map[string]interface{} `json:"parameters"`
	Severity   string                 `json:"severity"`
}

// QualityCheck defines a data quality check
type QualityCheck struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Threshold  float64                `json:"threshold"`
	Parameters map[string]interface{} `json:"parameters"`
}

// SamplingConfig contains sampling configuration for validation
type SamplingConfig struct {
	Strategy   string  `json:"strategy"`
	Size       int     `json:"size"`
	Percentage float64 `json:"percentage"`
	RandomSeed int     `json:"random_seed"`
}

// CachingConfig contains caching configuration
type CachingConfig struct {
	Enabled     bool          `json:"enabled"`
	Type        string        `json:"type"` // memory, disk, distributed
	MaxSize     int64         `json:"max_size"`
	TTL         time.Duration `json:"ttl"`
	Compression bool          `json:"compression"`
}

// ValidationConfig contains validation configuration for plugins
type ValidationConfig struct {
	Strategy        ValidationStrategy    `json:"strategy"`
	SplitRatio      float64               `json:"split_ratio"`
	ValidationSet   string                `json:"validation_set,omitempty"`
	Metrics         []string              `json:"metrics"`
	EvaluationFreq  int                   `json:"evaluation_frequency"`
	CrossValidation CrossValidationConfig `json:"cross_validation"`
	CustomMetrics   []CustomMetric        `json:"custom_metrics"`
}

// ValidationStrategy defines validation strategy
type ValidationStrategy string

const (
	ValidationStrategyHoldout         ValidationStrategy = "holdout"
	ValidationStrategyKFold           ValidationStrategy = "k_fold"
	ValidationStrategyTimeSeriesSplit ValidationStrategy = "time_series"
	ValidationStrategyCustom          ValidationStrategy = "custom"
)

// CrossValidationConfig contains cross-validation configuration
type CrossValidationConfig struct {
	Enabled    bool `json:"enabled"`
	Folds      int  `json:"folds"`
	Shuffle    bool `json:"shuffle"`
	Stratified bool `json:"stratified"`
	RandomSeed int  `json:"random_seed"`
}

// CustomMetric defines a custom validation metric
type CustomMetric struct {
	Name         string                 `json:"name"`
	Function     string                 `json:"function"`
	Parameters   map[string]interface{} `json:"parameters"`
	HigherBetter bool                   `json:"higher_better"`
}

// CallbackConfig contains callback configuration
type CallbackConfig struct {
	Callbacks          []TrainingCallback       `json:"callbacks"`
	LoggingConfig      LoggingConfig            `json:"logging"`
	MonitoringConfig   TrainingMonitoringConfig `json:"monitoring"`
	NotificationConfig NotificationConfig       `json:"notification"`
}

// TrainingCallback defines a training callback
type TrainingCallback struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Config   map[string]interface{} `json:"config"`
	Priority int                    `json:"priority"`
	Enabled  bool                   `json:"enabled"`
}

// LoggingConfig contains logging configuration for training
type LoggingConfig struct {
	Level       string `json:"level"`
	Format      string `json:"format"`
	OutputPath  string `json:"output_path"`
	MaxFileSize int64  `json:"max_file_size"`
	MaxFiles    int    `json:"max_files"`
	Structured  bool   `json:"structured"`
}

// TrainingMonitoringConfig contains monitoring configuration for training
type TrainingMonitoringConfig struct {
	Enabled         bool            `json:"enabled"`
	UpdateInterval  time.Duration   `json:"update_interval"`
	MetricsEndpoint string          `json:"metrics_endpoint"`
	Visualization   bool            `json:"visualization"`
	Dashboard       DashboardConfig `json:"dashboard"`
}

// DashboardConfig contains dashboard configuration
type DashboardConfig struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	Host    string `json:"host"`
	Theme   string `json:"theme"`
}

// NotificationConfig contains notification configuration
type NotificationConfig struct {
	Enabled   bool                  `json:"enabled"`
	Channels  []NotificationChannel `json:"channels"`
	Events    []NotificationEvent   `json:"events"`
	Templates map[string]string     `json:"templates"`
}

// NotificationChannel defines a notification channel
type NotificationChannel struct {
	Type    string                 `json:"type"` // email, slack, webhook
	Config  map[string]interface{} `json:"config"`
	Enabled bool                   `json:"enabled"`
}

// NotificationEvent defines when to send notifications
type NotificationEvent struct {
	Event     string `json:"event"`
	Condition string `json:"condition"`
	Enabled   bool   `json:"enabled"`
}

// ResourceConfig contains resource configuration for training
type ResourceConfig struct {
	CPU         string        `json:"cpu"`
	Memory      string        `json:"memory"`
	GPU         int           `json:"gpu"`
	GPUType     string        `json:"gpu_type"`
	Storage     string        `json:"storage"`
	StorageType string        `json:"storage_type"`
	NodeType    string        `json:"node_type,omitempty"`
	Priority    int           `json:"priority"`
	MaxRuntime  time.Duration `json:"max_runtime"`
	Scaling     ScalingConfig `json:"scaling"`
}

// ScalingConfig contains auto-scaling configuration
type ScalingConfig struct {
	Enabled  bool            `json:"enabled"`
	MinNodes int             `json:"min_nodes"`
	MaxNodes int             `json:"max_nodes"`
	Metrics  []ScalingMetric `json:"metrics"`
	Policy   ScalingPolicy   `json:"policy"`
}

// ScalingMetric defines a metric for auto-scaling
type ScalingMetric struct {
	Name      string  `json:"name"`
	Threshold float64 `json:"threshold"`
	Operation string  `json:"operation"` // greater_than, less_than
}

// ScalingPolicy defines scaling behavior
type ScalingPolicy struct {
	ScaleUpCooldown   time.Duration `json:"scale_up_cooldown"`
	ScaleDownCooldown time.Duration `json:"scale_down_cooldown"`
	ScaleUpStep       int           `json:"scale_up_step"`
	ScaleDownStep     int           `json:"scale_down_step"`
}

// RegularizationConfig contains regularization settings
type RegularizationConfig struct {
	L1          float64 `json:"l1"`
	L2          float64 `json:"l2"`
	Dropout     float64 `json:"dropout"`
	BatchNorm   bool    `json:"batch_norm"`
	LayerNorm   bool    `json:"layer_norm"`
	WeightDecay float64 `json:"weight_decay"`
}

// EarlyStoppingConfig contains early stopping settings
type EarlyStoppingConfig struct {
	Enabled       bool     `json:"enabled"`
	Metric        string   `json:"metric"`
	Mode          string   `json:"mode"` // min, max
	Patience      int      `json:"patience"`
	MinDelta      float64  `json:"min_delta"`
	RestoreBest   bool     `json:"restore_best"`
	BaselineValue *float64 `json:"baseline_value,omitempty"`
}

// LRSchedulerConfig contains learning rate scheduler settings
type LRSchedulerConfig struct {
	Type        string                 `json:"type"`
	Parameters  map[string]interface{} `json:"parameters"`
	WarmupSteps int                    `json:"warmup_steps"`
}

// CheckpointConfig contains checkpoint settings
type CheckpointConfig struct {
	Enabled       bool   `json:"enabled"`
	SaveFreq      int    `json:"save_frequency"` // epochs
	SavePath      string `json:"save_path"`
	SaveBest      bool   `json:"save_best"`
	SaveLast      bool   `json:"save_last"`
	MaxKeep       int    `json:"max_keep"`
	SaveOptimizer bool   `json:"save_optimizer"`
	Compression   bool   `json:"compression"`
}

// AugmentationConfig contains data augmentation settings
type AugmentationConfig struct {
	Enabled      bool                    `json:"enabled"`
	Techniques   []AugmentationTechnique `json:"techniques"`
	Probability  float64                 `json:"probability"`
	CustomConfig map[string]interface{}  `json:"custom_config"`
}

// AugmentationTechnique defines an augmentation technique
type AugmentationTechnique struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Parameters  map[string]interface{} `json:"parameters"`
	Probability float64                `json:"probability"`
}

// GradientClippingConfig contains gradient clipping settings
type GradientClippingConfig struct {
	Enabled bool    `json:"enabled"`
	Type    string  `json:"type"` // norm, value
	Value   float64 `json:"value"`
	Norm    float64 `json:"norm"`
}

// TrainingProgress represents training progress
type TrainingProgress struct {
	Epoch          int                `json:"epoch"`
	TotalEpochs    int                `json:"total_epochs"`
	Step           int64              `json:"step"`
	TotalSteps     int64              `json:"total_steps"`
	Percentage     float64            `json:"percentage"`
	EstimatedTime  time.Duration      `json:"estimated_time"`
	ElapsedTime    time.Duration      `json:"elapsed_time"`
	RemainingTime  time.Duration      `json:"remaining_time"`
	CurrentLoss    float64            `json:"current_loss"`
	CurrentMetrics map[string]float64 `json:"current_metrics"`
}

// Dataset represents a training dataset
type Dataset interface {
	ID() string
	Name() string
	Type() DatasetType
	Size() int64
	ItemCount() int64
	GetSample(index int) (interface{}, error)
	GetBatch(indices []int) ([]interface{}, error)
	GetStats() DatasetStats
	Validate(ctx context.Context) error
}

// DatasetStats contains dataset statistics
type DatasetStats struct {
	TotalItems       int64                  `json:"total_items"`
	TotalSize        int64                  `json:"total_size"`
	ItemDistribution map[string]int64       `json:"item_distribution"`
	FeatureStats     map[string]FeatureStat `json:"feature_stats"`
	QualityScore     float64                `json:"quality_score"`
	Completeness     float64                `json:"completeness"`
	Consistency      float64                `json:"consistency"`
}

// FeatureStat contains statistics for a feature
type FeatureStat struct {
	Type         string           `json:"type"`
	Count        int64            `json:"count"`
	Missing      int64            `json:"missing"`
	Unique       int64            `json:"unique"`
	Mean         *float64         `json:"mean,omitempty"`
	Std          *float64         `json:"std,omitempty"`
	Min          interface{}      `json:"min,omitempty"`
	Max          interface{}      `json:"max,omitempty"`
	Distribution map[string]int64 `json:"distribution,omitempty"`
}

// TrainedModel represents a trained model from plugins
type TrainedModel interface {
	ID() string
	Name() string
	Version() string
	ModelType() models.ModelType
	Framework() string
	TrainingJobID() string
	CreatedAt() time.Time
	GetMetrics() TrainingMetrics
	GetConfig() models.ModelConfig
	Save(ctx context.Context, path string) error
	Load(ctx context.Context, path string) error
	Export(ctx context.Context, format ExportFormat) ([]byte, error)
	Validate(ctx context.Context, dataset Dataset) (ValidationResults, error)
}

// ValidationResults contains model validation results
type ValidationResults struct {
	Accuracy  float64            `json:"accuracy"`
	Loss      float64            `json:"loss"`
	Metrics   map[string]float64 `json:"metrics"`
	Confusion [][]int            `json:"confusion_matrix,omitempty"`
	Report    string             `json:"classification_report,omitempty"`
	ROCCurve  []ROCPoint         `json:"roc_curve,omitempty"`
	PRCurve   []PRPoint          `json:"pr_curve,omitempty"`
}

// ROCPoint represents a point on ROC curve
type ROCPoint struct {
	FPR float64 `json:"fpr"`
	TPR float64 `json:"tpr"`
}

// PRPoint represents a point on Precision-Recall curve
type PRPoint struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
}

// TrainingMetrics contains comprehensive training metrics
type TrainingMetrics struct {
	JobID              string             `json:"job_id"`
	Epoch              int                `json:"epoch"`
	Step               int64              `json:"step"`
	Loss               float64            `json:"loss"`
	ValidationLoss     float64            `json:"validation_loss"`
	Accuracy           float64            `json:"accuracy"`
	ValidationAccuracy float64            `json:"validation_accuracy"`
	LearningRate       float64            `json:"learning_rate"`
	CustomMetrics      map[string]float64 `json:"custom_metrics"`
	ResourceMetrics    ResourceMetrics    `json:"resource_metrics"`
	Timestamp          time.Time          `json:"timestamp"`
}

// ResourceMetrics contains resource usage metrics during training
type ResourceMetrics struct {
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage int64   `json:"memory_usage"`
	GPUUsage    float64 `json:"gpu_usage"`
	GPUMemory   int64   `json:"gpu_memory"`
	NetworkIO   int64   `json:"network_io"`
	DiskIO      int64   `json:"disk_io"`
	PowerUsage  float64 `json:"power_usage"`
}

// TrainingLogEntry represents a training log entry
type TrainingLogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	JobID     string                 `json:"job_id"`
	Epoch     int                    `json:"epoch,omitempty"`
	Step      int64                  `json:"step,omitempty"`
	Metrics   map[string]interface{} `json:"metrics,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// TrainingRecommendation represents a training recommendation
type TrainingRecommendation struct {
	Type          RecommendationType     `json:"type"`
	Title         string                 `json:"title"`
	Description   string                 `json:"description"`
	Impact        ImpactLevel            `json:"impact"`
	Confidence    float64                `json:"confidence"`
	Action        string                 `json:"action"`
	Parameters    map[string]interface{} `json:"parameters"`
	Evidence      []string               `json:"evidence"`
	EstimatedGain EstimatedGain          `json:"estimated_gain"`
}

// RecommendationType defines the type of recommendation
type RecommendationType string

const (
	RecommendationTypeLearningRate   RecommendationType = "learning_rate"
	RecommendationTypeBatchSize      RecommendationType = "batch_size"
	RecommendationTypeOptimizer      RecommendationType = "optimizer"
	RecommendationTypeRegularization RecommendationType = "regularization"
	RecommendationTypeArchitecture   RecommendationType = "architecture"
	RecommendationTypeDataset        RecommendationType = "dataset"
	RecommendationTypeResource       RecommendationType = "resource"
)

// ImpactLevel defines the impact level of a recommendation
type ImpactLevel string

const (
	ImpactLevelLow    ImpactLevel = "low"
	ImpactLevelMedium ImpactLevel = "medium"
	ImpactLevelHigh   ImpactLevel = "high"
)

// EstimatedGain represents estimated improvement from a recommendation
type EstimatedGain struct {
	AccuracyImprovement float64       `json:"accuracy_improvement"`
	SpeedImprovement    float64       `json:"speed_improvement"`
	ResourceSavings     float64       `json:"resource_savings"`
	TimeToTrain         time.Duration `json:"time_to_train"`
}

// HyperparameterOptimizationConfig contains hyperparameter optimization configuration
type HyperparameterOptimizationConfig struct {
	Algorithm        OptimizationAlgorithm  `json:"algorithm"`
	SearchSpace      map[string]SearchSpace `json:"search_space"`
	Objective        ObjectiveConfig        `json:"objective"`
	Budget           OptimizationBudget     `json:"budget"`
	EarlyTermination EarlyTerminationConfig `json:"early_termination"`
	Parallelism      int                    `json:"parallelism"`
	RandomSeed       int                    `json:"random_seed"`
}

// OptimizationAlgorithm defines the optimization algorithm
type OptimizationAlgorithm string

const (
	OptimizationAlgorithmRandom    OptimizationAlgorithm = "random"
	OptimizationAlgorithmGrid      OptimizationAlgorithm = "grid"
	OptimizationAlgorithmBayesian  OptimizationAlgorithm = "bayesian"
	OptimizationAlgorithmTPE       OptimizationAlgorithm = "tpe"
	OptimizationAlgorithmHyperband OptimizationAlgorithm = "hyperband"
	OptimizationAlgorithmBOHB      OptimizationAlgorithm = "bohb"
)

// SearchSpace defines the search space for a hyperparameter
type SearchSpace struct {
	Type     string        `json:"type"` // float, int, categorical
	Low      interface{}   `json:"low,omitempty"`
	High     interface{}   `json:"high,omitempty"`
	Choices  []interface{} `json:"choices,omitempty"`
	LogScale bool          `json:"log_scale"`
	Step     interface{}   `json:"step,omitempty"`
}

// ObjectiveConfig defines the optimization objective
type ObjectiveConfig struct {
	Metric    string   `json:"metric"`
	Direction string   `json:"direction"` // minimize, maximize
	Target    *float64 `json:"target,omitempty"`
}

// OptimizationBudget defines the optimization budget
type OptimizationBudget struct {
	MaxTrials    int            `json:"max_trials"`
	MaxDuration  time.Duration  `json:"max_duration"`
	MaxResources ResourceConfig `json:"max_resources"`
	EarlyStop    bool           `json:"early_stop"`
}

// EarlyTerminationConfig contains early termination configuration
type EarlyTerminationConfig struct {
	Enabled   bool    `json:"enabled"`
	Policy    string  `json:"policy"` // median, truncation
	Min_t     int     `json:"min_t"`
	Grace_t   int     `json:"grace_t"`
	Reduction float64 `json:"reduction_factor"`
}

// OptimizationResult contains the result of hyperparameter optimization
type OptimizationResult struct {
	BestParams  map[string]interface{} `json:"best_params"`
	BestScore   float64                `json:"best_score"`
	TotalTrials int                    `json:"total_trials"`
	Duration    time.Duration          `json:"duration"`
	History     []OptimizationTrial    `json:"history"`
	Convergence ConvergenceInfo        `json:"convergence"`
}

// OptimizationTrial represents a single optimization trial
type OptimizationTrial struct {
	TrialID    int                    `json:"trial_id"`
	Parameters map[string]interface{} `json:"parameters"`
	Score      float64                `json:"score"`
	Duration   time.Duration          `json:"duration"`
	Status     string                 `json:"status"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
}

// ConvergenceInfo contains convergence information
type ConvergenceInfo struct {
	Converged       bool    `json:"converged"`
	ConvergenceRate float64 `json:"convergence_rate"`
	Plateau         bool    `json:"plateau"`
	PlateauLength   int     `json:"plateau_length"`
}

// DistributedTrainingConfig contains distributed training configuration
type DistributedTrainingConfig struct {
	Strategy        DistributedStrategy  `json:"strategy"`
	Nodes           []TrainingNode       `json:"nodes"`
	Communication   CommunicationConfig  `json:"communication"`
	Synchronization SyncConfig           `json:"synchronization"`
	FaultTolerance  FaultToleranceConfig `json:"fault_tolerance"`
}

// DistributedStrategy defines the distributed training strategy
type DistributedStrategy string

const (
	DistributedStrategyDataParallel     DistributedStrategy = "data_parallel"
	DistributedStrategyModelParallel    DistributedStrategy = "model_parallel"
	DistributedStrategyPipelineParallel DistributedStrategy = "pipeline_parallel"
	DistributedStrategyHybrid           DistributedStrategy = "hybrid"
)

// TrainingNode represents a training node
type TrainingNode struct {
	ID        string         `json:"id"`
	Address   string         `json:"address"`
	Resources ResourceConfig `json:"resources"`
	Role      string         `json:"role"` // worker, parameter_server, chief
}

// CommunicationConfig contains communication configuration
type CommunicationConfig struct {
	Backend     string        `json:"backend"` // nccl, gloo, mpi
	Compression bool          `json:"compression"`
	Timeout     time.Duration `json:"timeout"`
}

// SyncConfig contains synchronization configuration
type SyncConfig struct {
	Type      string `json:"type"` // sync, async, bounded_async
	Frequency int    `json:"frequency"`
	Staleness int    `json:"staleness"`
}

// FaultToleranceConfig contains fault tolerance configuration
type FaultToleranceConfig struct {
	Enabled           bool          `json:"enabled"`
	MaxFailures       int           `json:"max_failures"`
	RestartPolicy     string        `json:"restart_policy"`
	CheckpointFreq    int           `json:"checkpoint_frequency"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

// DistributedTrainingJob represents a distributed training job
type DistributedTrainingJob interface {
	training.TrainingJob
	GetNodes() []TrainingNode
	GetNodeStatus(nodeID string) (NodeStatus, error)
	GetCommunicationStats() CommunicationStats
}

// NodeStatus represents the status of a training node
type NodeStatus struct {
	NodeID        string           `json:"node_id"`
	Status        string           `json:"status"`
	Health        string           `json:"health"`
	LastHeartbeat time.Time        `json:"last_heartbeat"`
	Metrics       ResourceMetrics  `json:"metrics"`
	Progress      TrainingProgress `json:"progress"`
}

// CommunicationStats contains communication statistics
type CommunicationStats struct {
	TotalMessages    int64         `json:"total_messages"`
	TotalBytes       int64         `json:"total_bytes"`
	AverageLatency   time.Duration `json:"average_latency"`
	Bandwidth        float64       `json:"bandwidth"`
	CompressionRatio float64       `json:"compression_ratio"`
}

// DistributedTrainingStatus contains status of distributed training
type DistributedTrainingStatus struct {
	JobID         string                `json:"job_id"`
	Status        TrainingStatus        `json:"status"`
	Strategy      DistributedStrategy   `json:"strategy"`
	ActiveNodes   int                   `json:"active_nodes"`
	FailedNodes   int                   `json:"failed_nodes"`
	NodeStatuses  map[string]NodeStatus `json:"node_statuses"`
	Progress      TrainingProgress      `json:"progress"`
	Communication CommunicationStats    `json:"communication"`
}

// CustomTrainer defines interface for custom training logic
type CustomTrainer interface {
	Name() string
	Type() TrainerType
	Train(ctx context.Context, config TrainingConfig, dataset Dataset) (TrainingResult, error)
	GetSupportedModelTypes() []models.ModelType
	GetSchema() TrainerSchema
}

// TrainerType defines the type of trainer
type TrainerType string

const (
	TrainerTypeSupervised    TrainerType = "supervised"
	TrainerTypeUnsupervised  TrainerType = "unsupervised"
	TrainerTypeReinforcement TrainerType = "reinforcement"
	TrainerTypeCustom        TrainerType = "custom"
)

// TrainingResult contains the result of training
type TrainingResult struct {
	Model    TrainedModel      `json:"model"`
	Metrics  TrainingMetrics   `json:"metrics"`
	History  []TrainingMetrics `json:"history"`
	Duration time.Duration     `json:"duration"`
	Status   TrainingStatus    `json:"status"`
	Error    error             `json:"error,omitempty"`
}

// TrainerSchema defines the schema for a trainer
type TrainerSchema struct {
	ConfigSchema  map[string]interface{} `json:"config_schema"`
	DatasetSchema map[string]interface{} `json:"dataset_schema"`
	OutputSchema  map[string]interface{} `json:"output_schema"`
	Examples      []TrainerExample       `json:"examples"`
}

// TrainerExample contains example usage of a trainer
type TrainerExample struct {
	Description string         `json:"description"`
	Config      TrainingConfig `json:"config"`
	Dataset     DatasetConfig  `json:"dataset"`
	Expected    TrainingResult `json:"expected"`
}

// BaseTrainingPlugin provides a base implementation of TrainingPlugin
type BaseTrainingPlugin struct {
	id             string
	name           string
	version        string
	description    string
	author         string
	license        string
	modelTypes     []models.ModelType
	capabilities   []TrainingCapability
	customTrainers map[string]CustomTrainer
	trainedModels  map[string]TrainedModel
	trainingJobs   map[string]TrainingJob
	pipelines      map[string]training.TrainingPipeline
	logger         common.Logger
	mu             sync.RWMutex
}

// NewBaseTrainingPlugin creates a new base training plugin
func NewBaseTrainingPlugin(config BaseTrainingPluginConfig) *BaseTrainingPlugin {
	return &BaseTrainingPlugin{
		id:             config.ID,
		name:           config.Name,
		version:        config.Version,
		description:    config.Description,
		author:         config.Author,
		license:        config.License,
		modelTypes:     config.ModelTypes,
		capabilities:   config.Capabilities,
		customTrainers: make(map[string]CustomTrainer),
		trainedModels:  make(map[string]TrainedModel),
		trainingJobs:   make(map[string]TrainingJob),
		pipelines:      make(map[string]training.TrainingPipeline),
		logger:         config.Logger,
	}
}

// BaseTrainingPluginConfig contains configuration for base training plugin
type BaseTrainingPluginConfig struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Version      string               `json:"version"`
	Description  string               `json:"description"`
	Author       string               `json:"author"`
	License      string               `json:"license"`
	ModelTypes   []models.ModelType   `json:"model_types"`
	Capabilities []TrainingCapability `json:"capabilities"`
	Logger       common.Logger        `json:"-"`
}

// Basic Plugin interface implementation
func (p *BaseTrainingPlugin) ID() string          { return p.id }
func (p *BaseTrainingPlugin) Name() string        { return p.name }
func (p *BaseTrainingPlugin) Version() string     { return p.version }
func (p *BaseTrainingPlugin) Description() string { return p.description }
func (p *BaseTrainingPlugin) Author() string      { return p.author }
func (p *BaseTrainingPlugin) License() string     { return p.license }

func (p *BaseTrainingPlugin) Type() plugins.PluginEngineType {
	return plugins.PluginEngineTypeAI
}

func (p *BaseTrainingPlugin) Initialize(ctx context.Context, container common.Container) error {
	if p.logger != nil {
		p.logger.Info("initializing training plugin",
			logger.String("plugin_id", p.id),
			logger.String("plugin_name", p.name),
		)
	}
	return nil
}

func (p *BaseTrainingPlugin) OnStart(ctx context.Context) error {
	if p.logger != nil {
		p.logger.Info("starting training plugin",
			logger.String("plugin_id", p.id),
		)
	}
	return nil
}

func (p *BaseTrainingPlugin) OnStop(ctx context.Context) error {
	if p.logger != nil {
		p.logger.Info("stopping training plugin",
			logger.String("plugin_id", p.id),
		)
	}
	return nil
}

func (p *BaseTrainingPlugin) Cleanup(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop all training jobs
	for jobID, job := range p.trainingJobs {
		if err := job.Cancel(ctx); err != nil && p.logger != nil {
			p.logger.Warn("failed to cancel training job during cleanup",
				logger.String("job_id", jobID),
				logger.Error(err),
			)
		}
	}

	// Clear all data
	p.customTrainers = make(map[string]CustomTrainer)
	p.trainedModels = make(map[string]TrainedModel)
	p.trainingJobs = make(map[string]TrainingJob)
	p.pipelines = make(map[string]training.TrainingPipeline)

	return nil
}

func (p *BaseTrainingPlugin) GetSupportedModelTypes() []models.ModelType {
	return p.modelTypes
}

func (p *BaseTrainingPlugin) GetTrainingCapabilities() []TrainingCapability {
	return p.capabilities
}

func (p *BaseTrainingPlugin) GetSupportedDatasetTypes() []DatasetType {
	// Default implementation - can be overridden
	return []DatasetType{DatasetTypeText, DatasetTypeTabular}
}

func (p *BaseTrainingPlugin) ListTrainingJobs() []TrainingJob {
	p.mu.RLock()
	defer p.mu.RUnlock()

	jobs := make([]TrainingJob, 0, len(p.trainingJobs))
	for _, job := range p.trainingJobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (p *BaseTrainingPlugin) GetTrainingJob(jobID string) (TrainingJob, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	job, exists := p.trainingJobs[jobID]
	if !exists {
		return nil, fmt.Errorf("training job not found: %s", jobID)
	}

	return job, nil
}

func (p *BaseTrainingPlugin) GetCustomTrainers() []CustomTrainer {
	p.mu.RLock()
	defer p.mu.RUnlock()

	trainers := make([]CustomTrainer, 0, len(p.customTrainers))
	for _, trainer := range p.customTrainers {
		trainers = append(trainers, trainer)
	}
	return trainers
}

func (p *BaseTrainingPlugin) RegisterCustomTrainer(trainer CustomTrainer) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.customTrainers[trainer.Name()]; exists {
		return fmt.Errorf("trainer with name %s already exists", trainer.Name())
	}

	p.customTrainers[trainer.Name()] = trainer

	if p.logger != nil {
		p.logger.Info("registered custom trainer",
			logger.String("plugin_id", p.id),
			logger.String("trainer_name", trainer.Name()),
			logger.String("trainer_type", string(trainer.Type())),
		)
	}

	return nil
}

func (p *BaseTrainingPlugin) GetTrainingPipelines() []training.TrainingPipeline {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pipelines := make([]training.TrainingPipeline, 0, len(p.pipelines))
	for _, pipeline := range p.pipelines {
		pipelines = append(pipelines, pipeline)
	}
	return pipelines
}

// Default implementations that can be overridden by specific plugins
func (p *BaseTrainingPlugin) CreateTrainer(config training.TrainerConfig) (training.ModelTrainer, error) {
	return nil, fmt.Errorf("CreateTrainer not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) CreateTrainingPipeline(config training.PipelineConfig) (training.TrainingPipeline, error) {
	return nil, fmt.Errorf("CreateTrainingPipeline not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) DeleteTrainingPipeline(pipelineID string) error {
	return fmt.Errorf("DeleteTrainingPipeline not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) StartTraining(ctx context.Context, request TrainingRequest) (TrainingJob, error) {
	return nil, fmt.Errorf("StartTraining not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) StopTraining(ctx context.Context, jobID string) error {
	return fmt.Errorf("StopTraining not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) PrepareDataset(ctx context.Context, config DatasetConfig) (Dataset, error) {
	return nil, fmt.Errorf("PrepareDataset not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) ValidateDataset(ctx context.Context, dataset Dataset) error {
	return fmt.Errorf("ValidateDataset not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) SaveTrainedModel(ctx context.Context, jobID string, path string) (TrainedModel, error) {
	return nil, fmt.Errorf("SaveTrainedModel not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) LoadTrainedModel(ctx context.Context, path string) (TrainedModel, error) {
	return nil, fmt.Errorf("LoadTrainedModel not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) ExportModel(ctx context.Context, jobID string, format ExportFormat) ([]byte, error) {
	return nil, fmt.Errorf("ExportModel not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) GetTrainingMetrics(jobID string) (TrainingMetrics, error) {
	return TrainingMetrics{}, fmt.Errorf("GetTrainingMetrics not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) GetTrainingRecommendations(ctx context.Context, jobID string) ([]TrainingRecommendation, error) {
	return nil, fmt.Errorf("GetTrainingRecommendations not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) OptimizeHyperparameters(ctx context.Context, config HyperparameterOptimizationConfig) (OptimizationResult, error) {
	return OptimizationResult{}, fmt.Errorf("OptimizeHyperparameters not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) CreateDistributedTraining(ctx context.Context, config DistributedTrainingConfig) (DistributedTrainingJob, error) {
	return nil, fmt.Errorf("CreateDistributedTraining not implemented by plugin %s", p.id)
}

func (p *BaseTrainingPlugin) GetDistributedTrainingStatus(jobID string) (DistributedTrainingStatus, error) {
	return DistributedTrainingStatus{}, fmt.Errorf("GetDistributedTrainingStatus not implemented by plugin %s", p.id)
}

// Placeholder implementations for common.PluginEngine interface
func (p *BaseTrainingPlugin) Capabilities() []plugins.PluginEngineCapability {
	return []plugins.PluginEngineCapability{}
}
func (p *BaseTrainingPlugin) Dependencies() []plugins.PluginEngineDependency {
	return []plugins.PluginEngineDependency{}
}
func (p *BaseTrainingPlugin) Middleware() []common.MiddlewareDefinition {
	return []common.MiddlewareDefinition{}
}
func (p *BaseTrainingPlugin) Routes() []common.RouteDefinition { return []common.RouteDefinition{} }
func (p *BaseTrainingPlugin) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{}
}
func (p *BaseTrainingPlugin) Commands() []plugins.CLICommand        { return []plugins.CLICommand{} }
func (p *BaseTrainingPlugin) Hooks() []plugins.Hook                 { return []plugins.Hook{} }
func (p *BaseTrainingPlugin) ConfigSchema() plugins.ConfigSchema    { return plugins.ConfigSchema{} }
func (p *BaseTrainingPlugin) Configure(config interface{}) error    { return nil }
func (p *BaseTrainingPlugin) GetConfig() interface{}                { return nil }
func (p *BaseTrainingPlugin) HealthCheck(ctx context.Context) error { return nil }
func (p *BaseTrainingPlugin) GetMetrics() plugins.PluginEngineMetrics {
	return plugins.PluginEngineMetrics{}
}
