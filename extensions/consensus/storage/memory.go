package storage

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// MemoryStorage implements an in-memory storage backend
type MemoryStorage struct {
	data    map[string][]byte
	mu      sync.RWMutex
	logger  forge.Logger
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// MemoryStorageConfig contains configuration for memory storage
type MemoryStorageConfig struct {
	InitialCapacity int
}

// NewMemoryStorage creates a new memory storage
func NewMemoryStorage(config MemoryStorageConfig, logger forge.Logger) *MemoryStorage {
	capacity := config.InitialCapacity
	if capacity == 0 {
		capacity = 1000
	}

	return &MemoryStorage{
		data:   make(map[string][]byte, capacity),
		logger: logger,
	}
}

// Start starts the storage backend
func (s *MemoryStorage) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return internal.ErrAlreadyStarted
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.started = true

	s.logger.Info("memory storage started")
	return nil
}

// Stop stops the storage backend
func (s *MemoryStorage) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return internal.ErrNotStarted
	}

	if s.cancel != nil {
		s.cancel()
	}

	s.logger.Info("memory storage stopped")
	return nil
}

// Set stores a key-value pair
func (s *MemoryStorage) Set(key, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Make a copy of the value to ensure immutability
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	s.data[string(key)] = valueCopy
	return nil
}

// Get retrieves a value by key
func (s *MemoryStorage) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, exists := s.data[string(key)]
	if !exists {
		return nil, internal.ErrNodeNotFound // Using generic error for key not found
	}

	// Make a copy to ensure immutability
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	return valueCopy, nil
}

// Delete deletes a key
func (s *MemoryStorage) Delete(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, string(key))
	return nil
}

// Exists checks if a key exists
func (s *MemoryStorage) Exists(key []byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.data[string(key)]
	return exists, nil
}

// ListKeys lists all keys with a given prefix
func (s *MemoryStorage) ListKeys(prefix []byte) ([][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefixStr := string(prefix)
	keys := make([][]byte, 0)

	for key := range s.data {
		if len(prefix) == 0 || (len(key) >= len(prefixStr) && key[:len(prefixStr)] == prefixStr) {
			keyCopy := make([]byte, len(key))
			copy(keyCopy, []byte(key))
			keys = append(keys, keyCopy)
		}
	}

	return keys, nil
}

// WriteBatch writes a batch of operations atomically
func (s *MemoryStorage) WriteBatch(ops []internal.BatchOp) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate all operations first
	for i, op := range ops {
		if len(op.Key) == 0 {
			return fmt.Errorf("batch operation %d: empty key", i)
		}
	}

	// Apply all operations
	for _, op := range ops {
		switch op.Type {
		case internal.BatchOpSet:
			valueCopy := make([]byte, len(op.Value))
			copy(valueCopy, op.Value)
			s.data[string(op.Key)] = valueCopy

		case internal.BatchOpDelete:
			delete(s.data, string(op.Key))

		default:
			return fmt.Errorf("unknown batch operation type: %d", op.Type)
		}
	}

	return nil
}

// Iterate iterates over all key-value pairs
func (s *MemoryStorage) Iterate(prefix []byte, fn func(key, value []byte) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefixStr := string(prefix)

	for key, value := range s.data {
		if len(prefix) == 0 || (len(key) >= len(prefixStr) && key[:len(prefixStr)] == prefixStr) {
			keyCopy := make([]byte, len(key))
			copy(keyCopy, []byte(key))

			valueCopy := make([]byte, len(value))
			copy(valueCopy, value)

			if err := fn(keyCopy, valueCopy); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetRange retrieves a range of key-value pairs
func (s *MemoryStorage) GetRange(start, end []byte) ([]internal.KeyValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]internal.KeyValue, 0)

	for key, value := range s.data {
		if bytes.Compare([]byte(key), start) >= 0 && bytes.Compare([]byte(key), end) <= 0 {
			keyCopy := make([]byte, len(key))
			copy(keyCopy, []byte(key))

			valueCopy := make([]byte, len(value))
			copy(valueCopy, value)

			result = append(result, internal.KeyValue{
				Key:   keyCopy,
				Value: valueCopy,
			})
		}
	}

	return result, nil
}

// Size returns the number of stored keys
func (s *MemoryStorage) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

// Clear removes all stored data
func (s *MemoryStorage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = make(map[string][]byte)
	s.logger.Info("memory storage cleared")

	return nil
}
