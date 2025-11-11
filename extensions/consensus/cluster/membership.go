package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// MembershipManager manages cluster membership changes.
type MembershipManager struct {
	manager *Manager
	logger  forge.Logger

	// Membership change state
	pendingChanges []MembershipChange
	changesMu      sync.RWMutex

	// Configuration
	changeTimeout time.Duration
}

// MembershipChange represents a pending membership change.
type MembershipChange struct {
	ID        string
	Type      MembershipChangeType
	NodeInfo  internal.NodeInfo
	Initiated time.Time
	Status    MembershipChangeStatus
}

// MembershipChangeType represents the type of membership change.
type MembershipChangeType int

const (
	// MembershipChangeAdd adds a node to the cluster.
	MembershipChangeAdd MembershipChangeType = iota
	// MembershipChangeRemove removes a node from the cluster.
	MembershipChangeRemove
	// MembershipChangeUpdate updates node information.
	MembershipChangeUpdate
)

// MembershipChangeStatus represents the status of a membership change.
type MembershipChangeStatus int

const (
	// MembershipChangeStatusPending is waiting to be applied.
	MembershipChangeStatusPending MembershipChangeStatus = iota
	// MembershipChangeStatusInProgress is currently being applied.
	MembershipChangeStatusInProgress
	// MembershipChangeStatusCommitted has been committed.
	MembershipChangeStatusCommitted
	// MembershipChangeStatusFailed has failed.
	MembershipChangeStatusFailed
)

// NewMembershipManager creates a new membership manager.
func NewMembershipManager(manager *Manager, logger forge.Logger) *MembershipManager {
	return &MembershipManager{
		manager:        manager,
		logger:         logger,
		pendingChanges: make([]MembershipChange, 0),
		changeTimeout:  30 * time.Second,
	}
}

// AddNode proposes adding a node to the cluster.
func (mm *MembershipManager) AddNode(ctx context.Context, nodeInfo internal.NodeInfo) error {
	mm.logger.Info("proposing to add node",
		forge.F("node_id", nodeInfo.ID),
		forge.F("address", nodeInfo.Address),
		forge.F("port", nodeInfo.Port),
	)

	// Check if node already exists
	_, err := mm.manager.GetNode(nodeInfo.ID)
	if err == nil {
		return internal.ErrPeerExists
	}

	// Create membership change
	change := MembershipChange{
		ID:        fmt.Sprintf("add-%s-%d", nodeInfo.ID, time.Now().UnixNano()),
		Type:      MembershipChangeAdd,
		NodeInfo:  nodeInfo,
		Initiated: time.Now(),
		Status:    MembershipChangeStatusPending,
	}

	// Add to pending changes
	mm.changesMu.Lock()
	mm.pendingChanges = append(mm.pendingChanges, change)
	mm.changesMu.Unlock()

	// Apply change (in a real implementation, this would be through Raft log)
	return mm.applyChange(ctx, change)
}

// RemoveNode proposes removing a node from the cluster.
func (mm *MembershipManager) RemoveNode(ctx context.Context, nodeID string) error {
	mm.logger.Info("proposing to remove node",
		forge.F("node_id", nodeID),
	)

	// Check if node exists
	node, err := mm.manager.GetNode(nodeID)
	if err != nil {
		return internal.ErrNodeNotFound
	}

	// Create membership change
	change := MembershipChange{
		ID:        fmt.Sprintf("remove-%s-%d", nodeID, time.Now().UnixNano()),
		Type:      MembershipChangeRemove,
		NodeInfo:  *node,
		Initiated: time.Now(),
		Status:    MembershipChangeStatusPending,
	}

	// Add to pending changes
	mm.changesMu.Lock()
	mm.pendingChanges = append(mm.pendingChanges, change)
	mm.changesMu.Unlock()

	// Apply change
	return mm.applyChange(ctx, change)
}

// UpdateNode proposes updating node information.
func (mm *MembershipManager) UpdateNode(ctx context.Context, nodeInfo internal.NodeInfo) error {
	mm.logger.Info("proposing to update node",
		forge.F("node_id", nodeInfo.ID),
	)

	// Check if node exists
	_, err := mm.manager.GetNode(nodeInfo.ID)
	if err != nil {
		return internal.ErrNodeNotFound
	}

	// Create membership change
	change := MembershipChange{
		ID:        fmt.Sprintf("update-%s-%d", nodeInfo.ID, time.Now().UnixNano()),
		Type:      MembershipChangeUpdate,
		NodeInfo:  nodeInfo,
		Initiated: time.Now(),
		Status:    MembershipChangeStatusPending,
	}

	// Add to pending changes
	mm.changesMu.Lock()
	mm.pendingChanges = append(mm.pendingChanges, change)
	mm.changesMu.Unlock()

	// Apply change
	return mm.applyChange(ctx, change)
}

