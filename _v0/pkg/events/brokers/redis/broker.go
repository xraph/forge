package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/v0/pkg/common"
	eventsCore "github.com/xraph/forge/v0/pkg/events/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// RedisBroker implements MessageBroker for Redis pub/sub
type RedisBroker struct {
	client        redis.UniversalClient
	config        *RedisConfig
	subscriptions map[string]*RedisSubscription
	handlers      map[string][]eventsCore.EventHandler
	logger        common.Logger
	metrics       common.Metrics
	connected     bool
	stopping      bool
	mu            sync.RWMutex
	wg            sync.WaitGroup
	stats         *BrokerStats
}

// BrokerStats contains Redis broker statistics
type BrokerStats struct {
	Connected         bool          `json:"connected"`
	Subscriptions     int           `json:"subscriptions"`
	MessagesPublished int64         `json:"messages_published"`
	MessagesReceived  int64         `json:"messages_received"`
	PublishErrors     int64         `json:"publish_errors"`
	ReceiveErrors     int64         `json:"receive_errors"`
	ConnectionErrors  int64         `json:"connection_errors"`
	LastConnected     *time.Time    `json:"last_connected"`
	LastError         *time.Time    `json:"last_error"`
	TotalPublishTime  time.Duration `json:"total_publish_time"`
	AvgPublishTime    time.Duration `json:"avg_publish_time"`
	PoolStats         *PoolStats    `json:"pool_stats"`
}

// PoolStats contains Redis connection pool statistics
type PoolStats struct {
	TotalConns int `json:"total_conns"`
	IdleConns  int `json:"idle_conns"`
	StaleConns int `json:"stale_conns"`
	Hits       int `json:"hits"`
	Misses     int `json:"misses"`
	Timeouts   int `json:"timeouts"`
}

// NewRedisBroker creates a new Redis broker
func NewRedisBroker(config map[string]interface{}, logger common.Logger, metrics common.Metrics) (*RedisBroker, error) {
	redisConfig := DefaultRedisConfig()

	// Parse configuration
	if config != nil {
		if err := parseRedisConfig(config, redisConfig); err != nil {
			return nil, fmt.Errorf("failed to parse Redis config: %w", err)
		}
	}

	return &RedisBroker{
		config:        redisConfig,
		subscriptions: make(map[string]*RedisSubscription),
		handlers:      make(map[string][]eventsCore.EventHandler),
		logger:        logger,
		metrics:       metrics,
		stats: &BrokerStats{
			Connected: false,
			PoolStats: &PoolStats{},
		},
	}, nil
}

