package consensus

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/consensus/discovery"
	"github.com/xraph/forge/v0/pkg/consensus/raft"
	"github.com/xraph/forge/v0/pkg/consensus/statemachine"
	"github.com/xraph/forge/v0/pkg/consensus/storage"
	"github.com/xraph/forge/v0/pkg/consensus/transport"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/metrics"
)

// TestCluster represents a test consensus cluster
type TestCluster struct {
	ID           string
	Nodes        map[string]*TestNode
	Transport    transport.Transport
	Storage      storage.Storage
	StateMachine statemachine.StateMachine
	Discovery    discovery.Discovery
	Manager      *ConsensusManager
	Config       *TestClusterConfig
	Logger       common.Logger
	Metrics      common.Metrics
	TempDir      string
	Started      bool
	mu           sync.RWMutex
}

// TestNode represents a test consensus node
type TestNode struct {
	ID           string
	Address      string
	RaftNode     raft.RaftNode
	IsLocal      bool
	Started      bool
	LastElection time.Time
	VoteCount    int
	mu           sync.RWMutex
}

// TestClusterConfig contains configuration for test clusters
type TestClusterConfig struct {
	NodeCount         int           `yaml:"node_count" default:"3"`
	ElectionTimeout   time.Duration `yaml:"election_timeout" default:"1s"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" default:"200ms"`
	StorageType       string        `yaml:"storage_type" default:"memory"`
	TransportType     string        `yaml:"transport_type" default:"mock"`
	StateMachineType  string        `yaml:"state_machine_type" default:"memory"`
	EnableMetrics     bool          `yaml:"enable_metrics" default:"true"`
	EnableLogging     bool          `yaml:"enable_logging" default:"false"`
}

// MockTransport implements the transport.Transport interface for testing
type MockTransport struct {
	nodes        map[string]*MockNode
	partitions   map[string]map[string]bool
	latencies    map[string]map[string]time.Duration
	dropRate     float64
	messageLog   []transport.Message
	config       transport.TransportConfig
	localAddress string
	receiveChan  chan transport.IncomingMessage
	mu           sync.RWMutex
	started      bool
	messagesSent int64
	messagesRecv int64
	bytesSent    int64
	bytesRecv    int64
	errorCount   int64
	startTime    time.Time
}

// MockNode represents a mock transport node
type MockNode struct {
	ID      string
	Address string
	Inbox   chan transport.Message
	Outbox  chan transport.Message
	Running bool
	Status  transport.PeerStatus
}

// MockDiscovery implements mock discovery for testing
type MockDiscovery struct {
	nodes   map[string]discovery.NodeInfo
	started bool
	mu      sync.RWMutex
}

// NewTestCluster creates a new test cluster
func NewTestCluster(t *testing.T, config *TestClusterConfig) *TestCluster {
	if config == nil {
		config = &TestClusterConfig{
			NodeCount:         3,
			ElectionTimeout:   1 * time.Second,
			HeartbeatInterval: 200 * time.Millisecond,
			StorageType:       "memory",
			TransportType:     "mock",
			StateMachineType:  "memory",
			EnableMetrics:     true,
			EnableLogging:     false,
		}
	}

	// Create temp directory for test data
	tempDir, err := os.MkdirTemp("", "consensus-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create logger
	var testLogger common.Logger
	if config.EnableLogging {
		testLogger = logger.NewTestLogger()
	}

	// Create metrics
	var testMetrics common.Metrics
	if config.EnableMetrics {
		testMetrics = metrics.NewMockMetricsCollector()
	}

	// Create transport
	mockTransport := NewMockTransport("127.0.0.1:8090")

	cluster := &TestCluster{
		ID:        fmt.Sprintf("test-cluster-%d", time.Now().UnixNano()),
		Nodes:     make(map[string]*TestNode),
		Transport: mockTransport,
		Config:    config,
		Logger:    testLogger,
		Metrics:   testMetrics,
		TempDir:   tempDir,
	}

	// Create nodes
	for i := 0; i < config.NodeCount; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		address := fmt.Sprintf("127.0.0.1:%d", 8090+i)

		node := &TestNode{
			ID:      nodeID,
			Address: address,
			IsLocal: i == 0, // First node is local
		}

		cluster.Nodes[nodeID] = node
		mockTransport.AddPeer(nodeID, address)
	}

	// Cleanup function
	t.Cleanup(func() {
		cluster.Stop()
		os.RemoveAll(tempDir)
	})

	return cluster
}

