package sdk

import (
	"context"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/shared"
)

// SDK version.
const Version = "2.0.0"

// Generator provides the main entry point for AI operations.
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

// Options configures the SDK.
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

// Tracer interface for distributed tracing.
type Tracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// Span represents a trace span.
type Span interface {
	End()
	SetAttribute(key string, value any)
	SetError(err error)
	Context() context.Context
}

// StateStore interface for agent state persistence.
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

// VectorStore interface for embeddings and semantic search.
type VectorStore interface {
	Upsert(ctx context.Context, vectors []Vector) error
	Query(ctx context.Context, vector []float64, limit int, filter map[string]any) ([]VectorMatch, error)
	Delete(ctx context.Context, ids []string) error
}

// Vector represents a vector with metadata.
type Vector struct {
	ID       string
	Values   []float64
	Metadata map[string]any
}

// VectorMatch represents a search result.
type VectorMatch struct {
	ID       string
	Score    float64
	Metadata map[string]any
}

// CacheStore interface for caching.
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error
}

// RateLimitConfig configures rate limiting.
type RateLimitConfig struct {
	RequestsPerMinute int
	TokensPerMinute   int
	BurstSize         int
}

// CostManager interface for cost tracking and optimization.
type CostManager interface {
	RecordUsage(ctx context.Context, usage Usage) error
	GetInsights() CostInsights
	CheckBudget(ctx context.Context) error
}

// Usage represents resource usage.
type Usage struct {
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cost         float64
	Timestamp    time.Time
}

// CostInsights provides cost analytics.
type CostInsights struct {
	CostToday          float64
	CostThisMonth      float64
	ProjectedMonthly   float64
	CacheHitRate       float64
	PotentialSavings   float64
	TopExpensiveModels []ModelCost
}

// ModelCost represents cost by model.
type ModelCost struct {
	Model string
	Cost  float64
	Calls int64
}

// Guardrail interface for safety checks.
type Guardrail interface {
	Name() string
	ValidateInput(ctx context.Context, input string) error
	ValidateOutput(ctx context.Context, output string) error
}

// Result represents a generation result.
type Result struct {
	Content      string
	Metadata     map[string]any
	Usage        *Usage
	FinishReason string
	ToolCalls    []ToolCallResult
	Reasoning    []string
	Error        error
}

// ToolCallResult represents a tool call from the LLM.
type ToolCallResult struct {
	Name      string
	Arguments map[string]any
}

// F creates a log field (alias to forge.F).
func F(key string, value any) forge.Field {
	return forge.F(key, value)
}

// SDK provides a unified entry point for all AI operations.
// It implements the Generator interface and manages all SDK dependencies.
type SDK struct {
	// LLM configuration
	llmManager      LLMManager
	defaultProvider string
	defaultModel    string

	// Observability
	logger  forge.Logger
	metrics forge.Metrics
	tracer  Tracer

	// Storage
	stateStore  StateStore
	vectorStore VectorStore
	cacheStore  CacheStore

	// Limits
	defaultTimeout time.Duration
	maxRetries     int
	rateLimit      RateLimitConfig

	// Cost management
	costManager CostManager

	// Safety
	guardrails []Guardrail

	// Health
	healthManager shared.HealthManager
}

// New creates a new SDK instance with the provided options.
func New(llmManager LLMManager, opts *Options) *SDK {
	s := &SDK{
		llmManager:     llmManager,
		defaultTimeout: 30 * time.Second,
		maxRetries:     3,
	}

	if opts != nil {
		s.defaultProvider = opts.DefaultProvider
		s.defaultModel = opts.DefaultModel
		s.logger = opts.Logger
		s.metrics = opts.Metrics
		s.tracer = opts.Tracer
		s.stateStore = opts.StateStore
		s.vectorStore = opts.VectorStore
		s.cacheStore = opts.CacheStore
		s.costManager = opts.CostManager
		s.guardrails = opts.Guardrails
		s.healthManager = opts.HealthManager

		if opts.DefaultTimeout > 0 {
			s.defaultTimeout = opts.DefaultTimeout
		}

		if opts.MaxRetries > 0 {
			s.maxRetries = opts.MaxRetries
		}

		s.rateLimit = opts.RateLimit
	}

	return s
}

