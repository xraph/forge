package cache

import (
	"context"
	"time"
)

// Cache is the main interface for cache operations.
// All cache backends (in-memory, Redis, Memcached) implement this interface.
type Cache interface {
	// Connect establishes a connection to the cache backend
	Connect(ctx context.Context) error

	// Disconnect closes the connection to the cache backend
	Disconnect(ctx context.Context) error

	// Ping checks if the cache backend is accessible
	Ping(ctx context.Context) error

	// Get retrieves a value from the cache by key
	// Returns ErrNotFound if the key doesn't exist
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores a value in the cache with the given key and TTL
	// If ttl is 0, uses the default TTL from config
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a key from the cache
	Delete(ctx context.Context, key string) error

	// Exists checks if a key exists in the cache
	Exists(ctx context.Context, key string) (bool, error)

	// Clear removes all keys from the cache
	Clear(ctx context.Context) error

	// Keys returns all keys matching the pattern
	// Pattern uses glob-style matching (* for wildcard)
	Keys(ctx context.Context, pattern string) ([]string, error)

	// TTL returns the remaining time-to-live for a key
	// Returns 0 if the key has no expiration
	TTL(ctx context.Context, key string) (time.Duration, error)

	// Expire sets a new TTL for a key
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Typed operations for common use cases

// GetString retrieves a string value from the cache.
type StringCache interface {
	GetString(ctx context.Context, key string) (string, error)
	SetString(ctx context.Context, key string, value string, ttl time.Duration) error
}

// GetJSON retrieves and unmarshals a JSON value from the cache.
type JSONCache interface {
	GetJSON(ctx context.Context, key string, target any) error
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error
}

// Counter operations for atomic increments/decrements.
type CounterCache interface {
	Incr(ctx context.Context, key string) (int64, error)
	IncrBy(ctx context.Context, key string, value int64) (int64, error)
	Decr(ctx context.Context, key string) (int64, error)
	DecrBy(ctx context.Context, key string, value int64) (int64, error)
}

// Multi-key operations.
type MultiCache interface {
	MGet(ctx context.Context, keys ...string) ([][]byte, error)
	MSet(ctx context.Context, kvs map[string][]byte, ttl time.Duration) error
	MDelete(ctx context.Context, keys ...string) error
}
