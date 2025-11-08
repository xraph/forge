package internal

import (
	stderrors "errors"
	"time"

	"github.com/xraph/forge/internal/errors"
)

// Consensus error codes.
const (
	ErrCodeNotLeader            = "CONSENSUS_NOT_LEADER"
	ErrCodeNoLeader             = "CONSENSUS_NO_LEADER"
	ErrCodeNotStarted           = "CONSENSUS_NOT_STARTED"
	ErrCodeAlreadyStarted       = "CONSENSUS_ALREADY_STARTED"
	ErrCodeNodeNotFound         = "CONSENSUS_NODE_NOT_FOUND"
	ErrCodeClusterNotFound      = "CONSENSUS_CLUSTER_NOT_FOUND"
	ErrCodeStorageUnavailable   = "CONSENSUS_STORAGE_UNAVAILABLE"
	ErrCodeTransportUnavailable = "CONSENSUS_TRANSPORT_UNAVAILABLE"
	ErrCodeDiscoveryUnavailable = "CONSENSUS_DISCOVERY_UNAVAILABLE"
	ErrCodeNoQuorum             = "CONSENSUS_NO_QUORUM"
	ErrCodeInvalidTerm          = "CONSENSUS_INVALID_TERM"
	ErrCodeStaleTerm            = "CONSENSUS_STALE_TERM"
	ErrCodeLogInconsistent      = "CONSENSUS_LOG_INCONSISTENT"
	ErrCodeSnapshotFailed       = "CONSENSUS_SNAPSHOT_FAILED"
	ErrCodeCompactionFailed     = "CONSENSUS_COMPACTION_FAILED"
	ErrCodeElectionTimeout      = "CONSENSUS_ELECTION_TIMEOUT"
	ErrCodeInvalidPeer          = "CONSENSUS_INVALID_PEER"
	ErrCodePeerExists           = "CONSENSUS_PEER_EXISTS"
	ErrCodePeerNotFound         = "CONSENSUS_PEER_NOT_FOUND"
	ErrCodeInsufficientPeers    = "CONSENSUS_INSUFFICIENT_PEERS"
	ErrCodeNoAvailablePeers     = "CONSENSUS_NO_AVAILABLE_PEERS"
	ErrCodeFailoverFailed       = "CONSENSUS_FAILOVER_FAILED"
	ErrCodeOperationFailed      = "CONSENSUS_OPERATION_FAILED"
	ErrCodeReplicationFailed    = "CONSENSUS_REPLICATION_FAILED"
	ErrCodeTimeout              = "CONSENSUS_TIMEOUT"
	ErrCodeConnectionFailed     = "CONSENSUS_CONNECTION_FAILED"
	ErrCodeStaleRead            = "CONSENSUS_STALE_READ"
	ErrCodeRateLimitExceeded    = "CONSENSUS_RATE_LIMIT_EXCEEDED"
	ErrCodeAuthenticationFailed = "CONSENSUS_AUTHENTICATION_FAILED"
	ErrCodePoolExhausted        = "CONSENSUS_POOL_EXHAUSTED"
	ErrCodeConnectionTimeout    = "CONSENSUS_CONNECTION_TIMEOUT"
)

