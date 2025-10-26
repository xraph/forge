package sdk

import (
	"context"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/shared"
)

// SDK version
const Version = "2.0.0"

// Generator provides the main entry point for AI operations
type Generator interface {
	// Generate performs simple text generation
	Generate(ctx context.Context) *GenerateBuilder

	// Stream performs streaming generation with reasoning steps
	Stream(ctx context.Context) *StreamBuilder

	// NewAgent creates a new agent
	NewAgent(name string) *AgentBuilder

	// NewWorkflow creates a new workflow
	NewWorkflow(name string) *WorkflowBuilder
}

// Note: GenerateObjectBuilder is a generic type and should be used directly:
//   sdk.NewGenerateObjectBuilder[YourType](ctx, llm, logger, metrics)

// Options configures the SDK
type Options struct {
	// LLM configuration
	DefaultProvider string
	DefaultModel    string
	APIKey          map[string]string // provider -> key

	// Observability
	Logger  forge.Logger
	Metrics shared.Metrics
	Tracer  Tracer

	// Storage
	StateStore  StateStore
	VectorStore VectorStore
	CacheStore  CacheStore

	// Limits
	DefaultTimeout time.Duration
	MaxRetries     int
	RateLimit      RateLimitConfig

	// Cost management
	CostManager CostManager

	// Safety
	Guardrails []Guardrail

	// Health
	HealthManager shared.HealthManager
}

// Tracer interface for distributed tracing
type Tracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// Span represents a trace span
type Span interface {
	End()
	SetAttribute(key string, value interface{})
	SetError(err error)
	Context() context.Context
}

// StateStore interface for agent state persistence
type StateStore interface {
	// Save saves the agent state
	Save(ctx context.Context, state *AgentState) error
	// Load loads the agent state
	Load(ctx context.Context, agentID, sessionID string) (*AgentState, error)
	// Delete deletes the agent state
	Delete(ctx context.Context, agentID, sessionID string) error
	// List lists all sessions for an agent
	List(ctx context.Context, agentID string) ([]string, error)
}

// VectorStore interface for embeddings and semantic search
type VectorStore interface {
	Upsert(ctx context.Context, vectors []Vector) error
	Query(ctx context.Context, vector []float64, limit int, filter map[string]interface{}) ([]VectorMatch, error)
	Delete(ctx context.Context, ids []string) error
}

// Vector represents a vector with metadata
type Vector struct {
	ID       string
	Values   []float64
	Metadata map[string]interface{}
}

// VectorMatch represents a search result
type VectorMatch struct {
	ID       string
	Score    float64
	Metadata map[string]interface{}
}

// CacheStore interface for caching
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error
}

// RateLimitConfig configures rate limiting
type RateLimitConfig struct {
	RequestsPerMinute int
	TokensPerMinute   int
	BurstSize         int
}

// CostManager interface for cost tracking and optimization
type CostManager interface {
	RecordUsage(ctx context.Context, usage Usage) error
	GetInsights() CostInsights
	CheckBudget(ctx context.Context) error
}

// Usage represents resource usage
type Usage struct {
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cost         float64
	Timestamp    time.Time
}

// CostInsights provides cost analytics
type CostInsights struct {
	CostToday          float64
	CostThisMonth      float64
	ProjectedMonthly   float64
	CacheHitRate       float64
	PotentialSavings   float64
	TopExpensiveModels []ModelCost
}

// ModelCost represents cost by model
type ModelCost struct {
	Model string
	Cost  float64
	Calls int64
}

// Guardrail interface for safety checks
type Guardrail interface {
	Name() string
	ValidateInput(ctx context.Context, input string) error
	ValidateOutput(ctx context.Context, output string) error
}

// Result represents a generation result
type Result struct {
	Content      string
	Metadata     map[string]interface{}
	Usage        *Usage
	FinishReason string
	ToolCalls    []ToolCallResult
	Reasoning    []string
	Error        error
}

// ToolCallResult represents a tool call from the LLM
type ToolCallResult struct {
	Name      string
	Arguments map[string]interface{}
}

// F creates a log field (alias to forge.F)
func F(key string, value interface{}) forge.Field {
	return forge.F(key, value)
}