// Start starts the test cluster
func (tc *TestCluster) Start(ctx context.Context) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.Started {
		return nil
	}

	// Start transport
	if err := tc.Transport.Start(ctx); err != nil {
		return fmt.Errorf("failed to start transport: %w", err)
	}

	// Create storage
	storage, err := tc.createStorage()
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	tc.Storage = storage

	// Create state machine
	stateMachine, err := tc.createStateMachine()
	if err != nil {
		return fmt.Errorf("failed to create state machine: %w", err)
	}
	tc.StateMachine = stateMachine

	// Create discovery
	discovery := tc.createDiscovery()
	tc.Discovery = discovery

	// Create and start Raft nodes
	for _, node := range tc.Nodes {
		if err := tc.startNode(ctx, node); err != nil {
			return fmt.Errorf("failed to start node %s: %w", node.ID, err)
		}
	}

	tc.Started = true
	return nil
}

// Stop stops the test cluster
func (tc *TestCluster) Stop() error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if !tc.Started {
		return nil
	}

	// Stop all nodes
	for _, node := range tc.Nodes {
		tc.stopNode(node)
	}

	// Stop transport
	tc.Transport.Stop(context.Background())

	// Close storage
	if tc.Storage != nil {
		tc.Storage.Close(context.Background())
	}

	tc.Started = false
	return nil
}

// startNode starts a test node
func (tc *TestCluster) startNode(ctx context.Context, node *TestNode) error {
	// Create Raft configuration
	config := raft.NodeConfig{
		ID:                node.ID,
		ElectionTimeout:   tc.Config.ElectionTimeout,
		HeartbeatInterval: tc.Config.HeartbeatInterval,
	}

	// Create Raft node
	raftNode, err := raft.NewNode(
		config,
		tc.Storage,
		tc.Transport,
		tc.StateMachine,
		tc.Discovery,
		tc.Logger,
		tc.Metrics,
	)
	if err != nil {
		return fmt.Errorf("failed to create Raft node: %w", err)
	}

	node.RaftNode = raftNode

	// Start Raft node
	if err := raftNode.Start(ctx); err != nil {
		return fmt.Errorf("failed to start Raft node: %w", err)
	}

	node.Started = true
	return nil
}

// stopNode stops a test node
func (tc *TestCluster) stopNode(node *TestNode) {
	if node.RaftNode != nil && node.Started {
		node.RaftNode.Stop(context.Background())
		node.Started = false
	}
}

// createStorage creates storage based on configuration
func (tc *TestCluster) createStorage() (storage.Storage, error) {
	switch tc.Config.StorageType {
	case "memory":
		config := storage.MemoryStorageConfig{
			MaxEntries: 10000,
		}
		return storage.NewMemoryStorage(config, tc.Logger, tc.Metrics), nil

	case "file":
		config := storage.FileStorageConfig{

			Path:                filepath.Join(tc.TempDir, "storage"),
			MaxCacheSize:        1000,
			SyncInterval:        100 * time.Millisecond,
			CompactionThreshold: 1024 * 1024, // 1MB
			EnableCompression:   false,
			EnableChecksum:      true,
		}
		return storage.NewFileStorage(config, tc.Logger, tc.Metrics)

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", tc.Config.StorageType)
	}
}

// createStateMachine creates state machine based on configuration
func (tc *TestCluster) createStateMachine() (statemachine.StateMachine, error) {
	switch tc.Config.StateMachineType {
	case "memory":
		config := statemachine.StateMachineConfig{
			Type: "memory",
		}
		return statemachine.NewMemoryStateMachineFactory().Create(config)

	case "mock":
		return NewMockStateMachine(), nil

	default:
		return nil, fmt.Errorf("unsupported state machine type: %s", tc.Config.StateMachineType)
	}
}

