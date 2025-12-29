package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

// --- AgentBuilder Tests ---

func TestAgentBuilder(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLogger := testhelpers.NewMockLogger()
	mockMetrics := testhelpers.NewMockMetrics()
	mockStateStore := &mockBuilderStateStore{}

	t.Run("build basic agent", func(t *testing.T) {
		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			WithLogger(mockLogger).
			WithMetrics(mockMetrics).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if agent.ID != "test-agent" {
			t.Errorf("expected ID 'test-agent', got '%s'", agent.ID)
		}
		if agent.Name != "Test Agent" {
			t.Errorf("expected name 'Test Agent', got '%s'", agent.Name)
		}
	})

	t.Run("build agent with description", func(t *testing.T) {
		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithDescription("A test agent for testing").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if agent.Description != "A test agent for testing" {
			t.Errorf("expected description to be set, got '%s'", agent.Description)
		}
	})

	t.Run("build agent with model and provider", func(t *testing.T) {
		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithModel("gpt-4").
			WithProvider("openai").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if agent.Model != "gpt-4" {
			t.Errorf("expected model 'gpt-4', got '%s'", agent.Model)
		}
		if agent.Provider != "openai" {
			t.Errorf("expected provider 'openai', got '%s'", agent.Provider)
		}
	})

	t.Run("build agent with tools", func(t *testing.T) {
		tool1 := Tool{Name: "tool1", Description: "Tool 1"}
		tool2 := Tool{Name: "tool2", Description: "Tool 2"}

		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			WithTool(tool1).
			WithTools(tool2).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if len(agent.tools) != 2 {
			t.Errorf("expected 2 tools, got %d", len(agent.tools))
		}
	})

	t.Run("build agent with system prompt", func(t *testing.T) {
		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithSystemPrompt("You are a helpful assistant").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if agent.systemPrompt != "You are a helpful assistant" {
			t.Errorf("expected system prompt to be set")
		}
	})

	t.Run("build agent with max iterations and temperature", func(t *testing.T) {
		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithMaxIterations(5).
			WithTemperature(0.5).
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if agent.maxIterations != 5 {
			t.Errorf("expected maxIterations 5, got %d", agent.maxIterations)
		}
		if agent.temperature != 0.5 {
			t.Errorf("expected temperature 0.5, got %f", agent.temperature)
		}
	})

	t.Run("build agent with callbacks", func(t *testing.T) {
		var startCalled, errorCalled, completeCalled bool

		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			OnStart(func(ctx context.Context) error {
				startCalled = true
				return nil
			}).
			OnError(func(err error) {
				errorCalled = true
			}).
			OnComplete(func(state *AgentState) {
				completeCalled = true
			}).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		// Verify callbacks are set
		if agent.callbacks.OnStart == nil {
			t.Error("OnStart callback not set")
		}
		if agent.callbacks.OnError == nil {
			t.Error("OnError callback not set")
		}
		if agent.callbacks.OnComplete == nil {
			t.Error("OnComplete callback not set")
		}

		// Suppress unused variable warnings
		_ = startCalled
		_ = errorCalled
		_ = completeCalled
	})

	t.Run("build fails without ID", func(t *testing.T) {
		_, err := NewAgentBuilder().
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			Build()

		if err != ErrAgentIDRequired {
			t.Errorf("expected ErrAgentIDRequired, got %v", err)
		}
	})

	t.Run("build fails without LLM manager", func(t *testing.T) {
		_, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithStateStore(mockStateStore).
			Build()

		if err == nil {
			t.Error("expected error for missing LLM manager, got nil")
		}
	})

	t.Run("build fails without state store", func(t *testing.T) {
		_, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			Build()

		if err == nil {
			t.Error("expected error for missing state store, got nil")
		}
	})

	t.Run("name defaults to ID", func(t *testing.T) {
		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if agent.Name != "test-agent" {
			t.Errorf("expected name to default to ID 'test-agent', got '%s'", agent.Name)
		}
	})

	t.Run("build with guardrails", func(t *testing.T) {
		guardrails := NewGuardrailManager(mockLogger, mockMetrics, nil)

		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			WithGuardrails(guardrails).
			Build()

		if err != nil {
			t.Fatalf("failed to build agent: %v", err)
		}

		if agent.guardrails == nil {
			t.Error("expected guardrails to be set")
		}
	})
}

