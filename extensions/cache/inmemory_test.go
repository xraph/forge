package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/xraph/forge"
)

func TestInMemoryCache_ConnectDisconnect(t *testing.T) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()

	// Initially not connected
	err := cache.Ping(ctx)
	assert.Error(t, err)
	assert.Equal(t, ErrNotConnected, err)

	// Connect
	err = cache.Connect(ctx)
	assert.NoError(t, err)

	// Now connected
	err = cache.Ping(ctx)
	assert.NoError(t, err)

	// Connect again (should be idempotent)
	err = cache.Connect(ctx)
	assert.NoError(t, err)

	// Disconnect
	err = cache.Disconnect(ctx)
	assert.NoError(t, err)

	// Not connected again
	err = cache.Ping(ctx)
	assert.Error(t, err)
}

func TestInMemoryCache_BasicOperations(t *testing.T) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	t.Run("Set and Get", func(t *testing.T) {
		err := cache.Set(ctx, "key1", []byte("value1"), 0)
		assert.NoError(t, err)

		val, err := cache.Get(ctx, "key1")
		assert.NoError(t, err)
		assert.Equal(t, []byte("value1"), val)
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		_, err := cache.Get(ctx, "nonexistent")
		assert.Error(t, err)
		assert.Equal(t, ErrNotFound, err)
	})

	t.Run("Delete", func(t *testing.T) {
		_ = cache.Set(ctx, "key2", []byte("value2"), 0)

		err := cache.Delete(ctx, "key2")
		assert.NoError(t, err)

		_, err = cache.Get(ctx, "key2")
		assert.Error(t, err)
	})

	t.Run("Exists", func(t *testing.T) {
		_ = cache.Set(ctx, "key3", []byte("value3"), 0)

		exists, err := cache.Exists(ctx, "key3")
		assert.NoError(t, err)
		assert.True(t, exists)

		exists, err = cache.Exists(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Clear", func(t *testing.T) {
		_ = cache.Set(ctx, "key4", []byte("value4"), 0)
		_ = cache.Set(ctx, "key5", []byte("value5"), 0)

		err := cache.Clear(ctx)
		assert.NoError(t, err)

		_, err = cache.Get(ctx, "key4")
		assert.Error(t, err)

		_, err = cache.Get(ctx, "key5")
		assert.Error(t, err)
	})
}

func TestInMemoryCache_TTL(t *testing.T) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	t.Run("Set with TTL", func(t *testing.T) {
		err := cache.Set(ctx, "expire1", []byte("value"), 100*time.Millisecond)
		assert.NoError(t, err)

		// Should exist immediately
		val, err := cache.Get(ctx, "expire1")
		assert.NoError(t, err)
		assert.Equal(t, []byte("value"), val)

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)

		// Should not exist
		_, err = cache.Get(ctx, "expire1")
		assert.Error(t, err)
		assert.Equal(t, ErrNotFound, err)
	})

	t.Run("TTL method", func(t *testing.T) {
		err := cache.Set(ctx, "ttl1", []byte("value"), 1*time.Second)
		assert.NoError(t, err)

		ttl, err := cache.TTL(ctx, "ttl1")
		assert.NoError(t, err)
		assert.True(t, ttl > 0 && ttl <= 1*time.Second)
	})

	t.Run("Expire method", func(t *testing.T) {
		err := cache.Set(ctx, "expire2", []byte("value"), 1*time.Hour)
		assert.NoError(t, err)

		// Change TTL to 100ms
		err = cache.Expire(ctx, "expire2", 100*time.Millisecond)
		assert.NoError(t, err)

		// Wait and verify expiration
		time.Sleep(150 * time.Millisecond)

		_, err = cache.Get(ctx, "expire2")
		assert.Error(t, err)
	})
}

func TestInMemoryCache_Keys(t *testing.T) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	// Set up test data
	_ = cache.Set(ctx, "user:1", []byte("alice"), 0)
	_ = cache.Set(ctx, "user:2", []byte("bob"), 0)
	_ = cache.Set(ctx, "post:1", []byte("hello"), 0)
	_ = cache.Set(ctx, "post:2", []byte("world"), 0)

	t.Run("Match all keys", func(t *testing.T) {
		keys, err := cache.Keys(ctx, "*")
		assert.NoError(t, err)
		assert.Len(t, keys, 4)
	})

	t.Run("Match prefix", func(t *testing.T) {
		keys, err := cache.Keys(ctx, "user:*")
		assert.NoError(t, err)
		assert.Len(t, keys, 2)
		assert.Contains(t, keys, "user:1")
		assert.Contains(t, keys, "user:2")
	})

	t.Run("Match suffix", func(t *testing.T) {
		keys, err := cache.Keys(ctx, "*:1")
		assert.NoError(t, err)
		assert.Len(t, keys, 2)
		assert.Contains(t, keys, "user:1")
		assert.Contains(t, keys, "post:1")
	})
}

