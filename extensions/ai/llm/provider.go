package llm

import (
	"context"
	"time"
)

// LLMProvider defines the interface for LLM providers.
type LLMProvider interface {
	// Basic provider information
	Name() string
	Models() []string

	// Core LLM operations
	Chat(ctx context.Context, request ChatRequest) (ChatResponse, error)
	Complete(ctx context.Context, request CompletionRequest) (CompletionResponse, error)
	Embed(ctx context.Context, request EmbeddingRequest) (EmbeddingResponse, error)

	// Provider management
	GetUsage() LLMUsage
	HealthCheck(ctx context.Context) error
}

// StreamingProvider defines the interface for LLM providers that support streaming.
type StreamingProvider interface {
	LLMProvider
	// ChatStream performs a streaming chat completion request
	ChatStream(ctx context.Context, request ChatRequest, handler func(ChatStreamEvent) error) error
}

// LLMUsage represents usage statistics for an LLM provider.
type LLMUsage struct {
	InputTokens       int64         `json:"input_tokens"`
	OutputTokens      int64         `json:"output_tokens"`
	TotalTokens       int64         `json:"total_tokens"`
	RequestCount      int64         `json:"request_count"`
	Cost              float64       `json:"cost"`
	LastReset         time.Time     `json:"last_reset"`
	ThrottleCount     int64         `json:"throttle_count"`
	AverageLatency    time.Duration `json:"average_latency"`
	ErrorCount        int64         `json:"error_count"`
	RequestsPerSecond float64       `json:"requests_per_second"`
	TokensPerSecond   float64       `json:"tokens_per_second"`
	RateLimit         *RateLimit    `json:"rate_limit"`
}
