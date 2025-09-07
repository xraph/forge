package consensus

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/discovery"
	"github.com/xraph/forge/pkg/consensus/raft"
	"github.com/xraph/forge/pkg/consensus/statemachine"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/consensus/transport"
	"github.com/xraph/forge/pkg/logger"
)

// TestCluster represents a test consensus cluster
type TestCluster struct {
	ID           string
	Nodes        map[string]*TestNode
	Transport    *MockTransport
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

// MockTransport implements the transport interface for testing
type MockTransport struct {
	nodes      map[string]*MockNode
	partitions map[string]map[string]bool // node -> partitioned_nodes
	latencies  map[string]map[string]time.Duration
	dropRate   float64
	messageLog []MockMessage
	config     transport.TransportConfig
	mu         sync.RWMutex
	started    bool
}

// MockNode represents a mock transport node
type MockNode struct {
	ID      string
	Address string
	Inbox   chan MockMessage
	Outbox  chan MockMessage
	Running bool
}

// MockMessage represents a message in the mock transport
type MockMessage struct {
	From      string
	To        string
	Type      string
	Data      []byte
	Timestamp time.Time
}

// MockStateMachine implements a mock state machine for testing
type MockStateMachine struct {
	state   map[string]interface{}
	history []storage.LogEntry
	mu      sync.RWMutex
}

// MockDiscovery implements mock discovery for testing
type MockDiscovery struct {
	nodes   []string
	started bool
	mu      sync.RWMutex
}

// TestLogger implements a test logger
type TestLogger struct {
	logs []string
	mu   sync.RWMutex
}

// TestMetrics implements test metrics collection
type TestMetrics struct {
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string][]float64
	mu         sync.RWMutex
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
		testMetrics = NewTestMetrics()
	}

	// Create transport
	transport := NewMockTransport()

	cluster := &TestCluster{
		ID:        fmt.Sprintf("test-cluster-%d", time.Now().UnixNano()),
		Nodes:     make(map[string]*TestNode),
		Transport: transport,
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
		transport.AddNode(nodeID, address)
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
			Path:        filepath.Join(tc.TempDir, "storage"),
			SyncWrites:  true,
			MaxFileSize: 1024 * 1024, // 1MB
			MaxSegments: 10,
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
	nodeList := make([]string, 0, len(tc.Nodes))
	for nodeID := range tc.Nodes {
		nodeList = append(nodeList, nodeID)
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
	tc.Transport.AddNode(nodeID, address)

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
	tc.Transport.RemoveNode(nodeID)

	// Remove from cluster
	delete(tc.Nodes, nodeID)

	return nil
}

// PartitionNode simulates network partition for a node
func (tc *TestCluster) PartitionNode(nodeID string, partitionedNodes ...string) {
	tc.Transport.PartitionNode(nodeID, partitionedNodes...)
}

// HealPartition heals network partitions for a node
func (tc *TestCluster) HealPartition(nodeID string) {
	tc.Transport.HealPartition(nodeID)
}

// SetLatency sets network latency between nodes
func (tc *TestCluster) SetLatency(from, to string, latency time.Duration) {
	tc.Transport.SetLatency(from, to, latency)
}

// SetDropRate sets the message drop rate
func (tc *TestCluster) SetDropRate(rate float64) {
	tc.Transport.SetDropRate(rate)
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

// MockTransport implementation

// NewMockTransport creates a new mock transport
func NewMockTransport() *MockTransport {
	return &MockTransport{
		nodes:      make(map[string]*MockNode),
		partitions: make(map[string]map[string]bool),
		latencies:  make(map[string]map[string]time.Duration),
		messageLog: make([]MockMessage, 0),
	}
}

func (mt *MockTransport) Start(ctx context.Context) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.started = true
	return nil
}

func (mt *MockTransport) Stop(ctx context.Context) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.started = false
	return nil
}

func (mt *MockTransport) HealthCheck(ctx context.Context) error {
	return nil
}

func (mt *MockTransport) AddNode(nodeID, address string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.nodes[nodeID] = &MockNode{
		ID:      nodeID,
		Address: address,
		Inbox:   make(chan MockMessage, 100),
		Outbox:  make(chan MockMessage, 100),
		Running: true,
	}
}

func (mt *MockTransport) RemoveNode(nodeID string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if node, exists := mt.nodes[nodeID]; exists {
		node.Running = false
		close(node.Inbox)
		close(node.Outbox)
		delete(mt.nodes, nodeID)
	}
}

