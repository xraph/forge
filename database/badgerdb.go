package database

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/dgraph-io/badger/v3/options"
)

// badgerCache implements Cache interface for BadgerDB
type badgerCache struct {
	*baseCacheDatabase
	db     *badger.DB
	path   string
	prefix string

	// Cleanup ticker for expired keys
	cleanup *time.Ticker
	stop    chan struct{}
	wg      sync.WaitGroup
}

// NewBadgerCache creates a new BadgerDB cache connection
func newBadgerCache(config CacheConfig) (Cache, error) {
	cache := &badgerCache{
		baseCacheDatabase: &baseCacheDatabase{
			config:    config,
			driver:    "badger",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		prefix: config.Prefix,
		stop:   make(chan struct{}),
	}

	if err := cache.Connect(context.Background()); err != nil {
		return nil, err
	}

	return cache, nil
}

// Connect establishes BadgerDB connection
func (b *badgerCache) Connect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Determine database path
	b.path = b.config.URL
	if b.path == "" {
		b.path = "./badger_cache"
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(b.path, 0755); err != nil {
		return fmt.Errorf("failed to create BadgerDB directory: %w", err)
	}

	// Configure BadgerDB options
	opts := badger.DefaultOptions(b.path)

	// Configure logging (disable by default for cache usage)
	opts.Logger = nil

	// Configure memory settings
	if b.config.MaxSize > 0 {
		opts.ValueLogFileSize = b.config.MaxSize
	}

	// Configure sync writes (disable for better performance)
	opts.SyncWrites = false

	// Configure value log settings
	opts.ValueLogMaxEntries = 1000000
	opts.ValueThreshold = 1024 // Values larger than 1KB go to value log

	// Configure compression
	opts.Compression = options.Snappy

	// Open database
	db, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open BadgerDB: %w", err)
	}

	b.db = db
	b.connected = true

	// Start cleanup goroutine
	b.startCleanup()

	return nil
}

// startCleanup starts the cleanup goroutine for expired keys
func (b *badgerCache) startCleanup() {
	cleanupInterval := b.config.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 5 * time.Minute // Default cleanup interval
	}

	b.cleanup = time.NewTicker(cleanupInterval)
	b.wg.Add(1)

	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-b.cleanup.C:
				b.runGC()
			case <-b.stop:
				return
			}
		}
	}()
}

// runGC runs BadgerDB garbage collection
func (b *badgerCache) runGC() {
	if b.db == nil {
		return
	}

	// Run BadgerDB garbage collection
	for {
		err := b.db.RunValueLogGC(0.5)
		if err != nil {
			break
		}
	}
}

// Close closes BadgerDB connection
func (b *badgerCache) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return nil
	}

	// Stop cleanup goroutine
	close(b.stop)
	if b.cleanup != nil {
		b.cleanup.Stop()
	}
	b.wg.Wait()

	// Close database
	if b.db != nil {
		err := b.db.Close()
		b.connected = false
		return err
	}

	return nil
}

// Ping tests BadgerDB connection
func (b *badgerCache) Ping(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.connected || b.db == nil {
		return fmt.Errorf("BadgerDB not connected")
	}

	// Test by trying to start a transaction
	txn := b.db.NewTransaction(false)
	defer txn.Discard()

	return nil
}

// prefixKey adds prefix to key
func (b *badgerCache) prefixKey(key string) string {
	if b.prefix == "" {
		return key
	}
	return b.prefix + ":" + key
}

// unprefixKey removes prefix from key
func (b *badgerCache) unprefixKey(key string) string {
	if b.prefix == "" {
		return key
	}
	prefix := b.prefix + ":"
	if strings.HasPrefix(key, prefix) {
		return key[len(prefix):]
	}
	return key
}

// Get retrieves a value from BadgerDB
func (b *badgerCache) Get(ctx context.Context, key string) ([]byte, error) {
	if !b.connected {
		return nil, fmt.Errorf("BadgerDB not connected")
	}

	var value []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(b.prefixKey(key)))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			value = make([]byte, len(val))
			copy(value, val)
			return nil
		})
	})

	if err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, fmt.Errorf("key not found")
		}
		return nil, err
	}

	return value, nil
}

// Set stores a value in BadgerDB
func (b *badgerCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(b.prefixKey(key)), value)

		if ttl > 0 {
			entry.WithTTL(ttl)
		}

		return txn.SetEntry(entry)
	})
}

