package testhelpers

import (
	"context"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/internal/logger"
)

// MockLLMManager for testing.
type MockLLMManager struct {
	ChatFunc func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error)
}

func (m *MockLLMManager) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	if m.ChatFunc != nil {
		return m.ChatFunc(ctx, request)
	}

	return llm.ChatResponse{}, nil
}

// NewMockLLM returns a new mock LLM manager for testing.
func NewMockLLM() *MockLLMManager {
	return &MockLLMManager{}
}

// NewMockLogger returns a new mock logger for testing.
func NewMockLogger() forge.Logger {
	return logger.NewTestLogger()
}

// NewMockMetrics returns a NoOp metrics instance for testing.
func NewMockMetrics() forge.Metrics {
	return forge.NewNoOpMetrics()
}
