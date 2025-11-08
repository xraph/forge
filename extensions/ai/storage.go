package ai

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

// AgentStore defines the interface for agent persistence
// Users can implement this with ANY storage backend.
type AgentStore interface {
	// Create agent
	Create(ctx context.Context, agent *AgentDefinition) error

	// Get agent by ID
	Get(ctx context.Context, id string) (*AgentDefinition, error)

	// Update agent
	Update(ctx context.Context, agent *AgentDefinition) error

	// Delete agent
	Delete(ctx context.Context, id string) error

	// List agents with optional filters
	List(ctx context.Context, filter AgentFilter) ([]*AgentDefinition, error)

	// Get agent execution history (optional)
	GetExecutionHistory(ctx context.Context, agentID string, limit int) ([]*AgentExecution, error)
}

// AgentDefinition is a simple, storage-agnostic struct.
type AgentDefinition struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	SystemPrompt string         `json:"system_prompt"`
	Model        string         `json:"model"`
	Provider     string         `json:"provider"`
	Temperature  *float64       `json:"temperature,omitempty"`
	MaxTokens    *int           `json:"max_tokens,omitempty"`
	Tools        []llm.Tool     `json:"tools,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	CreatedBy    string         `json:"created_by,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
}

// AgentFilter for querying agents.
type AgentFilter struct {
	Type      string   `json:"type,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedBy string   `json:"created_by,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Offset    int      `json:"offset,omitempty"`
}

// AgentExecution represents execution history.
type AgentExecution struct {
	ID        string        `json:"id"`
	AgentID   string        `json:"agent_id"`
	Input     AgentInput    `json:"input"`
	Output    AgentOutput   `json:"output"`
	Status    string        `json:"status"` // success, error
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// AgentInfo provides basic agent information.
type AgentInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}
