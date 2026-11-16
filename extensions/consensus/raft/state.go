package raft

import (
	"context"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// runElectionTimer manages the election timeout and triggers elections.
func (n *Node) runElectionTimer() {
	defer n.wg.Done()

	n.mu.Lock()
	n.electionTimeout = n.randomElectionTimeout()
	n.electionTimer = time.NewTimer(n.electionTimeout)
	n.mu.Unlock()

	defer func() {
		n.mu.Lock()

		if n.electionTimer != nil {
			n.electionTimer.Stop()
		}

		n.mu.Unlock()
	}()

	for {
		select {
		case <-n.ctx.Done():
			return

		case <-n.electionTimer.C:
			// Election timeout - start election if not leader
			if !n.IsLeader() {
				n.startElection()
			}

			// Reset timer
			n.mu.Lock()
			n.electionTimeout = n.randomElectionTimeout()
			n.electionTimer.Reset(n.electionTimeout)
			n.mu.Unlock()
		}
	}
}

// resetElectionTimer resets the election timer.
func (n *Node) resetElectionTimer() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.resetElectionTimerUnsafe()
}

// resetElectionTimerUnsafe resets the election timer without acquiring the lock
// This should only be called when the caller already holds the lock.
func (n *Node) resetElectionTimerUnsafe() {
	if n.electionTimer != nil {
		n.electionTimeout = n.randomElectionTimeout()
		n.electionTimer.Reset(n.electionTimeout)
	}
}

// startElection starts a new election.
func (n *Node) startElection() {
	n.mu.Lock()

	// Transition to candidate
	n.setRole(internal.RoleCandidate)
	n.currentTerm++
	n.votedFor = n.id
	currentTerm := n.currentTerm

	// Persist state
	if err := n.persistState(); err != nil {
		n.logger.Error("failed to persist state during election",
			forge.F("node_id", n.id),
			forge.F("error", err),
		)
		n.mu.Unlock()

		return
	}

	lastLogIndex := n.log.LastIndex()
	lastLogTerm := n.log.LastTerm()
	electionTimeout := n.electionTimeout // Read timeout while holding lock

	n.mu.Unlock()

	n.electionCount.Add(1)

	n.logger.Info("starting election",
		forge.F("node_id", n.id),
		forge.F("term", currentTerm),
		forge.F("last_log_index", lastLogIndex),
		forge.F("last_log_term", lastLogTerm),
	)

	// Request votes from all peers
	n.peersLock.RLock()

	peers := make([]*PeerState, 0, len(n.peers))
	for _, peer := range n.peers {
		peers = append(peers, peer)
	}

	n.peersLock.RUnlock()

	peerCount := len(peers)
	votes := 1 // Vote for self
	votesNeeded := (peerCount+1)/2 + 1

	// If we have no peers, we are the majority.
	if peerCount == 0 {
		n.becomeLeader(currentTerm)

		return
	}

	// Buffered so vote goroutines never block once the election is decided.
	voteCh := make(chan bool, peerCount)

	for _, peer := range peers {
		go func(p *PeerState) {
			voteCh <- n.requestVote(p, currentTerm, lastLogIndex, lastLogTerm)
		}(peer)
	}

	// Collect votes with timeout
	electionTimeoutTimer := time.NewTimer(electionTimeout)
	defer electionTimeoutTimer.Stop()

	for range peerCount {
		select {
		case <-n.ctx.Done():
			return

		case <-electionTimeoutTimer.C:
			n.logger.Warn("election timeout",
				forge.F("node_id", n.id),
				forge.F("term", currentTerm),
				forge.F("votes", votes),
				forge.F("needed", votesNeeded),
			)

			return

		case granted := <-voteCh:
			if !granted {
				continue
			}

			votes++
			n.logger.Info("vote granted",
				forge.F("node_id", n.id),
				forge.F("votes", votes),
				forge.F("needed", votesNeeded),
			)

			if votes >= votesNeeded {
				// Do not wait for the remaining peers: an unreachable peer
				// would hold leadership back until its RPC timed out.
				n.becomeLeader(currentTerm)

				return
			}
		}
	}

	n.logger.Info("election lost",
		forge.F("node_id", n.id),
		forge.F("term", currentTerm),
		forge.F("votes", votes),
		forge.F("needed", votesNeeded),
	)
}

