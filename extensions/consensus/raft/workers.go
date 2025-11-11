package raft

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// runMessageProcessor processes incoming messages from the transport layer.
func (n *Node) runMessageProcessor() {
	defer n.wg.Done()

	// Get the message channel from transport
	msgCh := n.transport.Receive()

	for {
		select {
		case <-n.ctx.Done():
			return

		case msg := <-msgCh:
			n.handleMessage(msg)
		}
	}
}

// handleMessage handles incoming messages.
func (n *Node) handleMessage(msg internal.Message) {
	n.logger.Debug("received message",
		forge.F("from", msg.From),
		forge.F("to", msg.To),
		forge.F("type", msg.Type),
	)

	switch msg.Type {
	case internal.MessageTypeAppendEntries:
		if req, ok := msg.Payload.(*internal.AppendEntriesRequest); ok {
			resp := n.handleAppendEntries(*req)
			// Send response back to sender
			responseMsg := internal.Message{
				Type:    internal.MessageTypeAppendEntriesResponse,
				From:    n.id,
				To:      msg.From,
				Payload: &resp,
			}
			if err := n.transport.Send(n.ctx, msg.From, responseMsg); err != nil {
				n.logger.Error("failed to send AppendEntries response",
					forge.F("error", err),
					forge.F("to", msg.From),
				)
			}
		}

	case internal.MessageTypeAppendEntriesResponse:
		if resp, ok := msg.Payload.(*internal.AppendEntriesResponse); ok {
			// Send response to pending request
			requestID := fmt.Sprintf("append_entries_%s_%s", msg.To, msg.From)
			n.sendResponse(requestID, resp)

			// Also handle the response for replication logic
			n.peersLock.RLock()
			peer, exists := n.peers[msg.From]
			n.peersLock.RUnlock()

			if exists {
				// Create a dummy request for the response handler
				req := &internal.AppendEntriesRequest{
					Term: resp.Term,
				}
				n.handleAppendEntriesResponse(peer, req, resp)
			}
		}

	case internal.MessageTypeRequestVote:
		if req, ok := msg.Payload.(*internal.RequestVoteRequest); ok {
			resp := n.handleRequestVote(*req)
			// Send response back to sender
			responseMsg := internal.Message{
				Type:    internal.MessageTypeRequestVoteResponse,
				From:    n.id,
				To:      msg.From,
				Payload: &resp,
			}
			if err := n.transport.Send(n.ctx, msg.From, responseMsg); err != nil {
				n.logger.Error("failed to send RequestVote response",
					forge.F("error", err),
					forge.F("to", msg.From),
				)
			}
		}

	case internal.MessageTypeRequestVoteResponse:
		if resp, ok := msg.Payload.(*internal.RequestVoteResponse); ok {
			// Send response to pending request
			requestID := fmt.Sprintf("request_vote_%s_%s", msg.To, msg.From)
			n.sendResponse(requestID, resp)

			n.logger.Debug("received vote response",
				forge.F("from", msg.From),
				forge.F("granted", resp.VoteGranted),
				forge.F("term", resp.Term),
			)
		}

	case internal.MessageTypeInstallSnapshot:
		if req, ok := msg.Payload.(*internal.InstallSnapshotRequest); ok {
			resp := n.handleInstallSnapshot(*req)
			// Send response back to sender
			responseMsg := internal.Message{
				Type:    internal.MessageTypeInstallSnapshotResponse,
				From:    n.id,
				To:      msg.From,
				Payload: &resp,
			}
			if err := n.transport.Send(n.ctx, msg.From, responseMsg); err != nil {
				n.logger.Error("failed to send InstallSnapshot response",
					forge.F("error", err),
					forge.F("to", msg.From),
				)
			}
		}

	default:
		n.logger.Warn("unknown message type",
			forge.F("type", msg.Type),
			forge.F("from", msg.From),
		)
	}
}

