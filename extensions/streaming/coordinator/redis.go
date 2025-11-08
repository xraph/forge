package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/internal/errors"
)

// redisCoordinator implements StreamCoordinator using Redis Pub/Sub.
type redisCoordinator struct {
	client  *redis.Client
	nodeID  string
	pubsub  *redis.PubSub
	handler MessageHandler
	stopCh  chan struct{}
	running bool
}

// NewRedisCoordinator creates a Redis-based coordinator.
func NewRedisCoordinator(client *redis.Client, nodeID string) StreamCoordinator {
	return &redisCoordinator{
		client: client,
		nodeID: nodeID,
		stopCh: make(chan struct{}),
	}
}

// Start starts the coordinator.
func (rc *redisCoordinator) Start(ctx context.Context) error {
	if rc.running {
		return errors.New("coordinator already running")
	}

	// Subscribe to channels
	rc.pubsub = rc.client.Subscribe(ctx,
		"streaming:broadcast:global",
		"streaming:broadcast:node:"+rc.nodeID,
		"streaming:presence:updates",
		"streaming:state:rooms",
	)

	// Start message listener
	go rc.listen(ctx)

	rc.running = true

	return nil
}

// Stop stops the coordinator.
func (rc *redisCoordinator) Stop(ctx context.Context) error {
	if !rc.running {
		return nil
	}

	close(rc.stopCh)

	if rc.pubsub != nil {
		if err := rc.pubsub.Close(); err != nil {
			return fmt.Errorf("failed to close pubsub: %w", err)
		}
	}

	rc.running = false

	return nil
}

// BroadcastToNode sends message to specific node.
func (rc *redisCoordinator) BroadcastToNode(ctx context.Context, nodeID string, msg *streaming.Message) error {
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeBroadcast,
		NodeID:    nodeID,
		Payload:   msg,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(coordMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	channel := "streaming:broadcast:node:" + nodeID

	return rc.client.Publish(ctx, channel, data).Err()
}

// BroadcastToUser sends to user across all nodes.
func (rc *redisCoordinator) BroadcastToUser(ctx context.Context, userID string, msg *streaming.Message) error {
	// Get nodes where user is connected
	nodes, err := rc.GetUserNodes(ctx, userID)
	if err != nil {
		return err
	}

	// Broadcast to each node
	for _, nodeID := range nodes {
		if err := rc.BroadcastToNode(ctx, nodeID, msg); err != nil {
			// Log error but continue
			continue
		}
	}

	return nil
}

// BroadcastToRoom sends to room across all nodes.
func (rc *redisCoordinator) BroadcastToRoom(ctx context.Context, roomID string, msg *streaming.Message) error {
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeBroadcast,
		RoomID:    roomID,
		Payload:   msg,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(coordMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	channel := "streaming:broadcast:room:" + roomID

	return rc.client.Publish(ctx, channel, data).Err()
}

// BroadcastGlobal sends to all nodes.
func (rc *redisCoordinator) BroadcastGlobal(ctx context.Context, msg *streaming.Message) error {
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeBroadcast,
		Payload:   msg,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(coordMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return rc.client.Publish(ctx, "streaming:broadcast:global", data).Err()
}

// SyncPresence synchronizes presence across nodes.
func (rc *redisCoordinator) SyncPresence(ctx context.Context, presence *streaming.UserPresence) error {
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypePresenceUpdate,
		UserID:    presence.UserID,
		Payload:   presence,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(coordMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal presence: %w", err)
	}

	return rc.client.Publish(ctx, "streaming:presence:updates", data).Err()
}

// SyncRoomState synchronizes room state.
func (rc *redisCoordinator) SyncRoomState(ctx context.Context, roomID string, state *RoomState) error {
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeRoomStateSync,
		RoomID:    roomID,
		Payload:   state,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(coordMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal room state: %w", err)
	}

	// Store state
	stateKey := "streaming:room:state:" + roomID

	stateData, _ := json.Marshal(state)
	if err := rc.client.Set(ctx, stateKey, stateData, time.Hour).Err(); err != nil {
		return err
	}

	// Publish update
	return rc.client.Publish(ctx, "streaming:state:rooms", data).Err()
}

// GetUserNodes returns nodes where user is connected.
func (rc *redisCoordinator) GetUserNodes(ctx context.Context, userID string) ([]string, error) {
	key := "streaming:user:nodes:" + userID

	members, err := rc.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get user nodes: %w", err)
	}

	return members, nil
}

// GetRoomNodes returns nodes serving room.
func (rc *redisCoordinator) GetRoomNodes(ctx context.Context, roomID string) ([]string, error) {
	key := "streaming:room:nodes:" + roomID

	members, err := rc.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get room nodes: %w", err)
	}

	return members, nil
}

// RegisterNode registers this node.
func (rc *redisCoordinator) RegisterNode(ctx context.Context, nodeID string, metadata map[string]any) error {
	// Store node info
	nodeKey := "streaming:node:" + nodeID

	nodeData, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	if err := rc.client.Set(ctx, nodeKey, nodeData, time.Minute).Err(); err != nil {
		return err
	}

	// Add to active nodes set
	if err := rc.client.SAdd(ctx, "streaming:nodes:active", nodeID).Err(); err != nil {
		return err
	}

	// Publish registration event
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeNodeRegister,
		NodeID:    nodeID,
		Payload:   metadata,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(coordMsg)

	return rc.client.Publish(ctx, "streaming:broadcast:global", data).Err()
}

// UnregisterNode unregisters this node.
func (rc *redisCoordinator) UnregisterNode(ctx context.Context, nodeID string) error {
	// Remove node info
	nodeKey := "streaming:node:" + nodeID
	if err := rc.client.Del(ctx, nodeKey).Err(); err != nil {
		return err
	}

	// Remove from active nodes set
	if err := rc.client.SRem(ctx, "streaming:nodes:active", nodeID).Err(); err != nil {
		return err
	}

	// Publish unregistration event
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeNodeUnregister,
		NodeID:    nodeID,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(coordMsg)

	return rc.client.Publish(ctx, "streaming:broadcast:global", data).Err()
}

// Subscribe subscribes to coordinator events.
func (rc *redisCoordinator) Subscribe(ctx context.Context, handler MessageHandler) error {
	rc.handler = handler

	return nil
}

func (rc *redisCoordinator) listen(ctx context.Context) {
	ch := rc.pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		case msg := <-ch:
			rc.handleMessage(ctx, msg)
		}
	}
}

