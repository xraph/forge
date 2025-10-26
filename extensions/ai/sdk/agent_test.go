package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

// Mock StateStore
type MockStateStore struct {
	SaveFunc   func(ctx context.Context, state *AgentState) error
	LoadFunc   func(ctx context.Context, agentID, sessionID string) (*AgentState, error)
	DeleteFunc func(ctx context.Context, agentID, sessionID string) error
	ListFunc   func(ctx context.Context, agentID string) ([]string, error)
}

func (m *MockStateStore) Save(ctx context.Context, state *AgentState) error {
	if m.SaveFunc != nil {
		return m.SaveFunc(ctx, state)
	}
	return nil
}

func (m *MockStateStore) Load(ctx context.Context, agentID, sessionID string) (*AgentState, error) {
	if m.LoadFunc != nil {
		return m.LoadFunc(ctx, agentID, sessionID)
	}
	return nil, errors.New("not found")
}

func (m *MockStateStore) Delete(ctx context.Context, agentID, sessionID string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, agentID, sessionID)
	}
	return nil
}

func (m *MockStateStore) List(ctx context.Context, agentID string) ([]string, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, agentID)
	}
	return []string{}, nil
}

func TestNewAgent(t *testing.T) {
	llmManager := &testhelpers.MockLLMManager{}
	stateStore := &MockStateStore{}
	logger := &testhelpers.MockLogger{}
	metrics := testhelpers.NewMockMetrics()

	agent, err := NewAgent("agent-1", "Test Agent", llmManager, stateStore, logger, metrics, nil)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if agent == nil {
		t.Fatal("expected agent to be created")
	}

	if agent.ID != "agent-1" {
		t.Errorf("expected ID 'agent-1', got '%s'", agent.ID)
	}

	if agent.Name != "Test Agent" {
		t.Errorf("expected name 'Test Agent', got '%s'", agent.Name)
	}

	if agent.maxIterations != 10 {
		t.Errorf("expected default maxIterations 10, got %d", agent.maxIterations)
	}
}

func TestNewAgent_WithOptions(t *testing.T) {
	llmManager := &testhelpers.MockLLMManager{}
	stateStore := &MockStateStore{}

	opts := &AgentOptions{
		SessionID:     "session-123",
		SystemPrompt:  "You are a helpful assistant",
		MaxIterations: 5,
		Temperature:   0.8,
	}

	agent, err := NewAgent("agent-1", "Test Agent", llmManager, stateStore, nil, nil, opts)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if agent.systemPrompt != "You are a helpful assistant" {
		t.Error("expected system prompt to be set")
	}

	if agent.maxIterations != 5 {
		t.Errorf("expected maxIterations 5, got %d", agent.maxIterations)
	}

	if agent.temperature != 0.8 {
		t.Errorf("expected temperature 0.8, got %f", agent.temperature)
	}

	if agent.state.SessionID != "session-123" {
		t.Errorf("expected session ID 'session-123', got '%s'", agent.state.SessionID)
	}
}

