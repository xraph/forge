package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

// --- AgentRegistry Tests ---

func TestAgentRegistry(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	t.Run("register agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent := createHandoffTestAgent(t, "agent1", "Test Agent 1")

		err := registry.Register(agent)
		if err != nil {
			t.Fatalf("failed to register agent: %v", err)
		}

		// Verify agent is registered
		retrieved, err := registry.Get("agent1")
		if err != nil {
			t.Fatalf("failed to get agent: %v", err)
		}

		if retrieved.ID != "agent1" {
			t.Errorf("expected agent ID 'agent1', got '%s'", retrieved.ID)
		}
	})

	t.Run("register nil agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		err := registry.Register(nil)
		if !errors.Is(err, ErrAgentNil) {
			t.Errorf("expected ErrAgentNil, got %v", err)
		}
	})

	t.Run("register agent without ID", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent := &Agent{Name: "Test Agent"}

		err := registry.Register(agent)
		if !errors.Is(err, ErrAgentIDRequired) {
			t.Errorf("expected ErrAgentIDRequired, got %v", err)
		}
	})

	t.Run("register duplicate agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent1 := createHandoffTestAgent(t, "agent1", "Test Agent 1")
		agent2 := createHandoffTestAgent(t, "agent1", "Test Agent 2")

		err := registry.Register(agent1)
		if err != nil {
			t.Fatalf("failed to register first agent: %v", err)
		}

		err = registry.Register(agent2)
		if err == nil {
			t.Error("expected error for duplicate agent, got nil")
		}
	})

	t.Run("register with alias", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent := createHandoffTestAgent(t, "agent1", "Test Agent 1")

		err := registry.RegisterWithAlias(agent, "my-agent")
		if err != nil {
			t.Fatalf("failed to register agent with alias: %v", err)
		}

		// Get by alias
		retrieved, err := registry.Get("my-agent")
		if err != nil {
			t.Fatalf("failed to get agent by alias: %v", err)
		}

		if retrieved.ID != "agent1" {
			t.Errorf("expected agent ID 'agent1', got '%s'", retrieved.ID)
		}
	})

	t.Run("unregister agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent := createHandoffTestAgent(t, "agent1", "Test Agent 1")

		_ = registry.Register(agent)

		err := registry.Unregister("agent1")
		if err != nil {
			t.Fatalf("failed to unregister agent: %v", err)
		}

		// Verify agent is gone
		_, err = registry.Get("agent1")
		if err == nil {
			t.Error("expected error for unregistered agent, got nil")
		}
	})

	t.Run("unregister non-existent agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		err := registry.Unregister("non-existent")
		if err == nil {
			t.Error("expected error for non-existent agent, got nil")
		}
	})

	t.Run("list agents", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent1 := createHandoffTestAgent(t, "agent1", "Test Agent 1")
		agent2 := createHandoffTestAgent(t, "agent2", "Test Agent 2")

		_ = registry.Register(agent1)
		_ = registry.Register(agent2)

		agents := registry.List()
		if len(agents) != 2 {
			t.Errorf("expected 2 agents, got %d", len(agents))
		}
	})

	t.Run("list agent info", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent := createHandoffTestAgent(t, "agent1", "Test Agent 1")
		agent.Description = "A test agent"

		_ = registry.Register(agent)

		infos := registry.ListInfo()
		if len(infos) != 1 {
			t.Fatalf("expected 1 agent info, got %d", len(infos))
		}

		if infos[0].ID != "agent1" {
			t.Errorf("expected ID 'agent1', got '%s'", infos[0].ID)
		}

		if infos[0].Description != "A test agent" {
			t.Errorf("expected description 'A test agent', got '%s'", infos[0].Description)
		}
	})

	t.Run("find by tool", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent1 := createHandoffTestAgent(t, "agent1", "Agent 1")
		agent1.tools = []Tool{
			{Name: "search", Description: "Search tool"},
		}

		agent2 := createHandoffTestAgent(t, "agent2", "Agent 2")
		agent2.tools = []Tool{
			{Name: "calculate", Description: "Calculator tool"},
		}

		_ = registry.Register(agent1)
		_ = registry.Register(agent2)

		// Find by tool
		agents := registry.FindByTool("search")
		if len(agents) != 1 {
			t.Fatalf("expected 1 agent with search tool, got %d", len(agents))
		}

		if agents[0].ID != "agent1" {
			t.Errorf("expected agent1, got %s", agents[0].ID)
		}
	})
}