// Connect implements MessageBroker
func (rb *RedisBroker) Connect(ctx context.Context, config interface{}) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.connected {
		return nil
	}

	// Create Redis client options
	var client redis.UniversalClient

	if len(rb.config.Addresses) == 1 && rb.config.MasterName == "" {
		// Single instance mode
		options := &redis.Options{
			Addr:            rb.config.Addresses[0],
			Username:        rb.config.Username,
			Password:        rb.config.Password,
			DB:              rb.config.Database,
			PoolSize:        rb.config.PoolSize,
			MinIdleConns:    rb.config.MinIdleConns,
			MaxIdleConns:    rb.config.MaxIdleConns,
			ConnMaxIdleTime: rb.config.ConnMaxIdleTime,
			ConnMaxLifetime: rb.config.ConnMaxLifetime,
			DialTimeout:     rb.config.DialTimeout,
			ReadTimeout:     rb.config.ReadTimeout,
			WriteTimeout:    rb.config.WriteTimeout,
			MaxRetries:      rb.config.MaxRetries,
			MinRetryBackoff: rb.config.MinRetryBackoff,
			MaxRetryBackoff: rb.config.MaxRetryBackoff,
		}

		client = redis.NewClient(options)
	} else if rb.config.MasterName != "" {
		// Sentinel mode
		options := &redis.FailoverOptions{
			MasterName:      rb.config.MasterName,
			SentinelAddrs:   rb.config.Addresses,
			Username:        rb.config.Username,
			Password:        rb.config.Password,
			DB:              rb.config.Database,
			PoolSize:        rb.config.PoolSize,
			MinIdleConns:    rb.config.MinIdleConns,
			MaxIdleConns:    rb.config.MaxIdleConns,
			ConnMaxIdleTime: rb.config.ConnMaxIdleTime,
			ConnMaxLifetime: rb.config.ConnMaxLifetime,
			DialTimeout:     rb.config.DialTimeout,
			ReadTimeout:     rb.config.ReadTimeout,
			WriteTimeout:    rb.config.WriteTimeout,
			MaxRetries:      rb.config.MaxRetries,
			MinRetryBackoff: rb.config.MinRetryBackoff,
			MaxRetryBackoff: rb.config.MaxRetryBackoff,
		}

		client = redis.NewFailoverClient(options)
	} else {
		// Cluster mode
		options := &redis.ClusterOptions{
			Addrs:           rb.config.Addresses,
			Username:        rb.config.Username,
			Password:        rb.config.Password,
			PoolSize:        rb.config.PoolSize,
			MinIdleConns:    rb.config.MinIdleConns,
			MaxIdleConns:    rb.config.MaxIdleConns,
			ConnMaxIdleTime: rb.config.ConnMaxIdleTime,
			ConnMaxLifetime: rb.config.ConnMaxLifetime,
			DialTimeout:     rb.config.DialTimeout,
			ReadTimeout:     rb.config.ReadTimeout,
			WriteTimeout:    rb.config.WriteTimeout,
			MaxRetries:      rb.config.MaxRetries,
			MinRetryBackoff: rb.config.MinRetryBackoff,
			MaxRetryBackoff: rb.config.MaxRetryBackoff,
		}

		client = redis.NewClusterClient(options)
	}

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		rb.stats.ConnectionErrors++
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rb.client = client
	rb.connected = true
	now := time.Now()
	rb.stats.Connected = true
	rb.stats.LastConnected = &now

	if rb.logger != nil {
		rb.logger.Info("connected to Redis",
			logger.String("addresses", fmt.Sprintf("%v", rb.config.Addresses)),
			logger.Int("database", rb.config.Database),
		)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.events.redis.connections").Inc()
		rb.metrics.Gauge("forge.events.redis.connected").Set(1)
	}

	// Start pool stats collection
	go rb.collectPoolStats(ctx)

	return nil
}

