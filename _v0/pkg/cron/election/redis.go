package election

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// RedisBackend implements leader election using Redis
type RedisBackend struct {
	config    *Config
	client    *redis.Client
	leaderKey string
	watchKey  string
	nodeID    string
	clusterID string

	// Watch state
	watchCallback func(leaderID string)
	watchContext  context.Context
	watchCancel   context.CancelFunc

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// RedisConfig contains Redis-specific configuration
type RedisConfig struct {
	Address    string `json:"address" yaml:"address"`
	Password   string `json:"password" yaml:"password"`
	DB         int    `json:"db" yaml:"db"`
	PoolSize   int    `json:"pool_size" yaml:"pool_size"`
	MaxRetries int    `json:"max_retries" yaml:"max_retries"`
	TLSEnabled bool   `json:"tls_enabled" yaml:"tls_enabled"`
}

// NewRedisBackend creates a new Redis backend for leader election
func NewRedisBackend(config *Config, logger common.Logger, metrics common.Metrics) (Backend, error) {
	// Parse Redis config
	redisConfig := &RedisConfig{
		Address:    "localhost:6379",
		Password:   "",
		DB:         0,
		PoolSize:   10,
		MaxRetries: 3,
		TLSEnabled: false,
	}

	// Override with provided config
	if config.BackendConfig != nil {
		if address, ok := config.BackendConfig["address"].(string); ok {
			redisConfig.Address = address
		}
		if password, ok := config.BackendConfig["password"].(string); ok {
			redisConfig.Password = password
		}
		if db, ok := config.BackendConfig["db"].(int); ok {
			redisConfig.DB = db
		}
		if poolSize, ok := config.BackendConfig["pool_size"].(int); ok {
			redisConfig.PoolSize = poolSize
		}
		if maxRetries, ok := config.BackendConfig["max_retries"].(int); ok {
			redisConfig.MaxRetries = maxRetries
		}
		if tlsEnabled, ok := config.BackendConfig["tls_enabled"].(bool); ok {
			redisConfig.TLSEnabled = tlsEnabled
		}
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:       redisConfig.Address,
		Password:   redisConfig.Password,
		DB:         redisConfig.DB,
		PoolSize:   redisConfig.PoolSize,
		MaxRetries: redisConfig.MaxRetries,
	})

	backend := &RedisBackend{
		config:    config,
		client:    client,
		leaderKey: fmt.Sprintf("forge:cron:leader:%s", config.ClusterID),
		watchKey:  fmt.Sprintf("forge:cron:leader:watch:%s", config.ClusterID),
		nodeID:    config.NodeID,
		clusterID: config.ClusterID,
		logger:    logger,
		metrics:   metrics,
	}

	return backend, nil
}

// Start starts the Redis backend
func (rb *RedisBackend) Start(ctx context.Context) error {
	// Test connection
	if err := rb.client.Ping(ctx).Err(); err != nil {
		return common.ErrServiceStartFailed("redis-election", err)
	}

	if rb.logger != nil {
		rb.logger.Info("redis election backend started", logger.String("address", rb.client.Options().Addr))
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.election.redis_started").Inc()
	}

	return nil
}

// Stop stops the Redis backend
func (rb *RedisBackend) Stop(ctx context.Context) error {
	// Cancel watch if active
	if rb.watchCancel != nil {
		rb.watchCancel()
	}

	// Close client
	if err := rb.client.Close(); err != nil {
		return common.ErrServiceStopFailed("redis-election", err)
	}

	if rb.logger != nil {
		rb.logger.Info("redis election backend stopped")
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.election.redis_stopped").Inc()
	}

	return nil
}

// Campaign attempts to become leader
func (rb *RedisBackend) Campaign(ctx context.Context, nodeID string, ttl time.Duration) error {
	// Use SET with NX (not exists) and EX (expiry) options
	// This is atomic and will only succeed if the key doesn't exist
	result := rb.client.SetNX(ctx, rb.leaderKey, nodeID, ttl)
	if err := result.Err(); err != nil {
		return common.ErrContainerError("campaign", err)
	}

	success := result.Val()
	if !success {
		return common.ErrContainerError("campaign", fmt.Errorf("failed to acquire leadership"))
	}

	// Publish leadership change
	rb.publishLeadershipChange(ctx, nodeID)

	if rb.logger != nil {
		rb.logger.Info("successfully campaigned for leadership",
			logger.String("node_id", nodeID),
			logger.Duration("ttl", ttl),
		)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.election.campaigns_success").Inc()
	}

	return nil
}

