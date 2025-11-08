package sdk

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// Workflow represents a DAG-based workflow.
type Workflow struct {
	ID          string
	Name        string
	Description string
	Nodes       map[string]*WorkflowNode
	Edges       map[string][]string // node_id -> []dependent_node_ids
	StartNodes  []string
	Version     string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	logger  forge.Logger
	metrics forge.Metrics
	mu      sync.RWMutex
}

// WorkflowNode represents a single step in the workflow.
type WorkflowNode struct {
	ID          string
	Type        NodeType
	Name        string
	Description string
	Config      map[string]any
	Retry       *RetryConfig
	Timeout     time.Duration

	// For agent nodes
	AgentID string

	// For tool nodes
	ToolName    string
	ToolVersion string
	ToolParams  map[string]any

	// For condition nodes
	Condition string

	// For transform nodes
	Transform string

	// Execution state
	Status    NodeStatus
	StartTime time.Time
	EndTime   time.Time
	Result    any
	Error     error
	Attempts  int
}

// NodeType defines the type of workflow node.
type NodeType string

const (
	NodeTypeAgent     NodeType = "agent"
	NodeTypeTool      NodeType = "tool"
	NodeTypeCondition NodeType = "condition"
	NodeTypeTransform NodeType = "transform"
	NodeTypeParallel  NodeType = "parallel"
	NodeTypeSequence  NodeType = "sequence"
	NodeTypeWait      NodeType = "wait"
)

// NodeStatus represents the execution status of a node.
type NodeStatus string

const (
	NodeStatusPending   NodeStatus = "pending"
	NodeStatusRunning   NodeStatus = "running"
	NodeStatusCompleted NodeStatus = "completed"
	NodeStatusFailed    NodeStatus = "failed"
	NodeStatusSkipped   NodeStatus = "skipped"
)

// WorkflowExecution represents an execution instance of a workflow.
type WorkflowExecution struct {
	ID             string
	WorkflowID     string
	Status         WorkflowStatus
	StartTime      time.Time
	EndTime        time.Time
	Input          map[string]any
	Output         map[string]any
	NodeExecutions map[string]*NodeExecution
	Error          error

	mu sync.RWMutex
}

// NodeExecution represents the execution of a single node.
type NodeExecution struct {
	NodeID    string
	Status    NodeStatus
	StartTime time.Time
	EndTime   time.Time
	Input     map[string]any
	Output    any
	Error     error
	Attempts  int
}

// WorkflowStatus represents the overall workflow execution status.
type WorkflowStatus string

const (
	WorkflowStatusPending   WorkflowStatus = "pending"
	WorkflowStatusRunning   WorkflowStatus = "running"
	WorkflowStatusCompleted WorkflowStatus = "completed"
	WorkflowStatusFailed    WorkflowStatus = "failed"
	WorkflowStatusCancelled WorkflowStatus = "cancelled"
)

// NewWorkflow creates a new workflow.
func NewWorkflow(id, name string, logger forge.Logger, metrics forge.Metrics) *Workflow {
	return &Workflow{
		ID:         id,
		Name:       name,
		Nodes:      make(map[string]*WorkflowNode),
		Edges:      make(map[string][]string),
		StartNodes: make([]string, 0),
		Version:    "1.0.0",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		logger:     logger,
		metrics:    metrics,
	}
}

// AddNode adds a node to the workflow.
func (w *Workflow) AddNode(node *WorkflowNode) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if node.ID == "" {
		return errors.New("node ID is required")
	}

	if _, exists := w.Nodes[node.ID]; exists {
		return fmt.Errorf("node %s already exists", node.ID)
	}

	if node.Timeout == 0 {
		node.Timeout = 5 * time.Minute
	}

	node.Status = NodeStatusPending

	w.Nodes[node.ID] = node
	w.UpdatedAt = time.Now()

	if w.logger != nil {
		w.logger.Debug("Node added to workflow",
			F("workflow", w.ID),
			F("node", node.ID),
			F("type", node.Type),
		)
	}

	return nil
}