// Publish implements MessageBroker
func (rb *RedisBroker) Publish(ctx context.Context, topic string, event eventsCore.Event) error {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if !rb.connected || rb.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	start := time.Now()

	// Serialize event
	data, err := json.Marshal(event)
	if err != nil {
		rb.stats.PublishErrors++
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Publish to Redis
	if rb.config.EnableStreams {
		// Use Redis Streams
		err = rb.publishToStream(ctx, topic, data)
	} else {
		// Use Redis Pub/Sub
		err = rb.client.Publish(ctx, topic, data).Err()
	}

	if err != nil {
		rb.stats.PublishErrors++
		if rb.metrics != nil {
			rb.metrics.Counter("forge.events.redis.publish_errors", "topic", topic).Inc()
		}
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Update statistics
	duration := time.Since(start)
	rb.stats.MessagesPublished++
	rb.stats.TotalPublishTime += duration
	rb.stats.AvgPublishTime = rb.stats.TotalPublishTime / time.Duration(rb.stats.MessagesPublished)

	if rb.metrics != nil {
		rb.metrics.Counter("forge.events.redis.messages_published", "topic", topic).Inc()
		rb.metrics.Histogram("forge.events.redis.publish_duration", "topic", topic).Observe(duration.Seconds())
	}

	if rb.logger != nil {
		rb.logger.Debug("published event to Redis",
			logger.String("topic", topic),
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// publishToStream publishes to Redis Streams
func (rb *RedisBroker) publishToStream(ctx context.Context, stream string, data []byte) error {
	args := &redis.XAddArgs{
		Stream: stream,
		MaxLen: rb.config.StreamMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"event": string(data),
		},
	}

	return rb.client.XAdd(ctx, args).Err()
}

// Subscribe implements MessageBroker
func (rb *RedisBroker) Subscribe(ctx context.Context, topic string, handler eventsCore.EventHandler) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.connected || rb.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	// Add handler to the list
	if rb.handlers[topic] == nil {
		rb.handlers[topic] = make([]eventsCore.EventHandler, 0)
	}
	rb.handlers[topic] = append(rb.handlers[topic], handler)

	// Create subscription if it doesn't exist
	if _, exists := rb.subscriptions[topic]; !exists {
		var sub *RedisSubscription
		var err error

		if rb.config.EnableStreams {
			sub, err = rb.subscribeToStream(ctx, topic)
		} else {
			sub, err = rb.subscribeToPubSub(ctx, topic)
		}

		if err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
		}

		rb.subscriptions[topic] = sub
		rb.stats.Subscriptions++

		if rb.logger != nil {
			rb.logger.Info("subscribed to Redis topic",
				logger.String("topic", topic),
			)
		}

		if rb.metrics != nil {
			rb.metrics.Counter("forge.events.redis.subscriptions", "topic", topic).Inc()
			rb.metrics.Gauge("forge.events.redis.active_subscriptions").Set(float64(rb.stats.Subscriptions))
		}
	}

	if rb.logger != nil {
		rb.logger.Info("handler registered for Redis topic",
			logger.String("topic", topic),
			logger.String("handler", handler.Name()),
		)
	}

	return nil
}

// subscribeToPubSub subscribes to Redis Pub/Sub
func (rb *RedisBroker) subscribeToPubSub(ctx context.Context, topic string) (*RedisSubscription, error) {
	var pubsub *redis.PubSub

	if rb.config.Pattern {
		pubsub = rb.client.PSubscribe(ctx, topic)
	} else {
		pubsub = rb.client.Subscribe(ctx, topic)
	}

	subCtx, cancel := context.WithCancel(ctx)

	sub := &RedisSubscription{
		pubsub:  pubsub,
		channel: topic,
		cancel:  cancel,
		broker:  rb,
	}

	// Start message processing goroutine
	rb.wg.Add(1)
	go func() {
		defer rb.wg.Done()
		sub.processMessages(subCtx)
	}()

	return sub, nil
}

// subscribeToStream subscribes to Redis Streams
func (rb *RedisBroker) subscribeToStream(ctx context.Context, stream string) (*RedisSubscription, error) {
	// Create consumer group if it doesn't exist
	err := rb.client.XGroupCreateMkStream(ctx, stream, rb.config.ConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return nil, fmt.Errorf("failed to create consumer group: %w", err)
	}

	subCtx, cancel := context.WithCancel(ctx)

	sub := &RedisSubscription{
		channel: stream,
		cancel:  cancel,
		broker:  rb,
	}

	// Start stream processing goroutine
	rb.wg.Add(1)
	go func() {
		defer rb.wg.Done()
		sub.processStream(subCtx, stream)
	}()

	return sub, nil
}

// Unsubscribe implements MessageBroker
func (rb *RedisBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Remove handler from the list
	handlers, exists := rb.handlers[topic]
	if !exists {
		return fmt.Errorf("no handlers for topic %s", topic)
	}

	newHandlers := make([]eventsCore.EventHandler, 0)
	removed := false
	for _, h := range handlers {
		if h.Name() != handlerName {
			newHandlers = append(newHandlers, h)
		} else {
			removed = true
		}
	}

	if !removed {
		return fmt.Errorf("handler %s not found for topic %s", handlerName, topic)
	}

	rb.handlers[topic] = newHandlers

	// If no more handlers, unsubscribe from Redis
	if len(newHandlers) == 0 {
		if sub, exists := rb.subscriptions[topic]; exists {
			sub.close()
			delete(rb.subscriptions, topic)
			delete(rb.handlers, topic)
			rb.stats.Subscriptions--

			if rb.logger != nil {
				rb.logger.Info("unsubscribed from Redis topic",
					logger.String("topic", topic),
				)
			}

			if rb.metrics != nil {
				rb.metrics.Counter("forge.events.redis.unsubscriptions", "topic", topic).Inc()
				rb.metrics.Gauge("forge.events.redis.active_subscriptions").Set(float64(rb.stats.Subscriptions))
			}
		}
	}

	if rb.logger != nil {
		rb.logger.Info("handler unregistered from Redis topic",
			logger.String("topic", topic),
			logger.String("handler", handlerName),
		)
	}

	return nil
}

// Close implements MessageBroker
func (rb *RedisBroker) Close(ctx context.Context) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.connected || rb.client == nil {
		return nil
	}

	rb.stopping = true

	// Close all subscriptions
	for topic, sub := range rb.subscriptions {
		sub.close()
		if rb.logger != nil {
			rb.logger.Debug("closed Redis subscription",
				logger.String("topic", topic),
			)
		}
	}

	// Wait for all goroutines to finish
	rb.wg.Wait()

	// Close Redis client
	if err := rb.client.Close(); err != nil {
		if rb.logger != nil {
			rb.logger.Error("error closing Redis client",
				logger.Error(err),
			)
		}
	}

	rb.client = nil
	rb.connected = false
	rb.stopping = false
	rb.stats.Connected = false

	// Clear state
	rb.subscriptions = make(map[string]*RedisSubscription)
	rb.handlers = make(map[string][]eventsCore.EventHandler)
	rb.stats.Subscriptions = 0

	if rb.logger != nil {
		rb.logger.Info("Redis connection closed")
	}

	if rb.metrics != nil {
		rb.metrics.Gauge("forge.events.redis.connected").Set(0)
		rb.metrics.Gauge("forge.events.redis.active_subscriptions").Set(0)
	}

	return nil
}

// HealthCheck implements MessageBroker
func (rb *RedisBroker) HealthCheck(ctx context.Context) error {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if !rb.connected || rb.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	if err := rb.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis ping failed: %w", err)
	}

	return nil
}