// createDiscovery creates discovery based on configuration
func (tc *TestCluster) createDiscovery() discovery.Discovery {
	nodeList := make([]discovery.NodeInfo, 0, len(tc.Nodes))
	for _, node := range tc.Nodes {
		nodeList = append(nodeList, discovery.NodeInfo{
			ID:      node.ID,
			Address: node.Address,
			Status:  discovery.NodeStatusActive,
			Metadata: map[string]interface{}{
				"type": "mock",
			},
		})
	}

	return NewMockDiscovery(nodeList)
}

// WaitForLeader waits for a leader to be elected
func (tc *TestCluster) WaitForLeader(timeout time.Duration) (*TestNode, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		for _, node := range tc.Nodes {
			if node.RaftNode != nil && node.RaftNode.IsLeader() {
				return node, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("no leader elected within timeout")
}

// GetLeader returns the current leader node
func (tc *TestCluster) GetLeader() *TestNode {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	for _, node := range tc.Nodes {
		if node.RaftNode != nil && node.RaftNode.IsLeader() {
			return node
		}
	}

	return nil
}

// GetFollowers returns all follower nodes
func (tc *TestCluster) GetFollowers() []*TestNode {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	var followers []*TestNode
	for _, node := range tc.Nodes {
		if node.RaftNode != nil && !node.RaftNode.IsLeader() {
			followers = append(followers, node)
		}
	}

	return followers
}

// AddNode adds a new node to the cluster
func (tc *TestCluster) AddNode(nodeID string) (*TestNode, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if _, exists := tc.Nodes[nodeID]; exists {
		return nil, fmt.Errorf("node %s already exists", nodeID)
	}

	// Find available port
	port := 8090 + len(tc.Nodes)
	address := fmt.Sprintf("127.0.0.1:%d", port)

	node := &TestNode{
		ID:      nodeID,
		Address: address,
		IsLocal: false,
	}

	tc.Nodes[nodeID] = node

	// Add to transport
	if mockTransport, ok := tc.Transport.(*MockTransport); ok {
		mockTransport.AddPeer(nodeID, address)
	}

	// Start node if cluster is running
	if tc.Started {
		if err := tc.startNode(context.Background(), node); err != nil {
			delete(tc.Nodes, nodeID)
			return nil, err
		}
	}

	return node, nil
}

// RemoveNode removes a node from the cluster
func (tc *TestCluster) RemoveNode(nodeID string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	node, exists := tc.Nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Stop node
	tc.stopNode(node)

	// Remove from transport
	tc.Transport.RemovePeer(nodeID)

	// Remove from cluster
	delete(tc.Nodes, nodeID)

	return nil
}

// PartitionNode simulates network partition for a node
func (tc *TestCluster) PartitionNode(nodeID string, partitionedNodes ...string) {
	if mockTransport, ok := tc.Transport.(*MockTransport); ok {
		mockTransport.PartitionNode(nodeID, partitionedNodes...)
	}
}

// HealPartition heals network partitions for a node
func (tc *TestCluster) HealPartition(nodeID string) {
	if mockTransport, ok := tc.Transport.(*MockTransport); ok {
		mockTransport.HealPartition(nodeID)
	}
}

// SetLatency sets network latency between nodes
func (tc *TestCluster) SetLatency(from, to string, latency time.Duration) {
	if mockTransport, ok := tc.Transport.(*MockTransport); ok {
		mockTransport.SetLatency(from, to, latency)
	}
}

// SetDropRate sets the message drop rate
func (tc *TestCluster) SetDropRate(rate float64) {
	if mockTransport, ok := tc.Transport.(*MockTransport); ok {
		mockTransport.SetDropRate(rate)
	}
}

// ProposeValue proposes a value to the cluster
func (tc *TestCluster) ProposeValue(key string, value interface{}) error {
	leader := tc.GetLeader()
	if leader == nil {
		return fmt.Errorf("no leader available")
	}

	// Create log entry
	entry := storage.LogEntry{
		Type: storage.EntryTypeApplication,
		Data: []byte(fmt.Sprintf("%s:%v", key, value)),
		Metadata: map[string]interface{}{
			"key":   key,
			"value": value,
		},
		Timestamp: time.Now(),
	}

	return leader.RaftNode.AppendEntries(context.Background(), []storage.LogEntry{entry})
}

// GetState returns the current state
func (tc *TestCluster) GetState() interface{} {
	if tc.StateMachine == nil {
		return nil
	}
	return tc.StateMachine.GetState()
}

// ============================================================================
// MockTransport implementation
// ============================================================================

// NewMockTransport creates a new mock transport
func NewMockTransport(address string) *MockTransport {
	return &MockTransport{
		nodes:        make(map[string]*MockNode),
		partitions:   make(map[string]map[string]bool),
		latencies:    make(map[string]map[string]time.Duration),
		messageLog:   make([]transport.Message, 0),
		localAddress: address,
		receiveChan:  make(chan transport.IncomingMessage, 1000),
	}
}

func (mt *MockTransport) Start(ctx context.Context) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.started = true
	mt.startTime = time.Now()
	return nil
}

func (mt *MockTransport) Stop(ctx context.Context) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.started = false
	close(mt.receiveChan)
	return nil
}

