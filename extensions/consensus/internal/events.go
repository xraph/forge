package internal

import "time"

// ConsensusEventType represents the type of consensus event.
type ConsensusEventType string

const (
	// Node lifecycle events.
	ConsensusEventNodeStarted   ConsensusEventType = "consensus.node.started"
	ConsensusEventNodeStopped   ConsensusEventType = "consensus.node.stopped"
	ConsensusEventNodeJoined    ConsensusEventType = "consensus.node.joined"
	ConsensusEventNodeLeft      ConsensusEventType = "consensus.node.left"
	ConsensusEventNodeFailed    ConsensusEventType = "consensus.node.failed"
	ConsensusEventNodeRecovered ConsensusEventType = "consensus.node.recovered"

	// Leadership events.
	ConsensusEventLeaderElected  ConsensusEventType = "consensus.leader.elected"
	ConsensusEventLeaderStepDown ConsensusEventType = "consensus.leader.stepdown"
	ConsensusEventLeaderTransfer ConsensusEventType = "consensus.leader.transfer"
	ConsensusEventLeaderLost     ConsensusEventType = "consensus.leader.lost"

	// Role change events.
	ConsensusEventRoleChanged     ConsensusEventType = "consensus.role.changed"
	ConsensusEventBecameFollower  ConsensusEventType = "consensus.role.follower"
	ConsensusEventBecameCandidate ConsensusEventType = "consensus.role.candidate"
	ConsensusEventBecameLeader    ConsensusEventType = "consensus.role.leader"

	// Cluster events.
	ConsensusEventClusterFormed     ConsensusEventType = "consensus.cluster.formed"
	ConsensusEventClusterUpdated    ConsensusEventType = "consensus.cluster.updated"
	ConsensusEventQuorumAchieved    ConsensusEventType = "consensus.cluster.quorum.achieved"
	ConsensusEventQuorumLost        ConsensusEventType = "consensus.cluster.quorum.lost"
	ConsensusEventMembershipChanged ConsensusEventType = "consensus.cluster.membership.changed"

	// Log events.
	ConsensusEventLogAppended  ConsensusEventType = "consensus.log.appended"
	ConsensusEventLogCommitted ConsensusEventType = "consensus.log.committed"
	ConsensusEventLogCompacted ConsensusEventType = "consensus.log.compacted"
	ConsensusEventLogTruncated ConsensusEventType = "consensus.log.truncated"

	// Snapshot events.
	ConsensusEventSnapshotStarted   ConsensusEventType = "consensus.snapshot.started"
	ConsensusEventSnapshotCompleted ConsensusEventType = "consensus.snapshot.completed"
	ConsensusEventSnapshotFailed    ConsensusEventType = "consensus.snapshot.failed"
	ConsensusEventSnapshotRestored  ConsensusEventType = "consensus.snapshot.restored"

	// Health events.
	ConsensusEventHealthy    ConsensusEventType = "consensus.health.healthy"
	ConsensusEventUnhealthy  ConsensusEventType = "consensus.health.unhealthy"
	ConsensusEventDegraded   ConsensusEventType = "consensus.health.degraded"
	ConsensusEventRecovering ConsensusEventType = "consensus.health.recovering"

	// Configuration events.
	ConsensusEventConfigUpdated  ConsensusEventType = "consensus.config.updated"
	ConsensusEventConfigReloaded ConsensusEventType = "consensus.config.reloaded"
)

// ConsensusEvent represents a consensus event.
type ConsensusEvent struct {
	Type      ConsensusEventType `json:"type"`
	NodeID    string             `json:"node_id"`
	ClusterID string             `json:"cluster_id"`
	Data      map[string]any     `json:"data"`
	Timestamp time.Time          `json:"timestamp"`
}

// LeaderElectedEvent contains data for leader election events.
type LeaderElectedEvent struct {
	LeaderID         string        `json:"leader_id"`
	Term             uint64        `json:"term"`
	VotesReceived    int           `json:"votes_received"`
	TotalVotes       int           `json:"total_votes"`
	ElectionDuration time.Duration `json:"election_duration"`
}

// RoleChangedEvent contains data for role change events.
type RoleChangedEvent struct {
	OldRole string `json:"old_role"`
	NewRole string `json:"new_role"`
	Term    uint64 `json:"term"`
	Reason  string `json:"reason,omitempty"`
}

// MembershipChangedEvent contains data for membership change events.
type MembershipChangedEvent struct {
	Action     string   `json:"action"` // "added", "removed", "updated"
	NodeID     string   `json:"affected_node_id"`
	OldMembers []string `json:"old_members"`
	NewMembers []string `json:"new_members"`
}

// QuorumStatusEvent contains data for quorum status events.
type QuorumStatusEvent struct {
	HasQuorum         bool `json:"has_quorum"`
	TotalNodes        int  `json:"total_nodes"`
	HealthyNodes      int  `json:"healthy_nodes"`
	RequiredForQuorum int  `json:"required_for_quorum"`
}

// SnapshotEvent contains data for snapshot events.
type SnapshotEvent struct {
	Index    uint64        `json:"index"`
	Term     uint64        `json:"term"`
	Size     int64         `json:"size"`
	Duration time.Duration `json:"duration,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// HealthStatusEvent contains data for health status events.
type HealthStatusEvent struct {
	Status       string   `json:"status"` // "healthy", "unhealthy", "degraded"
	Details      string   `json:"details,omitempty"`
	ChecksFailed []string `json:"checks_failed,omitempty"`
}