// Sentinel errors for use with errors.Is.
var (
	// ErrNotLeader indicates the node is not the leader.
	ErrNotLeader = &errors.ForgeError{Code: ErrCodeNotLeader, Message: "node is not the leader"}

	// ErrNoLeader indicates there is no leader.
	ErrNoLeader = &errors.ForgeError{Code: ErrCodeNoLeader, Message: "no leader available"}

	// ErrNotStarted indicates the service is not started.
	ErrNotStarted = &errors.ForgeError{Code: ErrCodeNotStarted, Message: "consensus service not started"}

	// ErrAlreadyStarted indicates the service is already started.
	ErrAlreadyStarted = &errors.ForgeError{Code: ErrCodeAlreadyStarted, Message: "consensus service already started"}

	// ErrNodeNotFound indicates a node was not found.
	ErrNodeNotFound = &errors.ForgeError{Code: ErrCodeNodeNotFound, Message: "node not found"}

	// ErrClusterNotFound indicates a cluster was not found.
	ErrClusterNotFound = &errors.ForgeError{Code: ErrCodeClusterNotFound, Message: "cluster not found"}

	// ErrStorageUnavailable indicates storage is unavailable.
	ErrStorageUnavailable = &errors.ForgeError{Code: ErrCodeStorageUnavailable, Message: "storage unavailable"}

	// ErrTransportUnavailable indicates transport is unavailable.
	ErrTransportUnavailable = &errors.ForgeError{Code: ErrCodeTransportUnavailable, Message: "transport unavailable"}

	// ErrDiscoveryUnavailable indicates discovery service is unavailable.
	ErrDiscoveryUnavailable = &errors.ForgeError{Code: ErrCodeDiscoveryUnavailable, Message: "discovery service unavailable"}

	// ErrNoQuorum indicates no quorum is available.
	ErrNoQuorum = &errors.ForgeError{Code: ErrCodeNoQuorum, Message: "no quorum available"}

	// ErrInvalidTerm indicates an invalid term.
	ErrInvalidTerm = &errors.ForgeError{Code: ErrCodeInvalidTerm, Message: "invalid term"}

	// ErrStaleTerm indicates a stale term.
	ErrStaleTerm = &errors.ForgeError{Code: ErrCodeStaleTerm, Message: "stale term"}

	// ErrLogInconsistent indicates log inconsistency.
	ErrLogInconsistent = &errors.ForgeError{Code: ErrCodeLogInconsistent, Message: "log inconsistent"}

	// ErrSnapshotFailed indicates snapshot operation failed.
	ErrSnapshotFailed = &errors.ForgeError{Code: ErrCodeSnapshotFailed, Message: "snapshot operation failed"}

	// ErrCompactionFailed indicates compaction failed.
	ErrCompactionFailed = &errors.ForgeError{Code: ErrCodeCompactionFailed, Message: "compaction failed"}

	// ErrElectionTimeout indicates election timeout.
	ErrElectionTimeout = &errors.ForgeError{Code: ErrCodeElectionTimeout, Message: "election timeout"}

	// ErrInvalidPeer indicates an invalid peer.
	ErrInvalidPeer = &errors.ForgeError{Code: ErrCodeInvalidPeer, Message: "invalid peer"}

	// ErrPeerExists indicates peer already exists.
	ErrPeerExists = &errors.ForgeError{Code: ErrCodePeerExists, Message: "peer already exists"}

	// ErrPeerNotFound indicates peer not found.
	ErrPeerNotFound = &errors.ForgeError{Code: ErrCodePeerNotFound, Message: "peer not found"}

	// ErrInsufficientPeers indicates insufficient peers.
	ErrInsufficientPeers = &errors.ForgeError{Code: ErrCodeInsufficientPeers, Message: "insufficient peers"}

	// ErrNoAvailablePeers indicates no available peers for operation.
	ErrNoAvailablePeers = &errors.ForgeError{Code: ErrCodeNoAvailablePeers, Message: "no available peers"}

	// ErrFailoverFailed indicates failover operation failed.
	ErrFailoverFailed = &errors.ForgeError{Code: ErrCodeFailoverFailed, Message: "failover operation failed"}

	// ErrOperationFailed indicates a generic operation failure.
	ErrOperationFailed = &errors.ForgeError{Code: ErrCodeOperationFailed, Message: "operation failed"}

	// ErrReplicationFailed indicates replication operation failed.
	ErrReplicationFailed = &errors.ForgeError{Code: ErrCodeReplicationFailed, Message: "replication failed"}

	// ErrTimeout indicates operation timeout.
	ErrTimeout = &errors.ForgeError{Code: ErrCodeTimeout, Message: "operation timeout"}

	// ErrConnectionFailed indicates connection failure.
	ErrConnectionFailed = &errors.ForgeError{Code: ErrCodeConnectionFailed, Message: "connection failed"}

	// ErrStaleRead indicates a stale read attempt.
	ErrStaleRead = &errors.ForgeError{Code: ErrCodeStaleRead, Message: "stale read"}

	// ErrRateLimitExceeded indicates rate limit exceeded.
	ErrRateLimitExceeded = &errors.ForgeError{Code: ErrCodeRateLimitExceeded, Message: "rate limit exceeded"}

	// ErrAuthenticationFailed indicates authentication failure.
	ErrAuthenticationFailed = &errors.ForgeError{Code: ErrCodeAuthenticationFailed, Message: "authentication failed"}

	// ErrPoolExhausted indicates connection pool exhausted.
	ErrPoolExhausted = &errors.ForgeError{Code: ErrCodePoolExhausted, Message: "connection pool exhausted"}

	// ErrConnectionTimeout indicates connection timeout.
	ErrConnectionTimeout = &errors.ForgeError{Code: ErrCodeConnectionTimeout, Message: "connection timeout"}

	// ErrInvalidConfig indicates invalid configuration.
	ErrInvalidConfig = errors.ErrInvalidConfigSentinel
)

