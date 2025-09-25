package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/consensus/transport"
	"github.com/xraph/forge/pkg/logger"
)

// RaftRPC handles RPC communication between Raft nodes using the generic transport layer
type RaftRPC struct {
	nodeID         string
	transport      transport.Transport
	handler        RPCHandler
	logger         common.Logger
	metrics        common.Metrics
	mu             sync.RWMutex
	started        bool
	shutdown       chan struct{}
	stats          *RPCStats
	pendingRPCs    map[string]chan *rpcResponse
	pendingMu      sync.RWMutex
	messageHandler transport.MessageHandler
}

// RPCHandler handles incoming RPC requests (same as before)
type RPCHandler interface {
	HandleAppendEntries(req *AppendEntriesRequest) *AppendEntriesResponse
	HandleRequestVote(req *RequestVoteRequest) *RequestVoteResponse
	HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse
}

// RPCStats contains RPC statistics (same as before)
type RPCStats struct {
	AppendEntriesRequestsSent    int64 `json:"append_entries_requests_sent"`
	AppendEntriesResponsesRecv   int64 `json:"append_entries_responses_received"`
	RequestVoteRequestsSent      int64 `json:"request_vote_requests_sent"`
	RequestVoteResponsesRecv     int64 `json:"request_vote_responses_received"`
	InstallSnapshotRequestsSent  int64 `json:"install_snapshot_requests_sent"`
	InstallSnapshotResponsesRecv int64 `json:"install_snapshot_responses_received"`
	AppendEntriesRequestsRecv    int64 `json:"append_entries_requests_received"`
	AppendEntriesResponsesSent   int64 `json:"append_entries_responses_sent"`
	RequestVoteRequestsRecv      int64 `json:"request_vote_requests_received"`
	RequestVoteResponsesSent     int64 `json:"request_vote_responses_sent"`
	InstallSnapshotRequestsRecv  int64 `json:"install_snapshot_requests_received"`
	InstallSnapshotResponsesSent int64 `json:"install_snapshot_responses_sent"`
	Errors                       int64 `json:"errors"`
	mu                           sync.RWMutex
}

// rpcResponse represents a response to an RPC request
type rpcResponse struct {
	Data  []byte
	Error error
}

// raftMessageHandler implements transport.MessageHandler for Raft RPC messages
type raftMessageHandler struct {
	rpc *RaftRPC
}

// NewRaftRPC creates a new RaftRPC instance
func NewRaftRPC(nodeID string, transport transport.Transport, handler RPCHandler, logger common.Logger, metrics common.Metrics) *RaftRPC {
	rpc := &RaftRPC{
		nodeID:      nodeID,
		transport:   transport,
		handler:     handler,
		logger:      logger,
		metrics:     metrics,
		shutdown:    make(chan struct{}),
		stats:       &RPCStats{},
		pendingRPCs: make(map[string]chan *rpcResponse),
	}

	rpc.messageHandler = &raftMessageHandler{rpc: rpc}
	return rpc
}

// Start starts the RPC server
func (rpc *RaftRPC) Start(ctx context.Context) error {
	rpc.mu.Lock()
	defer rpc.mu.Unlock()

	if rpc.started {
		return fmt.Errorf("RPC server already started")
	}

	// OnStart transport
	if err := rpc.transport.Start(ctx); err != nil {
		return fmt.Errorf("failed to start transport: %w", err)
	}

	// OnStart message processing
	go rpc.processMessages(ctx)

	rpc.started = true

	if rpc.logger != nil {
		rpc.logger.Info("RPC server started", logger.String("node_id", rpc.nodeID))
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_server_started").Inc()
	}

	return nil
}

