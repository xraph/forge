package brokers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/events/core"
)

// RedisBroker supports ephemeral Pub/Sub and durable Streams delivery.
type RedisBroker struct {
	client        redis.UniversalClient
	config        *RedisConfig
	subscriptions map[string]*RedisSubscription
	handlers      map[string][]core.EventHandler
	logger        forge.Logger
	metrics       forge.Metrics
	connected     bool
	stopping      bool
	poolCancel    context.CancelFunc
	closeDone     chan struct{}
	recovery      map[string]string
	mu            sync.RWMutex
	wg            sync.WaitGroup
	stats         *RedisBrokerStats
}

// RedisBrokerStats contains Redis broker statistics.
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

// RedisPoolStats contains Redis connection pool statistics.
type RedisPoolStats struct {
	TotalConns int `json:"total_conns"`
	IdleConns  int `json:"idle_conns"`
	StaleConns int `json:"stale_conns"`
	Hits       int `json:"hits"`
	Misses     int `json:"misses"`
	Timeouts   int `json:"timeouts"`
}

// RedisConfig defines configuration for Redis broker.
type RedisConfig struct {
	Addresses           []string      `json:"addresses"             yaml:"addresses"`
	Username            string        `json:"username"              yaml:"username"`
	Password            string        `json:"password"              yaml:"password"`
	Database            int           `json:"database"              yaml:"database"`
	MasterName          string        `json:"master_name"           yaml:"master_name"`
	PoolSize            int           `json:"pool_size"             yaml:"pool_size"`
	MinIdleConns        int           `json:"min_idle_conns"        yaml:"min_idle_conns"`
	MaxIdleConns        int           `json:"max_idle_conns"        yaml:"max_idle_conns"`
	ConnMaxIdleTime     time.Duration `json:"conn_max_idle_time"    yaml:"conn_max_idle_time"`
	ConnMaxLifetime     time.Duration `json:"conn_max_lifetime"     yaml:"conn_max_lifetime"`
	DialTimeout         time.Duration `json:"dial_timeout"          yaml:"dial_timeout"`
	ReadTimeout         time.Duration `json:"read_timeout"          yaml:"read_timeout"`
	WriteTimeout        time.Duration `json:"write_timeout"         yaml:"write_timeout"`
	MaxRetries          int           `json:"max_retries"           yaml:"max_retries"`
	MinRetryBackoff     time.Duration `json:"min_retry_backoff"     yaml:"min_retry_backoff"`
	MaxRetryBackoff     time.Duration `json:"max_retry_backoff"     yaml:"max_retry_backoff"`
	ChannelSize         int           `json:"channel_size"          yaml:"channel_size"`
	EnableStreams       bool          `json:"enable_streams"        yaml:"enable_streams"`
	StreamMaxLen        int64         `json:"stream_max_len"        yaml:"stream_max_len"`
	ConsumerGroup       string        `json:"consumer_group"        yaml:"consumer_group"`
	ConsumerName        string        `json:"consumer_name"         yaml:"consumer_name"`
	StreamMaxDeliveries int64         `json:"stream_max_deliveries" yaml:"stream_max_deliveries"`
	StreamClaimIdle     time.Duration `json:"stream_claim_idle"     yaml:"stream_claim_idle"`
	StreamPollInterval  time.Duration `json:"stream_poll_interval"  yaml:"stream_poll_interval"`
	StreamLeaseDuration time.Duration `json:"stream_lease_duration" yaml:"stream_lease_duration"`
	StreamOrdering      string        `json:"stream_ordering"       yaml:"stream_ordering"`
}

// RedisSubscription wraps a Redis pub/sub subscription.
type RedisSubscription struct {
	pubsub   *redis.PubSub
	channel  string
	handlers []core.EventHandler
	cancel   context.CancelFunc
	broker   *RedisBroker
	group    string
	handler  core.EventHandler
}

// DefaultRedisConfig returns default Redis configuration.
func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		Addresses:           []string{"localhost:6379"},
		Database:            0,
		PoolSize:            10,
		MinIdleConns:        5,
		MaxIdleConns:        10,
		ConnMaxIdleTime:     time.Minute * 30,
		ConnMaxLifetime:     time.Hour,
		DialTimeout:         time.Second * 5,
		ReadTimeout:         time.Second * 3,
		WriteTimeout:        time.Second * 3,
		MaxRetries:          3,
		MinRetryBackoff:     time.Millisecond * 8,
		MaxRetryBackoff:     time.Millisecond * 512,
		ChannelSize:         100,
		EnableStreams:       false,
		StreamMaxLen:        10000,
		ConsumerGroup:       "forge-events",
		ConsumerName:        uuid.NewString(),
		StreamMaxDeliveries: 5,
		StreamClaimIdle:     30 * time.Second,
		StreamPollInterval:  100 * time.Millisecond,
		StreamLeaseDuration: 10 * time.Second,
		StreamOrdering:      "topic",
	}
}