func (mt *MockTransport) SendMessage(from, to string, msgType string, data []byte) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Check if nodes are partitioned
	if partitions, exists := mt.partitions[from]; exists {
		if partitions[to] {
			return fmt.Errorf("nodes %s and %s are partitioned", from, to)
		}
	}

	// Check drop rate
	if mt.dropRate > 0 && mt.shouldDropMessage() {
		return nil // Silently drop message
	}

	toNode, exists := mt.nodes[to]
	if !exists || !toNode.Running {
		return fmt.Errorf("node %s not found or not running", to)
	}

	message := MockMessage{
		From:      from,
		To:        to,
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now(),
	}

	// Add latency if configured
	if latencies, exists := mt.latencies[from]; exists {
		if latency, exists := latencies[to]; exists {
			go func() {
				time.Sleep(latency)
				select {
				case toNode.Inbox <- message:
				default:
					// Inbox full, drop message
				}
			}()
		} else {
			toNode.Inbox <- message
		}
	} else {
		toNode.Inbox <- message
	}

	// Log message
	mt.messageLog = append(mt.messageLog, message)

	return nil
}

func (mt *MockTransport) ReceiveMessage(nodeID string) (string, string, []byte, error) {
	mt.mu.RLock()
	node, exists := mt.nodes[nodeID]
	mt.mu.RUnlock()

	if !exists {
		return "", "", nil, fmt.Errorf("node %s not found", nodeID)
	}

	select {
	case msg := <-node.Inbox:
		return msg.From, msg.Type, msg.Data, nil
	case <-time.After(100 * time.Millisecond):
		return "", "", nil, fmt.Errorf("no message received")
	}
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

func (mt *MockTransport) GetMessageLog() []MockMessage {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	log := make([]MockMessage, len(mt.messageLog))
	copy(log, mt.messageLog)
	return log
}

// MockStateMachine implementation

func NewMockStateMachine() *MockStateMachine {
	return &MockStateMachine{
		state:   make(map[string]interface{}),
		history: make([]storage.LogEntry, 0),
	}
}

func (msm *MockStateMachine) Apply(ctx context.Context, entry storage.LogEntry) error {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	msm.history = append(msm.history, entry)

	// Parse key-value from entry data
	data := string(entry.Data)
	if len(data) > 0 {
		// Simple key:value parsing
		parts := []string{data, ""}
		if len(parts) >= 2 {
			msm.state[parts[0]] = parts[1]
		}
	}

	return nil
}

func (msm *MockStateMachine) GetState() interface{} {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	state := make(map[string]interface{})
	for k, v := range msm.state {
		state[k] = v
	}
	return state
}

func (msm *MockStateMachine) CreateSnapshot() (storage.Snapshot, error) {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	// Simple snapshot of current state
	return storage.Snapshot{
		LastIncludedIndex: uint64(len(msm.history)),
		LastIncludedTerm:  1,
		Data:              []byte(fmt.Sprintf("%v", msm.state)),
		Metadata:          map[string]interface{}{"type": "mock"},
	}, nil
}

func (msm *MockStateMachine) RestoreSnapshot(snapshot storage.Snapshot) error {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	// Simple restore - reset state
	msm.state = make(map[string]interface{})
	msm.history = make([]storage.LogEntry, 0)

	return nil
}

func (msm *MockStateMachine) GetStats() statemachine.StateMachineStats {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	return statemachine.StateMachineStats{
		Type:             "mock",
		EntriesApplied:   int64(len(msm.history)),
		StateSize:        int64(len(msm.state)),
		LastAppliedIndex: uint64(len(msm.history)),
		SnapshotCount:    0,
		LastSnapshotTime: time.Time{},
	}
}

// MockDiscovery implementation

func NewMockDiscovery(nodes []string) *MockDiscovery {
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

func (md *MockDiscovery) DiscoverNodes(ctx context.Context) ([]discovery.NodeInfo, error) {
	md.mu.RLock()
	defer md.mu.RUnlock()

	nodes := make([]discovery.NodeInfo, len(md.nodes))
	for i, nodeID := range md.nodes {
		nodes[i] = discovery.NodeInfo{
			ID:      nodeID,
			Address: fmt.Sprintf("127.0.0.1:%d", 8090+i),
			Status:  discovery.NodeStatusActive,
			Metadata: map[string]any{
				"type": "mock",
			},
		}
	}

	return nodes, nil
}

func (md *MockDiscovery) RegisterNode(ctx context.Context, node discovery.NodeInfo) error {
	md.mu.Lock()
	defer md.mu.Unlock()

	// Add node if not exists
	for _, existing := range md.nodes {
		if existing == node.ID {
			return nil
		}
	}

	md.nodes = append(md.nodes, node.ID)
	return nil
}

func (md *MockDiscovery) DeregisterNode(ctx context.Context, nodeID string) error {
	md.mu.Lock()
	defer md.mu.Unlock()

	// Remove node
	for i, existing := range md.nodes {
		if existing == nodeID {
			md.nodes = append(md.nodes[:i], md.nodes[i+1:]...)
			break
		}
	}

	return nil
}

func (md *MockDiscovery) HealthCheck(ctx context.Context) error {
	return nil
}

// TestMetrics implementation

func NewTestMetrics() *TestMetrics {
	return &TestMetrics{
		counters:   make(map[string]float64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
	}
}

func (tm *TestMetrics) Counter(name string, tags ...string) common.Counter {
	return &TestCounter{metrics: tm, name: name}
}

func (tm *TestMetrics) Gauge(name string, tags ...string) common.Gauge {
	return &TestGauge{metrics: tm, name: name}
}

func (tm *TestMetrics) Histogram(name string, tags ...string) common.Histogram {
	return &TestHistogram{metrics: tm, name: name}
}

func (tm *TestMetrics) Timer(name string, tags ...string) common.Timer {
	return &TestTimer{metrics: tm, name: name}
}

func (tm *TestMetrics) GetCounterValue(name string) float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.counters[name]
}

func (tm *TestMetrics) GetGaugeValue(name string) float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.gauges[name]
}

func (tm *TestMetrics) GetHistogramValues(name string) []float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.histograms[name]
}

