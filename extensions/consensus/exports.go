package consensus

import "github.com/xraph/forge/extensions/consensus/internal"

// DI container keys for consensus extension services.
const (
	// ServiceKey is the DI key for the consensus service.
	ServiceKey = "consensus"
	// ServiceKeyLegacy is the legacy DI key for the consensus service.
	ServiceKeyLegacy = "consensus:service"

	// ClusterManagerKey is the DI key for the cluster manager.
	ClusterManagerKey = "consensus:cluster"
	// RaftNodeKey is the DI key for the Raft node.
	RaftNodeKey = "consensus:raft"
	// StateMachineKey is the DI key for the state machine.
	StateMachineKey = "consensus:statemachine"
	// LeadershipCheckerKey is the DI key for the leadership checker.
	LeadershipCheckerKey = "consensus:leadership"
)

// Type exports.
type (
	NodeRole                = internal.NodeRole
	NodeStatus              = internal.NodeStatus
	NodeInfo                = internal.NodeInfo
	ClusterInfo             = internal.ClusterInfo
	ConsensusStats          = internal.ConsensusStats
	HealthStatus            = internal.HealthStatus
	HealthCheck             = internal.HealthCheck
	LogEntry                = internal.LogEntry
	EntryType               = internal.EntryType
	Snapshot                = internal.Snapshot
	Command                 = internal.Command
	ConsensusService        = internal.ConsensusService
	RaftNode                = internal.RaftNode
	RaftStats               = internal.RaftStats
	AppendEntriesRequest    = internal.AppendEntriesRequest
	AppendEntriesResponse   = internal.AppendEntriesResponse
	RequestVoteRequest      = internal.RequestVoteRequest
	RequestVoteResponse     = internal.RequestVoteResponse
	InstallSnapshotRequest  = internal.InstallSnapshotRequest
	InstallSnapshotResponse = internal.InstallSnapshotResponse
	StateMachine            = internal.StateMachine
	ClusterManager          = internal.ClusterManager
	Transport               = internal.Transport
	Message                 = internal.Message
	MessageType             = internal.MessageType
	Discovery               = internal.Discovery
	NodeChangeEvent         = internal.NodeChangeEvent
	NodeChangeType          = internal.NodeChangeType
	Storage                 = internal.Storage
	BatchOp                 = internal.BatchOp
	BatchOpType             = internal.BatchOpType
	KeyValue                = internal.KeyValue
)

// Config exports.
type (
	Config              = internal.Config
	PeerConfig          = internal.PeerConfig
	RaftConfig          = internal.RaftConfig
	TransportConfig     = internal.TransportConfig
	DiscoveryConfig     = internal.DiscoveryConfig
	StorageConfig       = internal.StorageConfig
	BadgerOptions       = internal.BadgerOptions
	BoltOptions         = internal.BoltOptions
	ElectionConfig      = internal.ElectionConfig
	HealthConfig        = internal.HealthConfig
	ObservabilityConfig = internal.ObservabilityConfig
	MetricsConfig       = internal.MetricsConfig
	TracingConfig       = internal.TracingConfig
	LoggingConfig       = internal.LoggingConfig
	SecurityConfig      = internal.SecurityConfig
	ResilienceConfig    = internal.ResilienceConfig
	AdminAPIConfig      = internal.AdminAPIConfig
	EventsConfig        = internal.EventsConfig
	AdvancedConfig      = internal.AdvancedConfig
	ConfigOption        = internal.ConfigOption
)

// Event exports.
type (
	ConsensusEventType     = internal.ConsensusEventType
	ConsensusEvent         = internal.ConsensusEvent
	LeaderElectedEvent     = internal.LeaderElectedEvent
	RoleChangedEvent       = internal.RoleChangedEvent
	MembershipChangedEvent = internal.MembershipChangedEvent
	QuorumStatusEvent      = internal.QuorumStatusEvent
	SnapshotEvent          = internal.SnapshotEvent
	HealthStatusEvent      = internal.HealthStatusEvent
)

// Constants - Node Roles.
const (
	RoleFollower  = internal.RoleFollower
	RoleCandidate = internal.RoleCandidate
	RoleLeader    = internal.RoleLeader
)

// Constants - Node Status.
const (
	StatusActive    = internal.StatusActive
	StatusInactive  = internal.StatusInactive
	StatusSuspected = internal.StatusSuspected
	StatusFailed    = internal.StatusFailed
)

// Constants - Entry Types.
const (
	EntryNormal  = internal.EntryNormal
	EntryConfig  = internal.EntryConfig
	EntryBarrier = internal.EntryBarrier
	EntryNoop    = internal.EntryNoop
)

// Constants - Message Types.
const (
	MessageTypeAppendEntries   = internal.MessageTypeAppendEntries
	MessageTypeRequestVote     = internal.MessageTypeRequestVote
	MessageTypeInstallSnapshot = internal.MessageTypeInstallSnapshot
	MessageTypeHeartbeat       = internal.MessageTypeHeartbeat
)

// Constants - Node Change Types.
const (
	NodeChangeTypeAdded   = internal.NodeChangeTypeAdded
	NodeChangeTypeRemoved = internal.NodeChangeTypeRemoved
	NodeChangeTypeUpdated = internal.NodeChangeTypeUpdated
)

// Constants - Batch Op Types.
const (
	BatchOpSet    = internal.BatchOpSet
	BatchOpDelete = internal.BatchOpDelete
)