// requestVote sends a RequestVote RPC to a peer and reports whether the peer
// granted its vote for this term.
func (n *Node) requestVote(peer *PeerState, term, lastLogIndex, lastLogTerm uint64) bool {
	req := &internal.RequestVoteRequest{
		Term:         term,
		CandidateID:  n.id,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
		PreVote:      n.config.PreVoteEnabled,
	}

	ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()

	response, err := n.call(ctx, peer.ID, internal.MessageTypeRequestVote, req)
	if err != nil {
		n.logger.Debug("RequestVote failed",
			forge.F("peer", peer.ID),
			forge.F("term", term),
			forge.F("error", err),
		)

		return false
	}

	resp, ok := response.(*internal.RequestVoteResponse)
	if !ok {
		return false
	}

	if resp.Term > term {
		n.stepDown(resp.Term)

		return false
	}

	// Only a grant for this very term counts.
	return resp.VoteGranted && resp.Term == term
}

// RequestVote handles a RequestVote RPC.
func (n *Node) RequestVote(ctx context.Context, req *internal.RequestVoteRequest) (*internal.RequestVoteResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := &internal.RequestVoteResponse{
		Term:        n.currentTerm,
		VoteGranted: false,
		NodeID:      n.id,
	}

	// Reply false if term < currentTerm
	if req.Term < n.currentTerm {
		n.logger.Debug("rejecting vote - stale term",
			forge.F("candidate", req.CandidateID),
			forge.F("req_term", req.Term),
			forge.F("current_term", n.currentTerm),
		)

		return resp, nil
	}

	// If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.setRole(internal.RoleFollower)
		resp.Term = n.currentTerm

		if err := n.persistState(); err != nil {
			n.logger.Error("failed to persist term from RequestVote, denying vote",
				forge.F("node_id", n.id),
				forge.F("term", n.currentTerm),
				forge.F("error", err),
			)

			return resp, nil
		}
	}

	// Vote granted if:
	// 1. We haven't voted yet in this term, or we already voted for this candidate
	// 2. Candidate's log is at least as up-to-date as ours
	logUpToDate := n.isLogUpToDate(req.LastLogIndex, req.LastLogTerm)

	if (n.votedFor == "" || n.votedFor == req.CandidateID) && logUpToDate {
		previousVote := n.votedFor
		n.votedFor = req.CandidateID

		// The vote must be durable before it is granted, or a restart could
		// let this node vote for a second candidate in the same term.
		if err := n.persistState(); err != nil {
			n.votedFor = previousVote
			n.logger.Error("failed to persist vote, denying it",
				forge.F("node_id", n.id),
				forge.F("candidate", req.CandidateID),
				forge.F("term", n.currentTerm),
				forge.F("error", err),
			)

			return resp, nil
		}

		n.resetElectionTimerUnsafe()

		resp.VoteGranted = true
		resp.Term = n.currentTerm

		n.logger.Info("granted vote",
			forge.F("candidate", req.CandidateID),
			forge.F("term", n.currentTerm),
		)
	} else {
		n.logger.Debug("denied vote",
			forge.F("candidate", req.CandidateID),
			forge.F("term", n.currentTerm),
			forge.F("voted_for", n.votedFor),
			forge.F("log_up_to_date", logUpToDate),
		)
	}

	return resp, nil
}

// isLogUpToDate checks if the candidate's log is at least as up-to-date as ours.
func (n *Node) isLogUpToDate(candidateIndex, candidateTerm uint64) bool {
	lastLogIndex := n.log.LastIndex()
	lastLogTerm := n.log.LastTerm()

	// If the logs have last entries with different terms,
	// then the log with the later term is more up-to-date
	if candidateTerm != lastLogTerm {
		return candidateTerm > lastLogTerm
	}

	// If the logs end with the same term,
	// then whichever log is longer is more up-to-date
	return candidateIndex >= lastLogIndex
}

