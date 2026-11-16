package raft

import (
	"context"
	"encoding/json"
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

	case internal.MessageTypeTimeoutNow:
		req, ok := payloadAs[internal.TimeoutNowRequest](msg.Payload)
		if !ok {
			n.dropMalformed(msg)

			return
		}

		resp := n.handleTimeoutNow(msg.From, *req)
		n.reply(msg, internal.MessageTypeTimeoutNowResponse, &resp)

	case internal.MessageTypeTimeoutNowResponse:
		if resp, ok := payloadAs[internal.TimeoutNowResponse](msg.Payload); ok {
			n.deliverResponse(msg.From, msg.RequestID, resp)
		} else {
			n.dropMalformed(msg)
		}

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
	cfg := n.configAtLocked(index).Clone()
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

	if err := n.saveSnapshot(snapshot, cfg); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	n.mu.Lock()

	err = n.log.Compact(index, term)
	if err == nil {
		n.compactConfigsLocked(index)
	}
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

// saveSnapshot saves a snapshot and its metadata, including the cluster
// configuration as of the snapshot index, to storage.
func (n *Node) saveSnapshot(snapshot *internal.Snapshot, cfg internal.ClusterConfiguration) error {
	snapshotKey := fmt.Appendf(nil, "snapshot_%d", snapshot.Index)
	if err := n.storage.Set(snapshotKey, snapshot.Data); err != nil {
		return fmt.Errorf("failed to save snapshot data: %w", err)
	}

	meta, err := json.Marshal(snapshotMeta{Index: snapshot.Index, Term: snapshot.Term, Config: &cfg})
	if err != nil {
		return fmt.Errorf("failed to encode snapshot metadata: %w", err)
	}

	if err := n.storage.Set([]byte(snapshotMetaKey), meta); err != nil {
		return fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

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