// Delete removes a key from BadgerDB
func (b *badgerCache) Delete(ctx context.Context, key string) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(b.prefixKey(key)))
	})
}

// Exists checks if a key exists in BadgerDB
func (b *badgerCache) Exists(ctx context.Context, key string) (bool, error) {
	if !b.connected {
		return false, fmt.Errorf("BadgerDB not connected")
	}

	exists := false
	err := b.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(b.prefixKey(key)))
		if err == nil {
			exists = true
		} else if err == badger.ErrKeyNotFound {
			exists = false
		}
		return nil
	})

	return exists, err
}

// GetMulti retrieves multiple values from BadgerDB
func (b *badgerCache) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	if !b.connected {
		return nil, fmt.Errorf("BadgerDB not connected")
	}

	result := make(map[string][]byte)

	err := b.db.View(func(txn *badger.Txn) error {
		for _, key := range keys {
			item, err := txn.Get([]byte(b.prefixKey(key)))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					continue
				}
				return err
			}

			err = item.Value(func(val []byte) error {
				value := make([]byte, len(val))
				copy(value, val)
				result[key] = value
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return result, err
}

// SetMulti stores multiple values in BadgerDB
func (b *badgerCache) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.Update(func(txn *badger.Txn) error {
		for key, value := range items {
			entry := badger.NewEntry([]byte(b.prefixKey(key)), value)

			if ttl > 0 {
				entry.WithTTL(ttl)
			}

			if err := txn.SetEntry(entry); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteMulti removes multiple keys from BadgerDB
func (b *badgerCache) DeleteMulti(ctx context.Context, keys []string) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			if err := txn.Delete([]byte(b.prefixKey(key))); err != nil {
				return err
			}
		}
		return nil
	})
}

// Expire sets TTL for a key (BadgerDB doesn't support changing TTL after creation)
func (b *badgerCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	// BadgerDB doesn't support changing TTL after creation
	// We need to get the value and set it again with new TTL
	value, err := b.Get(ctx, key)
	if err != nil {
		return err
	}

	return b.Set(ctx, key, value, ttl)
}

// TTL returns the TTL for a key (BadgerDB doesn't expose TTL directly)
func (b *badgerCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if !b.connected {
		return 0, fmt.Errorf("BadgerDB not connected")
	}

	var expiresAt uint64
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(b.prefixKey(key)))
		if err != nil {
			return err
		}
		expiresAt = item.ExpiresAt()
		return nil
	})

	if err != nil {
		return 0, err
	}

	if expiresAt == 0 {
		return 0, nil // No expiration
	}

	ttl := time.Until(time.Unix(int64(expiresAt), 0))
	if ttl < 0 {
		return 0, nil // Already expired
	}

	return ttl, nil
}

// Increment increments a numeric value
func (b *badgerCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	if !b.connected {
		return 0, fmt.Errorf("BadgerDB not connected")
	}

	var newValue int64
	err := b.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(b.prefixKey(key)))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				// Key doesn't exist, create with delta value
				newValue = delta
				data, _ := json.Marshal(newValue)
				return txn.Set([]byte(b.prefixKey(key)), data)
			}
			return err
		}

		// Get current value
		var currentValue int64
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &currentValue)
		})
		if err != nil {
			return fmt.Errorf("value is not a number")
		}

		// Increment
		newValue = currentValue + delta
		data, _ := json.Marshal(newValue)
		return txn.Set([]byte(b.prefixKey(key)), data)
	})

	return newValue, err
}

// Decrement decrements a numeric value
func (b *badgerCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return b.Increment(ctx, key, -delta)
}

// Keys returns all keys matching a pattern
func (b *badgerCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	if !b.connected {
		return nil, fmt.Errorf("BadgerDB not connected")
	}

	var keys []string
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		// Add prefix to pattern
		prefixedPattern := pattern
		if b.prefix != "" {
			prefixedPattern = b.prefix + ":" + pattern
		}

		fmt.Println(prefixedPattern)

		prefix := []byte(b.prefix + ":")
		if b.prefix != "" {
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := string(it.Item().Key())
				unprefixedKey := b.unprefixKey(key)

				// Simple pattern matching (would need proper glob matching)
				if pattern == "*" || strings.Contains(unprefixedKey, strings.TrimSuffix(pattern, "*")) {
					keys = append(keys, unprefixedKey)
				}
			}
		} else {
			for it.Rewind(); it.Valid(); it.Next() {
				key := string(it.Item().Key())

				// Simple pattern matching (would need proper glob matching)
				if pattern == "*" || strings.Contains(key, strings.TrimSuffix(pattern, "*")) {
					keys = append(keys, key)
				}
			}
		}

		return nil
	})

	return keys, err
}

