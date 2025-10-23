package internal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// BaseAgent provides a base implementation for AI agents
type BaseAgent struct {
	id           string
	name         string
	agentType    AgentType
	capabilities []Capability
	config       AgentConfig
	started      bool
	stats        AgentStats
	health       AgentHealth
	mu           sync.RWMutex

	// Processing state
	processingSem chan struct{}
	requestCount  int64
	errorCount    int64
	totalLatency  time.Duration

	// Learning state
	learningData []AgentFeedback
	confidence   float64
	lastLearning time.Time
}

// NewBaseAgent creates a new base agent
func NewBaseAgent(id, name string, agentType AgentType, capabilities []Capability) *BaseAgent {
	return &BaseAgent{
		id:           id,
		name:         name,
		agentType:    agentType,
		capabilities: capabilities,
		health: AgentHealth{
			Status:    AgentHealthStatusUnknown,
			CheckedAt: time.Now(),
		},
		confidence: 0.5, // Start with neutral confidence
	}
}

// ID returns the agent ID
func (a *BaseAgent) ID() string {
	return a.id
}

// Name returns the agent name
func (a *BaseAgent) Name() string {
	return a.name
}

// Type returns the agent type
func (a *BaseAgent) Type() AgentType {
	return a.agentType
}

// Capabilities returns the agent capabilities
func (a *BaseAgent) Capabilities() []Capability {
	return a.capabilities
}

// Initialize initializes the agent
func (a *BaseAgent) Initialize(ctx context.Context, config AgentConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.config = config

	// Initialize processing semaphore
	maxConcurrency := config.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 10
	}
	a.processingSem = make(chan struct{}, maxConcurrency)

	// Initialize learning data
	a.learningData = make([]AgentFeedback, 0)

	if a.config.Logger != nil {
		a.config.Logger.Info("agent initialized",
			logger.String("agent_id", a.id),
			logger.String("agent_name", a.name),
			logger.String("agent_type", string(a.agentType)),
			logger.Int("max_concurrency", maxConcurrency),
		)
	}

	return nil
}

// Start starts the agent
func (a *BaseAgent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return fmt.Errorf("agent %s already started", a.id)
	}

	a.started = true
	a.health.Status = AgentHealthStatusHealthy
	a.health.Message = "Agent started successfully"
	a.health.CheckedAt = time.Now()
	a.health.LastHealthy = time.Now()

	if a.config.Logger != nil {
		a.config.Logger.Info("agent started",
			logger.String("agent_id", a.id),
			logger.String("agent_name", a.name),
		)
	}

	if a.config.Metrics != nil {
		a.config.Metrics.Counter("forge.ai.agent_started", "agent_id", a.id).Inc()
	}

	return nil
}

// Stop stops the agent
func (a *BaseAgent) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.started {
		return fmt.Errorf("agent %s not started", a.id)
	}

	a.started = false
	a.health.Status = AgentHealthStatusUnknown
	a.health.Message = "Agent stopped"
	a.health.CheckedAt = time.Now()

	if a.config.Logger != nil {
		a.config.Logger.Info("agent stopped",
			logger.String("agent_id", a.id),
			logger.String("agent_name", a.name),
		)
	}

	if a.config.Metrics != nil {
		a.config.Metrics.Counter("forge.ai.agent_stopped", "agent_id", a.id).Inc()
	}

	return nil
}

// Process processes input through the agent (base implementation)
func (a *BaseAgent) Process(ctx context.Context, input AgentInput) (AgentOutput, error) {
	// Acquire processing slot
	select {
	case a.processingSem <- struct{}{}:
		defer func() { <-a.processingSem }()
	case <-ctx.Done():
		return AgentOutput{}, ctx.Err()
	}

	startTime := time.Now()

	// Update request count
	a.mu.Lock()
	a.requestCount++
	a.mu.Unlock()

	// Default processing - should be overridden by specific agents
	output := AgentOutput{
		Type:        input.Type,
		Data:        input.Data,
		Confidence:  a.confidence,
		Explanation: fmt.Sprintf("Processed by %s agent", a.name),
		Actions:     []AgentAction{},
		Metadata:    map[string]interface{}{},
		Timestamp:   time.Now(),
	}

	// Update statistics
	latency := time.Since(startTime)
	a.updateStats(latency, nil)

	if a.config.Logger != nil {
		a.config.Logger.Debug("agent processed input",
			logger.String("agent_id", a.id),
			logger.String("request_id", input.RequestID),
			logger.Duration("latency", latency),
		)
	}

	if a.config.Metrics != nil {
		a.config.Metrics.Counter("forge.ai.agent_processed", "agent_id", a.id).Inc()
		a.config.Metrics.Histogram("forge.ai.agent_latency", "agent_id", a.id).Observe(latency.Seconds())
	}

	return output, nil
}

