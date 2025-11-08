package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/internal/errors"
)

// DistributedBackend implements streaming.DistributedBackend with Redis pub/sub.
type DistributedBackend struct {
	client   *redis.Client
	pubsub   *redis.PubSub
	nodeID   string
	prefix   string
	handlers map[string]streaming.MessageHandler
}

// NewDistributedBackend creates a new Redis distributed backend.
func NewDistributedBackend(client *redis.Client, nodeID, prefix string) streaming.DistributedBackend {
	if prefix == "" {
		prefix = "streaming:distributed"
	}

	if nodeID == "" {
		nodeID = uuid.New().String()
	}

	return &DistributedBackend{
		client:   client,
		nodeID:   nodeID,
		prefix:   prefix,
		handlers: make(map[string]streaming.MessageHandler),
	}
}

func (d *DistributedBackend) RegisterNode(ctx context.Context, nodeID string, metadata map[string]any) error {
	nodeKey := fmt.Sprintf("%s:nodes:%s", d.prefix, nodeID)

	nodeData := map[string]any{
		"id":         nodeID,
		"registered": time.Now().Unix(),
		"metadata":   metadata,
	}

	dataJSON, err := json.Marshal(nodeData)
	if err != nil {
		return err
	}

	// Store with TTL for auto-cleanup of dead nodes
	return d.client.Set(ctx, nodeKey, dataJSON, 1*time.Minute).Err()
}

func (d *DistributedBackend) UnregisterNode(ctx context.Context, nodeID string) error {
	nodeKey := fmt.Sprintf("%s:nodes:%s", d.prefix, nodeID)

	return d.client.Del(ctx, nodeKey).Err()
}

