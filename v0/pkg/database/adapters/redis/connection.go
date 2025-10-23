package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// RedisConnection implements database.Connection for Redis
type RedisConnection struct {
	*database.BaseConnection
	client *redis.Client
	dbNum  int
}

// NewRedisConnection creates a new Redis connection
func NewRedisConnection(name string, config *database.ConnectionConfig, client *redis.Client, logger common.Logger, metrics common.Metrics) database.Connection {
	baseConn := database.NewBaseConnection(name, "redis", config, logger, metrics)

	dbNum := 0
	if db, exists := config.Config["db"]; exists {
		if dbNumber, ok := db.(int); ok {
			dbNum = dbNumber
		}
	}

	conn := &RedisConnection{
		BaseConnection: baseConn,
		client:         client,
		dbNum:          dbNum,
	}

	// Set the underlying DB in base connection
	baseConn.SetDB(client)

	return conn
}

// Connect establishes the Redis connection
func (rc *RedisConnection) Connect(ctx context.Context) error {
	if rc.client == nil {
		return fmt.Errorf("Redis client is not initialized")
	}

	// Test the connection with a ping
	start := time.Now()
	if err := rc.client.Ping(ctx).Err(); err != nil {
		rc.IncrementErrorCount()
		return fmt.Errorf("failed to ping Redis server: %w", err)
	}

	// Verify we can select the correct database
	if rc.dbNum != 0 {
		if err := rc.client.Do(ctx, "SELECT", rc.dbNum).Err(); err != nil {
			rc.IncrementErrorCount()
			return fmt.Errorf("failed to select Redis database %d: %w", rc.dbNum, err)
		}
	}

	// Mark as connected
	rc.SetConnected(true)

	// Record connection metrics
	duration := time.Since(start)
	if rc.Metrics() != nil {
		rc.Metrics().Counter("forge.database.redis.connections_created", "connection", rc.Name()).Inc()
		rc.Metrics().Histogram("forge.database.redis.connection_duration", "connection", rc.Name()).Observe(duration.Seconds())
	}

	rc.Logger().Info("Redis connection established",
		logger.String("connection", rc.Name()),
		logger.String("host", rc.Config().Host),
		logger.Int("port", rc.Config().Port),
		logger.Int("db", rc.dbNum),
		logger.Duration("connection_time", duration),
	)

	return nil
}

// Close closes the Redis connection
func (rc *RedisConnection) Close(ctx context.Context) error {
	if rc.client == nil {
		return nil
	}

	rc.Logger().Info("closing Redis connection",
		logger.String("connection", rc.Name()),
	)

	start := time.Now()
	if err := rc.client.Close(); err != nil {
		rc.IncrementErrorCount()
		return fmt.Errorf("failed to close Redis connection: %w", err)
	}

	rc.SetConnected(false)

	// Record disconnection metrics
	duration := time.Since(start)
	if rc.Metrics() != nil {
		rc.Metrics().Counter("forge.database.redis.connections_closed", "connection", rc.Name()).Inc()
		rc.Metrics().Histogram("forge.database.redis.disconnection_duration", "connection", rc.Name()).Observe(duration.Seconds())
	}

	rc.Logger().Info("Redis connection closed",
		logger.String("connection", rc.Name()),
		logger.Duration("close_time", duration),
	)

	return nil
}

// Ping tests the Redis connection
func (rc *RedisConnection) Ping(ctx context.Context) error {
	if rc.client == nil {
		return fmt.Errorf("Redis client is not initialized")
	}

	if !rc.IsConnected() {
		return fmt.Errorf("Redis connection is not established")
	}

	start := time.Now()
	result, err := rc.client.Ping(ctx).Result()
	duration := time.Since(start)

	if err != nil {
		rc.IncrementErrorCount()
		if rc.Metrics() != nil {
			rc.Metrics().Counter("forge.database.redis.ping_errors", "connection", rc.Name()).Inc()
		}
		return fmt.Errorf("Redis ping failed: %w", err)
	}

	if result != "PONG" {
		rc.IncrementErrorCount()
		return fmt.Errorf("unexpected ping response: %s", result)
	}

	// Record ping metrics
	if rc.Metrics() != nil {
		rc.Metrics().Counter("forge.database.redis.pings", "connection", rc.Name()).Inc()
		rc.Metrics().Histogram("forge.database.redis.ping_duration", "connection", rc.Name()).Observe(duration.Seconds())
	}

	return nil
}