// --- DefaultAgentRouter Tests ---

func TestDefaultAgentRouter(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()
	mockLLM := testhelpers.NewMockLLM()

	t.Run("route with single agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		agent := createHandoffTestAgent(t, "agent1", "Test Agent")
		_ = registry.Register(agent)

		router := NewDefaultAgentRouter(registry, "", mockLLM, logger, metrics)

		routed, err := router.Route(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("failed to route: %v", err)
		}

		if routed.ID != "agent1" {
			t.Errorf("expected agent1, got %s", routed.ID)
		}
	})

	t.Run("route to default agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent1 := createHandoffTestAgent(t, "agent1", "Agent 1")
		agent2 := createHandoffTestAgent(t, "agent2", "Agent 2")
		_ = registry.Register(agent1)
		_ = registry.Register(agent2)

		router := NewDefaultAgentRouter(registry, "agent2", mockLLM, logger, metrics)

		routed, err := router.Route(context.Background(), "Random input with no match")
		if err != nil {
			t.Fatalf("failed to route: %v", err)
		}
		// Should fall back to default
		if routed.ID != "agent2" {
			t.Errorf("expected agent2 (default), got %s", routed.ID)
		}
	})

	t.Run("route by agent name match", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent1 := createHandoffTestAgent(t, "agent1", "Calculator")
		agent2 := createHandoffTestAgent(t, "agent2", "SearchBot")
		_ = registry.Register(agent1)
		_ = registry.Register(agent2)

		router := NewDefaultAgentRouter(registry, "", mockLLM, logger, metrics)

		routed, err := router.Route(context.Background(), "I need to use the Calculator")
		if err != nil {
			t.Fatalf("failed to route: %v", err)
		}

		if routed.ID != "agent1" {
			t.Errorf("expected agent1 (Calculator), got %s", routed.ID)
		}
	})

	t.Run("route by tool match", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		agent1 := createHandoffTestAgent(t, "agent1", "Agent 1")
		agent1.tools = []Tool{{Name: "web_search", Description: "Search the web"}}

		agent2 := createHandoffTestAgent(t, "agent2", "Agent 2")
		agent2.tools = []Tool{{Name: "calculate", Description: "Do math"}}

		_ = registry.Register(agent1)
		_ = registry.Register(agent2)

		router := NewDefaultAgentRouter(registry, "", mockLLM, logger, metrics)

		routed, err := router.Route(context.Background(), "Please do a web_search for me")
		if err != nil {
			t.Fatalf("failed to route: %v", err)
		}

		if routed.ID != "agent1" {
			t.Errorf("expected agent1 (has web_search), got %s", routed.ID)
		}
	})

	t.Run("route with no agents", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", mockLLM, logger, metrics)

		_, err := router.Route(context.Background(), "Hello")
		if !errors.Is(err, ErrNoAgentsAvailable) {
			t.Errorf("expected ErrNoAgentsAvailable, got %v", err)
		}
	})
}

// --- HandoffManager Tests ---

