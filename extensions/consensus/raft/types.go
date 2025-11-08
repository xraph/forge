package raft

import (
	"time"

	"github.com/xraph/forge/extensions/consensus/internal"
)

// State represents the Raft node state.
type State int

const (
	// StateFollower represents follower state.
	StateFollower State = iota
	// StateCandidate represents candidate state.
	StateCandidate
	// StateLeader represents leader state.
	StateLeader
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateFollower:
		return "follower"
	case StateCandidate:
		return "candidate"
	case StateLeader:
		return "leader"
	default:
		return "unknown"
	}
}

// VoteResponse represents a vote response from a peer.
type VoteResponse struct {
	Term        uint64
	VoteGranted bool
	NodeID      string
}

// ReplicationState represents the replication state for a peer.
type ReplicationState struct {
	NextIndex     uint64
	MatchIndex    uint64
	LastContact   time.Time
	Inflight      int
	LastHeartbeat time.Time
}

// LogEntryBatch represents a batch of log entries.
type LogEntryBatch struct {
	Entries    []internal.LogEntry
	StartIndex uint64
	EndIndex   uint64
}

// SnapshotMeta represents snapshot metadata.
type SnapshotMeta struct {
	Index    uint64
	Term     uint64
	Size     int64
	Checksum string
	Created  time.Time
}

// ElectionResult represents the result of an election.
type ElectionResult struct {
	Won          bool
	Term         uint64
	VotesFor     int
	VotesAgainst int
	VotesNeeded  int
}

// ReplicationResult represents the result of log replication.
type ReplicationResult struct {
	Success    bool
	MatchIndex uint64
	NodeID     string
	Term       uint64
	Error      error
}