// Stop stops the RPC server
func (rpc *RaftRPC) Stop(ctx context.Context) error {
	rpc.mu.Lock()
	defer rpc.mu.Unlock()

	if !rpc.started {
		return fmt.Errorf("RPC server not started")
	}

	close(rpc.shutdown)

	// OnStop transport
	if err := rpc.transport.Stop(ctx); err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to stop transport", logger.Error(err))
		}
	}

	// Clean up pending RPCs
	rpc.pendingMu.Lock()
	for _, ch := range rpc.pendingRPCs {
		close(ch)
	}
	rpc.pendingRPCs = make(map[string]chan *rpcResponse)
	rpc.pendingMu.Unlock()

	rpc.started = false

	if rpc.logger != nil {
		rpc.logger.Info("RPC server stopped", logger.String("node_id", rpc.nodeID))
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_server_stopped").Inc()
	}

	return nil
}

// processMessages processes incoming messages from the transport
func (rpc *RaftRPC) processMessages(ctx context.Context) {
	messageChan, err := rpc.transport.Receive(ctx)
	if err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to get message channel", logger.Error(err))
		}
		return
	}

	for {
		select {
		case <-rpc.shutdown:
			return
		case <-ctx.Done():
			return
		case msg, ok := <-messageChan:
			if !ok {
				return
			}
			go rpc.handleMessage(msg)
		}
	}
}

// handleMessage handles an incoming message
func (rpc *RaftRPC) handleMessage(msg transport.IncomingMessage) {
	switch msg.Message.Type {
	case "append_entries_request":
		rpc.handleAppendEntriesRequest(msg)
	case "request_vote_request":
		rpc.handleRequestVoteRequest(msg)
	case "install_snapshot_request":
		rpc.handleInstallSnapshotRequest(msg)
	case "append_entries_response":
		rpc.handleAppendEntriesResponse(msg)
	case "request_vote_response":
		rpc.handleRequestVoteResponse(msg)
	case "install_snapshot_response":
		rpc.handleInstallSnapshotResponse(msg)
	default:
		if rpc.logger != nil {
			rpc.logger.Warn("unknown message type", logger.String("type", string(msg.Message.Type)))
		}
	}
}

// SendAppendEntries sends an AppendEntries RPC to a peer
func (rpc *RaftRPC) SendAppendEntries(ctx context.Context, peerID string, req *AppendEntriesRequest) (*AppendEntriesResponse, error) {
	startTime := time.Now()

	rpc.stats.mu.Lock()
	rpc.stats.AppendEntriesRequestsSent++
	rpc.stats.mu.Unlock()

	if rpc.logger != nil {
		rpc.logger.Debug("sending append entries",
			logger.String("peer_id", peerID),
			logger.Uint64("term", req.Term),
			logger.Uint64("prev_log_index", req.PrevLogIndex),
			logger.Int("entries", len(req.Entries)),
		)
	}

	// Serialize request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	// Send request and wait for response
	response, err := rpc.sendRPCRequest(ctx, peerID, "append_entries_request", data)
	if err != nil {
		rpc.stats.mu.Lock()
		rpc.stats.Errors++
		rpc.stats.mu.Unlock()

		if rpc.logger != nil {
			rpc.logger.Error("failed to send append entries",
				logger.String("peer_id", peerID),
				logger.Error(err),
			)
		}

		if rpc.metrics != nil {
			rpc.metrics.Counter("forge.consensus.raft.rpc_append_entries_errors").Inc()
		}

		return nil, err
	}

	// Deserialize response
	var resp AppendEntriesResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("failed to deserialize response: %w", err)
	}

	rpc.stats.mu.Lock()
	rpc.stats.AppendEntriesResponsesRecv++
	rpc.stats.mu.Unlock()

	duration := time.Since(startTime)

	if rpc.logger != nil {
		rpc.logger.Debug("received append entries response",
			logger.String("peer_id", peerID),
			logger.Uint64("term", resp.Term),
			logger.Bool("success", resp.Success),
			logger.Duration("duration", duration),
		)
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_append_entries_sent").Inc()
		rpc.metrics.Histogram("forge.consensus.raft.rpc_append_entries_duration").Observe(duration.Seconds())
	}

	return &resp, nil
}