// Transaction executes a function within a Redis transaction (using MULTI/EXEC)
func (rc *RedisConnection) Transaction(ctx context.Context, fn func(tx interface{}) error) error {
	if rc.client == nil {
		return fmt.Errorf("Redis client is not initialized")
	}

	if !rc.IsConnected() {
		return fmt.Errorf("Redis connection is not established")
	}

	start := time.Now()
	rc.IncrementTransactionCount()

	// Redis transactions are different from SQL transactions
	// We use pipelining with MULTI/EXEC for atomic operations
	pipe := rc.client.TxPipeline()

	// Execute the function with the pipeline
	err := fn(pipe)
	if err != nil {
		// Discard the pipeline
		pipe.Discard()
		rc.IncrementErrorCount()

		if rc.Metrics() != nil {
			rc.Metrics().Counter("forge.database.redis.transaction_errors", "connection", rc.Name()).Inc()
		}

		return fmt.Errorf("transaction function failed: %w", err)
	}

	// Execute the pipeline
	_, execErr := pipe.Exec(ctx)
	duration := time.Since(start)

	if execErr != nil {
		rc.IncrementErrorCount()
		if rc.Metrics() != nil {
			rc.Metrics().Counter("forge.database.redis.transaction_errors", "connection", rc.Name()).Inc()
		}
		return fmt.Errorf("failed to execute Redis transaction: %w", execErr)
	}

	// Record transaction metrics
	if rc.Metrics() != nil {
		rc.Metrics().Counter("forge.database.redis.transactions_success", "connection", rc.Name()).Inc()
		rc.Metrics().Histogram("forge.database.redis.transaction_duration", "connection", rc.Name()).Observe(duration.Seconds())
	}

	rc.Logger().Debug("Redis transaction completed",
		logger.String("connection", rc.Name()),
		logger.Duration("duration", duration),
	)

	return nil
}

// GetRedisClient returns the underlying Redis client
func (rc *RedisConnection) GetRedisClient() *redis.Client {
	return rc.client
}

// Stats returns enhanced Redis connection statistics
func (rc *RedisConnection) Stats() database.ConnectionStats {
	baseStats := rc.BaseConnection.Stats()

	// Add Redis-specific statistics
	if rc.client != nil {
		poolStats := rc.client.PoolStats()
		baseStats.OpenConnections = int(poolStats.TotalConns)
		baseStats.IdleConnections = int(poolStats.IdleConns)
		// Note: PoolStats doesn't provide all the metrics we want,
		// but we include what's available
	}

	return baseStats
}

// RedisTransaction wraps the Redis pipeline for transaction operations
type RedisTransaction struct {
	pipeline redis.Pipeliner
	conn     *RedisConnection
}

// NewRedisTransaction creates a new Redis transaction wrapper
func NewRedisTransaction(pipeline redis.Pipeliner, conn *RedisConnection) *RedisTransaction {
	return &RedisTransaction{
		pipeline: pipeline,
		conn:     conn,
	}
}

// Pipeline returns the underlying Redis pipeline
func (rt *RedisTransaction) Pipeline() redis.Pipeliner {
	return rt.pipeline
}

// Set executes a SET command within the transaction
func (rt *RedisTransaction) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return rt.pipeline.Set(ctx, key, value, expiration).Err()
}

// Get executes a GET command within the transaction
func (rt *RedisTransaction) Get(ctx context.Context, key string) (string, error) {
	return rt.pipeline.Get(ctx, key).Result()
}

// Del executes a DEL command within the transaction
func (rt *RedisTransaction) Del(ctx context.Context, keys ...string) error {
	return rt.pipeline.Del(ctx, keys...).Err()
}

// Exists executes an EXISTS command within the transaction
func (rt *RedisTransaction) Exists(ctx context.Context, keys ...string) (int64, error) {
	return rt.pipeline.Exists(ctx, keys...).Result()
}

// Incr executes an INCR command within the transaction
func (rt *RedisTransaction) Incr(ctx context.Context, key string) (int64, error) {
	return rt.pipeline.Incr(ctx, key).Result()
}

// Expire executes an EXPIRE command within the transaction
func (rt *RedisTransaction) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return rt.pipeline.Expire(ctx, key, expiration).Err()
}

// RedisOperations provides common Redis operations
type RedisOperations struct {
	client *redis.Client
	conn   *RedisConnection
}

// NewRedisOperations creates a new Redis operations helper
func NewRedisOperations(conn *RedisConnection) *RedisOperations {
	return &RedisOperations{
		client: conn.client,
		conn:   conn,
	}
}

// Set sets a key-value pair with optional expiration
func (ro *RedisOperations) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	err := ro.client.Set(ctx, key, value, expiration).Err()
	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "SET",
			).Inc()
		}
		return fmt.Errorf("Redis SET failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "SET",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "SET",
		).Observe(duration.Seconds())
	}

	return nil
}

// Get retrieves a value by key
func (ro *RedisOperations) Get(ctx context.Context, key string) (string, error) {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	result, err := ro.client.Get(ctx, key).Result()
	duration := time.Since(start)

	if err != nil {
		if err == redis.Nil {
			// Key not found is not considered an error for metrics
			if ro.conn.Metrics() != nil {
				ro.conn.Metrics().Counter("forge.database.redis.commands",
					"connection", ro.conn.Name(),
					"command", "GET",
				).Inc()
				ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
					"connection", ro.conn.Name(),
					"command", "GET",
				).Observe(duration.Seconds())
			}
			return "", err // Return redis.Nil as-is for caller to handle
		}

		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "GET",
			).Inc()
		}
		return "", fmt.Errorf("Redis GET failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "GET",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "GET",
		).Observe(duration.Seconds())
	}

	return result, nil
}

