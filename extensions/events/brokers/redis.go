package brokers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// RedisBroker implements MessageBroker for Redis pub/sub
type RedisBroker struct {
	client        redis.UniversalClient
	config        *RedisConfig
	subscriptions map[string]*RedisSubscription
	handlers      map[string][]core.EventHandler
	logger        forge.Logger
	metrics       forge.Metrics
	connected     bool
	stopping      bool
	mu            sync.RWMutex
	wg            sync.WaitGroup
	stats         *RedisBrokerStats
}

// RedisBrokerStats contains Redis broker statistics
type RedisBrokerStats struct {
	Connected         bool       `json:"connected"`
	Subscriptions     int        `json:"subscriptions"`
	MessagesPublished int64      `json:"messages_published"`
	MessagesReceived  int64      `json:"messages_received"`
	PublishErrors     int64      `json:"publish_errors"`
	ReceiveErrors     int64      `json:"receive_errors"`
	ConnectionErrors  int64      `json:"connection_errors"`
	LastConnected     *time.Time `json:"last_connected"`
	LastError         *time.Time `json:"last_error"`
	TotalPublishTime  time.Duration
	AvgPublishTime    time.Duration
	PoolStats         *RedisPoolStats `json:"pool_stats"`
}

// RedisPoolStats contains Redis connection pool statistics
type RedisPoolStats struct {
	TotalConns int `json:"total_conns"`
	IdleConns  int `json:"idle_conns"`
	StaleConns int `json:"stale_conns"`
	Hits       int `json:"hits"`
	Misses     int `json:"misses"`
	Timeouts   int `json:"timeouts"`
}

// RedisConfig defines configuration for Redis broker
type RedisConfig struct {
	Addresses       []string      `yaml:"addresses" json:"addresses"`
	Username        string        `yaml:"username" json:"username"`
	Password        string        `yaml:"password" json:"password"`
	Database        int           `yaml:"database" json:"database"`
	MasterName      string        `yaml:"master_name" json:"master_name"`
	PoolSize        int           `yaml:"pool_size" json:"pool_size"`
	MinIdleConns    int           `yaml:"min_idle_conns" json:"min_idle_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns" json:"max_idle_conns"`
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time" json:"conn_max_idle_time"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" json:"conn_max_lifetime"`
	DialTimeout     time.Duration `yaml:"dial_timeout" json:"dial_timeout"`
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	MaxRetries      int           `yaml:"max_retries" json:"max_retries"`
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff" json:"min_retry_backoff"`
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff" json:"max_retry_backoff"`
	ChannelSize     int           `yaml:"channel_size" json:"channel_size"`
	EnableStreams   bool          `yaml:"enable_streams" json:"enable_streams"`
	StreamMaxLen    int64         `yaml:"stream_max_len" json:"stream_max_len"`
	ConsumerGroup   string        `yaml:"consumer_group" json:"consumer_group"`
	ConsumerName    string        `yaml:"consumer_name" json:"consumer_name"`
}

// RedisSubscription wraps a Redis pub/sub subscription
type RedisSubscription struct {
	pubsub   *redis.PubSub
	channel  string
	handlers []core.EventHandler
	cancel   context.CancelFunc
	broker   *RedisBroker
}

// DefaultRedisConfig returns default Redis configuration
func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		Addresses:       []string{"localhost:6379"},
		Database:        0,
		PoolSize:        10,
		MinIdleConns:    5,
		MaxIdleConns:    10,
		ConnMaxIdleTime: time.Minute * 30,
		ConnMaxLifetime: time.Hour,
		DialTimeout:     time.Second * 5,
		ReadTimeout:     time.Second * 3,
		WriteTimeout:    time.Second * 3,
		MaxRetries:      3,
		MinRetryBackoff: time.Millisecond * 8,
		MaxRetryBackoff: time.Millisecond * 512,
		ChannelSize:     100,
		EnableStreams:   false,
		StreamMaxLen:    10000,
		ConsumerGroup:   "forge-events",
		ConsumerName:    "consumer-1",
	}
}