// SendRequestVote sends a RequestVote RPC to a peer
func (rpc *RaftRPC) SendRequestVote(ctx context.Context, peerID string, req *RequestVoteRequest) (*RequestVoteResponse, error) {
	startTime := time.Now()

	rpc.stats.mu.Lock()
	rpc.stats.RequestVoteRequestsSent++
	rpc.stats.mu.Unlock()

	if rpc.logger != nil {
		rpc.logger.Debug("sending request vote",
			logger.String("peer_id", peerID),
			logger.Uint64("term", req.Term),
			logger.String("candidate_id", req.CandidateID),
		)
	}

	// Serialize request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	// Send request and wait for response
	response, err := rpc.sendRPCRequest(ctx, peerID, "request_vote_request", data)
	if err != nil {
		rpc.stats.mu.Lock()
		rpc.stats.Errors++
		rpc.stats.mu.Unlock()

		if rpc.logger != nil {
			rpc.logger.Error("failed to send request vote",
				logger.String("peer_id", peerID),
				logger.Error(err),
			)
		}

		if rpc.metrics != nil {
			rpc.metrics.Counter("forge.consensus.raft.rpc_request_vote_errors").Inc()
		}

		return nil, err
	}

	// Deserialize response
	var resp RequestVoteResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("failed to deserialize response: %w", err)
	}

	rpc.stats.mu.Lock()
	rpc.stats.RequestVoteResponsesRecv++
	rpc.stats.mu.Unlock()

	duration := time.Since(startTime)

	if rpc.logger != nil {
		rpc.logger.Debug("received request vote response",
			logger.String("peer_id", peerID),
			logger.Uint64("term", resp.Term),
			logger.Bool("vote_granted", resp.VoteGranted),
			logger.Duration("duration", duration),
		)
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_request_vote_sent").Inc()
		rpc.metrics.Histogram("forge.consensus.raft.rpc_request_vote_duration").Observe(duration.Seconds())
	}

	return &resp, nil
}

// SendInstallSnapshot sends an InstallSnapshot RPC to a peer
func (rpc *RaftRPC) SendInstallSnapshot(ctx context.Context, peerID string, req *InstallSnapshotRequest) (*InstallSnapshotResponse, error) {
	startTime := time.Now()

	rpc.stats.mu.Lock()
	rpc.stats.InstallSnapshotRequestsSent++
	rpc.stats.mu.Unlock()

	if rpc.logger != nil {
		rpc.logger.Debug("sending install snapshot",
			logger.String("peer_id", peerID),
			logger.Uint64("term", req.Term),
			logger.Uint64("last_included_index", req.LastIncludedIndex),
			logger.Uint64("offset", req.Offset),
			logger.Int("data_size", len(req.Data)),
			logger.Bool("done", req.Done),
		)
	}

	// Serialize request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	// Send request and wait for response
	response, err := rpc.sendRPCRequest(ctx, peerID, "install_snapshot_request", data)
	if err != nil {
		rpc.stats.mu.Lock()
		rpc.stats.Errors++
		rpc.stats.mu.Unlock()

		if rpc.logger != nil {
			rpc.logger.Error("failed to send install snapshot",
				logger.String("peer_id", peerID),
				logger.Error(err),
			)
		}

		if rpc.metrics != nil {
			rpc.metrics.Counter("forge.consensus.raft.rpc_install_snapshot_errors").Inc()
		}

		return nil, err
	}

	// Deserialize response
	var resp InstallSnapshotResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("failed to deserialize response: %w", err)
	}

	rpc.stats.mu.Lock()
	rpc.stats.InstallSnapshotResponsesRecv++
	rpc.stats.mu.Unlock()

	duration := time.Since(startTime)

	if rpc.logger != nil {
		rpc.logger.Debug("received install snapshot response",
			logger.String("peer_id", peerID),
			logger.Uint64("term", resp.Term),
			logger.Duration("duration", duration),
		)
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_install_snapshot_sent").Inc()
		rpc.metrics.Histogram("forge.consensus.raft.rpc_install_snapshot_duration").Observe(duration.Seconds())
	}

	return &resp, nil
}

