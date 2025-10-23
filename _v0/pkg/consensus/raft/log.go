package raft

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/consensus/storage"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Log represents the Raft distributed log
type Log struct {
	storage     storage.Storage
	logger      common.Logger
	metrics     common.Metrics
	entries     []storage.LogEntry
	commitIndex uint64
	firstIndex  uint64
	lastIndex   uint64
	mu          sync.RWMutex
	started     bool
}

// LogConfig contains configuration for the log
type LogConfig struct {
	Storage storage.Storage
	Logger  common.Logger
	Metrics common.Metrics
}

// NewLog creates a new Raft log
func NewLog(store storage.Storage, logger common.Logger, metrics common.Metrics) (*Log, error) {
	log := &Log{
		storage:     store,
		logger:      logger,
		metrics:     metrics,
		entries:     make([]storage.LogEntry, 0),
		commitIndex: 0,
		firstIndex:  1,
		lastIndex:   0,
	}

	// Load existing log entries from storage
	if err := log.loadFromStorage(); err != nil {
		return nil, fmt.Errorf("failed to load log from storage: %w", err)
	}

	log.started = true
	return log, nil
}

// AppendEntry appends a new entry to the log
func (l *Log) AppendEntry(entry storage.LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	// Set index if not already set
	if entry.Index == 0 {
		entry.Index = l.lastIndex + 1
	}

	// Validate index
	if entry.Index != l.lastIndex+1 {
		return fmt.Errorf("invalid entry index: expected %d, got %d", l.lastIndex+1, entry.Index)
	}

	// Store entry in storage
	if err := l.storage.StoreEntry(context.Background(), entry); err != nil {
		return fmt.Errorf("failed to store log entry: %w", err)
	}

	// Add to in-memory log
	l.entries = append(l.entries, entry)
	l.lastIndex = entry.Index

	if l.logger != nil {
		l.logger.Debug("appended log entry",
			logger.Uint64("index", entry.Index),
			logger.Uint64("term", entry.Term),
			logger.String("type", string(entry.Type)),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.log.entries_appended").Inc()
		l.metrics.Gauge("forge.consensus.raft.log.last_index").Set(float64(l.lastIndex))
	}

	return nil
}

// AppendEntries appends multiple entries to the log
func (l *Log) AppendEntries(entries []storage.LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	for _, entry := range entries {
		// Set index if not already set
		if entry.Index == 0 {
			entry.Index = l.lastIndex + 1
		}

		// Validate index
		if entry.Index != l.lastIndex+1 {
			return fmt.Errorf("invalid entry index: expected %d, got %d", l.lastIndex+1, entry.Index)
		}

		// Store entry in storage
		if err := l.storage.StoreEntry(context.Background(), entry); err != nil {
			return fmt.Errorf("failed to store log entry: %w", err)
		}

		// Add to in-memory log
		l.entries = append(l.entries, entry)
		l.lastIndex = entry.Index
	}

	if l.logger != nil {
		l.logger.Debug("appended log entries",
			logger.Int("count", len(entries)),
			logger.Uint64("last_index", l.lastIndex),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.log.entries_appended").Add(float64(len(entries)))
		l.metrics.Gauge("forge.consensus.raft.log.last_index").Set(float64(l.lastIndex))
	}

	return nil
}

// GetEntry returns the log entry at the specified index
func (l *Log) GetEntry(index uint64) (storage.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.started {
		return storage.LogEntry{}, fmt.Errorf("log not started")
	}

	if index < l.firstIndex || index > l.lastIndex {
		return storage.LogEntry{}, fmt.Errorf("index %d out of range [%d, %d]", index, l.firstIndex, l.lastIndex)
	}

	// Try to get from in-memory log first
	arrayIndex := index - l.firstIndex
	if arrayIndex < uint64(len(l.entries)) {
		return l.entries[arrayIndex], nil
	}

	// Fallback to storage
	entry, err := l.storage.GetEntry(context.Background(), index)
	if err != nil {
		return storage.LogEntry{}, fmt.Errorf("failed to load log entry from storage: %w", err)
	}

	return *entry, nil
}

// GetEntries returns log entries in the specified range [start, end]
func (l *Log) GetEntries(start, end uint64) ([]storage.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.started {
		return nil, fmt.Errorf("log not started")
	}

	if start < l.firstIndex || end > l.lastIndex || start > end {
		return nil, fmt.Errorf("invalid range [%d, %d], log range is [%d, %d]", start, end, l.firstIndex, l.lastIndex)
	}

	entries := make([]storage.LogEntry, 0, end-start+1)
	for i := start; i <= end; i++ {
		entry, err := l.GetEntry(i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// GetEntriesAfter returns all log entries after the specified index
func (l *Log) GetEntriesAfter(index uint64) ([]storage.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.started {
		return nil, fmt.Errorf("log not started")
	}

	if index >= l.lastIndex {
		return []storage.LogEntry{}, nil
	}

	return l.GetEntries(index+1, l.lastIndex)
}

// GetEntriesFrom returns all log entries from the specified index
func (l *Log) GetEntriesFrom(index uint64) ([]storage.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.started {
		return nil, fmt.Errorf("log not started")
	}

	if index > l.lastIndex {
		return []storage.LogEntry{}, nil
	}

	return l.GetEntries(index, l.lastIndex)
}

// TruncateAfter truncates the log after the specified index
func (l *Log) TruncateAfter(index uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	if index < l.firstIndex {
		return fmt.Errorf("cannot truncate before first index %d", l.firstIndex)
	}

	if index >= l.lastIndex {
		// Nothing to truncate
		return nil
	}

	// Truncate in storage
	if err := l.storage.Compact(context.Background(), index); err != nil {
		return fmt.Errorf("failed to truncate log in storage: %w", err)
	}

	// Truncate in-memory log
	if index >= l.firstIndex && index < l.firstIndex+uint64(len(l.entries)) {
		arrayIndex := index - l.firstIndex + 1
		l.entries = l.entries[:arrayIndex]
	}

	oldLastIndex := l.lastIndex
	l.lastIndex = index

	if l.logger != nil {
		l.logger.Info("truncated log",
			logger.Uint64("index", index),
			logger.Uint64("old_last_index", oldLastIndex),
			logger.Uint64("new_last_index", l.lastIndex),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.log.truncations").Inc()
		l.metrics.Gauge("forge.consensus.raft.log.last_index").Set(float64(l.lastIndex))
	}

	return nil
}

// TruncateBefore truncates the log before the specified index (for snapshots)
func (l *Log) TruncateBefore(index uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	if index <= l.firstIndex {
		// Nothing to truncate
		return nil
	}

	if index > l.lastIndex {
		return fmt.Errorf("cannot truncate beyond last index %d", l.lastIndex)
	}

	// Remove entries from in-memory log
	if index > l.firstIndex {
		entriesToRemove := index - l.firstIndex
		if entriesToRemove < uint64(len(l.entries)) {
			l.entries = l.entries[entriesToRemove:]
		} else {
			l.entries = []storage.LogEntry{}
		}
	}

	oldFirstIndex := l.firstIndex
	l.firstIndex = index

	if l.logger != nil {
		l.logger.Info("truncated log before index",
			logger.Uint64("index", index),
			logger.Uint64("old_first_index", oldFirstIndex),
			logger.Uint64("new_first_index", l.firstIndex),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.log.truncations").Inc()
		l.metrics.Gauge("forge.consensus.raft.log.first_index").Set(float64(l.firstIndex))
	}

	return nil
}

// FirstIndex returns the first index in the log
func (l *Log) FirstIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.firstIndex
}

// LastIndex returns the last index in the log
func (l *Log) LastIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.lastIndex
}

// LastTerm returns the term of the last entry in the log
func (l *Log) LastTerm() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.lastIndex == 0 {
		return 0
	}

	// Try to get from in-memory log first
	if len(l.entries) > 0 {
		return l.entries[len(l.entries)-1].Term
	}

	// Fallback to storage
	entry, err := l.storage.GetEntry(context.Background(), l.lastIndex)
	if err != nil {
		if l.logger != nil {
			l.logger.Error("failed to get last term from storage", logger.Error(err))
		}
		return 0
	}

	return entry.Term
}

// GetTermAtIndex returns the term at the specified index
func (l *Log) GetTermAtIndex(index uint64) (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if index == 0 {
		return 0, nil
	}

	if index < l.firstIndex || index > l.lastIndex {
		return 0, fmt.Errorf("index %d out of range [%d, %d]", index, l.firstIndex, l.lastIndex)
	}

	// Try to get from in-memory log first
	arrayIndex := index - l.firstIndex
	if arrayIndex < uint64(len(l.entries)) {
		return l.entries[arrayIndex].Term, nil
	}

	// Fallback to storage
	entry, err := l.storage.GetEntry(context.Background(), index)
	if err != nil {
		return 0, fmt.Errorf("failed to load log entry from storage: %w", err)
	}

	return entry.Term, nil
}

// GetCommitIndex returns the commit index
func (l *Log) GetCommitIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.commitIndex
}

// SetCommitIndex sets the commit index
func (l *Log) SetCommitIndex(index uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index > l.lastIndex {
		if l.logger != nil {
			l.logger.Warn("commit index beyond last index",
				logger.Uint64("commit_index", index),
				logger.Uint64("last_index", l.lastIndex),
			)
		}
		return
	}

	oldCommitIndex := l.commitIndex
	l.commitIndex = index

	if l.logger != nil {
		l.logger.Debug("commit index updated",
			logger.Uint64("old_commit_index", oldCommitIndex),
			logger.Uint64("new_commit_index", l.commitIndex),
		)
	}

	if l.metrics != nil {
		l.metrics.Gauge("forge.consensus.raft.log.commit_index").Set(float64(l.commitIndex))
	}
}

// Size returns the size of the log in bytes
func (l *Log) Size() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.storage.GetStats().StorageSize
}

// Count returns the number of entries in the log
func (l *Log) Count() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.lastIndex < l.firstIndex {
		return 0
	}

	return l.lastIndex - l.firstIndex + 1
}

// IsEmpty returns true if the log is empty
func (l *Log) IsEmpty() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.lastIndex == 0
}

