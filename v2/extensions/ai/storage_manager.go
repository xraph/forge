package ai

import (
	"context"
	"fmt"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/ai/internal"
)

// CreateAgent creates and persists an agent from a definition
func (m *managerImpl) CreateAgent(ctx context.Context, def *AgentDefinition) (internal.AIAgent, error) {
	// Create runtime agent using factory
	if m.agentFactory == nil {
		return nil, fmt.Errorf("agent factory not configured")
	}

	agent, err := m.agentFactory.CreateAgentFromDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Persist to store (if available)
	if m.store != nil {
		if err := m.store.Create(ctx, def); err != nil {
			return nil, fmt.Errorf("failed to persist agent: %w", err)
		}
	}

	// Register in memory
	if err := m.RegisterAgent(agent); err != nil {
		// If registration fails and we persisted, try to clean up
		if m.store != nil {
			m.store.Delete(ctx, def.ID)
		}
		return nil, err
	}

	// Initialize and start agent
	if err := agent.Initialize(ctx, internal.AgentConfig{
		ID:      def.ID,
		Name:    def.Name,
		Logger:  m.logger,
		Metrics: m.metrics,
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	if err := agent.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}

	if m.logger != nil {
		m.logger.Info("agent created and registered",
			forge.F("agent_id", def.ID),
			forge.F("agent_name", def.Name),
			forge.F("agent_type", def.Type),
			forge.F("persisted", m.store != nil),
		)
	}

	return agent, nil
}

// LoadAgent loads an agent from the store
func (m *managerImpl) LoadAgent(ctx context.Context, id string) (internal.AIAgent, error) {
	// Check if already loaded
	agent, err := m.GetAgent(id)
	if err == nil {
		return agent, nil
	}

	// Load from store
	if m.store == nil {
		return nil, fmt.Errorf("agent %s not found (no store configured)", id)
	}

	def, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to load agent: %w", err)
	}

	// Create runtime agent from definition
	return m.CreateAgent(ctx, def)
}

// UpdateAgent updates an agent in storage and runtime
func (m *managerImpl) UpdateAgent(ctx context.Context, id string, updates *AgentDefinition) error {
	// Update in store
	if m.store != nil {
		if err := m.store.Update(ctx, updates); err != nil {
			return fmt.Errorf("failed to update agent in store: %w", err)
		}
	}

	// Update in memory (if loaded)
	m.agentsMu.Lock()
	defer m.agentsMu.Unlock()

	if agent, exists := m.agents[id]; exists {
		// Stop old agent
		if err := agent.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.Warn("failed to stop old agent", forge.F("agent_id", id), forge.F("error", err.Error()))
			}
		}

		// Unregister
		delete(m.agents, id)

		// Create and register new agent with updated config
		newAgent, err := m.CreateAgent(ctx, updates)
		if err != nil {
			return fmt.Errorf("failed to recreate agent: %w", err)
		}

		m.agents[id] = newAgent

		if m.logger != nil {
			m.logger.Info("agent updated", forge.F("agent_id", id))
		}
	}

	return nil
}

// DeleteAgent deletes an agent from storage and runtime
func (m *managerImpl) DeleteAgent(ctx context.Context, id string) error {
	// Delete from store
	if m.store != nil {
		if err := m.store.Delete(ctx, id); err != nil {
			return fmt.Errorf("failed to delete agent from store: %w", err)
		}
	}

	// Delete from memory
	m.agentsMu.Lock()
	defer m.agentsMu.Unlock()

	if agent, exists := m.agents[id]; exists {
		// Stop agent
		if err := agent.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.Warn("failed to stop agent", forge.F("agent_id", id), forge.F("error", err.Error()))
			}
		}
		delete(m.agents, id)
	}

	if m.logger != nil {
		m.logger.Info("agent deleted", forge.F("agent_id", id))
	}

	return nil
}

// ListAgentsWithFilter lists agents from storage with optional filters
func (m *managerImpl) ListAgentsWithFilter(ctx context.Context, filter AgentFilter) ([]*AgentDefinition, error) {
	if m.store != nil {
		// Load from store
		return m.store.List(ctx, filter)
	}

	// Return in-memory agents
	m.agentsMu.RLock()
	defer m.agentsMu.RUnlock()

	agents := make([]*AgentDefinition, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, &AgentDefinition{
			ID:   agent.ID(),
			Name: agent.Name(),
			Type: string(agent.Type()),
		})
	}

	return agents, nil
}

// GetExecutionHistory retrieves execution history for an agent
func (m *managerImpl) GetExecutionHistory(ctx context.Context, agentID string, limit int) ([]*AgentExecution, error) {
	if m.store == nil {
		return []*AgentExecution{}, nil
	}

	return m.store.GetExecutionHistory(ctx, agentID, limit)
}

// RegisterTeam registers a new agent team
func (m *managerImpl) RegisterTeam(team *AgentTeam) error {
	m.teamsMu.Lock()
	defer m.teamsMu.Unlock()

	if _, exists := m.teams[team.ID()]; exists {
		return fmt.Errorf("team %s already registered", team.ID())
	}

	m.teams[team.ID()] = team

	if m.logger != nil {
		m.logger.Info("team registered", forge.F("team_id", team.ID()), forge.F("team_name", team.Name()))
	}

	return nil
}

// GetTeam retrieves a team by ID
func (m *managerImpl) GetTeam(id string) (*AgentTeam, error) {
	m.teamsMu.RLock()
	defer m.teamsMu.RUnlock()

	team, exists := m.teams[id]
	if !exists {
		return nil, fmt.Errorf("team %s not found", id)
	}

	return team, nil
}

// ListTeams returns all registered teams
func (m *managerImpl) ListTeams() []*AgentTeam {
	m.teamsMu.RLock()
	defer m.teamsMu.RUnlock()

	teams := make([]*AgentTeam, 0, len(m.teams))
	for _, team := range m.teams {
		teams = append(teams, team)
	}

	return teams
}

// DeleteTeam unregisters a team
func (m *managerImpl) DeleteTeam(id string) error {
	m.teamsMu.Lock()
	defer m.teamsMu.Unlock()

	if _, exists := m.teams[id]; !exists {
		return fmt.Errorf("team %s not found", id)
	}

	delete(m.teams, id)

	if m.logger != nil {
		m.logger.Info("team deleted", forge.F("team_id", id))
	}

	return nil
}
