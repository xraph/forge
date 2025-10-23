package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus"
)

// Coordinator manages distributed HLS operations using consensus
type Coordinator struct {
	nodeID       string
	consensus    consensus.ConsensusService
	stateMachine *HLSStateMachine
	logger       forge.Logger

	// Local state cache
	streams   map[string]*StreamState
	streamsMu sync.RWMutex

	// Leadership
	isLeader bool
	leaderID string
	leaderMu sync.RWMutex

	// Background workers
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StreamState represents distributed stream state
type StreamState struct {
	StreamID  string            `json:"stream_id"`
	OwnerNode string            `json:"owner_node"`
	Status    string            `json:"status"`
	Type      string            `json:"type"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Metadata  map[string]string `json:"metadata"`
	Variants  []string          `json:"variants"`
}

// NodeState represents the state of an HLS node
type NodeState struct {
	NodeID        string    `json:"node_id"`
	ActiveStreams int       `json:"active_streams"`
	TotalViewers  int       `json:"total_viewers"`
	Bandwidth     int64     `json:"bandwidth"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Healthy       bool      `json:"healthy"`
}

// Command types for consensus
const (
	CommandCreateStream = "hls:create_stream"
	CommandDeleteStream = "hls:delete_stream"
	CommandUpdateStream = "hls:update_stream"
	CommandStartStream  = "hls:start_stream"
	CommandStopStream   = "hls:stop_stream"
	CommandUpdateNode   = "hls:update_node"
	CommandStreamOwner  = "hls:stream_owner"
)

// NewCoordinator creates a new distributed coordinator
func NewCoordinator(nodeID string, consensusSvc consensus.ConsensusService, logger forge.Logger) *Coordinator {
	ctx, cancel := context.WithCancel(context.Background())

	// Create state machine
	stateMachine := NewHLSStateMachine(logger)

	return &Coordinator{
		nodeID:       nodeID,
		consensus:    consensusSvc,
		stateMachine: stateMachine,
		logger:       logger,
		streams:      make(map[string]*StreamState),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start starts the distributed coordinator
func (c *Coordinator) Start() error {
	c.logger.Info("starting distributed HLS coordinator",
		forge.F("node_id", c.nodeID),
	)

	// Start leadership monitoring
	c.wg.Add(1)
	go c.monitorLeadership()

	// Start node heartbeat
	c.wg.Add(1)
	go c.sendHeartbeats()

	// Start state synchronization
	c.wg.Add(1)
	go c.synchronizeState()

	return nil
}

// Stop stops the coordinator
func (c *Coordinator) Stop() error {
	c.logger.Info("stopping distributed HLS coordinator")
	c.cancel()
	c.wg.Wait()
	return nil
}

// IsLeader returns true if this node is the consensus leader
func (c *Coordinator) IsLeader() bool {
	c.leaderMu.RLock()
	defer c.leaderMu.RUnlock()
	return c.isLeader
}

// GetLeader returns the current leader node ID
func (c *Coordinator) GetLeader() string {
	c.leaderMu.RLock()
	defer c.leaderMu.RUnlock()
	return c.leaderID
}

// CreateStream registers a new stream in the distributed system
func (c *Coordinator) CreateStream(ctx context.Context, streamID, streamType string, metadata map[string]string) error {
	state := &StreamState{
		StreamID:  streamID,
		OwnerNode: c.nodeID,
		Status:    "created",
		Type:      streamType,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  metadata,
		Variants:  []string{},
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal stream state: %w", err)
	}

	cmd := consensus.Command{
		Type: CommandCreateStream,
		Payload: map[string]interface{}{
			"stream_id": streamID,
			"data":      data,
		},
	}

	// Apply through consensus
	if err := c.consensus.Apply(ctx, cmd); err != nil {
		return fmt.Errorf("failed to create stream in consensus: %w", err)
	}

	// Update local cache
	c.streamsMu.Lock()
	c.streams[streamID] = state
	c.streamsMu.Unlock()

	c.logger.Info("stream created in distributed system",
		forge.F("stream_id", streamID),
		forge.F("owner", c.nodeID),
	)

	return nil
}

// DeleteStream removes a stream from the distributed system
func (c *Coordinator) DeleteStream(ctx context.Context, streamID string) error {
	cmd := consensus.Command{
		Type: CommandDeleteStream,
		Payload: map[string]interface{}{
			"stream_id": streamID,
		},
	}

	if err := c.consensus.Apply(ctx, cmd); err != nil {
		return fmt.Errorf("failed to delete stream in consensus: %w", err)
	}

	// Update local cache
	c.streamsMu.Lock()
	delete(c.streams, streamID)
	c.streamsMu.Unlock()

	c.logger.Info("stream deleted from distributed system",
		forge.F("stream_id", streamID),
	)

	return nil
}

// UpdateStream updates stream state in the distributed system
func (c *Coordinator) UpdateStream(ctx context.Context, streamID string, updates map[string]interface{}) error {
	c.streamsMu.RLock()
	state, exists := c.streams[streamID]
	c.streamsMu.RUnlock()

	if !exists {
		return fmt.Errorf("stream not found: %s", streamID)
	}

	// Apply updates
	if status, ok := updates["status"].(string); ok {
		state.Status = status
	}
	if variants, ok := updates["variants"].([]string); ok {
		state.Variants = variants
	}
	state.UpdatedAt = time.Now()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal stream state: %w", err)
	}

	cmd := consensus.Command{
		Type: CommandUpdateStream,
		Payload: map[string]interface{}{
			"stream_id": streamID,
			"data":      data,
		},
	}

	if err := c.consensus.Apply(ctx, cmd); err != nil {
		return fmt.Errorf("failed to update stream in consensus: %w", err)
	}

	// Update local cache
	c.streamsMu.Lock()
	c.streams[streamID] = state
	c.streamsMu.Unlock()

	return nil
}

// GetStream retrieves stream state from the distributed system
func (c *Coordinator) GetStream(ctx context.Context, streamID string) (*StreamState, error) {
	// Check local cache first
	c.streamsMu.RLock()
	if state, exists := c.streams[streamID]; exists {
		c.streamsMu.RUnlock()
		return state, nil
	}
	c.streamsMu.RUnlock()

	// Perform consistent read from consensus using state machine query
	query := map[string]interface{}{
		"type":      "get_stream",
		"stream_id": streamID,
	}

	result, err := c.consensus.Read(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to read stream from consensus: %w", err)
	}

	if result == nil {
		return nil, fmt.Errorf("stream not found: %s", streamID)
	}

	// Type assert the result
	state, ok := result.(*StreamState)
	if !ok {
		return nil, fmt.Errorf("unexpected result type from state machine")
	}

	// Update cache
	c.streamsMu.Lock()
	c.streams[streamID] = state
	c.streamsMu.Unlock()

	return state, nil
}

// ListStreams returns all streams in the distributed system
func (c *Coordinator) ListStreams(ctx context.Context) ([]*StreamState, error) {
	// Query from state machine for consistency
	query := map[string]interface{}{
		"type": "list_streams",
	}

	result, err := c.consensus.Read(ctx, query)
	if err != nil {
		// Fall back to local cache on error
		c.streamsMu.RLock()
		defer c.streamsMu.RUnlock()

		streams := make([]*StreamState, 0, len(c.streams))
		for _, state := range c.streams {
			streams = append(streams, state)
		}
		return streams, nil
	}

	// Type assert result
	streamMap, ok := result.(map[string]*StreamState)
	if !ok {
		return nil, fmt.Errorf("unexpected result type from state machine")
	}

	streams := make([]*StreamState, 0, len(streamMap))
	for _, state := range streamMap {
		streams = append(streams, state)
	}

	return streams, nil
}

// GetStreamOwner returns the node that owns the stream
func (c *Coordinator) GetStreamOwner(ctx context.Context, streamID string) (string, error) {
	state, err := c.GetStream(ctx, streamID)
	if err != nil {
		return "", err
	}

	return state.OwnerNode, nil
}

// IsStreamOwner checks if this node owns the stream
func (c *Coordinator) IsStreamOwner(ctx context.Context, streamID string) (bool, error) {
	owner, err := c.GetStreamOwner(ctx, streamID)
	if err != nil {
		return false, err
	}

	return owner == c.nodeID, nil
}

// TransferStreamOwnership transfers stream ownership to another node
func (c *Coordinator) TransferStreamOwnership(ctx context.Context, streamID, newOwner string) error {
	return c.UpdateStream(ctx, streamID, map[string]interface{}{
		"owner": newOwner,
	})
}

// GetClusterNodes returns all active nodes in the cluster
func (c *Coordinator) GetClusterNodes(ctx context.Context) ([]consensus.NodeInfo, error) {
	clusterInfo := c.consensus.GetClusterInfo()
	return clusterInfo.Nodes, nil
}

// GetClusterStats returns aggregated statistics across the cluster
func (c *Coordinator) GetClusterStats(ctx context.Context) (*ClusterStats, error) {
	nodes, err := c.GetClusterNodes(ctx)
	if err != nil {
		return nil, err
	}

	stats := &ClusterStats{
		TotalNodes:     len(nodes),
		HealthyNodes:   0,
		TotalStreams:   len(c.streams),
		TotalViewers:   0,
		TotalBandwidth: 0,
	}

	for _, node := range nodes {
		if node.Status == consensus.StatusActive {
			stats.HealthyNodes++
		}
	}

	// TODO: Aggregate viewer and bandwidth stats from all nodes

	return stats, nil
}

// monitorLeadership monitors leadership changes
func (c *Coordinator) monitorLeadership() {
	defer c.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			isLeader := c.consensus.IsLeader()
			leaderID := c.consensus.GetLeader()

			c.leaderMu.Lock()
			oldLeader := c.isLeader
			c.isLeader = isLeader
			c.leaderID = leaderID
			c.leaderMu.Unlock()

			// Handle leadership change
			if isLeader && !oldLeader {
				c.onBecameLeader()
			} else if !isLeader && oldLeader {
				c.onLostLeadership()
			}
		}
	}
}

