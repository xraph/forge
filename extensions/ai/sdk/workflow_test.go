package sdk

import (
	"context"
	"testing"
	"time"
)

// Test NewWorkflow

func TestNewWorkflow(t *testing.T) {
	wf := NewWorkflow("test_wf", "Test Workflow", nil, nil)

	if wf == nil {
		t.Fatal("expected workflow to be created")
	}

	if wf.ID != "test_wf" {
		t.Errorf("expected ID 'test_wf', got '%s'", wf.ID)
	}

	if len(wf.Nodes) != 0 {
		t.Error("expected empty nodes initially")
	}
}

// Test AddNode

func TestWorkflow_AddNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:   "node1",
		Type: NodeTypeTool,
		Name: "Test Node",
	}

	err := wf.AddNode(node)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(wf.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(wf.Nodes))
	}

	if node.Status != NodeStatusPending {
		t.Errorf("expected status pending, got %s", node.Status)
	}
}

func TestWorkflow_AddNode_NoID(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		Type: NodeTypeTool,
	}

	err := wf.AddNode(node)

	if err == nil {
		t.Error("expected error for node without ID")
	}
}

func TestWorkflow_AddNode_Duplicate(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:   "node1",
		Type: NodeTypeTool,
	}

	wf.AddNode(node)
	err := wf.AddNode(node)

	if err == nil {
		t.Error("expected error for duplicate node")
	}
}

// Test AddEdge

func TestWorkflow_AddEdge(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool}
	node2 := &WorkflowNode{ID: "node2", Type: NodeTypeTool}

	wf.AddNode(node1)
	wf.AddNode(node2)

	err := wf.AddEdge("node1", "node2")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(wf.Edges["node1"]) != 1 {
		t.Errorf("expected 1 edge from node1, got %d", len(wf.Edges["node1"]))
	}
}

func TestWorkflow_AddEdge_NonexistentNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool}
	wf.AddNode(node1)

	err := wf.AddEdge("node1", "nonexistent")

	if err == nil {
		t.Error("expected error for nonexistent target node")
	}
}

func TestWorkflow_AddEdge_Cycle(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool}
	node2 := &WorkflowNode{ID: "node2", Type: NodeTypeTool}
	node3 := &WorkflowNode{ID: "node3", Type: NodeTypeTool}

	wf.AddNode(node1)
	wf.AddNode(node2)
	wf.AddNode(node3)

	wf.AddEdge("node1", "node2")
	wf.AddEdge("node2", "node3")

	// This would create a cycle
	err := wf.AddEdge("node3", "node1")

	if err == nil {
		t.Error("expected error for edge creating cycle")
	}
}

// Test SetStartNode

func TestWorkflow_SetStartNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{ID: "node1", Type: NodeTypeTool}
	wf.AddNode(node)

	err := wf.SetStartNode("node1")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(wf.StartNodes) != 1 {
		t.Errorf("expected 1 start node, got %d", len(wf.StartNodes))
	}
}

func TestWorkflow_SetStartNode_Nonexistent(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	err := wf.SetStartNode("nonexistent")

	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

// Test Execute

func TestWorkflow_Execute_Simple(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:       "wait_node",
		Type:     NodeTypeWait,
		Name:     "Wait Node",
		Timeout:  1 * time.Second,
		Config:   map[string]interface{}{"duration": 10 * time.Millisecond},
	}

	wf.AddNode(node)
	wf.SetStartNode("wait_node")

	execution, err := wf.Execute(context.Background(), map[string]interface{}{})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if execution.Status != WorkflowStatusCompleted {
		t.Errorf("expected status completed, got %s", execution.Status)
	}
}

func TestWorkflow_Execute_NoStartNodes(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{ID: "node1", Type: NodeTypeTool}
	wf.AddNode(node)

	_, err := wf.Execute(context.Background(), map[string]interface{}{})

	if err == nil {
		t.Error("expected error for workflow without start nodes")
	}
}

func TestWorkflow_Execute_Sequence(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{
		ID:      "node1",
		Type:    NodeTypeWait,
		Config:  map[string]interface{}{"duration": 10 * time.Millisecond},
		Timeout: 1 * time.Second,
	}
	node2 := &WorkflowNode{
		ID:      "node2",
		Type:    NodeTypeWait,
		Config:  map[string]interface{}{"duration": 10 * time.Millisecond},
		Timeout: 1 * time.Second,
	}

	wf.AddNode(node1)
	wf.AddNode(node2)
	wf.AddEdge("node1", "node2")
	wf.SetStartNode("node1")

	execution, err := wf.Execute(context.Background(), map[string]interface{}{})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if execution.Status != WorkflowStatusCompleted {
		t.Errorf("expected status completed, got %s", execution.Status)
	}

	if len(execution.NodeExecutions) != 2 {
		t.Errorf("expected 2 node executions, got %d", len(execution.NodeExecutions))
	}
}

