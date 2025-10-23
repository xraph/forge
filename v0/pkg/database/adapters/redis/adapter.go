package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// RedisAdapter implements database.DatabaseAdapter for Redis
type RedisAdapter struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewRedisAdapter creates a new Redis adapter
func NewRedisAdapter(logger common.Logger, metrics common.Metrics) database.DatabaseAdapter {
	return &RedisAdapter{
		logger:  logger,
		metrics: metrics,
	}
}

// Name returns the adapter name
func (ra *RedisAdapter) Name() string {
	return "redis"
}

// SupportedTypes returns the database types supported by this adapter
func (ra *RedisAdapter) SupportedTypes() []string {
	return []string{"redis"}
}

// Connect creates a new Redis connection
func (ra *RedisAdapter) Connect(ctx context.Context, config *database.ConnectionConfig) (database.Connection, error) {
	if err := ra.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create Redis client options
	opts, err := ra.buildRedisOptions(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build Redis options: %w", err)
	}

	// Create Redis client
	client := redis.NewClient(opts)

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Create connection wrapper
	connectionName := fmt.Sprintf("redis_%s_%d", config.Host, config.Port)
	if dbNum, ok := config.Config["db"].(int); ok {
		connectionName = fmt.Sprintf("redis_%s_%d_%d", config.Host, config.Port, dbNum)
	}

	conn := NewRedisConnection(connectionName, config, client, ra.logger, ra.metrics)

	ra.logger.Info("Redis connection created",
		logger.String("connection", connectionName),
		logger.String("host", config.Host),
		logger.Int("port", config.Port),
		logger.Int("db", ra.getDB(config)),
	)

	return conn, nil
}

// ValidateConfig validates the Redis configuration
func (ra *RedisAdapter) ValidateConfig(config *database.ConnectionConfig) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	if config.Type != "redis" {
		return fmt.Errorf("invalid database type: %s", config.Type)
	}

	if config.Host == "" {
		return fmt.Errorf("host is required")
	}

	if config.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}

	// Validate database number
	if db, exists := config.Config["db"]; exists {
		if dbNum, ok := db.(int); ok {
			if dbNum < 0 || dbNum > 15 {
				return fmt.Errorf("Redis database number must be between 0 and 15, got %d", dbNum)
			}
		} else {
			return fmt.Errorf("Redis database number must be an integer")
		}
	}

	// Validate pool configuration
	if config.Pool.MaxOpenConns <= 0 {
		return fmt.Errorf("max open connections must be greater than 0")
	}

	if config.Pool.MaxIdleConns < 0 {
		return fmt.Errorf("max idle connections cannot be negative")
	}

	if config.Pool.MaxIdleConns > config.Pool.MaxOpenConns {
		return fmt.Errorf("max idle connections cannot be greater than max open connections")
	}

	return nil
}

// SupportsMigrations returns false as Redis doesn't support traditional migrations
func (ra *RedisAdapter) SupportsMigrations() bool {
	return false
}

// Migrate is not supported for Redis
func (ra *RedisAdapter) Migrate(ctx context.Context, conn database.Connection, migrationsPath string) error {
	return fmt.Errorf("migrations are not supported for Redis")
}

// HealthCheck performs a health check on the Redis connection
func (ra *RedisAdapter) HealthCheck(ctx context.Context, conn database.Connection) error {
	redisConn, ok := conn.(*RedisConnection)
	if !ok {
		return fmt.Errorf("connection is not a Redis connection")
	}

	client := redisConn.GetRedisClient()

	// Ping the Redis server
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	// Check if we can execute basic commands
	testKey := "forge:healthcheck:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	testValue := "ok"

	// Set and get a test key
	if err := client.Set(ctx, testKey, testValue, time.Minute).Err(); err != nil {
		return fmt.Errorf("failed to set test key: %w", err)
	}

	result, err := client.Get(ctx, testKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get test key: %w", err)
	}

	if result != testValue {
		return fmt.Errorf("unexpected test value: expected %s, got %s", testValue, result)
	}

	// Clean up test key
	client.Del(ctx, testKey)

	return nil
}