// Generate creates a new GenerateBuilder for text generation.
func (s *SDK) Generate(ctx context.Context) *GenerateBuilder {
	builder := NewGenerateBuilder(ctx, s.llmManager, s.logger, s.metrics)

	// Apply defaults
	if s.defaultProvider != "" {
		builder.WithProvider(s.defaultProvider)
	}

	if s.defaultModel != "" {
		builder.WithModel(s.defaultModel)
	}

	if s.defaultTimeout > 0 {
		builder.WithTimeout(s.defaultTimeout)
	}

	return builder
}

// Stream creates a new StreamBuilder for streaming generation.
func (s *SDK) Stream(ctx context.Context) *StreamBuilder {
	builder := NewStreamBuilder(ctx, s.llmManager, s.logger, s.metrics)

	// Apply defaults
	if s.defaultProvider != "" {
		builder.WithProvider(s.defaultProvider)
	}

	if s.defaultModel != "" {
		builder.WithModel(s.defaultModel)
	}

	if s.defaultTimeout > 0 {
		builder.WithTimeout(s.defaultTimeout)
	}

	return builder
}

// NewAgent creates a new AgentBuilder for building agents.
func (s *SDK) NewAgent(name string) *AgentBuilder {
	builder := NewAgentBuilder().
		WithName(name).
		WithLLMManager(s.llmManager).
		WithLogger(s.logger).
		WithMetrics(s.metrics)

	// Apply defaults
	if s.defaultProvider != "" {
		builder.WithProvider(s.defaultProvider)
	}

	if s.defaultModel != "" {
		builder.WithModel(s.defaultModel)
	}

	if s.stateStore != nil {
		builder.WithStateStore(s.stateStore)
	}

	return builder
}

// NewWorkflow creates a new WorkflowBuilder for building workflows.
func (s *SDK) NewWorkflow(name string) *WorkflowBuilder {
	return NewWorkflowBuilder().
		WithName(name).
		WithLogger(s.logger).
		WithMetrics(s.metrics)
}

// GenerateObject creates a new GenerateObjectBuilder for structured output generation.
// Note: This is a convenience method; for full type safety, use NewGenerateObjectBuilder directly.
func (s *SDK) GenerateObject(ctx context.Context) *GenerateObjectBuilder[map[string]any] {
	builder := NewGenerateObjectBuilder[map[string]any](ctx, s.llmManager, s.logger, s.metrics)

	if s.defaultProvider != "" {
		builder.WithProvider(s.defaultProvider)
	}

	if s.defaultModel != "" {
		builder.WithModel(s.defaultModel)
	}

	if s.defaultTimeout > 0 {
		builder.WithTimeout(s.defaultTimeout)
	}

	return builder
}

// MultiModal creates a new MultiModalBuilder for multi-modal generation.
func (s *SDK) MultiModal(ctx context.Context) *MultiModalBuilder {
	builder := NewMultiModalBuilder(ctx, s.llmManager, s.logger, s.metrics)

	if s.defaultModel != "" {
		builder.WithModel(s.defaultModel)
	}

	return builder
}

// LLMManager returns the SDK's LLM manager.
func (s *SDK) LLMManager() LLMManager {
	return s.llmManager
}

// StateStore returns the SDK's state store.
func (s *SDK) StateStore() StateStore {
	return s.stateStore
}

// VectorStore returns the SDK's vector store.
func (s *SDK) VectorStore() VectorStore {
	return s.vectorStore
}

// Logger returns the SDK's logger.
func (s *SDK) Logger() forge.Logger {
	return s.logger
}

// Metrics returns the SDK's metrics.
func (s *SDK) Metrics() forge.Metrics {
	return s.metrics
}

// Ensure SDK implements Generator interface.
var _ Generator = (*SDK)(nil)
