package training

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
)

// TrainingPipeline defines the interface for training pipelines.
type TrainingPipeline interface {
	// Start begins the training pipeline execution using the provided context. Returns an error if the start operation fails.
	Start(ctx context.Context) error

	// Stop halts the execution of the training pipeline and transitions its status to a stopped or inactive state.
	Stop(ctx context.Context) error

	// Pause pauses the execution of the training pipeline and transitions its status to a paused state.
	Pause(ctx context.Context) error

	// Resume resumes a paused training pipeline and transitions its state back to active, enabling its execution to continue.
	Resume(ctx context.Context) error

	// ID returns the unique identifier of the pipeline.
	ID() string

	// Name returns the name of the training pipeline.
	Name() string

	// Status returns the current status of the pipeline as a PipelineStatus.
	Status() PipelineStatus

	// GetConfig retrieves the current configuration of the training pipeline as a PipelineConfig struct.
	GetConfig() PipelineConfig

	// UpdateConfig updates the pipeline configuration with the provided PipelineConfig object and returns an error if it fails.
	UpdateConfig(config PipelineConfig) error

	// AddStage adds a new stage to the training pipeline. Returns an error if the operation fails.
	AddStage(stage PipelineStage) error

	// RemoveStage removes a stage from the training pipeline by its ID and returns an error if the operation fails.
	RemoveStage(stageID string) error

	// GetStage retrieves a specific pipeline stage by its unique stage ID. Returns the stage and an error if not found.
	GetStage(stageID string) (PipelineStage, error)

	// GetStages returns all stages defined in the pipeline as a slice of PipelineStage objects.
	GetStages() []PipelineStage

	// Execute processes the given pipeline input and returns the output along with an error if execution fails.
	Execute(ctx context.Context, input PipelineInput) (PipelineOutput, error)

	// GetExecution retrieves a pipeline execution by its unique execution ID. Returns the execution instance or an error.
	GetExecution(executionID string) (PipelineExecution, error)

	// ListExecutions retrieves all pipeline executions for the current pipeline. Returns a slice of PipelineExecution objects.
	ListExecutions() []PipelineExecution

	// GetMetrics retrieves the performance metrics of the training pipeline, including execution counts and success rate.
	GetMetrics() PipelineMetrics

	// GetLogs retrieves a list of log entries associated with the pipeline, including details such as timestamp, level, and message.
	GetLogs() []PipelineLogEntry

	// GetHealth retrieves the current health status of the training pipeline, including status, details, and timestamps.
	GetHealth() PipelineHealth
}

// PipelineManager manages training pipelines.
type PipelineManager interface {
	// CreatePipeline initializes a new training pipeline based on the provided configuration and returns it or an error.
	CreatePipeline(config PipelineConfig) (TrainingPipeline, error)

	// DeletePipeline removes a pipeline identified by the given pipelineID. It returns an error if the operation fails.
	DeletePipeline(pipelineID string) error

	// GetPipeline retrieves the details of a specific training pipeline by its ID. Returns the pipeline or an error if not found.
	GetPipeline(pipelineID string) (TrainingPipeline, error)

	// ListPipelines retrieves a list of all available training pipelines managed by the system.
	ListPipelines() []TrainingPipeline

	// ExecutePipeline starts the execution of a specified pipeline using the provided input and returns the execution details.
	ExecutePipeline(ctx context.Context, pipelineID string, input PipelineInput) (PipelineExecution, error)

	// GetExecution retrieves the details of a specific pipeline execution by its unique execution ID.
	GetExecution(executionID string) (PipelineExecution, error)

	// CancelExecution cancels an ongoing pipeline execution identified by the provided executionID.
	CancelExecution(executionID string) error

	// CreateTemplate creates a new pipeline template based on the provided PipelineTemplate configuration. Returns an error if creation fails.
	CreateTemplate(template PipelineTemplate) error

	// GetTemplate retrieves a pipeline template by its unique templateID and returns the PipelineTemplate or an error if not found.
	GetTemplate(templateID string) (PipelineTemplate, error)

	// ListTemplates retrieves all available pipeline templates from the system and returns them as a slice of PipelineTemplate.
	ListTemplates() []PipelineTemplate

	// CreateFromTemplate creates a new TrainingPipeline based on the specified templateID and configuration parameters.
	CreateFromTemplate(templateID string, config map[string]any) (TrainingPipeline, error)
}

