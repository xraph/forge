package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/ai/internal"
	"github.com/xraph/forge/v2/extensions/ai/llm"
)

// AgentFactory creates agents dynamically from templates
type AgentFactory struct {
	llm       *llm.LLMManager
	templates map[string]AgentTemplate
	logger    forge.Logger
	mu        sync.RWMutex
}

// AgentTemplate defines a template for agent creation
type AgentTemplate struct {
	Type         string                 `json:"type"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	SystemPrompt string                 `json:"system_prompt"`
	Model        string                 `json:"model"`
	Provider     string                 `json:"provider,omitempty"`
	Temperature  *float64               `json:"temperature,omitempty"`
	MaxTokens    *int                   `json:"max_tokens,omitempty"`
	Tools        []llm.Tool             `json:"tools,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty"`
}

// NewAgentFactory creates a new agent factory
func NewAgentFactory(llmManager *llm.LLMManager, logger forge.Logger) *AgentFactory {
	return &AgentFactory{
		llm:       llmManager,
		templates: make(map[string]AgentTemplate),
		logger:    logger,
	}
}

// RegisterTemplate registers a new agent template
func (f *AgentFactory) RegisterTemplate(name string, template AgentTemplate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.templates[name] = template
}

// GetTemplate retrieves a template by name
func (f *AgentFactory) GetTemplate(name string) (AgentTemplate, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	template, exists := f.templates[name]
	return template, exists
}

// ListTemplates returns all registered templates
func (f *AgentFactory) ListTemplates() []AgentTemplate {
	f.mu.RLock()
	defer f.mu.RUnlock()

	templates := make([]AgentTemplate, 0, len(f.templates))
	for _, template := range f.templates {
		templates = append(templates, template)
	}
	return templates
}

// CreateAgent creates an agent from a template
func (f *AgentFactory) CreateAgent(template AgentTemplate, config map[string]interface{}) (internal.AIAgent, error) {
	// Generate unique ID
	id := uuid.New().String()

	// Merge template config with provided config
	mergedConfig := make(map[string]interface{})
	for k, v := range template.Config {
		mergedConfig[k] = v
	}
	for k, v := range config {
		mergedConfig[k] = v
	}

	// Create LLM-based agent
	agent := &LLMAgent{
		id:           id,
		name:         template.Name,
		agentType:    template.Type,
		llm:          f.llm,
		systemPrompt: template.SystemPrompt,
		model:        template.Model,
		provider:     template.Provider,
		temperature:  template.Temperature,
		maxTokens:    template.MaxTokens,
		tools:        template.Tools,
		config:       mergedConfig,
		logger:       f.logger,
	}

	if f.logger != nil {
		f.logger.Info("agent created from template",
			forge.F("agent_id", id),
			forge.F("type", template.Type),
			forge.F("name", template.Name),
		)
	}

	return agent, nil
}

// CreateAgentFromDefinition creates an agent from an AgentDefinition
func (f *AgentFactory) CreateAgentFromDefinition(def *AgentDefinition) (internal.AIAgent, error) {
	template := AgentTemplate{
		Type:         def.Type,
		Name:         def.Name,
		SystemPrompt: def.SystemPrompt,
		Model:        def.Model,
		Provider:     def.Provider,
		Temperature:  def.Temperature,
		MaxTokens:    def.MaxTokens,
		Tools:        def.Tools,
		Config:       def.Config,
	}

	agent := &LLMAgent{
		id:           def.ID,
		name:         def.Name,
		agentType:    def.Type,
		llm:          f.llm,
		systemPrompt: def.SystemPrompt,
		model:        def.Model,
		provider:     def.Provider,
		temperature:  def.Temperature,
		maxTokens:    def.MaxTokens,
		tools:        def.Tools,
		config:       def.Config,
		logger:       f.logger,
	}

	return agent, nil
}

// LLMAgent is a dynamically created agent powered by LLMs
type LLMAgent struct {
	id           string
	name         string
	agentType    string
	llm          *llm.LLMManager
	systemPrompt string
	model        string
	provider     string
	temperature  *float64
	maxTokens    *int
	tools        []llm.Tool
	config       map[string]interface{}
	logger       forge.Logger
	session      *llm.ChatSession
	mu           sync.RWMutex
}

// ID returns the agent ID
func (a *LLMAgent) ID() string {
	return a.id
}

// Name returns the agent name
func (a *LLMAgent) Name() string {
	return a.name
}

// Type returns the agent type
func (a *LLMAgent) Type() internal.AgentType {
	return internal.AgentType(a.agentType)
}

// Capabilities returns the agent capabilities
func (a *LLMAgent) Capabilities() []internal.Capability {
	return []internal.Capability{
		{
			Name:        "chat",
			Description: "Engage in conversational interaction",
		},
		{
			Name:        "analyze",
			Description: "Analyze and process information",
		},
	}
}

// Initialize initializes the agent
func (a *LLMAgent) Initialize(ctx context.Context, config internal.AgentConfig) error {
	// Initialize session
	a.mu.Lock()
	defer a.mu.Unlock()

	provider := a.provider
	if provider == "" {
		provider = "openai" // Default
	}

	a.session = llm.NewChatSession(a.id, a.model, provider)
	if a.systemPrompt != "" {
		a.session.SetSystemPrompt(a.systemPrompt)
	}

	if a.logger != nil {
		a.logger.Info("agent initialized", forge.F("agent_id", a.id))
	}

	return nil
}

// Start starts the agent
func (a *LLMAgent) Start(ctx context.Context) error {
	if a.logger != nil {
		a.logger.Info("agent started", forge.F("agent_id", a.id))
	}
	return nil
}

// Stop stops the agent
func (a *LLMAgent) Stop(ctx context.Context) error {
	if a.logger != nil {
		a.logger.Info("agent stopped", forge.F("agent_id", a.id))
	}
	return nil
}

// Process processes input and returns output
func (a *LLMAgent) Process(ctx context.Context, input internal.AgentInput) (internal.AgentOutput, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Initialize session if not exists
	if a.session == nil {
		provider := a.provider
		if provider == "" {
			provider = "openai"
		}
		a.session = llm.NewChatSession(a.id, a.model, provider)
		if a.systemPrompt != "" {
			a.session.SetSystemPrompt(a.systemPrompt)
		}
	}

	// Add user message
	userContent := fmt.Sprintf("%v", input.Data)
	a.session.AddMessage(llm.ChatMessage{
		Role:    "user",
		Content: userContent,
	})

	// Build request
	request := a.session.ToRequest()
	if a.temperature != nil {
		request.Temperature = a.temperature
	}
	if a.maxTokens != nil {
		request.MaxTokens = a.maxTokens
	}
	if len(a.tools) > 0 {
		request.Tools = a.tools
	}

	// Execute chat
	response, err := a.llm.Chat(ctx, request)
	if err != nil {
		return internal.AgentOutput{}, fmt.Errorf("failed to execute chat: %w", err)
	}

	// Add assistant response to session
	if len(response.Choices) > 0 {
		a.session.AddMessage(response.Choices[0].Message)
	}

	// Build output
	output := internal.AgentOutput{
		Type:       "llm_response",
		Confidence: 0.8, // Default confidence
		Metadata:   make(map[string]interface{}),
	}

	if len(response.Choices) > 0 {
		output.Data = response.Choices[0].Message.Content
		output.Explanation = fmt.Sprintf("Generated by %s using model %s", a.provider, a.model)
	}

	return output, nil
}

// Learn learns from feedback
func (a *LLMAgent) Learn(ctx context.Context, feedback internal.AgentFeedback) error {
	// For LLM agents, learning is handled through conversation context
	// More sophisticated learning could be implemented here
	if a.logger != nil {
		a.logger.Info("agent received feedback",
			forge.F("agent_id", a.id),
			forge.F("success", feedback.Success),
		)
	}
	return nil
}

// GetStats returns agent statistics
func (a *LLMAgent) GetStats() internal.AgentStats {
	return internal.AgentStats{
		TotalProcessed: 0, // Would need to track this
		TotalErrors:    0,
		ErrorRate:      0.0,
		IsActive:       true,
		Confidence:     0.8,
	}
}

// GetHealth returns agent health status
func (a *LLMAgent) GetHealth() internal.AgentHealth {
	now := time.Now()
	return internal.AgentHealth{
		Status:      internal.AgentHealthStatusHealthy,
		Message:     "Agent is operational",
		Details:     make(map[string]interface{}),
		CheckedAt:   now,
		LastHealthy: now,
	}
}

