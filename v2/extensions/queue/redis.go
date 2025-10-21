package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/v2"
)

// RedisQueue implements Queue interface using Redis Streams
type RedisQueue struct {
	config    Config
	logger    forge.Logger
	metrics   forge.Metrics
	client    *redis.Client
	connected bool
	consumers map[string]*redisConsumer
	mu        sync.RWMutex
	startTime time.Time
}

type redisConsumer struct {
	queue   string
	handler MessageHandler
	opts    ConsumeOptions
	cancel  context.CancelFunc
	active  bool
}

// NewRedisQueue creates a new Redis-backed queue instance
func NewRedisQueue(config Config, logger forge.Logger, metrics forge.Metrics) (*RedisQueue, error) {
	if config.URL == "" && len(config.Hosts) == 0 {
		return nil, fmt.Errorf("redis requires URL or hosts")
	}

	return &RedisQueue{
		config:    config,
		logger:    logger,
		metrics:   metrics,
		consumers: make(map[string]*redisConsumer),
	}, nil
}

func (q *RedisQueue) Connect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.connected {
		return ErrAlreadyConnected
	}

	// Parse Redis URL or use first host
	addr := q.config.URL
	if addr == "" && len(q.config.Hosts) > 0 {
		addr = q.config.Hosts[0]
	}

	opts := &redis.Options{
		Addr:         addr,
		Password:     q.config.Password,
		DB:           0, // Use default DB
		MaxRetries:   q.config.MaxRetries,
		DialTimeout:  q.config.ConnectTimeout,
		ReadTimeout:  q.config.ReadTimeout,
		WriteTimeout: q.config.WriteTimeout,
		PoolSize:     q.config.MaxConnections,
		MinIdleConns: q.config.MaxIdleConnections,
	}

	if q.config.Username != "" {
		opts.Username = q.config.Username
	}

	q.client = redis.NewClient(opts)

	// Test connection
	if err := q.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}

	q.connected = true
	q.startTime = time.Now()
	q.logger.Info("connected to redis queue", forge.F("addr", addr))

	return nil
}

func (q *RedisQueue) Disconnect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Stop all consumers
	for _, c := range q.consumers {
		if c.cancel != nil {
			c.cancel()
		}
	}

	if q.client != nil {
		if err := q.client.Close(); err != nil {
			return fmt.Errorf("failed to close redis connection: %w", err)
		}
	}

	q.connected = false
	q.consumers = make(map[string]*redisConsumer)
	q.logger.Info("disconnected from redis queue")

	return nil
}

func (q *RedisQueue) Ping(ctx context.Context) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	return q.client.Ping(ctx).Err()
}

func (q *RedisQueue) DeclareQueue(ctx context.Context, name string, opts QueueOptions) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Store queue metadata
	key := fmt.Sprintf("queue:%s:meta", name)
	meta := map[string]interface{}{
		"name":       name,
		"durable":    opts.Durable,
		"created_at": time.Now().Unix(),
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal queue metadata: %w", err)
	}

	// Check if queue already exists
	exists, err := q.client.Exists(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to check queue existence: %w", err)
	}
	if exists > 0 {
		return ErrQueueAlreadyExists
	}

	if err := q.client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// Create the stream
	streamKey := fmt.Sprintf("queue:%s:stream", name)
	q.client.XGroupCreateMkStream(ctx, streamKey, "consumers", "$")

	q.logger.Info("declared redis queue", forge.F("queue", name))
	return nil
}

func (q *RedisQueue) DeleteQueue(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Delete queue metadata and stream
	keys := []string{
		fmt.Sprintf("queue:%s:meta", name),
		fmt.Sprintf("queue:%s:stream", name),
		fmt.Sprintf("queue:%s:dlq", name),
	}

	if err := q.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	q.logger.Info("deleted redis queue", forge.F("queue", name))
	return nil
}

