package database

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// RedisDatabase wraps go-redis client with support for standalone, cluster, and sentinel modes.
type RedisDatabase struct {
	name   string
	config DatabaseConfig

	// Native access - use UniversalClient for flexibility across modes
	client redis.UniversalClient

	// Connection mode detection
	mode RedisMode

	// Connection state
	state atomic.Int32

	logger  forge.Logger
	metrics forge.Metrics
}

// RedisMode represents the Redis connection mode.
type RedisMode string

const (
	RedisModeStandalone RedisMode = "standalone"
	RedisModeCluster    RedisMode = "cluster"
	RedisModeSentinel   RedisMode = "sentinel"
)

// NewRedisDatabase creates a new Redis database instance.
func NewRedisDatabase(config DatabaseConfig, logger forge.Logger, metrics forge.Metrics) (*RedisDatabase, error) {
	db := &RedisDatabase{
		name:    config.Name,
		config:  config,
		logger:  logger,
		metrics: metrics,
	}
	db.state.Store(int32(StateDisconnected))

	return db, nil
}

// Open establishes the Redis connection with retry logic.
func (d *RedisDatabase) Open(ctx context.Context) error {
	d.state.Store(int32(StateConnecting))

	var lastErr error

	for attempt := 0; attempt <= d.config.MaxRetries; attempt++ {
		if attempt > 0 {
			d.state.Store(int32(StateReconnecting))
			// Apply exponential backoff with cap
			delay := min(d.config.RetryDelay*time.Duration(1<<uint(attempt-1)), 30*time.Second)

			d.logger.Info("retrying redis connection",
				logger.String("name", d.name),
				logger.Int("attempt", attempt+1),
				logger.Int("max_attempts", d.config.MaxRetries+1),
				logger.Duration("delay", delay),
			)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				d.state.Store(int32(StateError))

				return ctx.Err()
			}
		}

		if err := d.openAttempt(ctx); err != nil {
			lastErr = err
			d.logger.Warn("redis connection attempt failed",
				logger.String("name", d.name),
				logger.Int("attempt", attempt+1),
				logger.Error(err),
			)

			continue
		}

		d.state.Store(int32(StateConnected))
		d.logger.Info("redis connected",
			logger.String("name", d.name),
			logger.String("mode", string(d.mode)),
			logger.String("dsn", MaskDSN(d.config.DSN, TypeRedis)),
			logger.Int("pool_size", d.config.MaxOpenConns),
			logger.Int("attempts", attempt+1),
		)

		return nil
	}

	d.state.Store(int32(StateError))

	return ErrConnectionFailed(d.name, TypeRedis, fmt.Errorf("failed after %d attempts: %w", d.config.MaxRetries+1, lastErr))
}

// openAttempt performs a single connection attempt.
func (d *RedisDatabase) openAttempt(ctx context.Context) error {
	// Add timeout for connection
	connectCtx := ctx

	if d.config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc

		connectCtx, cancel = context.WithTimeout(ctx, d.config.ConnectionTimeout)
		defer cancel()
	}

	// Parse DSN and determine mode
	opts, mode, err := d.parseOptions()
	if err != nil {
		return fmt.Errorf("failed to parse redis options: %w", err)
	}

	d.mode = mode

	// Create universal client (handles all modes)
	client := redis.NewUniversalClient(opts)

	// Add hooks for observability
	client.AddHook(d.commandHook())

	// Verify connection
	if err := client.Ping(connectCtx).Err(); err != nil {
		client.Close()

		return fmt.Errorf("failed to ping redis: %w", err)
	}

	d.client = client

	return nil
}

// Close closes the Redis connection.
func (d *RedisDatabase) Close(ctx context.Context) error {
	if d.client != nil {
		err := d.client.Close()
		if err != nil {
			d.state.Store(int32(StateError))

			return ErrConnectionFailed(d.name, TypeRedis, fmt.Errorf("failed to close: %w", err))
		}

		d.state.Store(int32(StateDisconnected))
		d.logger.Info("redis closed", logger.String("name", d.name))

		return nil
	}

	return nil
}