// NewNotLeaderError creates a not leader error with context.
func NewNotLeaderError(nodeID string, leaderID string) *errors.ForgeError {
	err := &errors.ForgeError{
		Code:    ErrCodeNotLeader,
		Message: "node is not the leader",
	}

	return err.WithContext("node_id", nodeID).WithContext("leader_id", leaderID)
}

// NewNoLeaderError creates a no leader error.
func NewNoLeaderError() *errors.ForgeError {
	return &errors.ForgeError{
		Code:    ErrCodeNoLeader,
		Message: "no leader available in the cluster",
	}
}

// NewTimeoutError creates a timeout error.
func NewTimeoutError(operation string) *errors.ForgeError {
	return errors.ErrTimeoutError(operation, 30*time.Second)
}

// NewNoQuorumError creates a no quorum error with context.
func NewNoQuorumError(required, available int) *errors.ForgeError {
	err := &errors.ForgeError{
		Code:    ErrCodeNoQuorum,
		Message: "no quorum available",
	}

	return err.WithContext("required", required).WithContext("available", available)
}

// NewStaleTermError creates a stale term error with context.
func NewStaleTermError(current, received uint64) *errors.ForgeError {
	err := &errors.ForgeError{
		Code:    ErrCodeStaleTerm,
		Message: "received stale term",
	}

	return err.WithContext("current_term", current).WithContext("received_term", received)
}

// IsNotLeaderError returns true if the error is a not leader error.
func IsNotLeaderError(err error) bool {
	return errors.Is(err, ErrNotLeader)
}

// IsNoLeaderError returns true if the error is a no leader error.
func IsNoLeaderError(err error) bool {
	return errors.Is(err, ErrNoLeader)
}

// IsNoQuorumError returns true if the error is a no quorum error.
func IsNoQuorumError(err error) bool {
	return errors.Is(err, ErrNoQuorum)
}

// IsStaleTermError returns true if the error is a stale term error.
func IsStaleTermError(err error) bool {
	return errors.Is(err, ErrStaleTerm)
}

// IsRetryable returns true if the error is retryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for retryable consensus errors
	return errors.Is(err, ErrNoLeader) ||
		errors.Is(err, ErrNoQuorum) ||
		errors.Is(err, ErrStorageUnavailable) ||
		errors.Is(err, ErrTransportUnavailable) ||
		errors.Is(err, ErrDiscoveryUnavailable) ||
		errors.IsTimeout(err)
}

// IsFatal returns true if the error is fatal and should cause shutdown.
func IsFatal(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, ErrInvalidConfig) ||
		errors.IsValidationError(err)
}

// Is is a helper function that wraps errors.Is from stdlib.
func Is(err, target error) bool {
	return stderrors.Is(err, target)
}