func TestAgent_SetAndGetStateData(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	err := agent.SetStateData("key1", "value1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	val, ok := agent.GetStateData("key1")
	if !ok {
		t.Error("expected key1 to exist")
	}

	if val != "value1" {
		t.Errorf("expected 'value1', got '%v'", val)
	}

	_, ok = agent.GetStateData("nonexistent")
	if ok {
		t.Error("expected nonexistent key to return false")
	}
}

func TestAgent_GetState(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	agent.SetStateData("test", "data")

	state := agent.GetState()

	if state == nil {
		t.Fatal("expected state to be returned")
	}

	if state.AgentID != "agent-1" {
		t.Error("expected agent ID to match")
	}

	if state.Data["test"] != "data" {
		t.Error("expected state data to be copied")
	}

	// Verify it's a copy (not a reference)
	state.Data["test"] = "modified"
	originalVal, _ := agent.GetStateData("test")
	if originalVal == "modified" {
		t.Error("state should be a copy, not a reference")
	}
}

func TestAgent_SaveState(t *testing.T) {
	saveCalled := false
	var savedState *AgentState

	stateStore := &MockStateStore{
		SaveFunc: func(ctx context.Context, state *AgentState) error {
			saveCalled = true
			savedState = state
			return nil
		},
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, stateStore, nil, nil, nil)
	agent.SetStateData("key", "value")

	initialVersion := agent.state.Version

	err := agent.SaveState(context.Background())

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !saveCalled {
		t.Error("expected Save to be called")
	}

	if savedState == nil {
		t.Fatal("expected state to be saved")
	}

	if savedState.Version != initialVersion+1 {
		t.Errorf("expected version to be incremented to %d, got %d", initialVersion+1, savedState.Version)
	}
}

func TestAgent_SaveState_Error(t *testing.T) {
	stateStore := &MockStateStore{
		SaveFunc: func(ctx context.Context, state *AgentState) error {
			return errors.New("save error")
		},
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, stateStore, nil, nil, nil)

	err := agent.SaveState(context.Background())

	if err == nil {
		t.Error("expected error from save")
	}

	if !strings.Contains(err.Error(), "failed to save state") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAgent_LoadState(t *testing.T) {
	existingState := &AgentState{
		AgentID:   "agent-1",
		SessionID: "session-123",
		Version:   5,
		Data: map[string]interface{}{
			"loaded": "data",
		},
		History:   []AgentMessage{{Role: "user", Content: "Hello"}},
		Context:   make(map[string]interface{}),
		CreatedAt: time.Now().Add(-1 * time.Hour),
		UpdatedAt: time.Now(),
	}

	stateStore := &MockStateStore{
		LoadFunc: func(ctx context.Context, agentID, sessionID string) (*AgentState, error) {
			if agentID == "agent-1" && sessionID == "session-123" {
				return existingState, nil
			}
			return nil, errors.New("not found")
		},
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, stateStore, nil, nil, nil)

	err := agent.LoadState(context.Background(), "session-123")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if agent.state.Version != 5 {
		t.Errorf("expected version 5, got %d", agent.state.Version)
	}

	val, ok := agent.GetStateData("loaded")
	if !ok || val != "data" {
		t.Error("expected loaded data to be present")
	}

	if len(agent.state.History) != 1 {
		t.Errorf("expected 1 history message, got %d", len(agent.state.History))
	}
}

func TestAgent_LoadState_NotFound(t *testing.T) {
	stateStore := &MockStateStore{
		LoadFunc: func(ctx context.Context, agentID, sessionID string) (*AgentState, error) {
			return nil, errors.New("not found")
		},
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, stateStore, nil, nil, nil)

	err := agent.LoadState(context.Background(), "nonexistent")

	if err == nil {
		t.Error("expected error when state not found")
	}
}

func TestAgent_ClearHistory(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	// Add some messages
	agent.addToHistory(AgentMessage{Role: "user", Content: "Hello"})
	agent.addToHistory(AgentMessage{Role: "assistant", Content: "Hi there"})

	if len(agent.state.History) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(agent.state.History))
	}

	agent.ClearHistory()

	if len(agent.state.History) != 0 {
		t.Errorf("expected history to be cleared, got %d messages", len(agent.state.History))
	}
}

func TestAgent_Reset(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	// Set up state
	agent.SetStateData("key", "value")
	agent.addToHistory(AgentMessage{Role: "user", Content: "Hello"})
	initialVersion := agent.state.Version

	agent.Reset()

	if len(agent.state.Data) != 0 {
		t.Error("expected data to be reset")
	}

	if len(agent.state.History) != 0 {
		t.Error("expected history to be reset")
	}

	if agent.state.Version != initialVersion+1 {
		t.Error("expected version to be incremented")
	}
}