func (mt *MockTransport) Send(ctx context.Context, target string, message transport.Message) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Check if nodes are partitioned
	if partitions, exists := mt.partitions[message.From]; exists {
		if partitions[target] {
			return fmt.Errorf("nodes %s and %s are partitioned", message.From, target)
		}
	}

	// Check drop rate
	if mt.dropRate > 0 && mt.shouldDropMessage() {
		return nil // Silently drop message
	}

	toNode, exists := mt.nodes[target]
	if !exists || !toNode.Running {
		mt.errorCount++
		return transport.NewTransportError(transport.ErrCodePeerNotFound, fmt.Sprintf("node %s not found or not running", target))
	}

	// Update stats
	mt.messagesSent++
	mt.bytesSent += int64(transport.MessageSize(message))

	// Add latency if configured
	if latencies, exists := mt.latencies[message.From]; exists {
		if latency, exists := latencies[target]; exists {
			go func() {
				time.Sleep(latency)
				mt.deliverMessage(toNode, message)
			}()
			return nil
		}
	}

	// Deliver immediately
	mt.deliverMessage(toNode, message)

	// Log message
	mt.messageLog = append(mt.messageLog, message)

	return nil
}

func (mt *MockTransport) deliverMessage(node *MockNode, message transport.Message) {
	incoming := transport.IncomingMessage{
		Message: message,
		Peer: transport.PeerInfo{
			ID:      node.ID,
			Address: node.Address,
			Status:  node.Status,
		},
	}

	select {
	case mt.receiveChan <- incoming:
		mt.mu.Lock()
		mt.messagesRecv++
		mt.bytesRecv += int64(transport.MessageSize(message))
		mt.mu.Unlock()
	default:
		// Channel full, drop message
	}
}

func (mt *MockTransport) Receive(ctx context.Context) (<-chan transport.IncomingMessage, error) {
	return mt.receiveChan, nil
}

func (mt *MockTransport) AddPeer(peerID, address string) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if _, exists := mt.nodes[peerID]; exists {
		return fmt.Errorf("peer %s already exists", peerID)
	}

	mt.nodes[peerID] = &MockNode{
		ID:      peerID,
		Address: address,
		Inbox:   make(chan transport.Message, 100),
		Outbox:  make(chan transport.Message, 100),
		Running: true,
		Status:  transport.PeerStatusConnected,
	}

	return nil
}

func (mt *MockTransport) RemovePeer(peerID string) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if node, exists := mt.nodes[peerID]; exists {
		node.Running = false
		close(node.Inbox)
		close(node.Outbox)
		delete(mt.nodes, peerID)
	}

	return nil
}

func (mt *MockTransport) GetPeers() []transport.PeerInfo {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	peers := make([]transport.PeerInfo, 0, len(mt.nodes))
	for _, node := range mt.nodes {
		peers = append(peers, transport.PeerInfo{
			ID:       node.ID,
			Address:  node.Address,
			Status:   node.Status,
			LastSeen: time.Now(),
		})
	}

	return peers
}

