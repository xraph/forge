package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	aicore "github.com/xraph/forge/pkg/ai/core"
	"github.com/xraph/forge/pkg/ai/training"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/plugins/common"
)

// AIAgentPlugin extends Plugin interface with AI agent-specific functionality
type AIAgentPlugin interface {
	common.Plugin

	// Agent management
	CreateAgent(config aicore.AgentConfig) (aicore.AIAgent, error)
	DestroyAgent(ctx context.Context, agentID string) error
	GetAgent(agentID string) (aicore.AIAgent, error)
	ListAgents() []aicore.AIAgent

	// Agent types and capabilities
	GetAgentTypes() []aicore.AgentType
	GetAgentCapabilities() []aicore.Capability
	GetSupportedInputTypes() []string
	GetSupportedOutputTypes() []string

	// Training and learning
	TrainAgent(ctx context.Context, agent aicore.AIAgent, data TrainingData) error
	UpdateAgentModel(ctx context.Context, agentID string, modelData []byte) error
	GetTrainingStatus(agentID string) (TrainingStatus, error)

	// Configuration and optimization
	OptimizeAgent(ctx context.Context, agentID string, criteria OptimizationCriteria) error
	GetAgentOptimizationSuggestions(agentID string) ([]AgentOptimizationSuggestion, error)

	// Monitoring and analytics
	GetAgentPerformance(agentID string) (AgentPerformance, error)
	GetAgentAnalytics(agentID string, timeRange TimeRange) (AgentAnalytics, error)
	MonitorAgent(ctx context.Context, agentID string, callback MonitorCallback) error
}

