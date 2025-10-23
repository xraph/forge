package internal

import (
	"context"
	"time"
)

// NodeRole represents the role of a node in the consensus system
type NodeRole string

const (
	// RoleFollower indicates the node is a follower
	RoleFollower NodeRole = "follower"
	// RoleCandidate indicates the node is a candidate in an election
	RoleCandidate NodeRole = "candidate"
	// RoleLeader indicates the node is the leader
	RoleLeader NodeRole = "leader"
)

// NodeStatus represents the status of a node
type NodeStatus string

const (
	// StatusActive indicates the node is active and healthy
	StatusActive NodeStatus = "active"
	// StatusInactive indicates the node is inactive
	StatusInactive NodeStatus = "inactive"
	// StatusSuspected indicates the node is suspected of failure
	StatusSuspected NodeStatus = "suspected"
	// StatusFailed indicates the node has failed
	StatusFailed NodeStatus = "failed"
)

// NodeInfo represents information about a node
type NodeInfo struct {
	ID            string                 `json:"id"`
	Address       string                 `json:"address"`
	Port          int                    `json:"port"`
	Role          NodeRole               `json:"role"`
	Status        NodeStatus             `json:"status"`
	Term          uint64                 `json:"term"`
	LastHeartbeat time.Time              `json:"last_heartbeat"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// ClusterInfo represents information about the cluster
type ClusterInfo struct {
	ID          string     `json:"id"`
	Leader      string     `json:"leader"`
	Term        uint64     `json:"term"`
	Nodes       []NodeInfo `json:"nodes"`
	TotalNodes  int        `json:"total_nodes"`
	ActiveNodes int        `json:"active_nodes"`
	HasQuorum   bool       `json:"has_quorum"`
	CommitIndex uint64     `json:"commit_index"`
	LastApplied uint64     `json:"last_applied"`
}

// ConsensusStats represents consensus statistics
type ConsensusStats struct {
	NodeID           string        `json:"node_id"`
	ClusterID        string        `json:"cluster_id"`
	Role             NodeRole      `json:"role"`
	Status           NodeStatus    `json:"status"`
	Term             uint64        `json:"term"`
	LeaderID         string        `json:"leader_id"`
	CommitIndex      uint64        `json:"commit_index"`
	LastApplied      uint64        `json:"last_applied"`
	LastLogIndex     uint64        `json:"last_log_index"`
	LastLogTerm      uint64        `json:"last_log_term"`
	ClusterSize      int           `json:"cluster_size"`
	HealthyNodes     int           `json:"healthy_nodes"`
	HasQuorum        bool          `json:"has_quorum"`
	ElectionsTotal   int64         `json:"elections_total"`
	ElectionsFailed  int64         `json:"elections_failed"`
	OperationsTotal  int64         `json:"operations_total"`
	OperationsFailed int64         `json:"operations_failed"`
	OperationsPerSec float64       `json:"operations_per_sec"`
	AverageLatencyMs float64       `json:"average_latency_ms"`
	ErrorRate        float64       `json:"error_rate"`
	LogEntries       int64         `json:"log_entries"`
	SnapshotsTotal   int64         `json:"snapshots_total"`
	LastSnapshotTime time.Time     `json:"last_snapshot_time"`
	Uptime           time.Duration `json:"uptime"`
	StartTime        time.Time     `json:"start_time"`
}

// HealthStatus represents the health status of the consensus system
type HealthStatus struct {
	Healthy     bool                   `json:"healthy"`
	Status      string                 `json:"status"` // "healthy", "degraded", "unhealthy"
	Leader      bool                   `json:"leader"`
	HasQuorum   bool                   `json:"has_quorum"`
	TotalNodes  int                    `json:"total_nodes"`
	ActiveNodes int                    `json:"active_nodes"`
	Details     []HealthCheck          `json:"details"`
	LastCheck   time.Time              `json:"last_check"`
	Checks      map[string]interface{} `json:"checks"`
}

// HealthCheck represents a single health check result
type HealthCheck struct {
	Name      string    `json:"name"`
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// LogEntry represents a log entry in the replicated log
type LogEntry struct {
	Index   uint64    `json:"index"`
	Term    uint64    `json:"term"`
	Type    EntryType `json:"type"`
	Data    []byte    `json:"data"`
	Created time.Time `json:"created"`
}

// EntryType represents the type of log entry
type EntryType int

const (
	// EntryNormal is a normal command entry
	EntryNormal EntryType = iota
	// EntryConfig is a configuration change entry
	EntryConfig
	// EntryBarrier is a barrier entry for read consistency
	EntryBarrier
	// EntryNoop is a no-op entry
	EntryNoop
)

// Snapshot represents a point-in-time snapshot of the state machine
type Snapshot struct {
	Index    uint64    `json:"index"`
	Term     uint64    `json:"term"`
	Data     []byte    `json:"data"`
	Size     int64     `json:"size"`
	Created  time.Time `json:"created"`
	Checksum string    `json:"checksum"`
}

// SnapshotMetadata contains metadata about a snapshot
type SnapshotMetadata struct {
	Index    uint64    `json:"index"`
	Term     uint64    `json:"term"`
	Size     int64     `json:"size"`
	Created  time.Time `json:"created"`
	Checksum string    `json:"checksum"`
}

// Command represents a command to be applied to the state machine
type Command struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// ConsensusService defines the interface for the consensus service
type ConsensusService interface {
	// Start starts the consensus service
	Start(ctx context.Context) error

	// Stop stops the consensus service
	Stop(ctx context.Context) error

	// IsLeader returns true if this node is the leader
	IsLeader() bool

	// GetLeader returns the current leader node ID
	GetLeader() string

	// GetRole returns the current role of this node
	GetRole() NodeRole

	// GetTerm returns the current term
	GetTerm() uint64

	// Apply applies a command to the state machine
	Apply(ctx context.Context, cmd Command) error

	// Read performs a consistent read operation
	Read(ctx context.Context, query interface{}) (interface{}, error)

	// GetStats returns consensus statistics
	GetStats() ConsensusStats

	// HealthCheck performs a health check
	HealthCheck(ctx context.Context) error

	// GetHealthStatus returns detailed health status
	GetHealthStatus(ctx context.Context) HealthStatus

	// GetClusterInfo returns cluster information
	GetClusterInfo() ClusterInfo

	// AddNode adds a node to the cluster
	AddNode(ctx context.Context, nodeID, address string, port int) error

	// RemoveNode removes a node from the cluster
	RemoveNode(ctx context.Context, nodeID string) error

	// TransferLeadership transfers leadership to another node
	TransferLeadership(ctx context.Context, targetNodeID string) error

	// StepDown causes the leader to step down
	StepDown(ctx context.Context) error

	// Snapshot creates a snapshot
	Snapshot(ctx context.Context) error

	// UpdateConfig updates the configuration
	UpdateConfig(ctx context.Context, config Config) error
}

// RaftNode defines the interface for Raft node operations
type RaftNode interface {
	// Start starts the Raft node
	Start(ctx context.Context) error

	// Stop stops the Raft node
	Stop(ctx context.Context) error

	// IsLeader returns true if this node is the leader
	IsLeader() bool

	// GetLeader returns the current leader ID
	GetLeader() string

	// GetTerm returns the current term
	GetTerm() uint64

	// GetRole returns the current role
	GetRole() NodeRole

	// Apply applies a log entry
	Apply(ctx context.Context, entry LogEntry) error

	// AppendEntries handles AppendEntries RPC
	AppendEntries(ctx context.Context, req *AppendEntriesRequest) (*AppendEntriesResponse, error)

	// RequestVote handles RequestVote RPC
	RequestVote(ctx context.Context, req *RequestVoteRequest) (*RequestVoteResponse, error)

	// InstallSnapshot handles InstallSnapshot RPC
	InstallSnapshot(ctx context.Context, req *InstallSnapshotRequest) (*InstallSnapshotResponse, error)

	// GetStats returns Raft statistics
	GetStats() RaftStats
}

// RaftStats represents Raft node statistics
type RaftStats struct {
	NodeID        string    `json:"node_id"`
	Role          NodeRole  `json:"role"`
	Term          uint64    `json:"term"`
	Leader        string    `json:"leader"`
	CommitIndex   uint64    `json:"commit_index"`
	LastApplied   uint64    `json:"last_applied"`
	LastLogIndex  uint64    `json:"last_log_index"`
	LastLogTerm   uint64    `json:"last_log_term"`
	VotedFor      string    `json:"voted_for"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// AppendEntriesRequest represents an AppendEntries RPC request
type AppendEntriesRequest struct {
	Term         uint64     `json:"term"`
	LeaderID     string     `json:"leader_id"`
	PrevLogIndex uint64     `json:"prev_log_index"`
	PrevLogTerm  uint64     `json:"prev_log_term"`
	Entries      []LogEntry `json:"entries"`
	LeaderCommit uint64     `json:"leader_commit"`
}

// AppendEntriesResponse represents an AppendEntries RPC response
type AppendEntriesResponse struct {
	Term       uint64 `json:"term"`
	Success    bool   `json:"success"`
	MatchIndex uint64 `json:"match_index"`
	NodeID     string `json:"node_id"`
}

// RequestVoteRequest represents a RequestVote RPC request
type RequestVoteRequest struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
	PreVote      bool   `json:"pre_vote"`
}