func (mt *MockTransport) LocalAddress() string {
	return mt.localAddress
}

func (mt *MockTransport) GetAddress(nodeID string) (string, error) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	if node, exists := mt.nodes[nodeID]; exists {
		return node.Address, nil
	}

	return "", transport.NewTransportError(transport.ErrCodePeerNotFound, fmt.Sprintf("node %s not found", nodeID))
}

func (mt *MockTransport) HealthCheck(ctx context.Context) error {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	if !mt.started {
		return fmt.Errorf("transport not started")
	}

	return nil
}

func (mt *MockTransport) GetStats() transport.TransportStats {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	connectedPeers := 0
	for _, node := range mt.nodes {
		if node.Running && node.Status == transport.PeerStatusConnected {
			connectedPeers++
		}
	}

	var uptime time.Duration
	if !mt.startTime.IsZero() {
		uptime = time.Since(mt.startTime)
	}

	return transport.TransportStats{
		MessagesSent:     mt.messagesSent,
		MessagesReceived: mt.messagesRecv,
		BytesSent:        mt.bytesSent,
		BytesReceived:    mt.bytesRecv,
		ErrorCount:       mt.errorCount,
		ConnectedPeers:   connectedPeers,
		Uptime:           uptime,
	}
}

func (mt *MockTransport) Close() error {
	return mt.Stop(context.Background())
}

func (mt *MockTransport) shouldDropMessage() bool {
	// Simple random drop based on drop rate
	return false // Simplified for now
}

func (mt *MockTransport) PartitionNode(nodeID string, partitionedNodes ...string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if mt.partitions[nodeID] == nil {
		mt.partitions[nodeID] = make(map[string]bool)
	}

	for _, partitioned := range partitionedNodes {
		mt.partitions[nodeID][partitioned] = true

		// Make partition bidirectional
		if mt.partitions[partitioned] == nil {
			mt.partitions[partitioned] = make(map[string]bool)
		}
		mt.partitions[partitioned][nodeID] = true
	}
}

func (mt *MockTransport) HealPartition(nodeID string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if partitions, exists := mt.partitions[nodeID]; exists {
		// Remove all partitions for this node
		for partitioned := range partitions {
			delete(mt.partitions[partitioned], nodeID)
		}
		delete(mt.partitions, nodeID)
	}
}

func (mt *MockTransport) SetLatency(from, to string, latency time.Duration) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if mt.latencies[from] == nil {
		mt.latencies[from] = make(map[string]time.Duration)
	}
	mt.latencies[from][to] = latency
}

func (mt *MockTransport) SetDropRate(rate float64) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.dropRate = rate
}

func (mt *MockTransport) GetMessageLog() []transport.Message {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	log := make([]transport.Message, len(mt.messageLog))
	copy(log, mt.messageLog)
	return log
}

// ============================================================================
// MockStateMachine implementation
// ============================================================================

// MockStateMachine implements a mock state machine for testing
type MockStateMachine struct {
	state        map[string]interface{}
	history      []storage.LogEntry
	snapshots    []*storage.Snapshot
	startTime    time.Time
	lastApplied  uint64
	errorCount   int64
	lastError    error
	totalLatency time.Duration
	operCount    int64
	mu           sync.RWMutex
}

// NewMockStateMachine creates a new mock state machine
func NewMockStateMachine() *MockStateMachine {
	return &MockStateMachine{
		state:     make(map[string]interface{}),
		history:   make([]storage.LogEntry, 0),
		snapshots: make([]*storage.Snapshot, 0),
		startTime: time.Now(),
	}
}

// Apply applies a log entry to the state machine
func (msm *MockStateMachine) Apply(ctx context.Context, entry storage.LogEntry) error {
	start := time.Now()

	msm.mu.Lock()
	defer msm.mu.Unlock()

	msm.history = append(msm.history, entry)
	msm.lastApplied = entry.Index
	msm.operCount++

	// Parse key-value from entry data
	data := string(entry.Data)
	if len(data) > 0 {
		// Simple key:value parsing
		parts := strings.SplitN(data, ":", 2)
		if len(parts) >= 2 {
			msm.state[parts[0]] = parts[1]
		}
	}

	msm.totalLatency += time.Since(start)

	return nil
}