// GetStats implements MessageBroker
func (rb *RedisBroker) GetStats() map[string]interface{} {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	stats := map[string]interface{}{
		"type":               "redis",
		"addresses":          rb.config.Addresses,
		"connected":          rb.stats.Connected,
		"subscriptions":      rb.stats.Subscriptions,
		"messages_published": rb.stats.MessagesPublished,
		"messages_received":  rb.stats.MessagesReceived,
		"publish_errors":     rb.stats.PublishErrors,
		"receive_errors":     rb.stats.ReceiveErrors,
		"connection_errors":  rb.stats.ConnectionErrors,
		"last_connected":     rb.stats.LastConnected,
		"last_error":         rb.stats.LastError,
		"avg_publish_time":   rb.stats.AvgPublishTime.String(),
		"pool_stats":         rb.stats.PoolStats,
	}

	return stats
}

// collectPoolStats collects Redis connection pool statistics
func (rb *RedisBroker) collectPoolStats(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if rb.stopping || !rb.connected {
				return
			}

			rb.updatePoolStats()
		}
	}
}

// updatePoolStats updates connection pool statistics
func (rb *RedisBroker) updatePoolStats() {
	if rb.client == nil {
		return
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Get pool stats (this is a simplified version)
	// In practice, you'd need to access the internal pool stats
	rb.stats.PoolStats = &PoolStats{
		TotalConns: rb.config.PoolSize,
		IdleConns:  rb.config.MinIdleConns,
	}

	if rb.metrics != nil {
		rb.metrics.Gauge("forge.events.redis.pool_total_conns").Set(float64(rb.stats.PoolStats.TotalConns))
		rb.metrics.Gauge("forge.events.redis.pool_idle_conns").Set(float64(rb.stats.PoolStats.IdleConns))
	}
}

// processMessages processes pub/sub messages
func (rs *RedisSubscription) processMessages(ctx context.Context) {
	ch := rs.pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			rs.handleMessage(ctx, msg.Payload)
		}
	}
}

