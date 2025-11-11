package storage

import (
	"context"
	"fmt"
	"sync"

	"github.com/dgraph-io/badger/v4"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// BadgerStorage implements BadgerDB-based persistent storage.
type BadgerStorage struct {
	db      *badger.DB
	path    string
	logger  forge.Logger
	started bool
	mu      sync.RWMutex
}

// BadgerStorageConfig contains BadgerDB storage configuration.
type BadgerStorageConfig struct {
	Path               string
	SyncWrites         bool
	ValueLogFileSize   int64
	NumVersionsToKeep  int
	NumCompactors      int
	MemTableSize       int64
	BaseTableSize      int64
	BloomFalsePositive float64
	BlockSize          int
	BlockCacheSize     int64
	IndexCacheSize     int64
	NumLevelZeroTables int
	NumLevelZeroStalls int
	ValueThreshold     int
	NumMemtables       int
	DetectConflicts    bool
	CompactL0OnClose   bool
}

// NewBadgerStorage creates a new BadgerDB storage.
func NewBadgerStorage(config BadgerStorageConfig, logger forge.Logger) (*BadgerStorage, error) {
	if config.Path == "" {
		return nil, errors.New("storage path is required")
	}

	return &BadgerStorage{
		path:   config.Path,
		logger: logger,
	}, nil
}

// Start starts the storage backend.
func (bs *BadgerStorage) Start(ctx context.Context) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.started {
		return internal.ErrAlreadyStarted
	}

	// Configure BadgerDB options
	opts := badger.DefaultOptions(bs.path)
	opts.SyncWrites = true // Ensure durability
	opts.Logger = nil      // Use our own logger

	// Open database
	db, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open badger database: %w", err)
	}

	bs.db = db
	bs.started = true

	bs.logger.Info("badger storage started",
		forge.F("path", bs.path),
	)

	return nil
}

// Stop stops the storage backend.
func (bs *BadgerStorage) Stop(ctx context.Context) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if !bs.started {
		return internal.ErrNotStarted
	}

	if bs.db != nil {
		if err := bs.db.Close(); err != nil {
			bs.logger.Error("failed to close badger database",
				forge.F("error", err),
			)

			return err
		}
	}

	bs.started = false
	bs.logger.Info("badger storage stopped")

	return nil
}

// Set stores a key-value pair.
func (bs *BadgerStorage) Set(key, value []byte) error {
	return bs.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
}

// Get retrieves a value by key.
func (bs *BadgerStorage) Get(key []byte) ([]byte, error) {
	var value []byte

	err := bs.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		value, err = item.ValueCopy(nil)

		return err
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, internal.ErrNodeNotFound
	}

	return value, err
}

// Delete deletes a key.
func (bs *BadgerStorage) Delete(key []byte) error {
	return bs.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

// Exists checks if a key exists.
func (bs *BadgerStorage) Exists(key []byte) (bool, error) {
	err := bs.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)

		return err
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// ListKeys lists all keys with a given prefix.
func (bs *BadgerStorage) ListKeys(prefix []byte) ([][]byte, error) {
	var keys [][]byte

	err := bs.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)
			keys = append(keys, key)
		}

		return nil
	})

	return keys, err
}

// WriteBatch writes a batch of operations atomically.
func (bs *BadgerStorage) WriteBatch(ops []internal.BatchOp) error {
	return bs.db.Update(func(txn *badger.Txn) error {
		for i, op := range ops {
			switch op.Type {
			case internal.BatchOpSet:
				if err := txn.Set(op.Key, op.Value); err != nil {
					return fmt.Errorf("batch op %d failed: %w", i, err)
				}

			case internal.BatchOpDelete:
				if err := txn.Delete(op.Key); err != nil {
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
func (bs *BadgerStorage) Iterate(prefix []byte, fn func(key, value []byte) error) error {
	return bs.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)

			err := item.Value(func(val []byte) error {
				valueCopy := make([]byte, len(val))
				copy(valueCopy, val)

				return fn(key, valueCopy)
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// GetRange retrieves a range of key-value pairs.
func (bs *BadgerStorage) GetRange(start, end []byte) ([]internal.KeyValue, error) {
	var result []internal.KeyValue

	err := bs.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(start); it.Valid(); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)

			// Check if we've passed the end
			if len(end) > 0 && string(key) > string(end) {
				break
			}

			var value []byte

			err := item.Value(func(val []byte) error {
				value = make([]byte, len(val))
				copy(value, val)

				return nil
			})
			if err != nil {
				return err
			}

			result = append(result, internal.KeyValue{
				Key:   key,
				Value: value,
			})
		}

		return nil
	})

	return result, err
}

// Compact triggers manual compaction.
func (bs *BadgerStorage) Compact() error {
	return bs.db.Flatten(1)
}

// Backup creates a backup of the database.
func (bs *BadgerStorage) Backup(writer any) error {
	// TODO: Implement backup
	return errors.New("backup not yet implemented")
}

// Size returns the approximate size of the database in bytes.
func (bs *BadgerStorage) Size() (int64, error) {
	lsm, vlog := bs.db.Size()

	return lsm + vlog, nil
}

// RunGC runs garbage collection.
func (bs *BadgerStorage) RunGC(discardRatio float64) error {
	return bs.db.RunValueLogGC(discardRatio)
}
