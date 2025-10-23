package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// natsCoordinator implements StreamCoordinator using NATS JetStream.
type natsCoordinator struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	nodeID  string
	handler MessageHandler
	subs    []*nats.Subscription
	stopCh  chan struct{}
	running bool
}

// NewNATSCoordinator creates a NATS-based coordinator.
func NewNATSCoordinator(conn *nats.Conn, nodeID string) (StreamCoordinator, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	// Create streams if they don't exist
	if err := setupNATSStreams(js); err != nil {
		return nil, err
	}

	return &natsCoordinator{
		conn:   conn,
		js:     js,
		nodeID: nodeID,
		stopCh: make(chan struct{}),
		subs:   make([]*nats.Subscription, 0),
	}, nil
}

func setupNATSStreams(js nats.JetStreamContext) error {
	// Broadcast stream
	_, err := js.AddStream(&nats.StreamConfig{
		Name:     "STREAMING_BROADCAST",
		Subjects: []string{"streaming.broadcast.>"},
		MaxAge:   time.Hour,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		return fmt.Errorf("failed to create broadcast stream: %w", err)
	}

	// Presence stream
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "STREAMING_PRESENCE",
		Subjects: []string{"streaming.presence.>"},
		MaxAge:   time.Minute * 30,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		return fmt.Errorf("failed to create presence stream: %w", err)
	}

	// State stream
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "STREAMING_STATE",
		Subjects: []string{"streaming.state.>"},
		MaxAge:   time.Hour * 24,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		return fmt.Errorf("failed to create state stream: %w", err)
	}

	return nil
}

// Start starts the coordinator.
func (nc *natsCoordinator) Start(ctx context.Context) error {
	if nc.running {
		return fmt.Errorf("coordinator already running")
	}

	// Subscribe to global broadcasts
	sub1, err := nc.js.QueueSubscribe(
		"streaming.broadcast.global",
		"streaming-workers",
		nc.messageHandler,
		nats.ManualAck(),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to global broadcasts: %w", err)
	}
	nc.subs = append(nc.subs, sub1)

	// Subscribe to node-specific broadcasts
	sub2, err := nc.js.QueueSubscribe(
		fmt.Sprintf("streaming.broadcast.node.%s", nc.nodeID),
		"streaming-workers",
		nc.messageHandler,
		nats.ManualAck(),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to node broadcasts: %w", err)
	}
	nc.subs = append(nc.subs, sub2)

	// Subscribe to presence updates
	sub3, err := nc.js.QueueSubscribe(
		"streaming.presence.*",
		"streaming-workers",
		nc.messageHandler,
		nats.ManualAck(),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to presence updates: %w", err)
	}
	nc.subs = append(nc.subs, sub3)

	// Subscribe to state updates
	sub4, err := nc.js.QueueSubscribe(
		"streaming.state.rooms.*",
		"streaming-workers",
		nc.messageHandler,
		nats.ManualAck(),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to state updates: %w", err)
	}
	nc.subs = append(nc.subs, sub4)

	nc.running = true
	return nil
}

// Stop stops the coordinator.
func (nc *natsCoordinator) Stop(ctx context.Context) error {
	if !nc.running {
		return nil
	}

	close(nc.stopCh)

	// Unsubscribe from all subjects
	for _, sub := range nc.subs {
		if err := sub.Unsubscribe(); err != nil {
			// Log error but continue
		}
	}
	nc.subs = nil

	nc.running = false
	return nil
}

// BroadcastToNode sends message to specific node.
func (nc *natsCoordinator) BroadcastToNode(ctx context.Context, nodeID string, msg *streaming.Message) error {
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

	subject := fmt.Sprintf("streaming.broadcast.node.%s", nodeID)
	_, err = nc.js.Publish(subject, data)
	return err
}

// BroadcastToUser sends to user across all nodes.
func (nc *natsCoordinator) BroadcastToUser(ctx context.Context, userID string, msg *streaming.Message) error {
	// Get nodes where user is connected
	nodes, err := nc.GetUserNodes(ctx, userID)
	if err != nil {
		return err
	}

	// Broadcast to each node
	for _, nodeID := range nodes {
		if err := nc.BroadcastToNode(ctx, nodeID, msg); err != nil {
			// Log error but continue
			continue
		}
	}

	return nil
}

// BroadcastToRoom sends to room across all nodes.
func (nc *natsCoordinator) BroadcastToRoom(ctx context.Context, roomID string, msg *streaming.Message) error {
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

	subject := fmt.Sprintf("streaming.broadcast.room.%s", roomID)
	_, err = nc.js.Publish(subject, data)
	return err
}