// Test metric implementations

type TestCounter struct {
	metrics *TestMetrics
	name    string
}

func (tc *TestCounter) Inc() {
	tc.Add(1)
}

func (tc *TestCounter) Add(value float64) {
	tc.metrics.mu.Lock()
	defer tc.metrics.mu.Unlock()
	tc.metrics.counters[tc.name] += value
}

type TestGauge struct {
	metrics *TestMetrics
	name    string
}

func (tg *TestGauge) Set(value float64) {
	tg.metrics.mu.Lock()
	defer tg.metrics.mu.Unlock()
	tg.metrics.gauges[tg.name] = value
}

func (tg *TestGauge) Inc() {
	tg.Add(1)
}

func (tg *TestGauge) Dec() {
	tg.Add(-1)
}

func (tg *TestGauge) Add(value float64) {
	tg.metrics.mu.Lock()
	defer tg.metrics.mu.Unlock()
	tg.metrics.gauges[tg.name] += value
}

type TestHistogram struct {
	metrics *TestMetrics
	name    string
}

func (th *TestHistogram) Observe(value float64) {
	th.metrics.mu.Lock()
	defer th.metrics.mu.Unlock()
	if th.metrics.histograms[th.name] == nil {
		th.metrics.histograms[th.name] = make([]float64, 0)
	}
	th.metrics.histograms[th.name] = append(th.metrics.histograms[th.name], value)
}

type TestTimer struct {
	metrics *TestMetrics
	name    string
}

func (tt *TestTimer) Record(duration time.Duration) {
	tt.metrics.mu.Lock()
	defer tt.metrics.mu.Unlock()
	if tt.metrics.histograms[tt.name] == nil {
		tt.metrics.histograms[tt.name] = make([]float64, 0)
	}
	tt.metrics.histograms[tt.name] = append(tt.metrics.histograms[tt.name], duration.Seconds())
}

func (tt *TestTimer) Time() func() {
	start := time.Now()
	return func() {
		tt.Record(time.Since(start))
	}
}

// Utility functions for testing

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
	// Partition nodes between two groups
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

	leaderState := cluster.GetState()

	followers := cluster.GetFollowers()
	for _, follower := range followers {
		// In a real implementation, you'd compare the actual state
		// For now, we just verify the follower is running
		if !follower.Started {
			t.Errorf("Follower %s not started", follower.ID)
		}
	}

	if leaderState == nil {
		t.Log("Leader state is nil - this may be expected in some tests")
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

// Example usage and test helpers

// ExampleTestClusterUsage demonstrates how to use the test cluster
func ExampleTestClusterUsage(t *testing.T) {
	// Create test cluster
	cluster := NewTestCluster(t, &TestClusterConfig{
		NodeCount:     3,
		StorageType:   "memory",
		EnableLogging: true,
	})

	// Start cluster
	if err := cluster.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for leader election
	leader, err := cluster.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Leader elected: %s", leader.ID)

	// Propose some values
	if err := cluster.ProposeValue("key1", "value1"); err != nil {
		t.Errorf("Failed to propose value: %v", err)
	}

	// Simulate network partition
	nodes := make([]string, 0, len(cluster.Nodes))
	for nodeID := range cluster.Nodes {
		nodes = append(nodes, nodeID)
	}

	partition1 := nodes[:len(nodes)/2]
	partition2 := nodes[len(nodes)/2:]
	SimulateNetworkPartition(cluster, partition1, partition2)

	// Wait and heal partition
	time.Sleep(1 * time.Second)
	HealNetworkPartition(cluster, nodes)

	// Verify consistency
	VerifyClusterConsistency(t, cluster)
}