// Contains returns true if the log contains the specified index
func (l *Log) Contains(index uint64) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return index >= l.firstIndex && index <= l.lastIndex
}

// HealthCheck performs a health check on the log
func (l *Log) HealthCheck(ctx context.Context) error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	// Check storage health
	if err := l.storage.HealthCheck(ctx); err != nil {
		return fmt.Errorf("storage unhealthy: %w", err)
	}

	// Check log consistency
	if l.lastIndex < l.firstIndex-1 {
		return fmt.Errorf("log index inconsistency: first=%d, last=%d", l.firstIndex, l.lastIndex)
	}

	// Check commit index consistency
	if l.commitIndex > l.lastIndex {
		return fmt.Errorf("commit index beyond last index: commit=%d, last=%d", l.commitIndex, l.lastIndex)
	}

	return nil
}

// GetLogInfo returns information about the log
func (l *Log) GetLogInfo() LogInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return LogInfo{
		FirstIndex:  l.firstIndex,
		LastIndex:   l.lastIndex,
		CommitIndex: l.commitIndex,
		Size:        l.Size(),
		EntryCount:  int64(l.Count()),
	}
}

// GetLogStats returns log statistics
func (l *Log) GetLogStats() LogStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return LogStats{
		FirstIndex:       l.firstIndex,
		LastIndex:        l.lastIndex,
		CommitIndex:      l.commitIndex,
		Size:             l.Size(),
		EntryCount:       int64(l.Count()),
		InMemoryCount:    int64(len(l.entries)),
		UncommittedCount: int64(l.lastIndex - l.commitIndex),
	}
}