// RequestVoteResponse represents a RequestVote RPC response
type RequestVoteResponse struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	NodeID      string `json:"node_id"`
}

// InstallSnapshotRequest represents an InstallSnapshot RPC request
type InstallSnapshotRequest struct {
	Term              uint64 `json:"term"`
	LeaderID          string `json:"leader_id"`
	LastIncludedIndex uint64 `json:"last_included_index"`
	LastIncludedTerm  uint64 `json:"last_included_term"`
	Offset            uint64 `json:"offset"`
	Data              []byte `json:"data"`
	Done              bool   `json:"done"`
}

// InstallSnapshotResponse represents an InstallSnapshot RPC response
type InstallSnapshotResponse struct {
	Term   uint64 `json:"term"`
	NodeID string `json:"node_id"`
}

// StateMachine defines the interface for the replicated state machine
type StateMachine interface {
	// Apply applies a log entry to the state machine
	Apply(entry LogEntry) error

	// Snapshot creates a snapshot of the current state
	Snapshot() (*Snapshot, error)

	// Restore restores the state machine from a snapshot
	Restore(snapshot *Snapshot) error

	// Query performs a read-only query
	Query(query interface{}) (interface{}, error)
}

// ClusterManager defines the interface for cluster management
type ClusterManager interface {
	// GetNodes returns all nodes in the cluster
	GetNodes() []NodeInfo

	// GetNode returns information about a specific node
	GetNode(nodeID string) (*NodeInfo, error)

	// AddNode adds a node to the cluster
	AddNode(nodeID, address string, port int) error

	// RemoveNode removes a node from the cluster
	RemoveNode(nodeID string) error

	// UpdateNode updates node information
	UpdateNode(nodeID string, info NodeInfo) error

	// GetLeader returns the current leader node
	GetLeader() *NodeInfo

	// HasQuorum returns true if the cluster has quorum
	HasQuorum() bool

	// GetClusterSize returns the size of the cluster
	GetClusterSize() int

	// GetHealthyNodes returns the number of healthy nodes
	GetHealthyNodes() int
}