// Resign resigns from leadership
func (rb *RedisBackend) Resign(ctx context.Context, nodeID string) error {
	// Use Lua script to ensure atomic resign operation
	script := redis.NewScript(`
		local current = redis.call('GET', KEYS[1])
		if current == ARGV[1] then
			redis.call('DEL', KEYS[1])
			return 1
		else
			return 0
		end
	`)

	result := script.Run(ctx, rb.client, []string{rb.leaderKey}, nodeID)
	if err := result.Err(); err != nil {
		return common.ErrContainerError("resign", err)
	}

	success := result.Val().(int64)
	if success == 0 {
		return common.ErrContainerError("resign", fmt.Errorf("not the current leader"))
	}

	// Publish leadership change
	rb.publishLeadershipChange(ctx, "")

	if rb.logger != nil {
		rb.logger.Info("successfully resigned from leadership", logger.String("node_id", nodeID))
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.election.resignations").Inc()
	}

	return nil
}

// GetLeader returns the current leader
func (rb *RedisBackend) GetLeader(ctx context.Context) (string, error) {
	result := rb.client.Get(ctx, rb.leaderKey)
	if err := result.Err(); err != nil {
		if err == redis.Nil {
			return "", nil // No leader
		}
		return "", common.ErrContainerError("get_leader", err)
	}

	return result.Val(), nil
}

// IsLeader checks if the given node is the leader
func (rb *RedisBackend) IsLeader(ctx context.Context, nodeID string) (bool, error) {
	leader, err := rb.GetLeader(ctx)
	if err != nil {
		return false, err
	}

	return leader == nodeID, nil
}

// Heartbeat extends the leadership TTL
func (rb *RedisBackend) Heartbeat(ctx context.Context, nodeID string, ttl time.Duration) error {
	// Use Lua script to ensure atomic heartbeat operation
	script := redis.NewScript(`
		local current = redis.call('GET', KEYS[1])
		if current == ARGV[1] then
			redis.call('EXPIRE', KEYS[1], ARGV[2])
			return 1
		else
			return 0
		end
	`)

	result := script.Run(ctx, rb.client, []string{rb.leaderKey}, nodeID, int(ttl.Seconds()))
	if err := result.Err(); err != nil {
		return common.ErrContainerError("heartbeat", err)
	}

	success := result.Val().(int64)
	if success == 0 {
		return common.ErrContainerError("heartbeat", fmt.Errorf("not the current leader"))
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.election.heartbeats_sent").Inc()
	}

	return nil
}

// Watch watches for leadership changes
func (rb *RedisBackend) Watch(ctx context.Context, callback func(leaderID string)) error {
	if callback == nil {
		return common.ErrValidationError("callback", fmt.Errorf("callback cannot be nil"))
	}

	rb.watchCallback = callback
	rb.watchContext, rb.watchCancel = context.WithCancel(ctx)

	// Start watching in a separate goroutine
	go rb.watchLoop()

	return nil
}

// HealthCheck performs a health check
func (rb *RedisBackend) HealthCheck(ctx context.Context) error {
	// Test basic connectivity
	if err := rb.client.Ping(ctx).Err(); err != nil {
		return common.ErrHealthCheckFailed("redis-election", err)
	}

	// Test key operations
	testKey := fmt.Sprintf("forge:cron:health:%s", rb.nodeID)
	if err := rb.client.Set(ctx, testKey, "test", time.Second).Err(); err != nil {
		return common.ErrHealthCheckFailed("redis-election", err)
	}

	if err := rb.client.Del(ctx, testKey).Err(); err != nil {
		return common.ErrHealthCheckFailed("redis-election", err)
	}

	return nil
}

// watchLoop runs the watch loop
func (rb *RedisBackend) watchLoop() {
	pubsub := rb.client.Subscribe(rb.watchContext, rb.watchKey)
	defer pubsub.Close()

	// Initial leader check
	if leader, err := rb.GetLeader(rb.watchContext); err == nil {
		rb.watchCallback(leader)
	}

	// Listen for changes
	ch := pubsub.Channel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rb.watchContext.Done():
			return
		case msg := <-ch:
			if msg != nil {
				rb.watchCallback(msg.Payload)
			}
		case <-ticker.C:
			// Periodic leader check in case we miss notifications
			if leader, err := rb.GetLeader(rb.watchContext); err == nil {
				rb.watchCallback(leader)
			}
		}
	}
}