// sendRPCRequest sends an RPC request and waits for the response
func (rpc *RaftRPC) sendRPCRequest(ctx context.Context, peerID string, msgType string, data []byte) ([]byte, error) {
	// Create a unique request ID
	requestID := transport.GenerateMessageID()

	// Create a channel to receive the response
	responseChan := make(chan *rpcResponse, 1)

	// Register the pending RPC
	rpc.pendingMu.Lock()
	rpc.pendingRPCs[requestID] = responseChan
	rpc.pendingMu.Unlock()

	// Clean up on exit
	defer func() {
		rpc.pendingMu.Lock()
		delete(rpc.pendingRPCs, requestID)
		rpc.pendingMu.Unlock()
	}()

	// Create the transport message
	message := transport.NewMessageBuilder().
		WithType(transport.MessageType(msgType)).
		WithFrom(rpc.nodeID).
		WithTo(peerID).
		WithData(data).
		WithID(requestID).
		Build()

	// Send the message
	if err := rpc.transport.Send(ctx, peerID, message); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response, ok := <-responseChan:
		if !ok {
			return nil, fmt.Errorf("response channel closed")
		}
		if response.Error != nil {
			return nil, response.Error
		}
		return response.Data, nil
	}
}

// sendRPCResponse sends an RPC response
func (rpc *RaftRPC) sendRPCResponse(ctx context.Context, peerID string, msgType string, requestID string, data []byte) error {
	// Create the transport message
	message := transport.NewMessageBuilder().
		WithType(transport.MessageType(msgType)).
		WithFrom(rpc.nodeID).
		WithTo(peerID).
		WithData(data).
		WithMetadata("request_id", requestID).
		Build()

	// Send the message
	return rpc.transport.Send(ctx, peerID, message)
}

// handleAppendEntriesRequest handles incoming AppendEntries requests
func (rpc *RaftRPC) handleAppendEntriesRequest(msg transport.IncomingMessage) {
	startTime := time.Now()

	rpc.stats.mu.Lock()
	rpc.stats.AppendEntriesRequestsRecv++
	rpc.stats.mu.Unlock()

	// Deserialize request
	var req AppendEntriesRequest
	if err := json.Unmarshal(msg.Message.Data, &req); err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to deserialize append entries request", logger.Error(err))
		}
		return
	}

	if rpc.logger != nil {
		rpc.logger.Debug("handling append entries",
			logger.String("leader_id", req.LeaderID),
			logger.Uint64("term", req.Term),
			logger.Uint64("prev_log_index", req.PrevLogIndex),
			logger.Int("entries", len(req.Entries)),
		)
	}

	// Handle the request
	resp := rpc.handler.HandleAppendEntries(&req)

	rpc.stats.mu.Lock()
	rpc.stats.AppendEntriesResponsesSent++
	rpc.stats.mu.Unlock()

	duration := time.Since(startTime)

	if rpc.logger != nil {
		rpc.logger.Debug("handled append entries",
			logger.String("leader_id", req.LeaderID),
			logger.Uint64("term", resp.Term),
			logger.Bool("success", resp.Success),
			logger.Duration("duration", duration),
		)
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_append_entries_handled").Inc()
		rpc.metrics.Histogram("forge.consensus.raft.rpc_append_entries_handle_duration").Observe(duration.Seconds())
	}

	// Serialize response
	data, err := json.Marshal(resp)
	if err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to serialize append entries response", logger.Error(err))
		}
		return
	}

	// Send response
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rpc.sendRPCResponse(ctx, msg.Message.From, "append_entries_response", msg.Message.ID, data); err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to send append entries response", logger.Error(err))
		}
	}
}