// becomeLeader transitions to leader state.
func (n *Node) becomeLeader(term uint64) {
	n.peersLock.RLock()

	peerIDs := make([]string, 0, len(n.peers))
	for peerID := range n.peers {
		peerIDs = append(peerIDs, peerID)
	}

	n.peersLock.RUnlock()

	n.mu.Lock()

	// The election may have been overtaken by a newer term or a valid leader.
	if n.currentTerm != term || n.GetRole() != internal.RoleCandidate {
		n.mu.Unlock()

		return
	}

	n.setRole(internal.RoleLeader)
	n.setLeader(n.id)

	lastLogIndex := n.log.LastIndex()

	n.nextIndex = make(map[string]uint64, len(peerIDs))
	n.matchIndex = make(map[string]uint64, len(peerIDs))

	for _, peerID := range peerIDs {
		n.nextIndex[peerID] = lastLogIndex + 1
		n.matchIndex[peerID] = 0
	}

	if n.heartbeatTicker != nil {
		n.heartbeatTicker.Stop()
	}

	n.heartbeatTicker = time.NewTicker(n.config.HeartbeatInterval)

	// A no-op from the new term lets entries from earlier terms commit (§5.4.2).
	noop := internal.LogEntry{
		Index:   lastLogIndex + 1,
		Term:    term,
		Type:    internal.EntryNoop,
		Data:    []byte{},
		Created: time.Now(),
	}

	if err := n.log.Append(noop); err != nil {
		n.logger.Error("failed to append no-op entry, stepping down",
			forge.F("node_id", n.id),
			forge.F("term", term),
			forge.F("error", err),
		)
		n.stepDownLocked(term)
		n.mu.Unlock()

		return
	}

	n.advanceCommitIndex()
	n.mu.Unlock()

	n.logger.Info("became leader",
		forge.F("node_id", n.id),
		forge.F("term", term),
		forge.F("last_log_index", lastLogIndex),
	)

	go n.sendHeartbeats()
}

// stepDown transitions to follower state.
func (n *Node) stepDown(term uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.stepDownLocked(term)
}

// stepDownLocked transitions to follower state (assumes n.mu is already held).
func (n *Node) stepDownLocked(term uint64) {
	if term > n.currentTerm {
		n.currentTerm = term
		n.votedFor = ""
		n.persistStateOrLog("stepped down to newer term")
	}

	if n.GetRole() != internal.RoleFollower {
		n.setRole(internal.RoleFollower)
		n.setLeader("")

		n.logger.Info("stepped down to follower",
			forge.F("node_id", n.id),
			forge.F("term", n.currentTerm),
		)
	}

	// Stop heartbeat ticker if we were leader
	if n.heartbeatTicker != nil {
		n.heartbeatTicker.Stop()
		n.heartbeatTicker = nil
	}

	n.resetElectionTimerUnsafe()
}

// runHeartbeat sends periodic heartbeats when leader.
func (n *Node) runHeartbeat() {
	defer n.wg.Done()

	for {
		select {
		case <-n.ctx.Done():
			return

		case <-time.After(n.config.HeartbeatInterval):
			if n.IsLeader() {
				n.sendHeartbeats()
			}
		}
	}
}

// sendHeartbeats sends heartbeat messages to all peers.
func (n *Node) sendHeartbeats() {
	n.lastHeartbeatMux.Lock()
	n.lastHeartbeat = time.Now()
	n.lastHeartbeatMux.Unlock()

	n.peersLock.RLock()

	peers := make([]*PeerState, 0, len(n.peers))
	for _, peer := range n.peers {
		peers = append(peers, peer)
	}

	n.peersLock.RUnlock()

	n.logger.Debug("sending heartbeats",
		forge.F("node_id", n.id),
		forge.F("peer_count", len(peers)),
	)

	for _, peer := range peers {
		go n.replicateToPeer(peer)
	}
}

// triggerReplication triggers log replication to all peers.
func (n *Node) triggerReplication() {
	if !n.IsLeader() {
		return
	}

	n.sendHeartbeats()
}