// TrainingData represents data used for training agents
type TrainingData struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	Labels    []interface{}          `json:"labels,omitempty"`
	Features  map[string]interface{} `json:"features"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
}

// TrainingStatus represents the status of agent training
type TrainingStatus struct {
	AgentID      string                  `json:"agent_id"`
	Status       training.TrainingStatus `json:"status"` // training, completed, failed, paused
	Progress     float64                 `json:"progress"`
	Epoch        int                     `json:"epoch"`
	Loss         float64                 `json:"loss"`
	Accuracy     float64                 `json:"accuracy"`
	StartedAt    time.Time               `json:"started_at"`
	EstimatedETA time.Duration           `json:"estimated_eta"`
	Message      string                  `json:"message"`
}

// OptimizationCriteria defines criteria for agent optimization
type OptimizationCriteria struct {
	Objectives    []string               `json:"objectives"`  // accuracy, speed, memory
	Constraints   map[string]interface{} `json:"constraints"` // max_latency, max_memory
	Preferences   map[string]float64     `json:"preferences"` // weighted preferences
	TargetMetrics map[string]float64     `json:"target_metrics"`
}

// AgentOptimizationSuggestion represents a suggestion for improving agent performance
type AgentOptimizationSuggestion struct {
	Type        string                 `json:"type"`     // hyperparameter, architecture, data
	Category    string                 `json:"category"` // performance, accuracy, efficiency
	Suggestion  string                 `json:"suggestion"`
	Impact      string                 `json:"impact"` // high, medium, low
	Effort      string                 `json:"effort"` // low, medium, high
	Parameters  map[string]interface{} `json:"parameters"`
	Confidence  float64                `json:"confidence"`
	Description string                 `json:"description"`
}

// AgentPerformance contains performance metrics for an agent
type AgentPerformance struct {
	AgentID         string                 `json:"agent_id"`
	Accuracy        float64                `json:"accuracy"`
	Precision       float64                `json:"precision"`
	Recall          float64                `json:"recall"`
	F1Score         float64                `json:"f1_score"`
	Latency         time.Duration          `json:"latency"`
	Throughput      float64                `json:"throughput"`
	MemoryUsage     int64                  `json:"memory_usage"`
	CPUUsage        float64                `json:"cpu_usage"`
	ErrorRate       float64                `json:"error_rate"`
	ConfidenceScore float64                `json:"confidence_score"`
	CustomMetrics   map[string]interface{} `json:"custom_metrics"`
	LastUpdated     time.Time              `json:"last_updated"`
}

// AgentAnalytics contains analytics data for an agent
type AgentAnalytics struct {
	AgentID           string               `json:"agent_id"`
	TimeRange         TimeRange            `json:"time_range"`
	RequestCount      int64                `json:"request_count"`
	SuccessCount      int64                `json:"success_count"`
	ErrorCount        int64                `json:"error_count"`
	AverageLatency    time.Duration        `json:"average_latency"`
	ThroughputHistory []ThroughputPoint    `json:"throughput_history"`
	AccuracyHistory   []AccuracyPoint      `json:"accuracy_history"`
	ErrorDistribution map[string]int64     `json:"error_distribution"`
	ConfidenceTrend   []ConfidencePoint    `json:"confidence_trend"`
	UserFeedback      []UserFeedbackPoint  `json:"user_feedback"`
	ResourceUsage     []ResourceUsagePoint `json:"resource_usage"`
	Insights          []AnalyticsInsight   `json:"insights"`
}

// TimeRange represents a time range for analytics
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ThroughputPoint represents a point in throughput history
type ThroughputPoint struct {
	Timestamp  time.Time `json:"timestamp"`
	Throughput float64   `json:"throughput"`
}

// AccuracyPoint represents a point in accuracy history
type AccuracyPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Accuracy  float64   `json:"accuracy"`
}

// ConfidencePoint represents a point in confidence trend
type ConfidencePoint struct {
	Timestamp  time.Time `json:"timestamp"`
	Confidence float64   `json:"confidence"`
}

// UserFeedbackPoint represents user feedback data
type UserFeedbackPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Rating    float64   `json:"rating"`
	Feedback  string    `json:"feedback"`
	Category  string    `json:"category"`
}

// ResourceUsagePoint represents resource usage data
type ResourceUsagePoint struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUUsage    float64   `json:"cpu_usage"`
	MemoryUsage int64     `json:"memory_usage"`
	GPUUsage    float64   `json:"gpu_usage,omitempty"`
}

// AnalyticsInsight represents an analytical insight
type AnalyticsInsight struct {
	Type        string                 `json:"type"`     // trend, anomaly, recommendation
	Severity    string                 `json:"severity"` // info, warning, critical
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   time.Time              `json:"timestamp"`
	ActionItems []string               `json:"action_items"`
}

// MonitorCallback defines callback function for agent monitoring
type MonitorCallback func(agentID string, event MonitorEvent)

// MonitorEvent represents a monitoring event
type MonitorEvent struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
	Severity  string                 `json:"severity"`
}

// BaseAIAgentPlugin provides a base implementation for AI agent plugins
type BaseAIAgentPlugin struct {
	*BasePlugin
	agents           map[string]aicore.AIAgent
	agentTypes       []aicore.AgentType
	capabilities     []aicore.Capability
	supportedInputs  []string
	supportedOutputs []string
	trainingJobs     map[string]*TrainingJob
	monitors         map[string]context.CancelFunc
	mu               sync.RWMutex
}

// BasePlugin should be implemented separately - this is a placeholder
type BasePlugin struct {
	id          string
	name        string
	version     string
	description string
	author      string
	license     string
	logger      common.Logger
	metrics     common.Metrics
	config      common.ConfigManager
	container   common.Container
	started     bool
	mu          sync.RWMutex
}

// TrainingJob represents a training job for an agent
type TrainingJob struct {
	AgentID     string
	Status      string
	Progress    float64
	StartedAt   time.Time
	CompletedAt time.Time
	Error       error
	mu          sync.RWMutex
}

// NewBaseAIAgentPlugin creates a new base AI agent plugin
func NewBaseAIAgentPlugin(id, name, version string, agentTypes []aicore.AgentType, capabilities []aicore.Capability) *BaseAIAgentPlugin {
	return &BaseAIAgentPlugin{
		BasePlugin: &BasePlugin{
			id:      id,
			name:    name,
			version: version,
		},
		agents:           make(map[string]aicore.AIAgent),
		agentTypes:       agentTypes,
		capabilities:     capabilities,
		supportedInputs:  []string{},
		supportedOutputs: []string{},
		trainingJobs:     make(map[string]*TrainingJob),
		monitors:         make(map[string]context.CancelFunc),
	}
}

// ID returns plugin ID
func (p *BaseAIAgentPlugin) ID() string {
	return p.BasePlugin.id
}

// Name returns plugin name
func (p *BaseAIAgentPlugin) Name() string {
	return p.BasePlugin.name
}

// Version returns plugin version
func (p *BaseAIAgentPlugin) Version() string {
	return p.BasePlugin.version
}

// Description returns plugin description
func (p *BaseAIAgentPlugin) Description() string {
	return p.BasePlugin.description
}

// Author returns plugin author
func (p *BaseAIAgentPlugin) Author() string {
	return p.BasePlugin.author
}

// License returns plugin license
func (p *BaseAIAgentPlugin) License() string {
	return p.BasePlugin.license
}

// Initialize initializes the AI agent plugin
func (p *BaseAIAgentPlugin) Initialize(ctx context.Context, container common.Container) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.BasePlugin.container = container

	// Resolve dependencies
	if logger, err := container.Resolve((*common.Logger)(nil)); err == nil {
		p.BasePlugin.logger = logger.(common.Logger)
	}

	if metrics, err := container.Resolve((*common.Metrics)(nil)); err == nil {
		p.BasePlugin.metrics = metrics.(common.Metrics)
	}

	if config, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		p.BasePlugin.config = config.(common.ConfigManager)
	}

	if p.BasePlugin.logger != nil {
		p.BasePlugin.logger.Info("AI agent plugin initialized",
			logger.String("plugin_id", p.id),
			logger.String("plugin_name", p.name),
			logger.Int("agent_types", len(p.agentTypes)),
		)
	}

	return nil
}

// Start starts the plugin
func (p *BaseAIAgentPlugin) OnStart(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.BasePlugin.started {
		return fmt.Errorf("plugin already started")
	}

	p.BasePlugin.started = true

	if p.BasePlugin.logger != nil {
		p.BasePlugin.logger.Info("AI agent plugin started", logger.String("plugin_id", p.id))
	}

	return nil
}

// Stop stops the plugin
func (p *BaseAIAgentPlugin) OnStop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.BasePlugin.started {
		return fmt.Errorf("plugin not started")
	}

	// OnStop all agents
	for agentID, agent := range p.agents {
		if err := agent.Stop(ctx); err != nil && p.BasePlugin.logger != nil {
			p.BasePlugin.logger.Error("failed to stop agent",
				logger.String("agent_id", agentID),
				logger.Error(err),
			)
		}
	}

	// Cancel all monitors
	for agentID, cancel := range p.monitors {
		cancel()
		delete(p.monitors, agentID)
	}

	p.BasePlugin.started = false

	if p.BasePlugin.logger != nil {
		p.BasePlugin.logger.Info("AI agent plugin stopped", logger.String("plugin_id", p.id))
	}

	return nil
}

// Cleanup cleans up plugin resources
func (p *BaseAIAgentPlugin) Cleanup(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clean up agents
	p.agents = make(map[string]aicore.AIAgent)
	p.trainingJobs = make(map[string]*TrainingJob)
	p.monitors = make(map[string]context.CancelFunc)

	return nil
}

// Type returns plugin type
func (p *BaseAIAgentPlugin) Type() plugins.PluginType {
	return plugins.PluginTypeAI
}

// CreateAgent creates a new AI agent
func (p *BaseAIAgentPlugin) CreateAgent(config aicore.AgentConfig) (aicore.AIAgent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// This is a base implementation - should be overridden
	agent := aicore.NewBaseAgent(
		fmt.Sprintf("%s-%d", config.ID, len(p.agents)),
		config.Name,
		aicore.AgentTypeOptimization, // Default type
		p.capabilities,
	)

	if err := agent.Initialize(context.Background(), config); err != nil {
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	p.agents[agent.ID()] = agent

	if p.BasePlugin.logger != nil {
		p.BasePlugin.logger.Info("agent created",
			logger.String("plugin_id", p.id),
			logger.String("agent_id", agent.ID()),
		)
	}

	return agent, nil
}

// DestroyAgent destroys an AI agent
func (p *BaseAIAgentPlugin) DestroyAgent(ctx context.Context, agentID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	agent, exists := p.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if err := agent.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	delete(p.agents, agentID)

	// Cancel monitoring if active
	if cancel, exists := p.monitors[agentID]; exists {
		cancel()
		delete(p.monitors, agentID)
	}

	if p.BasePlugin.logger != nil {
		p.BasePlugin.logger.Info("agent destroyed",
			logger.String("plugin_id", p.id),
			logger.String("agent_id", agentID),
		)
	}

	return nil
}

// GetAgent returns an AI agent by ID
func (p *BaseAIAgentPlugin) GetAgent(agentID string) (aicore.AIAgent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	agent, exists := p.agents[agentID]
	if !exists {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}

	return agent, nil
}

// ListAgents returns all agents
func (p *BaseAIAgentPlugin) ListAgents() []aicore.AIAgent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	agents := make([]aicore.AIAgent, 0, len(p.agents))
	for _, agent := range p.agents {
		agents = append(agents, agent)
	}

	return agents
}

// GetAgentTypes returns supported agent types
func (p *BaseAIAgentPlugin) GetAgentTypes() []aicore.AgentType {
	return p.agentTypes
}

// GetAgentCapabilities returns agent capabilities
func (p *BaseAIAgentPlugin) GetAgentCapabilities() []aicore.Capability {
	return p.capabilities
}

// GetSupportedInputTypes returns supported input types
func (p *BaseAIAgentPlugin) GetSupportedInputTypes() []string {
	return p.supportedInputs
}

// GetSupportedOutputTypes returns supported output types
func (p *BaseAIAgentPlugin) GetSupportedOutputTypes() []string {
	return p.supportedOutputs
}

// TrainAgent trains an agent with provided data
func (p *BaseAIAgentPlugin) TrainAgent(ctx context.Context, agent aicore.AIAgent, data TrainingData) error {
	p.mu.Lock()
	job := &TrainingJob{
		AgentID:   agent.ID(),
		Status:    "training",
		Progress:  0.0,
		StartedAt: time.Now(),
	}
	p.trainingJobs[agent.ID()] = job
	p.mu.Unlock()

	// OnStart training in background
	go func() {
		defer func() {
			job.mu.Lock()
			job.CompletedAt = time.Now()
			if job.Error == nil {
				job.Status = "completed"
				job.Progress = 1.0
			} else {
				job.Status = "failed"
			}
			job.mu.Unlock()
		}()

		// Convert training data to agent feedback
		feedback := aicore.AgentFeedback{
			ActionID:  fmt.Sprintf("training-%d", time.Now().Unix()),
			Success:   true, // Assume success for base implementation
			Outcome:   data.Data,
			Metrics:   map[string]float64{"accuracy": 0.85}, // Mock metrics
			Context:   data.Metadata,
			Timestamp: time.Now(),
		}

		if err := agent.Learn(ctx, feedback); err != nil {
			job.mu.Lock()
			job.Error = err
			job.mu.Unlock()
		}
	}()

	return nil
}

// UpdateAgentModel updates an agent's model
func (p *BaseAIAgentPlugin) UpdateAgentModel(ctx context.Context, agentID string, modelData []byte) error {
	agent, err := p.GetAgent(agentID)
	if err != nil {
		return err
	}

	// Base implementation - should be overridden
	_ = agent
	_ = modelData

	if p.BasePlugin.logger != nil {
		p.BasePlugin.logger.Info("agent model updated",
			logger.String("agent_id", agentID),
			logger.Int("model_size", len(modelData)),
		)
	}

	return nil
}

// GetTrainingStatus returns training status for an agent
func (p *BaseAIAgentPlugin) GetTrainingStatus(agentID string) (TrainingStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	job, exists := p.trainingJobs[agentID]
	if !exists {
		return TrainingStatus{}, fmt.Errorf("no training job found for agent %s", agentID)
	}

	job.mu.RLock()
	defer job.mu.RUnlock()

	status := TrainingStatus{
		AgentID:   agentID,
		Status:    job.Status,
		Progress:  job.Progress,
		StartedAt: job.StartedAt,
	}

	if job.Error != nil {
		status.Message = job.Error.Error()
	}

	return status, nil
}

// OptimizeAgent optimizes an agent based on criteria
func (p *BaseAIAgentPlugin) OptimizeAgent(ctx context.Context, agentID string, criteria OptimizationCriteria) error {
	agent, err := p.GetAgent(agentID)
	if err != nil {
		return err
	}

	// Base implementation - should be overridden
	_ = agent
	_ = criteria

	if p.BasePlugin.logger != nil {
		p.BasePlugin.logger.Info("agent optimization started",
			logger.String("agent_id", agentID),
			logger.String("objectives", fmt.Sprintf("%v", criteria.Objectives)),
		)
	}

	return nil
}

// GetAgentOptimizationSuggestions returns optimization suggestions for an agent
func (p *BaseAIAgentPlugin) GetAgentOptimizationSuggestions(agentID string) ([]AgentOptimizationSuggestion, error) {
	agent, err := p.GetAgent(agentID)
	if err != nil {
		return nil, err
	}

	// Base implementation - return mock suggestions
	stats := agent.GetStats()
	suggestions := []AgentOptimizationSuggestion{}

	if stats.ErrorRate > 0.1 {
		suggestions = append(suggestions, AgentOptimizationSuggestion{
			Type:        "training",
			Category:    "accuracy",
			Suggestion:  "Increase training data or adjust model parameters",
			Impact:      "high",
			Effort:      "medium",
			Confidence:  0.8,
			Description: "High error rate detected, additional training recommended",
		})
	}

	if stats.AverageLatency > 100*time.Millisecond {
		suggestions = append(suggestions, AgentOptimizationSuggestion{
			Type:        "optimization",
			Category:    "performance",
			Suggestion:  "Optimize model inference pipeline",
			Impact:      "medium",
			Effort:      "low",
			Confidence:  0.9,
			Description: "Latency above threshold, optimization recommended",
		})
	}

	return suggestions, nil
}

// GetAgentPerformance returns performance metrics for an agent
func (p *BaseAIAgentPlugin) GetAgentPerformance(agentID string) (AgentPerformance, error) {
	agent, err := p.GetAgent(agentID)
	if err != nil {
		return AgentPerformance{}, err
	}

	stats := agent.GetStats()
	health := agent.GetHealth()

	return AgentPerformance{
		AgentID:         agentID,
		Accuracy:        stats.LearningMetrics.AccuracyScore,
		Latency:         stats.AverageLatency,
		ErrorRate:       stats.ErrorRate,
		ConfidenceScore: stats.Confidence,
		CustomMetrics:   make(map[string]interface{}),
		LastUpdated:     time.Now(),
	}, nil
}

// GetAgentAnalytics returns analytics data for an agent
func (p *BaseAIAgentPlugin) GetAgentAnalytics(agentID string, timeRange TimeRange) (AgentAnalytics, error) {
	agent, err := p.GetAgent(agentID)
	if err != nil {
		return AgentAnalytics{}, err
	}

	stats := agent.GetStats()

	// Base implementation - return mock analytics
	return AgentAnalytics{
		AgentID:           agentID,
		TimeRange:         timeRange,
		RequestCount:      stats.TotalProcessed,
		SuccessCount:      stats.TotalProcessed - stats.TotalErrors,
		ErrorCount:        stats.TotalErrors,
		AverageLatency:    stats.AverageLatency,
		ThroughputHistory: []ThroughputPoint{},
		AccuracyHistory:   []AccuracyPoint{},
		ErrorDistribution: make(map[string]int64),
		ConfidenceTrend:   []ConfidencePoint{},
		UserFeedback:      []UserFeedbackPoint{},
		ResourceUsage:     []ResourceUsagePoint{},
		Insights:          []AnalyticsInsight{},
	}, nil
}

// MonitorAgent monitors an agent
func (p *BaseAIAgentPlugin) MonitorAgent(ctx context.Context, agentID string, callback MonitorCallback) error {
	agent, err := p.GetAgent(agentID)
	if err != nil {
		return err
	}

	// Cancel existing monitor if any
	p.mu.Lock()
	if cancel, exists := p.monitors[agentID]; exists {
		cancel()
	}

	// OnStart monitoring
	ctx, cancel := context.WithCancel(ctx)
	p.monitors[agentID] = cancel
	p.mu.Unlock()

	// Monitor in background
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := agent.GetStats()
				health := agent.GetHealth()

				event := MonitorEvent{
					Type:      "stats",
					Timestamp: time.Now(),
					Data: map[string]interface{}{
						"processed":     stats.TotalProcessed,
						"errors":        stats.TotalErrors,
						"error_rate":    stats.ErrorRate,
						"confidence":    stats.Confidence,
						"health_status": health.Status,
					},
					Severity: "info",
				}

				if stats.ErrorRate > 0.2 {
					event.Severity = "warning"
				}

				callback(agentID, event)
			}
		}
	}()

	return nil
}

// Implement remaining Plugin interface methods

func (p *BaseAIAgentPlugin) Capabilities() []plugins.PluginCapability {
	capabilities := make([]plugins.PluginCapability, len(p.capabilities))
	for i, cap := range p.capabilities {
		capabilities[i] = plugins.PluginCapability{
			Name:        cap.Name,
			Description: cap.Description,
			Interface:   "AIAgent",
			Methods:     []string{"Process", "Learn", "GetStats"},
			Metadata:    cap.Metadata,
		}
	}
	return capabilities
}

func (p *BaseAIAgentPlugin) Dependencies() []plugins.PluginDependency {
	return []plugins.PluginDependency{
		{
			Name:     "ai-manager",
			Type:     "service",
			Required: true,
		},
		{
			Name:     "logger",
			Type:     "service",
			Required: false,
		},
	}
}

func (p *BaseAIAgentPlugin) Middleware() []common.MiddlewareDefinition {
	return []common.MiddlewareDefinition{}
}

func (p *BaseAIAgentPlugin) Routes() []common.RouteDefinition {
	return []common.RouteDefinition{}
}

func (p *BaseAIAgentPlugin) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{}
}

func (p *BaseAIAgentPlugin) Commands() []plugins.CLICommand {
	return []plugins.CLICommand{}
}

func (p *BaseAIAgentPlugin) Hooks() []plugins.Hook {
	return []plugins.Hook{}
}

func (p *BaseAIAgentPlugin) ConfigSchema() plugins.ConfigSchema {
	return plugins.ConfigSchema{}
}

func (p *BaseAIAgentPlugin) Configure(config interface{}) error {
	return nil
}

func (p *BaseAIAgentPlugin) GetConfig() interface{} {
	return nil
}

func (p *BaseAIAgentPlugin) HealthCheck(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.BasePlugin.started {
		return fmt.Errorf("plugin not started")
	}

	// Check agent health
	for agentID, agent := range p.agents {
		health := agent.GetHealth()
		if health.Status == aicore.AgentHealthStatusUnhealthy {
			return fmt.Errorf("agent %s is unhealthy: %s", agentID, health.Message)
		}
	}

	return nil
}

func (p *BaseAIAgentPlugin) GetMetrics() plugins.PluginMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return plugins.PluginMetrics{
		CallCount:      int64(len(p.agents)),
		ErrorCount:     0,
		AverageLatency: 0,
		LastExecuted:   time.Now(),
		MemoryUsage:    0,
		CPUUsage:       0,
		HealthScore:    1.0,
		Uptime:         time.Since(time.Now()),
	}
}
