package invalidation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RedisInvalidation provides Redis-based cache invalidation
type RedisInvalidation struct {
	client       RedisClient
	pubsubClient RedisClient
	keyPrefix    string
	channel      string
	logger       common.Logger
	metrics      common.Metrics
	config       RedisInvalidationConfig
	subscribers  map[string]cachecore.InvalidationCallback
	started      bool
	shutdown     chan struct{}
	wg           sync.WaitGroup
	mu           sync.RWMutex
}

// RedisClient interface for Redis operations
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	Keys(ctx context.Context, pattern string) ([]string, error)
	Publish(ctx context.Context, channel string, message interface{}) error
	Subscribe(ctx context.Context, channels ...string) PubSub
	Ping(ctx context.Context) error
	Close() error
}

// PubSub interface for Redis pub/sub operations
type PubSub interface {
	Channel() <-chan *Message
	Subscribe(ctx context.Context, channels ...string) error
	Unsubscribe(ctx context.Context, channels ...string) error
	Close() error
}

// Message represents a Redis pub/sub message
type Message struct {
	Channel string
	Payload string
}

// RedisInvalidationConfig contains configuration for Redis invalidation
type RedisInvalidationConfig struct {
	Addresses      []string      `yaml:"addresses" json:"addresses"`
	Password       string        `yaml:"password" json:"password"`
	Database       int           `yaml:"database" json:"database"`
	KeyPrefix      string        `yaml:"key_prefix" json:"key_prefix"`
	Channel        string        `yaml:"channel" json:"channel"`
	PoolSize       int           `yaml:"pool_size" json:"pool_size"`
	DialTimeout    time.Duration `yaml:"dial_timeout" json:"dial_timeout"`
	ReadTimeout    time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout" json:"write_timeout"`
	MaxRetries     int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay     time.Duration `yaml:"retry_delay" json:"retry_delay"`
	EnableCluster  bool          `yaml:"enable_cluster" json:"enable_cluster"`
	EnableSentinel bool          `yaml:"enable_sentinel" json:"enable_sentinel"`
	SentinelAddrs  []string      `yaml:"sentinel_addrs" json:"sentinel_addrs"`
	MasterName     string        `yaml:"master_name" json:"master_name"`
}

// RedisInvalidationStats contains Redis invalidation statistics
type RedisInvalidationStats struct {
	EventsPublished   int64         `json:"events_published"`
	EventsReceived    int64         `json:"events_received"`
	PublishErrors     int64         `json:"publish_errors"`
	ReceiveErrors     int64         `json:"receive_errors"`
	AverageLatency    time.Duration `json:"average_latency"`
	LastEvent         time.Time     `json:"last_event"`
	Subscribers       int           `json:"subscribers"`
	RedisConnections  int           `json:"redis_connections"`
	MessagesInChannel int64         `json:"messages_in_channel"`
}

// NewRedisInvalidation creates a new Redis invalidation manager
func NewRedisInvalidation(config RedisInvalidationConfig, client, pubsubClient RedisClient, logger common.Logger, metrics common.Metrics) *RedisInvalidation {
	keyPrefix := config.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = "forge:cache:invalidation"
	}

	channel := config.Channel
	if channel == "" {
		channel = "forge:cache:invalidation:events"
	}

	return &RedisInvalidation{
		client:       client,
		pubsubClient: pubsubClient,
		keyPrefix:    keyPrefix,
		channel:      channel,
		logger:       logger,
		metrics:      metrics,
		config:       config,
		subscribers:  make(map[string]cachecore.InvalidationCallback),
		shutdown:     make(chan struct{}),
	}
}

// Start starts the Redis invalidation manager
func (ri *RedisInvalidation) Start(ctx context.Context) error {
	ri.mu.Lock()
	defer ri.mu.Unlock()

	if ri.started {
		return fmt.Errorf("Redis invalidation already started")
	}

	// Test Redis connection
	if err := ri.client.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	// OnStart pub/sub listener
	ri.wg.Add(1)
	go ri.pubsubListener(ctx)

	ri.started = true

	ri.logger.Info("Redis invalidation started",
		logger.String("channel", ri.channel),
		logger.String("key_prefix", ri.keyPrefix),
	)

	if ri.metrics != nil {
		ri.metrics.Counter("forge.cache.invalidation.redis.started").Inc()
	}

	return nil
}