// applyChange applies a membership change.
func (mm *MembershipManager) applyChange(ctx context.Context, change MembershipChange) error {
	mm.logger.Info("applying membership change",
		forge.F("change_id", change.ID),
		forge.F("type", change.Type),
	)

	// Update status
	mm.updateChangeStatus(change.ID, MembershipChangeStatusInProgress)

	// Apply based on type
	var err error

	switch change.Type {
	case MembershipChangeAdd:
		err = mm.manager.AddNode(change.NodeInfo.ID, change.NodeInfo.Address, change.NodeInfo.Port)

	case MembershipChangeRemove:
		err = mm.manager.RemoveNode(change.NodeInfo.ID)

	case MembershipChangeUpdate:
		err = mm.manager.UpdateNode(change.NodeInfo.ID, change.NodeInfo)

	default:
		err = fmt.Errorf("unknown membership change type: %d", change.Type)
	}

	// Update status based on result
	if err != nil {
		mm.updateChangeStatus(change.ID, MembershipChangeStatusFailed)
		mm.logger.Error("membership change failed",
			forge.F("change_id", change.ID),
			forge.F("error", err),
		)

		return err
	}

	mm.updateChangeStatus(change.ID, MembershipChangeStatusCommitted)
	mm.logger.Info("membership change committed",
		forge.F("change_id", change.ID),
	)

	return nil
}

// updateChangeStatus updates the status of a change.
func (mm *MembershipManager) updateChangeStatus(changeID string, status MembershipChangeStatus) {
	mm.changesMu.Lock()
	defer mm.changesMu.Unlock()

	for i := range mm.pendingChanges {
		if mm.pendingChanges[i].ID == changeID {
			mm.pendingChanges[i].Status = status

			break
		}
	}
}

// GetPendingChanges returns all pending membership changes.
func (mm *MembershipManager) GetPendingChanges() []MembershipChange {
	mm.changesMu.RLock()
	defer mm.changesMu.RUnlock()

	changes := make([]MembershipChange, len(mm.pendingChanges))
	copy(changes, mm.pendingChanges)

	return changes
}

// CleanupOldChanges removes old completed/failed changes.
func (mm *MembershipManager) CleanupOldChanges(maxAge time.Duration) {
	mm.changesMu.Lock()
	defer mm.changesMu.Unlock()

	cutoff := time.Now().Add(-maxAge)

	var remaining []MembershipChange

	for _, change := range mm.pendingChanges {
		// Keep pending and in-progress changes
		if change.Status == MembershipChangeStatusPending ||
			change.Status == MembershipChangeStatusInProgress {
			remaining = append(remaining, change)

			continue
		}

		// Keep recent changes
		if change.Initiated.After(cutoff) {
			remaining = append(remaining, change)
		}
	}

	removed := len(mm.pendingChanges) - len(remaining)
	mm.pendingChanges = remaining

	if removed > 0 {
		mm.logger.Info("cleaned up old membership changes",
			forge.F("removed", removed),
		)
	}
}

// ValidateClusterSize validates that cluster size is within bounds.
func (mm *MembershipManager) ValidateClusterSize(proposedSize int) error {
	if proposedSize < 1 {
		return errors.New("cluster must have at least 1 node")
	}

	if proposedSize > 100 {
		return errors.New("cluster size cannot exceed 100 nodes")
	}

	// Warn if even number (can't form majority)
	if proposedSize%2 == 0 {
		mm.logger.Warn("cluster size is even, consider odd number for better fault tolerance",
			forge.F("size", proposedSize),
		)
	}

	return nil
}

// CanSafelyRemoveNode checks if a node can be safely removed.
func (mm *MembershipManager) CanSafelyRemoveNode(nodeID string) (bool, string) {
	nodes := mm.manager.GetNodes()

	// Can't remove if it would leave cluster too small
	if len(nodes) <= 1 {
		return false, "cannot remove last node in cluster"
	}

	// Check if removing would break quorum
	healthyNodes := 0

	for _, node := range nodes {
		if node.Status == internal.StatusActive {
			healthyNodes++
		}
	}

	if nodeID != "" {
		// Assume the node being removed is currently healthy
		node, err := mm.manager.GetNode(nodeID)
		if err == nil && node.Status == internal.StatusActive {
			healthyNodes--
		}
	}

	newClusterSize := len(nodes) - 1
	quorumSize := (newClusterSize / 2) + 1

	if healthyNodes < quorumSize {
		return false, fmt.Sprintf("removing node would break quorum (healthy: %d, quorum: %d)",
			healthyNodes, quorumSize)
	}

	return true, ""
}

// String returns string representation of change type.
func (t MembershipChangeType) String() string {
	switch t {
	case MembershipChangeAdd:
		return "add"
	case MembershipChangeRemove:
		return "remove"
	case MembershipChangeUpdate:
		return "update"
	default:
		return "unknown"
	}
}

// String returns string representation of change status.
func (s MembershipChangeStatus) String() string {
	switch s {
	case MembershipChangeStatusPending:
		return "pending"
	case MembershipChangeStatusInProgress:
		return "in_progress"
	case MembershipChangeStatusCommitted:
		return "committed"
	case MembershipChangeStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}