// Ping checks Redis connectivity.
func (d *RedisDatabase) Ping(ctx context.Context) error {
	if d.client == nil {
		return ErrDatabaseNotOpened(d.name)
	}

	// Add timeout if configured
	pingCtx := ctx

	if d.config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc

		pingCtx, cancel = context.WithTimeout(ctx, d.config.ConnectionTimeout)
		defer cancel()
	}

	return d.client.Ping(pingCtx).Err()
}

// IsOpen returns whether the database is connected.
func (d *RedisDatabase) IsOpen() bool {
	return d.State() == StateConnected
}

// State returns the current connection state.
func (d *RedisDatabase) State() ConnectionState {
	return ConnectionState(d.state.Load())
}

// Name returns the database name.
func (d *RedisDatabase) Name() string {
	return d.name
}

// Type returns the database type.
func (d *RedisDatabase) Type() DatabaseType {
	return TypeRedis
}

// Driver returns the redis.UniversalClient for native driver access.
func (d *RedisDatabase) Driver() any {
	return d.client
}

// Client returns the Redis universal client.
func (d *RedisDatabase) Client() redis.UniversalClient {
	return d.client
}

// Health returns the health status.
func (d *RedisDatabase) Health(ctx context.Context) HealthStatus {
	start := time.Now()

	status := HealthStatus{
		CheckedAt: time.Now(),
	}

	if err := d.Ping(ctx); err != nil {
		status.Healthy = false
		status.Message = err.Error()

		return status
	}

	status.Healthy = true
	status.Message = "ok"
	status.Latency = time.Since(start)

	return status
}

// Stats returns connection pool statistics.
func (d *RedisDatabase) Stats() DatabaseStats {
	if d.client == nil {
		return DatabaseStats{}
	}

	stats := d.client.PoolStats()

	return DatabaseStats{
		OpenConnections:   int(stats.TotalConns),
		InUse:             int(stats.TotalConns - stats.IdleConns),
		Idle:              int(stats.IdleConns),
		WaitCount:         int64(stats.Timeouts),
		MaxIdleClosed:     int64(stats.StaleConns),
		MaxLifetimeClosed: int64(stats.StaleConns),
	}
}

// Pipeline creates a Redis pipeline for batching commands.
func (d *RedisDatabase) Pipeline() redis.Pipeliner {
	return d.client.Pipeline()
}

// TxPipeline creates a Redis transaction pipeline (MULTI/EXEC).
func (d *RedisDatabase) TxPipeline() redis.Pipeliner {
	return d.client.TxPipeline()
}

// Transaction executes a function in a Redis transaction with panic recovery.
// Uses WATCH/MULTI/EXEC pattern for optimistic locking.
func (d *RedisDatabase) Transaction(ctx context.Context, watchKeys []string, fn func(tx *redis.Tx) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrPanicRecovered(d.name, TypeRedis, r)
			d.logger.Error("panic recovered in transaction",
				logger.String("db", d.name),
				logger.Any("panic", r),
			)

			if d.metrics != nil {
				d.metrics.Counter("db_transaction_panics",
					"db", d.name,
				).Inc()
			}
		}
	}()

	// Use WATCH-based transactions
	err = d.client.Watch(ctx, func(tx *redis.Tx) error {
		return fn(tx)
	}, watchKeys...)

	return err
}

// Pipelined executes commands in a pipeline with panic recovery.
func (d *RedisDatabase) Pipelined(ctx context.Context, fn func(pipe redis.Pipeliner) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrPanicRecovered(d.name, TypeRedis, r)
			d.logger.Error("panic recovered in pipeline",
				logger.String("db", d.name),
				logger.Any("panic", r),
			)

			if d.metrics != nil {
				d.metrics.Counter("db_pipeline_panics",
					"db", d.name,
				).Inc()
			}
		}
	}()

	_, err = d.client.Pipelined(ctx, fn)

	return err
}

