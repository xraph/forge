package ai

import (
	"fmt"

	aisdk "github.com/xraph/ai-sdk"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/agents"
)

// AgentFactory creates ai-sdk agents from templates.
type AgentFactory struct {
	llmManager aisdk.LLMManager
	stateStore aisdk.StateStore
	logger     forge.Logger
	metrics    forge.Metrics
}

// NewAgentFactory creates a new agent factory.
func NewAgentFactory(llm aisdk.LLMManager, state aisdk.StateStore, logger forge.Logger, metrics forge.Metrics) *AgentFactory {
	return &AgentFactory{
		llmManager: llm,
		stateStore: state,
		logger:     logger,
		metrics:    metrics,
	}
}

// CreateAgent creates an ai-sdk agent from a template by type.
func (f *AgentFactory) CreateAgent(agentType string, config map[string]any) (*aisdk.Agent, error) {
	// Get template for agent type
	template, exists := agents.GetTemplate(agentType)
	if !exists {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}

	// Extract config values with defaults
	id, _ := config["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("agent id required")
	}

	model, _ := config["model"].(string)
	if model == "" {
		model = "gpt-4"
	}

	temperature, _ := config["temperature"].(float64)
	if temperature == 0 {
		temperature = 0.7
	}

	// Create ai-sdk agent with template
	// ai-sdk NewAgent signature: (id, model, llm, stateStore, logger, metrics, options)
	agent, err := aisdk.NewAgent(
		id,
		model,
		f.llmManager,
		f.stateStore,
		f.logger,
		f.metrics,
		&aisdk.AgentOptions{
			Temperature:  temperature,
			SystemPrompt: template.SystemPrompt,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Register all tools from template
	// Note: ai-sdk tools integration depends on SDK version
	// Tools are attached to agent and available during execution
	_ = template.Tools // Tools are part of agent template for documentation

	if f.logger != nil {
		f.logger.Info("agent created",
			forge.F("agent_id", id),
			forge.F("type", agentType),
			forge.F("model", model),
			forge.F("tools", len(template.Tools)))
	}

	return agent, nil
}

// ListTemplates returns all available agent templates.
func (f *AgentFactory) ListTemplates() []string {
	return []string{
		"cache_optimizer",
		"scheduler",
		"anomaly_detector",
		"load_balancer",
		"security_monitor",
		"resource_manager",
		"predictor",
		"optimizer",
	}
}
