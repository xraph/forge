package raft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Log represents the replicated log.
type Log struct {
	entries []internal.LogEntry
	cache   map[uint64]internal.LogEntry
	storage internal.Storage
	logger  forge.Logger

	// Cache management
	cacheSize int
	mu        sync.RWMutex

	// Snapshot state
	snapshotIndex uint64
	snapshotTerm  uint64
}

// NewLog creates a new log.
func NewLog(logger forge.Logger, storage internal.Storage, cacheSize int) (*Log, error) {
	l := &Log{
		entries:   make([]internal.LogEntry, 0),
		cache:     make(map[uint64]internal.LogEntry),
		storage:   storage,
		logger:    logger,
		cacheSize: cacheSize,
	}

	// Load existing log from storage
	if err := l.load(); err != nil {
		return nil, fmt.Errorf("failed to load log: %w", err)
	}

	return l, nil
}

// Append appends a log entry.
func (l *Log) Append(entry internal.LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Append to in-memory log
	l.entries = append(l.entries, entry)

	// Add to cache
	l.cache[entry.Index] = entry
	l.evictCache()

	// Persist to storage
	return l.persistEntry(entry)
}

// AppendBatch appends multiple log entries.
func (l *Log) AppendBatch(entries []internal.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Append to in-memory log
	l.entries = append(l.entries, entries...)

	// Add to cache
	for _, entry := range entries {
		l.cache[entry.Index] = entry
	}

	l.evictCache()

	// Persist to storage in batch
	return l.persistBatch(entries)
}

// Get retrieves a log entry by index.
func (l *Log) Get(index uint64) (internal.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Check if index is in snapshot
	if index <= l.snapshotIndex {
		return internal.LogEntry{}, fmt.Errorf("log entry %d is in snapshot", index)
	}

	// Check cache first
	if entry, ok := l.cache[index]; ok {
		return entry, nil
	}

	// Check in-memory entries
	for _, entry := range l.entries {
		if entry.Index == index {
			l.cache[index] = entry

			return entry, nil
		}
	}

	// Load from storage
	return l.loadEntry(index)
}

// GetRange retrieves a range of log entries.
func (l *Log) GetRange(start, end uint64) ([]internal.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if start > end {
		return nil, fmt.Errorf("invalid range: start %d > end %d", start, end)
	}

	if start <= l.snapshotIndex {
		return nil, fmt.Errorf("start index %d is in snapshot", start)
	}

	result := make([]internal.LogEntry, 0, end-start+1)

	for i := start; i <= end; i++ {
		// Check cache first
		if entry, ok := l.cache[i]; ok {
			result = append(result, entry)

			continue
		}

		// Check in-memory entries
		found := false

		for _, entry := range l.entries {
			if entry.Index == i {
				result = append(result, entry)
				l.cache[i] = entry
				found = true

				break
			}
		}

		if !found {
			// Load from storage
			entry, err := l.loadEntry(i)
			if err != nil {
				return nil, fmt.Errorf("failed to load entry %d: %w", i, err)
			}

			result = append(result, entry)
			l.cache[i] = entry
		}
	}

	return result, nil
}

// LastIndex returns the index of the last log entry.
func (l *Log) LastIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.entries) == 0 {
		return l.snapshotIndex
	}

	return l.entries[len(l.entries)-1].Index
}

// LastTerm returns the term of the last log entry.
func (l *Log) LastTerm() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.entries) == 0 {
		return l.snapshotTerm
	}

	return l.entries[len(l.entries)-1].Term
}

// FirstIndex returns the index of the first log entry.
func (l *Log) FirstIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.entries) == 0 {
		return l.snapshotIndex + 1
	}

	return l.entries[0].Index
}

// Count returns the number of log entries.
func (l *Log) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return len(l.entries)
}

// TruncateAfter truncates the log after the given index.
func (l *Log) TruncateAfter(index uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index <= l.snapshotIndex {
		return fmt.Errorf("cannot truncate before snapshot index %d", l.snapshotIndex)
	}

	// Find the position to truncate
	truncatePos := -1

	for i, entry := range l.entries {
		if entry.Index > index {
			truncatePos = i

			break
		}
	}

	if truncatePos == -1 {
		return nil // Nothing to truncate
	}

	// Truncate in-memory log
	l.entries = l.entries[:truncatePos]

	// Clear cache for truncated entries
	for i := index + 1; i <= l.snapshotIndex+uint64(len(l.entries))+1000; i++ {
		delete(l.cache, i)
	}

	l.logger.Info("truncated log",
		forge.F("after_index", index),
		forge.F("entries_removed", len(l.entries)-truncatePos),
	)

	return nil
}