// AddEdge adds a dependency edge between nodes.
func (w *Workflow) AddEdge(fromNode, toNode string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Verify nodes exist
	if _, exists := w.Nodes[fromNode]; !exists {
		return fmt.Errorf("source node %s not found", fromNode)
	}

	if _, exists := w.Nodes[toNode]; !exists {
		return fmt.Errorf("target node %s not found", toNode)
	}

	// Check for cycles
	if w.wouldCreateCycle(fromNode, toNode) {
		return fmt.Errorf("adding edge %s -> %s would create a cycle", fromNode, toNode)
	}

	w.Edges[fromNode] = append(w.Edges[fromNode], toNode)
	w.UpdatedAt = time.Now()

	return nil
}

// SetStartNode marks a node as a starting point.
func (w *Workflow) SetStartNode(nodeID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.Nodes[nodeID]; !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Check if already a start node
	if slices.Contains(w.StartNodes, nodeID) {
		return nil
	}

	w.StartNodes = append(w.StartNodes, nodeID)

	return nil
}

// Execute runs the workflow.
func (w *Workflow) Execute(ctx context.Context, input map[string]any) (*WorkflowExecution, error) {
	w.mu.RLock()

	if len(w.StartNodes) == 0 {
		w.mu.RUnlock()

		return nil, errors.New("workflow has no start nodes")
	}

	if err := w.validate(); err != nil {
		w.mu.RUnlock()

		return nil, fmt.Errorf("workflow validation failed: %w", err)
	}

	w.mu.RUnlock()

	execution := &WorkflowExecution{
		ID:             fmt.Sprintf("%s_%d", w.ID, time.Now().Unix()),
		WorkflowID:     w.ID,
		Status:         WorkflowStatusRunning,
		StartTime:      time.Now(),
		Input:          input,
		NodeExecutions: make(map[string]*NodeExecution),
	}

	if w.logger != nil {
		w.logger.Info("Starting workflow execution",
			F("workflow", w.ID),
			F("execution", execution.ID),
		)
	}

	// Execute workflow DAG
	err := w.executeDAG(ctx, execution)

	execution.EndTime = time.Now()
	if err != nil {
		execution.Status = WorkflowStatusFailed
		execution.Error = err
	} else {
		execution.Status = WorkflowStatusCompleted
	}

	if w.metrics != nil {
		w.metrics.Counter("forge.ai.sdk.workflow.executions",
			"workflow", w.ID,
			"status", string(execution.Status),
		).Inc()

		w.metrics.Histogram("forge.ai.sdk.workflow.duration",
			"workflow", w.ID,
		).Observe(time.Since(execution.StartTime).Seconds())
	}

	return execution, err
}

// executeDAG executes the workflow DAG.
func (w *Workflow) executeDAG(ctx context.Context, execution *WorkflowExecution) error {
	// Track completed nodes
	completed := make(map[string]bool)
	executing := make(map[string]bool)

	// Start with start nodes
	toExecute := make([]string, len(w.StartNodes))
	copy(toExecute, w.StartNodes)

	for len(toExecute) > 0 {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Find nodes ready to execute (all dependencies completed)
		readyNodes := make([]string, 0)

		for _, nodeID := range toExecute {
			if w.isNodeReady(nodeID, completed, executing) {
				readyNodes = append(readyNodes, nodeID)
			}
		}

		if len(readyNodes) == 0 {
			// Check if we're stuck (all remaining nodes waiting on failed nodes)
			if len(toExecute) > 0 {
				return fmt.Errorf("workflow stuck: %d nodes waiting but none ready", len(toExecute))
			}

			break
		}

		// Execute ready nodes in parallel
		var wg sync.WaitGroup

		errors := make(chan error, len(readyNodes))

		for _, nodeID := range readyNodes {
			wg.Add(1)

			executing[nodeID] = true

			go func(nid string) {
				defer wg.Done()

				err := w.executeNode(ctx, execution, nid)
				if err != nil {
					errors <- fmt.Errorf("node %s failed: %w", nid, err)
				}

				execution.mu.Lock()

				completed[nid] = true
				delete(executing, nid)
				execution.mu.Unlock()

				// Add dependent nodes to execution queue
				w.mu.RLock()
				dependents := w.Edges[nid]
				w.mu.RUnlock()

				execution.mu.Lock()

				for _, dep := range dependents {
					if !completed[dep] && !executing[dep] {
						toExecute = append(toExecute, dep)
					}
				}

				execution.mu.Unlock()
			}(nodeID)
		}

		// Wait for this batch to complete
		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			if err != nil {
				return err
			}
		}

		// Remove completed nodes from toExecute
		newToExecute := make([]string, 0)

		for _, nodeID := range toExecute {
			if !completed[nodeID] {
				newToExecute = append(newToExecute, nodeID)
			}
		}

		toExecute = newToExecute
	}

	return nil
}