// handleRequestVoteRequest handles incoming RequestVote requests
func (rpc *RaftRPC) handleRequestVoteRequest(msg transport.IncomingMessage) {
	startTime := time.Now()

	rpc.stats.mu.Lock()
	rpc.stats.RequestVoteRequestsRecv++
	rpc.stats.mu.Unlock()

	// Deserialize request
	var req RequestVoteRequest
	if err := json.Unmarshal(msg.Message.Data, &req); err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to deserialize request vote request", logger.Error(err))
		}
		return
	}

	if rpc.logger != nil {
		rpc.logger.Debug("handling request vote",
			logger.String("candidate_id", req.CandidateID),
			logger.Uint64("term", req.Term),
			logger.Uint64("last_log_index", req.LastLogIndex),
		)
	}

	// Handle the request
	resp := rpc.handler.HandleRequestVote(&req)

	rpc.stats.mu.Lock()
	rpc.stats.RequestVoteResponsesSent++
	rpc.stats.mu.Unlock()

	duration := time.Since(startTime)

	if rpc.logger != nil {
		rpc.logger.Debug("handled request vote",
			logger.String("candidate_id", req.CandidateID),
			logger.Uint64("term", resp.Term),
			logger.Bool("vote_granted", resp.VoteGranted),
			logger.Duration("duration", duration),
		)
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_request_vote_handled").Inc()
		rpc.metrics.Histogram("forge.consensus.raft.rpc_request_vote_handle_duration").Observe(duration.Seconds())
	}

	// Serialize response
	data, err := json.Marshal(resp)
	if err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to serialize request vote response", logger.Error(err))
		}
		return
	}

	// Send response
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rpc.sendRPCResponse(ctx, msg.Message.From, "request_vote_response", msg.Message.ID, data); err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to send request vote response", logger.Error(err))
		}
	}
}

// handleInstallSnapshotRequest handles incoming InstallSnapshot requests
func (rpc *RaftRPC) handleInstallSnapshotRequest(msg transport.IncomingMessage) {
	startTime := time.Now()

	rpc.stats.mu.Lock()
	rpc.stats.InstallSnapshotRequestsRecv++
	rpc.stats.mu.Unlock()

	// Deserialize request
	var req InstallSnapshotRequest
	if err := json.Unmarshal(msg.Message.Data, &req); err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to deserialize install snapshot request", logger.Error(err))
		}
		return
	}

	if rpc.logger != nil {
		rpc.logger.Debug("handling install snapshot",
			logger.String("leader_id", req.LeaderID),
			logger.Uint64("term", req.Term),
			logger.Uint64("last_included_index", req.LastIncludedIndex),
			logger.Uint64("offset", req.Offset),
			logger.Int("data_size", len(req.Data)),
			logger.Bool("done", req.Done),
		)
	}

	// Handle the request
	resp := rpc.handler.HandleInstallSnapshot(&req)

	rpc.stats.mu.Lock()
	rpc.stats.InstallSnapshotResponsesSent++
	rpc.stats.mu.Unlock()

	duration := time.Since(startTime)

	if rpc.logger != nil {
		rpc.logger.Debug("handled install snapshot",
			logger.String("leader_id", req.LeaderID),
			logger.Uint64("term", resp.Term),
			logger.Duration("duration", duration),
		)
	}

	if rpc.metrics != nil {
		rpc.metrics.Counter("forge.consensus.raft.rpc_install_snapshot_handled").Inc()
		rpc.metrics.Histogram("forge.consensus.raft.rpc_install_snapshot_handle_duration").Observe(duration.Seconds())
	}

	// Serialize response
	data, err := json.Marshal(resp)
	if err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to serialize install snapshot response", logger.Error(err))
		}
		return
	}

	// Send response
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rpc.sendRPCResponse(ctx, msg.Message.From, "install_snapshot_response", msg.Message.ID, data); err != nil {
		if rpc.logger != nil {
			rpc.logger.Error("failed to send install snapshot response", logger.Error(err))
		}
	}
}