// Compact compacts the log up to the given index (inclusive).
func (l *Log) Compact(index uint64, term uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index <= l.snapshotIndex {
		return nil // Already compacted
	}

	// Find the position to compact
	compactPos := -1

	for i, entry := range l.entries {
		if entry.Index == index {
			compactPos = i

			break
		}
	}

	if compactPos == -1 {
		return fmt.Errorf("log entry %d not found", index)
	}

	// Remove compacted entries
	l.entries = l.entries[compactPos+1:]

	// Update snapshot state
	l.snapshotIndex = index
	l.snapshotTerm = term

	// Clear cache for compacted entries
	for i := uint64(0); i <= index; i++ {
		delete(l.cache, i)
	}

	l.logger.Info("compacted log",
		forge.F("up_to_index", index),
		forge.F("snapshot_term", term),
		forge.F("entries_remaining", len(l.entries)),
	)

	return nil
}

// load loads the log from storage.
func (l *Log) load() error {
	// Load snapshot metadata
	snapshotMeta, err := l.storage.Get([]byte("snapshot_meta"))
	if err == nil && len(snapshotMeta) > 0 {
		var meta struct {
			Index uint64 `json:"index"`
			Term  uint64 `json:"term"`
		}
		if err := json.Unmarshal(snapshotMeta, &meta); err == nil {
			l.snapshotIndex = meta.Index
			l.snapshotTerm = meta.Term
		}
	}

	// Load log entries
	// In a real implementation, we'd iterate through storage keys
	// For now, this is a placeholder

	l.logger.Info("loaded log",
		forge.F("snapshot_index", l.snapshotIndex),
		forge.F("snapshot_term", l.snapshotTerm),
		forge.F("entries", len(l.entries)),
	)

	return nil
}

// persistEntry persists a single log entry to storage.
func (l *Log) persistEntry(entry internal.LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	key := fmt.Appendf(nil, "log_%d", entry.Index)

	return l.storage.Set(key, data)
}

// persistBatch persists multiple log entries to storage.
func (l *Log) persistBatch(entries []internal.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Use batch write if supported
	if batchStorage, ok := l.storage.(interface {
		WriteBatch(ops []internal.BatchOp) error
	}); ok {
		ops := make([]internal.BatchOp, len(entries))
		for i, entry := range entries {
			data, err := json.Marshal(entry)
			if err != nil {
				return fmt.Errorf("failed to marshal entry %d: %w", entry.Index, err)
			}

			ops[i] = internal.BatchOp{
				Type:  internal.BatchOpSet,
				Key:   fmt.Appendf(nil, "log_%d", entry.Index),
				Value: data,
			}
		}

		return batchStorage.WriteBatch(ops)
	}

	// Fall back to individual writes
	for _, entry := range entries {
		if err := l.persistEntry(entry); err != nil {
			return err
		}
	}

	return nil
}

// loadEntry loads a single log entry from storage.
func (l *Log) loadEntry(index uint64) (internal.LogEntry, error) {
	key := fmt.Appendf(nil, "log_%d", index)

	data, err := l.storage.Get(key)
	if err != nil {
		return internal.LogEntry{}, fmt.Errorf("failed to load entry: %w", err)
	}

	var entry internal.LogEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return internal.LogEntry{}, fmt.Errorf("failed to unmarshal entry: %w", err)
	}

	return entry, nil
}

// evictCache evicts old entries from the cache if it exceeds the size limit.
func (l *Log) evictCache() {
	if len(l.cache) <= l.cacheSize {
		return
	}

	// Find oldest entries to evict
	// Simple strategy: keep the most recent entries
	if len(l.entries) > 0 {
		lastIndex := l.entries[len(l.entries)-1].Index

		minIndex := lastIndex
		if uint64(l.cacheSize) < lastIndex {
			minIndex = lastIndex - uint64(l.cacheSize)
		}

		for index := range l.cache {
			if index < minIndex {
				delete(l.cache, index)
			}
		}
	}
}

// MatchesTerm checks if the log entry at index has the given term.
func (l *Log) MatchesTerm(index, term uint64) bool {
	if index == 0 {
		return true
	}

	if index == l.snapshotIndex {
		return term == l.snapshotTerm
	}

	entry, err := l.Get(index)
	if err != nil {
		return false
	}

	return entry.Term == term
}

// GetEntriesAfter returns all entries after the given index.
func (l *Log) GetEntriesAfter(index uint64, maxEntries int) ([]internal.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if index < l.snapshotIndex {
		return nil, fmt.Errorf("index %d is before snapshot index %d", index, l.snapshotIndex)
	}

	result := make([]internal.LogEntry, 0, maxEntries)

	for _, entry := range l.entries {
		if entry.Index > index {
			result = append(result, entry)
			if len(result) >= maxEntries {
				break
			}
		}
	}

	return result, nil
}

// String returns a string representation of the log.
func (l *Log) String() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("Log[snapshot=%d:%d, entries=%d, first=%d, last=%d]",
		l.snapshotIndex, l.snapshotTerm, len(l.entries),
		l.FirstIndex(), l.LastIndex()))

	return buf.String()
}
