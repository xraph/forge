package testing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/cluster"
	"github.com/xraph/forge/extensions/consensus/discovery"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/raft"
	"github.com/xraph/forge/extensions/consensus/statemachine"
	"github.com/xraph/forge/extensions/consensus/storage"
	"github.com/xraph/forge/extensions/consensus/transport"
)

// TestHarness provides utilities for testing consensus
type TestHarness struct {
	t          *testing.T
	nodes      map[string]*TestNode
	transports map[string]*transport.LocalTransport
	logger     forge.Logger
	mu         sync.RWMutex
}

// TestCluster is an alias for TestHarness for backward compatibility
type TestCluster = TestHarness

// TestNode represents a node in the test cluster
type TestNode struct {
	ID           string
	RaftNode     internal.RaftNode
	Transport    internal.Transport
	Storage      internal.Storage
	StateMachine internal.StateMachine
	ClusterMgr   *cluster.Manager
	Discovery    internal.Discovery
	Started      bool
}

// NewTestHarness creates a new test harness
func NewTestHarness(t *testing.T) *TestHarness {
	return &TestHarness{
		t:          t,
		nodes:      make(map[string]*TestNode),
		transports: make(map[string]*transport.LocalTransport),
		logger:     &testLogger{t: t},
	}
}

// CreateCluster creates a test cluster with the specified number of nodes
func (h *TestHarness) CreateCluster(ctx context.Context, numNodes int) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if numNodes < 1 {
		return fmt.Errorf("cluster must have at least 1 node")
	}

	// Create nodes
	for i := 0; i < numNodes; i++ {
		nodeID := fmt.Sprintf("node-%d", i+1)

		// Create components
		trans := transport.NewLocalTransport(transport.LocalTransportConfig{
			NodeID:     nodeID,
			BufferSize: 100,
		}, h.logger)
		h.transports[nodeID] = trans

		stor := storage.NewMemoryStorage(storage.MemoryStorageConfig{
			InitialCapacity: 1000,
		}, h.logger)
		sm := statemachine.NewMemoryStateMachine(statemachine.MemoryStateMachineConfig{
			InitialCapacity: 1000,
		}, h.logger)

		// Create peer list
		var peers []internal.NodeInfo
		for j := 0; j < numNodes; j++ {
			peerID := fmt.Sprintf("node-%d", j+1)
			peers = append(peers, internal.NodeInfo{
				ID:      peerID,
				Address: "localhost",
				Port:    9000 + j,
			})
		}

		disco := discovery.NewStaticDiscovery(discovery.StaticDiscoveryConfig{
			Nodes: peers,
		}, h.logger)

		// Create cluster manager
		clusterCfg := cluster.ManagerConfig{
			NodeID:              nodeID,
			HealthCheckInterval: 100 * time.Millisecond,
			HealthTimeout:       500 * time.Millisecond,
		}
		clusterMgr := cluster.NewManager(clusterCfg, h.logger)

		// Initialize cluster with peers
		for _, peer := range peers {
			if peer.ID != nodeID {
				clusterMgr.AddNode(peer.ID, peer.Address, peer.Port)
			}
		}

		// Create Raft node
		raftCfg := raft.Config{
			NodeID:             nodeID,
			HeartbeatInterval:  50 * time.Millisecond,
			ElectionTimeoutMin: 200 * time.Millisecond,
			ElectionTimeoutMax: 400 * time.Millisecond,
			MaxAppendEntries:   64,
			EnableSnapshots:    false, // Disable snapshots for testing
		}

		raftNode, err := raft.NewNode(raftCfg, h.logger, sm, trans, stor)
		if err != nil {
			return err
		}

		h.nodes[nodeID] = &TestNode{
			ID:           nodeID,
			RaftNode:     raftNode,
			Transport:    trans,
			Storage:      stor,
			StateMachine: sm,
			ClusterMgr:   clusterMgr,
			Discovery:    disco,
		}
	}

	// Connect all transports and add peers to Raft nodes
	for _, node := range h.nodes {
		localTrans := node.Transport.(*transport.LocalTransport)
		for peerID, peerTrans := range h.transports {
			if peerID != node.ID {
				localTrans.Connect(peerTrans)
				// Add peer to Raft node
				node.RaftNode.AddPeer(peerID)
			}
		}
	}

	h.t.Logf("Created test cluster with %d nodes", numNodes)
	return nil
}

