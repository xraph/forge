package raft

import (
	"context"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// maxReplicationRounds bounds how many AppendEntries round trips one
// replicateToPeer call makes before leaving the rest to the next heartbeat.
const maxReplicationRounds = 64

// replicateToPeer replicates log entries to a single peer. It keeps sending
// while the peer is behind or rejected the last batch, so a lagging follower
// catches up without waiting a heartbeat interval per batch.
func (n *Node) replicateToPeer(peer *PeerState) {
	// Try to acquire lock, skip if already replicating
	if !peer.ReplicationMu.TryLock() {
		return
	}
	defer peer.ReplicationMu.Unlock()

	for range maxReplicationRounds {
		req, ok := n.buildAppendEntries(peer)
		if !ok {
			return
		}

		resp, err := n.sendAppendEntries(peer, req)
		if err != nil {
			n.logger.Debug("failed to send AppendEntries",
				forge.F("peer", peer.ID),
				forge.F("error", err),
			)

			return
		}

		// No locks held here: the handler may step down, which takes n.mu.
		if !n.handleAppendEntriesResponse(peer, req, resp) {
			return
		}
	}
}

// buildAppendEntries builds the next AppendEntries request for a peer from a
// consistent view of the leader state. It returns false when this node is not
// the leader or the peer needs entries that were compacted away.
func (n *Node) buildAppendEntries(peer *PeerState) (*internal.AppendEntriesRequest, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.GetRole() != internal.RoleLeader {
		return nil, false
	}

	nextIndex := max(n.nextIndex[peer.ID], 1)
	prevLogIndex := nextIndex - 1

	prevLogTerm, ok := n.log.Term(prevLogIndex)
	if !ok {
		n.logger.Error("peer needs log entries that are only in a snapshot; InstallSnapshot is not implemented, so it cannot catch up",
			forge.F("node_id", n.id),
			forge.F("peer", peer.ID),
			forge.F("prev_index", prevLogIndex),
			forge.F("snapshot_index", n.log.SnapshotIndex()),
		)

		return nil, false
	}

	entries, err := n.log.GetEntriesAfter(prevLogIndex, n.config.MaxAppendEntries)
	if err != nil {
		n.logger.Error("failed to get log entries",
			forge.F("peer", peer.ID),
			forge.F("error", err),
		)

		return nil, false
	}

	return &internal.AppendEntriesRequest{
		Term:         n.currentTerm,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: n.commitIndex,
	}, true
}

// sendAppendEntries sends an AppendEntries RPC to a peer and waits for the
// response to this exact request.
func (n *Node) sendAppendEntries(peer *PeerState, req *internal.AppendEntriesRequest) (*internal.AppendEntriesResponse, error) {
	ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()

	response, err := n.call(ctx, peer.ID, internal.MessageTypeAppendEntries, req)
	if err != nil {
		return nil, err
	}

	resp, ok := response.(*internal.AppendEntriesResponse)
	if !ok {
		return nil, errors.New("invalid response type")
	}

	n.peersLock.Lock()
	peer.LastContact = time.Now()
	n.peersLock.Unlock()

	return resp, nil
}

// handleAppendEntriesResponse applies a peer's answer to the request it was
// sent for. It reports whether the peer should be sent another batch now.
//
// matchIndex only moves forward and is bounded by what the request actually
// carried, and nextIndex only backs off when the rejected request is the one
// nextIndex currently describes, so reordered or duplicated responses cannot
// overstate replication or back off twice.
func (n *Node) handleAppendEntriesResponse(peer *PeerState, req *internal.AppendEntriesRequest, resp *internal.AppendEntriesResponse) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	// If term in response is higher, step down
	if resp.Term > n.currentTerm {
		n.stepDownLocked(resp.Term)

		return false
	}

	// Check if we're still leader in the same term
	if n.GetRole() != internal.RoleLeader || n.currentTerm != req.Term {
		return false
	}

	if resp.Success {
		sent := req.PrevLogIndex + uint64(len(req.Entries))

		match := sent
		if resp.MatchIndex != 0 && resp.MatchIndex < sent {
			match = resp.MatchIndex
		}

		if match > n.matchIndex[peer.ID] {
			n.matchIndex[peer.ID] = match

			n.logger.Debug("successfully replicated to peer",
				forge.F("peer", peer.ID),
				forge.F("match_index", match),
			)
		}

		if n.nextIndex[peer.ID] < n.matchIndex[peer.ID]+1 {
			n.nextIndex[peer.ID] = n.matchIndex[peer.ID] + 1
		}

		n.advanceCommitIndex()

		return n.nextIndex[peer.ID] <= n.log.LastIndex()
	}

	// Stale rejection for a request nextIndex has already moved past.
	if n.nextIndex[peer.ID] != req.PrevLogIndex+1 {
		return false
	}

	next := max(min(req.PrevLogIndex, resp.LastLogIndex+1), n.matchIndex[peer.ID]+1, 1)
	n.nextIndex[peer.ID] = next

	n.logger.Debug("replication rejected, backing off nextIndex",
		forge.F("peer", peer.ID),
		forge.F("next_index", next),
	)

	// Retry at once only if backing off changed what will be sent.
	return next < req.PrevLogIndex+1
}