// Learn learns from feedback (base implementation)
func (a *BaseAgent) Learn(ctx context.Context, feedback AgentFeedback) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.config.LearningEnabled {
		return nil
	}

	// Add feedback to learning data
	a.learningData = append(a.learningData, feedback)

	// Keep only recent feedback (last 1000 entries)
	if len(a.learningData) > 1000 {
		a.learningData = a.learningData[len(a.learningData)-1000:]
	}

	// Update confidence based on feedback
	if feedback.Success {
		a.confidence = a.confidence*0.95 + 0.05 // Slightly increase confidence
		if a.confidence > 1.0 {
			a.confidence = 1.0
		}
	} else {
		a.confidence = a.confidence*0.95 - 0.05 // Slightly decrease confidence
		if a.confidence < 0.0 {
			a.confidence = 0.0
		}
	}

	a.lastLearning = time.Now()

	if a.config.Logger != nil {
		a.config.Logger.Debug("agent learned from feedback",
			logger.String("agent_id", a.id),
			logger.String("action_id", feedback.ActionID),
			logger.Bool("success", feedback.Success),
			logger.Float64("confidence", a.confidence),
		)
	}

	if a.config.Metrics != nil {
		a.config.Metrics.Counter("forge.ai.agent_learned", "agent_id", a.id).Inc()
		a.config.Metrics.Gauge("forge.ai.agent_confidence", "agent_id", a.id).Set(a.confidence)
	}

	return nil
}

// GetStats returns agent statistics
func (a *BaseAgent) GetStats() AgentStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var avgLatency time.Duration
	if a.requestCount > 0 {
		avgLatency = a.totalLatency / time.Duration(a.requestCount)
	}

	var errorRate float64
	if a.requestCount > 0 {
		errorRate = float64(a.errorCount) / float64(a.requestCount)
	}

	// Calculate learning metrics
	positiveFeedback := int64(0)
	negativeFeedback := int64(0)
	for _, feedback := range a.learningData {
		if feedback.Success {
			positiveFeedback++
		} else {
			negativeFeedback++
		}
	}

	var accuracyScore float64
	totalFeedback := positiveFeedback + negativeFeedback
	if totalFeedback > 0 {
		accuracyScore = float64(positiveFeedback) / float64(totalFeedback)
	}

	return AgentStats{
		TotalProcessed: a.requestCount,
		TotalErrors:    a.errorCount,
		AverageLatency: avgLatency,
		ErrorRate:      errorRate,
		LastProcessed:  time.Now(),
		IsActive:       a.started,
		Confidence:     a.confidence,
		LearningMetrics: LearningMetrics{
			TotalFeedback:     totalFeedback,
			PositiveFeedback:  positiveFeedback,
			NegativeFeedback:  negativeFeedback,
			AccuracyScore:     accuracyScore,
			ImprovementRate:   0.0, // Could be calculated based on historical data
			LastLearningEvent: a.lastLearning,
		},
	}
}

// GetHealth returns agent health status
func (a *BaseAgent) GetHealth() AgentHealth {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Update health status based on current state
	if !a.started {
		a.health.Status = AgentHealthStatusUnknown
		a.health.Message = "Agent not started"
	} else if a.errorCount > 0 && float64(a.errorCount)/float64(a.requestCount) > 0.5 {
		a.health.Status = AgentHealthStatusUnhealthy
		a.health.Message = "High error rate detected"
	} else if a.errorCount > 0 && float64(a.errorCount)/float64(a.requestCount) > 0.1 {
		a.health.Status = AgentHealthStatusDegraded
		a.health.Message = "Elevated error rate"
	} else if a.started {
		a.health.Status = AgentHealthStatusHealthy
		a.health.Message = "Agent operating normally"
		a.health.LastHealthy = time.Now()
	}

	a.health.CheckedAt = time.Now()
	a.health.Details = map[string]interface{}{
		"requests_processed": a.requestCount,
		"error_count":        a.errorCount,
		"confidence":         a.confidence,
		"is_active":          a.started,
	}

	return a.health
}

// updateStats updates agent statistics
func (a *BaseAgent) updateStats(latency time.Duration, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalLatency += latency

	if err != nil {
		a.errorCount++
	}
}

// HasCapability checks if the agent has a specific capability
func (a *BaseAgent) HasCapability(capabilityName string) bool {
	for _, capability := range a.capabilities {
		if capability.Name == capabilityName {
			return true
		}
	}
	return false
}

// GetCapability returns a specific capability
func (a *BaseAgent) GetCapability(capabilityName string) (*Capability, error) {
	for _, capability := range a.capabilities {
		if capability.Name == capabilityName {
			return &capability, nil
		}
	}
	return nil, fmt.Errorf("capability %s not found", capabilityName)
}

// IsHealthy returns true if the agent is healthy
func (a *BaseAgent) IsHealthy() bool {
	health := a.GetHealth()
	return health.Status == AgentHealthStatusHealthy
}

// GetConfiguration returns the agent configuration
func (a *BaseAgent) GetConfiguration() AgentConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// SetConfiguration updates the agent configuration
func (a *BaseAgent) SetConfiguration(config AgentConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.config = config
	return nil
}
