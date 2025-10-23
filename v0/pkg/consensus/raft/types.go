package raft

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/consensus/storage"
)

// RPC Request/Response types

// AppendEntriesRequest represents an AppendEntries RPC request
type AppendEntriesRequest struct {
	Term         uint64             `json:"term"`
	LeaderID     string             `json:"leader_id"`
	PrevLogIndex uint64             `json:"prev_log_index"`
	PrevLogTerm  uint64             `json:"prev_log_term"`
	Entries      []storage.LogEntry `json:"entries"`
	LeaderCommit uint64             `json:"leader_commit"`
}

// AppendEntriesResponse represents an AppendEntries RPC response
type AppendEntriesResponse struct {
	Term       uint64 `json:"term"`
	Success    bool   `json:"success"`
	NodeID     string `json:"node_id"`
	MatchIndex uint64 `json:"match_index,omitempty"`
}

// RequestVoteRequest represents a RequestVote RPC request
type RequestVoteRequest struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// RequestVoteResponse represents a RequestVote RPC response
type RequestVoteResponse struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	VoterID     string `json:"voter_id"`
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

// ElectionResult represents the result of an election
type ElectionResult struct {
	Success       bool          `json:"success"`
	Term          uint64        `json:"term"`
	VotesReceived int           `json:"votes_received"`
	VotesNeeded   int           `json:"votes_needed"`
	Duration      time.Duration `json:"duration"`
	StartTime     time.Time     `json:"start_time"`
}

// VoteResult represents the result of a vote request
type VoteResult struct {
	NodeID      string `json:"node_id"`
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	Error       error  `json:"error,omitempty"`
}

// NodeInfo represents information about a node
type NodeInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Role     string    `json:"role"`
	Status   string    `json:"status"`
	Term     uint64    `json:"term"`
	LastSeen time.Time `json:"last_seen"`
}

// ClusterInfo represents information about the cluster
type ClusterInfo struct {
	ID          string     `json:"id"`
	Nodes       []NodeInfo `json:"nodes"`
	Leader      string     `json:"leader"`
	Term        uint64     `json:"term"`
	CommitIndex uint64     `json:"commit_index"`
	LastApplied uint64     `json:"last_applied"`
	Status      string     `json:"status"`
}

// LogInfo represents information about the log
type LogInfo struct {
	FirstIndex  uint64 `json:"first_index"`
	LastIndex   uint64 `json:"last_index"`
	CommitIndex uint64 `json:"commit_index"`
	Size        int64  `json:"size"`
	EntryCount  int64  `json:"entry_count"`
}

// SnapshotInfo represents information about snapshots
type SnapshotInfo struct {
	LastIncludedIndex uint64    `json:"last_included_index"`
	LastIncludedTerm  uint64    `json:"last_included_term"`
	Size              int64     `json:"size"`
	CreatedAt         time.Time `json:"created_at"`
	Count             int64     `json:"count"`
}

// Configuration represents cluster configuration
type Configuration struct {
	Nodes     []string  `json:"nodes"`
	Index     uint64    `json:"index"`
	Term      uint64    `json:"term"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConfigurationChange represents a configuration change
type ConfigurationChange struct {
	Type    ConfigurationChangeType `json:"type"`
	NodeID  string                  `json:"node_id"`
	Address string                  `json:"address"`
}

// ConfigurationChangeType represents the type of configuration change
type ConfigurationChangeType string

const (
	ConfigurationChangeAddNode    ConfigurationChangeType = "add_node"
	ConfigurationChangeRemoveNode ConfigurationChangeType = "remove_node"
	ConfigurationChangeUpdateNode ConfigurationChangeType = "update_node"
)

// RaftError Error types
type RaftError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Term    uint64 `json:"term,omitempty"`
	NodeID  string `json:"node_id,omitempty"`
}

func (e *RaftError) Error() string {
	return e.Message
}

// Common error codes
const (
	ErrNotLeader       = "not_leader"
	ErrStaleterm       = "stale_term"
	ErrLogInconsistent = "log_inconsistent"
	ErrNotFound        = "not_found"
	ErrTimeout         = "timeout"
	ErrShutdown        = "shutdown"
)

// NewRaftError creates a new Raft error
func NewRaftError(code, message string) *RaftError {
	return &RaftError{
		Code:    code,
		Message: message,
	}
}

// RaftNode interface defines the Raft node operations
type RaftNode interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsLeader() bool
	GetLeader() string
	GetTerm() uint64
	GetCommitIndex() uint64
	AppendEntries(ctx context.Context, entries []storage.LogEntry) error
	RequestVote(ctx context.Context, req *RequestVoteRequest) (*RequestVoteResponse, error)
	InstallSnapshot(ctx context.Context, snapshot *storage.Snapshot) error
	HealthCheck(ctx context.Context) error
	GetStats() RaftStats
}