func TestHandoffManager(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	t.Run("simple handoff", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		// Create mock agents that return responses
		agent1 := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Response from agent 1")
		agent2 := createHandoffTestAgentWithResponse(t, "agent2", "Agent 2", "Response from agent 2")
		_ = registry.Register(agent1)
		_ = registry.Register(agent2)

		handoff := &AgentHandoff{
			FromAgentID: "agent1",
			ToAgentID:   "agent2",
			Reason:      "Need specialized help",
			Input:       "Please help me",
		}

		result, err := manager.Handoff(context.Background(), handoff)
		if err != nil {
			t.Fatalf("handoff failed: %v", err)
		}

		if result.FromAgentID != "agent1" {
			t.Errorf("expected FromAgentID 'agent1', got '%s'", result.FromAgentID)
		}

		if result.ToAgentID != "agent2" {
			t.Errorf("expected ToAgentID 'agent2', got '%s'", result.ToAgentID)
		}

		if result.Response == nil {
			t.Fatal("expected response, got nil")
		}

		if result.Response.Content != "Response from agent 2" {
			t.Errorf("expected 'Response from agent 2', got '%s'", result.Response.Content)
		}
	})

	t.Run("handoff to non-existent agent", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		agent1 := createHandoffTestAgent(t, "agent1", "Agent 1")
		_ = registry.Register(agent1)

		handoff := &AgentHandoff{
			FromAgentID: "agent1",
			ToAgentID:   "non-existent",
			Reason:      "Test",
			Input:       "Test input",
		}

		_, err := manager.Handoff(context.Background(), handoff)
		if err == nil {
			t.Error("expected error for non-existent target agent, got nil")
		}
	})

	t.Run("handoff to tool", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		agent1 := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Result")
		agent1.tools = []Tool{{Name: "special_tool", Description: "Special tool"}}
		_ = registry.Register(agent1)

		result, err := manager.HandoffToTool(context.Background(), "", "special_tool", "Use the tool", nil)
		if err != nil {
			t.Fatalf("handoff to tool failed: %v", err)
		}

		if result.ToAgentID != "agent1" {
			t.Errorf("expected ToAgentID 'agent1', got '%s'", result.ToAgentID)
		}
	})

	t.Run("handoff to tool not found", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		agent1 := createHandoffTestAgent(t, "agent1", "Agent 1")
		_ = registry.Register(agent1)

		_, err := manager.HandoffToTool(context.Background(), "agent1", "non_existent_tool", "Use the tool", nil)
		if err == nil {
			t.Error("expected error for non-existent tool, got nil")
		}
	})

	t.Run("auto route", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		agent := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Auto routed response")
		_ = registry.Register(agent)

		result, err := manager.AutoRoute(context.Background(), "Hello", nil)
		if err != nil {
			t.Fatalf("auto route failed: %v", err)
		}

		if result.ToAgentID != "agent1" {
			t.Errorf("expected ToAgentID 'agent1', got '%s'", result.ToAgentID)
		}
	})

	t.Run("chained handoff", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		agent1 := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Response 1")
		agent2 := createHandoffTestAgentWithResponse(t, "agent2", "Agent 2", "Response 2")
		agent3 := createHandoffTestAgentWithResponse(t, "agent3", "Agent 3", "Response 3")
		_ = registry.Register(agent1)
		_ = registry.Register(agent2)
		_ = registry.Register(agent3)

		handoffs := []*AgentHandoff{
			{FromAgentID: "", ToAgentID: "agent1", Input: "Start"},
			{FromAgentID: "agent1", ToAgentID: "agent2", Input: "Step 2"},
			{FromAgentID: "agent2", ToAgentID: "agent3", Input: "Final"},
		}

		result, err := manager.ChainedHandoff(context.Background(), handoffs)
		if err != nil {
			t.Fatalf("chained handoff failed: %v", err)
		}

		if len(result.HandoffChain) != 3 {
			t.Errorf("expected 3 handoffs in chain, got %d", len(result.HandoffChain))
		}

		if result.Response.Content != "Response 3" {
			t.Errorf("expected final response 'Response 3', got '%s'", result.Response.Content)
		}
	})

	t.Run("chained handoff exceeds max depth", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, &HandoffManagerOptions{
			MaxChainDepth: 2,
		})

		handoffs := []*AgentHandoff{
			{ToAgentID: "agent1"},
			{ToAgentID: "agent2"},
			{ToAgentID: "agent3"},
		}

		_, err := manager.ChainedHandoff(context.Background(), handoffs)
		if err == nil {
			t.Error("expected error for exceeding max depth, got nil")
		}
	})

	t.Run("empty handoff chain", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		_, err := manager.ChainedHandoff(context.Background(), []*AgentHandoff{})
		if !errors.Is(err, ErrEmptyHandoffChain) {
			t.Errorf("expected ErrEmptyHandoffChain, got %v", err)
		}
	})

	t.Run("handoff callbacks", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)

		var (
			startCalled    bool
			completeCalled bool
		)

		manager := NewHandoffManager(registry, router, logger, metrics, &HandoffManagerOptions{
			Callbacks: HandoffCallbacks{
				OnHandoffStart: func(h *AgentHandoff) {
					startCalled = true
				},
				OnHandoffComplete: func(r *HandoffResult) {
					completeCalled = true
				},
			},
		})

		agent := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Response")
		_ = registry.Register(agent)

		handoff := &AgentHandoff{
			ToAgentID: "agent1",
			Input:     "Test",
		}

		_, err := manager.Handoff(context.Background(), handoff)
		if err != nil {
			t.Fatalf("handoff failed: %v", err)
		}

		if !startCalled {
			t.Error("OnHandoffStart callback was not called")
		}

		if !completeCalled {
			t.Error("OnHandoffComplete callback was not called")
		}
	})
}

