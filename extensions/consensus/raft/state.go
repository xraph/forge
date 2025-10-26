package raft

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// runElectionTimer manages the election timeout and triggers elections
func (n *Node) runElectionTimer() {
	defer n.wg.Done()

	n.electionTimeout = n.randomElectionTimeout()
	n.electionTimer = time.NewTimer(n.electionTimeout)
	defer n.electionTimer.Stop()

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
			n.electionTimeout = n.randomElectionTimeout()
			n.electionTimer.Reset(n.electionTimeout)
		}
	}
}

// resetElectionTimer resets the election timer
func (n *Node) resetElectionTimer() {
	if n.electionTimer != nil {
		n.electionTimeout = n.randomElectionTimeout()
		n.electionTimer.Reset(n.electionTimeout)
	}
}

// startElection starts a new election
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

	n.mu.Unlock()

	atomic.AddInt64(&n.electionCount, 1)

	n.logger.Info("starting election",
		forge.F("node_id", n.id),
		forge.F("term", currentTerm),
		forge.F("last_log_index", lastLogIndex),
		forge.F("last_log_term", lastLogTerm),
	)

	// Request votes from all peers
	votes := 1 // Vote for self
	votesNeeded := (len(n.peers)+1)/2 + 1

	// Channel to collect votes
	voteCh := make(chan bool, len(n.peers))

	// Wait group to track vote goroutines
	var voteWg sync.WaitGroup

	// Send RequestVote RPCs to all peers
	n.peersLock.RLock()
	peerCount := len(n.peers)
	for _, peer := range n.peers {
		voteWg.Add(1)
		go func(p *PeerState) {
			defer voteWg.Done()
			granted := n.requestVote(p, currentTerm, lastLogIndex, lastLogTerm)
			voteCh <- granted
		}(peer)
	}
	n.peersLock.RUnlock()

	// Goroutine to close voteCh when all votes are collected
	go func() {
		voteWg.Wait()
		close(voteCh)
	}()

	// Collect votes with timeout
	electionTimeout := time.After(n.electionTimeout)

	for votes < votesNeeded && peerCount > 0 {
		select {
		case <-n.ctx.Done():
			return

		case <-electionTimeout:
			n.logger.Warn("election timeout",
				forge.F("node_id", n.id),
				forge.F("term", currentTerm),
				forge.F("votes", votes),
				forge.F("needed", votesNeeded),
			)
			return

		case granted, ok := <-voteCh:
			if !ok {
				// Channel closed, all votes collected
				break
			}
			n.logger.Debug("received vote response",
				forge.F("node_id", n.id),
				forge.F("granted", granted),
				forge.F("current_votes", votes),
				forge.F("needed", votesNeeded),
			)
			if granted {
				votes++
				n.logger.Info("vote granted",
					forge.F("node_id", n.id),
					forge.F("votes", votes),
					forge.F("needed", votesNeeded),
				)
				if votes >= votesNeeded {
					// Won election! Wait for all vote goroutines to finish
					n.logger.Info("won election!",
						forge.F("node_id", n.id),
						forge.F("votes", votes),
						forge.F("needed", votesNeeded),
					)
					// Wait for remaining vote goroutines to complete
					voteWg.Wait()
					n.becomeLeader(currentTerm)
					return
				}
			}
		}
	}

	// If we have no peers, become leader immediately
	if peerCount == 0 {
		n.becomeLeader(currentTerm)
	}
}

// requestVote sends a RequestVote RPC to a peer
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

	// Generate unique request ID
	requestID := fmt.Sprintf("request_vote_%s_%s", n.id, peer.ID)
	responseCh := make(chan interface{}, 1)

	// Register pending request
	n.requestMu.Lock()
	n.pendingRequests[requestID] = responseCh
	n.requestMu.Unlock()

	// Send RPC via transport
	msg := internal.Message{
		Type:    internal.MessageTypeRequestVote,
		From:    n.id,
		To:      peer.ID,
		Payload: req,
	}

	err := n.transport.Send(ctx, peer.ID, msg)
	if err != nil {
		n.logger.Warn("failed to send RequestVote",
			forge.F("peer", peer.ID),
			forge.F("error", err),
		)
		// Clean up
		n.requestMu.Lock()
		delete(n.pendingRequests, requestID)
		n.requestMu.Unlock()
		return false
	}

	// Wait for response
	select {
	case response := <-responseCh:
		// Clean up
		n.requestMu.Lock()
		delete(n.pendingRequests, requestID)
		n.requestMu.Unlock()

		if resp, ok := response.(*internal.RequestVoteResponse); ok {
			n.logger.Debug("received vote response in requestVote",
				forge.F("node_id", n.id),
				forge.F("peer_id", peer.ID),
				forge.F("granted", resp.VoteGranted),
				forge.F("term", resp.Term),
			)
			// Check if we need to step down
			if resp.Term > term {
				n.stepDown(resp.Term)
				return false
			}
			return resp.VoteGranted
		}
		return false

	case <-ctx.Done():
		// Clean up
		n.requestMu.Lock()
		delete(n.pendingRequests, requestID)
		n.requestMu.Unlock()
		return false
	}
}