func (d *DistributedBackend) GetNodes(ctx context.Context) ([]streaming.NodeInfo, error) {
	pattern := d.prefix + ":nodes:*"

	var cursor uint64

	nodes := make([]streaming.NodeInfo, 0)

	for {
		keys, nextCursor, err := d.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			data, err := d.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var nodeData map[string]any
			if err := json.Unmarshal([]byte(data), &nodeData); err != nil {
				continue
			}

			node := streaming.NodeInfo{
				ID:       nodeData["id"].(string),
				Started:  time.Unix(int64(nodeData["registered"].(float64)), 0),
				LastSeen: time.Now(), // Active since we just read it
				Metadata: getMap(nodeData, "metadata"),
			}

			nodes = append(nodes, node)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nodes, nil
}

func (d *DistributedBackend) GetNode(ctx context.Context, nodeID string) (*streaming.NodeInfo, error) {
	nodeKey := fmt.Sprintf("%s:nodes:%s", d.prefix, nodeID)

	data, err := d.client.Get(ctx, nodeKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, streaming.ErrNodeNotFound
	}

	if err != nil {
		return nil, err
	}

	var nodeData map[string]any
	if err := json.Unmarshal([]byte(data), &nodeData); err != nil {
		return nil, err
	}

	node := &streaming.NodeInfo{
		ID:       nodeData["id"].(string),
		Started:  time.Unix(int64(nodeData["registered"].(float64)), 0),
		LastSeen: time.Now(),
		Metadata: getMap(nodeData, "metadata"),
	}

	return node, nil
}

func (d *DistributedBackend) Heartbeat(ctx context.Context, nodeID string) error {
	nodeKey := fmt.Sprintf("%s:nodes:%s", d.prefix, nodeID)

	// Refresh TTL
	return d.client.Expire(ctx, nodeKey, 1*time.Minute).Err()
}

func (d *DistributedBackend) Publish(ctx context.Context, channel string, message *streaming.Message) error {
	channelKey := fmt.Sprintf("%s:channels:%s", d.prefix, channel)

	dataJSON, err := json.Marshal(message)
	if err != nil {
		return err
	}

	return d.client.Publish(ctx, channelKey, dataJSON).Err()
}

func (d *DistributedBackend) Subscribe(ctx context.Context, channel string, handler streaming.MessageHandler) error {
	channelKey := fmt.Sprintf("%s:channels:%s", d.prefix, channel)

	// Store handler
	d.handlers[channel] = handler

	// Subscribe to channel
	if d.pubsub == nil {
		d.pubsub = d.client.Subscribe(ctx, channelKey)
	} else {
		if err := d.pubsub.Subscribe(ctx, channelKey); err != nil {
			return err
		}
	}

	// Start receiving messages in background
	go d.receiveMessages(ctx)

	return nil
}

func (d *DistributedBackend) Unsubscribe(ctx context.Context, channel string) error {
	channelKey := fmt.Sprintf("%s:channels:%s", d.prefix, channel)

	delete(d.handlers, channel)

	if d.pubsub != nil {
		return d.pubsub.Unsubscribe(ctx, channelKey)
	}

	return nil
}

func (d *DistributedBackend) SetPresence(ctx context.Context, userID, status string, ttl time.Duration) error {
	presenceKey := fmt.Sprintf("%s:presence:%s", d.prefix, userID)

	presenceData := map[string]any{
		"user_id": userID,
		"status":  status,
		"updated": time.Now().Unix(),
	}

	dataJSON, err := json.Marshal(presenceData)
	if err != nil {
		return err
	}

	return d.client.Set(ctx, presenceKey, dataJSON, ttl).Err()
}

func (d *DistributedBackend) GetPresence(ctx context.Context, userID string) (*streaming.UserPresence, error) {
	presenceKey := fmt.Sprintf("%s:presence:%s", d.prefix, userID)

	data, err := d.client.Get(ctx, presenceKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, streaming.ErrPresenceNotFound
	}

	if err != nil {
		return nil, err
	}

	var presenceData map[string]any
	if err := json.Unmarshal([]byte(data), &presenceData); err != nil {
		return nil, err
	}

	return &streaming.UserPresence{
		UserID:   presenceData["user_id"].(string),
		Status:   presenceData["status"].(string),
		LastSeen: time.Unix(int64(presenceData["updated"].(float64)), 0),
	}, nil
}

func (d *DistributedBackend) GetOnlineUsers(ctx context.Context) ([]string, error) {
	pattern := d.prefix + ":presence:*"

	var cursor uint64

	users := make([]string, 0)

	for {
		keys, nextCursor, err := d.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			data, err := d.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var presenceData map[string]any
			if err := json.Unmarshal([]byte(data), &presenceData); err != nil {
				continue
			}

			if presenceData["status"].(string) == streaming.StatusOnline {
				users = append(users, presenceData["user_id"].(string))
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return users, nil
}

func (d *DistributedBackend) AcquireLock(ctx context.Context, key string, ttl time.Duration) (streaming.Lock, error) {
	lockKey := fmt.Sprintf("%s:locks:%s", d.prefix, key)
	lockID := uuid.New().String()

	// Try to acquire lock with NX (only if not exists)
	success, err := d.client.SetNX(ctx, lockKey, lockID, ttl).Result()
	if err != nil {
		return nil, err
	}

	if !success {
		return nil, streaming.ErrLockAcquisitionFailed
	}

	return &redisLock{
		client: d.client,
		key:    lockKey,
		lockID: lockID,
		expiry: time.Now().Add(ttl),
		held:   true,
	}, nil
}

func (d *DistributedBackend) ReleaseLock(ctx context.Context, lock streaming.Lock) error {
	redisLock, ok := lock.(*redisLock)
	if !ok {
		return errors.New("invalid lock type")
	}

	// Only release if we still hold it
	if !redisLock.IsHeld() {
		return streaming.ErrLockNotHeld
	}

	// Verify it's our lock before deleting
	current, err := d.client.Get(ctx, redisLock.key).Result()
	if err != nil {
		return err
	}

	if current != redisLock.lockID {
		return streaming.ErrLockNotHeld
	}

	redisLock.held = false

	return d.client.Del(ctx, redisLock.key).Err()
}

func (d *DistributedBackend) Increment(ctx context.Context, key string) (int64, error) {
	counterKey := fmt.Sprintf("%s:counters:%s", d.prefix, key)

	return d.client.Incr(ctx, counterKey).Result()
}

func (d *DistributedBackend) Decrement(ctx context.Context, key string) (int64, error) {
	counterKey := fmt.Sprintf("%s:counters:%s", d.prefix, key)

	return d.client.Decr(ctx, counterKey).Result()
}

func (d *DistributedBackend) GetCounter(ctx context.Context, key string) (int64, error) {
	counterKey := fmt.Sprintf("%s:counters:%s", d.prefix, key)

	val, err := d.client.Get(ctx, counterKey).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}

	return val, err
}

func (d *DistributedBackend) DiscoverNodes(ctx context.Context) ([]streaming.NodeInfo, error) {
	return d.GetNodes(ctx)
}

func (d *DistributedBackend) WatchNodes(ctx context.Context, handler streaming.NodeChangeHandler) error {
	// Use pub/sub for node change notifications
	channelKey := d.prefix + ":node_events"

	if d.pubsub == nil {
		d.pubsub = d.client.Subscribe(ctx, channelKey)
	} else {
		if err := d.pubsub.Subscribe(ctx, channelKey); err != nil {
			return err
		}
	}

	// Receive node events
	go func() {
		ch := d.pubsub.Channel()
		for msg := range ch {
			var event streaming.NodeChangeEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}

			handler(event)
		}
	}()

	return nil
}

func (d *DistributedBackend) Ping(ctx context.Context) error {
	return d.client.Ping(ctx).Err()
}

func (d *DistributedBackend) Connect(ctx context.Context) error {
	return d.client.Ping(ctx).Err()
}

func (d *DistributedBackend) Disconnect(ctx context.Context) error {
	if d.pubsub != nil {
		return d.pubsub.Close()
	}

	return nil
}

func (d *DistributedBackend) receiveMessages(ctx context.Context) {
	if d.pubsub == nil {
		return
	}

	ch := d.pubsub.Channel()
	for msg := range ch {
		// Extract channel name from full key
		channelName := msg.Channel
		if len(channelName) > len(d.prefix+":channels:") {
			channelName = channelName[len(d.prefix+":channels:"):]
		}

		// Find handler
		handler, exists := d.handlers[channelName]
		if !exists {
			continue
		}

		// Parse message
		var streamMsg streaming.Message
		if err := json.Unmarshal([]byte(msg.Payload), &streamMsg); err != nil {
			continue
		}

		// Call handler
		_ = handler(ctx, &streamMsg)
	}
}

// redisLock implements streaming.Lock.
type redisLock struct {
	client *redis.Client
	key    string
	lockID string
	expiry time.Time
	held   bool
}

func (l *redisLock) GetKey() string       { return l.key }
func (l *redisLock) GetOwner() string     { return l.lockID }
func (l *redisLock) GetExpiry() time.Time { return l.expiry }
func (l *redisLock) IsHeld() bool         { return l.held && time.Now().Before(l.expiry) }

func (l *redisLock) Renew(ctx context.Context, ttl time.Duration) error {
	if !l.held {
		return streaming.ErrLockNotHeld
	}

	// Verify we still own the lock
	current, err := l.client.Get(ctx, l.key).Result()
	if err != nil {
		return err
	}

	if current != l.lockID {
		l.held = false

		return streaming.ErrLockNotHeld
	}

	// Extend TTL
	if err := l.client.Expire(ctx, l.key, ttl).Err(); err != nil {
		return err
	}

	l.expiry = time.Now().Add(ttl)

	return nil
}
