package raft

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// RPCHandler handles Raft RPC requests.
type RPCHandler struct {
	node   *Node
	logger forge.Logger
}

// NewRPCHandler creates a new RPC handler.
func NewRPCHandler(node *Node, logger forge.Logger) *RPCHandler {
	return &RPCHandler{
		node:   node,
		logger: logger,
	}
}

// HandleAppendEntries handles AppendEntries RPC.
func (rh *RPCHandler) HandleAppendEntries(ctx context.Context, req internal.AppendEntriesRequest) internal.AppendEntriesResponse {
	startTime := time.Now()

	rh.logger.Debug("received AppendEntries RPC",
		forge.F("leader", req.LeaderID),
		forge.F("term", req.Term),
		forge.F("prev_log_index", req.PrevLogIndex),
		forge.F("prev_log_term", req.PrevLogTerm),
		forge.F("entries_count", len(req.Entries)),
		forge.F("leader_commit", req.LeaderCommit),
	)

	// Validate request
	if err := rh.validateAppendEntriesRequest(req); err != nil {
		rh.logger.Error("invalid AppendEntries request",
			forge.F("error", err),
		)

		return internal.AppendEntriesResponse{
			Term:    rh.node.GetCurrentTerm(),
			Success: false,
		}
	}

	// Handle in node
	response := rh.node.handleAppendEntries(req)

	duration := time.Since(startTime)
	rh.logger.Debug("AppendEntries RPC handled",
		forge.F("success", response.Success),
		forge.F("term", response.Term),
		forge.F("duration_ms", duration.Milliseconds()),
	)

	return response
}

// HandleRequestVote handles RequestVote RPC.
func (rh *RPCHandler) HandleRequestVote(ctx context.Context, req internal.RequestVoteRequest) internal.RequestVoteResponse {
	startTime := time.Now()

	rh.logger.Debug("received RequestVote RPC",
		forge.F("candidate", req.CandidateID),
		forge.F("term", req.Term),
		forge.F("last_log_index", req.LastLogIndex),
		forge.F("last_log_term", req.LastLogTerm),
	)

	// Validate request
	if err := rh.validateRequestVoteRequest(req); err != nil {
		rh.logger.Error("invalid RequestVote request",
			forge.F("error", err),
		)

		return internal.RequestVoteResponse{
			Term:        rh.node.GetCurrentTerm(),
			VoteGranted: false,
		}
	}

	// Handle in node
	response := rh.node.handleRequestVote(req)

	duration := time.Since(startTime)
	rh.logger.Debug("RequestVote RPC handled",
		forge.F("granted", response.VoteGranted),
		forge.F("term", response.Term),
		forge.F("duration_ms", duration.Milliseconds()),
	)

	return response
}

// HandleInstallSnapshot handles InstallSnapshot RPC.
func (rh *RPCHandler) HandleInstallSnapshot(ctx context.Context, req internal.InstallSnapshotRequest) internal.InstallSnapshotResponse {
	startTime := time.Now()

	rh.logger.Info("received InstallSnapshot RPC",
		forge.F("leader", req.LeaderID),
		forge.F("term", req.Term),
		forge.F("last_index", req.LastIncludedIndex),
		forge.F("last_term", req.LastIncludedTerm),
		forge.F("offset", req.Offset),
		forge.F("data_size", len(req.Data)),
		forge.F("done", req.Done),
	)

	// Validate request
	if err := rh.validateInstallSnapshotRequest(req); err != nil {
		rh.logger.Error("invalid InstallSnapshot request",
			forge.F("error", err),
		)

		return internal.InstallSnapshotResponse{
			Term: rh.node.GetCurrentTerm(),
		}
	}

	// Handle in node
	response := rh.node.handleInstallSnapshot(req)

	duration := time.Since(startTime)
	rh.logger.Info("InstallSnapshot RPC handled",
		forge.F("term", response.Term),
		forge.F("duration_ms", duration.Milliseconds()),
	)

	return response
}

// validateAppendEntriesRequest validates AppendEntries request.
func (rh *RPCHandler) validateAppendEntriesRequest(req internal.AppendEntriesRequest) error {
	if req.LeaderID == "" {
		return errors.New("leader ID is required")
	}

	if req.Term == 0 {
		return errors.New("term must be greater than 0")
	}

	// Additional validation can be added here

	return nil
}

