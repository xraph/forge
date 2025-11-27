package database

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestRedisDatabase_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	config := DatabaseConfig{
		Name:                "test-redis",
		Type:                TypeRedis,
		DSN:                 "redis://localhost:6379/0",
		MaxOpenConns:        10,
		MaxIdleConns:        5,
		ConnMaxLifetime:     5 * time.Minute,
		ConnMaxIdleTime:     5 * time.Minute,
		ConnectionTimeout:   5 * time.Second,
		QueryTimeout:        30 * time.Second,
		MaxRetries:          3,
		RetryDelay:          time.Second,
		SlowQueryThreshold:  100 * time.Millisecond,
		HealthCheckInterval: 30 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)
	require.NotNil(t, db)

	// Test identity
	assert.Equal(t, "test-redis", db.Name())
	assert.Equal(t, TypeRedis, db.Type())
	assert.Equal(t, StateDisconnected, db.State())
	assert.False(t, db.IsOpen())

	ctx := context.Background()

	// Open connection
	err = db.Open(ctx)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	defer db.Close(ctx)

	assert.Equal(t, StateConnected, db.State())
	assert.True(t, db.IsOpen())

	// Test ping
	err = db.Ping(ctx)
	assert.NoError(t, err)

	// Test health
	health := db.Health(ctx)
	assert.True(t, health.Healthy)
	assert.Equal(t, "ok", health.Message)
	assert.Greater(t, health.Latency, time.Duration(0))

	// Test stats
	stats := db.Stats()
	assert.GreaterOrEqual(t, stats.OpenConnections, 0)

	// Test driver access
	client := db.Client()
	assert.NotNil(t, client)

	driver := db.Driver()
	assert.NotNil(t, driver)
}

func TestRedisDatabase_Operations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	config := DatabaseConfig{
		Name:              "test-redis-ops",
		Type:              TypeRedis,
		DSN:               "redis://localhost:6379/1",
		MaxOpenConns:      10,
		ConnectionTimeout: 5 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	err = db.Open(ctx)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	defer db.Close(ctx)

	client := db.Client()

	// Test basic SET/GET
	err = client.Set(ctx, "test:key", "test-value", 10*time.Second).Err()
	assert.NoError(t, err)

	val, err := client.Get(ctx, "test:key").Result()
	assert.NoError(t, err)
	assert.Equal(t, "test-value", val)

	// Test DEL
	err = client.Del(ctx, "test:key").Err()
	assert.NoError(t, err)

	// Test INCR
	err = client.Set(ctx, "test:counter", "10", 10*time.Second).Err()
	assert.NoError(t, err)

	count, err := client.Incr(ctx, "test:counter").Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(11), count)

	// Cleanup
	client.Del(ctx, "test:counter")
}

func TestRedisDatabase_Pipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	config := DatabaseConfig{
		Name:              "test-redis-pipe",
		Type:              TypeRedis,
		DSN:               "redis://localhost:6379/2",
		MaxOpenConns:      10,
		ConnectionTimeout: 5 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	err = db.Open(ctx)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	defer db.Close(ctx)

	client := db.Client()

	// Test regular pipeline
	err = db.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, "test:pipe:key1", "value1", 10*time.Second)
		pipe.Set(ctx, "test:pipe:key2", "value2", 10*time.Second)
		pipe.Set(ctx, "test:pipe:key3", "value3", 10*time.Second)

		return nil
	})
	assert.NoError(t, err)

	// Verify values
	val, err := client.Get(ctx, "test:pipe:key1").Result()
	assert.NoError(t, err)
	assert.Equal(t, "value1", val)

	// Cleanup
	client.Del(ctx, "test:pipe:key1", "test:pipe:key2", "test:pipe:key3")
}