func TestWorkflow_Execute_Parallel(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	start := &WorkflowNode{
		ID:      "start",
		Type:    NodeTypeWait,
		Config:  map[string]interface{}{"duration": 10 * time.Millisecond},
		Timeout: 1 * time.Second,
	}
	parallel1 := &WorkflowNode{
		ID:      "parallel1",
		Type:    NodeTypeWait,
		Config:  map[string]interface{}{"duration": 10 * time.Millisecond},
		Timeout: 1 * time.Second,
	}
	parallel2 := &WorkflowNode{
		ID:      "parallel2",
		Type:    NodeTypeWait,
		Config:  map[string]interface{}{"duration": 10 * time.Millisecond},
		Timeout: 1 * time.Second,
	}

	wf.AddNode(start)
	wf.AddNode(parallel1)
	wf.AddNode(parallel2)
	wf.AddEdge("start", "parallel1")
	wf.AddEdge("start", "parallel2")
	wf.SetStartNode("start")

	execution, err := wf.Execute(context.Background(), map[string]interface{}{})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if execution.Status != WorkflowStatusCompleted {
		t.Errorf("expected status completed, got %s", execution.Status)
	}

	if len(execution.NodeExecutions) != 3 {
		t.Errorf("expected 3 node executions, got %d", len(execution.NodeExecutions))
	}
}

func TestWorkflow_Execute_ContextCancellation(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:      "slow_node",
		Type:    NodeTypeWait,
		Config:  map[string]interface{}{"duration": 1 * time.Second},
		Timeout: 5 * time.Second,
	}

	wf.AddNode(node)
	wf.SetStartNode("slow_node")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	execution, err := wf.Execute(ctx, map[string]interface{}{})

	if err == nil {
		t.Error("expected error from context cancellation")
	}

	if execution.Status != WorkflowStatusFailed {
		t.Errorf("expected status failed, got %s", execution.Status)
	}
}

// Test Validate

func TestWorkflow_Validate_Success(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:       "tool_node",
		Type:     NodeTypeTool,
		ToolName: "test_tool",
	}

	wf.AddNode(node)
	wf.SetStartNode("tool_node")

	err := wf.validate()

	if err != nil {
		t.Errorf("expected no validation error, got %v", err)
	}
}

func TestWorkflow_Validate_MissingToolName(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:   "tool_node",
		Type: NodeTypeTool,
	}

	wf.AddNode(node)

	err := wf.validate()

	if err == nil {
		t.Error("expected validation error for tool node without tool name")
	}
}

func TestWorkflow_Validate_MissingAgentID(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:   "agent_node",
		Type: NodeTypeAgent,
	}

	wf.AddNode(node)

	err := wf.validate()

	if err == nil {
		t.Error("expected validation error for agent node without agent ID")
	}
}

// Test Cycle Detection

func TestWorkflow_HasCycle_NoCycle(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool, ToolName: "tool1"}
	node2 := &WorkflowNode{ID: "node2", Type: NodeTypeTool, ToolName: "tool2"}
	node3 := &WorkflowNode{ID: "node3", Type: NodeTypeTool, ToolName: "tool3"}

	wf.AddNode(node1)
	wf.AddNode(node2)
	wf.AddNode(node3)
	wf.AddEdge("node1", "node2")
	wf.AddEdge("node2", "node3")

	if wf.hasCycle() {
		t.Error("expected no cycle")
	}
}

func TestWorkflow_HasCycle_WithCycle(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool, ToolName: "tool1"}
	node2 := &WorkflowNode{ID: "node2", Type: NodeTypeTool, ToolName: "tool2"}
	node3 := &WorkflowNode{ID: "node3", Type: NodeTypeTool, ToolName: "tool3"}

	wf.AddNode(node1)
	wf.AddNode(node2)
	wf.AddNode(node3)
	
	// Manually create a cycle (bypassing AddEdge validation)
	wf.Edges["node1"] = []string{"node2"}
	wf.Edges["node2"] = []string{"node3"}
	wf.Edges["node3"] = []string{"node1"}

	if !wf.hasCycle() {
		t.Error("expected cycle to be detected")
	}
}

// Test Node Operations

func TestWorkflow_GetNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	original := &WorkflowNode{
		ID:   "node1",
		Type: NodeTypeTool,
		Name: "Test Node",
	}

	wf.AddNode(original)

	retrieved, err := wf.GetNode("node1")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if retrieved.ID != "node1" {
		t.Errorf("expected ID 'node1', got '%s'", retrieved.ID)
	}
}