// NewRedisBroker creates a new Redis broker
func NewRedisBroker(config map[string]interface{}, logger forge.Logger, metrics forge.Metrics) (*RedisBroker, error) {
	redisConfig := DefaultRedisConfig()

	// Parse configuration from map
	if config != nil {
		if addresses, ok := config["addresses"].([]interface{}); ok {
			redisConfig.Addresses = make([]string, len(addresses))
			for i, addr := range addresses {
				if addrStr, ok := addr.(string); ok {
					redisConfig.Addresses[i] = addrStr
				}
			}
		} else if addr, ok := config["address"].(string); ok {
			redisConfig.Addresses = []string{addr}
		}
		if password, ok := config["password"].(string); ok {
			redisConfig.Password = password
		}
		if database, ok := config["database"].(int); ok {
			redisConfig.Database = database
		}
		if poolSize, ok := config["pool_size"].(int); ok {
			redisConfig.PoolSize = poolSize
		}
		if enableStreams, ok := config["enable_streams"].(bool); ok {
			redisConfig.EnableStreams = enableStreams
		}
	}

	return &RedisBroker{
		config:        redisConfig,
		subscriptions: make(map[string]*RedisSubscription),
		handlers:      make(map[string][]core.EventHandler),
		logger:        logger,
		metrics:       metrics,
		stats: &RedisBrokerStats{
			Connected: false,
			PoolStats: &RedisPoolStats{},
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
		rb.logger.Info("connected to Redis", forge.F("addresses", fmt.Sprintf("%v", rb.config.Addresses)), forge.F("database", rb.config.Database))
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
func (rb *RedisBroker) Publish(ctx context.Context, topic string, event core.Event) error {
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
		err = rb.publishToStream(ctx, topic, data)
	} else {
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
	if rb.stats.MessagesPublished > 0 {
		rb.stats.AvgPublishTime = rb.stats.TotalPublishTime / time.Duration(rb.stats.MessagesPublished)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.events.redis.messages_published", "topic", topic).Inc()
		rb.metrics.Histogram("forge.events.redis.publish_duration", "topic", topic).Observe(duration.Seconds())
	}

	if rb.logger != nil {
		rb.logger.Debug("published event to Redis", forge.F("topic", topic), forge.F("event_id", event.ID), forge.F("event_type", event.Type), forge.F("duration", duration))
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
			"data": data,
		},
	}

	_, err := rb.client.XAdd(ctx, args).Result()
	return err
}

// Subscribe implements MessageBroker
func (rb *RedisBroker) Subscribe(ctx context.Context, topic string, handler core.EventHandler) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.connected || rb.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	// Add handler to the list
	if rb.handlers[topic] == nil {
		rb.handlers[topic] = make([]core.EventHandler, 0)
	}
	rb.handlers[topic] = append(rb.handlers[topic], handler)

	// Create subscription if it doesn't exist
	if _, exists := rb.subscriptions[topic]; !exists {
		pubsub := rb.client.Subscribe(ctx, topic)

		subCtx, cancel := context.WithCancel(context.Background())
		subscription := &RedisSubscription{
			pubsub:   pubsub,
			channel:  topic,
			handlers: rb.handlers[topic],
			cancel:   cancel,
			broker:   rb,
		}

		rb.subscriptions[topic] = subscription
		rb.stats.Subscriptions++

		// Start listening in background
		rb.wg.Add(1)
		go rb.listen(subCtx, subscription)

		if rb.logger != nil {
			rb.logger.Info("subscribed to Redis topic", forge.F("topic", topic))
		}

		if rb.metrics != nil {
			rb.metrics.Counter("forge.events.redis.subscriptions", "topic", topic).Inc()
			rb.metrics.Gauge("forge.events.redis.active_subscriptions").Set(float64(rb.stats.Subscriptions))
		}
	}

	if rb.logger != nil {
		rb.logger.Info("handler registered for Redis topic", forge.F("topic", topic), forge.F("handler", handler.Name()))
	}

	return nil
}

// listen listens for messages from Redis
func (rb *RedisBroker) listen(ctx context.Context, sub *RedisSubscription) {
	defer rb.wg.Done()

	ch := sub.pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			rb.handleMessage(ctx, sub.channel, msg.Payload)
		}
	}
}