// StartCluster starts all nodes in the cluster
func (h *TestHarness) StartCluster(ctx context.Context) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, node := range h.nodes {
		if err := h.startNode(ctx, node); err != nil {
			return fmt.Errorf("failed to start node %s: %w", node.ID, err)
		}
	}

	h.t.Logf("Started cluster with %d nodes", len(h.nodes))
	return nil
}

// StopCluster stops all nodes in the cluster
func (h *TestHarness) StopCluster(ctx context.Context) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, node := range h.nodes {
		if node.Started {
			if err := h.stopNode(ctx, node); err != nil {
				h.t.Logf("Warning: failed to stop node %s: %v", node.ID, err)
			}
		}
	}

	h.t.Logf("Stopped cluster")
	return nil
}

// WaitForLeader waits for a leader to be elected
func (h *TestHarness) WaitForLeader(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		h.mu.RLock()
		for nodeID, node := range h.nodes {
			if node.Started && node.RaftNode.IsLeader() {
				h.mu.RUnlock()
				h.t.Logf("Leader elected: %s", nodeID)
				return nodeID, nil
			}
		}
		h.mu.RUnlock()

		time.Sleep(50 * time.Millisecond)
	}

	return "", fmt.Errorf("no leader elected within timeout")
}

// GetLeader returns the current leader node ID
func (h *TestHarness) GetLeader() (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for nodeID, node := range h.nodes {
		if node.Started && node.RaftNode.IsLeader() {
			return nodeID, nil
		}
	}

	return "", fmt.Errorf("no leader found")
}

// GetNode returns a node by ID
func (h *TestHarness) GetNode(nodeID string) (*TestNode, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	node, exists := h.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}

	return node, nil
}

// StopNode stops a specific node (for partition testing)
func (h *TestHarness) StopNode(ctx context.Context, nodeID string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	node, exists := h.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	return h.stopNode(ctx, node)
}

// StartNode starts a specific node
func (h *TestHarness) StartNode(ctx context.Context, nodeID string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	node, exists := h.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	return h.startNode(ctx, node)
}

// PartitionNode simulates a network partition by disconnecting a node
func (h *TestHarness) PartitionNode(nodeID string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	trans, exists := h.transports[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	trans.DisconnectAll()
	h.t.Logf("Partitioned node: %s", nodeID)
	return nil
}

// HealPartition reconnects a partitioned node
func (h *TestHarness) HealPartition(nodeID string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	trans, exists := h.transports[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Reconnect to all other nodes
	for peerID, peerTrans := range h.transports {
		if peerID != nodeID {
			trans.Connect(peerTrans)
		}
	}

	h.t.Logf("Healed partition for node: %s", nodeID)
	return nil
}

// SubmitToLeader submits a command to the leader
func (h *TestHarness) SubmitToLeader(ctx context.Context, command []byte) error {
	leaderID, err := h.GetLeader()
	if err != nil {
		return err
	}

	node, err := h.GetNode(leaderID)
	if err != nil {
		return err
	}

	return node.RaftNode.Propose(ctx, command)
}

// WaitForCommit waits for a specific index to be committed on all nodes
func (h *TestHarness) WaitForCommit(index uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allCommitted := true

		h.mu.RLock()
		for _, node := range h.nodes {
			if !node.Started {
				continue
			}

			commitIndex := node.RaftNode.GetCommitIndex()
			if commitIndex < index {
				allCommitted = false
				break
			}
		}
		h.mu.RUnlock()

		if allCommitted {
			h.t.Logf("All nodes committed index %d", index)
			return nil
		}

		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("not all nodes committed index %d within timeout", index)
}

// AssertLeaderExists asserts that a leader exists
func (h *TestHarness) AssertLeaderExists() {
	_, err := h.GetLeader()
	if err != nil {
		h.t.Fatalf("Expected leader to exist: %v", err)
	}
}

// AssertNoLeader asserts that no leader exists
func (h *TestHarness) AssertNoLeader() {
	_, err := h.GetLeader()
	if err == nil {
		h.t.Fatal("Expected no leader to exist")
	}
}

// startNode starts a node
func (h *TestHarness) startNode(ctx context.Context, node *TestNode) error {
	if node.Started {
		return fmt.Errorf("node already started")
	}

	// Start components
	if err := node.Storage.Start(ctx); err != nil {
		return err
	}

	if err := node.Transport.Start(ctx); err != nil {
		return err
	}

	if err := node.RaftNode.Start(ctx); err != nil {
		return err
	}

	node.Started = true
	return nil
}

// stopNode stops a node
func (h *TestHarness) stopNode(ctx context.Context, node *TestNode) error {
	if !node.Started {
		return nil
	}

	// Disconnect this node from all other nodes
	// This is critical for detection of node failures in tests
	if localTrans, ok := node.Transport.(*transport.LocalTransport); ok {
		localTrans.DisconnectAll()
	}

	// Stop components in reverse order
	if err := node.RaftNode.Stop(ctx); err != nil {
		return err
	}

	if err := node.Transport.Stop(ctx); err != nil {
		return err
	}

	if err := node.Storage.Stop(ctx); err != nil {
		return err
	}

	node.Started = false
	return nil
}

// GetNodes returns a slice of all test nodes
func (h *TestHarness) GetNodes() []*TestNode {
	h.mu.RLock()
	defer h.mu.RUnlock()

	nodes := make([]*TestNode, 0, len(h.nodes))
	for _, node := range h.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetHealthyNodeCount returns the number of started nodes
func (h *TestHarness) GetHealthyNodeCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, node := range h.nodes {
		if node.Started {
			count++
		}
	}
	return count
}

// GetQuorumSize returns the quorum size for the cluster
func (h *TestHarness) GetQuorumSize() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return (len(h.nodes) / 2) + 1
}

// CreatePartition creates a network partition for the specified nodes
func (h *TestHarness) CreatePartition(nodeIDs []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// For each partitioned node, disconnect it from others
	for _, nodeID := range nodeIDs {
		node, exists := h.nodes[nodeID]
		if !exists {
			continue
		}
		// Disconnect transport (implementation depends on transport type)
		if trans, ok := node.Transport.(*transport.LocalTransport); ok {
			for _, otherID := range nodeIDs {
				if otherID != nodeID {
					trans.Disconnect(otherID)
				}
			}
		}
	}
	return nil
}

// HealAllPartitions heals all network partitions
func (h *TestHarness) HealAllPartitions() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Reconnect all transports
	for _, node := range h.nodes {
		if trans, ok := node.Transport.(*transport.LocalTransport); ok {
			for _, otherNode := range h.nodes {
				if otherNode.ID != node.ID {
					if otherTrans, ok := otherNode.Transport.(*transport.LocalTransport); ok {
						trans.Connect(otherTrans)
					}
				}
			}
		}
	}
	return nil
}