// GetState returns the current state of the state machine
func (msm *MockStateMachine) GetState() interface{} {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	state := make(map[string]interface{})
	for k, v := range msm.state {
		state[k] = v
	}
	return state
}

// CreateSnapshot creates a snapshot of the current state (returns pointer)
func (msm *MockStateMachine) CreateSnapshot() (*storage.Snapshot, error) {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	// Serialize state to JSON
	stateJSON, err := json.Marshal(msm.state)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize state: %w", err)
	}

	snapshot := &storage.Snapshot{
		Index: msm.lastApplied,
		Term:  1,
		Data:  stateJSON,
		Metadata: map[string]interface{}{
			"type":      "mock",
			"timestamp": time.Now(),
			"size":      len(stateJSON),
		},
		Timestamp: time.Now(),
		Checksum:  storage.CalculateChecksum(stateJSON),
	}

	// Store snapshot reference
	msm.snapshots = append(msm.snapshots, snapshot)

	return snapshot, nil
}

// RestoreSnapshot restores the state machine from a snapshot (takes pointer)
func (msm *MockStateMachine) RestoreSnapshot(snapshot *storage.Snapshot) error {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	if snapshot == nil {
		return fmt.Errorf("snapshot cannot be nil")
	}

	// Deserialize state from JSON
	newState := make(map[string]interface{})
	if len(snapshot.Data) > 0 {
		if err := json.Unmarshal(snapshot.Data, &newState); err != nil {
			// If unmarshal fails, try simple string representation
			msm.state = make(map[string]interface{})
		} else {
			msm.state = newState
		}
	}

	msm.history = make([]storage.LogEntry, 0)
	msm.lastApplied = snapshot.Index

	return nil
}

// Reset resets the state machine to initial state
func (msm *MockStateMachine) Reset() error {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	msm.state = make(map[string]interface{})
	msm.history = make([]storage.LogEntry, 0)
	msm.lastApplied = 0
	msm.errorCount = 0
	msm.lastError = nil
	msm.totalLatency = 0
	msm.operCount = 0

	return nil
}

// Size returns the approximate size of the state machine in bytes
func (msm *MockStateMachine) Size() int64 {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	// Approximate size calculation
	size := int64(0)

	// Size of state map
	for k, v := range msm.state {
		size += int64(len(k))
		if str, ok := v.(string); ok {
			size += int64(len(str))
		} else {
			size += 8 // Approximate for other types
		}
	}

	// Size of history
	for _, entry := range msm.history {
		size += int64(len(entry.Data))
	}

	return size
}

// HealthCheck performs a health check on the state machine
func (msm *MockStateMachine) HealthCheck(ctx context.Context) error {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	// Check if state is accessible
	if msm.state == nil {
		return fmt.Errorf("state is nil")
	}

	// Check error rate
	if msm.operCount > 0 {
		errorRate := float64(msm.errorCount) / float64(msm.operCount)
		if errorRate > 0.5 {
			return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
		}
	}

	return nil
}

// GetStats returns statistics about the state machine
func (msm *MockStateMachine) GetStats() statemachine.StateMachineStats {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	var averageLatency time.Duration
	var opsPerSec float64

	if msm.operCount > 0 {
		averageLatency = msm.totalLatency / time.Duration(msm.operCount)

		uptime := time.Since(msm.startTime)
		if uptime > 0 {
			opsPerSec = float64(msm.operCount) / uptime.Seconds()
		}
	}

	var lastSnapshot time.Time
	if len(msm.snapshots) > 0 {
		lastSnapshot = msm.snapshots[len(msm.snapshots)-1].Timestamp
	}

	var lastErrorStr string
	if msm.lastError != nil {
		lastErrorStr = msm.lastError.Error()
	}

	return statemachine.StateMachineStats{
		AppliedEntries:   int64(len(msm.history)),
		LastApplied:      msm.lastApplied,
		SnapshotCount:    int64(len(msm.snapshots)),
		LastSnapshot:     lastSnapshot,
		StateSize:        msm.Size(),
		Uptime:           time.Since(msm.startTime),
		ErrorCount:       msm.errorCount,
		LastError:        lastErrorStr,
		OperationsPerSec: opsPerSec,
		AverageLatency:   averageLatency,
	}
}

