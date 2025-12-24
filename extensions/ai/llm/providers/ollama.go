package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// OllamaProvider implements LLM provider for Ollama.
// Note: This is a stub implementation. Full Ollama provider pending.
type OllamaProvider struct {
	name    string
	baseURL string
	models  []string
	logger  forge.Logger
	metrics forge.Metrics
}

// OllamaConfig contains configuration for Ollama provider.
type OllamaConfig struct {
	BaseURL    string        `default:"http://localhost:11434" yaml:"base_url"`
	Timeout    time.Duration `default:"60s" yaml:"timeout"`
	MaxRetries int           `default:"2" yaml:"max_retries"`
	Models     []string      `yaml:"models"`
	Logger     forge.Logger
	Metrics    forge.Metrics
}

// NewOllamaProvider creates a new Ollama provider.
// Note: This is a stub implementation. Full Ollama provider pending.
func NewOllamaProvider(config OllamaConfig, logger forge.Logger, metrics forge.Metrics) (*OllamaProvider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:11434"
	}

	return &OllamaProvider{
		name:    "ollama",
		baseURL: config.BaseURL,
		models:  config.Models,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the provider name.
func (p *OllamaProvider) Name() string {
	return p.name
}

// Models returns the available models.
func (p *OllamaProvider) Models() []string {
	return p.models
}

// Chat performs a chat completion request.
func (p *OllamaProvider) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, fmt.Errorf("ollama provider not yet implemented - chat functionality pending")
}

// Complete performs a text completion request.
func (p *OllamaProvider) Complete(ctx context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{}, fmt.Errorf("ollama provider not yet implemented - completion functionality pending")
}

// Embed performs an embedding request.
func (p *OllamaProvider) Embed(ctx context.Context, request llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	return llm.EmbeddingResponse{}, fmt.Errorf("ollama provider not yet implemented - embedding functionality pending")
}

// GetUsage returns current usage statistics.
func (p *OllamaProvider) GetUsage() llm.LLMUsage {
	return llm.LLMUsage{
		LastReset: time.Now(),
	}
}

// HealthCheck performs a health check.
func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	return fmt.Errorf("ollama provider not yet implemented - health check functionality pending")
}

// Stop stops the provider.
func (p *OllamaProvider) Stop(ctx context.Context) error {
	return nil
}
