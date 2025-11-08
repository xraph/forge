package testhelpers

import (
	"context"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
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

// MockLogger implements basic logging for tests.
type MockLogger struct {
	DebugCalls []string
	InfoCalls  []string
	WarnCalls  []string
	ErrorCalls []string
}

func (m *MockLogger) Debug(msg string, fields ...forge.Field) {
	m.DebugCalls = append(m.DebugCalls, msg)
}
func (m *MockLogger) Debugf(format string, args ...any)       {}
func (m *MockLogger) Debugw(msg string, keysAndValues ...any) {}
func (m *MockLogger) Info(msg string, fields ...forge.Field)  { m.InfoCalls = append(m.InfoCalls, msg) }
func (m *MockLogger) Infof(format string, args ...any)        {}
func (m *MockLogger) Infow(msg string, keysAndValues ...any)  {}
func (m *MockLogger) Warn(msg string, fields ...forge.Field)  { m.WarnCalls = append(m.WarnCalls, msg) }
func (m *MockLogger) Warnf(format string, args ...any)        {}
func (m *MockLogger) Warnw(msg string, keysAndValues ...any)  {}
func (m *MockLogger) Error(msg string, fields ...forge.Field) {
	m.ErrorCalls = append(m.ErrorCalls, msg)
}
func (m *MockLogger) Errorf(format string, args ...any)            {}
func (m *MockLogger) Errorw(msg string, keysAndValues ...any)      {}
func (m *MockLogger) Fatal(msg string, fields ...forge.Field)      {}
func (m *MockLogger) Fatalf(format string, args ...any)            {}
func (m *MockLogger) Fatalw(msg string, keysAndValues ...any)      {}
func (m *MockLogger) Panic(msg string, fields ...forge.Field)      {}
func (m *MockLogger) Panicf(format string, args ...any)            {}
func (m *MockLogger) Panicw(msg string, keysAndValues ...any)      {}
func (m *MockLogger) With(fields ...forge.Field) forge.Logger      { return m }
func (m *MockLogger) WithContext(ctx context.Context) forge.Logger { return m }
func (m *MockLogger) Named(name string) forge.Logger               { return m }
func (m *MockLogger) Sync() error                                  { return nil }
func (m *MockLogger) Sugar() forge.SugarLogger                     { return &MockSugarLogger{m} }

// MockSugarLogger implements forge.SugarLogger for tests.
type MockSugarLogger struct {
	logger *MockLogger
}

func (s *MockSugarLogger) Debugw(msg string, keysAndValues ...any) {}
func (s *MockSugarLogger) Infow(msg string, keysAndValues ...any)  {}
func (s *MockSugarLogger) Warnw(msg string, keysAndValues ...any)  {}
func (s *MockSugarLogger) Errorw(msg string, keysAndValues ...any) {}
func (s *MockSugarLogger) Fatalw(msg string, keysAndValues ...any) {}
func (s *MockSugarLogger) With(args ...any) forge.SugarLogger      { return s }

// NewMockLLM returns a new mock LLM manager for testing.
func NewMockLLM() *MockLLMManager {
	return &MockLLMManager{}
}

// NewMockLogger returns a new mock logger for testing.
func NewMockLogger() *MockLogger {
	return &MockLogger{
		DebugCalls: make([]string, 0),
		InfoCalls:  make([]string, 0),
		WarnCalls:  make([]string, 0),
		ErrorCalls: make([]string, 0),
	}
}

// NewMockMetrics returns a NoOp metrics instance for testing.
func NewMockMetrics() forge.Metrics {
	return forge.NewNoOpMetrics()
}