// publishLeadershipChange publishes a leadership change event
func (rb *RedisBackend) publishLeadershipChange(ctx context.Context, leaderID string) {
	if err := rb.client.Publish(ctx, rb.watchKey, leaderID).Err(); err != nil {
		if rb.logger != nil {
			rb.logger.Error("failed to publish leadership change", logger.Error(err))
		}
	}
}

// RedisClusterBackend implements leader election using Redis Cluster
type RedisClusterBackend struct {
	config    *Config
	client    *redis.ClusterClient
	leaderKey string
	watchKey  string
	nodeID    string
	clusterID string

	// Watch state
	watchCallback func(leaderID string)
	watchContext  context.Context
	watchCancel   context.CancelFunc

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// NewRedisClusterBackend creates a new Redis Cluster backend for leader election
func NewRedisClusterBackend(config *Config, logger common.Logger, metrics common.Metrics) (Backend, error) {
	// Parse Redis cluster config
	var addrs []string
	if config.BackendConfig != nil {
		if addresses, ok := config.BackendConfig["addresses"].([]string); ok {
			addrs = addresses
		}
	}

	if len(addrs) == 0 {
		addrs = []string{"localhost:6379"}
	}

	// Create Redis cluster client
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    addrs,
		Password: "",
		PoolSize: 10,
	})

	backend := &RedisClusterBackend{
		config:    config,
		client:    client,
		leaderKey: fmt.Sprintf("forge:cron:leader:%s", config.ClusterID),
		watchKey:  fmt.Sprintf("forge:cron:leader:watch:%s", config.ClusterID),
		nodeID:    config.NodeID,
		clusterID: config.ClusterID,
		logger:    logger,
		metrics:   metrics,
	}

	return backend, nil
}

// Start starts the Redis Cluster backend
func (rcb *RedisClusterBackend) Start(ctx context.Context) error {
	// Test connection
	if err := rcb.client.Ping(ctx).Err(); err != nil {
		return common.ErrServiceStartFailed("redis-cluster-election", err)
	}

	if rcb.logger != nil {
		rcb.logger.Info("redis cluster election backend started")
	}

	if rcb.metrics != nil {
		rcb.metrics.Counter("forge.cron.election.redis_cluster_started").Inc()
	}

	return nil
}

// Stop stops the Redis Cluster backend
func (rcb *RedisClusterBackend) Stop(ctx context.Context) error {
	// Cancel watch if active
	if rcb.watchCancel != nil {
		rcb.watchCancel()
	}

	// Close client
	if err := rcb.client.Close(); err != nil {
		return common.ErrServiceStopFailed("redis-cluster-election", err)
	}

	if rcb.logger != nil {
		rcb.logger.Info("redis cluster election backend stopped")
	}

	if rcb.metrics != nil {
		rcb.metrics.Counter("forge.cron.election.redis_cluster_stopped").Inc()
	}

	return nil
}

// Campaign attempts to become leader (Redis Cluster implementation)
func (rcb *RedisClusterBackend) Campaign(ctx context.Context, nodeID string, ttl time.Duration) error {
	result := rcb.client.SetNX(ctx, rcb.leaderKey, nodeID, ttl)
	if err := result.Err(); err != nil {
		return common.ErrContainerError("campaign", err)
	}

	success := result.Val()
	if !success {
		return common.ErrContainerError("campaign", fmt.Errorf("failed to acquire leadership"))
	}

	// Publish leadership change
	rcb.publishLeadershipChange(ctx, nodeID)

	if rcb.logger != nil {
		rcb.logger.Info("successfully campaigned for leadership",
			logger.String("node_id", nodeID),
			logger.Duration("ttl", ttl),
		)
	}

	if rcb.metrics != nil {
		rcb.metrics.Counter("forge.cron.election.campaigns_success").Inc()
	}

	return nil
}

// Resign resigns from leadership (Redis Cluster implementation)
func (rcb *RedisClusterBackend) Resign(ctx context.Context, nodeID string) error {
	script := redis.NewScript(`
		local current = redis.call('GET', KEYS[1])
		if current == ARGV[1] then
			redis.call('DEL', KEYS[1])
			return 1
		else
			return 0
		end
	`)

	result := script.Run(ctx, rcb.client, []string{rcb.leaderKey}, nodeID)
	if err := result.Err(); err != nil {
		return common.ErrContainerError("resign", err)
	}

	success := result.Val().(int64)
	if success == 0 {
		return common.ErrContainerError("resign", fmt.Errorf("not the current leader"))
	}

	// Publish leadership change
	rcb.publishLeadershipChange(ctx, "")

	if rcb.logger != nil {
		rcb.logger.Info("successfully resigned from leadership", logger.String("node_id", nodeID))
	}

	if rcb.metrics != nil {
		rcb.metrics.Counter("forge.cron.election.resignations").Inc()
	}

	return nil
}

