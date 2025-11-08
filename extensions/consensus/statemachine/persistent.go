package statemachine

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/internal/errors"
)

// PersistentStateMachine is a state machine backed by persistent storage.
type PersistentStateMachine struct {
	storage internal.Storage
	logger  forge.Logger

	// In-memory cache for fast reads
	cache   map[string][]byte
	cacheMu sync.RWMutex

	// Apply queue for batching
	applyQueue    []internal.LogEntry
	applyQueueMu  sync.Mutex
	applyNotify   chan struct{}
	batchSize     int
	batchInterval time.Duration

	// Statistics
	appliedCount uint64
	lastApplied  uint64
	lastSnapshot uint64
	cacheHits    uint64
	cacheMisses  uint64
	statsMu      sync.RWMutex

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// PersistentStateMachineConfig contains persistent state machine configuration.
type PersistentStateMachineConfig struct {
	Storage       internal.Storage
	EnableCache   bool
	MaxCacheSize  int
	BatchSize     int
	BatchInterval time.Duration
	SyncWrites    bool
}

// NewPersistentStateMachine creates a new persistent state machine.
func NewPersistentStateMachine(config PersistentStateMachineConfig, logger forge.Logger) (*PersistentStateMachine, error) {
	if config.Storage == nil {
		return nil, errors.New("storage is required")
	}

	if config.BatchSize == 0 {
		config.BatchSize = 100
	}

	if config.BatchInterval == 0 {
		config.BatchInterval = 10 * time.Millisecond
	}

	if config.MaxCacheSize == 0 {
		config.MaxCacheSize = 10000
	}

	psm := &PersistentStateMachine{
		storage:       config.Storage,
		logger:        logger,
		batchSize:     config.BatchSize,
		batchInterval: config.BatchInterval,
		applyQueue:    make([]internal.LogEntry, 0, config.BatchSize),
		applyNotify:   make(chan struct{}, 1),
	}

	if config.EnableCache {
		psm.cache = make(map[string][]byte)
	}

	return psm, nil
}

// Start starts the persistent state machine.
func (psm *PersistentStateMachine) Start(ctx context.Context) error {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	if psm.started {
		return internal.ErrAlreadyStarted
	}

	psm.ctx, psm.cancel = context.WithCancel(ctx)
	psm.started = true

	// Start apply worker
	psm.wg.Add(1)

	go psm.applyWorker()

	psm.logger.Info("persistent state machine started",
		forge.F("batch_size", psm.batchSize),
		forge.F("batch_interval", psm.batchInterval),
		forge.F("cache_enabled", psm.cache != nil),
	)

	return nil
}

// Stop stops the persistent state machine.
func (psm *PersistentStateMachine) Stop(ctx context.Context) error {
	psm.mu.Lock()

	if !psm.started {
		psm.mu.Unlock()

		return internal.ErrNotStarted
	}

	psm.mu.Unlock()

	if psm.cancel != nil {
		psm.cancel()
	}

	// Flush any pending applies
	psm.flushApplyQueue()

	// Wait for workers
	done := make(chan struct{})

	go func() {
		psm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		psm.logger.Info("persistent state machine stopped")
	case <-ctx.Done():
		psm.logger.Warn("persistent state machine stop timed out")
	}

	return nil
}

// Apply applies a log entry to the state machine.
func (psm *PersistentStateMachine) Apply(entry internal.LogEntry) error {
	// Add to apply queue
	psm.applyQueueMu.Lock()
	psm.applyQueue = append(psm.applyQueue, entry)
	queueSize := len(psm.applyQueue)
	psm.applyQueueMu.Unlock()

	// Notify worker if batch size reached
	if queueSize >= psm.batchSize {
		select {
		case psm.applyNotify <- struct{}{}:
		default:
		}
	}

	return nil
}

// Get retrieves a value from the state machine.
func (psm *PersistentStateMachine) Get(key string) ([]byte, error) {
	// Check cache first
	if psm.cache != nil {
		psm.cacheMu.RLock()

		if value, exists := psm.cache[key]; exists {
			psm.cacheMu.RUnlock()

			psm.statsMu.Lock()
			psm.cacheHits++
			psm.statsMu.Unlock()

			return value, nil
		}

		psm.cacheMu.RUnlock()

		psm.statsMu.Lock()
		psm.cacheMisses++
		psm.statsMu.Unlock()
	}

	// Read from storage
	storageKey := []byte("sm/" + key)

	value, err := psm.storage.Get(storageKey)
	if err != nil {
		return nil, err
	}

	// Update cache
	if psm.cache != nil {
		psm.updateCache(key, value)
	}

	return value, nil
}

// CreateSnapshot creates a snapshot of the state machine.
func (psm *PersistentStateMachine) CreateSnapshot() ([]byte, error) {
	psm.logger.Info("creating persistent state machine snapshot")

	// Get all keys with state machine prefix
	prefix := []byte("sm/")
	// Use GetRange to list keys (ListKeys not available in Storage interface)
	endKey := []byte("sm/~") // ~ is after all ASCII characters

	kvPairs, err := psm.storage.GetRange(prefix, endKey)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	// Build snapshot
	snapshot := make(map[string][]byte)

	for _, kv := range kvPairs {
		// Remove prefix
		snapshotKey := string(kv.Key[len(prefix):])
		snapshot[snapshotKey] = kv.Value
	}

	// Encode snapshot
	var buf bytes.Buffer

	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(snapshot); err != nil {
		return nil, fmt.Errorf("failed to encode snapshot: %w", err)
	}

	psm.statsMu.Lock()
	psm.lastSnapshot = psm.lastApplied
	psm.statsMu.Unlock()

	psm.logger.Info("snapshot created",
		forge.F("entries", len(snapshot)),
		forge.F("size_bytes", buf.Len()),
	)

	return buf.Bytes(), nil
}

// RestoreSnapshot restores a snapshot to the state machine.
func (psm *PersistentStateMachine) RestoreSnapshot(data []byte) error {
	psm.logger.Info("restoring persistent state machine snapshot",
		forge.F("size_bytes", len(data)),
	)

	// Decode snapshot
	buf := bytes.NewReader(data)
	decoder := gob.NewDecoder(buf)

	var snapshot map[string][]byte
	if err := decoder.Decode(&snapshot); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Clear existing data
	prefix := []byte("sm/")
	// Use GetRange to list keys (ListKeys not available in Storage interface)
	endKey := []byte("sm/~") // ~ is after all ASCII characters

	kvPairs, err := psm.storage.GetRange(prefix, endKey)
	if err != nil {
		return fmt.Errorf("failed to list keys: %w", err)
	}

	// Build batch operations
	var ops []internal.BatchOp

	// Delete existing keys
	for _, kv := range kvPairs {
		ops = append(ops, internal.BatchOp{
			Type: internal.BatchOpDelete,
			Key:  kv.Key,
		})
	}

	// Add snapshot data
	for key, value := range snapshot {
		storageKey := []byte("sm/" + key)
		ops = append(ops, internal.BatchOp{
			Type:  internal.BatchOpSet,
			Key:   storageKey,
			Value: value,
		})
	}

	// Execute batch
	if err := psm.storage.Batch(ops); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}

	// Clear and rebuild cache
	if psm.cache != nil {
		psm.cacheMu.Lock()

		psm.cache = make(map[string][]byte)
		maps.Copy(psm.cache, snapshot)

		psm.cacheMu.Unlock()
	}

	psm.logger.Info("snapshot restored",
		forge.F("entries", len(snapshot)),
	)

	return nil
}

