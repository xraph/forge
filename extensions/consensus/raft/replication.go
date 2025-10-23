package raft

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// replicateToPeer replicates log entries to a single peer
func (n *Node) replicateToPeer(peer *PeerState) {
	// Use mutex to prevent concurrent replication to the same peer
	peer.ReplicationMu.Lock()
	defer peer.ReplicationMu.Unlock()

	n.mu.RLock()
	if !n.IsLeader() {
		n.mu.RUnlock()
		return
	}

	currentTerm := n.currentTerm
	commitIndex := n.commitIndex
	nextIndex := n.nextIndex[peer.ID]
	n.mu.RUnlock()

	// Get entries to send
	prevLogIndex := nextIndex - 1
	prevLogTerm := uint64(0)

	if prevLogIndex > 0 {
		entry, err := n.log.Get(prevLogIndex)
		if err != nil {
			// Previous entry might be in snapshot
			n.logger.Debug("previous log entry not found, may need snapshot",
				forge.F("peer", peer.ID),
				forge.F("prev_index", prevLogIndex),
			)
			// TODO: Send snapshot if needed
			return
		}
		prevLogTerm = entry.Term
	}

	// Get entries to replicate
	maxEntries := n.config.MaxAppendEntries
	entries, err := n.log.GetEntriesAfter(prevLogIndex, maxEntries)
	if err != nil {
		n.logger.Error("failed to get log entries",
			forge.F("peer", peer.ID),
			forge.F("error", err),
		)
		return
	}

	// Send AppendEntries RPC
	req := &internal.AppendEntriesRequest{
		Term:         currentTerm,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: commitIndex,
	}

	resp, err := n.sendAppendEntries(peer, req)
	if err != nil {
		n.logger.Warn("failed to send AppendEntries",
			forge.F("peer", peer.ID),
			forge.F("error", err),
		)
		return
	}

	// Handle response
	n.handleAppendEntriesResponse(peer, req, resp)
}

// sendAppendEntries sends an AppendEntries RPC to a peer
func (n *Node) sendAppendEntries(peer *PeerState, req *internal.AppendEntriesRequest) (*internal.AppendEntriesResponse, error) {
	ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()

	msg := internal.Message{
		Type:    internal.MessageTypeAppendEntries,
		From:    n.id,
		To:      peer.ID,
		Payload: req,
	}

	// Send via transport
	err := n.transport.Send(ctx, peer.ID, msg)
	if err != nil {
		return nil, err
	}

	// In a real implementation, we would wait for response via a response channel
	// For now, return a placeholder response
	// TODO: Implement proper RPC response handling
	peer.LastContact = time.Now()

	resp := &internal.AppendEntriesResponse{
		Term:    req.Term,
		Success: true,
		NodeID:  peer.ID,
	}

	return resp, nil
}

// handleAppendEntriesResponse handles the response from an AppendEntries RPC
func (n *Node) handleAppendEntriesResponse(peer *PeerState, req *internal.AppendEntriesRequest, resp *internal.AppendEntriesResponse) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Check if we're still leader in the same term
	if !n.IsLeader() || n.currentTerm != req.Term {
		return
	}

	// If term in response is higher, step down
	if resp.Term > n.currentTerm {
		n.stepDown(resp.Term)
		return
	}

	if resp.Success {
		// Update nextIndex and matchIndex
		if len(req.Entries) > 0 {
			lastEntry := req.Entries[len(req.Entries)-1]
			n.nextIndex[peer.ID] = lastEntry.Index + 1
			n.matchIndex[peer.ID] = lastEntry.Index

			n.logger.Debug("successfully replicated to peer",
				forge.F("peer", peer.ID),
				forge.F("match_index", lastEntry.Index),
			)

			// Try to advance commit index
			n.advanceCommitIndex()
		}
	} else {
		// Replication failed, decrement nextIndex and retry
		if n.nextIndex[peer.ID] > 1 {
			n.nextIndex[peer.ID]--
		}

		n.logger.Debug("replication failed, decrementing nextIndex",
			forge.F("peer", peer.ID),
			forge.F("next_index", n.nextIndex[peer.ID]),
		)

		// Immediately retry
		go n.replicateToPeer(peer)
	}
}

