package raft

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/xraph/forge"
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
//
// Requests are answered with a response that echoes the request ID. Responses
// are only handed to the request that is waiting for that ID; they are not
// processed a second time here, so replication state is updated exactly once
// per response by the goroutine that sent the request.
func (n *Node) handleMessage(msg internal.Message) {
	n.logger.Debug("received message",
		forge.F("from", msg.From),
		forge.F("to", msg.To),
		forge.F("type", msg.Type),
	)

	switch msg.Type {
	case internal.MessageTypeAppendEntries:
		req, ok := payloadAs[internal.AppendEntriesRequest](msg.Payload)
		if !ok {
			n.dropMalformed(msg)

			return
		}

		resp := n.handleAppendEntries(*req)
		n.reply(msg, internal.MessageTypeAppendEntriesResponse, &resp)

	case internal.MessageTypeAppendEntriesResponse:
		if resp, ok := payloadAs[internal.AppendEntriesResponse](msg.Payload); ok {
			n.deliverResponse(msg.From, msg.RequestID, resp)
		} else {
			n.dropMalformed(msg)
		}

	case internal.MessageTypeRequestVote:
		req, ok := payloadAs[internal.RequestVoteRequest](msg.Payload)
		if !ok {
			n.dropMalformed(msg)

			return
		}

		resp := n.handleRequestVote(*req)
		n.reply(msg, internal.MessageTypeRequestVoteResponse, &resp)

	case internal.MessageTypeRequestVoteResponse:
		if resp, ok := payloadAs[internal.RequestVoteResponse](msg.Payload); ok {
			n.deliverResponse(msg.From, msg.RequestID, resp)
		} else {
			n.dropMalformed(msg)
		}

	case internal.MessageTypeInstallSnapshot:
		req, ok := payloadAs[internal.InstallSnapshotRequest](msg.Payload)
		if !ok {
			n.dropMalformed(msg)

			return
		}

		resp := n.handleInstallSnapshot(*req)
		n.reply(msg, internal.MessageTypeInstallSnapshotResponse, &resp)

	case internal.MessageTypeInstallSnapshotResponse:
		if resp, ok := payloadAs[internal.InstallSnapshotResponse](msg.Payload); ok {
			n.deliverResponse(msg.From, msg.RequestID, resp)
		} else {
			n.dropMalformed(msg)
		}

	default:
		n.logger.Warn("unknown message type",
			forge.F("type", msg.Type),
			forge.F("from", msg.From),
		)
	}
}

// reply sends a response to the sender of req, echoing its request ID.
func (n *Node) reply(req internal.Message, msgType internal.MessageType, payload any) {
	responseMsg := internal.Message{
		Type:      msgType,
		From:      n.id,
		To:        req.From,
		Payload:   payload,
		Timestamp: time.Now().UnixNano(),
		RequestID: req.RequestID,
	}

	if err := n.transport.Send(n.ctx, req.From, responseMsg); err != nil {
		n.logger.Error("failed to send response",
			forge.F("type", msgType),
			forge.F("to", req.From),
			forge.F("error", err),
		)
	}
}

func (n *Node) dropMalformed(msg internal.Message) {
	n.logger.Warn("dropping message with unexpected payload",
		forge.F("type", msg.Type),
		forge.F("from", msg.From),
		forge.F("payload_type", fmt.Sprintf("%T", msg.Payload)),
	)
}

// payloadAs accepts a payload as either T or *T. Network transports always
// decode to *T; in-process callers sometimes pass values.
func payloadAs[T any](payload any) (*T, bool) {
	switch p := payload.(type) {
	case *T:
		return p, p != nil
	case T:
		return &p, true
	default:
		return nil, false
	}
}

// runStateMachine is the single applier goroutine. It applies committed
// entries whenever commitIndex moves.
func (n *Node) runStateMachine() {
	defer n.wg.Done()

	for {
		select {
		case <-n.ctx.Done():
			return

		case <-n.applyNotify:
			n.applyCommitted()
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
			snapshotThreshold := snapshotThresholdAsInt(n.config.SnapshotThreshold)
			n.mu.RUnlock()

			if logSize > snapshotThreshold {
				n.logger.Info("triggering snapshot",
					forge.F("log_size", logSize),
					forge.F("threshold", snapshotThreshold),
					forge.F("last_applied", lastApplied),
				)

				if err := n.createSnapshot(); err != nil {
					n.logger.Error("failed to create snapshot",
						forge.F("error", err),
					)
				}
			}

		case msg := <-n.snapshotCh:
			// Manual snapshot request
			if err := n.createSnapshot(); err != nil {
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

// createSnapshot snapshots the state machine at lastApplied and compacts the
// log up to it. It holds applyMu so the state machine cannot move while the
// snapshot is taken, which keeps the snapshot's index exact.
func (n *Node) createSnapshot() error {
	n.applyMu.Lock()
	defer n.applyMu.Unlock()

	n.mu.RLock()
	index := n.lastApplied
	snapshotIndex := n.log.SnapshotIndex()
	term, ok := n.log.Term(index)
	n.mu.RUnlock()

	if index <= snapshotIndex {
		return nil // Nothing new to snapshot
	}

	if !ok {
		return fmt.Errorf("log entry %d is not available to snapshot", index)
	}

	n.logger.Info("creating snapshot",
		forge.F("index", index),
		forge.F("term", term),
	)

	snapshot, err := n.stateMachine.Snapshot()
	if err != nil {
		return fmt.Errorf("failed to create snapshot from state machine: %w", err)
	}

	snapshot.Index = index
	snapshot.Term = term
	snapshot.Created = time.Now()

	if err := n.saveSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	n.mu.Lock()
	err = n.log.Compact(index, term)
	n.mu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to compact log: %w", err)
	}

	n.snapshotCount.Add(1)

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

// snapshotThresholdAsInt clamps the configured threshold to the int range so a
// huge value can't wrap negative and trigger a snapshot on every tick.
func snapshotThresholdAsInt(threshold uint64) int {
	if threshold > math.MaxInt {
		return math.MaxInt
	}

	return int(threshold)
}