// onBecameLeader handles becoming leader
func (c *Coordinator) onBecameLeader() {
	c.logger.Info("node became HLS cluster leader",
		forge.F("node_id", c.nodeID),
	)

	// Perform leader initialization
	// - Reconcile stream ownership
	// - Check for orphaned streams
	// - Initiate failover if needed
}

// onLostLeadership handles losing leadership
func (c *Coordinator) onLostLeadership() {
	c.logger.Info("node lost HLS cluster leadership",
		forge.F("node_id", c.nodeID),
	)
}

// sendHeartbeats sends periodic heartbeats to the cluster
func (c *Coordinator) sendHeartbeats() {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.sendHeartbeat()
		}
	}
}

// sendHeartbeat sends a single heartbeat
func (c *Coordinator) sendHeartbeat() {
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()

	c.streamsMu.RLock()
	activeStreams := len(c.streams)
	c.streamsMu.RUnlock()

	nodeState := &NodeState{
		NodeID:        c.nodeID,
		ActiveStreams: activeStreams,
		LastHeartbeat: time.Now(),
		Healthy:       true,
	}

	data, err := json.Marshal(nodeState)
	if err != nil {
		c.logger.Error("failed to marshal node state", forge.F("error", err))
		return
	}

	cmd := consensus.Command{
		Type: CommandUpdateNode,
		Payload: map[string]interface{}{
			"node_id": c.nodeID,
			"data":    data,
		},
	}

	if err := c.consensus.Apply(ctx, cmd); err != nil {
		c.logger.Warn("failed to send heartbeat", forge.F("error", err))
	}
}

// synchronizeState synchronizes local state with consensus
func (c *Coordinator) synchronizeState() {
	defer c.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.syncStreams()
		}
	}
}

// syncStreams synchronizes stream state
func (c *Coordinator) syncStreams() {
	// TODO: Query all stream keys from consensus
	// TODO: Update local cache
	// TODO: Detect and handle stale streams
}

// Helper functions

func streamKey(streamID string) string {
	return fmt.Sprintf("hls:stream:%s", streamID)
}

func nodeKey(nodeID string) string {
	return fmt.Sprintf("hls:node:%s", nodeID)
}

// ClusterStats represents cluster-wide statistics
type ClusterStats struct {
	TotalNodes     int   `json:"total_nodes"`
	HealthyNodes   int   `json:"healthy_nodes"`
	TotalStreams   int   `json:"total_streams"`
	TotalViewers   int   `json:"total_viewers"`
	TotalBandwidth int64 `json:"total_bandwidth"`
}
