package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/ai/coordination"
	"github.com/xraph/forge/pkg/ai/inference"
	"github.com/xraph/forge/pkg/ai/llm"
	"github.com/xraph/forge/pkg/ai/models"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// AIManager implements the core AI management interface
type AIManager struct {
	agents          map[string]AIAgent
	modelServer     models.ModelServer
	llmManager      *llm.LLMManager
	inferenceEngine *inference.InferenceEngine
	coordinator *coordination.MultiAgentCoordinator
	logger      common.Logger
	metrics     common.Metrics
	config      common.ConfigManager
	started     bool
	mu              sync.RWMutex
}

// AIConfig contains configuration for the AI system
type AIConfig struct {
	MaxAgents           int           `yaml:"max_agents" default:"50"`
	AgentTimeout        time.Duration `yaml:"agent_timeout" default:"30s"`
	ModelCacheSize      int           `yaml:"model_cache_size" default:"10"`
	InferenceWorkers    int           `yaml:"inference_workers" default:"4"`
	EnableCoordination  bool          `yaml:"enable_coordination" default:"true"`
	LogLevel            string        `yaml:"log_level" default:"info"`
	MetricsInterval     time.Duration `yaml:"metrics_interval" default:"30s"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval" default:"60s"`
}

// AIStats contains statistics about the AI system
type AIStats struct {
	TotalAgents       int                          `json:"total_agents"`
	ActiveAgents      int                          `json:"active_agents"`
	ModelsLoaded      int                          `json:"models_loaded"`
	InferenceRequests int64                        `json:"inference_requests"`
	TotalInferences   int64                        `json:"total_inferences"`
	AverageLatency    time.Duration                `json:"average_latency"`
	ErrorRate         float64                      `json:"error_rate"`
	AgentStats        map[string]AgentStats        `json:"agent_stats"`
	ModelStats        map[string]models.ModelStats `json:"model_stats"`
	LastUpdated       time.Time                    `json:"last_updated"`
}

// NewAIManager creates a new AI manager
func NewAIManager(logger common.Logger, metrics common.Metrics, config common.ConfigManager) *AIManager {
	return &AIManager{
		agents:      make(map[string]AIAgent),
		logger:      logger,
		metrics:     metrics,
		config:      config,
		coordinator: coordination.NewMultiAgentCoordinator(, logger, metrics),
	}
}

// Name returns the service name
func (m *AIManager) Name() string {
	return "ai-manager"
}

// Dependencies returns the service dependencies
func (m *AIManager) Dependencies() []string {
	return []string{"config-manager", "logger", "metrics-collector"}
}

// OnStart initializes the AI manager
func (m *AIManager) OnStart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrLifecycleError("start", fmt.Errorf("AI manager already started"))
	}

	// Load configuration
	var config AIConfig
	if err := m.config.Bind("ai", &config); err != nil {
		return common.ErrConfigError("failed to load AI configuration", err)
	}

	// Initialize model server
	modelServer, err := models.NewModelServer(models.ModelServerConfig{
		MaxModels: config.ModelCacheSize,
		// Workers:   config.InferenceWorkers,
		Logger:    m.logger,
		Metrics:   m.metrics,
	})
	if err != nil {
		return common.ErrServiceStartFailed("ai-manager", err)
	}
	m.modelServer = modelServer

	// Initialize LLM manager
	llmManager, err := llm.NewLLMManager(llm.LLMManagerConfig{
		Logger:  m.logger,
		Metrics: m.metrics,
	})
	if err != nil {
		return common.ErrServiceStartFailed("ai-manager", err)
	}
	m.llmManager = llmManager

	// Initialize inference engine
	inferenceEngine, err := inference.NewInferenceEngine(inference.InferenceConfig{
		Workers:   config.InferenceWorkers,
		BatchSize: 10,
		CacheSize: 1000,
		Logger:    m.logger,
		Metrics:   m.metrics,
	})
	if err != nil {
		return common.ErrServiceStartFailed("ai-manager", err)
	}
	m.inferenceEngine = inferenceEngine

	// OnStart coordinator if enabled
	if config.EnableCoordination {
		if err := m.coordinator.Start(ctx); err != nil {
			return common.ErrServiceStartFailed("ai-manager", err)
		}
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("AI manager started successfully",
			logger.Int("max_agents", config.MaxAgents),
			logger.Int("inference_workers", config.InferenceWorkers),
			logger.Bool("coordination_enabled", config.EnableCoordination),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.manager_started").Inc()
	}

	return nil
}