// AppendEntries handles an AppendEntries RPC from the leader
func (n *Node) AppendEntries(ctx context.Context, req *internal.AppendEntriesRequest) (*internal.AppendEntriesResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := &internal.AppendEntriesResponse{
		Term:    n.currentTerm,
		Success: false,
		NodeID:  n.id,
	}

	// Reply false if term < currentTerm
	if req.Term < n.currentTerm {
		return resp, nil
	}

	// Reset election timer since we heard from leader
	n.resetElectionTimer()

	// If RPC term is higher, update our term and step down
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.persistState()
	}

	// Step down to follower if we're not already
	if n.GetRole() != internal.RoleFollower {
		n.setRole(internal.RoleFollower)
	}

	// Update leader
	if n.GetLeader() != req.LeaderID {
		n.setLeader(req.LeaderID)
	}

	// Reply false if log doesn't contain an entry at prevLogIndex whose term matches prevLogTerm
	if req.PrevLogIndex > 0 {
		if !n.log.MatchesTerm(req.PrevLogIndex, req.PrevLogTerm) {
			n.logger.Debug("log doesn't match at prevLogIndex",
				forge.F("prev_index", req.PrevLogIndex),
				forge.F("prev_term", req.PrevLogTerm),
			)
			return resp, nil
		}
	}

	// If an existing entry conflicts with a new one (same index but different terms),
	// delete the existing entry and all that follow it
	for _, entry := range req.Entries {
		existingEntry, err := n.log.Get(entry.Index)
		if err == nil && existingEntry.Term != entry.Term {
			// Conflict detected, truncate log
			if err := n.log.TruncateAfter(entry.Index - 1); err != nil {
				n.logger.Error("failed to truncate log",
					forge.F("error", err),
				)
				return resp, nil
			}
			break
		}
	}

	// Append any new entries not already in the log
	for _, entry := range req.Entries {
		_, err := n.log.Get(entry.Index)
		if err != nil {
			// Entry doesn't exist, append it
			if err := n.log.Append(entry); err != nil {
				n.logger.Error("failed to append entry",
					forge.F("error", err),
				)
				return resp, nil
			}
		}
	}

	// Update commit index
	if req.LeaderCommit > n.commitIndex {
		lastNewIndex := n.log.LastIndex()
		if req.LeaderCommit < lastNewIndex {
			n.commitIndex = req.LeaderCommit
		} else {
			n.commitIndex = lastNewIndex
		}

		n.logger.Debug("updated commit index",
			forge.F("commit_index", n.commitIndex),
		)

		// Apply committed entries
		go n.applyCommitted()
	}

	resp.Success = true
	if len(req.Entries) > 0 {
		resp.MatchIndex = req.Entries[len(req.Entries)-1].Index
	}

	atomic.AddInt64(&n.appendCount, 1)

	return resp, nil
}

// advanceCommitIndex tries to advance the commit index based on matchIndex of peers
func (n *Node) advanceCommitIndex() {
	// Find the highest index that a majority of nodes have replicated
	lastLogIndex := n.log.LastIndex()

	for n.commitIndex < lastLogIndex {
		testIndex := n.commitIndex + 1

		// Count how many nodes have replicated this index
		replicated := 1 // Count self

		for _, matchIndex := range n.matchIndex {
			if matchIndex >= testIndex {
				replicated++
			}
		}

		// Check if majority has replicated
		majority := (len(n.peers)+1)/2 + 1
		if replicated >= majority {
			// Verify the entry is from current term (Raft safety requirement)
			entry, err := n.log.Get(testIndex)
			if err == nil && entry.Term == n.currentTerm {
				n.commitIndex = testIndex
				n.logger.Debug("advanced commit index",
					forge.F("commit_index", n.commitIndex),
					forge.F("replicated", replicated),
					forge.F("majority", majority),
				)

				// Apply committed entries
				go n.applyCommitted()
			} else {
				break
			}
		} else {
			break
		}
	}
}

// applyCommitted applies committed log entries to the state machine
func (n *Node) applyCommitted() {
	for {
		n.mu.Lock()
		if n.lastApplied >= n.commitIndex {
			n.mu.Unlock()
			return
		}

		// Apply next entry
		applyIndex := n.lastApplied + 1
		entry, err := n.log.Get(applyIndex)
		if err != nil {
			n.logger.Error("failed to get log entry for apply",
				forge.F("index", applyIndex),
				forge.F("error", err),
			)
			n.mu.Unlock()
			return
		}

		n.mu.Unlock()

		// Apply to state machine (outside lock)
		if err := n.stateMachine.Apply(entry); err != nil {
			n.logger.Error("failed to apply entry to state machine",
				forge.F("index", applyIndex),
				forge.F("error", err),
			)
			return
		}

		n.mu.Lock()
		n.lastApplied = applyIndex
		n.mu.Unlock()

		n.logger.Debug("applied entry",
			forge.F("index", applyIndex),
			forge.F("term", entry.Term),
			forge.F("type", entry.Type),
		)

		// Notify any waiting Apply() calls
		select {
		case msg := <-n.applyCh:
			if msg.Index == applyIndex {
				msg.ResultCh <- nil
			}
		default:
		}
	}
}

// InstallSnapshot handles an InstallSnapshot RPC
func (n *Node) InstallSnapshot(ctx context.Context, req *internal.InstallSnapshotRequest) (*internal.InstallSnapshotResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := &internal.InstallSnapshotResponse{
		Term:   n.currentTerm,
		NodeID: n.id,
	}

	// Reply immediately if term < currentTerm
	if req.Term < n.currentTerm {
		return resp, nil
	}

	// Reset election timer
	n.resetElectionTimer()

	// If term is higher, update our term
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.persistState()
	}

	// Update leader
	if n.GetLeader() != req.LeaderID {
		n.setLeader(req.LeaderID)
	}

	// TODO: Handle snapshot installation
	// For now, just acknowledge receipt

	n.logger.Info("received snapshot",
		forge.F("leader", req.LeaderID),
		forge.F("last_included_index", req.LastIncludedIndex),
		forge.F("last_included_term", req.LastIncludedTerm),
	)

	return resp, nil
}