// InjectLatency injects artificial latency for a node
func (h *TestHarness) InjectLatency(nodeID string, latency time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	node, exists := h.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	// Inject latency (implementation depends on transport type)
	if trans, ok := node.Transport.(*transport.LocalTransport); ok {
		trans.SetLatency(latency)
	}
	return nil
}

// RemoveLatency removes artificial latency for a node
func (h *TestHarness) RemoveLatency(nodeID string) error {
	return h.InjectLatency(nodeID, 0)
}

// testLogger implements forge.Logger for testing (no-op to reduce terminal output)
type testLogger struct {
	t *testing.T
}

func (tl *testLogger) Debug(msg string, fields ...forge.Field) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Info(msg string, fields ...forge.Field) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Warn(msg string, fields ...forge.Field) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Error(msg string, fields ...forge.Field) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Fatal(msg string, fields ...forge.Field) {
	tl.t.Fatalf("[FATAL] %s %v", msg, fields)
}

func (tl *testLogger) With(fields ...forge.Field) forge.Logger {
	return tl
}

func (tl *testLogger) Debugf(template string, args ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Infof(template string, args ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Warnf(template string, args ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Errorf(template string, args ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tl *testLogger) Fatalf(template string, args ...interface{}) {
	tl.t.Fatalf("[FATAL] "+template, args...)
}

func (tl *testLogger) WithContext(ctx context.Context) forge.Logger {
	return tl
}

func (tl *testLogger) Named(name string) forge.Logger {
	return tl
}

func (tl *testLogger) Sugar() forge.SugarLogger {
	return &testSugarLogger{t: tl.t}
}

func (tl *testLogger) Sync() error {
	return nil
}

// testSugarLogger implements forge.SugarLogger for testing (no-op to reduce terminal output)
type testSugarLogger struct {
	t *testing.T
}

func (tsl *testSugarLogger) Debugw(msg string, keysAndValues ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tsl *testSugarLogger) Infow(msg string, keysAndValues ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tsl *testSugarLogger) Warnw(msg string, keysAndValues ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tsl *testSugarLogger) Errorw(msg string, keysAndValues ...interface{}) {
	// No-op to reduce terminal output during tests
}

func (tsl *testSugarLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	tsl.t.Fatalf("[FATAL] %s %v", msg, keysAndValues)
}

func (tsl *testSugarLogger) With(args ...interface{}) forge.SugarLogger {
	return tsl
}