// runStateMachine processes apply messages and applies them to the state machine.
func (n *Node) runStateMachine() {
	defer n.wg.Done()

	for {
		select {
		case <-n.ctx.Done():
			return

		case msg := <-n.applyCh:
			// Wait for the entry to be committed
			n.mu.RLock()
			committed := n.commitIndex >= msg.Index
			n.mu.RUnlock()

			if !committed {
				// Re-queue the message
				select {
				case n.applyCh <- msg:
				case <-time.After(1 * time.Second):
					msg.ResultCh <- errors.New("timeout waiting for commit")
				}

				continue
			}

			// Apply to state machine
			err := n.stateMachine.Apply(msg.Entry)
			if err != nil {
				n.logger.Error("failed to apply entry to state machine",
					forge.F("index", msg.Index),
					forge.F("error", err),
				)

				msg.ResultCh <- err

				continue
			}

			// Update lastApplied
			n.mu.Lock()

			if msg.Index > n.lastApplied {
				n.lastApplied = msg.Index
			}

			n.mu.Unlock()

			n.logger.Debug("applied entry to state machine",
				forge.F("index", msg.Index),
				forge.F("term", msg.Term),
			)

			// Signal completion
			msg.ResultCh <- nil
		}
	}
}

// runSnapshotManager manages snapshot creation.
func (n *Node) runSnapshotManager() {
	defer n.wg.Done()

	// If snapshots are disabled, just return
	if !n.config.EnableSnapshots {
		return
	}

	// Create a ticker for periodic snapshots
	ticker := time.NewTicker(n.config.SnapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return

		case <-ticker.C:
			// Check if we should create a snapshot
			n.mu.RLock()
			logSize := n.log.Count()
			lastApplied := n.lastApplied
			snapshotThreshold := int(n.config.SnapshotThreshold)
			n.mu.RUnlock()

			if logSize > snapshotThreshold {
				n.logger.Info("triggering snapshot",
					forge.F("log_size", logSize),
					forge.F("threshold", snapshotThreshold),
					forge.F("last_applied", lastApplied),
				)

				if err := n.createSnapshot(lastApplied); err != nil {
					n.logger.Error("failed to create snapshot",
						forge.F("error", err),
					)
				}
			}

		case msg := <-n.snapshotCh:
			// Manual snapshot request
			if err := n.createSnapshot(msg.Index); err != nil {
				n.logger.Error("failed to create snapshot",
					forge.F("error", err),
				)

				msg.ResultCh <- err
			} else {
				msg.ResultCh <- nil
			}
		}
	}
}

// createSnapshot creates a snapshot up to the given index.
func (n *Node) createSnapshot(index uint64) error {
	n.mu.RLock()

	if index > n.lastApplied {
		n.mu.RUnlock()

		return errors.New("cannot snapshot beyond last applied index")
	}

	// Get the entry at the snapshot index to get its term
	entry, err := n.log.Get(index)
	if err != nil {
		n.mu.RUnlock()

		return fmt.Errorf("failed to get log entry at index %d: %w", index, err)
	}

	term := entry.Term

	n.mu.RUnlock()

	n.logger.Info("creating snapshot",
		forge.F("index", index),
		forge.F("term", term),
	)

	// Create snapshot from state machine
	snapshot, err := n.stateMachine.Snapshot()
	if err != nil {
		return fmt.Errorf("failed to create snapshot from state machine: %w", err)
	}

	snapshot.Index = index
	snapshot.Term = term
	snapshot.Created = time.Now()

	// Save snapshot to storage
	if err := n.saveSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Compact the log
	n.mu.Lock()

	if err := n.log.Compact(index, term); err != nil {
		n.mu.Unlock()

		return fmt.Errorf("failed to compact log: %w", err)
	}

	n.mu.Unlock()

	atomic.AddInt64(&n.snapshotCount, 1)

	n.logger.Info("snapshot created successfully",
		forge.F("index", index),
		forge.F("term", term),
		forge.F("size", snapshot.Size),
	)

	return nil
}

