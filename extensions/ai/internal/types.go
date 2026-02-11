package internal

import (
	"context"
	"time"

	"github.com/xraph/forge"
)

// AgentType defines the type of AI agent (for REST API compatibility).
type AgentType string

const (
	AgentTypeCacheOptimizer  AgentType = "cache_optimizer"
	AgentTypeScheduler       AgentType = "scheduler"
	AgentTypeAnomalyDetector AgentType = "anomaly_detector"
	AgentTypeLoadBalancer    AgentType = "load_balancer"
	AgentTypeSecurityMonitor AgentType = "security_monitor"
	AgentTypeResourceManager AgentType = "resource_manager"
	AgentTypePredictor       AgentType = "predictor"
	AgentTypeOptimizer       AgentType = "optimizer"
)

// AgentHealthStatus represents the health status of an agent.
type AgentHealthStatus string

const (
	AgentHealthStatusHealthy   AgentHealthStatus = "healthy"
	AgentHealthStatusUnhealthy AgentHealthStatus = "unhealthy"
	AgentHealthStatusDegraded  AgentHealthStatus = "degraded"
	AgentHealthStatusUnknown   AgentHealthStatus = "unknown"
)

// AgentHealth represents the health status of an agent.
type AgentHealth struct {
	Status      AgentHealthStatus `json:"status"`
	Message     string            `json:"message"`
	Details     map[string]any    `json:"details"`
	CheckedAt   time.Time         `json:"checked_at"`
	LastHealthy time.Time         `json:"last_healthy"`
}

// AgentStats contains statistics about an agent.
type AgentStats struct {
	TotalProcessed  int64              `json:"total_processed"`
	TotalErrors     int64              `json:"total_errors"`
	AverageLatency  time.Duration      `json:"average_latency"`
	ErrorRate       float64            `json:"error_rate"`
	Confidence      float64            `json:"confidence"`
	LastProcessed   time.Time          `json:"last_processed"`
	IsActive        bool               `json:"is_active"`
	LearningMetrics AgentLearningStats `json:"learning_metrics"`
}

// AgentLearningStats contains learning-related statistics for an agent.
type AgentLearningStats struct {
	AccuracyScore    float64 `json:"accuracyScore"`
	TotalFeedback    int64   `json:"totalFeedback"`
	PositiveFeedback int64   `json:"positiveFeedback"`
	NegativeFeedback int64   `json:"negativeFeedback"`
}

// AI is the main interface for the AI manager that coordinates agents, models, and inference.
type AI interface {
	// GetStats returns system-wide statistics as a flexible map.
	GetStats() map[string]any

	// GetAgents returns all registered AI agents.
	GetAgents() []AIAgent

	// GetAgent returns a specific agent by ID.
	GetAgent(agentID string) (AIAgent, error)

	// GetInferenceEngine returns the inference engine, or nil if not available.
	GetInferenceEngine() any

	// GetModelServer returns the model server, or nil if not available.
	GetModelServer() any

	// GetLLMManager returns the LLM manager, or nil if not available.
	GetLLMManager() any

	// GetCoordinator returns the coordinator, or nil if not available.
	GetCoordinator() any

	// OnHealthCheck performs a health check on the AI system.
	OnHealthCheck(ctx context.Context) error
}

// AIAgent is the interface for an AI agent that can process inputs and learn from feedback.
type AIAgent interface {
	// ID returns the unique identifier of the agent.
	ID() string

	// Name returns the human-readable name of the agent.
	Name() string

	// Type returns the agent type.
	Type() AgentType

	// GetStats returns the agent's statistics.
	GetStats() AgentStats

	// GetHealth returns the agent's health status.
	GetHealth() AgentHealth

	// Initialize initializes the agent with configuration.
	Initialize(ctx context.Context, config AgentConfig) error

	// Start starts the agent.
	Start(ctx context.Context) error

	// Stop stops the agent.
	Stop(ctx context.Context) error

	// Process processes an input and returns output.
	Process(ctx context.Context, input AgentInput) (AgentOutput, error)

	// Learn learns from feedback to improve future processing.
	Learn(ctx context.Context, feedback AgentFeedback) error
}

// AgentConfig contains configuration for initializing an AI agent.
type AgentConfig struct {
	MaxConcurrency  int            `json:"maxConcurrency"`
	Timeout         time.Duration  `json:"timeout"`
	RetryAttempts   int            `json:"retryAttempts"`
	LearningEnabled bool           `json:"learningEnabled"`
	AutoApply       bool           `json:"autoApply"`
	Parameters      map[string]any `json:"parameters"`
	Metadata        map[string]any `json:"metadata"`
	Logger          forge.Logger   `json:"-"`
	Metrics         forge.Metrics  `json:"-"`
}