// validateRequestVoteRequest validates RequestVote request.
func (rh *RPCHandler) validateRequestVoteRequest(req internal.RequestVoteRequest) error {
	if req.CandidateID == "" {
		return errors.New("candidate ID is required")
	}

	if req.Term == 0 {
		return errors.New("term must be greater than 0")
	}

	return nil
}

// validateInstallSnapshotRequest validates InstallSnapshot request.
func (rh *RPCHandler) validateInstallSnapshotRequest(req internal.InstallSnapshotRequest) error {
	if req.LeaderID == "" {
		return errors.New("leader ID is required")
	}

	if req.Term == 0 {
		return errors.New("term must be greater than 0")
	}

	if len(req.Data) == 0 && req.Done {
		return errors.New("snapshot data is required")
	}

	return nil
}

// SendAppendEntries sends AppendEntries RPC to a peer.
func (rh *RPCHandler) SendAppendEntries(ctx context.Context, target string, req internal.AppendEntriesRequest) (internal.AppendEntriesResponse, error) {
	rh.logger.Debug("sending AppendEntries RPC",
		forge.F("target", target),
		forge.F("term", req.Term),
		forge.F("entries_count", len(req.Entries)),
	)

	// Create message
	msg := internal.Message{
		Type:      internal.MessageTypeAppendEntries,
		From:      rh.node.id,
		To:        target,
		Timestamp: time.Now().UnixNano(),
		Payload:   req,
	}

	// Send via transport
	if err := rh.node.transport.Send(ctx, target, msg); err != nil {
		return internal.AppendEntriesResponse{}, fmt.Errorf("failed to send message: %w", err)
	}

	// TODO: Wait for response with timeout
	// For now, return empty response
	return internal.AppendEntriesResponse{}, nil
}

// SendRequestVote sends RequestVote RPC to a peer.
func (rh *RPCHandler) SendRequestVote(ctx context.Context, target string, req internal.RequestVoteRequest) (internal.RequestVoteResponse, error) {
	rh.logger.Debug("sending RequestVote RPC",
		forge.F("target", target),
		forge.F("term", req.Term),
		forge.F("candidate", req.CandidateID),
	)

	// Create message
	msg := internal.Message{
		Type:      internal.MessageTypeRequestVote,
		From:      rh.node.id,
		To:        target,
		Timestamp: time.Now().UnixNano(),
		Payload:   req,
	}

	// Send via transport
	if err := rh.node.transport.Send(ctx, target, msg); err != nil {
		return internal.RequestVoteResponse{}, fmt.Errorf("failed to send message: %w", err)
	}

	// TODO: Wait for response with timeout
	// For now, return empty response
	return internal.RequestVoteResponse{}, nil
}

// SendInstallSnapshot sends InstallSnapshot RPC to a peer.
func (rh *RPCHandler) SendInstallSnapshot(ctx context.Context, target string, req internal.InstallSnapshotRequest) (internal.InstallSnapshotResponse, error) {
	rh.logger.Info("sending InstallSnapshot RPC",
		forge.F("target", target),
		forge.F("term", req.Term),
		forge.F("last_index", req.LastIncludedIndex),
		forge.F("data_size", len(req.Data)),
	)

	// Create message
	msg := internal.Message{
		Type:      internal.MessageTypeInstallSnapshot,
		From:      rh.node.id,
		To:        target,
		Timestamp: time.Now().UnixNano(),
		Payload:   req,
	}

	// Send via transport
	if err := rh.node.transport.Send(ctx, target, msg); err != nil {
		return internal.InstallSnapshotResponse{}, fmt.Errorf("failed to send message: %w", err)
	}

	// TODO: Wait for response with timeout
	// For now, return empty response
	return internal.InstallSnapshotResponse{}, nil
}

// BroadcastAppendEntries broadcasts AppendEntries to all peers.
func (rh *RPCHandler) BroadcastAppendEntries(ctx context.Context, peers []string, req internal.AppendEntriesRequest) map[string]error {
	results := make(map[string]error)

	for _, peer := range peers {
		if peer == rh.node.id {
			continue
		}

		_, err := rh.SendAppendEntries(ctx, peer, req)
		results[peer] = err
	}

	return results
}

// BroadcastRequestVote broadcasts RequestVote to all peers.
func (rh *RPCHandler) BroadcastRequestVote(ctx context.Context, peers []string, req internal.RequestVoteRequest) map[string]error {
	results := make(map[string]error)

	for _, peer := range peers {
		if peer == rh.node.id {
			continue
		}

		_, err := rh.SendRequestVote(ctx, peer, req)
		results[peer] = err
	}

	return results
}