// LogStats contains log statistics
type LogStats struct {
	FirstIndex       uint64 `json:"first_index"`
	LastIndex        uint64 `json:"last_index"`
	CommitIndex      uint64 `json:"commit_index"`
	Size             int64  `json:"size"`
	EntryCount       int64  `json:"entry_count"`
	InMemoryCount    int64  `json:"in_memory_count"`
	UncommittedCount int64  `json:"uncommitted_count"`
}

// loadFromStorage loads log entries from storage
func (l *Log) loadFromStorage() error {
	// This is a simplified version - in production, you'd want to load
	// a reasonable number of recent entries into memory

	// For now, just ensure we have the correct indices
	l.firstIndex = 1
	l.lastIndex = 0
	l.commitIndex = 0

	return nil
}

// Compact compacts the log by removing entries covered by snapshots
func (l *Log) Compact(lastIncludedIndex uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	if lastIncludedIndex <= l.firstIndex {
		// Nothing to compact
		return nil
	}

	if lastIncludedIndex > l.lastIndex {
		return fmt.Errorf("cannot compact beyond last index %d", l.lastIndex)
	}

	// Remove entries from in-memory log
	if lastIncludedIndex > l.firstIndex {
		entriesToRemove := lastIncludedIndex - l.firstIndex + 1
		if entriesToRemove < uint64(len(l.entries)) {
			l.entries = l.entries[entriesToRemove:]
		} else {
			l.entries = []storage.LogEntry{}
		}
	}

	oldFirstIndex := l.firstIndex
	l.firstIndex = lastIncludedIndex + 1

	if l.logger != nil {
		l.logger.Info("compacted log",
			logger.Uint64("last_included_index", lastIncludedIndex),
			logger.Uint64("old_first_index", oldFirstIndex),
			logger.Uint64("new_first_index", l.firstIndex),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.log.compactions").Inc()
		l.metrics.Gauge("forge.consensus.raft.log.first_index").Set(float64(l.firstIndex))
	}

	return nil
}

// Close closes the log
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return nil
	}

	// Close storage
	if err := l.storage.Close(context.Background()); err != nil {
		return fmt.Errorf("failed to close storage: %w", err)
	}

	l.started = false

	if l.logger != nil {
		l.logger.Info("log closed")
	}

	return nil
}