// Del deletes one or more keys
func (ro *RedisOperations) Del(ctx context.Context, keys ...string) (int64, error) {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	result, err := ro.client.Del(ctx, keys...).Result()
	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "DEL",
			).Inc()
		}
		return 0, fmt.Errorf("Redis DEL failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "DEL",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "DEL",
		).Observe(duration.Seconds())
	}

	return result, nil
}

// Exists checks if one or more keys exist
func (ro *RedisOperations) Exists(ctx context.Context, keys ...string) (int64, error) {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	result, err := ro.client.Exists(ctx, keys...).Result()
	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "EXISTS",
			).Inc()
		}
		return 0, fmt.Errorf("Redis EXISTS failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "EXISTS",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "EXISTS",
		).Observe(duration.Seconds())
	}

	return result, nil
}

// Incr increments a key's integer value
func (ro *RedisOperations) Incr(ctx context.Context, key string) (int64, error) {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	result, err := ro.client.Incr(ctx, key).Result()
	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "INCR",
			).Inc()
		}
		return 0, fmt.Errorf("Redis INCR failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "INCR",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "INCR",
		).Observe(duration.Seconds())
	}

	return result, nil
}

// Expire sets an expiration time for a key
func (ro *RedisOperations) Expire(ctx context.Context, key string, expiration time.Duration) error {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	err := ro.client.Expire(ctx, key, expiration).Err()
	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "EXPIRE",
			).Inc()
		}
		return fmt.Errorf("Redis EXPIRE failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "EXPIRE",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "EXPIRE",
		).Observe(duration.Seconds())
	}

	return nil
}

// FlushDB flushes the current database
func (ro *RedisOperations) FlushDB(ctx context.Context) error {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	err := ro.client.FlushDB(ctx).Err()
	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "FLUSHDB",
			).Inc()
		}
		return fmt.Errorf("Redis FLUSHDB failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "FLUSHDB",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "FLUSHDB",
		).Observe(duration.Seconds())
	}

	ro.conn.Logger().Info("Redis database flushed",
		logger.String("connection", ro.conn.Name()),
		logger.Duration("duration", duration),
	)

	return nil
}

// Info returns Redis server information
func (ro *RedisOperations) Info(ctx context.Context, sections ...string) (string, error) {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	var result string
	var err error

	if len(sections) > 0 {
		result, err = ro.client.Info(ctx, sections...).Result()
	} else {
		result, err = ro.client.Info(ctx).Result()
	}

	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "INFO",
			).Inc()
		}
		return "", fmt.Errorf("Redis INFO failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "INFO",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "INFO",
		).Observe(duration.Seconds())
	}

	return result, nil
}

// Keys returns all keys matching a pattern (use with caution in production)
func (ro *RedisOperations) Keys(ctx context.Context, pattern string) ([]string, error) {
	start := time.Now()
	ro.conn.IncrementQueryCount()

	result, err := ro.client.Keys(ctx, pattern).Result()
	duration := time.Since(start)

	if err != nil {
		ro.conn.IncrementErrorCount()
		if ro.conn.Metrics() != nil {
			ro.conn.Metrics().Counter("forge.database.redis.command_errors",
				"connection", ro.conn.Name(),
				"command", "KEYS",
			).Inc()
		}
		return nil, fmt.Errorf("Redis KEYS failed: %w", err)
	}

	// Record command metrics
	if ro.conn.Metrics() != nil {
		ro.conn.Metrics().Counter("forge.database.redis.commands",
			"connection", ro.conn.Name(),
			"command", "KEYS",
		).Inc()
		ro.conn.Metrics().Histogram("forge.database.redis.command_duration",
			"connection", ro.conn.Name(),
			"command", "KEYS",
		).Observe(duration.Seconds())
	}

	// Log warning for KEYS command as it can be dangerous in production
	if pattern == "*" || strings.Contains(pattern, "*") {
		ro.conn.Logger().Warn("KEYS command used with wildcard pattern - use with caution in production",
			logger.String("connection", ro.conn.Name()),
			logger.String("pattern", pattern),
			logger.Int("result_count", len(result)),
		)
	}

	return result, nil
}

// GetOperations returns a Redis operations helper for this connection
func (rc *RedisConnection) GetOperations() *RedisOperations {
	return NewRedisOperations(rc)
}

// ConnectionString returns a sanitized connection string
func (rc *RedisConnection) ConnectionString() string {
	config := rc.Config()
	if config.Password != "" {
		return fmt.Sprintf("redis://***@%s:%d/%d", config.Host, config.Port, rc.dbNum)
	}
	return fmt.Sprintf("redis://%s:%d/%d", config.Host, config.Port, rc.dbNum)
}