// Response handlers

// handleAppendEntriesResponse handles AppendEntries responses
func (rpc *RaftRPC) handleAppendEntriesResponse(msg transport.IncomingMessage) {
	requestID := msg.Message.ID
	if correlationID, ok := msg.Message.Metadata["request_id"]; ok {
		if id, ok := correlationID.(string); ok {
			requestID = id
		}
	}

	rpc.pendingMu.RLock()
	responseChan, exists := rpc.pendingRPCs[requestID]
	rpc.pendingMu.RUnlock()

	if !exists {
		if rpc.logger != nil {
			rpc.logger.Warn("received response for unknown request", logger.String("request_id", requestID))
		}
		return
	}

	select {
	case responseChan <- &rpcResponse{Data: msg.Message.Data, Error: nil}:
	default:
		if rpc.logger != nil {
			rpc.logger.Warn("response channel full", logger.String("request_id", requestID))
		}
	}
}

// handleRequestVoteResponse handles RequestVote responses
func (rpc *RaftRPC) handleRequestVoteResponse(msg transport.IncomingMessage) {
	requestID := msg.Message.ID
	if correlationID, ok := msg.Message.Metadata["request_id"]; ok {
		if id, ok := correlationID.(string); ok {
			requestID = id
		}
	}

	rpc.pendingMu.RLock()
	responseChan, exists := rpc.pendingRPCs[requestID]
	rpc.pendingMu.RUnlock()

	if !exists {
		if rpc.logger != nil {
			rpc.logger.Warn("received response for unknown request", logger.String("request_id", requestID))
		}
		return
	}

	select {
	case responseChan <- &rpcResponse{Data: msg.Message.Data, Error: nil}:
	default:
		if rpc.logger != nil {
			rpc.logger.Warn("response channel full", logger.String("request_id", requestID))
		}
	}
}

// handleInstallSnapshotResponse handles InstallSnapshot responses
func (rpc *RaftRPC) handleInstallSnapshotResponse(msg transport.IncomingMessage) {
	requestID := msg.Message.ID
	if correlationID, ok := msg.Message.Metadata["request_id"]; ok {
		if id, ok := correlationID.(string); ok {
			requestID = id
		}
	}

	rpc.pendingMu.RLock()
	responseChan, exists := rpc.pendingRPCs[requestID]
	rpc.pendingMu.RUnlock()

	if !exists {
		if rpc.logger != nil {
			rpc.logger.Warn("received response for unknown request", logger.String("request_id", requestID))
		}
		return
	}

	select {
	case responseChan <- &rpcResponse{Data: msg.Message.Data, Error: nil}:
	default:
		if rpc.logger != nil {
			rpc.logger.Warn("response channel full", logger.String("request_id", requestID))
		}
	}
}

// Remaining methods (same as before, but updated to use the new interface)

// GetStats returns RPC statistics
func (rpc *RaftRPC) GetStats() *RPCStats {
	rpc.stats.mu.RLock()
	defer rpc.stats.mu.RUnlock()

	return &RPCStats{
		AppendEntriesRequestsSent:    rpc.stats.AppendEntriesRequestsSent,
		AppendEntriesResponsesRecv:   rpc.stats.AppendEntriesResponsesRecv,
		RequestVoteRequestsSent:      rpc.stats.RequestVoteRequestsSent,
		RequestVoteResponsesRecv:     rpc.stats.RequestVoteResponsesRecv,
		InstallSnapshotRequestsSent:  rpc.stats.InstallSnapshotRequestsSent,
		InstallSnapshotResponsesRecv: rpc.stats.InstallSnapshotResponsesRecv,
		AppendEntriesRequestsRecv:    rpc.stats.AppendEntriesRequestsRecv,
		AppendEntriesResponsesSent:   rpc.stats.AppendEntriesResponsesSent,
		RequestVoteRequestsRecv:      rpc.stats.RequestVoteRequestsRecv,
		RequestVoteResponsesSent:     rpc.stats.RequestVoteResponsesSent,
		InstallSnapshotRequestsRecv:  rpc.stats.InstallSnapshotRequestsRecv,
		InstallSnapshotResponsesSent: rpc.stats.InstallSnapshotResponsesSent,
		Errors:                       rpc.stats.Errors,
	}
}

