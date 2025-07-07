package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// natsKVCache implements Cache interface for NATS KV
type natsKVCache struct {
	*baseCacheDatabase
	conn   *nats.Conn
	js     jetstream.JetStream
	kv     jetstream.KeyValue
	bucket string
	prefix string
}

// NewNATSKVCache creates a new NATS KV cache connection
func newNATSKVCache(config CacheConfig) (Cache, error) {
	cache := &natsKVCache{
		baseCacheDatabase: &baseCacheDatabase{
			config:    config,
			driver:    "nats_kv",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		bucket: "forge_cache",
		prefix: config.Prefix,
	}

	if err := cache.Connect(context.Background()); err != nil {
		return nil, err
	}

	return cache, nil
}

// Connect establishes NATS KV connection
func (n *natsKVCache) Connect(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Build NATS connection URL
	url := n.buildNATSURL()

	// Create connection options
	opts := []nats.Option{
		nats.Name("forge-cache"),
		nats.ReconnectWait(time.Second),
		nats.MaxReconnects(-1),
	}

	// Add authentication if provided
	if n.config.Username != "" && n.config.Password != "" {
		opts = append(opts, nats.UserInfo(n.config.Username, n.config.Password))
	}

	// Connect to NATS
	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	n.conn = conn

	// Create JetStream context
	js, err := jetstream.New(conn)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	n.js = js

	// Create or get KV bucket
	if err := n.ensureKVBucket(ctx); err != nil {
		return fmt.Errorf("failed to ensure KV bucket: %w", err)
	}

	n.connected = true
	return nil
}

// buildNATSURL builds NATS connection URL
func (n *natsKVCache) buildNATSURL() string {
	if n.config.URL != "" {
		return n.config.URL
	}

	host := n.config.Host
	if host == "" {
		host = "localhost"
	}

	port := n.config.Port
	if port == 0 {
		port = 4222 // NATS default port
	}

	return fmt.Sprintf("nats://%s:%d", host, port)
}

// ensureKVBucket creates or gets the KV bucket
func (n *natsKVCache) ensureKVBucket(ctx context.Context) error {
	// Set bucket name from config if provided
	if n.config.Database != 0 {
		n.bucket = fmt.Sprintf("forge_cache_%d", n.config.Database)
	}

	// Try to get existing bucket
	kv, err := n.js.KeyValue(ctx, n.bucket)
	if err != nil {
		// Create bucket if it doesn't exist
		config := jetstream.KeyValueConfig{
			Bucket:      n.bucket,
			Description: "Forge cache bucket",
			Storage:     jetstream.FileStorage,
			Replicas:    1,
		}

		// Configure TTL if specified
		if n.config.DefaultTTL > 0 {
			config.TTL = n.config.DefaultTTL
		}

		// Configure max size if specified
		if n.config.MaxSize > 0 {
			config.MaxBytes = n.config.MaxSize
		}

		// Configure max value size
		config.MaxValueSize = 1024 * 1024 // 1MB default

		kv, err = n.js.CreateKeyValue(ctx, config)
		if err != nil {
			return fmt.Errorf("failed to create KV bucket: %w", err)
		}
	}

	n.kv = kv
	return nil
}

// Close closes NATS KV connection
func (n *natsKVCache) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.conn != nil {
		n.conn.Close()
		n.connected = false
	}

	return nil
}

// Ping tests NATS KV connection
func (n *natsKVCache) Ping(ctx context.Context) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.connected || n.conn == nil {
		return fmt.Errorf("NATS KV not connected")
	}

	// Check connection status
	if !n.conn.IsConnected() {
		return fmt.Errorf("NATS connection lost")
	}

	return nil
}

// prefixKey adds prefix to key
func (n *natsKVCache) prefixKey(key string) string {
	if n.prefix == "" {
		return key
	}
	return n.prefix + ":" + key
}

// unprefixKey removes prefix from key
func (n *natsKVCache) unprefixKey(key string) string {
	if n.prefix == "" {
		return key
	}
	prefix := n.prefix + ":"
	if strings.HasPrefix(key, prefix) {
		return key[len(prefix):]
	}
	return key
}