// --- AgentWithHandoff Tests ---

func TestAgentWithHandoff(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	t.Run("create agent with handoff", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		baseAgent := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Response")

		agentWithHandoff := NewAgentWithHandoff(baseAgent, manager)

		// Verify handoff tools were added
		tools := agentWithHandoff.tools
		hasHandoffTool := false
		hasRoutingTool := false

		for _, tool := range tools {
			if tool.Name == "handoff_to_agent" {
				hasHandoffTool = true
			}

			if tool.Name == "use_specialized_tool" {
				hasRoutingTool = true
			}
		}

		if !hasHandoffTool {
			t.Error("expected handoff_to_agent tool to be added")
		}

		if !hasRoutingTool {
			t.Error("expected use_specialized_tool tool to be added")
		}
	})

	t.Run("execute with handoff", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		baseAgent := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Response")
		_ = registry.Register(baseAgent)

		agentWithHandoff := NewAgentWithHandoff(baseAgent, manager)

		result, err := agentWithHandoff.ExecuteWithHandoff(context.Background(), "Hello")
		if err != nil {
			t.Fatalf("execute with handoff failed: %v", err)
		}

		if result.Response == nil {
			t.Fatal("expected response, got nil")
		}

		if result.ToAgentID != "agent1" {
			t.Errorf("expected ToAgentID 'agent1', got '%s'", result.ToAgentID)
		}
	})
}

// --- Context Tests ---

func TestAgentContext(t *testing.T) {
	t.Run("set and get context", func(t *testing.T) {
		agent := createHandoffTestAgent(t, "agent1", "Agent 1")

		agent.SetContext("key1", "value1")
		agent.SetContext("key2", 42)

		val1, ok := agent.GetContext("key1")
		if !ok {
			t.Error("expected key1 to exist")
		}

		if val1 != "value1" {
			t.Errorf("expected 'value1', got '%v'", val1)
		}

		val2, ok := agent.GetContext("key2")
		if !ok {
			t.Error("expected key2 to exist")
		}

		if val2 != 42 {
			t.Errorf("expected 42, got '%v'", val2)
		}
	})

	t.Run("get non-existent context", func(t *testing.T) {
		agent := createHandoffTestAgent(t, "agent1", "Agent 1")

		_, ok := agent.GetContext("non-existent")
		if ok {
			t.Error("expected key to not exist")
		}
	})

	t.Run("get all context", func(t *testing.T) {
		agent := createHandoffTestAgent(t, "agent1", "Agent 1")

		agent.SetContext("key1", "value1")
		agent.SetContext("key2", "value2")

		allContext := agent.GetAllContext()
		if len(allContext) != 2 {
			t.Errorf("expected 2 context entries, got %d", len(allContext))
		}
	})
}

// --- Helper functions ---

func createHandoffTestAgent(t *testing.T, id, name string) *Agent {
	t.Helper()

	mockLLM := testhelpers.NewMockLLM()
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()
	stateStore := &mockHandoffStateStore{}

	agent, err := NewAgent(id, name, mockLLM, stateStore, logger, metrics, nil)
	if err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}

	return agent
}

func createHandoffTestAgentWithResponse(t *testing.T, id, name, response string) *Agent {
	t.Helper()

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: response,
						},
						FinishReason: "stop",
					},
				},
			}, nil
		},
	}

	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()
	stateStore := &mockHandoffStateStore{}

	agent, err := NewAgent(id, name, mockLLM, stateStore, logger, metrics, &AgentOptions{
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}

	return agent
}

// --- Mock StateStore for testing ---

type mockHandoffStateStore struct{}

func (m *mockHandoffStateStore) Save(ctx context.Context, state *AgentState) error {
	return nil
}

func (m *mockHandoffStateStore) Load(ctx context.Context, agentID, sessionID string) (*AgentState, error) {
	return nil, ErrStateNotFound
}