func TestWorkflow_GetNode_NotFound(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	_, err := wf.GetNode("nonexistent")

	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

func TestWorkflow_RemoveNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool, ToolName: "tool1"}
	node2 := &WorkflowNode{ID: "node2", Type: NodeTypeTool, ToolName: "tool2"}

	wf.AddNode(node1)
	wf.AddNode(node2)
	wf.AddEdge("node1", "node2")
	wf.SetStartNode("node1")

	err := wf.RemoveNode("node1")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(wf.Nodes) != 1 {
		t.Errorf("expected 1 node after removal, got %d", len(wf.Nodes))
	}

	if len(wf.StartNodes) != 0 {
		t.Errorf("expected 0 start nodes after removal, got %d", len(wf.StartNodes))
	}
}

func TestWorkflow_RemoveNode_NotFound(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	err := wf.RemoveNode("nonexistent")

	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

// Test Node Execution Types

func TestWorkflow_ExecuteToolNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:       "tool_node",
		Type:     NodeTypeTool,
		ToolName: "test_tool",
	}

	result, err := wf.executeToolNode(context.Background(), node)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result == nil {
		t.Error("expected result from tool node")
	}
}

func TestWorkflow_ExecuteAgentNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:      "agent_node",
		Type:    NodeTypeAgent,
		AgentID: "test_agent",
	}

	result, err := wf.executeAgentNode(context.Background(), node)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result == nil {
		t.Error("expected result from agent node")
	}
}

func TestWorkflow_ExecuteConditionNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:        "condition_node",
		Type:      NodeTypeCondition,
		Condition: "x > 5",
	}

	execution := &WorkflowExecution{
		NodeExecutions: make(map[string]*NodeExecution),
	}

	result, err := wf.executeConditionNode(context.Background(), node, execution)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result == nil {
		t.Error("expected result from condition node")
	}
}

func TestWorkflow_ExecuteTransformNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:        "transform_node",
		Type:      NodeTypeTransform,
		Transform: "uppercase",
	}

	execution := &WorkflowExecution{
		NodeExecutions: make(map[string]*NodeExecution),
	}

	result, err := wf.executeTransformNode(context.Background(), node, execution)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result == nil {
		t.Error("expected result from transform node")
	}
}

func TestWorkflow_ExecuteWaitNode(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{
		ID:     "wait_node",
		Type:   NodeTypeWait,
		Config: map[string]interface{}{"duration": 10 * time.Millisecond},
	}

	start := time.Now()
	result, err := wf.executeWaitNode(context.Background(), node)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result == nil {
		t.Error("expected result from wait node")
	}

	if duration < 10*time.Millisecond {
		t.Errorf("expected wait of at least 10ms, got %v", duration)
	}
}

// Test isNodeReady

func TestWorkflow_IsNodeReady_NoParents(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{ID: "node1", Type: NodeTypeTool, ToolName: "tool1"}
	wf.AddNode(node)

	completed := make(map[string]bool)
	executing := make(map[string]bool)

	ready := wf.isNodeReady("node1", completed, executing)

	if !ready {
		t.Error("expected node with no parents to be ready")
	}
}

func TestWorkflow_IsNodeReady_ParentCompleted(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool, ToolName: "tool1"}
	node2 := &WorkflowNode{ID: "node2", Type: NodeTypeTool, ToolName: "tool2"}

	wf.AddNode(node1)
	wf.AddNode(node2)
	wf.AddEdge("node1", "node2")

	completed := map[string]bool{"node1": true}
	executing := make(map[string]bool)

	ready := wf.isNodeReady("node2", completed, executing)

	if !ready {
		t.Error("expected node to be ready when parent is completed")
	}
}

func TestWorkflow_IsNodeReady_ParentNotCompleted(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node1 := &WorkflowNode{ID: "node1", Type: NodeTypeTool, ToolName: "tool1"}
	node2 := &WorkflowNode{ID: "node2", Type: NodeTypeTool, ToolName: "tool2"}

	wf.AddNode(node1)
	wf.AddNode(node2)
	wf.AddEdge("node1", "node2")

	completed := make(map[string]bool)
	executing := make(map[string]bool)

	ready := wf.isNodeReady("node2", completed, executing)

	if ready {
		t.Error("expected node not to be ready when parent is not completed")
	}
}

func TestWorkflow_IsNodeReady_AlreadyExecuting(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	node := &WorkflowNode{ID: "node1", Type: NodeTypeTool, ToolName: "tool1"}
	wf.AddNode(node)

	completed := make(map[string]bool)
	executing := map[string]bool{"node1": true}

	ready := wf.isNodeReady("node1", completed, executing)

	if ready {
		t.Error("expected node not to be ready when already executing")
	}
}

// Test Thread Safety

func TestWorkflow_ThreadSafety(t *testing.T) {
	wf := NewWorkflow("test", "Test", nil, nil)

	done := make(chan bool)

	// Concurrent node additions
	for i := 0; i < 5; i++ {
		go func(index int) {
			node := &WorkflowNode{
				ID:   string(rune('a' + index)),
				Type: NodeTypeTool,
				ToolName: "tool",
			}
			wf.AddNode(node)
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			wf.GetNode("a")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 5 nodes
	if len(wf.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(wf.Nodes))
	}
}