// PipelineConfig contains pipeline configuration.
type PipelineConfig struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Tags        map[string]string `json:"tags"`
	Schedule    ScheduleConfig    `json:"schedule"`
	Stages      []StageConfig     `json:"stages"`
	Parameters  map[string]any    `json:"parameters"`
	Resources   ResourceConfig    `json:"resources"`
	Timeouts    TimeoutConfig     `json:"timeouts"`
	Retry       RetryConfig       `json:"retry"`
	Triggers    []TriggerConfig   `json:"triggers"`
	Outputs     []OutputConfig    `json:"outputs"`
}

// PipelineStatus represents pipeline status.
type PipelineStatus string

const (
	PipelineStatusCreated   PipelineStatus = "created"
	PipelineStatusRunning   PipelineStatus = "running"
	PipelineStatusPaused    PipelineStatus = "paused"
	PipelineStatusCompleted PipelineStatus = "completed"
	PipelineStatusFailed    PipelineStatus = "failed"
	PipelineStatusCancelled PipelineStatus = "cancelled"
)

// PipelineStage represents a stage in the pipeline.
type PipelineStage interface {
	ID() string
	Name() string
	Type() StageType
	Dependencies() []string
	Execute(ctx context.Context, input StageInput) (StageOutput, error)
	Validate(input StageInput) error
	GetConfig() StageConfig
	GetMetrics() StageMetrics
}

// StageType defines the type of pipeline stage.
type StageType string

const (
	StageTypeDataPreparation   StageType = "data_preparation"
	StageTypeDataValidation    StageType = "data_validation"
	StageTypeFeatureExtraction StageType = "feature_extraction"
	StageTypeModelTraining     StageType = "model_training"
	StageTypeModelValidation   StageType = "model_validation"
	StageTypeModelEvaluation   StageType = "model_evaluation"
	StageTypeModelDeployment   StageType = "model_deployment"
	StageTypeCustom            StageType = "custom"
)

// StageConfig contains stage configuration.
type StageConfig struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         StageType         `json:"type"`
	Dependencies []string          `json:"dependencies"`
	Parameters   map[string]any    `json:"parameters"`
	Resources    ResourceConfig    `json:"resources"`
	Timeout      time.Duration     `json:"timeout"`
	Retry        RetryConfig       `json:"retry"`
	Conditional  ConditionalConfig `json:"conditional"`
	Outputs      []string          `json:"outputs"`
}

// PipelineInput represents input to a pipeline.
type PipelineInput struct {
	Data       any            `json:"data"`
	Parameters map[string]any `json:"parameters"`
	Context    map[string]any `json:"context"`
	Metadata   map[string]any `json:"metadata"`
}

// PipelineOutput represents output from a pipeline.
type PipelineOutput struct {
	Data      any                `json:"data"`
	Artifacts map[string]any     `json:"artifacts"`
	Metrics   map[string]float64 `json:"metrics"`
	Metadata  map[string]any     `json:"metadata"`
	Status    PipelineStatus     `json:"status"`
	Error     error              `json:"error,omitempty"`
}

// PipelineExecution represents a pipeline execution.
type PipelineExecution interface {
	ID() string
	PipelineID() string
	Status() PipelineStatus
	Progress() ExecutionProgress
	StartedAt() time.Time
	CompletedAt() time.Time
	Duration() time.Duration
	Input() PipelineInput
	Output() PipelineOutput
	GetStageExecution(stageID string) (StageExecution, error)
	GetStageExecutions() []StageExecution
	Cancel() error
}