// TxPipelined executes commands in a transaction pipeline (MULTI/EXEC) with panic recovery.
func (d *RedisDatabase) TxPipelined(ctx context.Context, fn func(pipe redis.Pipeliner) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrPanicRecovered(d.name, TypeRedis, r)
			d.logger.Error("panic recovered in tx pipeline",
				logger.String("db", d.name),
				logger.Any("panic", r),
			)

			if d.metrics != nil {
				d.metrics.Counter("db_transaction_panics",
					"db", d.name,
				).Inc()
			}
		}
	}()

	_, err = d.client.TxPipelined(ctx, fn)

	return err
}

// Helper: Parse Redis options from DSN.
func (d *RedisDatabase) parseOptions() (*redis.UniversalOptions, RedisMode, error) {
	dsn := d.config.DSN

	// Default options
	opts := &redis.UniversalOptions{
		DialTimeout:   d.config.ConnectionTimeout,
		ReadTimeout:   d.config.QueryTimeout,
		WriteTimeout:  d.config.QueryTimeout,
		PoolSize:      d.config.MaxOpenConns,
		MinIdleConns:  d.config.MaxIdleConns,
		ConnMaxLifetime: d.config.ConnMaxLifetime,
		ConnMaxIdleTime: d.config.ConnMaxIdleTime,
		PoolTimeout:   d.config.ConnectionTimeout,
	}

	// Parse DSN
	mode := RedisModeStandalone

	// Handle redis-sentinel:// scheme
	if strings.HasPrefix(dsn, "redis-sentinel://") {
		mode = RedisModeSentinel

		if err := d.parseSentinelDSN(dsn, opts); err != nil {
			return nil, "", err
		}

		return opts, mode, nil
	}

	// Handle redis:// or rediss:// scheme
	if strings.HasPrefix(dsn, "redis://") || strings.HasPrefix(dsn, "rediss://") {
		if err := d.parseRedisDSN(dsn, opts); err != nil {
			return nil, "", err
		}

		// Detect cluster mode (multiple addresses)
		if len(opts.Addrs) > 1 {
			mode = RedisModeCluster
		}

		return opts, mode, nil
	}

	// Fallback: treat as host:port
	opts.Addrs = []string{dsn}

	return opts, mode, nil
}

// parseSentinelDSN parses redis-sentinel:// DSN format.
// Format: redis-sentinel://[user:password@]sentinel1:port1,sentinel2:port2/master-name/db
func (d *RedisDatabase) parseSentinelDSN(dsn string, opts *redis.UniversalOptions) error {
	// Remove scheme
	dsn = strings.TrimPrefix(dsn, "redis-sentinel://")

	// Extract auth
	var auth string

	if idx := strings.Index(dsn, "@"); idx != -1 {
		auth = dsn[:idx]
		dsn = dsn[idx+1:]

		// Parse username:password
		if colonIdx := strings.Index(auth, ":"); colonIdx != -1 {
			opts.Username = auth[:colonIdx]
			opts.Password = auth[colonIdx+1:]
		} else {
			opts.Password = auth
		}
	}

	// Split remaining: sentinels/master/db
	parts := strings.Split(dsn, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid sentinel DSN format: missing master name")
	}

	// Parse sentinel addresses
	sentinelAddrs := strings.Split(parts[0], ",")
	for i, addr := range sentinelAddrs {
		sentinelAddrs[i] = strings.TrimSpace(addr)
	}

	opts.Addrs = sentinelAddrs
	opts.MasterName = parts[1]

	// Parse DB if present
	if len(parts) > 2 && parts[2] != "" {
		var db int
		if _, err := fmt.Sscanf(parts[2], "%d", &db); err == nil {
			opts.DB = db
		}
	}

	return nil
}

