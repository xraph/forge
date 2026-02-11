package ai

import (
	"github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/extensions/ai/training"
)

// Note: AI SDK types are imported directly from github.com/xraph/ai-sdk
// Use: aisdk "github.com/xraph/ai-sdk" in your imports

// Agent type constants for REST API.
const (
	AgentTypeCacheOptimizer  = internal.AgentTypeCacheOptimizer
	AgentTypeScheduler       = internal.AgentTypeScheduler
	AgentTypeAnomalyDetector = internal.AgentTypeAnomalyDetector
	AgentTypeLoadBalancer    = internal.AgentTypeLoadBalancer
	AgentTypeSecurityMonitor = internal.AgentTypeSecurityMonitor
	AgentTypeResourceManager = internal.AgentTypeResourceManager
	AgentTypePredictor       = internal.AgentTypePredictor
	AgentTypeOptimizer       = internal.AgentTypeOptimizer
)

// Agent health status constants.
const (
	AgentHealthStatusHealthy   = internal.AgentHealthStatusHealthy
	AgentHealthStatusUnhealthy = internal.AgentHealthStatusUnhealthy
	AgentHealthStatusDegraded  = internal.AgentHealthStatusDegraded
	AgentHealthStatusUnknown   = internal.AgentHealthStatusUnknown
)

// ========================================
// Training Package Exports
// ========================================

// Training service interfaces - use these types when injecting via DI
type (
	// ModelTrainer manages model training lifecycle
	ModelTrainer = training.ModelTrainer
	// DataManager manages datasets and data sources
	DataManager = training.DataManager
	// PipelineManager manages training pipelines
	PipelineManager = training.PipelineManager
)

// Training job types
type (
	TrainingRequest  = training.TrainingRequest
	TrainingJob      = training.TrainingJob
	TrainingStatus   = training.TrainingStatus
	TrainingProgress = training.TrainingProgress
	TrainingMetrics  = training.TrainingMetrics
	TrainingLogEntry = training.TrainingLogEntry
	TrainingConfig   = training.TrainingConfig
	TrainingCallback = training.TrainingCallback
	TrainedModel     = training.TrainedModel
)

// Dataset types
type (
	Dataset         = training.Dataset
	DatasetConfig   = training.DatasetConfig
	DatasetType     = training.DatasetType
	DatasetMetrics  = training.DatasetMetrics
	DataSource      = training.DataSource
	DataSourceType  = training.DataSourceType
	DataRecord      = training.DataRecord
	DataBatch       = training.DataBatch
	DataIterator    = training.DataIterator
	DataSchema      = training.DataSchema
	DataStatistics  = training.DataStatistics
	DataTransformer = training.DataTransformer
	DataFilter      = training.DataFilter
)

// Pipeline types
type (
	TrainingPipeline  = training.TrainingPipeline
	PipelineConfig    = training.PipelineConfig
	PipelineStatus    = training.PipelineStatus
	PipelineExecution = training.PipelineExecution
	PipelineInput     = training.PipelineInput
	PipelineOutput    = training.PipelineOutput
	PipelineStage     = training.PipelineStage
	PipelineTemplate  = training.PipelineTemplate
	PipelineMetrics   = training.PipelineMetrics
	PipelineLogEntry  = training.PipelineLogEntry
)

// Configuration types
type (
	ModelConfig      = training.ModelConfig
	ValidationConfig = training.ValidationConfig
	ResourceConfig   = training.ResourceConfig
)

// Training status constants
const (
	TrainingStatusPending   = training.TrainingStatusPending
	TrainingStatusRunning   = training.TrainingStatusRunning
	TrainingStatusPaused    = training.TrainingStatusPaused
	TrainingStatusCompleted = training.TrainingStatusCompleted
	TrainingStatusFailed    = training.TrainingStatusFailed
	TrainingStatusCancelled = training.TrainingStatusCancelled
)

// Dataset type constants
const (
	DatasetTypeTabular    = training.DatasetTypeTabular
	DatasetTypeImage      = training.DatasetTypeImage
	DatasetTypeText       = training.DatasetTypeText
	DatasetTypeAudio      = training.DatasetTypeAudio
	DatasetTypeVideo      = training.DatasetTypeVideo
	DatasetTypeTimeSeries = training.DatasetTypeTimeSeries
	DatasetTypeGraph      = training.DatasetTypeGraph
	DatasetTypeMultimodal = training.DatasetTypeMultimodal
)

// Constructors for direct instantiation (without DI)
var (
	// NewModelTrainer creates a new model trainer instance
	NewModelTrainer = training.NewModelTrainer
	// NewDataManager creates a new data manager instance
	NewDataManager = training.NewDataManager
	// NewPipelineManager creates a new pipeline manager instance
	NewPipelineManager = training.NewPipelineManager
)
