package consensus

import "github.com/xraph/forge/extensions/consensus/internal"

// Type exports.
type NodeRole = internal.NodeRole
type NodeStatus = internal.NodeStatus
type NodeInfo = internal.NodeInfo
type ClusterInfo = internal.ClusterInfo
type ConsensusStats = internal.ConsensusStats
type HealthStatus = internal.HealthStatus
type HealthCheck = internal.HealthCheck
type LogEntry = internal.LogEntry
type EntryType = internal.EntryType
type Snapshot = internal.Snapshot
type Command = internal.Command
type ConsensusService = internal.ConsensusService
type RaftNode = internal.RaftNode
type RaftStats = internal.RaftStats
type AppendEntriesRequest = internal.AppendEntriesRequest
type AppendEntriesResponse = internal.AppendEntriesResponse
type RequestVoteRequest = internal.RequestVoteRequest
type RequestVoteResponse = internal.RequestVoteResponse
type InstallSnapshotRequest = internal.InstallSnapshotRequest
type InstallSnapshotResponse = internal.InstallSnapshotResponse
type StateMachine = internal.StateMachine
type ClusterManager = internal.ClusterManager
type Transport = internal.Transport
type Message = internal.Message
type MessageType = internal.MessageType
type Discovery = internal.Discovery
type NodeChangeEvent = internal.NodeChangeEvent
type NodeChangeType = internal.NodeChangeType
type Storage = internal.Storage
type BatchOp = internal.BatchOp
type BatchOpType = internal.BatchOpType
type KeyValue = internal.KeyValue

// Config exports.
type Config = internal.Config
type PeerConfig = internal.PeerConfig
type RaftConfig = internal.RaftConfig
type TransportConfig = internal.TransportConfig
type DiscoveryConfig = internal.DiscoveryConfig
type StorageConfig = internal.StorageConfig
type BadgerOptions = internal.BadgerOptions
type BoltOptions = internal.BoltOptions
type ElectionConfig = internal.ElectionConfig
type HealthConfig = internal.HealthConfig
type ObservabilityConfig = internal.ObservabilityConfig
type MetricsConfig = internal.MetricsConfig
type TracingConfig = internal.TracingConfig
type LoggingConfig = internal.LoggingConfig
type SecurityConfig = internal.SecurityConfig
type ResilienceConfig = internal.ResilienceConfig
type AdminAPIConfig = internal.AdminAPIConfig
type EventsConfig = internal.EventsConfig
type AdvancedConfig = internal.AdvancedConfig
type ConfigOption = internal.ConfigOption

// Event exports.
type ConsensusEventType = internal.ConsensusEventType
type ConsensusEvent = internal.ConsensusEvent
type LeaderElectedEvent = internal.LeaderElectedEvent
type RoleChangedEvent = internal.RoleChangedEvent
type MembershipChangedEvent = internal.MembershipChangedEvent
type QuorumStatusEvent = internal.QuorumStatusEvent
type SnapshotEvent = internal.SnapshotEvent
type HealthStatusEvent = internal.HealthStatusEvent

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