// handleMessage processes a message from Redis
func (rb *RedisBroker) handleMessage(ctx context.Context, topic string, payload string) {
	start := time.Now()

	// Deserialize event
	var event core.Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		rb.stats.ReceiveErrors++
		if rb.logger != nil {
			rb.logger.Error("failed to deserialize event from Redis", forge.F("topic", topic), forge.F("error", err))
		}
		if rb.metrics != nil {
			rb.metrics.Counter("forge.events.redis.receive_errors", "topic", topic).Inc()
		}
		return
	}

	rb.stats.MessagesReceived++

	// Get handlers for this topic
	rb.mu.RLock()
	handlers := make([]core.EventHandler, len(rb.handlers[topic]))
	copy(handlers, rb.handlers[topic])
	rb.mu.RUnlock()

	// Process with all handlers
	for _, handler := range handlers {
		if !handler.CanHandle(&event) {
			continue
		}

		if err := handler.Handle(ctx, &event); err != nil {
			rb.stats.ReceiveErrors++
			if rb.logger != nil {
				rb.logger.Error("handler failed to process Redis event", forge.F("topic", topic), forge.F("handler", handler.Name()), forge.F("event_id", event.ID), forge.F("error", err))
			}
			if rb.metrics != nil {
				rb.metrics.Counter("forge.events.redis.handler_errors", "topic", topic, "handler", handler.Name()).Inc()
			}
		}
	}

	duration := time.Since(start)
	if rb.metrics != nil {
		rb.metrics.Counter("forge.events.redis.messages_received", "topic", topic).Inc()
		rb.metrics.Histogram("forge.events.redis.receive_duration", "topic", topic).Observe(duration.Seconds())
	}

	if rb.logger != nil {
		rb.logger.Debug("processed Redis message", forge.F("topic", topic), forge.F("event_id", event.ID), forge.F("event_type", event.Type), forge.F("handlers", len(handlers)), forge.F("duration", duration))
	}
}

// Unsubscribe implements MessageBroker
func (rb *RedisBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	handlers, exists := rb.handlers[topic]
	if !exists {
		return fmt.Errorf("no handlers for topic %s", topic)
	}

	newHandlers := make([]core.EventHandler, 0)
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
			sub.cancel()
			if err := sub.pubsub.Close(); err != nil {
				if rb.logger != nil {
					rb.logger.Error("failed to close Redis subscription", forge.F("topic", topic), forge.F("error", err))
				}
			}

			delete(rb.subscriptions, topic)
			delete(rb.handlers, topic)
			rb.stats.Subscriptions--

			if rb.logger != nil {
				rb.logger.Info("unsubscribed from Redis topic", forge.F("topic", topic))
			}

			if rb.metrics != nil {
				rb.metrics.Counter("forge.events.redis.unsubscriptions", "topic", topic).Inc()
				rb.metrics.Gauge("forge.events.redis.active_subscriptions").Set(float64(rb.stats.Subscriptions))
			}
		}
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

	// Unsubscribe from all topics
	for topic, sub := range rb.subscriptions {
		sub.cancel()
		if err := sub.pubsub.Close(); err != nil {
			if rb.logger != nil {
				rb.logger.Error("failed to close Redis subscription during close", forge.F("topic", topic), forge.F("error", err))
			}
		}
	}

	// Wait for all listeners to stop
	rb.wg.Wait()

	// Close client
	if err := rb.client.Close(); err != nil {
		if rb.logger != nil {
			rb.logger.Error("failed to close Redis client", forge.F("error", err))
		}
	}

	rb.client = nil
	rb.connected = false
	rb.stats.Connected = false

	// Clear state
	rb.subscriptions = make(map[string]*RedisSubscription)
	rb.handlers = make(map[string][]core.EventHandler)
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
		return fmt.Errorf("Redis health check failed: %w", err)
	}

	return nil
}

// GetStats implements MessageBroker
func (rb *RedisBroker) GetStats() map[string]interface{} {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return map[string]interface{}{
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
			if rb.client != nil {
				if poolStater, ok := rb.client.(interface{ PoolStats() *redis.PoolStats }); ok {
					stats := poolStater.PoolStats()
					rb.mu.Lock()
					rb.stats.PoolStats.TotalConns = stats.TotalConns
					rb.stats.PoolStats.IdleConns = stats.IdleConns
					rb.stats.PoolStats.StaleConns = stats.StaleConns
					rb.stats.PoolStats.Hits = int(stats.Hits)
					rb.stats.PoolStats.Misses = int(stats.Misses)
					rb.stats.PoolStats.Timeouts = int(stats.Timeouts)
					rb.mu.Unlock()

					if rb.metrics != nil {
						rb.metrics.Gauge("forge.events.redis.pool.total_conns").Set(float64(stats.TotalConns))
						rb.metrics.Gauge("forge.events.redis.pool.idle_conns").Set(float64(stats.IdleConns))
						rb.metrics.Gauge("forge.events.redis.pool.stale_conns").Set(float64(stats.StaleConns))
					}
				}
			}
		}
	}
}