// AgentInput represents input to an AI agent for processing.
type AgentInput struct {
	Type      string         `json:"type"`
	Data      any            `json:"data"`
	Context   map[string]any `json:"context"`
	RequestID string         `json:"requestId"`
	Timestamp time.Time      `json:"timestamp"`
}

// AgentOutput represents output from an AI agent after processing.
type AgentOutput struct {
	Data        any            `json:"data"`
	Confidence  float64        `json:"confidence"`
	Explanation string         `json:"explanation"`
	Actions     []AgentAction  `json:"actions"`
	Metadata    map[string]any `json:"metadata"`
	Timestamp   time.Time      `json:"timestamp"`
}

// AgentAction represents an action recommended by an AI agent.
type AgentAction struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Priority    int            `json:"priority"`
	Parameters  map[string]any `json:"parameters"`
}

// AgentFeedback represents feedback for agent learning.
type AgentFeedback struct {
	InputID   string             `json:"inputId"`
	ActionID  string             `json:"actionId"`
	Success   bool               `json:"success"`
	Outcome   any                `json:"outcome"`
	Score     float64            `json:"score"`
	Metrics   map[string]float64 `json:"metrics"`
	Context   map[string]any     `json:"context"`
	Timestamp time.Time          `json:"timestamp"`
}

// LearningMetrics contains metrics about agent learning performance.
type LearningMetrics struct {
	TotalSamples        int64     `json:"totalSamples"`
	LearningRate        float64   `json:"learningRate"`
	AccuracyImprovement float64   `json:"accuracyImprovement"`
	LastTrainedAt       time.Time `json:"lastTrainedAt"`
	ModelVersion        string    `json:"modelVersion"`
}

// Capability represents a capability of an AI agent.
type Capability struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Metadata    map[string]any `json:"metadata"`
}

// Additional agent type constants used by middleware.
const (
	AgentTypeAnomalyDetection     AgentType = "anomaly_detection"
	AgentTypeIntelligentRateLimit AgentType = "intelligent_rate_limit"
	AgentTypePersonalization      AgentType = "personalization"
	AgentTypeResponseOptimizer    AgentType = "response_optimizer"
	AgentTypeSecurityScanner      AgentType = "security_scanner"
	AgentTypeOptimization         AgentType = "optimization"
)

// BaseAgent provides a default implementation of AIAgent.
type BaseAgent struct {
	id           string
	name         string
	agentType    AgentType
	capabilities []Capability
	stats        AgentStats
	health       AgentHealth
	config       AgentConfig
	started      bool
}

// NewBaseAgent creates a new base agent with the given parameters.
func NewBaseAgent(id, name string, agentType AgentType, capabilities []Capability) *BaseAgent {
	return &BaseAgent{
		id:           id,
		name:         name,
		agentType:    agentType,
		capabilities: capabilities,
		health: AgentHealth{
			Status:  AgentHealthStatusHealthy,
			Message: "initialized",
			Details: make(map[string]any),
		},
	}
}

// ID returns the agent's unique identifier.
func (a *BaseAgent) ID() string { return a.id }

// Name returns the agent's name.
func (a *BaseAgent) Name() string { return a.name }

// Type returns the agent's type.
func (a *BaseAgent) Type() AgentType { return a.agentType }

// GetStats returns the agent's statistics.
func (a *BaseAgent) GetStats() AgentStats { return a.stats }

// GetHealth returns the agent's health.
func (a *BaseAgent) GetHealth() AgentHealth { return a.health }

// Initialize initializes the agent.
func (a *BaseAgent) Initialize(_ context.Context, config AgentConfig) error {
	a.config = config
	return nil
}

// Start starts the agent.
func (a *BaseAgent) Start(_ context.Context) error {
	a.started = true
	a.health.Status = AgentHealthStatusHealthy
	a.health.Message = "running"
	return nil
}

// Stop stops the agent.
func (a *BaseAgent) Stop(_ context.Context) error {
	a.started = false
	a.health.Status = AgentHealthStatusUnknown
	a.health.Message = "stopped"
	return nil
}

// Process processes an input. Base implementation returns empty output.
func (a *BaseAgent) Process(_ context.Context, _ AgentInput) (AgentOutput, error) {
	a.stats.TotalProcessed++
	a.stats.LastProcessed = time.Now()
	return AgentOutput{
		Data:      map[string]any{},
		Timestamp: time.Now(),
	}, nil
}

// Learn learns from feedback. Base implementation is a no-op.
func (a *BaseAgent) Learn(_ context.Context, _ AgentFeedback) error {
	return nil
}