func (q *RedisQueue) ListQueues(ctx context.Context) ([]string, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	keys, err := q.client.Keys(ctx, "queue:*:meta").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %w", err)
	}

	queues := make([]string, 0, len(keys))
	for _, key := range keys {
		// Extract queue name from key: queue:<name>:meta
		name := key[6 : len(key)-5] // Remove "queue:" prefix and ":meta" suffix
		queues = append(queues, name)
	}

	return queues, nil
}

func (q *RedisQueue) GetQueueInfo(ctx context.Context, name string) (*QueueInfo, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	// Get queue metadata
	metaKey := fmt.Sprintf("queue:%s:meta", name)
	data, err := q.client.Get(ctx, metaKey).Result()
	if err == redis.Nil {
		return nil, ErrQueueNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get queue info: %w", err)
	}

	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(data), &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queue metadata: %w", err)
	}

	// Get stream length
	streamKey := fmt.Sprintf("queue:%s:stream", name)
	length, err := q.client.XLen(ctx, streamKey).Result()
	if err != nil {
		length = 0
	}

	return &QueueInfo{
		Name:       name,
		Messages:   length,
		Consumers:  len(q.consumers),
		Durable:    meta["durable"].(bool),
		AutoDelete: false,
		CreatedAt:  time.Unix(int64(meta["created_at"].(float64)), 0),
	}, nil
}

func (q *RedisQueue) PurgeQueue(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	streamKey := fmt.Sprintf("queue:%s:stream", name)

	// Delete and recreate the stream
	if err := q.client.Del(ctx, streamKey).Err(); err != nil {
		return fmt.Errorf("failed to purge queue: %w", err)
	}

	// Recreate consumer group
	q.client.XGroupCreateMkStream(ctx, streamKey, "consumers", "$")

	q.logger.Info("purged redis queue", forge.F("queue", name))
	return nil
}

func (q *RedisQueue) Publish(ctx context.Context, queueName string, message Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	streamKey := fmt.Sprintf("queue:%s:stream", queueName)

	// Serialize message
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Add to stream
	args := &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"data":      data,
			"timestamp": time.Now().Unix(),
		},
	}

	if _, err := q.client.XAdd(ctx, args).Result(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	q.logger.Debug("published message to redis",
		forge.F("queue", queueName),
		forge.F("size", len(data)),
	)

	return nil
}

func (q *RedisQueue) PublishBatch(ctx context.Context, queueName string, messages []Message) error {
	q.mu.RLock()
	connected := q.connected
	q.mu.RUnlock()

	if !connected {
		return ErrNotConnected
	}

	for _, msg := range messages {
		if err := q.Publish(ctx, queueName, msg); err != nil {
			return err
		}
	}
	return nil
}

func (q *RedisQueue) PublishDelayed(ctx context.Context, queueName string, message Message, delay time.Duration) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	// Use Redis sorted set for delayed messages
	delayedKey := fmt.Sprintf("queue:%s:delayed", queueName)

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	score := float64(time.Now().Add(delay).Unix())

	if err := q.client.ZAdd(ctx, delayedKey, redis.Z{
		Score:  score,
		Member: data,
	}).Err(); err != nil {
		return fmt.Errorf("failed to publish delayed message: %w", err)
	}

	return nil
}