// IsStarted returns true if the log is started
func (l *Log) IsStarted() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.started
}

// GetStorage returns the storage backend
func (l *Log) GetStorage() storage.Storage {
	return l.storage
}

// Flush flushes any pending writes to storage
func (l *Log) Flush() error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	// Storage implementations should handle flushing
	return nil
}

// GetEntryCount returns the number of entries in the specified range
func (l *Log) GetEntryCount(start, end uint64) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if start > end || start < l.firstIndex || end > l.lastIndex {
		return 0
	}

	return int64(end - start + 1)
}

// HasEntry returns true if the log has an entry at the specified index
func (l *Log) HasEntry(index uint64) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return index >= l.firstIndex && index <= l.lastIndex
}

// GetEntryType returns the type of the entry at the specified index
func (l *Log) GetEntryType(index uint64) (storage.EntryType, error) {
	entry, err := l.GetEntry(index)
	if err != nil {
		return "", err
	}
	return entry.Type, nil
}

// GetEntriesByType returns all entries of the specified type in the given range
func (l *Log) GetEntriesByType(entryType storage.EntryType, start, end uint64) ([]storage.LogEntry, error) {
	entries, err := l.GetEntries(start, end)
	if err != nil {
		return nil, err
	}

	var filtered []storage.LogEntry
	for _, entry := range entries {
		if entry.Type == entryType {
			filtered = append(filtered, entry)
		}
	}

	return filtered, nil
}

// TruncateLog truncates the log at the specified index
func (l *Log) TruncateLog(index uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	if index < l.firstIndex {
		return fmt.Errorf("cannot truncate before first index %d", l.firstIndex)
	}

	if index >= l.lastIndex {
		// Nothing to truncate
		return nil
	}

	// Truncate in storage
	if err := l.storage.Compact(context.Background(), index); err != nil {
		return fmt.Errorf("failed to truncate log in storage: %w", err)
	}

	// Truncate in-memory log
	if index >= l.firstIndex && index < l.firstIndex+uint64(len(l.entries)) {
		arrayIndex := index - l.firstIndex + 1
		l.entries = l.entries[:arrayIndex]
	}

	oldLastIndex := l.lastIndex
	l.lastIndex = index

	if l.logger != nil {
		l.logger.Info("truncated log",
			logger.Uint64("index", index),
			logger.Uint64("old_last_index", oldLastIndex),
			logger.Uint64("new_last_index", l.lastIndex),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.log.truncations").Inc()
		l.metrics.Gauge("forge.consensus.raft.log.last_index").Set(float64(l.lastIndex))
	}

	return nil
}

// IsConsistent checks if the log is consistent at the given index and term
func (l *Log) IsConsistent(prevLogIndex, prevLogTerm uint64) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.started {
		return false
	}

	// If prevLogIndex is 0, it's always consistent (start of log)
	if prevLogIndex == 0 {
		return true
	}

	// Check if we have the entry at prevLogIndex
	if prevLogIndex < l.firstIndex || prevLogIndex > l.lastIndex {
		return false
	}

	// Get the term at prevLogIndex
	term, err := l.GetTermAtIndex(prevLogIndex)
	if err != nil {
		if l.logger != nil {
			l.logger.Error("failed to get term at index for consistency check",
				logger.Uint64("index", prevLogIndex),
				logger.Error(err),
			)
		}
		return false
	}

	// Check if terms match
	consistent := term == prevLogTerm

	if l.logger != nil {
		l.logger.Debug("log consistency check",
			logger.Uint64("prev_log_index", prevLogIndex),
			logger.Uint64("prev_log_term", prevLogTerm),
			logger.Uint64("actual_term", term),
			logger.Bool("consistent", consistent),
		)
	}

	return consistent
}