// BroadcastGlobal sends to all nodes.
func (nc *natsCoordinator) BroadcastGlobal(ctx context.Context, msg *streaming.Message) error {
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeBroadcast,
		Payload:   msg,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(coordMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	_, err = nc.js.Publish("streaming.broadcast.global", data)
	return err
}

// SyncPresence synchronizes presence across nodes.
func (nc *natsCoordinator) SyncPresence(ctx context.Context, presence *streaming.UserPresence) error {
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

	subject := fmt.Sprintf("streaming.presence.%s", presence.UserID)
	_, err = nc.js.Publish(subject, data)
	return err
}

// SyncRoomState synchronizes room state.
func (nc *natsCoordinator) SyncRoomState(ctx context.Context, roomID string, state *RoomState) error {
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

	// Store in KV store
	kv, err := nc.js.KeyValue("streaming_room_state")
	if err != nil {
		// Create KV bucket if it doesn't exist
		kv, err = nc.js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket: "streaming_room_state",
			TTL:    time.Hour,
		})
		if err != nil {
			return fmt.Errorf("failed to access KV store: %w", err)
		}
	}

	stateData, _ := json.Marshal(state)
	if _, err := kv.Put(roomID, stateData); err != nil {
		return fmt.Errorf("failed to store room state: %w", err)
	}

	// Publish update
	subject := fmt.Sprintf("streaming.state.rooms.%s", roomID)
	_, err = nc.js.Publish(subject, data)
	return err
}

// GetUserNodes returns nodes where user is connected.
func (nc *natsCoordinator) GetUserNodes(ctx context.Context, userID string) ([]string, error) {
	kv, err := nc.js.KeyValue("streaming_user_nodes")
	if err != nil {
		return nil, fmt.Errorf("failed to access KV store: %w", err)
	}

	entry, err := kv.Get(userID)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return []string{}, nil
		}
		return nil, err
	}

	var nodes []string
	if err := json.Unmarshal(entry.Value(), &nodes); err != nil {
		return nil, err
	}

	return nodes, nil
}

// GetRoomNodes returns nodes serving room.
func (nc *natsCoordinator) GetRoomNodes(ctx context.Context, roomID string) ([]string, error) {
	kv, err := nc.js.KeyValue("streaming_room_nodes")
	if err != nil {
		return nil, fmt.Errorf("failed to access KV store: %w", err)
	}

	entry, err := kv.Get(roomID)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return []string{}, nil
		}
		return nil, err
	}

	var nodes []string
	if err := json.Unmarshal(entry.Value(), &nodes); err != nil {
		return nil, err
	}

	return nodes, nil
}

// RegisterNode registers this node.
func (nc *natsCoordinator) RegisterNode(ctx context.Context, nodeID string, metadata map[string]any) error {
	// Store node info in KV
	kv, err := nc.js.KeyValue("streaming_nodes")
	if err != nil {
		kv, err = nc.js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket: "streaming_nodes",
			TTL:    time.Minute,
		})
		if err != nil {
			return fmt.Errorf("failed to access nodes KV: %w", err)
		}
	}

	nodeData, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	if _, err := kv.Put(nodeID, nodeData); err != nil {
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
	_, err = nc.js.Publish("streaming.broadcast.global", data)
	return err
}

// UnregisterNode unregisters this node.
func (nc *natsCoordinator) UnregisterNode(ctx context.Context, nodeID string) error {
	// Remove from KV
	kv, err := nc.js.KeyValue("streaming_nodes")
	if err != nil {
		return err
	}

	if err := kv.Delete(nodeID); err != nil {
		return err
	}

	// Publish unregistration event
	coordMsg := &CoordinatorMessage{
		Type:      MessageTypeNodeUnregister,
		NodeID:    nodeID,
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(coordMsg)
	_, err = nc.js.Publish("streaming.broadcast.global", data)
	return err
}

// Subscribe subscribes to coordinator events.
func (nc *natsCoordinator) Subscribe(ctx context.Context, handler MessageHandler) error {
	nc.handler = handler
	return nil
}

func (nc *natsCoordinator) messageHandler(msg *nats.Msg) {
	if nc.handler == nil {
		msg.Ack()
		return
	}

	var coordMsg CoordinatorMessage
	if err := json.Unmarshal(msg.Data, &coordMsg); err != nil {
		msg.Nak()
		return
	}

	// Skip messages from this node
	if coordMsg.NodeID == nc.nodeID {
		msg.Ack()
		return
	}

	// Call handler
	ctx := context.Background()
	if err := nc.handler(ctx, &coordMsg); err != nil {
		msg.Nak()
		return
	}

	msg.Ack()
}