// Get retrieves a value from NATS KV
func (n *natsKVCache) Get(ctx context.Context, key string) ([]byte, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	entry, err := n.kv.Get(ctx, n.prefixKey(key))
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, fmt.Errorf("key not found")
		}
		return nil, err
	}

	return entry.Value(), nil
}

// Set stores a value in NATS KV
func (n *natsKVCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !n.connected {
		return fmt.Errorf("NATS KV not connected")
	}

	// NATS KV doesn't support per-key TTL in the same way
	// TTL is configured at the bucket level
	_, err := n.kv.Put(ctx, n.prefixKey(key), value)
	return err
}

// Delete removes a key from NATS KV
func (n *natsKVCache) Delete(ctx context.Context, key string) error {
	if !n.connected {
		return fmt.Errorf("NATS KV not connected")
	}

	return n.kv.Delete(ctx, n.prefixKey(key))
}

// Exists checks if a key exists in NATS KV
func (n *natsKVCache) Exists(ctx context.Context, key string) (bool, error) {
	if !n.connected {
		return false, fmt.Errorf("NATS KV not connected")
	}

	_, err := n.kv.Get(ctx, n.prefixKey(key))
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// GetMulti retrieves multiple values from NATS KV
func (n *natsKVCache) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	result := make(map[string][]byte)

	for _, key := range keys {
		if value, err := n.Get(ctx, key); err == nil {
			result[key] = value
		}
	}

	return result, nil
}

// SetMulti stores multiple values in NATS KV
func (n *natsKVCache) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	if !n.connected {
		return fmt.Errorf("NATS KV not connected")
	}

	for key, value := range items {
		if err := n.Set(ctx, key, value, ttl); err != nil {
			return err
		}
	}

	return nil
}

// DeleteMulti removes multiple keys from NATS KV
func (n *natsKVCache) DeleteMulti(ctx context.Context, keys []string) error {
	if !n.connected {
		return fmt.Errorf("NATS KV not connected")
	}

	for _, key := range keys {
		if err := n.Delete(ctx, key); err != nil {
			return err
		}
	}

	return nil
}

// Expire sets TTL for a key (not supported in NATS KV)
func (n *natsKVCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if !n.connected {
		return fmt.Errorf("NATS KV not connected")
	}

	// NATS KV doesn't support per-key TTL
	// TTL is configured at the bucket level
	return fmt.Errorf("per-key TTL not supported in NATS KV")
}

// TTL returns the TTL for a key (not supported in NATS KV)
func (n *natsKVCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if !n.connected {
		return 0, fmt.Errorf("NATS KV not connected")
	}

	// NATS KV doesn't expose per-key TTL
	return 0, fmt.Errorf("per-key TTL not supported in NATS KV")
}

// Increment increments a numeric value
func (n *natsKVCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	if !n.connected {
		return 0, fmt.Errorf("NATS KV not connected")
	}

	prefixedKey := n.prefixKey(key)

	// NATS KV doesn't have atomic increment, so we need to implement it
	// This is a simplified implementation - in production, you'd want to handle race conditions
	entry, err := n.kv.Get(ctx, prefixedKey)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			// Key doesn't exist, create with delta value
			newValue := delta
			data, _ := json.Marshal(newValue)
			_, err := n.kv.Put(ctx, prefixedKey, data)
			return newValue, err
		}
		return 0, err
	}

	// Parse current value
	var currentValue int64
	if err := json.Unmarshal(entry.Value(), &currentValue); err != nil {
		return 0, fmt.Errorf("value is not a number")
	}

	// Increment
	newValue := currentValue + delta
	data, _ := json.Marshal(newValue)
	_, err = n.kv.Put(ctx, prefixedKey, data)

	return newValue, err
}

// Decrement decrements a numeric value
func (n *natsKVCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return n.Increment(ctx, key, -delta)
}