func TestRedisDatabase_Transaction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	config := DatabaseConfig{
		Name:              "test-redis-tx",
		Type:              TypeRedis,
		DSN:               "redis://localhost:6379/3",
		MaxOpenConns:      10,
		ConnectionTimeout: 5 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	err = db.Open(ctx)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	defer db.Close(ctx)

	client := db.Client()

	// Setup initial value
	err = client.Set(ctx, "test:tx:counter", "100", 10*time.Second).Err()
	assert.NoError(t, err)

	// Test transaction with WATCH
	err = db.Transaction(ctx, []string{"test:tx:counter"}, func(tx *redis.Tx) error {
		// Get current value
		val, err := tx.Get(ctx, "test:tx:counter").Result()
		if err != nil {
			return err
		}

		// Increment in transaction
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, "test:tx:counter", val+"1", 10*time.Second)

			return nil
		})

		return err
	})
	assert.NoError(t, err)

	// Verify result
	val, err := client.Get(ctx, "test:tx:counter").Result()
	assert.NoError(t, err)
	assert.Equal(t, "1001", val)

	// Cleanup
	client.Del(ctx, "test:tx:counter")
}

func TestRedisDatabase_TxPipelined(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	config := DatabaseConfig{
		Name:              "test-redis-txpipe",
		Type:              TypeRedis,
		DSN:               "redis://localhost:6379/4",
		MaxOpenConns:      10,
		ConnectionTimeout: 5 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	err = db.Open(ctx)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	defer db.Close(ctx)

	client := db.Client()

	// Test MULTI/EXEC transaction
	err = db.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, "test:txpipe:key1", "txvalue1", 10*time.Second)
		pipe.Set(ctx, "test:txpipe:key2", "txvalue2", 10*time.Second)
		pipe.Incr(ctx, "test:txpipe:counter")

		return nil
	})
	assert.NoError(t, err)

	// Verify values
	val1, err := client.Get(ctx, "test:txpipe:key1").Result()
	assert.NoError(t, err)
	assert.Equal(t, "txvalue1", val1)

	val2, err := client.Get(ctx, "test:txpipe:key2").Result()
	assert.NoError(t, err)
	assert.Equal(t, "txvalue2", val2)

	counter, err := client.Get(ctx, "test:txpipe:counter").Result()
	assert.NoError(t, err)
	assert.Equal(t, "1", counter)

	// Cleanup
	client.Del(ctx, "test:txpipe:key1", "test:txpipe:key2", "test:txpipe:counter")
}

func TestRedisDatabase_DSNParsing(t *testing.T) {
	tests := []struct {
		name        string
		dsn         string
		wantMode    RedisMode
		wantAddrs   []string
		wantDB      int
		wantErr     bool
		wantTLS     bool
		wantMaster  string
		wantUser    string
		wantPass    string
	}{
		{
			name:      "standalone simple",
			dsn:       "redis://localhost:6379/0",
			wantMode:  RedisModeStandalone,
			wantAddrs: []string{"localhost:6379"},
			wantDB:    0,
			wantErr:   false,
		},
		{
			name:      "standalone with auth",
			dsn:       "redis://user:password@localhost:6379/1",
			wantMode:  RedisModeStandalone,
			wantAddrs: []string{"localhost:6379"},
			wantDB:    1,
			wantUser:  "user",
			wantPass:  "password",
			wantErr:   false,
		},
		{
			name:      "standalone with password only",
			dsn:       "redis://:password@localhost:6379/2",
			wantMode:  RedisModeStandalone,
			wantAddrs: []string{"localhost:6379"},
			wantDB:    2,
			wantPass:  "password",
			wantErr:   false,
		},
		{
			name:      "standalone TLS",
			dsn:       "rediss://localhost:6379/0",
			wantMode:  RedisModeStandalone,
			wantAddrs: []string{"localhost:6379"},
			wantDB:    0,
			wantTLS:   true,
			wantErr:   false,
		},
		{
			name:      "cluster",
			dsn:       "redis://node1:6379,node2:6379,node3:6379/0",
			wantMode:  RedisModeCluster,
			wantAddrs: []string{"node1:6379", "node2:6379", "node3:6379"},
			wantDB:    0,
			wantErr:   false,
		},
		{
			name:       "sentinel",
			dsn:        "redis-sentinel://sentinel1:26379,sentinel2:26379/mymaster/0",
			wantMode:   RedisModeSentinel,
			wantAddrs:  []string{"sentinel1:26379", "sentinel2:26379"},
			wantMaster: "mymaster",
			wantDB:     0,
			wantErr:    false,
		},
		{
			name:       "sentinel with auth",
			dsn:        "redis-sentinel://user:password@sentinel1:26379,sentinel2:26379/mymaster/1",
			wantMode:   RedisModeSentinel,
			wantAddrs:  []string{"sentinel1:26379", "sentinel2:26379"},
			wantMaster: "mymaster",
			wantDB:     1,
			wantUser:   "user",
			wantPass:   "password",
			wantErr:    false,
		},
		{
			name:      "fallback host:port",
			dsn:       "localhost:6379",
			wantMode:  RedisModeStandalone,
			wantAddrs: []string{"localhost:6379"},
			wantDB:    0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DatabaseConfig{
				Name: "test-dsn",
				Type: TypeRedis,
				DSN:  tt.dsn,
			}

			log := logger.NewTestLogger()
			metrics := forge.NewNoOpMetrics()

			db, err := NewRedisDatabase(config, log, metrics)
			require.NoError(t, err)

			opts, mode, err := db.parseOptions()

			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantMode, mode)
			assert.Equal(t, tt.wantAddrs, opts.Addrs)
			assert.Equal(t, tt.wantDB, opts.DB)

			if tt.wantTLS {
				assert.NotNil(t, opts.TLSConfig)
			}

			if tt.wantMaster != "" {
				assert.Equal(t, tt.wantMaster, opts.MasterName)
			}

			if tt.wantUser != "" {
				assert.Equal(t, tt.wantUser, opts.Username)
			}

			if tt.wantPass != "" {
				assert.Equal(t, tt.wantPass, opts.Password)
			}
		})
	}
}