// executeNode executes a single workflow node.
func (w *Workflow) executeNode(ctx context.Context, execution *WorkflowExecution, nodeID string) error {
	w.mu.RLock()
	node := w.Nodes[nodeID]
	w.mu.RUnlock()

	nodeExec := &NodeExecution{
		NodeID:    nodeID,
		Status:    NodeStatusRunning,
		StartTime: time.Now(),
		Input:     make(map[string]any),
		Attempts:  1,
	}

	execution.mu.Lock()
	execution.NodeExecutions[nodeID] = nodeExec
	execution.mu.Unlock()

	// Create context with timeout
	nodeCtx, cancel := context.WithTimeout(ctx, node.Timeout)
	defer cancel()

	var (
		err    error
		result any
	)

	// Execute based on node type

	switch node.Type {
	case NodeTypeTool:
		result, err = w.executeToolNode(nodeCtx, node)
	case NodeTypeAgent:
		result, err = w.executeAgentNode(nodeCtx, node)
	case NodeTypeCondition:
		result, err = w.executeConditionNode(nodeCtx, node, execution)
	case NodeTypeTransform:
		result, err = w.executeTransformNode(nodeCtx, node, execution)
	case NodeTypeWait:
		result, err = w.executeWaitNode(nodeCtx, node)
	default:
		err = fmt.Errorf("unsupported node type: %s", node.Type)
	}

	nodeExec.EndTime = time.Now()
	nodeExec.Output = result
	nodeExec.Error = err

	if err != nil {
		nodeExec.Status = NodeStatusFailed

		if w.logger != nil {
			w.logger.Warn("Node execution failed",
				F("workflow", w.ID),
				F("node", nodeID),
				F("error", err.Error()),
			)
		}

		return err
	}

	nodeExec.Status = NodeStatusCompleted

	if w.logger != nil {
		w.logger.Debug("Node execution completed",
			F("workflow", w.ID),
			F("node", nodeID),
			F("duration", nodeExec.EndTime.Sub(nodeExec.StartTime)),
		)
	}

	return nil
}

// executeToolNode executes a tool node.
func (w *Workflow) executeToolNode(ctx context.Context, node *WorkflowNode) (any, error) {
	// This would integrate with the ToolRegistry
	// For now, return a placeholder
	return map[string]any{
		"tool":   node.ToolName,
		"status": "executed",
	}, nil
}

// executeAgentNode executes an agent node.
func (w *Workflow) executeAgentNode(ctx context.Context, node *WorkflowNode) (any, error) {
	// This would integrate with the Agent system
	return map[string]any{
		"agent":  node.AgentID,
		"status": "executed",
	}, nil
}

// executeConditionNode executes a conditional node.
func (w *Workflow) executeConditionNode(ctx context.Context, node *WorkflowNode, execution *WorkflowExecution) (any, error) {
	// Evaluate condition based on previous node outputs
	// Simplified implementation
	return map[string]any{
		"condition": node.Condition,
		"result":    true,
	}, nil
}