// processStream processes Redis Streams messages
func (rs *RedisSubscription) processStream(ctx context.Context, stream string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			rs.readFromStream(ctx, stream)
		}
	}
}

// readFromStream reads messages from Redis Streams
func (rs *RedisSubscription) readFromStream(ctx context.Context, stream string) {
	args := &redis.XReadGroupArgs{
		Group:    rs.broker.config.ConsumerGroup,
		Consumer: rs.broker.config.ConsumerName,
		Streams:  []string{stream, ">"},
		Count:    10,
		Block:    time.Second,
	}

	streams, err := rs.broker.client.XReadGroup(ctx, args).Result()
	if err != nil {
		if err != redis.Nil {
			rs.broker.stats.ReceiveErrors++
			if rs.broker.logger != nil {
				rs.broker.logger.Error("failed to read from Redis stream",
					logger.String("stream", stream),
					logger.Error(err),
				)
			}
		}
		time.Sleep(time.Second)
		return
	}

	for _, stream := range streams {
		for _, message := range stream.Messages {
			if eventData, ok := message.Values["event"].(string); ok {
				rs.handleMessage(ctx, eventData)

				// Acknowledge message
				rs.broker.client.XAck(ctx, stream.Stream, rs.broker.config.ConsumerGroup, message.ID)
			}
		}
	}
}

// handleMessage handles a received message
func (rs *RedisSubscription) handleMessage(ctx context.Context, payload string) {
	start := time.Now()

	// Deserialize event
	var event eventsCore.Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		rs.broker.stats.ReceiveErrors++
		if rs.broker.logger != nil {
			rs.broker.logger.Error("failed to deserialize event from Redis",
				logger.String("channel", rs.channel),
				logger.Error(err),
			)
		}
		if rs.broker.metrics != nil {
			rs.broker.metrics.Counter("forge.events.redis.receive_errors", "topic", rs.channel).Inc()
		}
		return
	}

	rs.broker.stats.MessagesReceived++

	// Get handlers for this topic
	rs.broker.mu.RLock()
	handlers := make([]eventsCore.EventHandler, len(rs.broker.handlers[rs.channel]))
	copy(handlers, rs.broker.handlers[rs.channel])
	rs.broker.mu.RUnlock()

	// Process with all handlers
	for _, handler := range handlers {
		if !handler.CanHandle(&event) {
			continue
		}

		if err := handler.Handle(ctx, &event); err != nil {
			rs.broker.stats.ReceiveErrors++
			if rs.broker.logger != nil {
				rs.broker.logger.Error("handler failed to process Redis event",
					logger.String("channel", rs.channel),
					logger.String("handler", handler.Name()),
					logger.String("event_id", event.ID),
					logger.Error(err),
				)
			}
			if rs.broker.metrics != nil {
				rs.broker.metrics.Counter("forge.events.redis.handler_errors", "topic", rs.channel, "handler", handler.Name()).Inc()
			}
		}
	}

	duration := time.Since(start)
	if rs.broker.metrics != nil {
		rs.broker.metrics.Counter("forge.events.redis.messages_received", "topic", rs.channel).Inc()
		rs.broker.metrics.Histogram("forge.events.redis.receive_duration", "topic", rs.channel).Observe(duration.Seconds())
	}

	if rs.broker.logger != nil {
		rs.broker.logger.Debug("processed Redis message",
			logger.String("channel", rs.channel),
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
			logger.Int("handlers", len(handlers)),
			logger.Duration("duration", duration),
		)
	}
}

// close closes the subscription
func (rs *RedisSubscription) close() {
	if rs.cancel != nil {
		rs.cancel()
	}
	if rs.pubsub != nil {
		rs.pubsub.Close()
	}
}
