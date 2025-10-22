package internal

import (
	"context"
	"reflect"
	"time"

	"github.com/xraph/forge/v2"
)

// AI defines the contract for AI manager implementations
type AI interface {
	// Lifecycle management
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	OnHealthCheck(ctx context.Context) error

	// Configuration
	GetConfig() AIConfig
	UpdateConfig(config AIConfig) error

	// Status and statistics
	GetStats() map[string]interface{}
	IsLLMEnabled() bool
	IsAgentsEnabled() bool
	IsInferenceEnabled() bool
	IsCoordinationEnabled() bool
	IsTrainingEnabled() bool

	// Agent management
	RegisterAgent(agent AIAgent) error
	UnregisterAgent(id string) error
	GetAgent(id string) (AIAgent, error)
	ListAgents() []AIAgent
	GetAgents() []AIAgent
	ProcessAgentRequest(ctx context.Context, request AgentRequest) (*AgentResponse, error)

	// Component accessors
	GetInferenceEngine() interface{}
	GetModelServer() interface{}
	GetLLMManager() interface{}
	GetCoordinator() interface{}
}

// AgentType defines the type of AI agent
type AgentType string

const (
	AgentTypeOptimization     AgentType = "optimization"      // Performance optimization
	AgentTypeAnomalyDetection AgentType = "anomaly_detection" // Anomaly detection
	AgentTypeLoadBalancer     AgentType = "load_balancer"     // Intelligent load balancing
	AgentTypeCacheManager     AgentType = "cache_manager"     // Cache optimization
	AgentTypeJobScheduler     AgentType = "job_scheduler"     // Job scheduling optimization
	AgentTypeSecurityMonitor  AgentType = "security_monitor"  // Security monitoring
	AgentTypeResourceManager  AgentType = "resource_manager"  // Resource optimization
	AgentTypePredictor        AgentType = "predictor"         // Predictive analytics
)

// AgentHealthStatus represents the health status of an agent
type AgentHealthStatus string

const (
	AgentHealthStatusHealthy   AgentHealthStatus = "healthy"
	AgentHealthStatusUnhealthy AgentHealthStatus = "unhealthy"
	AgentHealthStatusDegraded  AgentHealthStatus = "degraded"
	AgentHealthStatusUnknown   AgentHealthStatus = "unknown"
)

// AIAgent interface defines the contract for AI agents
type AIAgent interface {
	// Basic agent information
	ID() string
	Name() string
	Type() AgentType
	Capabilities() []Capability

	// Agent lifecycle
	Initialize(ctx context.Context, config AgentConfig) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Agent processing
	Process(ctx context.Context, input AgentInput) (AgentOutput, error)
	Learn(ctx context.Context, feedback AgentFeedback) error

	// Agent status
	GetStats() AgentStats
	GetHealth() AgentHealth
}

// Capability represents a capability that an agent can perform
type Capability struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputType   reflect.Type           `json:"-"`
	OutputType  reflect.Type           `json:"-"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// AgentConfig contains configuration for an AI agent
type AgentConfig struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Logger          forge.Logger           `json:"-"`
	Metrics         forge.Metrics          `json:"-"`
	MaxConcurrency  int                    `json:"max_concurrency"`
	Timeout         time.Duration          `json:"timeout"`
	LearningEnabled bool                   `json:"learning_enabled"`
	AutoApply       bool                   `json:"auto_apply"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// AgentInput represents input data for an agent
type AgentInput struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	Context   map[string]interface{} `json:"context"`
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id"`
}

// AgentOutput represents output from an agent
type AgentOutput struct {
	Type        string                 `json:"type"`
	Data        interface{}            `json:"data"`
	Confidence  float64                `json:"confidence"`
	Explanation string                 `json:"explanation"`
	Actions     []AgentAction          `json:"actions"`
	Metadata    map[string]interface{} `json:"metadata"`
	Timestamp   time.Time              `json:"timestamp"`
}

// AgentAction represents an action recommended by an agent
type AgentAction struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Target      string                 `json:"target"`
	Action      string                 `json:"action"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Priority    int                    `json:"priority"`
	Condition   string                 `json:"condition,omitempty"`
	Rollback    bool                   `json:"rollback"`
	Timeout     time.Duration          `json:"timeout"`
	Automatic   bool                   `json:"automatic"`
	Metadata    map[string]interface{} `json:"metadata"`
	Timestamp   time.Time              `json:"timestamp"`
	Owner       string                 `json:"owner"`
	Status      string                 `json:"status"`
}

// AgentFeedback represents feedback about an agent's performance
type AgentFeedback struct {
	ActionID  string                 `json:"action_id"`
	Success   bool                   `json:"success"`
	Outcome   interface{}            `json:"outcome"`
	Metrics   map[string]float64     `json:"metrics"`
	Context   map[string]interface{} `json:"context"`
	Timestamp time.Time              `json:"timestamp"`
}

// AgentStats contains statistics about an agent
type AgentStats struct {
	TotalProcessed  int64           `json:"total_processed"`
	TotalErrors     int64           `json:"total_errors"`
	AverageLatency  time.Duration   `json:"average_latency"`
	ErrorRate       float64         `json:"error_rate"`
	LastProcessed   time.Time       `json:"last_processed"`
	IsActive        bool            `json:"is_active"`
	Confidence      float64         `json:"confidence"`
	LearningMetrics LearningMetrics `json:"learning_metrics"`
}

// LearningMetrics contains metrics about an agent's learning
type LearningMetrics struct {
	TotalFeedback     int64     `json:"total_feedback"`
	PositiveFeedback  int64     `json:"positive_feedback"`
	NegativeFeedback  int64     `json:"negative_feedback"`
	AccuracyScore     float64   `json:"accuracy_score"`
	ImprovementRate   float64   `json:"improvement_rate"`
	LastLearningEvent time.Time `json:"last_learning_event"`
}

// AgentHealth represents the health status of an agent
type AgentHealth struct {
	Status      AgentHealthStatus      `json:"status"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details"`
	CheckedAt   time.Time              `json:"checked_at"`
	LastHealthy time.Time              `json:"last_healthy"`
}

// AgentRequest represents a request to an agent
type AgentRequest struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Data    interface{}            `json:"data"`
	Context map[string]interface{} `json:"context"`
}

// AgentResponse represents a response from an agent
type AgentResponse struct {
	ID      string      `json:"id"`
	AgentID string      `json:"agent_id"`
	Output  AgentOutput `json:"output"`
	Error   error       `json:"error,omitempty"`
}