func (m *mockHandoffStateStore) Delete(ctx context.Context, agentID, sessionID string) error {
	return nil
}

func (m *mockHandoffStateStore) List(ctx context.Context, agentID string) ([]string, error) {
	return []string{}, nil
}

// --- Registry Listener Tests ---

func TestAgentRegistryListener(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	t.Run("listener notifications", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)

		var (
			registeredID   string
			unregisteredID string
		)

		listener := &mockRegistryListener{
			onRegistered: func(agent *Agent) {
				registeredID = agent.ID
			},
			onUnregistered: func(id string) {
				unregisteredID = id
			},
		}

		registry.AddListener(listener)

		agent := createHandoffTestAgent(t, "agent1", "Agent 1")
		_ = registry.Register(agent)

		if registeredID != "agent1" {
			t.Errorf("expected registered ID 'agent1', got '%s'", registeredID)
		}

		_ = registry.Unregister("agent1")

		if unregisteredID != "agent1" {
			t.Errorf("expected unregistered ID 'agent1', got '%s'", unregisteredID)
		}
	})
}

type mockRegistryListener struct {
	onRegistered   func(*Agent)
	onUnregistered func(string)
}

func (m *mockRegistryListener) OnAgentRegistered(agent *Agent) {
	if m.onRegistered != nil {
		m.onRegistered(agent)
	}
}

func (m *mockRegistryListener) OnAgentUnregistered(agentID string) {
	if m.onUnregistered != nil {
		m.onUnregistered(agentID)
	}
}

// --- Handoff Tool Tests ---

func TestHandoffTools(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	t.Run("create handoff tool", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		tool := manager.CreateHandoffTool("agent1")

		if tool.Name != "handoff_to_agent" {
			t.Errorf("expected tool name 'handoff_to_agent', got '%s'", tool.Name)
		}

		if tool.Handler == nil {
			t.Error("expected tool handler to be set")
		}
	})

	t.Run("create tool routing tool", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		tool := manager.CreateToolRoutingTool("agent1")

		if tool.Name != "use_specialized_tool" {
			t.Errorf("expected tool name 'use_specialized_tool', got '%s'", tool.Name)
		}

		if tool.Handler == nil {
			t.Error("expected tool handler to be set")
		}
	})
}

// --- Handoff Timing Tests ---

func TestHandoffTiming(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	t.Run("handoff records duration", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		agent := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Response")
		_ = registry.Register(agent)

		handoff := &AgentHandoff{
			ToAgentID: "agent1",
			Input:     "Test",
		}

		result, err := manager.Handoff(context.Background(), handoff)
		if err != nil {
			t.Fatalf("handoff failed: %v", err)
		}

		if result.Duration <= 0 {
			t.Error("expected duration to be recorded")
		}

		if len(result.HandoffChain) != 1 {
			t.Fatalf("expected 1 handoff in chain, got %d", len(result.HandoffChain))
		}

		if result.HandoffChain[0].Timestamp.IsZero() {
			t.Error("expected timestamp to be recorded")
		}
	})
}

// --- Context Preservation Tests ---

func TestContextPreservation(t *testing.T) {
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	t.Run("preserve context during handoff", func(t *testing.T) {
		registry := NewAgentRegistry(logger, metrics)
		router := NewDefaultAgentRouter(registry, "", nil, logger, metrics)
		manager := NewHandoffManager(registry, router, logger, metrics, nil)

		agent1 := createHandoffTestAgentWithResponse(t, "agent1", "Agent 1", "Response 1")
		agent2 := createHandoffTestAgentWithResponse(t, "agent2", "Agent 2", "Response 2")
		_ = registry.Register(agent1)
		_ = registry.Register(agent2)

		handoff := &AgentHandoff{
			FromAgentID: "agent1",
			ToAgentID:   "agent2",
			Input:       "Test",
			Context: map[string]any{
				"custom_key": "custom_value",
			},
		}

		result, err := manager.Handoff(context.Background(), handoff)
		if err != nil {
			t.Fatalf("handoff failed: %v", err)
		}

		if result.HandoffChain[0].Context["custom_key"] != "custom_value" {
			t.Error("expected context to be preserved in handoff chain")
		}
	})
}