// ResetStats resets RPC statistics
func (rpc *RaftRPC) ResetStats() {
	rpc.stats.mu.Lock()
	defer rpc.stats.mu.Unlock()

	rpc.stats.AppendEntriesRequestsSent = 0
	rpc.stats.AppendEntriesResponsesRecv = 0
	rpc.stats.RequestVoteRequestsSent = 0
	rpc.stats.RequestVoteResponsesRecv = 0
	rpc.stats.InstallSnapshotRequestsSent = 0
	rpc.stats.InstallSnapshotResponsesRecv = 0
	rpc.stats.AppendEntriesRequestsRecv = 0
	rpc.stats.AppendEntriesResponsesSent = 0
	rpc.stats.RequestVoteRequestsRecv = 0
	rpc.stats.RequestVoteResponsesSent = 0
	rpc.stats.InstallSnapshotRequestsRecv = 0
	rpc.stats.InstallSnapshotResponsesSent = 0
	rpc.stats.Errors = 0
}

// IsStarted returns true if the RPC server is started
func (rpc *RaftRPC) IsStarted() bool {
	rpc.mu.RLock()
	defer rpc.mu.RUnlock()
	return rpc.started
}

// GetNodeID returns the node ID
func (rpc *RaftRPC) GetNodeID() string {
	return rpc.nodeID
}

// GetTransport returns the transport
func (rpc *RaftRPC) GetTransport() transport.Transport {
	return rpc.transport
}

// SetHandler sets the RPC handler
func (rpc *RaftRPC) SetHandler(handler RPCHandler) {
	rpc.mu.Lock()
	defer rpc.mu.Unlock()
	rpc.handler = handler
}

// GetHandler returns the RPC handler
func (rpc *RaftRPC) GetHandler() RPCHandler {
	rpc.mu.RLock()
	defer rpc.mu.RUnlock()
	return rpc.handler
}

// HealthCheck performs a health check on the RPC server
func (rpc *RaftRPC) HealthCheck(ctx context.Context) error {
	rpc.mu.RLock()
	defer rpc.mu.RUnlock()

	if !rpc.started {
		return fmt.Errorf("RPC server not started")
	}

	// Check transport health
	if err := rpc.transport.HealthCheck(ctx); err != nil {
		return fmt.Errorf("transport unhealthy: %w", err)
	}

	// Check error rate
	stats := rpc.GetStats()
	totalRequests := stats.AppendEntriesRequestsSent + stats.RequestVoteRequestsSent + stats.InstallSnapshotRequestsSent
	if totalRequests > 0 {
		errorRate := float64(stats.Errors) / float64(totalRequests)
		if errorRate > 0.1 { // 10% error rate threshold
			return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
		}
	}

	return nil
}

// GetNodeAddress returns the address of a node
func (rpc *RaftRPC) GetNodeAddress(nodeID string) (string, error) {
	return rpc.transport.GetAddress(nodeID)
}

// BroadcastAppendEntries sends AppendEntries to all peers
func (rpc *RaftRPC) BroadcastAppendEntries(ctx context.Context, peers []string, req *AppendEntriesRequest) map[string]*AppendEntriesResponse {
	results := make(map[string]*AppendEntriesResponse)
	var wg sync.WaitGroup

	for _, peerID := range peers {
		wg.Add(1)
		go func(peer string) {
			defer wg.Done()
			resp, err := rpc.SendAppendEntries(ctx, peer, req)
			if err != nil {
				if rpc.logger != nil {
					rpc.logger.Error("failed to send append entries to peer",
						logger.String("peer_id", peer),
						logger.Error(err),
					)
				}
				return
			}
			results[peer] = resp
		}(peerID)
	}

	wg.Wait()
	return results
}

