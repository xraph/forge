package training

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// TrainerConfig contains configuration for a trainer
type TrainerConfig struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	ModelType   string                 `json:"model_type"`
	Config      map[string]interface{} `json:"config"`
	Resources   ResourceConfig         `json:"resources"`
	Environment map[string]string      `json:"environment"`
	Tags        map[string]string      `json:"tags"`
	Enabled     bool                   `json:"enabled"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// ModelTrainer defines the interface for model training
type ModelTrainer interface {
	// Training lifecycle
	StartTraining(ctx context.Context, request TrainingRequest) (TrainingJob, error)
	StopTraining(ctx context.Context, jobID string) error
	ResumeTraining(ctx context.Context, jobID string) error
	GetTrainingJob(jobID string) (TrainingJob, error)
	ListTrainingJobs() []TrainingJob

	// Model management
	SaveModel(ctx context.Context, jobID string, path string) error
	LoadModel(ctx context.Context, path string) (TrainedModel, error)
	ExportModel(ctx context.Context, jobID string, format string) ([]byte, error)

	// Training data
	PrepareDataset(ctx context.Context, config DatasetConfig) (Dataset, error)
	ValidateDataset(ctx context.Context, dataset Dataset) error

	// Monitoring
	GetTrainingMetrics(jobID string) (TrainingMetrics, error)
	GetTrainingLogs(jobID string) ([]TrainingLogEntry, error)
	GetTrainingStatus(jobID string) (TrainingStatus, error)
}

// TrainingRequest represents a training request
type TrainingRequest struct {
	ID               string                 `json:"id"`
	ModelType        string                 `json:"model_type"`
	ModelConfig      ModelConfig            `json:"model_config"`
	TrainingConfig   TrainingConfig         `json:"training_config"`
	DatasetConfig    DatasetConfig          `json:"dataset_config"`
	HyperParameters  map[string]interface{} `json:"hyperparameters"`
	ValidationConfig ValidationConfig       `json:"validation_config"`
	Callbacks        []TrainingCallback     `json:"callbacks"`
	Resources        ResourceConfig         `json:"resources"`
	Tags             map[string]string      `json:"tags"`
	Priority         int                    `json:"priority"`
}

// TrainingJob represents a training job
type TrainingJob interface {
	ID() string
	Name() string
	Status() TrainingStatus
	Progress() TrainingProgress
	CreatedAt() time.Time
	StartedAt() time.Time
	CompletedAt() time.Time
	Duration() time.Duration
	GetMetrics() TrainingMetrics
	GetLogs() []TrainingLogEntry
	GetConfig() TrainingConfig
	Cancel(ctx context.Context) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
}

// TrainingStatus represents the status of a training job
type TrainingStatus string

const (
	TrainingStatusPending   TrainingStatus = "pending"
	TrainingStatusRunning   TrainingStatus = "running"
	TrainingStatusPaused    TrainingStatus = "paused"
	TrainingStatusCompleted TrainingStatus = "completed"
	TrainingStatusFailed    TrainingStatus = "failed"
	TrainingStatusCancelled TrainingStatus = "cancelled"
)

// TrainingProgress represents training progress information
type TrainingProgress struct {
	Epoch         int           `json:"epoch"`
	TotalEpochs   int           `json:"total_epochs"`
	Step          int64         `json:"step"`
	TotalSteps    int64         `json:"total_steps"`
	Percentage    float64       `json:"percentage"`
	EstimatedTime time.Duration `json:"estimated_time"`
	ElapsedTime   time.Duration `json:"elapsed_time"`
}

// ModelConfig contains model configuration
type ModelConfig struct {
	Architecture   string                 `json:"architecture"`
	Framework      string                 `json:"framework"`
	Version        string                 `json:"version"`
	Parameters     map[string]interface{} `json:"parameters"`
	PretrainedPath string                 `json:"pretrained_path,omitempty"`
}

// TrainingConfig contains training configuration
type TrainingConfig struct {
	Epochs         int                   `json:"epochs"`
	BatchSize      int                   `json:"batch_size"`
	LearningRate   float64               `json:"learning_rate"`
	Optimizer      string                `json:"optimizer"`
	LossFunction   string                `json:"loss_function"`
	Regularization RegularizationConfig  `json:"regularization"`
	EarlyStopping  EarlyStoppingConfig   `json:"early_stopping"`
	LRScheduler    LearningRateScheduler `json:"lr_scheduler"`
	Checkpoints    CheckpointConfig      `json:"checkpoints"`
	Environment    map[string]string     `json:"environment"`
}

// ValidationConfig contains validation configuration
type ValidationConfig struct {
	SplitRatio      float64               `json:"split_ratio"`
	ValidationSet   string                `json:"validation_set,omitempty"`
	Metrics         []string              `json:"metrics"`
	EvaluationFreq  int                   `json:"evaluation_frequency"`
	CrossValidation CrossValidationConfig `json:"cross_validation"`
}

// ResourceConfig contains resource allocation configuration
type ResourceConfig struct {
	CPU        string        `json:"cpu"`
	Memory     string        `json:"memory"`
	GPU        int           `json:"gpu"`
	Storage    string        `json:"storage"`
	NodeType   string        `json:"node_type,omitempty"`
	Priority   int           `json:"priority"`
	MaxRuntime time.Duration `json:"max_runtime"`
}

// TrainingCallback represents a training callback
type TrainingCallback struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Config   map[string]interface{} `json:"config"`
	Priority int                    `json:"priority"`
}

// RegularizationConfig contains regularization settings
type RegularizationConfig struct {
	L1          float64 `json:"l1"`
	L2          float64 `json:"l2"`
	Dropout     float64 `json:"dropout"`
	BatchNorm   bool    `json:"batch_norm"`
	WeightDecay float64 `json:"weight_decay"`
}

// EarlyStoppingConfig contains early stopping settings
type EarlyStoppingConfig struct {
	Enabled   bool    `json:"enabled"`
	Metric    string  `json:"metric"`
	Mode      string  `json:"mode"` // min, max
	Patience  int     `json:"patience"`
	Threshold float64 `json:"threshold"`
}

// LearningRateScheduler contains learning rate scheduler settings
type LearningRateScheduler struct {
	Type       string                 `json:"type"`
	Parameters map[string]interface{} `json:"parameters"`
}

// CheckpointConfig contains checkpoint settings
type CheckpointConfig struct {
	Enabled    bool   `json:"enabled"`
	Frequency  int    `json:"frequency"` // epochs
	Path       string `json:"path"`
	SaveBest   bool   `json:"save_best"`
	SaveLatest bool   `json:"save_latest"`
	MaxKeep    int    `json:"max_keep"`
}

// CrossValidationConfig contains cross-validation settings
type CrossValidationConfig struct {
	Enabled bool `json:"enabled"`
	Folds   int  `json:"folds"`
	Shuffle bool `json:"shuffle"`
	Seed    int  `json:"seed"`
}

// TrainedModel represents a trained model
type TrainedModel interface {
	ID() string
	Name() string
	Version() string
	Framework() string
	TrainingJob() string
	CreatedAt() time.Time
	Metrics() TrainingMetrics
	Save(ctx context.Context, path string) error
	Load(ctx context.Context, path string) error
	Export(ctx context.Context, format string) ([]byte, error)
	Validate(ctx context.Context, dataset Dataset) (ValidationResults, error)
}

// ValidationResults contains validation results
type ValidationResults struct {
	Accuracy  float64            `json:"accuracy"`
	Loss      float64            `json:"loss"`
	Metrics   map[string]float64 `json:"metrics"`
	Confusion [][]int            `json:"confusion_matrix,omitempty"`
	Report    string             `json:"classification_report,omitempty"`
}

// TrainingLogEntry represents a training log entry
type TrainingLogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Epoch     int                    `json:"epoch,omitempty"`
	Step      int64                  `json:"step,omitempty"`
	Metrics   map[string]interface{} `json:"metrics,omitempty"`
}

// ModelTrainerImpl implements the ModelTrainer interface
type ModelTrainerImpl struct {
	jobs     map[string]TrainingJob
	datasets map[string]Dataset
	models   map[string]TrainedModel
	executor TrainingExecutor
	monitor  TrainingMonitor
	storage  ModelStorage
	logger   common.Logger
	metrics  common.Metrics
	mu       sync.RWMutex
	started  bool
}

// NewModelTrainer creates a new model trainer
func NewModelTrainer(logger common.Logger, metrics common.Metrics) ModelTrainer {
	return &ModelTrainerImpl{
		jobs:     make(map[string]TrainingJob),
		datasets: make(map[string]Dataset),
		models:   make(map[string]TrainedModel),
		logger:   logger,
		metrics:  metrics,
	}
}

// StartTraining starts a new training job
func (t *ModelTrainerImpl) StartTraining(ctx context.Context, request TrainingRequest) (TrainingJob, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if job already exists
	if _, exists := t.jobs[request.ID]; exists {
		return nil, common.ErrServiceAlreadyExists(request.ID)
	}

	// Validate request
	if err := t.validateTrainingRequest(request); err != nil {
		return nil, fmt.Errorf("invalid training request: %w", err)
	}

	// Create training job
	job := NewTrainingJob(request, t.logger, t.metrics)

	// Store job
	t.jobs[request.ID] = job

	// Start training execution
	go func() {
		if err := t.executor.Execute(ctx, job); err != nil {
			t.logger.Error("training execution failed",
				logger.String("job_id", request.ID),
				logger.Error(err),
			)
		}
	}()

	t.logger.Info("training job started",
		logger.String("job_id", request.ID),
		logger.String("model_type", request.ModelType),
	)

	if t.metrics != nil {
		t.metrics.Counter("forge.ai.training.jobs_started").Inc()
	}

	return job, nil
}

// StopTraining stops a training job
func (t *ModelTrainerImpl) StopTraining(ctx context.Context, jobID string) error {
	t.mu.RLock()
	job, exists := t.jobs[jobID]
	t.mu.RUnlock()

	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	if err := job.Cancel(ctx); err != nil {
		return fmt.Errorf("failed to stop training job: %w", err)
	}

	t.logger.Info("training job stopped",
		logger.String("job_id", jobID),
	)

	if t.metrics != nil {
		t.metrics.Counter("forge.ai.training.jobs_stopped").Inc()
	}

	return nil
}

// ResumeTraining resumes a paused training job
func (t *ModelTrainerImpl) ResumeTraining(ctx context.Context, jobID string) error {
	t.mu.RLock()
	job, exists := t.jobs[jobID]
	t.mu.RUnlock()

	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	if err := job.Resume(ctx); err != nil {
		return fmt.Errorf("failed to resume training job: %w", err)
	}

	t.logger.Info("training job resumed",
		logger.String("job_id", jobID),
	)

	return nil
}

// GetTrainingJob returns a training job by ID
func (t *ModelTrainerImpl) GetTrainingJob(jobID string) (TrainingJob, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	job, exists := t.jobs[jobID]
	if !exists {
		return nil, common.ErrServiceNotFound(jobID)
	}

	return job, nil
}

// ListTrainingJobs returns all training jobs
func (t *ModelTrainerImpl) ListTrainingJobs() []TrainingJob {
	t.mu.RLock()
	defer t.mu.RUnlock()

	jobs := make([]TrainingJob, 0, len(t.jobs))
	for _, job := range t.jobs {
		jobs = append(jobs, job)
	}

	return jobs
}

// SaveModel saves a trained model
func (t *ModelTrainerImpl) SaveModel(ctx context.Context, jobID string, path string) error {
	job, err := t.GetTrainingJob(jobID)
	if err != nil {
		return err
	}

	if job.Status() != TrainingStatusCompleted {
		return fmt.Errorf("job %s is not completed", jobID)
	}

	// Save model using storage
	if err := t.storage.SaveModel(ctx, jobID, path); err != nil {
		return fmt.Errorf("failed to save model: %w", err)
	}

	t.logger.Info("model saved",
		logger.String("job_id", jobID),
		logger.String("path", path),
	)

	return nil
}

// LoadModel loads a trained model
func (t *ModelTrainerImpl) LoadModel(ctx context.Context, path string) (TrainedModel, error) {
	model, err := t.storage.LoadModel(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to load model: %w", err)
	}

	t.mu.Lock()
	t.models[model.ID()] = model
	t.mu.Unlock()

	t.logger.Info("model loaded",
		logger.String("model_id", model.ID()),
		logger.String("path", path),
	)

	return model, nil
}

// ExportModel exports a model in the specified format
func (t *ModelTrainerImpl) ExportModel(ctx context.Context, jobID string, format string) ([]byte, error) {
	job, err := t.GetTrainingJob(jobID)
	if err != nil {
		return nil, err
	}

	if job.Status() != TrainingStatusCompleted {
		return nil, fmt.Errorf("job %s is not completed", jobID)
	}

	// Export model using storage
	data, err := t.storage.ExportModel(ctx, jobID, format)
	if err != nil {
		return nil, fmt.Errorf("failed to export model: %w", err)
	}

	t.logger.Info("model exported",
		logger.String("job_id", jobID),
		logger.String("format", format),
		logger.Int("size", len(data)),
	)

	return data, nil
}

// PrepareDataset prepares a dataset for training
func (t *ModelTrainerImpl) PrepareDataset(ctx context.Context, config DatasetConfig) (Dataset, error) {
	dataset := NewDataset(config, t.logger)

	if err := dataset.Prepare(ctx); err != nil {
		return nil, fmt.Errorf("failed to prepare dataset: %w", err)
	}

	t.mu.Lock()
	t.datasets[dataset.ID()] = dataset
	t.mu.Unlock()

	t.logger.Info("dataset prepared",
		logger.String("dataset_id", dataset.ID()),
		logger.String("type", string(config.Type)),
	)

	return dataset, nil
}

// ValidateDataset validates a dataset
func (t *ModelTrainerImpl) ValidateDataset(ctx context.Context, dataset Dataset) error {
	if err := dataset.Validate(ctx); err != nil {
		return fmt.Errorf("dataset validation failed: %w", err)
	}

	t.logger.Info("dataset validated",
		logger.String("dataset_id", dataset.ID()),
	)

	return nil
}

// GetTrainingMetrics returns training metrics for a job
func (t *ModelTrainerImpl) GetTrainingMetrics(jobID string) (TrainingMetrics, error) {
	job, err := t.GetTrainingJob(jobID)
	if err != nil {
		return TrainingMetrics{}, err
	}

	return job.GetMetrics(), nil
}

// GetTrainingLogs returns training logs for a job
func (t *ModelTrainerImpl) GetTrainingLogs(jobID string) ([]TrainingLogEntry, error) {
	job, err := t.GetTrainingJob(jobID)
	if err != nil {
		return nil, err
	}

	return job.GetLogs(), nil
}

// GetTrainingStatus returns training status for a job
func (t *ModelTrainerImpl) GetTrainingStatus(jobID string) (TrainingStatus, error) {
	job, err := t.GetTrainingJob(jobID)
	if err != nil {
		return "", err
	}

	return job.Status(), nil
}

// Helper methods

func (t *ModelTrainerImpl) validateTrainingRequest(request TrainingRequest) error {
	if request.ID == "" {
		return fmt.Errorf("training request ID is required")
	}

	if request.ModelType == "" {
		return fmt.Errorf("model type is required")
	}

	if request.TrainingConfig.Epochs <= 0 {
		return fmt.Errorf("epochs must be greater than 0")
	}

	if request.TrainingConfig.BatchSize <= 0 {
		return fmt.Errorf("batch size must be greater than 0")
	}

	if request.TrainingConfig.LearningRate <= 0 {
		return fmt.Errorf("learning rate must be greater than 0")
	}

	return nil
}

// TrainingExecutor defines the interface for training execution
type TrainingExecutor interface {
	Execute(ctx context.Context, job TrainingJob) error
	Pause(ctx context.Context, jobID string) error
	Resume(ctx context.Context, jobID string) error
	Cancel(ctx context.Context, jobID string) error
}

// TrainingMonitor defines the interface for training monitoring
type TrainingMonitor interface {
	StartMonitoring(ctx context.Context, jobID string) error
	StopMonitoring(ctx context.Context, jobID string) error
	GetMetrics(jobID string) (TrainingMetrics, error)
	GetLogs(jobID string) ([]TrainingLogEntry, error)
}

// ModelStorage defines the interface for model storage
type ModelStorage interface {
	SaveModel(ctx context.Context, jobID string, path string) error
	LoadModel(ctx context.Context, path string) (TrainedModel, error)
	ExportModel(ctx context.Context, jobID string, format string) ([]byte, error)
	DeleteModel(ctx context.Context, modelID string) error
	ListModels(ctx context.Context) ([]TrainedModel, error)
}

// NewTrainingJob creates a new training job
func NewTrainingJob(request TrainingRequest, logger common.Logger, metrics common.Metrics) TrainingJob {
	return &TrainingJobImpl{
		request:   request,
		status:    TrainingStatusPending,
		createdAt: time.Now(),
		logger:    logger,
		metrics:   TrainingMetrics{}, // Initialize empty metrics
	}
}

// TrainingJobImpl implements the TrainingJob interface
type TrainingJobImpl struct {
	request          TrainingRequest
	status           TrainingStatus
	progress         TrainingProgress
	metrics          TrainingMetrics
	logs             []TrainingLogEntry
	createdAt        time.Time
	startedAt        time.Time
	completedAt      time.Time
	logger           common.Logger
	metricsCollector common.Metrics
	mu               sync.RWMutex
}

func (j *TrainingJobImpl) ID() string {
	return j.request.ID
}

func (j *TrainingJobImpl) Name() string {
	return j.request.ModelType
}

func (j *TrainingJobImpl) Status() TrainingStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.status
}

func (j *TrainingJobImpl) Progress() TrainingProgress {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.progress
}

func (j *TrainingJobImpl) CreatedAt() time.Time {
	return j.createdAt
}

func (j *TrainingJobImpl) StartedAt() time.Time {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.startedAt
}

func (j *TrainingJobImpl) CompletedAt() time.Time {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.completedAt
}

func (j *TrainingJobImpl) Duration() time.Duration {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.startedAt.IsZero() {
		return 0
	}
	if j.completedAt.IsZero() {
		return time.Since(j.startedAt)
	}
	return j.completedAt.Sub(j.startedAt)
}

func (j *TrainingJobImpl) GetMetrics() TrainingMetrics {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.metrics
}

func (j *TrainingJobImpl) GetLogs() []TrainingLogEntry {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.logs
}

func (j *TrainingJobImpl) GetConfig() TrainingConfig {
	return j.request.TrainingConfig
}

func (j *TrainingJobImpl) Cancel(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = TrainingStatusCancelled
	j.completedAt = time.Now()
	return nil
}

func (j *TrainingJobImpl) Pause(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = TrainingStatusPaused
	return nil
}

func (j *TrainingJobImpl) Resume(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = TrainingStatusRunning
	return nil
}