func (rc *redisCoordinator) handleMessage(ctx context.Context, msg *redis.Message) {
	if rc.handler == nil {
		return
	}

	var coordMsg CoordinatorMessage
	if err := json.Unmarshal([]byte(msg.Payload), &coordMsg); err != nil {
		// Log error
		return
	}

	// Skip messages from this node
	if coordMsg.NodeID == rc.nodeID {
		return
	}

	// Call handler
	if err := rc.handler(ctx, &coordMsg); err != nil {
		// Log error
	}
}

// TrackUserNode tracks user connection to node.
func (rc *redisCoordinator) TrackUserNode(ctx context.Context, userID, nodeID string) error {
	key := "streaming:user:nodes:" + userID

	return rc.client.SAdd(ctx, key, nodeID).Err()
}

// UntrackUserNode removes user connection from node.
func (rc *redisCoordinator) UntrackUserNode(ctx context.Context, userID, nodeID string) error {
	key := "streaming:user:nodes:" + userID

	return rc.client.SRem(ctx, key, nodeID).Err()
}

// TrackRoomNode tracks room on node.
func (rc *redisCoordinator) TrackRoomNode(ctx context.Context, roomID, nodeID string) error {
	key := "streaming:room:nodes:" + roomID

	return rc.client.SAdd(ctx, key, nodeID).Err()
}

// UntrackRoomNode removes room from node.
func (rc *redisCoordinator) UntrackRoomNode(ctx context.Context, roomID, nodeID string) error {
	key := "streaming:room:nodes:" + roomID

	return rc.client.SRem(ctx, key, nodeID).Err()
}