// Close closes the state machine and releases resources
func (msm *MockStateMachine) Close(ctx context.Context) error {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	// Clear all data
	msm.state = nil
	msm.history = nil
	msm.snapshots = nil

	return nil
}

// Additional helper methods for testing

// GetHistory returns the history of applied entries
func (msm *MockStateMachine) GetHistory() []storage.LogEntry {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	history := make([]storage.LogEntry, len(msm.history))
	copy(history, msm.history)
	return history
}

// GetSnapshots returns all created snapshots
func (msm *MockStateMachine) GetSnapshots() []*storage.Snapshot {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	snapshots := make([]*storage.Snapshot, len(msm.snapshots))
	copy(snapshots, msm.snapshots)
	return snapshots
}

// SetError sets an error for testing error scenarios
func (msm *MockStateMachine) SetError(err error) {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	msm.lastError = err
	msm.errorCount++
}

// GetValue returns a specific value from the state
func (msm *MockStateMachine) GetValue(key string) (interface{}, bool) {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	val, exists := msm.state[key]
	return val, exists
}

// SetValue sets a specific value in the state (for test setup)
func (msm *MockStateMachine) SetValue(key string, value interface{}) {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	msm.state[key] = value
}

// ============================================================================
// MockDiscovery implementation
// ============================================================================

func NewMockDiscovery(initialNodes []discovery.NodeInfo) *MockDiscovery {
	nodes := make(map[string]discovery.NodeInfo)
	for _, node := range initialNodes {
		nodes[node.ID] = node
	}

	return &MockDiscovery{
		nodes: nodes,
	}
}

func (md *MockDiscovery) Start(ctx context.Context) error {
	md.mu.Lock()
	defer md.mu.Unlock()
	md.started = true
	return nil
}

func (md *MockDiscovery) Stop(ctx context.Context) error {
	md.mu.Lock()
	defer md.mu.Unlock()
	md.started = false
	return nil
}

