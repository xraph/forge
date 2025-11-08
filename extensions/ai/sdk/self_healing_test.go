package sdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

func TestNewSelfHealingAgent(t *testing.T) {
	agent := createTestAgent()
	config := SelfHealingConfig{
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}

	sha := NewSelfHealingAgent(agent, config, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	if sha == nil {
		t.Fatal("expected self-healing agent, got nil")
	}

	if len(sha.strategies) != 4 {
		t.Errorf("expected 4 default strategies, got %d", len(sha.strategies))
	}

	if sha.config.MaxRetries != 3 {
		t.Errorf("expected max retries 3, got %d", sha.config.MaxRetries)
	}
}

func TestSelfHealingAgent_RegisterStrategy(t *testing.T) {
	agent := createTestAgent()
	sha := NewSelfHealingAgent(agent, SelfHealingConfig{}, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	customStrategy := &TestRecoveryStrategy{name: "custom"}
	sha.RegisterStrategy(customStrategy)

	if len(sha.strategies) != 5 { // 4 default + 1 custom
		t.Errorf("expected 5 strategies, got %d", len(sha.strategies))
	}
}

func TestSelfHealingAgent_Process_Success(t *testing.T) {
	agent := createTestAgentWithMocks()
	mockLLM := agent.llmManager.(*testhelpers.MockLLMManager)
	mockLLM.ChatFunc = func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
		response := llm.ChatResponse{
			Choices: []llm.ChatChoice{
				{
					Message:      llm.ChatMessage{Content: "Success!"},
					FinishReason: "stop",
				},
			},
		}

		return response, nil
	}

	config := SelfHealingConfig{
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	}
	sha := NewSelfHealingAgent(agent, config, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	response, err := sha.Process(context.Background(), "test input")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if response != "Success!" {
		t.Errorf("expected 'Success!', got %s", response)
	}
}

func TestSelfHealingAgent_Process_RetrySuccess(t *testing.T) {
	agent := createTestAgentWithMocks()
	attempts := 0
	mockLLM := agent.llmManager.(*testhelpers.MockLLMManager)
	mockLLM.ChatFunc = func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
		attempts++
		if attempts < 3 {
			return llm.ChatResponse{}, errors.New("temporary error")
		}

		return llm.ChatResponse{
			Choices: []llm.ChatChoice{
				{
					Message:      llm.ChatMessage{Content: "Success after retries!"},
					FinishReason: "stop",
				},
			},
		}, nil
	}

	config := SelfHealingConfig{
		MaxRetries:  3,
		RetryDelay:  10 * time.Millisecond,
		AutoRecover: true,
	}
	sha := NewSelfHealingAgent(agent, config, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	response, err := sha.Process(context.Background(), "test input")
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}

	if response != "Success after retries!" {
		t.Errorf("expected 'Success after retries!', got %s", response)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestSelfHealingAgent_Process_AllRetriesFail(t *testing.T) {
	agent := createTestAgentWithMocks()
	attempts := 0
	mockLLM := agent.llmManager.(*testhelpers.MockLLMManager)
	mockLLM.ChatFunc = func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
		attempts++

		return llm.ChatResponse{}, errors.New("persistent error")
	}

	config := SelfHealingConfig{
		MaxRetries:  2,
		RetryDelay:  10 * time.Millisecond,
		AutoRecover: true,
	}
	sha := NewSelfHealingAgent(agent, config, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	_, err := sha.Process(context.Background(), "test input")
	if err == nil {
		t.Fatal("expected error after all retries, got nil")
	}

	if attempts != 3 { // Initial + 2 retries
		t.Errorf("expected 3 attempts, got %d", attempts)
	}

	// Check error history
	history := sha.GetErrorHistory()
	if len(history) == 0 {
		t.Error("expected error history to be recorded")
	}
}

func TestSelfHealingAgent_RecordRecovery(t *testing.T) {
	agent := createTestAgent()
	sha := NewSelfHealingAgent(agent, SelfHealingConfig{}, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	testErr := errors.New("test error")
	sha.recordRecovery(testErr, true, 2)

	history := sha.GetErrorHistory()
	if len(history) != 1 {
		t.Errorf("expected 1 error record, got %d", len(history))
	}

	if !history[0].Recovered {
		t.Error("expected recovery to be recorded as successful")
	}

	if history[0].Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", history[0].Attempts)
	}
}

func TestSelfHealingAgent_UpdateRecoveryStats(t *testing.T) {
	agent := createTestAgent()
	sha := NewSelfHealingAgent(agent, SelfHealingConfig{}, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	sha.updateRecoveryStats("test_strategy", true)
	sha.updateRecoveryStats("test_strategy", true)
	sha.updateRecoveryStats("test_strategy", false)

	stats := sha.GetRecoveryStats()

	strategyStats, exists := stats["test_strategy"]
	if !exists {
		t.Fatal("expected stats for test_strategy")
	}

	if strategyStats.TotalAttempts != 3 {
		t.Errorf("expected 3 total attempts, got %d", strategyStats.TotalAttempts)
	}

	if strategyStats.SuccessfulRecoveries != 2 {
		t.Errorf("expected 2 successful recoveries, got %d", strategyStats.SuccessfulRecoveries)
	}

	if strategyStats.FailedRecoveries != 1 {
		t.Errorf("expected 1 failed recovery, got %d", strategyStats.FailedRecoveries)
	}
}

func TestSelfHealingAgent_LearnPattern(t *testing.T) {
	agent := createTestAgent()
	config := SelfHealingConfig{
		EnableLearning: true,
	}
	sha := NewSelfHealingAgent(agent, config, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	testErr := errors.New("test error")
	sha.learnPattern(testErr, "retry")

	patterns := sha.GetLearnedPatterns()
	errorType := "*errors.errorString"

	action, exists := patterns[errorType]
	if !exists {
		t.Fatal("expected learned pattern for error type")
	}

	if action.StrategyName != "retry" {
		t.Errorf("expected strategy 'retry', got %s", action.StrategyName)
	}

	if action.SuccessRate != 1.0 {
		t.Errorf("expected success rate 1.0, got %f", action.SuccessRate)
	}
}

func TestSelfHealingAgent_LearnPattern_StrategySwitch(t *testing.T) {
	agent := createTestAgent()
	config := SelfHealingConfig{
		EnableLearning: true,
	}
	sha := NewSelfHealingAgent(agent, config, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	testErr := errors.New("test error")

	// Learn with first strategy
	sha.learnPattern(testErr, "retry")

	// Try different strategy multiple times to reduce confidence
	for range 5 {
		sha.learnPattern(testErr, "reset_state")
	}

	patterns := sha.GetLearnedPatterns()
	errorType := "*errors.errorString"

	action, exists := patterns[errorType]
	if !exists {
		t.Fatal("expected learned pattern for error type")
	}

	// After enough attempts with different strategy, it should switch
	if action.StrategyName != "reset_state" {
		t.Errorf("expected strategy to switch to 'reset_state', got %s", action.StrategyName)
	}
}

func TestSelfHealingAgent_ErrorHistoryLimit(t *testing.T) {
	agent := createTestAgent()
	config := SelfHealingConfig{
		ErrorHistorySize: 5,
	}
	sha := NewSelfHealingAgent(agent, config, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	// Record more errors than the limit
	for range 10 {
		sha.recordRecovery(errors.New("test error"), false, 1)
	}

	history := sha.GetErrorHistory()
	if len(history) > 5 {
		t.Errorf("expected history size <= 5, got %d", len(history))
	}
}

func TestSelfHealingAgent_DefaultConfig(t *testing.T) {
	agent := createTestAgent()
	sha := NewSelfHealingAgent(agent, SelfHealingConfig{}, testhelpers.NewMockLogger(), testhelpers.NewMockMetrics())

	if sha.config.MaxRetries != 3 {
		t.Errorf("expected default MaxRetries 3, got %d", sha.config.MaxRetries)
	}

	if sha.config.RetryDelay != time.Second {
		t.Errorf("expected default RetryDelay 1s, got %v", sha.config.RetryDelay)
	}

	if sha.config.BackoffMultiplier != 2.0 {
		t.Errorf("expected default BackoffMultiplier 2.0, got %f", sha.config.BackoffMultiplier)
	}

	if sha.config.ErrorHistorySize != 100 {
		t.Errorf("expected default ErrorHistorySize 100, got %d", sha.config.ErrorHistorySize)
	}
}

func TestRecoveryStrategies_Names(t *testing.T) {
	strategies := []RecoveryStrategy{
		&RetryStrategy{},
		&ResetStateStrategy{},
		&SimplifyPromptStrategy{},
		&FallbackModelStrategy{},
	}

	expectedNames := []string{"retry", "reset_state", "simplify_prompt", "fallback_model"}

	for i, strategy := range strategies {
		if strategy.Name() != expectedNames[i] {
			t.Errorf("expected strategy name %s, got %s", expectedNames[i], strategy.Name())
		}
	}
}

func TestRecoveryStrategies_Priority(t *testing.T) {
	strategies := []RecoveryStrategy{
		&RetryStrategy{},
		&ResetStateStrategy{},
		&SimplifyPromptStrategy{},
		&FallbackModelStrategy{},
	}

	for _, strategy := range strategies {
		if strategy.Priority() <= 0 {
			t.Errorf("strategy %s has invalid priority %d", strategy.Name(), strategy.Priority())
		}
	}
}

func TestRecoveryStrategies_CanHandle(t *testing.T) {
	testErr := errors.New("test error")

	strategies := []RecoveryStrategy{
		&RetryStrategy{},
		&ResetStateStrategy{},
		&SimplifyPromptStrategy{},
		&FallbackModelStrategy{},
	}

	for _, strategy := range strategies {
		if !strategy.CanHandle(testErr) {
			t.Errorf("strategy %s should be able to handle errors", strategy.Name())
		}
	}
}

func TestResetStateStrategy_Recover(t *testing.T) {
	agent := createTestAgent()
	agent.state.History = []AgentMessage{
		{Role: "user", Content: "test1"},
		{Role: "assistant", Content: "response1"},
	}

	strategy := &ResetStateStrategy{}

	err := strategy.Recover(context.Background(), agent, errors.New("test"))
	if err != nil {
		t.Errorf("expected recovery to succeed, got error: %v", err)
	}

	if len(agent.state.History) != 0 {
		t.Errorf("expected history to be cleared, got %d messages", len(agent.state.History))
	}
}

func TestSimplifyPromptStrategy_Recover(t *testing.T) {
	agent := createTestAgent()

	strategy := &SimplifyPromptStrategy{}

	err := strategy.Recover(context.Background(), agent, errors.New("test"))
	if err != nil {
		t.Errorf("expected recovery to succeed, got error: %v", err)
	}

	simplified, exists := agent.state.Data["simplified_mode"]
	if !exists {
		t.Error("expected simplified_mode to be set")
	}

	if simplified != true {
		t.Errorf("expected simplified_mode to be true, got %v", simplified)
	}
}

func TestFallbackModelStrategy_Recover(t *testing.T) {
	agent := createTestAgent()

	strategy := &FallbackModelStrategy{}

	err := strategy.Recover(context.Background(), agent, errors.New("test"))
	if err != nil {
		t.Errorf("expected recovery to succeed, got error: %v", err)
	}

	fallback, exists := agent.state.Data["use_fallback_model"]
	if !exists {
		t.Error("expected use_fallback_model to be set")
	}

	if fallback != true {
		t.Errorf("expected use_fallback_model to be true, got %v", fallback)
	}
}

// --- Test Helpers ---

func createTestAgent() *Agent {
	agent := &Agent{
		Name: "test-agent",
		state: &AgentState{
			AgentID:   "test",
			SessionID: "session-1",
			History:   make([]AgentMessage, 0),
			Data:      make(map[string]any),
		},
	}

	return agent
}

func createTestAgentWithMocks() *Agent {
	mockStateStore := &MockStateStore{}
	agent := &Agent{
		ID:            "test-agent",
		Name:          "test-agent",
		Provider:      "test-provider",
		Model:         "test-model",
		llmManager:    testhelpers.NewMockLLM(),
		logger:        testhelpers.NewMockLogger(),
		metrics:       testhelpers.NewMockMetrics(),
		stateStore:    mockStateStore,
		maxIterations: 10,
		temperature:   0.7,
		tools:         make([]Tool, 0),
		state: &AgentState{
			AgentID:   "test-agent",
			SessionID: "session-1",
			Version:   1,
			History:   make([]AgentMessage, 0),
			Data:      make(map[string]any),
			Context:   make(map[string]any),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	return agent
}

// TestRecoveryStrategy for testing.
type TestRecoveryStrategy struct {
	name            string
	canHandleResult bool
	recoverError    error
}

func (s *TestRecoveryStrategy) Name() string             { return s.name }
func (s *TestRecoveryStrategy) Priority() int            { return 50 }
func (s *TestRecoveryStrategy) CanHandle(err error) bool { return s.canHandleResult }
func (s *TestRecoveryStrategy) Recover(ctx context.Context, agent *Agent, err error) error {
	return s.recoverError
}