// parseRedisDSN parses redis:// or rediss:// DSN format.
// Format: redis://[user:password@]host:port[,host2:port2]/db
func (d *RedisDatabase) parseRedisDSN(dsn string, opts *redis.UniversalOptions) error {
	// Determine TLS
	if strings.HasPrefix(dsn, "rediss://") {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		dsn = strings.TrimPrefix(dsn, "rediss://")
	} else {
		dsn = strings.TrimPrefix(dsn, "redis://")
	}

	// Extract auth
	var auth string

	if idx := strings.Index(dsn, "@"); idx != -1 {
		auth = dsn[:idx]
		dsn = dsn[idx+1:]

		// Parse username:password
		if colonIdx := strings.Index(auth, ":"); colonIdx != -1 {
			opts.Username = auth[:colonIdx]
			opts.Password = auth[colonIdx+1:]
		} else {
			opts.Password = auth
		}
	}

	// Split host(s) and path
	var hostPart, pathPart string

	if idx := strings.Index(dsn, "/"); idx != -1 {
		hostPart = dsn[:idx]
		pathPart = dsn[idx+1:]
	} else {
		hostPart = dsn
	}

	// Parse addresses (may be comma-separated for cluster)
	addrs := strings.Split(hostPart, ",")
	for i, addr := range addrs {
		addr = strings.TrimSpace(addr)
		// Add default port if missing
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, "6379")
		}

		addrs[i] = addr
	}

	opts.Addrs = addrs

	// Parse DB number from path
	if pathPart != "" {
		var db int
		if _, err := fmt.Sscanf(pathPart, "%d", &db); err == nil {
			opts.DB = db
		}
	}

	return nil
}

// Helper: Command hook for observability.
func (d *RedisDatabase) commandHook() redis.Hook {
	return &RedisCommandHook{
		logger:             d.logger,
		metrics:            d.metrics,
		dbName:             d.name,
		slowQueryThreshold: d.config.SlowQueryThreshold,
	}
}

// RedisCommandHook provides observability for Redis commands.
type RedisCommandHook struct {
	logger             forge.Logger
	metrics            forge.Metrics
	dbName             string
	slowQueryThreshold time.Duration
}

// DialHook is called when a new connection is established.
func (h *RedisCommandHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

// ProcessHook is called for each command.
func (h *RedisCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()

		// Execute command
		err := next(ctx, cmd)

		duration := time.Since(start)

		// Log slow commands
		if duration > h.slowQueryThreshold {
			h.logger.Warn("slow redis command",
				logger.String("db", h.dbName),
				logger.String("command", cmd.Name()),
				logger.String("args", fmt.Sprint(cmd.Args())),
				logger.Duration("duration", duration),
				logger.Duration("threshold", h.slowQueryThreshold),
			)
		}

		// Record metrics
		if h.metrics != nil {
			h.metrics.Histogram("db_command_duration",
				"db", h.dbName,
				"command", cmd.Name(),
			).Observe(duration.Seconds())

			if err != nil && err != redis.Nil {
				h.metrics.Counter("db_command_errors",
					"db", h.dbName,
					"command", cmd.Name(),
				).Inc()

				h.logger.Error("redis command error",
					logger.String("db", h.dbName),
					logger.String("command", cmd.Name()),
					logger.Error(err),
				)
			}
		}

		return err
	}
}

// ProcessPipelineHook is called for pipelined commands.
func (h *RedisCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()

		// Execute pipeline
		err := next(ctx, cmds)

		duration := time.Since(start)

		// Log slow pipelines
		if duration > h.slowQueryThreshold {
			h.logger.Warn("slow redis pipeline",
				logger.String("db", h.dbName),
				logger.Int("commands", len(cmds)),
				logger.Duration("duration", duration),
				logger.Duration("threshold", h.slowQueryThreshold),
			)
		}

		// Record metrics
		if h.metrics != nil {
			h.metrics.Histogram("db_pipeline_duration",
				"db", h.dbName,
			).Observe(duration.Seconds())

			h.metrics.Histogram("db_pipeline_size",
				"db", h.dbName,
			).Observe(float64(len(cmds)))

			if err != nil {
				h.metrics.Counter("db_pipeline_errors",
					"db", h.dbName,
				).Inc()
			}
		}

		return err
	}
}