// saveSnapshot saves a snapshot to storage.
func (n *Node) saveSnapshot(snapshot *internal.Snapshot) error {
	// Save snapshot data
	snapshotKey := fmt.Appendf(nil, "snapshot_%d", snapshot.Index)
	if err := n.storage.Set(snapshotKey, snapshot.Data); err != nil {
		return fmt.Errorf("failed to save snapshot data: %w", err)
	}

	// Save snapshot metadata
	metaKey := []byte("snapshot_meta")

	metaValue := fmt.Sprintf(`{"index":%d,"term":%d}`, snapshot.Index, snapshot.Term)
	if err := n.storage.Set(metaKey, []byte(metaValue)); err != nil {
		return fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

	return nil
}

// runConfigChangeProcessor processes configuration changes.
func (n *Node) runConfigChangeProcessor() {
	defer n.wg.Done()

	for {
		select {
		case <-n.ctx.Done():
			return

		case msg := <-n.configChangeCh:
			if err := n.processConfigChange(msg); err != nil {
				n.logger.Error("failed to process config change",
					forge.F("type", msg.Type),
					forge.F("node_id", msg.NodeID),
					forge.F("error", err),
				)

				msg.ResultCh <- err
			} else {
				msg.ResultCh <- nil
			}
		}
	}
}

// processConfigChange processes a configuration change.
func (n *Node) processConfigChange(msg ConfigChangeMsg) error {
	// Configuration changes must go through Raft consensus
	var configEntry internal.LogEntry

	configEntry.Type = internal.EntryConfig

	switch msg.Type {
	case ConfigChangeAdd:
		// Add node to configuration
		n.peersLock.Lock()
		n.peers[msg.NodeID] = &PeerState{
			ID:         msg.NodeID,
			Address:    msg.Address,
			Port:       msg.Port,
			NextIndex:  n.log.LastIndex() + 1,
			MatchIndex: 0,
		}
		n.peersLock.Unlock()

		n.logger.Info("added peer to cluster",
			forge.F("peer_id", msg.NodeID),
			forge.F("address", msg.Address),
			forge.F("port", msg.Port),
		)

	case ConfigChangeRemove:
		// Remove node from configuration
		n.peersLock.Lock()
		delete(n.peers, msg.NodeID)
		n.peersLock.Unlock()

		n.logger.Info("removed peer from cluster",
			forge.F("peer_id", msg.NodeID),
		)

	default:
		return fmt.Errorf("unknown config change type: %d", msg.Type)
	}

	return nil
}

// AddNode adds a node to the cluster (must be called on leader).
func (n *Node) AddNode(ctx context.Context, nodeID, address string, port int) error {
	if !n.IsLeader() {
		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	resultCh := make(chan error, 1)
	msg := ConfigChangeMsg{
		Type:     ConfigChangeAdd,
		NodeID:   nodeID,
		Address:  address,
		Port:     port,
		ResultCh: resultCh,
	}

	select {
	case n.configChangeCh <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-resultCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RemoveNode removes a node from the cluster (must be called on leader).
func (n *Node) RemoveNode(ctx context.Context, nodeID string) error {
	if !n.IsLeader() {
		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	resultCh := make(chan error, 1)
	msg := ConfigChangeMsg{
		Type:     ConfigChangeRemove,
		NodeID:   nodeID,
		ResultCh: resultCh,
	}

	select {
	case n.configChangeCh <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-resultCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TransferLeadership transfers leadership to another node.
func (n *Node) TransferLeadership(ctx context.Context, targetNodeID string) error {
	if !n.IsLeader() {
		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	n.logger.Info("transferring leadership",
		forge.F("target", targetNodeID),
	)

	// Step down and let the target node win the next election
	// In a production implementation, we would:
	// 1. Ensure target is caught up
	// 2. Stop sending heartbeats
	// 3. Send a TimeoutNow message to target
	// For now, just step down
	n.stepDown(n.GetTerm())

	return nil
}

// StepDown causes the leader to step down.
func (n *Node) StepDown(ctx context.Context) error {
	if !n.IsLeader() {
		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	n.logger.Info("stepping down as leader")
	n.stepDown(n.GetTerm())

	return nil
}