// applyWorker processes the apply queue in batches.
func (psm *PersistentStateMachine) applyWorker() {
	defer psm.wg.Done()

	ticker := time.NewTicker(psm.batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-psm.ctx.Done():
			return

		case <-psm.applyNotify:
			psm.flushApplyQueue()

		case <-ticker.C:
			psm.flushApplyQueue()
		}
	}
}

// flushApplyQueue flushes the apply queue to storage.
func (psm *PersistentStateMachine) flushApplyQueue() {
	psm.applyQueueMu.Lock()

	if len(psm.applyQueue) == 0 {
		psm.applyQueueMu.Unlock()

		return
	}

	// Take snapshot of queue
	queue := make([]internal.LogEntry, len(psm.applyQueue))
	copy(queue, psm.applyQueue)
	psm.applyQueue = psm.applyQueue[:0]
	psm.applyQueueMu.Unlock()

	// Build batch operations
	var ops []internal.BatchOp

	cacheUpdates := make(map[string][]byte)

	for _, entry := range queue {
		// Parse command
		cmd, err := psm.parseCommand(entry.Data)
		if err != nil {
			psm.logger.Error("failed to parse command",
				forge.F("index", entry.Index),
				forge.F("error", err),
			)

			continue
		}

		// Build operation
		storageKey := []byte("sm/" + cmd.Key)

		switch cmd.Op {
		case "set":
			ops = append(ops, internal.BatchOp{
				Type:  internal.BatchOpSet,
				Key:   storageKey,
				Value: cmd.Value,
			})
			cacheUpdates[cmd.Key] = cmd.Value

		case "delete":
			ops = append(ops, internal.BatchOp{
				Type: internal.BatchOpDelete,
				Key:  storageKey,
			})
			cacheUpdates[cmd.Key] = nil
		}
	}

	// Execute batch
	if len(ops) > 0 {
		if err := psm.storage.Batch(ops); err != nil {
			psm.logger.Error("failed to write batch",
				forge.F("operations", len(ops)),
				forge.F("error", err),
			)

			return
		}
	}

	// Update cache
	if psm.cache != nil {
		psm.cacheMu.Lock()

		for key, value := range cacheUpdates {
			if value == nil {
				delete(psm.cache, key)
			} else {
				psm.cache[key] = value
			}
		}

		psm.cacheMu.Unlock()
	}

	// Update statistics
	psm.statsMu.Lock()

	psm.appliedCount += uint64(len(queue))
	if len(queue) > 0 {
		psm.lastApplied = queue[len(queue)-1].Index
	}

	psm.statsMu.Unlock()

	psm.logger.Debug("flushed apply queue",
		forge.F("entries", len(queue)),
		forge.F("operations", len(ops)),
	)
}