func TestAgent_MarshalUnmarshalState(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	agent.SetStateData("key", "value")
	agent.addToHistory(AgentMessage{Role: "user", Content: "Test"})

	// Marshal
	data, err := agent.MarshalState()
	if err != nil {
		t.Fatalf("expected no error marshaling, got %v", err)
	}

	// Create new agent and unmarshal
	agent2, _ := NewAgent("agent-2", "Test2", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	err = agent2.UnmarshalState(data)
	if err != nil {
		t.Fatalf("expected no error unmarshaling, got %v", err)
	}

	// Verify state was restored
	val, ok := agent2.GetStateData("key")
	if !ok || val != "value" {
		t.Error("expected state data to be restored")
	}

	if len(agent2.state.History) != 1 {
		t.Errorf("expected 1 history message, got %d", len(agent2.state.History))
	}
}

func TestAgent_GetHistoryLength(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	if agent.GetHistoryLength() != 0 {
		t.Error("expected initial history length to be 0")
	}

	agent.addToHistory(AgentMessage{Role: "user", Content: "Hello"})
	agent.addToHistory(AgentMessage{Role: "assistant", Content: "Hi"})

	if agent.GetHistoryLength() != 2 {
		t.Errorf("expected history length 2, got %d", agent.GetHistoryLength())
	}
}

func TestAgent_GetSessionID(t *testing.T) {
	opts := &AgentOptions{
		SessionID: "test-session",
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, opts)

	if agent.GetSessionID() != "test-session" {
		t.Errorf("expected session ID 'test-session', got '%s'", agent.GetSessionID())
	}
}

func TestAgent_Callbacks(t *testing.T) {
	startCalled := false
	var messagesReceived []AgentMessage
	var toolsCalled []string
	iterationCount := 0
	completeCalled := false

	callbacks := AgentCallbacks{
		OnStart: func(ctx context.Context) error {
			startCalled = true
			return nil
		},
		OnMessage: func(msg AgentMessage) {
			messagesReceived = append(messagesReceived, msg)
		},
		OnToolCall: func(name string, args map[string]interface{}) {
			toolsCalled = append(toolsCalled, name)
		},
		OnIteration: func(iteration int) {
			iterationCount = iteration
		},
		OnComplete: func(state *AgentState) {
			completeCalled = true
		},
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, &AgentOptions{
		Callbacks: callbacks,
	})

	// Manually trigger callbacks
	if agent.callbacks.OnStart != nil {
		agent.callbacks.OnStart(context.Background())
	}

	agent.addToHistory(AgentMessage{Role: "user", Content: "Test"})

	if agent.callbacks.OnIteration != nil {
		agent.callbacks.OnIteration(1)
	}

	if agent.callbacks.OnComplete != nil {
		agent.callbacks.OnComplete(agent.GetState())
	}

	// Verify callbacks were called
	if !startCalled {
		t.Error("expected OnStart to be called")
	}

	if len(messagesReceived) == 0 {
		t.Error("expected OnMessage to be called")
	}

	if iterationCount != 1 {
		t.Errorf("expected iteration 1, got %d", iterationCount)
	}

	if !completeCalled {
		t.Error("expected OnComplete to be called")
	}
}

func TestAgent_BuildMessages(t *testing.T) {
	opts := &AgentOptions{
		SystemPrompt: "You are helpful",
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, opts)

	agent.addToHistory(AgentMessage{Role: "user", Content: "Hello"})
	agent.addToHistory(AgentMessage{Role: "assistant", Content: "Hi"})

	messages := agent.buildMessages()

	// Should have system + 2 messages
	if len(messages) != 3 {
		t.Errorf("expected 3 messages (system + 2), got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Error("expected first message to be system")
	}

	if messages[0].Content != "You are helpful" {
		t.Error("expected system prompt content")
	}

	if messages[1].Role != "user" {
		t.Error("expected second message to be user")
	}
}

func TestAgent_BuildMessages_NoSystemPrompt(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	agent.addToHistory(AgentMessage{Role: "user", Content: "Hello"})

	messages := agent.buildMessages()

	if len(messages) != 1 {
		t.Errorf("expected 1 message (no system), got %d", len(messages))
	}

	if messages[0].Role != "user" {
		t.Error("expected first message to be user")
	}
}

func TestAgent_AddToHistory(t *testing.T) {
	messageCalled := false
	var receivedMessage AgentMessage

	callbacks := AgentCallbacks{
		OnMessage: func(msg AgentMessage) {
			messageCalled = true
			receivedMessage = msg
		},
	}

	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, &AgentOptions{
		Callbacks: callbacks,
	})

	msg := AgentMessage{
		Role:      "user",
		Content:   "Test message",
		Timestamp: time.Now(),
	}

	err := agent.addToHistory(msg)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !messageCalled {
		t.Error("expected OnMessage callback to be called")
	}

	if receivedMessage.Content != "Test message" {
		t.Error("expected message to be passed to callback")
	}

	if len(agent.state.History) != 1 {
		t.Errorf("expected 1 message in history, got %d", len(agent.state.History))
	}
}

func TestAgent_ThreadSafety(t *testing.T) {
	agent, _ := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, &MockStateStore{}, nil, nil, nil)

	// Concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			agent.SetStateData("key", i)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			agent.GetStateData("key")
			agent.GetState()
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// If we get here without data races, the test passes
}

func TestNewAgent_WithExistingState(t *testing.T) {
	existingState := &AgentState{
		AgentID:   "agent-1",
		SessionID: "existing-session",
		Version:   3,
		Data: map[string]interface{}{
			"previous": "data",
		},
		History:   []AgentMessage{{Role: "user", Content: "Previous message"}},
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-1 * time.Hour),
	}

	stateStore := &MockStateStore{
		LoadFunc: func(ctx context.Context, agentID, sessionID string) (*AgentState, error) {
			if sessionID == "existing-session" {
				return existingState, nil
			}
			return nil, errors.New("not found")
		},
	}

	opts := &AgentOptions{
		SessionID: "existing-session",
	}

	agent, err := NewAgent("agent-1", "Test", &testhelpers.MockLLMManager{}, stateStore, nil, nil, opts)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if agent.state.Version != 3 {
		t.Errorf("expected version 3 from loaded state, got %d", agent.state.Version)
	}

	val, ok := agent.GetStateData("previous")
	if !ok || val != "data" {
		t.Error("expected previous data to be loaded")
	}

	if len(agent.state.History) != 1 {
		t.Error("expected history to be loaded")
	}
}