// executeTransformNode executes a data transformation node.
func (w *Workflow) executeTransformNode(ctx context.Context, node *WorkflowNode, execution *WorkflowExecution) (any, error) {
	// Transform data from previous nodes
	return map[string]any{
		"transform": node.Transform,
		"status":    "transformed",
	}, nil
}

// executeWaitNode executes a wait/delay node.
func (w *Workflow) executeWaitNode(ctx context.Context, node *WorkflowNode) (any, error) {
	// Default wait time
	waitTime := 1 * time.Second
	if duration, ok := node.Config["duration"].(time.Duration); ok {
		waitTime = duration
	}

	select {
	case <-time.After(waitTime):
		return map[string]any{"waited": waitTime.String()}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// isNodeReady checks if a node's dependencies are satisfied.
func (w *Workflow) isNodeReady(nodeID string, completed, executing map[string]bool) bool {
	if completed[nodeID] || executing[nodeID] {
		return false
	}

	// Check if all parent nodes are completed
	w.mu.RLock()
	defer w.mu.RUnlock()

	for parentID, children := range w.Edges {
		for _, childID := range children {
			if childID == nodeID && !completed[parentID] {
				return false
			}
		}
	}

	return true
}

// validate validates the workflow structure.
func (w *Workflow) validate() error {
	// Check for cycles
	if w.hasCycle() {
		return errors.New("workflow contains cycles")
	}

	// Validate node types
	for _, node := range w.Nodes {
		switch node.Type {
		case NodeTypeTool:
			if node.ToolName == "" {
				return fmt.Errorf("tool node %s missing tool name", node.ID)
			}
		case NodeTypeAgent:
			if node.AgentID == "" {
				return fmt.Errorf("agent node %s missing agent ID", node.ID)
			}
		}
	}

	return nil
}

// hasCycle detects cycles in the workflow DAG.
func (w *Workflow) hasCycle() bool {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for nodeID := range w.Nodes {
		if w.hasCycleUtil(nodeID, visited, recStack) {
			return true
		}
	}

	return false
}

// hasCycleUtil is a helper for cycle detection.
func (w *Workflow) hasCycleUtil(nodeID string, visited, recStack map[string]bool) bool {
	visited[nodeID] = true
	recStack[nodeID] = true

	for _, childID := range w.Edges[nodeID] {
		if !visited[childID] {
			if w.hasCycleUtil(childID, visited, recStack) {
				return true
			}
		} else if recStack[childID] {
			return true
		}
	}

	recStack[nodeID] = false

	return false
}

// wouldCreateCycle checks if adding an edge would create a cycle.
func (w *Workflow) wouldCreateCycle(from, to string) bool {
	// Temporarily add edge
	w.Edges[from] = append(w.Edges[from], to)

	hasCycle := w.hasCycle()

	// Remove temporary edge
	edges := w.Edges[from]
	for i, nodeID := range edges {
		if nodeID == to {
			w.Edges[from] = append(edges[:i], edges[i+1:]...)

			break
		}
	}

	return hasCycle
}

// GetNode retrieves a node by ID.
func (w *Workflow) GetNode(nodeID string) (*WorkflowNode, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	node, exists := w.Nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}

	return node, nil
}

// RemoveNode removes a node from the workflow.
func (w *Workflow) RemoveNode(nodeID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.Nodes[nodeID]; !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Remove node
	delete(w.Nodes, nodeID)

	// Remove from edges
	delete(w.Edges, nodeID)

	for parent, children := range w.Edges {
		newChildren := make([]string, 0)

		for _, child := range children {
			if child != nodeID {
				newChildren = append(newChildren, child)
			}
		}

		w.Edges[parent] = newChildren
	}

	// Remove from start nodes
	newStartNodes := make([]string, 0)

	for _, id := range w.StartNodes {
		if id != nodeID {
			newStartNodes = append(newStartNodes, id)
		}
	}

	w.StartNodes = newStartNodes

	w.UpdatedAt = time.Now()

	return nil
}
