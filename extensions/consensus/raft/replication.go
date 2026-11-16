package raft

import (
	"context"
	"fmt"
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
		req, needSnapshot, ok := n.buildAppendEntries(peer)
		if !ok {
			return
		}

		if needSnapshot {
			if !n.sendSnapshot(peer) {
				return
			}

			continue
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
// consistent view of the leader state. needSnapshot is true when the peer
// needs entries that were compacted into the snapshot; ok is false when this
// node is not the leader or nothing can be sent.
func (n *Node) buildAppendEntries(peer *PeerState) (req *internal.AppendEntriesRequest, needSnapshot, ok bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.GetRole() != internal.RoleLeader {
		return nil, false, false
	}

	nextIndex := max(n.nextIndex[peer.ID], 1)
	prevLogIndex := nextIndex - 1

	if prevLogIndex < n.log.SnapshotIndex() {
		return nil, true, true
	}

	prevLogTerm, found := n.log.Term(prevLogIndex)
	if !found {
		n.logger.Error("no log entry to anchor replication on",
			forge.F("node_id", n.id),
			forge.F("peer", peer.ID),
			forge.F("prev_index", prevLogIndex),
		)

		return nil, false, false
	}

	entries, err := n.log.GetEntriesAfter(prevLogIndex, n.config.MaxAppendEntries)
	if err != nil {
		n.logger.Error("failed to get log entries",
			forge.F("peer", peer.ID),
			forge.F("error", err),
		)

		return nil, false, false
	}

	return &internal.AppendEntriesRequest{
		Term:         n.currentTerm,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: n.commitIndex,
	}, false, true
}

// sendSnapshot sends the latest snapshot to a peer whose next entry was
// compacted away, in chunks of at most SnapshotChunkSize bytes, and advances
// the peer's replication state once the peer has installed it. It reports
// whether the peer accepted the snapshot.
func (n *Node) sendSnapshot(peer *PeerState) bool {
	n.mu.RLock()
	term := n.currentTerm
	index := n.log.SnapshotIndex()
	snapshotTerm := n.log.SnapshotTerm()
	leader := n.GetRole() == internal.RoleLeader
	n.mu.RUnlock()

	if !leader {
		return false
	}

	data, err := n.storage.Get(fmt.Appendf(nil, "snapshot_%d", index))
	if err != nil {
		n.logger.Error("cannot read snapshot to send to peer",
			forge.F("peer", peer.ID),
			forge.F("index", index),
			forge.F("error", err),
		)

		return false
	}

	n.logger.Info("sending snapshot to peer",
		forge.F("peer", peer.ID),
		forge.F("index", index),
		forge.F("size", len(data)),
	)

	chunkSize := n.config.SnapshotChunkSize

	for offset := 0; ; {
		end := min(offset+chunkSize, len(data))
		req := &internal.InstallSnapshotRequest{
			Term:              term,
			LeaderID:          n.id,
			LastIncludedIndex: index,
			LastIncludedTerm:  snapshotTerm,
			Offset:            uint64(offset),
			Data:              data[offset:end],
			Done:              end == len(data),
		}

		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		response, err := n.call(ctx, peer.ID, internal.MessageTypeInstallSnapshot, req)

		cancel()

		if err != nil {
			n.logger.Debug("failed to send snapshot chunk",
				forge.F("peer", peer.ID),
				forge.F("offset", offset),
				forge.F("error", err),
			)

			return false
		}

		resp, ok := response.(*internal.InstallSnapshotResponse)
		if !ok {
			return false
		}

		if resp.Term > term {
			n.stepDown(resp.Term)

			return false
		}

		if !resp.Success {
			n.logger.Warn("peer rejected snapshot chunk",
				forge.F("peer", peer.ID),
				forge.F("index", index),
				forge.F("offset", offset),
			)

			return false
		}

		if req.Done {
			break
		}

		offset = end
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.GetRole() != internal.RoleLeader || n.currentTerm != term {
		return false
	}

	if index > n.matchIndex[peer.ID] {
		n.matchIndex[peer.ID] = index
	}

	if n.nextIndex[peer.ID] < index+1 {
		n.nextIndex[peer.ID] = index + 1
	}

	n.advanceCommitIndex()

	return true
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
	for n.applyNext() {
	}
}

// applyNext applies the next committed entry, if any, and reports whether it
// did. It holds applyMu so snapshots never interleave with an apply.
func (n *Node) applyNext() bool {
	n.applyMu.Lock()
	defer n.applyMu.Unlock()

	n.mu.RLock()

	if n.lastApplied >= n.commitIndex {
		n.mu.RUnlock()

		return false
	}

	applyIndex := n.lastApplied + 1
	entry, err := n.log.Get(applyIndex)
	n.mu.RUnlock()

	if err != nil {
		n.logger.Error("failed to get committed log entry for apply",
			forge.F("index", applyIndex),
			forge.F("error", err),
		)

		return false
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

	return true
}

// InstallSnapshot handles an InstallSnapshot RPC.
func (n *Node) InstallSnapshot(ctx context.Context, req *internal.InstallSnapshotRequest) (*internal.InstallSnapshotResponse, error) {
	resp := n.handleInstallSnapshot(*req)

	return &resp, nil
}
