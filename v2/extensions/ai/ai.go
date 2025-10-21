package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/ai/internal"
	"github.com/xraph/forge/v2/internal/logger"
)

// managerImpl provides centralized AI capabilities management
type managerImpl struct {
	config       internal.AIConfig
	logger       forge.Logger
	metrics      forge.Metrics
	started      bool
	mu           sync.RWMutex
	shutdownC    chan struct{}
	wg           sync.WaitGroup
	// Agent management
	agents       map[string]internal.AIAgent
	agentsMu     sync.RWMutex
	// Storage and factory
	store        AgentStore    // Optional persistent store
	agentFactory *AgentFactory // For dynamic creation
	teams        map[string]*AgentTeam
	teamsMu      sync.RWMutex
}

// AIManager type alias for backward compatibility
type AIM = internal.AI

// NewManager creates a new AI manager
func NewManager(config internal.AIConfig, opts ...ManagerOption) (*managerImpl, error) {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	m := &managerImpl{
		config:    config,
		logger:    config.Logger,
		metrics:   config.Metrics,
		shutdownC: make(chan struct{}),
		agents:    make(map[string]internal.AIAgent),
		teams:     make(map[string]*AgentTeam),
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// ManagerOption configures the manager
type ManagerOption func(*managerImpl)

// WithStore sets the agent store
func WithStore(store AgentStore) ManagerOption {
	return func(m *managerImpl) {
		m.store = store
	}
}

// WithFactory sets the agent factory
func WithFactory(factory *AgentFactory) ManagerOption {
	return func(m *managerImpl) {
		m.agentFactory = factory
	}
}

// Start starts the AI manager
func (m *managerImpl) Start(ctx context.Context) error {
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
func (m *managerImpl) Stop(ctx context.Context) error {
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
func (m *managerImpl) HealthCheck(ctx context.Context) error {
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
func (m *managerImpl) GetStats() map[string]interface{} {
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
func (m *managerImpl) IsLLMEnabled() bool {
	return m.config.EnableLLM
}

// IsAgentsEnabled returns whether agent capabilities are enabled
func (m *managerImpl) IsAgentsEnabled() bool {
	return m.config.EnableAgents
}

// IsInferenceEnabled returns whether inference capabilities are enabled
func (m *managerImpl) IsInferenceEnabled() bool {
	return m.config.EnableInference
}

// IsCoordinationEnabled returns whether coordination capabilities are enabled
func (m *managerImpl) IsCoordinationEnabled() bool {
	return m.config.EnableCoordination
}

// IsTrainingEnabled returns whether training capabilities are enabled
func (m *managerImpl) IsTrainingEnabled() bool {
	return m.config.EnableTraining
}

// GetConfig returns the current configuration
func (m *managerImpl) GetConfig() internal.AIConfig {
	return m.config
}

// UpdateConfig updates the configuration
func (m *managerImpl) UpdateConfig(config internal.AIConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot update config while manager is running")
	}

	m.config = config
	return nil
}

// RegisterAgent registers a new agent with the manager
func (m *managerImpl) RegisterAgent(agent internal.AIAgent) error {
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
func (m *managerImpl) UnregisterAgent(id string) error {
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
func (m *managerImpl) GetAgent(id string) (internal.AIAgent, error) {
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()

	agent, exists := m.agents[id]
	if !exists {
		return nil, fmt.Errorf("agent with ID %s not found", id)
	}

	return agent, nil
}

// ListAgents returns all registered agents
func (m *managerImpl) ListAgents() []internal.AIAgent {
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()

	agents := make([]internal.AIAgent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}

	return agents
}

// ProcessAgentRequest processes a request through the appropriate agent
func (m *managerImpl) ProcessAgentRequest(ctx context.Context, request AgentRequest) (*AgentResponse, error) {
	// Find the best agent for this request type
	agent, err := m.findBestAgent(request.Type)
	if err != nil {
		return &AgentResponse{
			ID:      request.ID,
			AgentID: "",
			Output:  internal.AgentOutput{},
			Error:   err,
		}, err
	}

	// Convert request to agent input
	input := internal.AgentInput{
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
func (m *managerImpl) findBestAgent(requestType string) (internal.AIAgent, error) {
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
func (m *managerImpl) GetAgents() []internal.AIAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]internal.AIAgent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}
	return agents
}

// GetInferenceEngine returns the inference engine (placeholder)
func (m *managerImpl) GetInferenceEngine() interface{} {
	// Placeholder implementation
	return nil
}

// GetModelServer returns the model server (placeholder)
func (m *managerImpl) GetModelServer() interface{} {
	// Placeholder implementation
	return nil
}

// GetLLMManager returns the LLM manager (placeholder)
func (m *managerImpl) GetLLMManager() interface{} {
	// Placeholder implementation
	return nil
}

// GetCoordinator returns the coordinator (placeholder)
func (m *managerImpl) GetCoordinator() interface{} {
	// Placeholder implementation
	return nil
}

// OnHealthCheck handles health check requests (placeholder)
func (m *managerImpl) OnHealthCheck(ctx context.Context) error {
	// Placeholder implementation
	return nil
}