// Transport defines the interface for network transport
type Transport interface {
	// Start starts the transport
	Start(ctx context.Context) error

	// Stop stops the transport
	Stop(ctx context.Context) error

	// Send sends a message to a peer
	Send(ctx context.Context, target string, message interface{}) error

	// Receive returns a channel for receiving messages
	Receive() <-chan Message

	// AddPeer adds a peer
	AddPeer(nodeID, address string, port int) error

	// RemovePeer removes a peer
	RemovePeer(nodeID string) error

	// GetAddress returns the local address
	GetAddress() string
}

// Message represents a message sent between nodes
type Message struct {
	Type      MessageType `json:"type"`
	From      string      `json:"from"`
	To        string      `json:"to"`
	Payload   interface{} `json:"payload"`
	Timestamp int64       `json:"timestamp"`
}

// MessageType represents the type of message
type MessageType string

const (
	// MessageTypeAppendEntries is for AppendEntries RPC
	MessageTypeAppendEntries MessageType = "append_entries"
	// MessageTypeRequestVote is for RequestVote RPC
	MessageTypeRequestVote MessageType = "request_vote"
	// MessageTypeInstallSnapshot is for InstallSnapshot RPC
	MessageTypeInstallSnapshot MessageType = "install_snapshot"
	// MessageTypeHeartbeat is for heartbeat messages
	MessageTypeHeartbeat MessageType = "heartbeat"
)