// parseCommand parses a command from data.
func (psm *PersistentStateMachine) parseCommand(data []byte) (*Command, error) {
	var cmd Command

	buf := bytes.NewReader(data)
	decoder := gob.NewDecoder(buf)

	if err := decoder.Decode(&cmd); err != nil {
		return nil, err
	}

	return &cmd, nil
}

// updateCache updates the cache with size limiting.
func (psm *PersistentStateMachine) updateCache(key string, value []byte) {
	psm.cacheMu.Lock()
	defer psm.cacheMu.Unlock()

	// Simple LRU: if cache is full, don't add new entries
	// In production, implement proper LRU eviction
	if len(psm.cache) < 10000 {
		psm.cache[key] = value
	}
}

// GetStats returns state machine statistics.
func (psm *PersistentStateMachine) GetStats() map[string]any {
	psm.statsMu.RLock()
	defer psm.statsMu.RUnlock()

	psm.applyQueueMu.Lock()
	queueSize := len(psm.applyQueue)
	psm.applyQueueMu.Unlock()

	stats := map[string]any{
		"applied_count": psm.appliedCount,
		"last_applied":  psm.lastApplied,
		"last_snapshot": psm.lastSnapshot,
		"queue_size":    queueSize,
	}

	if psm.cache != nil {
		psm.cacheMu.RLock()
		cacheSize := len(psm.cache)
		psm.cacheMu.RUnlock()

		stats["cache_size"] = cacheSize
		stats["cache_hits"] = psm.cacheHits
		stats["cache_misses"] = psm.cacheMisses

		if psm.cacheHits+psm.cacheMisses > 0 {
			hitRate := float64(psm.cacheHits) / float64(psm.cacheHits+psm.cacheMisses)
			stats["cache_hit_rate"] = hitRate
		}
	}

	return stats
}

// Command represents a state machine command.
type Command struct {
	Op    string
	Key   string
	Value []byte
}