// Clear removes all items from BadgerDB
func (b *badgerCache) Clear(ctx context.Context) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	// If we have a prefix, only clear keys with that prefix
	if b.prefix != "" {
		return b.db.Update(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := txn.NewIterator(opts)
			defer it.Close()

			prefix := []byte(b.prefix + ":")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				if err := txn.Delete(it.Item().Key()); err != nil {
					return err
				}
			}
			return nil
		})
	}

	// Clear all keys (dangerous!)
	return b.db.DropAll()
}

// GetJSON retrieves and unmarshals JSON from BadgerDB
func (b *badgerCache) GetJSON(ctx context.Context, key string, dest interface{}) error {
	data, err := b.Get(ctx, key)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// SetJSON marshals and stores JSON in BadgerDB
func (b *badgerCache) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return b.Set(ctx, key, data, ttl)
}

// Stats returns BadgerDB statistics
func (b *badgerCache) Stats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range b.stats {
		stats[k] = v
	}

	stats["driver"] = b.driver
	stats["connected"] = b.connected
	stats["path"] = b.path

	if b.db != nil {
		// Get BadgerDB LSM tree stats
		lsmStats := b.db.LevelsToString()
		stats["lsm_tree"] = lsmStats

		// // Get value log stats
		// vlStats := b.db.LevelsToString()
		// stats["value_log"] = vlStats
	}

	return stats
}

// BadgerDB-specific utility functions

// Backup creates a backup of the database
func (b *badgerCache) Backup(ctx context.Context, backupPath string) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	// Create backup directory
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create backup file
	backupFile, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer backupFile.Close()

	// Create backup
	_, err = b.db.Backup(backupFile, 0)
	return err
}

// Restore restores from a backup
func (b *badgerCache) Restore(ctx context.Context, backupPath string) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	// Open backup file
	backupFile, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer backupFile.Close()

	// Load backup
	return b.db.Load(backupFile, 256)
}

// GetSize returns the size of the database
func (b *badgerCache) GetSize() (int64, error) {
	if !b.connected {
		return 0, fmt.Errorf("BadgerDB not connected")
	}

	lsm, vlog := b.db.Size()
	return lsm + vlog, nil
}

// Sync forces a sync of the database
func (b *badgerCache) Sync() error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.Sync()
}

// RunValueLogGC runs value log garbage collection
func (b *badgerCache) RunValueLogGC(discardRatio float64) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.RunValueLogGC(discardRatio)
}

// Flatten flattens the LSM tree
func (b *badgerCache) Flatten(workers int) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.Flatten(workers)
}

// GetTables returns information about LSM tree tables
func (b *badgerCache) GetTables() []badger.TableInfo {
	if !b.connected {
		return nil
	}

	return b.db.Tables()
}

// GetKeyCount returns the approximate number of keys
func (b *badgerCache) GetKeyCount() (int64, error) {
	if !b.connected {
		return 0, fmt.Errorf("BadgerDB not connected")
	}

	var count int64
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		if b.prefix != "" {
			prefix := []byte(b.prefix + ":")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				count++
			}
		} else {
			for it.Rewind(); it.Valid(); it.Next() {
				count++
			}
		}

		return nil
	})

	return count, err
}

// Transaction support

// NewTransaction creates a new transaction
func (b *badgerCache) NewTransaction(update bool) *badger.Txn {
	if !b.connected {
		return nil
	}

	return b.db.NewTransaction(update)
}

// Update runs a read-write transaction
func (b *badgerCache) Update(fn func(txn *badger.Txn) error) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.Update(fn)
}

// View runs a read-only transaction
func (b *badgerCache) View(fn func(txn *badger.Txn) error) error {
	if !b.connected {
		return fmt.Errorf("BadgerDB not connected")
	}

	return b.db.View(fn)
}

// init function to override the BadgerDB constructor
func init() {
	NewBadgerCache = func(config CacheConfig) (Cache, error) {
		return newBadgerCache(config)
	}
}
