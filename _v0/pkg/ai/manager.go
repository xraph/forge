package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// AIMiddlewareType defines the type of AI middleware
type AIMiddlewareType string

const (
	AIMiddlewareTypeLoadBalance          AIMiddlewareType = "load_balance"
	AIMiddlewareTypeAnomalyDetection     AIMiddlewareType = "anomaly_detection"
	AIMiddlewareTypeRateLimit            AIMiddlewareType = "rate_limit"
	AIMiddlewareTypeResponseOptimization AIMiddlewareType = "response_optimization"
	AIMiddlewareTypePersonalization      AIMiddlewareType = "personalization"
	AIMiddlewareTypeOptimization         AIMiddlewareType = "optimization"
	AIMiddlewareTypeSecurity             AIMiddlewareType = "security"
)

// AIMiddlewareConfig contains configuration for AI middleware
type AIMiddlewareConfig struct {
	Enabled    bool                   `json:"enabled"`
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    time.Duration          `json:"timeout"`
	Retries    int                    `json:"retries"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// AIMiddlewareStats contains statistics for AI middleware
type AIMiddlewareStats struct {
	Name              string                 `json:"name"`
	Type              string                 `json:"type"`
	RequestsTotal     int64                  `json:"requests_total"`
	RequestsProcessed int64                  `json:"requests_processed"`
	RequestsBlocked   int64                  `json:"requests_blocked"`
	AverageLatency    time.Duration          `json:"average_latency"`
	ErrorRate         float64                `json:"error_rate"`
	LearningEnabled   bool                   `json:"learning_enabled"`
	AdaptiveChanges   int64                  `json:"adaptive_changes"`
	LastUpdated       time.Time              `json:"last_updated"`
	CustomMetrics     map[string]interface{} `json:"custom_metrics"`
	Metadata          map[string]interface{} `json:"metadata"`
}

// Manager provides centralized AI capabilities management
type Manager struct {
	config    ManagerConfig
	logger    common.Logger
	metrics   common.Metrics
	started   bool
	mu        sync.RWMutex
	shutdownC chan struct{}
	wg        sync.WaitGroup
	// Agent management
	agents   map[string]AIAgent
	agentsMu sync.RWMutex
}

// AIManager type alias for backward compatibility
type AIManager = Manager

// ManagerConfig contains configuration for the AI manager
type ManagerConfig struct {
	EnableLLM          bool           `yaml:"enable_llm" default:"true"`
	EnableAgents       bool           `yaml:"enable_agents" default:"true"`
	EnableTraining     bool           `yaml:"enable_training" default:"false"`
	EnableInference    bool           `yaml:"enable_inference" default:"true"`
	EnableCoordination bool           `yaml:"enable_coordination" default:"true"`
	MaxConcurrency     int            `yaml:"max_concurrency" default:"10"`
	RequestTimeout     time.Duration  `yaml:"request_timeout" default:"30s"`
	CacheSize          int            `yaml:"cache_size" default:"1000"`
	Logger             common.Logger  `yaml:"-"`
	Metrics            common.Metrics `yaml:"-"`
}

// NewManager creates a new AI manager
func NewManager(config ManagerConfig) (*Manager, error) {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	return &Manager{
		config:    config,
		logger:    config.Logger,
		metrics:   config.Metrics,
		shutdownC: make(chan struct{}),
		agents:    make(map[string]AIAgent),
	}, nil
}

// Start starts the AI manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("AI manager already started")
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("AI manager started",
			logger.Bool("llm_enabled", m.config.EnableLLM),
			logger.Bool("agents_enabled", m.config.EnableAgents),
			logger.Bool("inference_enabled", m.config.EnableInference),
			logger.Bool("coordination_enabled", m.config.EnableCoordination),
		)
	}

	return nil
}

// Stop stops the AI manager
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	m.started = false
	close(m.shutdownC)

	// Wait for all goroutines to finish
	m.wg.Wait()

	if m.logger != nil {
		m.logger.Info("AI manager stopped")
	}

	return nil
}

// HealthCheck performs health check on the AI manager
func (m *Manager) HealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return fmt.Errorf("AI manager not started")
	}

	// Perform basic health checks
	// In a real implementation, this would check:
	// - Model availability
	// - Inference engine health
	// - Agent coordination status
	// - Training pipeline status

	return nil
}

// GetStats returns AI manager statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"started":              m.started,
		"llm_enabled":          m.config.EnableLLM,
		"agents_enabled":       m.config.EnableAgents,
		"inference_enabled":    m.config.EnableInference,
		"coordination_enabled": m.config.EnableCoordination,
		"training_enabled":     m.config.EnableTraining,
		"max_concurrency":      m.config.MaxConcurrency,
		"request_timeout":      m.config.RequestTimeout.String(),
		"cache_size":           m.config.CacheSize,
	}

	return stats
}

// IsLLMEnabled returns whether LLM capabilities are enabled
func (m *Manager) IsLLMEnabled() bool {
	return m.config.EnableLLM
}

// IsAgentsEnabled returns whether agent capabilities are enabled
func (m *Manager) IsAgentsEnabled() bool {
	return m.config.EnableAgents
}

// IsInferenceEnabled returns whether inference capabilities are enabled
func (m *Manager) IsInferenceEnabled() bool {
	return m.config.EnableInference
}

// IsCoordinationEnabled returns whether coordination capabilities are enabled
func (m *Manager) IsCoordinationEnabled() bool {
	return m.config.EnableCoordination
}

// IsTrainingEnabled returns whether training capabilities are enabled
func (m *Manager) IsTrainingEnabled() bool {
	return m.config.EnableTraining
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() ManagerConfig {
	return m.config
}

// UpdateConfig updates the configuration
func (m *Manager) UpdateConfig(config ManagerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot update config while manager is running")
	}

	m.config = config
	return nil
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

// RegisterAgent registers a new agent with the manager
func (m *Manager) RegisterAgent(agent AIAgent) error {
	if agent == nil {
		return fmt.Errorf("agent cannot be nil")
	}

	m.agentsMu.Lock()
	defer m.agentsMu.Unlock()

	agentID := agent.ID()
	if _, exists := m.agents[agentID]; exists {
		return fmt.Errorf("agent with ID %s already registered", agentID)
	}

	m.agents[agentID] = agent

	if m.logger != nil {
		m.logger.Info("Agent registered",
			logger.String("agent_id", agentID),
			logger.String("agent_name", agent.Name()),
			logger.String("agent_type", string(agent.Type())),
		)
	}

	return nil
}

// UnregisterAgent removes an agent from the manager
func (m *Manager) UnregisterAgent(id string) error {
	m.agentsMu.Lock()
	defer m.agentsMu.Unlock()

	if _, exists := m.agents[id]; !exists {
		return fmt.Errorf("agent with ID %s not found", id)
	}

	delete(m.agents, id)

	if m.logger != nil {
		m.logger.Info("Agent unregistered", logger.String("agent_id", id))
	}

	return nil
}

// GetAgent retrieves an agent by ID
func (m *Manager) GetAgent(id string) (AIAgent, error) {
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()

	agent, exists := m.agents[id]
	if !exists {
		return nil, fmt.Errorf("agent with ID %s not found", id)
	}

	return agent, nil
}

// ListAgents returns all registered agents
func (m *Manager) ListAgents() []AIAgent {
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()

	agents := make([]AIAgent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}

	return agents
}

// ProcessAgentRequest processes a request through the appropriate agent
func (m *Manager) ProcessAgentRequest(ctx context.Context, request AgentRequest) (*AgentResponse, error) {
	// Find the best agent for this request type
	agent, err := m.findBestAgent(request.Type)
	if err != nil {
		return &AgentResponse{
			ID:      request.ID,
			AgentID: "",
			Output:  AgentOutput{},
			Error:   err,
		}, err
	}

	// Convert request to agent input
	input := AgentInput{
		Type:      request.Type,
		Data:      request.Data,
		Context:   request.Context,
		Timestamp: time.Now(),
		RequestID: request.ID,
	}

	// Process the request
	output, err := agent.Process(ctx, input)
	if err != nil {
		return &AgentResponse{
			ID:      request.ID,
			AgentID: agent.ID(),
			Output:  output,
			Error:   err,
		}, err
	}

	return &AgentResponse{
		ID:      request.ID,
		AgentID: agent.ID(),
		Output:  output,
		Error:   nil,
	}, nil
}

// findBestAgent finds the best agent for a given request type
func (m *Manager) findBestAgent(requestType string) (AIAgent, error) {
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()

	// Simple implementation: find first agent that can handle this type
	for _, agent := range m.agents {
		capabilities := agent.Capabilities()
		for _, capability := range capabilities {
			if capability.Name == requestType {
				return agent, nil
			}
		}
	}

	return nil, fmt.Errorf("no agent found for request type: %s", requestType)
}

// GetAgents returns all registered agents
func (m *Manager) GetAgents() []AIAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]AIAgent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}
	return agents
}

// GetInferenceEngine returns the inference engine (placeholder)
func (m *Manager) GetInferenceEngine() interface{} {
	// Placeholder implementation
	return nil
}

// GetModelServer returns the model server (placeholder)
func (m *Manager) GetModelServer() interface{} {
	// Placeholder implementation
	return nil
}

// GetLLMManager returns the LLM manager (placeholder)
func (m *Manager) GetLLMManager() interface{} {
	// Placeholder implementation
	return nil
}

// GetCoordinator returns the coordinator (placeholder)
func (m *Manager) GetCoordinator() interface{} {
	// Placeholder implementation
	return nil
}

// OnHealthCheck handles health check requests (placeholder)
func (m *Manager) OnHealthCheck(ctx context.Context) error {
	// Placeholder implementation
	return nil
}
