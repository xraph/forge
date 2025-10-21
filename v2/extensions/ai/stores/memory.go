package stores

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v2/extensions/ai"
)

// MemoryAgentStore provides in-memory storage for agents (no database required)
type MemoryAgentStore struct {
	agents     map[string]*ai.AgentDefinition
	executions map[string][]*ai.AgentExecution
	mu         sync.RWMutex
}

// NewMemoryAgentStore creates a new in-memory agent store
func NewMemoryAgentStore() *MemoryAgentStore {
	return &MemoryAgentStore{
		agents:     make(map[string]*ai.AgentDefinition),
		executions: make(map[string][]*ai.AgentExecution),
	}
}

// Create creates a new agent
func (s *MemoryAgentStore) Create(ctx context.Context, agent *ai.AgentDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.agents[agent.ID]; exists {
		return fmt.Errorf("agent %s already exists", agent.ID)
	}

	// Set timestamps
	now := time.Now()
	agent.CreatedAt = now
	agent.UpdatedAt = now

	s.agents[agent.ID] = agent
	return nil
}

// Get retrieves an agent by ID
func (s *MemoryAgentStore) Get(ctx context.Context, id string) (*ai.AgentDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, exists := s.agents[id]
	if !exists {
		return nil, fmt.Errorf("agent %s not found", id)
	}

	return agent, nil
}

// Update updates an existing agent
func (s *MemoryAgentStore) Update(ctx context.Context, agent *ai.AgentDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.agents[agent.ID]; !exists {
		return fmt.Errorf("agent %s not found", agent.ID)
	}

	agent.UpdatedAt = time.Now()
	s.agents[agent.ID] = agent
	return nil
}

// Delete deletes an agent by ID
func (s *MemoryAgentStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.agents[id]; !exists {
		return fmt.Errorf("agent %s not found", id)
	}

	delete(s.agents, id)
	delete(s.executions, id) // Also delete execution history
	return nil
}

// List lists agents with optional filters
func (s *MemoryAgentStore) List(ctx context.Context, filter ai.AgentFilter) ([]*ai.AgentDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]*ai.AgentDefinition, 0)

	for _, agent := range s.agents {
		// Apply filters
		if filter.Type != "" && agent.Type != filter.Type {
			continue
		}

		if filter.CreatedBy != "" && agent.CreatedBy != filter.CreatedBy {
			continue
		}

		if len(filter.Tags) > 0 {
			hasTag := false
			for _, filterTag := range filter.Tags {
				for _, agentTag := range agent.Tags {
					if filterTag == agentTag {
						hasTag = true
						break
					}
				}
				if hasTag {
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		agents = append(agents, agent)
	}

	// Apply pagination
	if filter.Offset > 0 && filter.Offset < len(agents) {
		agents = agents[filter.Offset:]
	}

	if filter.Limit > 0 && filter.Limit < len(agents) {
		agents = agents[:filter.Limit]
	}

	return agents, nil
}

// GetExecutionHistory retrieves execution history for an agent
func (s *MemoryAgentStore) GetExecutionHistory(ctx context.Context, agentID string, limit int) ([]*ai.AgentExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	executions, exists := s.executions[agentID]
	if !exists {
		return []*ai.AgentExecution{}, nil
	}

	// Return most recent executions
	if limit > 0 && limit < len(executions) {
		// Return last N executions
		start := len(executions) - limit
		return executions[start:], nil
	}

	return executions, nil
}

// RecordExecution records an agent execution (helper method)
func (s *MemoryAgentStore) RecordExecution(ctx context.Context, execution *ai.AgentExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.executions[execution.AgentID] = append(s.executions[execution.AgentID], execution)
	return nil
}