// ExecutionProgress represents execution progress.
type ExecutionProgress struct {
	Stage         string        `json:"stage"`
	StageIndex    int           `json:"stage_index"`
	TotalStages   int           `json:"total_stages"`
	Percentage    float64       `json:"percentage"`
	EstimatedTime time.Duration `json:"estimated_time"`
	ElapsedTime   time.Duration `json:"elapsed_time"`
}

// StageInput represents input to a stage.
type StageInput struct {
	Data       any            `json:"data"`
	Parameters map[string]any `json:"parameters"`
	Artifacts  map[string]any `json:"artifacts"`
	Context    map[string]any `json:"context"`
}

// StageOutput represents output from a stage.
type StageOutput struct {
	Data      any                `json:"data"`
	Artifacts map[string]any     `json:"artifacts"`
	Metrics   map[string]float64 `json:"metrics"`
	Status    StageStatus        `json:"status"`
	Error     error              `json:"error,omitempty"`
}

// StageStatus represents stage execution status.
type StageStatus string

const (
	StageStatusPending   StageStatus = "pending"
	StageStatusRunning   StageStatus = "running"
	StageStatusCompleted StageStatus = "completed"
	StageStatusFailed    StageStatus = "failed"
	StageStatusSkipped   StageStatus = "skipped"
)

// StageExecution represents a stage execution.
type StageExecution interface {
	ID() string
	StageID() string
	Status() StageStatus
	StartedAt() time.Time
	CompletedAt() time.Time
	Duration() time.Duration
	Input() StageInput
	Output() StageOutput
	GetLogs() []StageLogEntry
	GetMetrics() StageMetrics
}

// ScheduleConfig contains pipeline scheduling configuration.
type ScheduleConfig struct {
	Enabled   bool            `json:"enabled"`
	Cron      string          `json:"cron,omitempty"`
	Interval  time.Duration   `json:"interval,omitempty"`
	StartTime *time.Time      `json:"start_time,omitempty"`
	EndTime   *time.Time      `json:"end_time,omitempty"`
	Timezone  string          `json:"timezone"`
	Triggers  []TriggerConfig `json:"triggers"`
}

// TriggerConfig contains trigger configuration.
type TriggerConfig struct {
	Type       string         `json:"type"` // cron, event, webhook, manual
	Parameters map[string]any `json:"parameters"`
	Condition  string         `json:"condition,omitempty"`
}

// OutputConfig contains output configuration.
type OutputConfig struct {
	Type        string         `json:"type"` // file, database, api, notification
	Destination string         `json:"destination"`
	Format      string         `json:"format"`
	Parameters  map[string]any `json:"parameters"`
}

// TimeoutConfig contains timeout configuration.
type TimeoutConfig struct {
	Pipeline time.Duration            `json:"pipeline"`
	Stages   map[string]time.Duration `json:"stages"`
}

// RetryConfig contains retry configuration.
type RetryConfig struct {
	Enabled     bool          `json:"enabled"`
	MaxAttempts int           `json:"max_attempts"`
	BackoffType string        `json:"backoff_type"` // linear, exponential, fixed
	Delay       time.Duration `json:"delay"`
	MaxDelay    time.Duration `json:"max_delay"`
}

// ConditionalConfig contains conditional execution configuration.
type ConditionalConfig struct {
	Condition string         `json:"condition"`
	OnTrue    map[string]any `json:"on_true,omitempty"`
	OnFalse   map[string]any `json:"on_false,omitempty"`
}

