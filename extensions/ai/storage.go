package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	aisdk "github.com/xraph/ai-sdk"
	"github.com/xraph/forge"
)

// AgentDefinition simplified for REST API.
type AgentDefinition struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Model       string                 `json:"model"`
	Temperature float64                `json:"temperature"`
	Config      map[string]interface{} `json:"config"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// AgentManager wraps ai-sdk for REST API management.
type AgentManager struct {
	factory    *AgentFactory
	agents     map[string]*aisdk.Agent
	definitions map[string]*AgentDefinition
	stateStore aisdk.StateStore
	logger     forge.Logger
	metrics    forge.Metrics
	mu         sync.RWMutex
}

// NewAgentManager creates a new agent manager.
func NewAgentManager(factory *AgentFactory, stateStore aisdk.StateStore, logger forge.Logger, metrics forge.Metrics) *AgentManager {
	return &AgentManager{
		factory:     factory,
		agents:      make(map[string]*aisdk.Agent),
		definitions: make(map[string]*AgentDefinition),
		stateStore:  stateStore,
		logger:      logger,
		metrics:     metrics,
	}
}

// CreateAgent creates a new agent from a definition.
func (m *AgentManager) CreateAgent(ctx context.Context, def *AgentDefinition) (*aisdk.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.agents[def.ID]; exists {
		return nil, fmt.Errorf("agent %s already exists", def.ID)
	}

	config := map[string]any{
		"id":          def.ID,
		"name":        def.Name,
		"model":       def.Model,
		"temperature": def.Temperature,
	}

	for k, v := range def.Config {
		config[k] = v
	}

	agent, err := m.factory.CreateAgent(def.Type, config)
	if err != nil {
		return nil, err
	}

	m.agents[def.ID] = agent
	m.definitions[def.ID] = def

	if m.logger != nil {
		m.logger.Info("agent created",
			forge.F("agent_id", def.ID),
			forge.F("type", def.Type),
			forge.F("name", def.Name))
	}

	return agent, nil
}

// GetAgent retrieves an agent by ID.
func (m *AgentManager) GetAgent(ctx context.Context, id string) (*aisdk.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, exists := m.agents[id]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", id)
	}

	return agent, nil
}

// GetDefinition retrieves an agent definition by ID.
func (m *AgentManager) GetDefinition(ctx context.Context, id string) (*AgentDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	def, exists := m.definitions[id]
	if !exists {
		return nil, fmt.Errorf("agent definition not found: %s", id)
	}

	return def, nil
}

// UpdateAgent updates an agent's configuration.
func (m *AgentManager) UpdateAgent(ctx context.Context, id string, updates *AgentDefinition) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	def, exists := m.definitions[id]
	if !exists {
		return fmt.Errorf("agent not found: %s", id)
	}

	// Update definition
	if updates.Name != "" {
		def.Name = updates.Name
	}
	if updates.Model != "" {
		def.Model = updates.Model
	}
	if updates.Temperature > 0 {
		def.Temperature = updates.Temperature
	}
	if updates.Config != nil {
		for k, v := range updates.Config {
			def.Config[k] = v
		}
	}
	def.UpdatedAt = time.Now()

	// Recreate agent with new config
	config := map[string]any{
		"id":          def.ID,
		"name":        def.Name,
		"model":       def.Model,
		"temperature": def.Temperature,
	}
	for k, v := range def.Config {
		config[k] = v
	}

	agent, err := m.factory.CreateAgent(def.Type, config)
	if err != nil {
		return err
	}

	m.agents[id] = agent

	if m.logger != nil {
		m.logger.Info("agent updated", forge.F("agent_id", id))
	}

	return nil
}

// DeleteAgent deletes an agent.
func (m *AgentManager) DeleteAgent(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.agents[id]; !exists {
		return fmt.Errorf("agent not found: %s", id)
	}

	def, hasDefinition := m.definitions[id]

	delete(m.agents, id)
	delete(m.definitions, id)

	// Also delete state if available
	if m.stateStore != nil && hasDefinition {
		_ = m.stateStore.Delete(ctx, id, def.Model) // Ignore error if state doesn't exist
	}

	if m.logger != nil {
		m.logger.Info("agent deleted", forge.F("agent_id", id))
	}

	return nil
}

// ListAgents lists all agents.
func (m *AgentManager) ListAgents(ctx context.Context) ([]*AgentDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	defs := make([]*AgentDefinition, 0, len(m.definitions))
	for _, def := range m.definitions {
		defs = append(defs, def)
	}

	return defs, nil
}