// Discovery defines the interface for service discovery
type Discovery interface {
	// Start starts the discovery service
	Start(ctx context.Context) error

	// Stop stops the discovery service
	Stop(ctx context.Context) error

	// Register registers this node with the discovery service
	Register(ctx context.Context, node NodeInfo) error

	// Unregister unregisters this node from the discovery service
	Unregister(ctx context.Context) error

	// GetNodes returns all discovered nodes
	GetNodes(ctx context.Context) ([]NodeInfo, error)

	// Watch watches for node changes
	Watch(ctx context.Context) (<-chan NodeChangeEvent, error)
}

// NodeChangeEvent represents a node change event
type NodeChangeEvent struct {
	Type NodeChangeType `json:"type"`
	Node NodeInfo       `json:"node"`
}

// NodeChangeType represents the type of node change
type NodeChangeType string

const (
	// NodeChangeTypeAdded indicates a node was added
	NodeChangeTypeAdded NodeChangeType = "added"
	// NodeChangeTypeRemoved indicates a node was removed
	NodeChangeTypeRemoved NodeChangeType = "removed"
	// NodeChangeTypeUpdated indicates a node was updated
	NodeChangeTypeUpdated NodeChangeType = "updated"
)

// DiscoveryEvent represents a discovery event
type DiscoveryEvent struct {
	Type      DiscoveryEventType `json:"type"`
	NodeID    string             `json:"node_id"`
	Address   string             `json:"address"`
	Port      int                `json:"port"`
	Timestamp time.Time          `json:"timestamp"`
}

// DiscoveryEventType represents the type of discovery event
type DiscoveryEventType string

const (
	// DiscoveryEventTypeJoin indicates a node joined
	DiscoveryEventTypeJoin DiscoveryEventType = "join"
	// DiscoveryEventTypeLeave indicates a node left
	DiscoveryEventTypeLeave DiscoveryEventType = "leave"
	// DiscoveryEventTypeUpdate indicates a node updated
	DiscoveryEventTypeUpdate DiscoveryEventType = "update"
)

// Storage defines the interface for persistent storage
type Storage interface {
	// Start starts the storage backend
	Start(ctx context.Context) error

	// Stop stops the storage backend
	Stop(ctx context.Context) error

	// Set stores a key-value pair
	Set(key, value []byte) error

	// Get retrieves a value by key
	Get(key []byte) ([]byte, error)

	// Delete deletes a key
	Delete(key []byte) error

	// Batch executes a batch of operations
	Batch(ops []BatchOp) error

	// GetRange retrieves a range of key-value pairs
	GetRange(start, end []byte) ([]KeyValue, error)

	// Close closes the storage backend
	Close() error
}

// BatchOp represents a batch operation
type BatchOp struct {
	Type  BatchOpType
	Key   []byte
	Value []byte
}

// BatchOpType represents the type of batch operation
type BatchOpType int

const (
	// BatchOpSet is a set operation
	BatchOpSet BatchOpType = iota
	// BatchOpDelete is a delete operation
	BatchOpDelete
)

// KeyValue represents a key-value pair
type KeyValue struct {
	Key   []byte
	Value []byte
}