// AppendEntriesByIndex appends entries to the log after validating consistency
func (l *Log) AppendEntriesByIndex(prevLogIndex uint64, entries []storage.LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return fmt.Errorf("log not started")
	}

	if len(entries) == 0 {
		return nil
	}

	// Validate consistency
	if !l.isConsistentUnlocked(prevLogIndex, entries[0].Term) {
		return fmt.Errorf("log inconsistent at index %d", prevLogIndex)
	}

	// Remove conflicting entries
	if prevLogIndex < l.lastIndex {
		// Remove entries after prevLogIndex
		if err := l.truncateAfterUnlocked(prevLogIndex); err != nil {
			return fmt.Errorf("failed to truncate conflicting entries: %w", err)
		}
	}

	// Append new entries
	for i, entry := range entries {
		// Set index if not already set
		if entry.Index == 0 {
			entry.Index = prevLogIndex + uint64(i) + 1
		}

		// Validate index
		expectedIndex := prevLogIndex + uint64(i) + 1
		if entry.Index != expectedIndex {
			return fmt.Errorf("invalid entry index: expected %d, got %d", expectedIndex, entry.Index)
		}

		// Store entry in storage
		if err := l.storage.StoreEntry(context.Background(), entry); err != nil {
			return fmt.Errorf("failed to store log entry: %w", err)
		}

		// Add to in-memory log
		l.entries = append(l.entries, entry)
		l.lastIndex = entry.Index
	}

	if l.logger != nil {
		l.logger.Debug("appended entries after consistency check",
			logger.Uint64("prev_log_index", prevLogIndex),
			logger.Int("count", len(entries)),
			logger.Uint64("new_last_index", l.lastIndex),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.log.entries_appended").Add(float64(len(entries)))
		l.metrics.Gauge("forge.consensus.raft.log.last_index").Set(float64(l.lastIndex))
	}

	return nil
}

// isConsistentUnlocked checks consistency without locking (assumes lock is held)
func (l *Log) isConsistentUnlocked(prevLogIndex, prevLogTerm uint64) bool {
	if prevLogIndex == 0 {
		return true
	}

	if prevLogIndex < l.firstIndex || prevLogIndex > l.lastIndex {
		return false
	}

	// Get the term at prevLogIndex
	var term uint64
	arrayIndex := prevLogIndex - l.firstIndex
	if arrayIndex < uint64(len(l.entries)) {
		term = l.entries[arrayIndex].Term
	} else {
		// Fallback to storage
		entry, err := l.storage.GetEntry(context.Background(), prevLogIndex)
		if err != nil {
			return false
		}
		term = entry.Term
	}

	return term == prevLogTerm
}

// truncateAfterUnlocked truncates the log after the specified index without locking
func (l *Log) truncateAfterUnlocked(index uint64) error {
	if index < l.firstIndex {
		return fmt.Errorf("cannot truncate before first index %d", l.firstIndex)
	}

	if index >= l.lastIndex {
		// Nothing to truncate
		return nil
	}

	// Truncate in storage
	if err := l.storage.Compact(context.Background(), index); err != nil {
		return fmt.Errorf("failed to truncate log in storage: %w", err)
	}

	// Truncate in-memory log
	if index >= l.firstIndex && index < l.firstIndex+uint64(len(l.entries)) {
		arrayIndex := index - l.firstIndex + 1
		l.entries = l.entries[:arrayIndex]
	}

	l.lastIndex = index

	return nil
}