// NewRedisBroker creates a new Redis broker.
func NewRedisBroker(config map[string]any, logger forge.Logger, metrics forge.Metrics) (*RedisBroker, error) {
	redisConfig := DefaultRedisConfig()

	// Parse configuration from map
	if config != nil {
		if addresses, ok := config["addresses"].([]any); ok {
			redisConfig.Addresses = make([]string, len(addresses))
			for i, addr := range addresses {
				if addrStr, ok := addr.(string); ok {
					redisConfig.Addresses[i] = addrStr
				}
			}
		} else if addresses, ok := config["addresses"].([]string); ok {
			redisConfig.Addresses = append([]string(nil), addresses...)
		} else if addr, ok := config["address"].(string); ok {
			redisConfig.Addresses = []string{addr}
		}

		for key, target := range map[string]*string{"username": &redisConfig.Username, "password": &redisConfig.Password, "master_name": &redisConfig.MasterName, "consumer_group": &redisConfig.ConsumerGroup, "consumer_name": &redisConfig.ConsumerName, "stream_ordering": &redisConfig.StreamOrdering} {
			if value, ok := config[key].(string); ok {
				*target = value
			}
		}

		for key, target := range map[string]*int{"database": &redisConfig.Database, "pool_size": &redisConfig.PoolSize} {
			if value, ok := config[key].(int); ok {
				*target = value
			}
		}

		for key, target := range map[string]*int64{"stream_max_len": &redisConfig.StreamMaxLen, "stream_max_deliveries": &redisConfig.StreamMaxDeliveries} {
			switch value := config[key].(type) {
			case int:
				*target = int64(value)
			case int64:
				*target = value
			case float64:
				*target = int64(value)
			}
		}

		for key, target := range map[string]*time.Duration{"stream_claim_idle": &redisConfig.StreamClaimIdle, "stream_poll_interval": &redisConfig.StreamPollInterval, "stream_lease_duration": &redisConfig.StreamLeaseDuration} {
			switch value := config[key].(type) {
			case time.Duration:
				*target = value
			case string:
				parsed, err := time.ParseDuration(value)
				if err != nil {
					return nil, fmt.Errorf("invalid %s: %w", key, err)
				}

				*target = parsed
			}
		}

		if value, ok := config["enable_streams"].(bool); ok {
			redisConfig.EnableStreams = value
		}
	}

	if redisConfig.EnableStreams {
		if strings.TrimSpace(redisConfig.ConsumerGroup) == "" || strings.TrimSpace(redisConfig.ConsumerName) == "" {
			return nil, errors.New("streams requires consumer_group and replica consumer_name")
		}

		if redisConfig.StreamMaxLen < 1 || redisConfig.StreamMaxDeliveries < 1 || redisConfig.StreamClaimIdle < time.Millisecond || redisConfig.StreamPollInterval < time.Millisecond || redisConfig.StreamLeaseDuration < 10*time.Millisecond {
			return nil, errors.New("streams capacity, retry and lease settings must be positive")
		}

		if redisConfig.StreamOrdering != "topic" {
			return nil, errors.New("streams supports topic ordering only")
		}

		if len(redisConfig.Addresses) > 1 && redisConfig.MasterName == "" {
			return nil, errors.New("streams cluster mode is unsupported; use a single instance or Sentinel")
		}
	}

	return &RedisBroker{
		config:        redisConfig,
		recovery:      make(map[string]string),
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

// Connect implements MessageBroker.
func (rb *RedisBroker) Connect(ctx context.Context, config any) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.connected {
		return nil
	}

	rb.stopping = false

	var client redis.UniversalClient

	switch {
	case len(rb.config.Addresses) == 1 && rb.config.MasterName == "":
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
	case rb.config.MasterName != "":
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
	default:
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
		_ = client.Close()
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
	poolCtx, cancel := context.WithCancel(context.Background())
	rb.poolCancel = cancel

	rb.wg.Add(1)
	go rb.collectPoolStats(poolCtx)

	return nil
}

// Publish preserves the event ID and applies backpressure when retained history is full.
func (rb *RedisBroker) Publish(ctx context.Context, topic string, event core.Event) error {
	rb.mu.RLock()

	if !rb.connected || rb.stopping || rb.client == nil {
		rb.mu.RUnlock()

		return errors.New("not connected to Redis")
	}

	client := rb.client
	rb.wg.Add(1)

	rb.mu.RUnlock()
	defer rb.wg.Done()

	start := time.Now()

	data, err := json.Marshal(event)
	if err == nil {
		if rb.config.EnableStreams {
			err = rb.publishToStream(ctx, client, topic, data)
		} else {
			err = client.Publish(ctx, topic, data).Err()
		}
	}

	rb.mu.Lock()
	if err != nil {
		rb.stats.PublishErrors++
		if strings.Contains(err.Error(), "RECOVERY_REQUIRED") {
			rb.recovery[topic] = err.Error()
		}
	} else {
		rb.stats.MessagesPublished++
		rb.stats.TotalPublishTime += time.Since(start)
		rb.stats.AvgPublishTime = rb.stats.TotalPublishTime / time.Duration(rb.stats.MessagesPublished)
	}
	rb.mu.Unlock()

	if rb.metrics != nil {
		if err != nil {
			rb.metrics.Counter("forge.events.redis.publish_errors", forge.WithLabel("topic", topic)).Inc()
		} else {
			rb.metrics.Counter("forge.events.redis.messages_published", forge.WithLabel("topic", topic)).Inc()
			rb.metrics.Histogram("forge.events.redis.publish_duration", forge.WithLabel("topic", topic)).Observe(time.Since(start).Seconds())
		}
	}

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// DurableCapabilities reports the configured Streams delivery guarantees.
func (rb *RedisBroker) DurableCapabilities() (core.DurableBrokerCapabilities, error) {
	if rb == nil {
		return core.DurableBrokerCapabilities{}, core.ErrDurableDeliveryUnavailable
	}

	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if !rb.config.EnableStreams {
		return core.DurableBrokerCapabilities{}, core.ErrDurableDeliveryUnavailable
	}

	return core.DurableBrokerCapabilities{
		Ordering: rb.config.StreamOrdering, MaxInFlight: 1,
		AcknowledgeAfterHandler: true, PendingRecovery: true,
		ConsumerGroup: rb.config.ConsumerGroup, ReplicaID: rb.config.ConsumerName,
	}, nil
}

// SubscribeDurable requires Streams delivery and uses ctx for its lifetime.
func (rb *RedisBroker) SubscribeDurable(ctx context.Context, topic string, handler core.EventHandler) error {
	if _, err := rb.DurableCapabilities(); err != nil {
		return err
	}

	return rb.Subscribe(ctx, topic, handler)
}

// Subscribe starts the configured listener. Pub/Sub uses ctx only for setup;
// its listener lasts until Unsubscribe or Close. Streams uses ctx for its lifetime.
func (rb *RedisBroker) Subscribe(ctx context.Context, topic string, handler core.EventHandler) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.connected || rb.stopping || rb.client == nil {
		return errors.New("not connected to Redis")
	}

	if handler == nil {
		return errors.New("handler is required")
	}

	if rb.config.EnableStreams {
		err := rb.subscribeStream(ctx, topic, handler)
		if err == nil {
			rb.recordSubscription(topic)
		}

		return err
	}

	if _, exists := rb.subscriptions[topic]; !exists {
		pubsub := rb.client.Subscribe(ctx, topic)
		if _, err := pubsub.Receive(ctx); err != nil {
			_ = pubsub.Close()

			return err
		}

		subCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		sub := &RedisSubscription{pubsub: pubsub, channel: topic, cancel: cancel, broker: rb}
		rb.subscriptions[topic] = sub
		rb.stats.Subscriptions++
		rb.recordSubscription(topic)

		rb.wg.Add(1)
		go rb.listen(subCtx, sub)
	}

	rb.handlers[topic] = append(rb.handlers[topic], handler)

	return nil
}

func (rb *RedisBroker) recordSubscription(topic string) {
	if rb.metrics != nil {
		rb.metrics.Counter("forge.events.redis.subscriptions", forge.WithLabel("topic", topic)).Inc()
		rb.metrics.Gauge("forge.events.redis.active_subscriptions").Set(float64(rb.stats.Subscriptions))
	}
}

func (rb *RedisBroker) listen(ctx context.Context, sub *RedisSubscription) {
	defer rb.wg.Done()

	ch := sub.pubsub.Channel(redis.WithChannelSize(rb.config.ChannelSize))

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

func (rb *RedisBroker) handleMessage(ctx context.Context, topic, payload string) {
	var event core.Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		rb.recordReceive(false, topic)

		return
	}

	rb.recordReceive(true, topic)
	rb.mu.RLock()
	handlers := append([]core.EventHandler(nil), rb.handlers[topic]...)
	rb.mu.RUnlock()

	for _, handler := range handlers {
		if handler.CanHandle(&event) {
			if err := handler.Handle(ctx, &event); err != nil {
				rb.recordReceive(false, topic)
			}
		}
	}
}

func (rb *RedisBroker) recordReceive(success bool, topic string) {
	if rb.metrics != nil {
		if success {
			rb.metrics.Counter("forge.events.redis.messages_received", forge.WithLabel("topic", topic)).Inc()
		} else {
			rb.metrics.Counter("forge.events.redis.receive_errors", forge.WithLabel("topic", topic)).Inc()
		}
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	if success {
		rb.stats.MessagesReceived++
	} else {
		rb.stats.ReceiveErrors++
		now := time.Now()
		rb.stats.LastError = &now
	}
}

func (rb *RedisBroker) Unsubscribe(ctx context.Context, topic, handlerName string) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.config.EnableStreams {
		key := streamSubscriptionKey(topic, handlerName)

		sub, ok := rb.subscriptions[key]
		if !ok {
			return fmt.Errorf("handler %s not found for topic %s", handlerName, topic)
		}

		sub.cancel()
		delete(rb.subscriptions, key)
		rb.stats.Subscriptions--

		return nil
	}

	handlers := rb.handlers[topic]
	kept := make([]core.EventHandler, 0, len(handlers))
	removed := false

	for _, handler := range handlers {
		if handler.Name() == handlerName {
			removed = true
		} else {
			kept = append(kept, handler)
		}
	}

	if !removed {
		return fmt.Errorf("handler %s not found for topic %s", handlerName, topic)
	}

	rb.handlers[topic] = kept
	if len(kept) == 0 {
		sub := rb.subscriptions[topic]
		sub.cancel()
		_ = sub.pubsub.Close()

		delete(rb.subscriptions, topic)
		delete(rb.handlers, topic)
		rb.stats.Subscriptions--
	}

	return nil
}

// Close cancels handlers before waiting and never holds the broker lock during a handler.
func (rb *RedisBroker) Close(ctx context.Context) error {
	rb.mu.Lock()
	if rb.stopping {
		done := rb.closeDone
		rb.mu.Unlock()

		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if !rb.connected {
		rb.mu.Unlock()

		return nil
	}

	rb.stopping = true
	rb.closeDone = make(chan struct{})
	done := rb.closeDone

	client := rb.client
	if rb.poolCancel != nil {
		rb.poolCancel()
	}

	for _, sub := range rb.subscriptions {
		sub.cancel()

		if sub.pubsub != nil {
			_ = sub.pubsub.Close()
		}
	}

	rb.mu.Unlock()
	go func() {
		rb.wg.Wait()

		_ = client.Close()

		rb.mu.Lock()
		rb.client = nil
		rb.connected = false
		rb.stats.Connected = false
		rb.stats.Subscriptions = 0
		rb.subscriptions = make(map[string]*RedisSubscription)
		rb.handlers = make(map[string][]core.EventHandler)

		close(done)
		rb.mu.Unlock()

		if rb.metrics != nil {
			rb.metrics.Gauge("forge.events.redis.connected").Set(0)
			rb.metrics.Gauge("forge.events.redis.active_subscriptions").Set(0)
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (rb *RedisBroker) HealthCheck(ctx context.Context) error {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if !rb.connected || rb.stopping || rb.client == nil {
		return errors.New("not connected to Redis")
	}

	for group, reason := range rb.recovery {
		return fmt.Errorf("recovery required for %s: %s", group, reason)
	}

	return rb.client.Ping(ctx).Err()
}

func (rb *RedisBroker) GetStats() map[string]any {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	recovery := make(map[string]string, len(rb.recovery))
	maps.Copy(recovery, rb.recovery)

	pool := *rb.stats.PoolStats

	return map[string]any{"type": "redis", "addresses": append([]string(nil), rb.config.Addresses...), "last_connected": rb.stats.LastConnected, "last_error": rb.stats.LastError, "connected": rb.stats.Connected, "subscriptions": rb.stats.Subscriptions, "messages_published": rb.stats.MessagesPublished, "messages_received": rb.stats.MessagesReceived, "publish_errors": rb.stats.PublishErrors, "receive_errors": rb.stats.ReceiveErrors, "connection_errors": rb.stats.ConnectionErrors, "avg_publish_time": rb.stats.AvgPublishTime.String(), "pool_stats": pool, "enable_streams": rb.config.EnableStreams, "recovery_required": recovery, "consumer_name": rb.config.ConsumerName, "ordering": rb.config.StreamOrdering}
}

func (rb *RedisBroker) collectPoolStats(ctx context.Context) {
	defer rb.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rb.mu.Lock()
			if poolStater, ok := rb.client.(interface{ PoolStats() *redis.PoolStats }); ok {
				stats := poolStater.PoolStats()
				rb.stats.PoolStats = &RedisPoolStats{TotalConns: int(stats.TotalConns), IdleConns: int(stats.IdleConns), StaleConns: int(stats.StaleConns), Hits: int(stats.Hits), Misses: int(stats.Misses), Timeouts: int(stats.Timeouts)}
			}
			rb.mu.Unlock()
		}
	}
}