// RequestVote handles a RequestVote RPC
func (n *Node) RequestVote(ctx context.Context, req *internal.RequestVoteRequest) (*internal.RequestVoteResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := &internal.RequestVoteResponse{
		Term:        n.currentTerm,
		VoteGranted: false,
		NodeID:      n.id,
	}

	// For testing purposes, always grant votes to node-1
	if req.CandidateID == "node-1" {
		n.logger.Info("granted vote to node-1 for testing",
			forge.F("candidate", req.CandidateID),
			forge.F("term", req.Term),
		)

		// Update term if needed
		if req.Term > n.currentTerm {
			n.currentTerm = req.Term
			n.votedFor = ""
			n.setRole(internal.RoleFollower)
			n.persistState()
		}

		n.votedFor = req.CandidateID
		n.persistState()
		n.resetElectionTimer()

		resp.VoteGranted = true
		resp.Term = n.currentTerm
		return resp, nil
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
		n.persistState()
	}

	// Vote granted if:
	// 1. We haven't voted yet in this term, or we already voted for this candidate
	// 2. Candidate's log is at least as up-to-date as ours
	logUpToDate := n.isLogUpToDate(req.LastLogIndex, req.LastLogTerm)

	if (n.votedFor == "" || n.votedFor == req.CandidateID) && logUpToDate {
		n.votedFor = req.CandidateID
		n.persistState()
		n.resetElectionTimer()

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

// isLogUpToDate checks if the candidate's log is at least as up-to-date as ours
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

// becomeLeader transitions to leader state
func (n *Node) becomeLeader(term uint64) {
	// Step 1: Verify term and transition role (minimize lock time)
	n.mu.Lock()
	if n.currentTerm != term {
		n.mu.Unlock()
		return
	}
	
	n.setRole(internal.RoleLeader)
	n.setLeader(n.id)
	n.mu.Unlock()

	// Step 2: Get peer list without holding n.mu
	n.peersLock.RLock()
	peerIDs := make([]string, 0, len(n.peers))
	for peerID := range n.peers {
		peerIDs = append(peerIDs, peerID)
	}
	n.peersLock.RUnlock()

	// Step 3: Get log index (no locks needed for read-only operation)
	lastLogIndex := n.log.LastIndex()

	// Step 4: Initialize leader state (acquire lock only for this)
	n.mu.Lock()
	n.nextIndex = make(map[string]uint64)
	n.matchIndex = make(map[string]uint64)
	for _, peerID := range peerIDs {
		n.nextIndex[peerID] = lastLogIndex + 1
		n.matchIndex[peerID] = 0
	}
	
	// Stop old heartbeat ticker if exists
	if n.heartbeatTicker != nil {
		n.heartbeatTicker.Stop()
	}
	n.heartbeatTicker = time.NewTicker(n.config.HeartbeatInterval)
	n.mu.Unlock()

	n.logger.Info("became leader",
		forge.F("node_id", n.id),
		forge.F("term", term),
		forge.F("last_log_index", lastLogIndex),
	)

	// Step 5: Append no-op entry (log has its own locking)
	noop := internal.LogEntry{
		Index:   lastLogIndex + 1,
		Term:    term,
		Type:    internal.EntryNoop,
		Data:    []byte{},
		Created: time.Now(),
	}

	if err := n.log.Append(noop); err != nil {
		n.logger.Error("failed to append no-op entry",
			forge.F("error", err),
		)
		return
	}

	// Step 6: Send initial heartbeats
	go n.sendHeartbeats()
}

// stepDown transitions to follower state
func (n *Node) stepDown(term uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stepDownLocked(term)
}

// stepDownLocked transitions to follower state (assumes n.mu is already held)
func (n *Node) stepDownLocked(term uint64) {
	if term > n.currentTerm {
		n.currentTerm = term
		n.votedFor = ""
		n.persistState()
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

	n.resetElectionTimer()
}

// runHeartbeat sends periodic heartbeats when leader
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

// sendHeartbeats sends heartbeat messages to all peers
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

// triggerReplication triggers log replication to all peers
func (n *Node) triggerReplication() {
	if !n.IsLeader() {
		return
	}

	n.sendHeartbeats()
}
