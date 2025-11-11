package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
	bolt "go.etcd.io/bbolt"
)

// BoltDBStorage implements BoltDB-based persistent storage.
type BoltDBStorage struct {
	db         *bolt.DB
	path       string
	bucketName []byte
	logger     forge.Logger
	started    bool
	mu         sync.RWMutex
}

// BoltDBStorageConfig contains BoltDB storage configuration.
type BoltDBStorageConfig struct {
	Path       string
	BucketName string
	Timeout    time.Duration
	NoSync     bool
	NoGrowSync bool
	ReadOnly   bool
	MmapFlags  int
}

// NewBoltDBStorage creates a new BoltDB storage.
func NewBoltDBStorage(config BoltDBStorageConfig, logger forge.Logger) (*BoltDBStorage, error) {
	if config.Path == "" {
		return nil, errors.New("storage path is required")
	}

	if config.BucketName == "" {
		config.BucketName = "consensus"
	}

	return &BoltDBStorage{
		path:       config.Path,
		bucketName: []byte(config.BucketName),
		logger:     logger,
	}, nil
}

// Start starts the storage backend.
func (bs *BoltDBStorage) Start(ctx context.Context) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.started {
		return internal.ErrAlreadyStarted
	}

	// Open database
	db, err := bolt.Open(bs.path, 0600, &bolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to open boltdb database: %w", err)
	}

	// Create bucket if not exists
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bs.bucketName)

		return err
	})
	if err != nil {
		db.Close()

		return fmt.Errorf("failed to create bucket: %w", err)
	}

	bs.db = db
	bs.started = true

	bs.logger.Info("boltdb storage started",
		forge.F("path", bs.path),
		forge.F("bucket", string(bs.bucketName)),
	)

	return nil
}

// Stop stops the storage backend.
func (bs *BoltDBStorage) Stop(ctx context.Context) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if !bs.started {
		return internal.ErrNotStarted
	}

	if bs.db != nil {
		if err := bs.db.Close(); err != nil {
			bs.logger.Error("failed to close boltdb database",
				forge.F("error", err),
			)

			return err
		}
	}

	bs.started = false
	bs.logger.Info("boltdb storage stopped")

	return nil
}

// Set stores a key-value pair.
func (bs *BoltDBStorage) Set(key, value []byte) error {
	return bs.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)

		return bucket.Put(key, value)
	})
}

// Get retrieves a value by key.
func (bs *BoltDBStorage) Get(key []byte) ([]byte, error) {
	var value []byte

	err := bs.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)

		v := bucket.Get(key)
		if v == nil {
			return internal.ErrNodeNotFound
		}

		// Make a copy since bolt's value is only valid during transaction
		value = make([]byte, len(v))
		copy(value, v)

		return nil
	})

	return value, err
}

// Delete deletes a key.
func (bs *BoltDBStorage) Delete(key []byte) error {
	return bs.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)

		return bucket.Delete(key)
	})
}

// Exists checks if a key exists.
func (bs *BoltDBStorage) Exists(key []byte) (bool, error) {
	var exists bool

	err := bs.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)
		exists = bucket.Get(key) != nil

		return nil
	})

	return exists, err
}

// ListKeys lists all keys with a given prefix.
func (bs *BoltDBStorage) ListKeys(prefix []byte) ([][]byte, error) {
	var keys [][]byte

	err := bs.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)
		cursor := bucket.Cursor()

		for k, _ := cursor.Seek(prefix); k != nil && hasPrefix(k, prefix); k, _ = cursor.Next() {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			keys = append(keys, keyCopy)
		}

		return nil
	})

	return keys, err
}

// WriteBatch writes a batch of operations atomically.
func (bs *BoltDBStorage) WriteBatch(ops []internal.BatchOp) error {
	return bs.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)

		for i, op := range ops {
			switch op.Type {
			case internal.BatchOpSet:
				if err := bucket.Put(op.Key, op.Value); err != nil {
					return fmt.Errorf("batch op %d failed: %w", i, err)
				}

			case internal.BatchOpDelete:
				if err := bucket.Delete(op.Key); err != nil {
					return fmt.Errorf("batch op %d failed: %w", i, err)
				}

			default:
				return fmt.Errorf("unknown batch operation type: %d", op.Type)
			}
		}

		return nil
	})
}

// Iterate iterates over all key-value pairs with a given prefix.
func (bs *BoltDBStorage) Iterate(prefix []byte, fn func(key, value []byte) error) error {
	return bs.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)
		cursor := bucket.Cursor()

		for k, v := cursor.Seek(prefix); k != nil && hasPrefix(k, prefix); k, v = cursor.Next() {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)

			valueCopy := make([]byte, len(v))
			copy(valueCopy, v)

			if err := fn(keyCopy, valueCopy); err != nil {
				return err
			}
		}

		return nil
	})
}

// GetRange retrieves a range of key-value pairs.
func (bs *BoltDBStorage) GetRange(start, end []byte) ([]internal.KeyValue, error) {
	var result []internal.KeyValue

	err := bs.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bs.bucketName)
		cursor := bucket.Cursor()

		for k, v := cursor.Seek(start); k != nil; k, v = cursor.Next() {
			// Check if we've passed the end
			if len(end) > 0 && string(k) > string(end) {
				break
			}

			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)

			valueCopy := make([]byte, len(v))
			copy(valueCopy, v)

			result = append(result, internal.KeyValue{
				Key:   keyCopy,
				Value: valueCopy,
			})
		}

		return nil
	})

	return result, err
}

// Compact triggers manual compaction (BoltDB compacts automatically).
func (bs *BoltDBStorage) Compact() error {
	// BoltDB doesn't have manual compaction
	// It compacts automatically during transactions
	bs.logger.Info("boltdb compacts automatically, no manual compaction needed")

	return nil
}

// Backup creates a backup of the database.
func (bs *BoltDBStorage) Backup(path string) error {
	return bs.db.View(func(tx *bolt.Tx) error {
		return tx.CopyFile(path, 0600)
	})
}

// Size returns the size of the database in bytes.
func (bs *BoltDBStorage) Size() (int64, error) {
	var size int64

	err := bs.db.View(func(tx *bolt.Tx) error {
		size = tx.Size()

		return nil
	})

	return size, err
}

// Stats returns database statistics.
func (bs *BoltDBStorage) Stats() map[string]any {
	stats := bs.db.Stats()

	return map[string]any{
		"freelist_pages": stats.FreePageN,
		"pending_pages":  stats.PendingPageN,
		"free_alloc":     stats.FreeAlloc,
		"freelist_inuse": stats.FreelistInuse,
		"tx_count":       stats.TxN,
		"open_tx_count":  stats.OpenTxN,
		"tx_stats":       stats.TxStats,
	}
}

// hasPrefix checks if a byte slice has a given prefix.
func hasPrefix(s, prefix []byte) bool {
	if len(prefix) == 0 {
		return true
	}

	if len(s) < len(prefix) {
		return false
	}

	for i := range prefix {
		if s[i] != prefix[i] {
			return false
		}
	}

	return true
}