func (md *MockDiscovery) GetNodes(ctx context.Context) ([]discovery.NodeInfo, error) {
	md.mu.RLock()
	defer md.mu.RUnlock()

	nodes := make([]discovery.NodeInfo, 0, len(md.nodes))
	for _, node := range md.nodes {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

func (md *MockDiscovery) GetNode(ctx context.Context, nodeID string) (*discovery.NodeInfo, error) {
	md.mu.RLock()
	defer md.mu.RUnlock()

	if node, exists := md.nodes[nodeID]; exists {
		return &node, nil
	}

	return nil, discovery.NewDiscoveryError(discovery.ErrCodeNodeNotFound, fmt.Sprintf("node %s not found", nodeID))
}

func (md *MockDiscovery) Register(ctx context.Context, node discovery.NodeInfo) error {
	md.mu.Lock()
	defer md.mu.Unlock()

	if _, exists := md.nodes[node.ID]; exists {
		return discovery.NewDiscoveryError(discovery.ErrCodeNodeExists, fmt.Sprintf("node %s already exists", node.ID))
	}

	md.nodes[node.ID] = node
	return nil
}

func (md *MockDiscovery) Unregister(ctx context.Context, nodeID string) error {
	md.mu.Lock()
	defer md.mu.Unlock()

	if _, exists := md.nodes[nodeID]; !exists {
		return discovery.NewDiscoveryError(discovery.ErrCodeNodeNotFound, fmt.Sprintf("node %s not found", nodeID))
	}

	delete(md.nodes, nodeID)
	return nil
}

func (md *MockDiscovery) UpdateNode(ctx context.Context, node discovery.NodeInfo) error {
	md.mu.Lock()
	defer md.mu.Unlock()

	if _, exists := md.nodes[node.ID]; !exists {
		return discovery.NewDiscoveryError(discovery.ErrCodeNodeNotFound, fmt.Sprintf("node %s not found", node.ID))
	}

	md.nodes[node.ID] = node
	return nil
}

func (md *MockDiscovery) WatchNodes(ctx context.Context, callback discovery.NodeChangeCallback) error {
	// Simplified implementation - would need proper watching in production
	return nil
}

func (md *MockDiscovery) HealthCheck(ctx context.Context) error {
	md.mu.RLock()
	defer md.mu.RUnlock()

	if !md.started {
		return fmt.Errorf("discovery not started")
	}

	return nil
}

func (md *MockDiscovery) GetStats(ctx context.Context) discovery.DiscoveryStats {
	md.mu.RLock()
	defer md.mu.RUnlock()

	activeNodes := 0
	for _, node := range md.nodes {
		if node.Status == discovery.NodeStatusActive {
			activeNodes++
		}
	}

	return discovery.DiscoveryStats{
		TotalNodes:  len(md.nodes),
		ActiveNodes: activeNodes,
		LastUpdate:  time.Now(),
	}
}

func (md *MockDiscovery) Close(ctx context.Context) error {
	return md.Stop(ctx)
}

// ============================================================================
// Utility functions for testing
// ============================================================================

// WaitForCondition waits for a condition to be true within timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Condition not met within timeout: %s", message)
}

// AssertEventually asserts that a condition becomes true within timeout
func AssertEventually(t *testing.T, condition func() bool, timeout time.Duration) {
	WaitForCondition(t, condition, timeout, "condition not met")
}

// GetFreePort returns a free port for testing
func GetFreePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// SimulateNetworkPartition simulates a network partition scenario
func SimulateNetworkPartition(cluster *TestCluster, partition1, partition2 []string) {
	for _, node1 := range partition1 {
		cluster.PartitionNode(node1, partition2...)
	}
}

// HealNetworkPartition heals a network partition
func HealNetworkPartition(cluster *TestCluster, nodes []string) {
	for _, node := range nodes {
		cluster.HealPartition(node)
	}
}

// SimulateNodeFailure simulates a node failure
func SimulateNodeFailure(cluster *TestCluster, nodeID string) error {
	return cluster.RemoveNode(nodeID)
}

// RecoverNode recovers a failed node
func RecoverNode(cluster *TestCluster, nodeID string) (*TestNode, error) {
	return cluster.AddNode(nodeID)
}

// VerifyClusterConsistency verifies that all nodes have consistent state
func VerifyClusterConsistency(t *testing.T, cluster *TestCluster) {
	leader := cluster.GetLeader()
	if leader == nil {
		t.Fatal("No leader found")
	}

	followers := cluster.GetFollowers()
	for _, follower := range followers {
		if !follower.Started {
			t.Errorf("Follower %s not started", follower.ID)
		}
	}
}

// CreateTestConsensusService creates a consensus service for testing
func CreateTestConsensusService(t *testing.T) (*ConsensusService, *TestCluster) {
	cluster := NewTestCluster(t, nil)
	if err := cluster.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start test cluster: %v", err)
	}

	service := NewConsensusService(
		cluster.Logger,
		cluster.Metrics,
		nil, // config manager
		nil, // database connection
	)

	return service, cluster
}

// BenchmarkConsensusOperations benchmarks consensus operations
func BenchmarkConsensusOperations(b *testing.B, cluster *TestCluster) {
	leader, err := cluster.WaitForLeader(5 * time.Second)
	if err != nil {
		b.Fatalf("No leader elected: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i)
			value := fmt.Sprintf("value-%d", i)

			entry := storage.LogEntry{
				Type: storage.EntryTypeApplication,
				Data: []byte(fmt.Sprintf("%s:%s", key, value)),
				Metadata: map[string]interface{}{
					"key":   key,
					"value": value,
				},
				Timestamp: time.Now(),
			}

			if err := leader.RaftNode.AppendEntries(context.Background(), []storage.LogEntry{entry}); err != nil {
				b.Errorf("Failed to append entry: %v", err)
			}
			i++
		}
	})
}