func TestAgentBuilderWithHandoff(t *testing.T) {
	mockLLM := testhelpers.NewMockLLM()
	mockLogger := testhelpers.NewMockLogger()
	mockMetrics := testhelpers.NewMockMetrics()
	mockStateStore := &mockBuilderStateStore{}

	t.Run("build with handoff manager fails without manager", func(t *testing.T) {
		_, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			BuildWithHandoff()

		if err == nil {
			t.Error("expected error for missing handoff manager, got nil")
		}
	})

	t.Run("build with handoff manager succeeds", func(t *testing.T) {
		registry := NewAgentRegistry(mockLogger, mockMetrics)
		router := NewDefaultAgentRouter(registry, "", mockLLM, mockLogger, mockMetrics)
		handoffManager := NewHandoffManager(registry, router, mockLogger, mockMetrics, nil)

		agent, err := NewAgentBuilder().
			WithID("test-agent").
			WithName("Test Agent").
			WithLLMManager(mockLLM).
			WithStateStore(mockStateStore).
			WithHandoffManager(handoffManager).
			BuildWithHandoff()

		if err != nil {
			t.Fatalf("failed to build agent with handoff: %v", err)
		}

		if agent == nil {
			t.Fatal("expected agent with handoff, got nil")
		}
	})
}

// --- WorkflowBuilder Tests ---

func TestWorkflowBuilder(t *testing.T) {
	mockLogger := testhelpers.NewMockLogger()
	mockMetrics := testhelpers.NewMockMetrics()

	t.Run("build basic workflow", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			WithLogger(mockLogger).
			WithMetrics(mockMetrics).
			AddNode(&WorkflowNode{
				ID:   "node1",
				Type: NodeTypeTool,
				Name: "Node 1",
			}).
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		if workflow.ID != "test-workflow" {
			t.Errorf("expected ID 'test-workflow', got '%s'", workflow.ID)
		}
		if workflow.Name != "Test Workflow" {
			t.Errorf("expected name 'Test Workflow', got '%s'", workflow.Name)
		}
	})

	t.Run("build workflow with description", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			WithDescription("A test workflow").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		if workflow.Description != "A test workflow" {
			t.Errorf("expected description 'A test workflow', got '%s'", workflow.Description)
		}
	})

	t.Run("build workflow with version", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			WithVersion("2.0.0").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		if workflow.Version != "2.0.0" {
			t.Errorf("expected version '2.0.0', got '%s'", workflow.Version)
		}
	})

	t.Run("build workflow with edges", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			AddNode(&WorkflowNode{ID: "node2", Type: NodeTypeTool, Name: "Node 2"}).
			AddEdge("node1", "node2").
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		if len(workflow.Edges["node1"]) != 1 {
			t.Errorf("expected 1 edge from node1, got %d", len(workflow.Edges["node1"]))
		}
		if workflow.Edges["node1"][0] != "node2" {
			t.Errorf("expected edge to node2, got %s", workflow.Edges["node1"][0])
		}
	})

	t.Run("build workflow with sequence", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			AddNode(&WorkflowNode{ID: "node2", Type: NodeTypeTool, Name: "Node 2"}).
			AddNode(&WorkflowNode{ID: "node3", Type: NodeTypeTool, Name: "Node 3"}).
			AddSequence("node1", "node2", "node3").
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		// Verify edges
		if len(workflow.Edges["node1"]) != 1 || workflow.Edges["node1"][0] != "node2" {
			t.Error("expected edge node1 -> node2")
		}
		if len(workflow.Edges["node2"]) != 1 || workflow.Edges["node2"][0] != "node3" {
			t.Error("expected edge node2 -> node3")
		}
	})

	t.Run("build fails without ID", func(t *testing.T) {
		_, err := NewWorkflowBuilder().
			WithName("Test Workflow").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			SetStartNode("node1").
			Build()

		if err == nil {
			t.Error("expected error for missing ID, got nil")
		}
	})

	t.Run("build fails without nodes", func(t *testing.T) {
		_, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			Build()

		if err == nil {
			t.Error("expected error for missing nodes, got nil")
		}
	})

	t.Run("build fails without start nodes", func(t *testing.T) {
		_, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			Build()

		if err != ErrWorkflowNoEntryPoint {
			t.Errorf("expected ErrWorkflowNoEntryPoint, got %v", err)
		}
	})

	t.Run("name defaults to ID", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		if workflow.Name != "test-workflow" {
			t.Errorf("expected name to default to ID, got '%s'", workflow.Name)
		}
	})

	t.Run("build workflow with multiple start nodes", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			AddNode(&WorkflowNode{ID: "node2", Type: NodeTypeTool, Name: "Node 2"}).
			SetStartNodes("node1", "node2").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		if len(workflow.StartNodes) != 2 {
			t.Errorf("expected 2 start nodes, got %d", len(workflow.StartNodes))
		}
	})
}