// Constants - Event Types.
const (
	ConsensusEventNodeStarted       = internal.ConsensusEventNodeStarted
	ConsensusEventNodeStopped       = internal.ConsensusEventNodeStopped
	ConsensusEventNodeJoined        = internal.ConsensusEventNodeJoined
	ConsensusEventNodeLeft          = internal.ConsensusEventNodeLeft
	ConsensusEventNodeFailed        = internal.ConsensusEventNodeFailed
	ConsensusEventNodeRecovered     = internal.ConsensusEventNodeRecovered
	ConsensusEventLeaderElected     = internal.ConsensusEventLeaderElected
	ConsensusEventLeaderStepDown    = internal.ConsensusEventLeaderStepDown
	ConsensusEventLeaderTransfer    = internal.ConsensusEventLeaderTransfer
	ConsensusEventLeaderLost        = internal.ConsensusEventLeaderLost
	ConsensusEventRoleChanged       = internal.ConsensusEventRoleChanged
	ConsensusEventBecameFollower    = internal.ConsensusEventBecameFollower
	ConsensusEventBecameCandidate   = internal.ConsensusEventBecameCandidate
	ConsensusEventBecameLeader      = internal.ConsensusEventBecameLeader
	ConsensusEventClusterFormed     = internal.ConsensusEventClusterFormed
	ConsensusEventClusterUpdated    = internal.ConsensusEventClusterUpdated
	ConsensusEventQuorumAchieved    = internal.ConsensusEventQuorumAchieved
	ConsensusEventQuorumLost        = internal.ConsensusEventQuorumLost
	ConsensusEventMembershipChanged = internal.ConsensusEventMembershipChanged
	ConsensusEventLogAppended       = internal.ConsensusEventLogAppended
	ConsensusEventLogCommitted      = internal.ConsensusEventLogCommitted
	ConsensusEventLogCompacted      = internal.ConsensusEventLogCompacted
	ConsensusEventLogTruncated      = internal.ConsensusEventLogTruncated
	ConsensusEventSnapshotStarted   = internal.ConsensusEventSnapshotStarted
	ConsensusEventSnapshotCompleted = internal.ConsensusEventSnapshotCompleted
	ConsensusEventSnapshotFailed    = internal.ConsensusEventSnapshotFailed
	ConsensusEventSnapshotRestored  = internal.ConsensusEventSnapshotRestored
	ConsensusEventHealthy           = internal.ConsensusEventHealthy
	ConsensusEventUnhealthy         = internal.ConsensusEventUnhealthy
	ConsensusEventDegraded          = internal.ConsensusEventDegraded
	ConsensusEventRecovering        = internal.ConsensusEventRecovering
	ConsensusEventConfigUpdated     = internal.ConsensusEventConfigUpdated
	ConsensusEventConfigReloaded    = internal.ConsensusEventConfigReloaded
)

// Error exports.
var (
	ErrNotLeader            = internal.ErrNotLeader
	ErrNoLeader             = internal.ErrNoLeader
	ErrNotStarted           = internal.ErrNotStarted
	ErrAlreadyStarted       = internal.ErrAlreadyStarted
	ErrNodeNotFound         = internal.ErrNodeNotFound
	ErrClusterNotFound      = internal.ErrClusterNotFound
	ErrStorageUnavailable   = internal.ErrStorageUnavailable
	ErrTransportUnavailable = internal.ErrTransportUnavailable
	ErrDiscoveryUnavailable = internal.ErrDiscoveryUnavailable
	ErrNoQuorum             = internal.ErrNoQuorum
	ErrInvalidTerm          = internal.ErrInvalidTerm
	ErrStaleTerm            = internal.ErrStaleTerm
	ErrLogInconsistent      = internal.ErrLogInconsistent
	ErrSnapshotFailed       = internal.ErrSnapshotFailed
	ErrCompactionFailed     = internal.ErrCompactionFailed
	ErrElectionTimeout      = internal.ErrElectionTimeout
	ErrInvalidPeer          = internal.ErrInvalidPeer
	ErrPeerExists           = internal.ErrPeerExists
	ErrPeerNotFound         = internal.ErrPeerNotFound
	ErrInsufficientPeers    = internal.ErrInsufficientPeers
	ErrInvalidConfig        = internal.ErrInvalidConfig
)

// Error helper functions.
var (
	NewNotLeaderError = internal.NewNotLeaderError
	NewNoLeaderError  = internal.NewNoLeaderError
	NewTimeoutError   = internal.NewTimeoutError
	NewNoQuorumError  = internal.NewNoQuorumError
	NewStaleTermError = internal.NewStaleTermError
	IsNotLeaderError  = internal.IsNotLeaderError
	IsNoLeaderError   = internal.IsNoLeaderError
	IsNoQuorumError   = internal.IsNoQuorumError
	IsStaleTermError  = internal.IsStaleTermError
	IsRetryable       = internal.IsRetryable
	IsFatal           = internal.IsFatal
)

// Note: Additional type exports for production features would go here
// type SnapshotMetadata = internal.SnapshotMetadata (not yet implemented)

// Note: Message type constants are available through MessageType

// Config functions.
var (
	DefaultConfig     = internal.DefaultConfig
	WithNodeID        = internal.WithNodeID
	WithClusterID     = internal.WithClusterID
	WithBindAddress   = internal.WithBindAddress
	WithPeers         = internal.WithPeers
	WithTransportType = internal.WithTransportType
	WithDiscoveryType = internal.WithDiscoveryType
	WithStorageType   = internal.WithStorageType
	WithStoragePath   = internal.WithStoragePath
	WithTLS           = internal.WithTLS
	WithMTLS          = internal.WithMTLS
	WithConfig        = internal.WithConfig
	WithRequireConfig = internal.WithRequireConfig
)