func (q *RedisQueue) Consume(ctx context.Context, queueName string, handler MessageHandler, opts ConsumeOptions) error {
	q.mu.Lock()

	if !q.connected {
		q.mu.Unlock()
		return ErrNotConnected
	}

	streamKey := fmt.Sprintf("queue:%s:stream", queueName)
	consumerID := fmt.Sprintf("consumer-%d", time.Now().UnixNano())

	consumerCtx, cancel := context.WithCancel(ctx)
	q.consumers[consumerID] = &redisConsumer{
		queue:   queueName,
		handler: handler,
		opts:    opts,
		cancel:  cancel,
		active:  true,
	}
	q.mu.Unlock()

	go func() {
		for {
			select {
			case <-consumerCtx.Done():
				return
			default:
				// Read from stream
				results, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
					Group:    "consumers",
					Consumer: consumerID,
					Streams:  []string{streamKey, ">"},
					Count:    int64(opts.PrefetchCount),
					Block:    time.Second,
				}).Result()

				if err != nil && err != redis.Nil {
					q.logger.Error("failed to read from stream",
						forge.F("queue", queueName),
						forge.F("error", err),
					)
					time.Sleep(time.Second)
					continue
				}

				for _, result := range results {
					for _, message := range result.Messages {
						// Parse message
						dataStr, ok := message.Values["data"].(string)
						if !ok {
							continue
						}

						var msg Message
						if err := json.Unmarshal([]byte(dataStr), &msg); err != nil {
							q.logger.Error("failed to unmarshal message",
								forge.F("error", err),
							)
							continue
						}

						msg.ID = message.ID

						// Handle message
						if err := handler(consumerCtx, msg); err != nil {
							q.logger.Error("message handler failed",
								forge.F("queue", queueName),
								forge.F("msg_id", msg.ID),
								forge.F("error", err),
							)

							// Move to DLQ
							if q.config.EnableDeadLetter {
								dlqKey := fmt.Sprintf("queue:%s:dlq", queueName)
								q.client.RPush(ctx, dlqKey, dataStr)
							}
						}

						// Acknowledge message
						q.client.XAck(ctx, streamKey, "consumers", message.ID)
					}
				}
			}
		}
	}()

	q.logger.Info("started redis consumer", forge.F("queue", queueName))
	return nil
}

func (q *RedisQueue) StopConsuming(ctx context.Context, queueName string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.connected {
		return ErrNotConnected
	}

	for id, c := range q.consumers {
		if c.queue == queueName && c.active {
			if c.cancel != nil {
				c.cancel()
			}
			c.active = false
			delete(q.consumers, id)
		}
	}

	q.logger.Info("stopped redis consumer", forge.F("queue", queueName))
	return nil
}

func (q *RedisQueue) Ack(ctx context.Context, messageID string) error {
	// Acknowledged in the consumer loop
	return nil
}

func (q *RedisQueue) Nack(ctx context.Context, messageID string, requeue bool) error {
	// Handle in consumer loop
	return nil
}

func (q *RedisQueue) Reject(ctx context.Context, messageID string) error {
	// Handle in consumer loop
	return nil
}

func (q *RedisQueue) GetDeadLetterQueue(ctx context.Context, queueName string) ([]Message, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	dlqKey := fmt.Sprintf("queue:%s:dlq", queueName)
	items, err := q.client.LRange(ctx, dlqKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get DLQ: %w", err)
	}

	messages := make([]Message, 0, len(items))
	for _, item := range items {
		var msg Message
		if err := json.Unmarshal([]byte(item), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (q *RedisQueue) RequeueDeadLetter(ctx context.Context, queueName string, messageID string) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return ErrNotConnected
	}

	// For Redis, we'll need to move from DLQ back to stream
	// This is simplified - production would need better tracking
	return nil
}

func (q *RedisQueue) Stats(ctx context.Context) (*QueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if !q.connected {
		return nil, ErrNotConnected
	}

	// Get Redis info
	info, err := q.client.Info(ctx, "stats").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	queues, _ := q.ListQueues(ctx)

	var totalMessages int64
	for _, queueName := range queues {
		streamKey := fmt.Sprintf("queue:%s:stream", queueName)
		length, _ := q.client.XLen(ctx, streamKey).Result()
		totalMessages += length
	}

	uptime := time.Duration(0)
	if !q.startTime.IsZero() {
		uptime = time.Since(q.startTime)
	}

	return &QueueStats{
		QueueCount:      int64(len(queues)),
		TotalMessages:   totalMessages,
		TotalConsumers:  len(q.consumers),
		Uptime:          uptime,
		Version:         "redis-7.0.0",
		ConnectionCount: 1,
		Extra: map[string]interface{}{
			"info": info,
		},
	}, nil
}