func TestWorkflowBuilderNodeTypes(t *testing.T) {
	mockLogger := testhelpers.NewMockLogger()
	mockMetrics := testhelpers.NewMockMetrics()
	mockStateStore := &mockBuilderStateStore{}

	t.Run("add agent node", func(t *testing.T) {
		mockLLM := testhelpers.NewMockLLM()
		agent, _ := NewAgent("agent1", "Agent 1", mockLLM, mockStateStore, mockLogger, mockMetrics, nil)

		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			AddAgentNode("node1", "Agent Node", agent).
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		node, _ := workflow.GetNode("node1")
		if node.Type != NodeTypeAgent {
			t.Errorf("expected NodeTypeAgent, got %s", node.Type)
		}
		if node.AgentID != "agent1" {
			t.Errorf("expected AgentID 'agent1', got '%s'", node.AgentID)
		}
	})

	t.Run("add tool node", func(t *testing.T) {
		tool := Tool{Name: "test_tool", Description: "A test tool"}

		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			AddToolNode("node1", "Tool Node", tool).
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		node, _ := workflow.GetNode("node1")
		if node.Type != NodeTypeTool {
			t.Errorf("expected NodeTypeTool, got %s", node.Type)
		}
		if node.ToolName != "test_tool" {
			t.Errorf("expected ToolName 'test_tool', got '%s'", node.ToolName)
		}
	})

	t.Run("add condition node", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			AddConditionNode("node1", "Condition Node", "result > 10").
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		node, _ := workflow.GetNode("node1")
		if node.Type != NodeTypeCondition {
			t.Errorf("expected NodeTypeCondition, got %s", node.Type)
		}
		if node.Condition != "result > 10" {
			t.Errorf("expected Condition 'result > 10', got '%s'", node.Condition)
		}
	})

	t.Run("add transform node", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			AddTransformNode("node1", "Transform Node", "value * 2").
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		node, _ := workflow.GetNode("node1")
		if node.Type != NodeTypeTransform {
			t.Errorf("expected NodeTypeTransform, got %s", node.Type)
		}
		if node.Transform != "value * 2" {
			t.Errorf("expected Transform 'value * 2', got '%s'", node.Transform)
		}
	})

	t.Run("add wait node", func(t *testing.T) {
		workflow, err := NewWorkflowBuilder().
			WithID("test-workflow").
			AddWaitNode("node1", "Wait Node", 5*time.Second).
			SetStartNode("node1").
			Build()

		if err != nil {
			t.Fatalf("failed to build workflow: %v", err)
		}

		node, _ := workflow.GetNode("node1")
		if node.Type != NodeTypeWait {
			t.Errorf("expected NodeTypeWait, got %s", node.Type)
		}
		if node.Config["duration"] != 5*time.Second {
			t.Errorf("expected duration 5s, got %v", node.Config["duration"])
		}
	})
}

func TestWorkflowBuilderWithRegistries(t *testing.T) {
	mockLogger := testhelpers.NewMockLogger()
	mockMetrics := testhelpers.NewMockMetrics()

	t.Run("build with registries", func(t *testing.T) {
		toolRegistry := NewToolRegistry(mockLogger, mockMetrics)
		agentRegistry := NewAgentRegistry(mockLogger, mockMetrics)

		result, err := NewWorkflowBuilder().
			WithID("test-workflow").
			WithName("Test Workflow").
			WithToolRegistry(toolRegistry).
			WithAgentRegistry(agentRegistry).
			AddNode(&WorkflowNode{ID: "node1", Type: NodeTypeTool, Name: "Node 1"}).
			SetStartNode("node1").
			BuildWithRegistries()

		if err != nil {
			t.Fatalf("failed to build workflow with registries: %v", err)
		}

		if result.ToolRegistry == nil {
			t.Error("expected tool registry to be set")
		}
		if result.AgentRegistry == nil {
			t.Error("expected agent registry to be set")
		}
	})
}

// --- Mock StateStore for testing ---

type mockBuilderStateStore struct{}

func (m *mockBuilderStateStore) Save(ctx context.Context, state *AgentState) error {
	return nil
}

func (m *mockBuilderStateStore) Load(ctx context.Context, agentID, sessionID string) (*AgentState, error) {
	return nil, ErrStateNotFound
}

func (m *mockBuilderStateStore) Delete(ctx context.Context, agentID, sessionID string) error {
	return nil
}

func (m *mockBuilderStateStore) List(ctx context.Context, agentID string) ([]string, error) {
	return []string{}, nil
}