// BroadcastRequestVote sends RequestVote to all peers
func (rpc *RaftRPC) BroadcastRequestVote(ctx context.Context, peers []string, req *RequestVoteRequest) map[string]*RequestVoteResponse {
	results := make(map[string]*RequestVoteResponse)
	var wg sync.WaitGroup

	for _, peerID := range peers {
		wg.Add(1)
		go func(peer string) {
			defer wg.Done()
			resp, err := rpc.SendRequestVote(ctx, peer, req)
			if err != nil {
				if rpc.logger != nil {
					rpc.logger.Error("failed to send request vote to peer",
						logger.String("peer_id", peer),
						logger.Error(err),
					)
				}
				return
			}
			results[peer] = resp
		}(peerID)
	}

	wg.Wait()
	return results
}

// GetPeerLatency returns the latency to a peer
func (rpc *RaftRPC) GetPeerLatency(ctx context.Context, peerID string) (time.Duration, error) {
	// Send a heartbeat to measure latency
	req := &AppendEntriesRequest{
		Term:         0,
		LeaderID:     rpc.nodeID,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      []storage.LogEntry{},
		LeaderCommit: 0,
	}

	start := time.Now()
	_, err := rpc.SendAppendEntries(ctx, peerID, req)
	if err != nil {
		return 0, err
	}

	return time.Since(start), nil
}

// GetPeerStatus returns the status of a peer
func (rpc *RaftRPC) GetPeerStatus(ctx context.Context, peerID string) (*PeerStatus, error) {
	latency, err := rpc.GetPeerLatency(ctx, peerID)
	if err != nil {
		return &PeerStatus{
			PeerID:      peerID,
			Reachable:   false,
			LastContact: time.Time{},
			Latency:     0,
		}, nil
	}

	return &PeerStatus{
		PeerID:      peerID,
		Reachable:   true,
		LastContact: time.Now(),
		Latency:     latency,
	}, nil
}

// PeerStatus represents the status of a peer
type PeerStatus struct {
	PeerID      string        `json:"peer_id"`
	Reachable   bool          `json:"reachable"`
	LastContact time.Time     `json:"last_contact"`
	Latency     time.Duration `json:"latency"`
}

// GetAllPeerStatus returns the status of all peers
func (rpc *RaftRPC) GetAllPeerStatus(ctx context.Context, peers []string) map[string]*PeerStatus {
	results := make(map[string]*PeerStatus)
	var wg sync.WaitGroup

	for _, peerID := range peers {
		wg.Add(1)
		go func(peer string) {
			defer wg.Done()
			status, err := rpc.GetPeerStatus(ctx, peer)
			if err != nil {
				status = &PeerStatus{
					PeerID:      peer,
					Reachable:   false,
					LastContact: time.Time{},
					Latency:     0,
				}
			}
			results[peer] = status
		}(peerID)
	}

	wg.Wait()
	return results
}

// Close closes the RPC server
func (rpc *RaftRPC) Close() error {
	return rpc.Stop(context.Background())
}

// MessageTypes returns the message types this handler supports
func (h *raftMessageHandler) MessageTypes() []transport.MessageType {
	return []transport.MessageType{
		"append_entries_request",
		"append_entries_response",
		"request_vote_request",
		"request_vote_response",
		"install_snapshot_request",
		"install_snapshot_response",
	}
}

// HandleMessage handles a message (implements transport.MessageHandler)
func (h *raftMessageHandler) HandleMessage(ctx context.Context, message transport.IncomingMessage) error {
	h.rpc.handleMessage(message)
	return nil
}