// OnStop shuts down the AI manager
func (m *AIManager) OnStop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("AI manager not started"))
	}

	// OnStop all agents
	for _, agent := range m.agents {
		if err := agent.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to stop agent",
					logger.String("agent_id", agent.ID()),
					logger.Error(err),
				)
			}
		}
	}

	// OnStop coordinator
	if err := m.coordinator.Stop(ctx); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to stop coordinator", logger.Error(err))
		}
	}

	// OnStop inference engine
	if m.inferenceEngine != nil {
		if err := m.inferenceEngine.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to stop inference engine", logger.Error(err))
			}
		}
	}

	// OnStop model server
	if m.modelServer != nil {
		if err := m.modelServer.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to stop model server", logger.Error(err))
			}
		}
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("AI manager stopped successfully")
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.manager_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs health checks on the AI system
func (m *AIManager) OnHealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return common.ErrHealthCheckFailed("ai-manager", fmt.Errorf("AI manager not started"))
	}

	// Check model server health
	if m.modelServer != nil {
		if health := m.modelServer.GetHealth(); health.Status != models.ModelHealthStatusHealthy {
			return common.ErrHealthCheckFailed("model-server", fmt.Errorf("model server unhealthy: %s", health.Message))
		}
	}

	// Check inference engine health
	if m.inferenceEngine != nil {
		if health := m.inferenceEngine.GetHealth(); health.Status != inference.InferenceHealthStatusHealthy {
			return common.ErrHealthCheckFailed("inference-engine", fmt.Errorf("inference engine unhealthy: %s", health.Message))
		}
	}

	// Check agent health
	unhealthyAgents := 0
	for _, agent := range m.agents {
		if health := agent.GetHealth(); health.Status != AgentHealthStatusHealthy {
			unhealthyAgents++
		}
	}

	if unhealthyAgents > len(m.agents)/2 {
		return common.ErrHealthCheckFailed("ai-agents", fmt.Errorf("too many unhealthy agents: %d/%d", unhealthyAgents, len(m.agents)))
	}

	return nil
}

// RegisterAgent registers a new AI agent
func (m *AIManager) RegisterAgent(agent AIAgent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return common.ErrLifecycleError("register_agent", fmt.Errorf("AI manager not started"))
	}

	agentID := agent.ID()
	if _, exists := m.agents[agentID]; exists {
		return common.ErrServiceAlreadyExists(agentID)
	}

	// Initialize agent
	if err := agent.Initialize(context.Background(), AgentConfig{
		Coordinator: m.coordinator,
		Logger:      m.logger,
		Metrics:     m.metrics,
	}); err != nil {
		return common.ErrServiceStartFailed(agentID, err)
	}

	// OnStart agent
	if err := agent.Start(context.Background()); err != nil {
		return common.ErrServiceStartFailed(agentID, err)
	}

	m.agents[agentID] = agent

	// Register with coordinator
	if m.coordinator != nil {
		if err := m.coordinator.RegisterAgent(agent); err != nil {
			if m.logger != nil {
				m.logger.Warn("failed to register agent with coordinator",
					logger.String("agent_id", agentID),
					logger.Error(err),
				)
			}
		}
	}

	if m.logger != nil {
		m.logger.Info("AI agent registered successfully",
			logger.String("agent_id", agentID),
			logger.String("agent_name", agent.Name()),
			logger.String("agent_type", string(agent.Type())),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.agents_registered").Inc()
		m.metrics.Gauge("forge.ai.active_agents").Set(float64(len(m.agents)))
	}

	return nil
}

// UnregisterAgent removes an AI agent
func (m *AIManager) UnregisterAgent(agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return common.ErrServiceNotFound(agentID)
	}

	// OnStop agent
	if err := agent.Stop(context.Background()); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to stop agent during unregistration",
				logger.String("agent_id", agentID),
				logger.Error(err),
			)
		}
	}

	// Unregister from coordinator
	if m.coordinator != nil {
		if err := m.coordinator.UnregisterAgent(agentID); err != nil {
			if m.logger != nil {
				m.logger.Warn("failed to unregister agent from coordinator",
					logger.String("agent_id", agentID),
					logger.Error(err),
				)
			}
		}
	}

	delete(m.agents, agentID)

	if m.logger != nil {
		m.logger.Info("AI agent unregistered successfully",
			logger.String("agent_id", agentID),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.agents_unregistered").Inc()
		m.metrics.Gauge("forge.ai.active_agents").Set(float64(len(m.agents)))
	}

	return nil
}

// GetAgent retrieves an AI agent by ID
func (m *AIManager) GetAgent(agentID string) (AIAgent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return nil, common.ErrServiceNotFound(agentID)
	}

	return agent, nil
}

// GetAgents returns all registered AI agents
func (m *AIManager) GetAgents() []AIAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]AIAgent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}

	return agents
}