// Stop stops the Redis invalidation manager
func (ri *RedisInvalidation) Stop(ctx context.Context) error {
	ri.mu.Lock()
	defer ri.mu.Unlock()

	if !ri.started {
		return fmt.Errorf("Redis invalidation not started")
	}

	ri.logger.Info("stopping Redis invalidation")

	// Signal shutdown
	close(ri.shutdown)

	// Wait for goroutines to finish
	ri.wg.Wait()

	// Close Redis connections
	if err := ri.client.Close(); err != nil {
		ri.logger.Error("failed to close Redis client", logger.Error(err))
	}

	if err := ri.pubsubClient.Close(); err != nil {
		ri.logger.Error("failed to close Redis pub/sub client", logger.Error(err))
	}

	ri.started = false

	ri.logger.Info("Redis invalidation stopped")

	if ri.metrics != nil {
		ri.metrics.Counter("forge.cache.invalidation.redis.stopped").Inc()
	}

	return nil
}

// PublishInvalidation publishes an invalidation event to Redis
func (ri *RedisInvalidation) PublishInvalidation(ctx context.Context, event cachecore.InvalidationEvent) error {
	start := time.Now()

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal invalidation event: %w", err)
	}

	// Publish to Redis channel
	if err := ri.client.Publish(ctx, ri.channel, string(data)); err != nil {
		if ri.metrics != nil {
			ri.metrics.Counter("forge.cache.invalidation.redis.publish.failed").Inc()
		}
		return fmt.Errorf("failed to publish invalidation event: %w", err)
	}

	// Store invalidation metadata
	if err := ri.storeInvalidationMetadata(ctx, event); err != nil {
		ri.logger.Warn("failed to store invalidation metadata",
			logger.String("event_type", string(event.Type)),
			logger.Error(err),
		)
	}

	if ri.metrics != nil {
		ri.metrics.Counter("forge.cache.invalidation.redis.published").Inc()
		ri.metrics.Histogram("forge.cache.invalidation.redis.publish.latency").Observe(time.Since(start).Seconds())
	}

	return nil
}

// PublishBatchInvalidation publishes multiple invalidation events
func (ri *RedisInvalidation) PublishBatchInvalidation(ctx context.Context, events []cachecore.InvalidationEvent) error {
	start := time.Now()

	for _, event := range events {
		if err := ri.PublishInvalidation(ctx, event); err != nil {
			return err
		}
	}

	if ri.metrics != nil {
		ri.metrics.Counter("forge.cache.invalidation.redis.batch.published").Inc()
		ri.metrics.Histogram("forge.cache.invalidation.redis.batch.latency").Observe(time.Since(start).Seconds())
	}

	return nil
}

// SubscribeToInvalidations subscribes to invalidation events
func (ri *RedisInvalidation) SubscribeToInvalidations(id string, callback cachecore.InvalidationCallback) error {
	ri.mu.Lock()
	defer ri.mu.Unlock()

	ri.subscribers[id] = callback

	ri.logger.Info("invalidation subscriber registered",
		logger.String("subscriber_id", id),
	)

	if ri.metrics != nil {
		ri.metrics.Counter("forge.cache.invalidation.redis.subscribers").Inc()
	}

	return nil
}

// UnsubscribeFromInvalidations unsubscribes from invalidation events
func (ri *RedisInvalidation) UnsubscribeFromInvalidations(id string) error {
	ri.mu.Lock()
	defer ri.mu.Unlock()

	delete(ri.subscribers, id)

	ri.logger.Info("invalidation subscriber unregistered",
		logger.String("subscriber_id", id),
	)

	if ri.metrics != nil {
		ri.metrics.Counter("forge.cache.invalidation.redis.subscribers").Dec()
	}

	return nil
}