// Keys returns all keys matching a pattern
func (n *natsKVCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	// Get all keys
	keys, err := n.kv.Keys(ctx)
	if err != nil {
		return nil, err
	}

	var matchingKeys []string
	for _, key := range keys {
		unprefixedKey := n.unprefixKey(key)

		// Simple pattern matching (would need proper glob matching)
		if pattern == "*" || strings.Contains(unprefixedKey, strings.TrimSuffix(pattern, "*")) {
			matchingKeys = append(matchingKeys, unprefixedKey)
		}
	}

	return matchingKeys, nil
}

// Clear removes all items from NATS KV
func (n *natsKVCache) Clear(ctx context.Context) error {
	if !n.connected {
		return fmt.Errorf("NATS KV not connected")
	}

	// If we have a prefix, only clear keys with that prefix
	if n.prefix != "" {
		keys, err := n.kv.Keys(ctx)
		if err != nil {
			return err
		}

		prefix := n.prefix + ":"
		for _, key := range keys {
			if strings.HasPrefix(key, prefix) {
				if err := n.kv.Delete(ctx, key); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Clear all keys
	return n.kv.PurgeDeletes(ctx)
}

// GetJSON retrieves and unmarshals JSON from NATS KV
func (n *natsKVCache) GetJSON(ctx context.Context, key string, dest interface{}) error {
	data, err := n.Get(ctx, key)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// SetJSON marshals and stores JSON in NATS KV
func (n *natsKVCache) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return n.Set(ctx, key, data, ttl)
}

// Stats returns NATS KV statistics
func (n *natsKVCache) Stats() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range n.stats {
		stats[k] = v
	}

	stats["driver"] = n.driver
	stats["connected"] = n.connected
	stats["bucket"] = n.bucket

	if n.conn != nil {
		stats["servers"] = n.conn.Servers()
		stats["discovered_servers"] = n.conn.DiscoveredServers()
		stats["is_connected"] = n.conn.IsConnected()
		stats["is_reconnecting"] = n.conn.IsReconnecting()
	}

	return stats
}

// NATS KV-specific utility functions

// GetConn returns the underlying NATS connection
func (n *natsKVCache) GetConn() *nats.Conn {
	return n.conn
}

// GetJetStream returns the JetStream context
func (n *natsKVCache) GetJetStream() jetstream.JetStream {
	return n.js
}

// GetKV returns the KeyValue store
func (n *natsKVCache) GetKV() jetstream.KeyValue {
	return n.kv
}

// GetBucketStatus returns the bucket status
func (n *natsKVCache) GetBucketStatus(ctx context.Context) (jetstream.KeyValueStatus, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	return n.kv.Status(ctx)
}

// GetBucketInfo returns bucket information
func (n *natsKVCache) GetBucketInfo(ctx context.Context) (map[string]interface{}, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	status, err := n.kv.Status(ctx)
	if err != nil {
		return nil, err
	}

	info := make(map[string]interface{})
	info["bucket"] = status.Bucket()
	info["values"] = status.Values()
	info["bytes"] = status.Bytes()
	info["replicas"] = status.BackingStore()
	info["is_compressed"] = status.IsCompressed()

	return info, nil
}

// Watch watches for changes to a key
func (n *natsKVCache) Watch(ctx context.Context, key string) (jetstream.KeyWatcher, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	return n.kv.Watch(ctx, n.prefixKey(key))
}

// WatchAll watches for changes to all keys
func (n *natsKVCache) WatchAll(ctx context.Context) (jetstream.KeyWatcher, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	if n.prefix != "" {
		return n.kv.Watch(ctx, n.prefix+":*")
	}

	return n.kv.WatchAll(ctx)
}

// GetHistory returns the history of a key
func (n *natsKVCache) GetHistory(ctx context.Context, key string) ([]jetstream.KeyValueEntry, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	return n.kv.History(ctx, n.prefixKey(key))
}

// GetRevision returns a specific revision of a key
func (n *natsKVCache) GetRevision(ctx context.Context, key string, revision uint64) (jetstream.KeyValueEntry, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	return n.kv.GetRevision(ctx, n.prefixKey(key), revision)
}

// Update updates a key only if the revision matches
func (n *natsKVCache) Update(ctx context.Context, key string, value []byte, revision uint64) (jetstream.KeyValueEntry, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	rev, err := n.kv.Update(ctx, n.prefixKey(key), value, revision)
	if err != nil {
		return nil, err
	}

	return n.kv.GetRevision(ctx, n.prefixKey(key), rev)
}

// Create creates a key only if it doesn't exist
func (n *natsKVCache) Create(ctx context.Context, key string, value []byte) (jetstream.KeyValueEntry, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	rev, err := n.kv.Create(ctx, n.prefixKey(key), value)
	if err != nil {
		return nil, err
	}

	return n.kv.GetRevision(ctx, n.prefixKey(key), rev)
}

// PutString puts a string value
func (n *natsKVCache) PutString(ctx context.Context, key string, value string) (jetstream.KeyValueEntry, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	rev, err := n.kv.PutString(ctx, n.prefixKey(key), value)
	if err != nil {
		return nil, err
	}

	return n.kv.GetRevision(ctx, n.prefixKey(key), rev)
}

// GetString gets a string value
func (n *natsKVCache) GetString(ctx context.Context, key string) (string, error) {
	if !n.connected {
		return "", fmt.Errorf("NATS KV not connected")
	}

	entry, err := n.kv.Get(ctx, n.prefixKey(key))
	if err != nil {
		return "", err
	}

	return string(entry.Value()), nil
}

// CompareAndSwap atomically compares and swaps a value
func (n *natsKVCache) CompareAndSwap(ctx context.Context, key string, oldValue, newValue []byte) (jetstream.KeyValueEntry, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	// Get current entry
	entry, err := n.kv.Get(ctx, n.prefixKey(key))
	if err != nil {
		return nil, err
	}

	// Compare values
	if string(entry.Value()) != string(oldValue) {
		return nil, fmt.Errorf("value mismatch")
	}

	// Update with revision check
	rev, err := n.kv.Update(ctx, n.prefixKey(key), newValue, entry.Revision())
	if err != nil {
		return nil, err
	}

	return n.kv.GetRevision(ctx, n.prefixKey(key), rev)
}

// Backup creates a backup of the KV store
func (n *natsKVCache) Backup(ctx context.Context) (map[string][]byte, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	keys, err := n.kv.Keys(ctx)
	if err != nil {
		return nil, err
	}

	backup := make(map[string][]byte)
	for _, key := range keys {
		entry, err := n.kv.Get(ctx, key)
		if err != nil {
			continue
		}
		backup[key] = entry.Value()
	}

	return backup, nil
}

// Restore restores from a backup
func (n *natsKVCache) Restore(ctx context.Context, backup map[string][]byte) error {
	if !n.connected {
		return fmt.Errorf("NATS KV not connected")
	}

	for key, value := range backup {
		if _, err := n.kv.Put(ctx, key, value); err != nil {
			return err
		}
	}

	return nil
}

// GetConnectionInfo returns NATS connection information
func (n *natsKVCache) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if !n.connected {
		return nil, fmt.Errorf("NATS KV not connected")
	}

	info := make(map[string]interface{})

	// Connection info
	info["connected_url"] = n.conn.ConnectedUrl()
	info["connected_server_name"] = n.conn.ConnectedServerName()
	info["connected_server_version"] = n.conn.ConnectedServerVersion()
	info["servers"] = n.conn.Servers()
	info["discovered_servers"] = n.conn.DiscoveredServers()
	info["is_connected"] = n.conn.IsConnected()
	info["is_reconnecting"] = n.conn.IsReconnecting()

	// JetStream info
	if n.js != nil {
		if jsInfo, err := n.js.AccountInfo(ctx); err == nil {
			info["jetstream_account"] = jsInfo
		}
	}

	// Bucket info
	if bucketInfo, err := n.GetBucketInfo(ctx); err == nil {
		info["bucket_info"] = bucketInfo
	}

	return info, nil
}

// init function to override the NATS KV constructor
func init() {
	NewNATSKVCache = func(config CacheConfig) (Cache, error) {
		return newNATSKVCache(config)
	}
}