// buildRedisOptions builds Redis client options from configuration
func (ra *RedisAdapter) buildRedisOptions(config *database.ConnectionConfig) (*redis.Options, error) {
	opts := &redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       ra.getDB(config),

		// Connection pool settings
		PoolSize:        config.Pool.MaxOpenConns,
		MinIdleConns:    config.Pool.MaxIdleConns,
		MaxIdleConns:    config.Pool.MaxIdleConns,
		ConnMaxLifetime: config.Pool.ConnMaxLifetime,
		ConnMaxIdleTime: config.Pool.ConnMaxIdleTime,

		// Default timeouts
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,

		// Retry settings
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
	}

	// Apply configuration-specific settings
	if dialTimeout, ok := config.Config["dial_timeout"].(time.Duration); ok {
		opts.DialTimeout = dialTimeout
	}
	if readTimeout, ok := config.Config["read_timeout"].(time.Duration); ok {
		opts.ReadTimeout = readTimeout
	}
	if writeTimeout, ok := config.Config["write_timeout"].(time.Duration); ok {
		opts.WriteTimeout = writeTimeout
	}
	if poolTimeout, ok := config.Config["pool_timeout"].(time.Duration); ok {
		opts.PoolTimeout = poolTimeout
	}
	if maxRetries, ok := config.Config["max_retries"].(int); ok {
		opts.MaxRetries = maxRetries
	}
	if minRetryBackoff, ok := config.Config["min_retry_backoff"].(time.Duration); ok {
		opts.MinRetryBackoff = minRetryBackoff
	}
	if maxRetryBackoff, ok := config.Config["max_retry_backoff"].(time.Duration); ok {
		opts.MaxRetryBackoff = maxRetryBackoff
	}

	return opts, nil
}

// getDB returns the database number from configuration
func (ra *RedisAdapter) getDB(config *database.ConnectionConfig) int {
	if db, exists := config.Config["db"]; exists {
		if dbNum, ok := db.(int); ok {
			return dbNum
		}
	}
	return 0 // Default to database 0
}

// RedisConfig contains Redis-specific configuration
type RedisConfig struct {
	*database.ConnectionConfig
	DB              int           `yaml:"db" json:"db"`
	DialTimeout     time.Duration `yaml:"dial_timeout" json:"dial_timeout"`
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	PoolTimeout     time.Duration `yaml:"pool_timeout" json:"pool_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	MaxRetries      int           `yaml:"max_retries" json:"max_retries"`
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff" json:"min_retry_backoff"`
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff" json:"max_retry_backoff"`
}

// NewRedisConfig creates a new Redis configuration
func NewRedisConfig() *RedisConfig {
	return &RedisConfig{
		ConnectionConfig: &database.ConnectionConfig{
			Type:     "redis",
			Host:     "localhost",
			Port:     6379,
			Password: "",
			Pool: database.PoolConfig{
				MaxOpenConns:    10,
				MaxIdleConns:    5,
				ConnMaxLifetime: time.Hour,
				ConnMaxIdleTime: 5 * time.Minute,
			},
			Retry: database.RetryConfig{
				MaxAttempts:   3,
				InitialDelay:  8 * time.Millisecond,
				MaxDelay:      512 * time.Millisecond,
				BackoffFactor: 2.0,
			},
		},
		DB:              0,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolTimeout:     4 * time.Second,
		IdleTimeout:     5 * time.Minute,
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
	}
}

// Apply applies Redis-specific configuration
func (rc *RedisConfig) Apply() {
	if rc.ConnectionConfig.Config == nil {
		rc.ConnectionConfig.Config = make(map[string]interface{})
	}

	rc.ConnectionConfig.Config["db"] = rc.DB
	rc.ConnectionConfig.Config["dial_timeout"] = rc.DialTimeout
	rc.ConnectionConfig.Config["read_timeout"] = rc.ReadTimeout
	rc.ConnectionConfig.Config["write_timeout"] = rc.WriteTimeout
	rc.ConnectionConfig.Config["pool_timeout"] = rc.PoolTimeout
	rc.ConnectionConfig.Config["idle_timeout"] = rc.IdleTimeout
	rc.ConnectionConfig.Config["max_retries"] = rc.MaxRetries
	rc.ConnectionConfig.Config["min_retry_backoff"] = rc.MinRetryBackoff
	rc.ConnectionConfig.Config["max_retry_backoff"] = rc.MaxRetryBackoff
}