// CreateInferenceEngine creates a new inference engine
func (m *AIManager) CreateInferenceEngine(config inference.InferenceConfig) (*inference.InferenceEngine, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil, common.ErrLifecycleError("create_inference_engine", fmt.Errorf("AI manager not started"))
	}

	return inference.NewInferenceEngine(config)
}

// GetModelServer returns the model server
func (m *AIManager) GetModelServer() models.ModelServer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.modelServer
}

// GetLLMManager returns the LLM manager
func (m *AIManager) GetLLMManager() *llm.LLMManager {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.llmManager
}

// GetInferenceEngine returns the inference engine
func (m *AIManager) GetInferenceEngine() *inference.InferenceEngine {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.inferenceEngine
}

// GetCoordinator returns the multi-agent coordinator
func (m *AIManager) GetCoordinator() *coordination.MultiAgentCoordinator {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.coordinator
}

// GetStats returns AI system statistics
func (m *AIManager) GetStats() AIStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	activeAgents := 0
	agentStats := make(map[string]AgentStats)
	for id, agent := range m.agents {
		stats := agent.GetStats()
		agentStats[id] = stats
		if stats.IsActive {
			activeAgents++
		}
	}

	var modelStats map[string]models.ModelStats
	var modelsLoaded int
	if m.modelServer != nil {
		modelStats = m.modelServer.GetStats().ModelStats
		modelsLoaded = len(modelStats)
	}

	var inferenceRequests, totalInferences int64
	var averageLatency time.Duration
	var errorRate float64
	if m.inferenceEngine != nil {
		stats := m.inferenceEngine.GetStats()
		inferenceRequests = stats.RequestsProcessed
		totalInferences = stats.TotalInferences
		averageLatency = stats.AverageLatency
		errorRate = stats.ErrorRate
	}

	return AIStats{
		TotalAgents:       len(m.agents),
		ActiveAgents:      activeAgents,
		ModelsLoaded:      modelsLoaded,
		InferenceRequests: inferenceRequests,
		TotalInferences:   totalInferences,
		AverageLatency:    averageLatency,
		ErrorRate:         errorRate,
		AgentStats:        agentStats,
		ModelStats:        modelStats,
		LastUpdated:       time.Now(),
	}
}

// ProcessAgentRequest processes a request through the agent system
func (m *AIManager) ProcessAgentRequest(ctx context.Context, request AgentRequest) (*AgentResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil, common.ErrLifecycleError("process_request", fmt.Errorf("AI manager not started"))
	}

	// Find the best agent for this request
	agent, err := m.findBestAgent(request)
	if err != nil {
		return nil, err
	}

	// Process the request through the agent
	input := AgentInput{
		Type:      request.Type,
		Data:      request.Data,
		Context:   request.Context,
		Timestamp: time.Now(),
		RequestID: request.ID,
	}

	output, err := agent.Process(ctx, input)
	if err != nil {
		return nil, err
	}

	return &AgentResponse{
		ID:          request.ID,
		AgentID:     agent.ID(),
		Output:      output,
		ProcessedAt: time.Now(),
	}, nil
}

// findBestAgent finds the best agent to handle a request
func (m *AIManager) findBestAgent(request AgentRequest) (AIAgent, error) {
	var bestAgent AIAgent
	var bestScore float64

	for _, agent := range m.agents {
		// Check if agent can handle this request type
		canHandle := false
		for _, capability := range agent.Capabilities() {
			if capability.Name == request.Type {
				canHandle = true
				break
			}
		}

		if !canHandle {
			continue
		}

		// Calculate agent score based on health and performance
		health := agent.GetHealth()
		stats := agent.GetStats()

		if health.Status != AgentHealthStatusHealthy {
			continue
		}

		// Simple scoring: higher is better
		score := 1.0
		if stats.AverageLatency > 0 {
			score = 1.0 / stats.AverageLatency.Seconds()
		}
		if stats.ErrorRate > 0 {
			score *= (1.0 - stats.ErrorRate)
		}

		if bestAgent == nil || score > bestScore {
			bestAgent = agent
			bestScore = score
		}
	}

	if bestAgent == nil {
		return nil, common.ErrServiceNotFound(fmt.Sprintf("no agent available for request type: %s", request.Type))
	}

	return bestAgent, nil
}

// AgentRequest represents a request to the agent system
type AgentRequest struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Data    interface{}            `json:"data"`
	Context map[string]interface{} `json:"context"`
}

// AgentResponse represents a response from the agent system
type AgentResponse struct {
	ID          string      `json:"id"`
	AgentID     string      `json:"agent_id"`
	Output      AgentOutput `json:"output"`
	ProcessedAt time.Time   `json:"processed_at"`
}