func TestRedisDatabase_ConnectionRetry(t *testing.T) {
	config := DatabaseConfig{
		Name:              "test-redis-retry",
		Type:              TypeRedis,
		DSN:               "redis://nonexistent:6379/0",
		MaxRetries:        2,
		RetryDelay:        100 * time.Millisecond,
		ConnectionTimeout: 1 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	start := time.Now()
	err = db.Open(ctx)
	duration := time.Since(start)

	// Should fail after retries
	assert.Error(t, err)
	assert.Equal(t, StateError, db.State())

	// Should have taken at least the retry delays
	// 2 retries with exponential backoff: 100ms + 200ms = 300ms minimum
	assert.Greater(t, duration, 200*time.Millisecond)
}

func TestRedisDatabase_CloseIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	config := DatabaseConfig{
		Name:              "test-redis-close",
		Type:              TypeRedis,
		DSN:               "redis://localhost:6379/5",
		ConnectionTimeout: 5 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	err = db.Open(ctx)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// Close multiple times should not error
	err = db.Close(ctx)
	assert.NoError(t, err)

	err = db.Close(ctx)
	assert.NoError(t, err)
}

func TestRedisDatabase_MaskDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "redis with password",
			dsn:  "redis://user:secret123@localhost:6379/0",
			want: "redis://user:***@localhost:6379/0",
		},
		{
			name: "redis sentinel with password",
			dsn:  "redis-sentinel://user:secret123@sentinel1:26379,sentinel2:26379/mymaster/0",
			want: "redis-sentinel://user:***@sentinel1:26379,sentinel2:26379/mymaster/0",
		},
		{
			name: "redis without password",
			dsn:  "redis://localhost:6379/0",
			want: "redis://localhost:6379/0",
		},
		{
			name: "rediss with password",
			dsn:  "rediss://user:secret123@localhost:6379/0",
			want: "rediss://user:***@localhost:6379/0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskDSN(tt.dsn, TypeRedis)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRedisDatabase_PanicRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	config := DatabaseConfig{
		Name:              "test-redis-panic",
		Type:              TypeRedis,
		DSN:               "redis://localhost:6379/6",
		ConnectionTimeout: 5 * time.Second,
	}

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	db, err := NewRedisDatabase(config, log, metrics)
	require.NoError(t, err)

	ctx := context.Background()

	err = db.Open(ctx)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	defer db.Close(ctx)

	// Test panic recovery in pipeline
	err = db.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		panic("test panic in pipeline")
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered")

	// Test panic recovery in transaction
	err = db.Transaction(ctx, []string{"test:key"}, func(tx *redis.Tx) error {
		panic("test panic in transaction")
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered")

	// Test panic recovery in TxPipelined
	err = db.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		panic("test panic in tx pipeline")
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered")
}