// PipelineTemplate represents a pipeline template.
type PipelineTemplate struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Version     string               `json:"version"`
	Category    string               `json:"category"`
	Tags        []string             `json:"tags"`
	Config      PipelineConfig       `json:"config"`
	Parameters  map[string]Parameter `json:"parameters"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

// Parameter represents a template parameter.
type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     any    `json:"default"`
	Required    bool   `json:"required"`
	Validation  string `json:"validation,omitempty"`
}

// PipelineMetrics contains pipeline metrics.
type PipelineMetrics struct {
	TotalExecutions int64                   `json:"total_executions"`
	SuccessfulRuns  int64                   `json:"successful_runs"`
	FailedRuns      int64                   `json:"failed_runs"`
	AverageDuration time.Duration           `json:"average_duration"`
	LastExecution   time.Time               `json:"last_execution"`
	LastSuccess     time.Time               `json:"last_success"`
	SuccessRate     float64                 `json:"success_rate"`
	StageMetrics    map[string]StageMetrics `json:"stage_metrics"`
}

// StageMetrics contains stage-specific metrics.
type StageMetrics struct {
	ExecutionCount  int64         `json:"execution_count"`
	SuccessCount    int64         `json:"success_count"`
	FailureCount    int64         `json:"failure_count"`
	AverageDuration time.Duration `json:"average_duration"`
	MinDuration     time.Duration `json:"min_duration"`
	MaxDuration     time.Duration `json:"max_duration"`
	SuccessRate     float64       `json:"success_rate"`
}

// PipelineHealth represents pipeline health status.
type PipelineHealth struct {
	Status      HealthStatus   `json:"status"`
	Message     string         `json:"message"`
	Details     map[string]any `json:"details"`
	CheckedAt   time.Time      `json:"checked_at"`
	LastHealthy time.Time      `json:"last_healthy"`
}

// HealthStatus represents health status.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// PipelineLogEntry represents a pipeline log entry.
type PipelineLogEntry struct {
	Timestamp   time.Time      `json:"timestamp"`
	Level       string         `json:"level"`
	Stage       string         `json:"stage,omitempty"`
	Message     string         `json:"message"`
	ExecutionID string         `json:"execution_id"`
	Metadata    map[string]any `json:"metadata"`
}

// StageLogEntry represents a stage log entry.
type StageLogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	StageID   string         `json:"stage_id"`
	Metadata  map[string]any `json:"metadata"`
}

// TrainingPipelineImpl implements the TrainingPipeline interface.
type TrainingPipelineImpl struct {
	config     PipelineConfig
	stages     map[string]PipelineStage
	executions map[string]PipelineExecution
	status     PipelineStatus
	metrics    PipelineMetrics
	health     PipelineHealth
	logger     logger.Logger
	mu         sync.RWMutex
	createdAt  time.Time
}

// NewTrainingPipeline creates a new training pipeline.
func NewTrainingPipeline(config PipelineConfig, logger logger.Logger) TrainingPipeline {
	return &TrainingPipelineImpl{
		config:     config,
		stages:     make(map[string]PipelineStage),
		executions: make(map[string]PipelineExecution),
		status:     PipelineStatusCreated,
		logger:     logger,
		createdAt:  time.Now(),
		health: PipelineHealth{
			Status:    HealthStatusUnknown,
			CheckedAt: time.Now(),
		},
	}
}

// ID returns the pipeline ID.
func (p *TrainingPipelineImpl) ID() string {
	return p.config.ID
}

// Name returns the pipeline name.
func (p *TrainingPipelineImpl) Name() string {
	return p.config.Name
}

// Status returns the pipeline status.
func (p *TrainingPipelineImpl) Status() PipelineStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.status
}

// GetConfig returns the pipeline configuration.
func (p *TrainingPipelineImpl) GetConfig() PipelineConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.config
}

// UpdateConfig updates the pipeline configuration.
func (p *TrainingPipelineImpl) UpdateConfig(config PipelineConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == PipelineStatusRunning {
		return errors.New("cannot update configuration while pipeline is running")
	}

	p.config = config

	return nil
}

// Start starts the pipeline.
func (p *TrainingPipelineImpl) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == PipelineStatusRunning {
		return errors.New("pipeline is already running")
	}

	p.status = PipelineStatusRunning

	p.logger.Info("training pipeline started",
		logger.String("pipeline_id", p.config.ID),
		logger.String("pipeline_name", p.config.Name),
	)

	return nil
}

// Stop stops the pipeline.
func (p *TrainingPipelineImpl) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status != PipelineStatusRunning {
		return errors.New("pipeline is not running")
	}

	p.status = PipelineStatusCompleted

	p.logger.Info("training pipeline stopped",
		logger.String("pipeline_id", p.config.ID),
	)

	return nil
}

// Pause pauses the pipeline.
func (p *TrainingPipelineImpl) Pause(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status != PipelineStatusRunning {
		return errors.New("pipeline is not running")
	}

	p.status = PipelineStatusPaused

	p.logger.Info("training pipeline paused",
		logger.String("pipeline_id", p.config.ID),
	)

	return nil
}

// Resume resumes the pipeline.
func (p *TrainingPipelineImpl) Resume(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status != PipelineStatusPaused {
		return errors.New("pipeline is not paused")
	}

	p.status = PipelineStatusRunning

	p.logger.Info("training pipeline resumed",
		logger.String("pipeline_id", p.config.ID),
	)

	return nil
}

// AddStage adds a stage to the pipeline.
func (p *TrainingPipelineImpl) AddStage(stage PipelineStage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == PipelineStatusRunning {
		return errors.New("cannot add stage while pipeline is running")
	}

	p.stages[stage.ID()] = stage

	return nil
}

// RemoveStage removes a stage from the pipeline.
func (p *TrainingPipelineImpl) RemoveStage(stageID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == PipelineStatusRunning {
		return errors.New("cannot remove stage while pipeline is running")
	}

	delete(p.stages, stageID)

	return nil
}

// GetStage returns a stage by ID.
func (p *TrainingPipelineImpl) GetStage(stageID string) (PipelineStage, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stage, exists := p.stages[stageID]
	if !exists {
		return nil, errors.ErrServiceNotFound(stageID)
	}

	return stage, nil
}

// GetStages returns all stages.
func (p *TrainingPipelineImpl) GetStages() []PipelineStage {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stages := make([]PipelineStage, 0, len(p.stages))
	for _, stage := range p.stages {
		stages = append(stages, stage)
	}

	return stages
}

// Execute executes the pipeline with given input.
func (p *TrainingPipelineImpl) Execute(ctx context.Context, input PipelineInput) (PipelineOutput, error) {
	execution := NewPipelineExecution(p.config.ID, input, p.logger)

	p.mu.Lock()
	p.executions[execution.ID()] = execution
	p.mu.Unlock()

	// Execute stages in order
	output := PipelineOutput{
		Artifacts: make(map[string]any),
		Metrics:   make(map[string]float64),
		Metadata:  make(map[string]any),
	}

	for _, stageConfig := range p.config.Stages {
		stage, exists := p.stages[stageConfig.ID]
		if !exists {
			output.Status = PipelineStatusFailed
			output.Error = fmt.Errorf("stage not found: %s", stageConfig.ID)

			return output, output.Error
		}

		stageInput := StageInput{
			Data:       input.Data,
			Parameters: input.Parameters,
			Artifacts:  output.Artifacts,
			Context:    input.Context,
		}

		stageOutput, err := stage.Execute(ctx, stageInput)
		if err != nil {
			output.Status = PipelineStatusFailed
			output.Error = err

			return output, err
		}

		// Merge stage output into pipeline output
		maps.Copy(output.Artifacts, stageOutput.Artifacts)

		maps.Copy(output.Metrics, stageOutput.Metrics)

		// Update data for next stage
		input.Data = stageOutput.Data
	}

	output.Status = PipelineStatusCompleted
	output.Data = input.Data

	p.logger.Info("pipeline execution completed",
		logger.String("pipeline_id", p.config.ID),
		logger.String("execution_id", execution.ID()),
	)

	return output, nil
}

// GetExecution returns an execution by ID.
func (p *TrainingPipelineImpl) GetExecution(executionID string) (PipelineExecution, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	execution, exists := p.executions[executionID]
	if !exists {
		return nil, errors.ErrServiceNotFound(executionID)
	}

	return execution, nil
}

// ListExecutions returns all executions.
func (p *TrainingPipelineImpl) ListExecutions() []PipelineExecution {
	p.mu.RLock()
	defer p.mu.RUnlock()

	executions := make([]PipelineExecution, 0, len(p.executions))
	for _, execution := range p.executions {
		executions = append(executions, execution)
	}

	return executions
}

// GetMetrics returns pipeline metrics.
func (p *TrainingPipelineImpl) GetMetrics() PipelineMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Calculate metrics based on executions
	totalExecutions := int64(len(p.executions))
	successfulRuns := int64(0)
	failedRuns := int64(0)
	totalDuration := time.Duration(0)

	var lastExecution, lastSuccess time.Time

	for _, execution := range p.executions {
		if execution.Status() == PipelineStatusCompleted {
			successfulRuns++

			if execution.CompletedAt().After(lastSuccess) {
				lastSuccess = execution.CompletedAt()
			}
		} else if execution.Status() == PipelineStatusFailed {
			failedRuns++
		}

		if execution.StartedAt().After(lastExecution) {
			lastExecution = execution.StartedAt()
		}

		totalDuration += execution.Duration()
	}

	var (
		averageDuration time.Duration
		successRate     float64
	)

	if totalExecutions > 0 {
		averageDuration = totalDuration / time.Duration(totalExecutions)
		successRate = float64(successfulRuns) / float64(totalExecutions)
	}

	return PipelineMetrics{
		TotalExecutions: totalExecutions,
		SuccessfulRuns:  successfulRuns,
		FailedRuns:      failedRuns,
		AverageDuration: averageDuration,
		LastExecution:   lastExecution,
		LastSuccess:     lastSuccess,
		SuccessRate:     successRate,
		StageMetrics:    make(map[string]StageMetrics),
	}
}

// GetLogs returns pipeline logs.
func (p *TrainingPipelineImpl) GetLogs() []PipelineLogEntry {
	// Implementation would collect logs from all executions
	return []PipelineLogEntry{}
}

// GetHealth returns pipeline health.
func (p *TrainingPipelineImpl) GetHealth() PipelineHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Update health based on recent executions
	metrics := p.GetMetrics()

	if metrics.SuccessRate > 0.9 {
		p.health.Status = HealthStatusHealthy
		p.health.Message = "Pipeline operating normally"
	} else if metrics.SuccessRate > 0.7 {
		p.health.Status = HealthStatusDegraded
		p.health.Message = "Pipeline has elevated failure rate"
	} else {
		p.health.Status = HealthStatusUnhealthy
		p.health.Message = "Pipeline has high failure rate"
	}

	p.health.CheckedAt = time.Now()
	p.health.Details = map[string]any{
		"total_executions": metrics.TotalExecutions,
		"success_rate":     metrics.SuccessRate,
		"last_execution":   metrics.LastExecution,
	}

	return p.health
}

// Helper function to create pipeline execution.
func NewPipelineExecution(pipelineID string, input PipelineInput, logger logger.Logger) PipelineExecution {
	return &PipelineExecutionImpl{
		id:         generateExecutionID(),
		pipelineID: pipelineID,
		status:     PipelineStatusRunning,
		input:      input,
		startedAt:  time.Now(),
		logger:     logger,
	}
}

// PipelineExecutionImpl implements PipelineExecution.
type PipelineExecutionImpl struct {
	id          string
	pipelineID  string
	status      PipelineStatus
	progress    ExecutionProgress
	startedAt   time.Time
	completedAt time.Time
	input       PipelineInput
	output      PipelineOutput
	logger      logger.Logger
	mu          sync.RWMutex
}

func (e *PipelineExecutionImpl) ID() string         { return e.id }
func (e *PipelineExecutionImpl) PipelineID() string { return e.pipelineID }
func (e *PipelineExecutionImpl) Status() PipelineStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.status
}
func (e *PipelineExecutionImpl) Progress() ExecutionProgress { return e.progress }
func (e *PipelineExecutionImpl) StartedAt() time.Time        { return e.startedAt }
func (e *PipelineExecutionImpl) CompletedAt() time.Time      { return e.completedAt }
func (e *PipelineExecutionImpl) Duration() time.Duration {
	if e.completedAt.IsZero() {
		return time.Since(e.startedAt)
	}

	return e.completedAt.Sub(e.startedAt)
}
func (e *PipelineExecutionImpl) Input() PipelineInput   { return e.input }
func (e *PipelineExecutionImpl) Output() PipelineOutput { return e.output }
func (e *PipelineExecutionImpl) GetStageExecution(stageID string) (StageExecution, error) {
	return nil, errors.ErrServiceNotFound(stageID)
}
func (e *PipelineExecutionImpl) GetStageExecutions() []StageExecution { return []StageExecution{} }
func (e *PipelineExecutionImpl) Cancel() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.status = PipelineStatusCancelled
	e.completedAt = time.Now()

	return nil
}

func generateExecutionID() string {
	return fmt.Sprintf("exec_%d", time.Now().UnixNano())
}

// PipelineManagerImpl implements the PipelineManager interface.
type PipelineManagerImpl struct {
	pipelines  map[string]TrainingPipeline
	executions map[string]PipelineExecution
	templates  map[string]PipelineTemplate
	logger     logger.Logger
	mu         sync.RWMutex
}

// NewPipelineManager creates a new pipeline manager instance.
func NewPipelineManager(logger logger.Logger) PipelineManager {
	return &PipelineManagerImpl{
		pipelines:  make(map[string]TrainingPipeline),
		executions: make(map[string]PipelineExecution),
		templates:  make(map[string]PipelineTemplate),
		logger:     logger,
	}
}

// CreatePipeline creates a new training pipeline.
func (pm *PipelineManagerImpl) CreatePipeline(config PipelineConfig) (TrainingPipeline, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.pipelines[config.ID]; exists {
		return nil, errors.ErrServiceAlreadyExists(config.ID)
	}

	pipeline := NewTrainingPipeline(config, pm.logger)
	pm.pipelines[config.ID] = pipeline

	pm.logger.Info("training pipeline created",
		logger.String("pipeline_id", config.ID),
		logger.String("pipeline_name", config.Name),
	)

	return pipeline, nil
}

// DeletePipeline deletes a training pipeline.
func (pm *PipelineManagerImpl) DeletePipeline(pipelineID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pipeline, exists := pm.pipelines[pipelineID]
	if !exists {
		return errors.ErrServiceNotFound(pipelineID)
	}

	// Check if pipeline is running
	if pipeline.Status() == PipelineStatusRunning {
		return errors.New("cannot delete running pipeline")
	}

	delete(pm.pipelines, pipelineID)

	pm.logger.Info("training pipeline deleted",
		logger.String("pipeline_id", pipelineID),
	)

	return nil
}

// GetPipeline retrieves a pipeline by ID.
func (pm *PipelineManagerImpl) GetPipeline(pipelineID string) (TrainingPipeline, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pipeline, exists := pm.pipelines[pipelineID]
	if !exists {
		return nil, errors.ErrServiceNotFound(pipelineID)
	}

	return pipeline, nil
}

// ListPipelines returns all training pipelines.
func (pm *PipelineManagerImpl) ListPipelines() []TrainingPipeline {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pipelines := make([]TrainingPipeline, 0, len(pm.pipelines))
	for _, pipeline := range pm.pipelines {
		pipelines = append(pipelines, pipeline)
	}

	return pipelines
}

// ExecutePipeline executes a pipeline with given input.
func (pm *PipelineManagerImpl) ExecutePipeline(ctx context.Context, pipelineID string, input PipelineInput) (PipelineExecution, error) {
	pipeline, err := pm.GetPipeline(pipelineID)
	if err != nil {
		return nil, err
	}

	// Create execution
	execution := NewPipelineExecution(pipelineID, input, pm.logger)

	pm.mu.Lock()
	pm.executions[execution.ID()] = execution
	pm.mu.Unlock()

	// Execute pipeline in background
	go func() {
		_, err := pipeline.Execute(ctx, input)
		if err != nil {
			pm.logger.Error("pipeline execution failed",
				logger.String("pipeline_id", pipelineID),
				logger.String("execution_id", execution.ID()),
				logger.Error(err),
			)
		}
	}()

	pm.logger.Info("pipeline execution started",
		logger.String("pipeline_id", pipelineID),
		logger.String("execution_id", execution.ID()),
	)

	return execution, nil
}

// GetExecution retrieves an execution by ID.
func (pm *PipelineManagerImpl) GetExecution(executionID string) (PipelineExecution, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	execution, exists := pm.executions[executionID]
	if !exists {
		return nil, errors.ErrServiceNotFound(executionID)
	}

	return execution, nil
}

// CancelExecution cancels a pipeline execution.
func (pm *PipelineManagerImpl) CancelExecution(executionID string) error {
	execution, err := pm.GetExecution(executionID)
	if err != nil {
		return err
	}

	if err := execution.Cancel(); err != nil {
		return fmt.Errorf("failed to cancel execution: %w", err)
	}

	pm.logger.Info("pipeline execution cancelled",
		logger.String("execution_id", executionID),
	)

	return nil
}

// CreateTemplate creates a new pipeline template.
func (pm *PipelineManagerImpl) CreateTemplate(template PipelineTemplate) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.templates[template.ID]; exists {
		return errors.ErrServiceAlreadyExists(template.ID)
	}

	pm.templates[template.ID] = template

	pm.logger.Info("pipeline template created",
		logger.String("template_id", template.ID),
		logger.String("template_name", template.Name),
	)

	return nil
}

// GetTemplate retrieves a template by ID.
func (pm *PipelineManagerImpl) GetTemplate(templateID string) (PipelineTemplate, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	template, exists := pm.templates[templateID]
	if !exists {
		return PipelineTemplate{}, errors.ErrServiceNotFound(templateID)
	}

	return template, nil
}

// ListTemplates returns all pipeline templates.
func (pm *PipelineManagerImpl) ListTemplates() []PipelineTemplate {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	templates := make([]PipelineTemplate, 0, len(pm.templates))
	for _, template := range pm.templates {
		templates = append(templates, template)
	}

	return templates
}

// CreateFromTemplate creates a pipeline from a template.
func (pm *PipelineManagerImpl) CreateFromTemplate(templateID string, config map[string]any) (TrainingPipeline, error) {
	template, err := pm.GetTemplate(templateID)
	if err != nil {
		return nil, err
	}

	// Create pipeline config from template
	pipelineConfig := template.Config

	// Apply custom configuration
	if name, ok := config["name"].(string); ok {
		pipelineConfig.Name = name
	}

	if id, ok := config["id"].(string); ok {
		pipelineConfig.ID = id
	} else {
		pipelineConfig.ID = fmt.Sprintf("pipeline_%d", time.Now().UnixNano())
	}

	// Merge parameters
	if params, ok := config["parameters"].(map[string]any); ok {
		if pipelineConfig.Parameters == nil {
			pipelineConfig.Parameters = make(map[string]any)
		}

		for key, value := range params {
			pipelineConfig.Parameters[key] = value
		}
	}

	// Create the pipeline
	pipeline, err := pm.CreatePipeline(pipelineConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline from template: %w", err)
	}

	pm.logger.Info("pipeline created from template",
		logger.String("template_id", templateID),
		logger.String("pipeline_id", pipeline.ID()),
	)

	return pipeline, nil
}