// AppendEntries handles an AppendEntries RPC from the leader.
func (n *Node) AppendEntries(ctx context.Context, req *internal.AppendEntriesRequest) (*internal.AppendEntriesResponse, error) {
	resp := n.handleAppendEntries(*req)

	return &resp, nil
}

// advanceCommitIndex moves commitIndex to the highest index stored on a
// majority whose entry is from the current term. The caller holds n.mu.
func (n *Node) advanceCommitIndex() {
	if n.GetRole() != internal.RoleLeader {
		return
	}

	n.peersLock.RLock()
	clusterSize := len(n.peers) + 1
	n.peersLock.RUnlock()

	majority := clusterSize/2 + 1
	lastLogIndex := n.log.LastIndex()
	newCommit := n.commitIndex

	for index := lastLogIndex; index > n.commitIndex; index-- {
		// Only entries from the current term are committed by counting
		// replicas (§5.4.2); earlier ones commit with them.
		term, ok := n.log.Term(index)
		if !ok || term != n.currentTerm {
			break
		}

		replicated := 1 // Count self

		for _, matchIndex := range n.matchIndex {
			if matchIndex >= index {
				replicated++
			}
		}

		if replicated >= majority {
			newCommit = index

			break
		}
	}

	if newCommit > n.commitIndex {
		n.commitIndex = newCommit
		n.logger.Debug("advanced commit index",
			forge.F("commit_index", n.commitIndex),
		)
		n.signalApply()
	}
}

// signalApply wakes the applier. The caller holds n.mu.
func (n *Node) signalApply() {
	select {
	case n.applyNotify <- struct{}{}:
	default:
	}
}

// applyCommitted applies committed entries to the state machine in order. It
// only runs on the applier goroutine, so each entry is applied exactly once.
//
// A state machine error for an entry is returned to the Apply call waiting for
// it and does not stop the applier: the entry is committed on every node, and
// stalling here would wedge the node for good.
func (n *Node) applyCommitted() {
	for {
		n.mu.RLock()

		if n.lastApplied >= n.commitIndex {
			n.mu.RUnlock()

			return
		}

		applyIndex := n.lastApplied + 1
		entry, err := n.log.Get(applyIndex)
		n.mu.RUnlock()

		if err != nil {
			n.logger.Error("failed to get committed log entry for apply",
				forge.F("index", applyIndex),
				forge.F("error", err),
			)

			return
		}

		applyErr := n.stateMachine.Apply(entry)
		if applyErr != nil {
			n.logger.Error("state machine rejected committed entry",
				forge.F("index", applyIndex),
				forge.F("error", applyErr),
			)
		}

		n.mu.Lock()
		n.lastApplied = applyIndex
		waiter, waiting := n.applyWaiters[applyIndex]
		delete(n.applyWaiters, applyIndex)
		n.mu.Unlock()

		if waiting {
			if waiter.term == entry.Term {
				waiter.ch <- applyErr
			} else {
				waiter.ch <- errors.New("entry was replaced after a leadership change and was not applied")
			}
		}
	}
}

// InstallSnapshot handles an InstallSnapshot RPC.
func (n *Node) InstallSnapshot(ctx context.Context, req *internal.InstallSnapshotRequest) (*internal.InstallSnapshotResponse, error) {
	resp := n.handleInstallSnapshot(*req)

	return &resp, nil
}