// InvalidateKey invalidates a specific cache key
func (ri *RedisInvalidation) InvalidateKey(ctx context.Context, cacheName, key string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypeKey,
		CacheName: cacheName,
		Key:       key,
		Source:    "redis",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return ri.PublishInvalidation(ctx, event)
}

// InvalidatePattern invalidates keys matching a pattern
func (ri *RedisInvalidation) InvalidatePattern(ctx context.Context, cacheName, pattern string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypePattern,
		CacheName: cacheName,
		Pattern:   pattern,
		Source:    "redis",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return ri.PublishInvalidation(ctx, event)
}

// InvalidateByTags invalidates keys with specific tags
func (ri *RedisInvalidation) InvalidateByTags(ctx context.Context, cacheName string, tags []string) error {
	event := cachecore.InvalidationEvent{
		Type:      cachecore.InvalidationTypeTags,
		CacheName: cacheName,
		Tags:      tags,
		Source:    "redis",
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	return ri.PublishInvalidation(ctx, event)
}

// GetInvalidationHistory retrieves invalidation history for a cache
func (ri *RedisInvalidation) GetInvalidationHistory(ctx context.Context, cacheName string, limit int) ([]cachecore.InvalidationEvent, error) {
	historyKey := fmt.Sprintf("%s:history:%s", ri.keyPrefix, cacheName)

	// Get history entries from Redis (this is a simplified implementation)
	keys, err := ri.client.Keys(ctx, historyKey+"*")
	if err != nil {
		return nil, fmt.Errorf("failed to get invalidation history: %w", err)
	}

	var events []cachecore.InvalidationEvent
	for i, key := range keys {
		if limit > 0 && i >= limit {
			break
		}

		data, err := ri.client.Get(ctx, key)
		if err != nil {
			continue
		}

		var event cachecore.InvalidationEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// GetStats returns Redis invalidation statistics
func (ri *RedisInvalidation) GetStats() RedisInvalidationStats {
	ri.mu.RLock()
	defer ri.mu.RUnlock()

	return RedisInvalidationStats{
		Subscribers:      len(ri.subscribers),
		RedisConnections: 2, // client + pubsub
	}
}

// HealthCheck performs health check
func (ri *RedisInvalidation) HealthCheck(ctx context.Context) error {
	if !ri.started {
		return fmt.Errorf("Redis invalidation not started")
	}

	// Ping Redis
	if err := ri.client.Ping(ctx); err != nil {
		return fmt.Errorf("Redis ping failed: %w", err)
	}

	return nil
}

// pubsubListener listens for invalidation events on Redis pub/sub
func (ri *RedisInvalidation) pubsubListener(ctx context.Context) {
	defer ri.wg.Done()

	ri.logger.Info("starting Redis pub/sub listener", logger.String("channel", ri.channel))

	// Subscribe to invalidation channel
	pubsub := ri.pubsubClient.Subscribe(ctx, ri.channel)
	defer pubsub.Close()

	if err := pubsub.Subscribe(ctx, ri.channel); err != nil {
		ri.logger.Error("failed to subscribe to Redis channel",
			logger.String("channel", ri.channel),
			logger.Error(err),
		)
		return
	}

	ch := pubsub.Channel()

	for {
		select {
		case <-ri.shutdown:
			ri.logger.Info("Redis pub/sub listener shutting down")
			return
		case <-ctx.Done():
			return
		case msg := <-ch:
			if msg == nil {
				continue
			}
			ri.handleInvalidationMessage(msg)
		}
	}
}

// handleInvalidationMessage handles an invalidation message from Redis
func (ri *RedisInvalidation) handleInvalidationMessage(msg *Message) {
	start := time.Now()

	// Parse invalidation event
	var event cachecore.InvalidationEvent
	if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
		ri.logger.Error("failed to parse invalidation event",
			logger.String("payload", msg.Payload),
			logger.Error(err),
		)

		if ri.metrics != nil {
			ri.metrics.Counter("forge.cache.invalidation.redis.parse.failed").Inc()
		}
		return
	}

	// Execute callbacks
	ri.mu.RLock()
	subscribers := make(map[string]cachecore.InvalidationCallback)
	for id, callback := range ri.subscribers {
		subscribers[id] = callback
	}
	ri.mu.RUnlock()

	for id, callback := range subscribers {
		if err := callback(event); err != nil {
			ri.logger.Error("invalidation callback failed",
				logger.String("subscriber_id", id),
				logger.String("event_type", string(event.Type)),
				logger.Error(err),
			)

			if ri.metrics != nil {
				ri.metrics.Counter("forge.cache.invalidation.redis.callback.failed").Inc()
			}
		}
	}

	if ri.metrics != nil {
		ri.metrics.Counter("forge.cache.invalidation.redis.received").Inc()
		ri.metrics.Histogram("forge.cache.invalidation.redis.process.latency").Observe(time.Since(start).Seconds())
	}
}

// storeInvalidationMetadata stores metadata about invalidation events
func (ri *RedisInvalidation) storeInvalidationMetadata(ctx context.Context, event cachecore.InvalidationEvent) error {
	// Store invalidation timestamp
	timestampKey := fmt.Sprintf("%s:timestamp:%s", ri.keyPrefix, event.CacheName)
	if err := ri.client.Set(ctx, timestampKey, strconv.FormatInt(event.Timestamp.Unix(), 10), time.Hour); err != nil {
		return err
	}

	// Store invalidation history (limited)
	historyKey := fmt.Sprintf("%s:history:%s:%d", ri.keyPrefix, event.CacheName, event.Timestamp.UnixNano())
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if err := ri.client.Set(ctx, historyKey, string(data), 24*time.Hour); err != nil {
		return err
	}

	// Store invalidation count
	countKey := fmt.Sprintf("%s:count:%s", ri.keyPrefix, event.CacheName)
	fmt.Println(countKey)
	// This would increment a counter - simplified for this implementation

	return nil
}

// RedisInvalidationPublisher implements InvalidationPublisher for Redis
type RedisInvalidationPublisher struct {
	redis *RedisInvalidation
}

// NewRedisInvalidationPublisher creates a new Redis invalidation publisher
func NewRedisInvalidationPublisher(redis *RedisInvalidation) *RedisInvalidationPublisher {
	return &RedisInvalidationPublisher{
		redis: redis,
	}
}

// Publish publishes a single invalidation event
func (rip *RedisInvalidationPublisher) Publish(ctx context.Context, event cachecore.InvalidationEvent) error {
	return rip.redis.PublishInvalidation(ctx, event)
}

// PublishBatch publishes multiple invalidation events
func (rip *RedisInvalidationPublisher) PublishBatch(ctx context.Context, events []cachecore.InvalidationEvent) error {
	return rip.redis.PublishBatchInvalidation(ctx, events)
}

// Close closes the publisher
func (rip *RedisInvalidationPublisher) Close() error {
	return nil // Redis connection is managed by RedisInvalidation
}

// RedisInvalidationSubscriber implements InvalidationSubscriber for Redis
type RedisInvalidationSubscriber struct {
	redis *RedisInvalidation
	id    string
}

// NewRedisInvalidationSubscriber creates a new Redis invalidation subscriber
func NewRedisInvalidationSubscriber(redis *RedisInvalidation, id string) *RedisInvalidationSubscriber {
	return &RedisInvalidationSubscriber{
		redis: redis,
		id:    id,
	}
}

// Subscribe subscribes to invalidation events
func (ris *RedisInvalidationSubscriber) Subscribe(ctx context.Context, callback cachecore.InvalidationCallback) error {
	return ris.redis.SubscribeToInvalidations(ris.id, callback)
}

// Unsubscribe unsubscribes from invalidation events
func (ris *RedisInvalidationSubscriber) Unsubscribe(ctx context.Context) error {
	return ris.redis.UnsubscribeFromInvalidations(ris.id)
}

// Close closes the subscriber
func (ris *RedisInvalidationSubscriber) Close() error {
	return ris.redis.UnsubscribeFromInvalidations(ris.id)
}