func TestInMemoryCache_Prefix(t *testing.T) {
	config := DefaultConfig()
	config.Prefix = "myapp:"
	cache := NewInMemoryCache(config, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	// Set without prefix (will be added internally)
	err := cache.Set(ctx, "key1", []byte("value1"), 0)
	assert.NoError(t, err)

	// Get without prefix (will be added internally)
	val, err := cache.Get(ctx, "key1")
	assert.NoError(t, err)
	assert.Equal(t, []byte("value1"), val)
}

func TestInMemoryCache_MaxSize(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 3
	cache := NewInMemoryCache(config, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	// Add 3 items (at limit)
	_ = cache.Set(ctx, "key1", []byte("value1"), 0)
	_ = cache.Set(ctx, "key2", []byte("value2"), 0)
	_ = cache.Set(ctx, "key3", []byte("value3"), 0)

	// All should exist
	_, err := cache.Get(ctx, "key1")
	assert.NoError(t, err)

	// Add 4th item (should evict one)
	_ = cache.Set(ctx, "key4", []byte("value4"), 0)

	// key4 should exist
	_, err = cache.Get(ctx, "key4")
	assert.NoError(t, err)

	// At least one of the first 3 should be evicted
	keys, _ := cache.Keys(ctx, "*")
	assert.LessOrEqual(t, len(keys), 3)
}

func TestInMemoryCache_TypedOperations(t *testing.T) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	t.Run("String operations", func(t *testing.T) {
		err := cache.SetString(ctx, "str1", "hello world", 0)
		assert.NoError(t, err)

		val, err := cache.GetString(ctx, "str1")
		assert.NoError(t, err)
		assert.Equal(t, "hello world", val)
	})

	t.Run("JSON operations", func(t *testing.T) {
		type User struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		user := User{ID: 1, Name: "Alice"}
		err := cache.SetJSON(ctx, "user:1", user, 0)
		assert.NoError(t, err)

		var retrieved User
		err = cache.GetJSON(ctx, "user:1", &retrieved)
		assert.NoError(t, err)
		assert.Equal(t, user, retrieved)
	})
}

func TestInMemoryCache_Validation(t *testing.T) {
	config := DefaultConfig()
	config.MaxKeySize = 10
	config.MaxValueSize = 20
	cache := NewInMemoryCache(config, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	t.Run("Key too large", func(t *testing.T) {
		err := cache.Set(ctx, "verylongkeythatexceedslimit", []byte("value"), 0)
		assert.Error(t, err)
		assert.Equal(t, ErrKeyTooLarge, err)
	})

	t.Run("Value too large", func(t *testing.T) {
		err := cache.Set(ctx, "key", []byte("verylongvaluethatexceedsthelimit"), 0)
		assert.Error(t, err)
		assert.Equal(t, ErrValueTooLarge, err)
	})
}

func TestInMemoryCache_Cleanup(t *testing.T) {
	config := DefaultConfig()
	config.CleanupInterval = 100 * time.Millisecond
	cache := NewInMemoryCache(config, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	// Add expired items
	_ = cache.Set(ctx, "expire1", []byte("value1"), 50*time.Millisecond)
	_ = cache.Set(ctx, "expire2", []byte("value2"), 50*time.Millisecond)
	_ = cache.Set(ctx, "persist", []byte("value3"), 0)

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	// Expired items should be gone
	_, err := cache.Get(ctx, "expire1")
	assert.Error(t, err)

	// Non-expired item should remain
	val, err := cache.Get(ctx, "persist")
	assert.NoError(t, err)
	assert.Equal(t, []byte("value3"), val)
}

func TestInMemoryCache_Concurrent(t *testing.T) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			key := fmt.Sprintf("key%d", idx)
			_ = cache.Set(ctx, key, []byte(fmt.Sprintf("value%d", idx)), 0)
			_, _ = cache.Get(ctx, key)
			_, _ = cache.Exists(ctx, key)
			_ = cache.Delete(ctx, key)
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}

// Benchmarks

func BenchmarkInMemoryCache_Set(b *testing.B) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	value := []byte("test value")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = cache.Set(ctx, fmt.Sprintf("key%d", i), value, 0)
	}
}

func BenchmarkInMemoryCache_Get(b *testing.B) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		_ = cache.Set(ctx, fmt.Sprintf("key%d", i), []byte("value"), 0)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(ctx, fmt.Sprintf("key%d", i%1000))
	}
}

func BenchmarkInMemoryCache_Exists(b *testing.B) {
	cache := NewInMemoryCache(DefaultConfig(), forge.NewNoopLogger(), forge.NewNoOpMetrics())
	ctx := context.Background()
	_ = cache.Connect(ctx)
	defer cache.Disconnect(ctx)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		_ = cache.Set(ctx, fmt.Sprintf("key%d", i), []byte("value"), 0)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = cache.Exists(ctx, fmt.Sprintf("key%d", i%1000))
	}
}