// GetLeader returns the current leader (Redis Cluster implementation)
func (rcb *RedisClusterBackend) GetLeader(ctx context.Context) (string, error) {
	result := rcb.client.Get(ctx, rcb.leaderKey)
	if err := result.Err(); err != nil {
		if err == redis.Nil {
			return "", nil // No leader
		}
		return "", common.ErrContainerError("get_leader", err)
	}

	return result.Val(), nil
}

// IsLeader checks if the given node is the leader (Redis Cluster implementation)
func (rcb *RedisClusterBackend) IsLeader(ctx context.Context, nodeID string) (bool, error) {
	leader, err := rcb.GetLeader(ctx)
	if err != nil {
		return false, err
	}

	return leader == nodeID, nil
}

// Heartbeat extends the leadership TTL (Redis Cluster implementation)
func (rcb *RedisClusterBackend) Heartbeat(ctx context.Context, nodeID string, ttl time.Duration) error {
	script := redis.NewScript(`
		local current = redis.call('GET', KEYS[1])
		if current == ARGV[1] then
			redis.call('EXPIRE', KEYS[1], ARGV[2])
			return 1
		else
			return 0
		end
	`)

	result := script.Run(ctx, rcb.client, []string{rcb.leaderKey}, nodeID, int(ttl.Seconds()))
	if err := result.Err(); err != nil {
		return common.ErrContainerError("heartbeat", err)
	}

	success := result.Val().(int64)
	if success == 0 {
		return common.ErrContainerError("heartbeat", fmt.Errorf("not the current leader"))
	}

	if rcb.metrics != nil {
		rcb.metrics.Counter("forge.cron.election.heartbeats_sent").Inc()
	}

	return nil
}

// Watch watches for leadership changes (Redis Cluster implementation)
func (rcb *RedisClusterBackend) Watch(ctx context.Context, callback func(leaderID string)) error {
	if callback == nil {
		return common.ErrValidationError("callback", fmt.Errorf("callback cannot be nil"))
	}

	rcb.watchCallback = callback
	rcb.watchContext, rcb.watchCancel = context.WithCancel(ctx)

	// Start watching in a separate goroutine
	go rcb.watchLoop()

	return nil
}

// HealthCheck performs a health check (Redis Cluster implementation)
func (rcb *RedisClusterBackend) HealthCheck(ctx context.Context) error {
	// Test basic connectivity
	if err := rcb.client.Ping(ctx).Err(); err != nil {
		return common.ErrHealthCheckFailed("redis-cluster-election", err)
	}

	// Test key operations
	testKey := fmt.Sprintf("forge:cron:health:%s", rcb.nodeID)
	if err := rcb.client.Set(ctx, testKey, "test", time.Second).Err(); err != nil {
		return common.ErrHealthCheckFailed("redis-cluster-election", err)
	}

	if err := rcb.client.Del(ctx, testKey).Err(); err != nil {
		return common.ErrHealthCheckFailed("redis-cluster-election", err)
	}

	return nil
}

// watchLoop runs the watch loop (Redis Cluster implementation)
func (rcb *RedisClusterBackend) watchLoop() {
	pubsub := rcb.client.Subscribe(rcb.watchContext, rcb.watchKey)
	defer pubsub.Close()

	// Initial leader check
	if leader, err := rcb.GetLeader(rcb.watchContext); err == nil {
		rcb.watchCallback(leader)
	}

	// Listen for changes
	ch := pubsub.Channel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rcb.watchContext.Done():
			return
		case msg := <-ch:
			if msg != nil {
				rcb.watchCallback(msg.Payload)
			}
		case <-ticker.C:
			// Periodic leader check in case we miss notifications
			if leader, err := rcb.GetLeader(rcb.watchContext); err == nil {
				rcb.watchCallback(leader)
			}
		}
	}
}

// publishLeadershipChange publishes a leadership change event (Redis Cluster implementation)
func (rcb *RedisClusterBackend) publishLeadershipChange(ctx context.Context, leaderID string) {
	if err := rcb.client.Publish(ctx, rcb.watchKey, leaderID).Err(); err != nil {
		if rcb.logger != nil {
			rcb.logger.Error("failed to publish leadership change", logger.Error(err))
		}
	}
}
