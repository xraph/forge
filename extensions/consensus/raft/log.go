package raft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// snapshotMetaKey holds the index and term of the latest snapshot.
const snapshotMetaKey = "snapshot_meta"

// Log represents the replicated log.
//
// Every entry after the last snapshot is held in memory in a slice addressed
// by index: entries[i].Index == snapshotIndex+1+i always holds. That makes Get,
// Term and range reads O(1) and lets AppendEntries compare, skip and truncate
// entries by index as section 5.3 of the Raft paper describes.
//
// Storage is the source of truth. Entries are written to storage before they
// become visible in memory, truncation deletes them from storage, and NewLog
// reloads them so a restarted node comes back with the log it acknowledged.
type Log struct {
	entries []internal.LogEntry
	storage internal.Storage
	logger  forge.Logger

	mu sync.RWMutex

	// Snapshot state
	snapshotIndex uint64
	snapshotTerm  uint64
}

// NewLog creates a log and loads any entries already held in storage.
//
// cacheSize is accepted for compatibility and ignored: the log keeps every
// entry after the last snapshot in memory, so a separate cache would only
// duplicate it.
func NewLog(logger forge.Logger, storage internal.Storage, cacheSize int) (*Log, error) {
	_ = cacheSize

	l := &Log{
		entries: make([]internal.LogEntry, 0),
		storage: storage,
		logger:  logger,
	}

	if err := l.load(); err != nil {
		return nil, fmt.Errorf("failed to load log: %w", err)
	}

	return l, nil
}

// Append appends a single entry. Its index must be LastIndex()+1. The entry is
// persisted first and only added to memory when the write succeeded, so a
// failed write never leaves an entry the node could acknowledge and then lose.
func (l *Log) Append(entry internal.LogEntry) error {
	return l.AppendBatch([]internal.LogEntry{entry})
}

// AppendBatch appends entries that continue the log without gaps.
func (l *Log) AppendBatch(entries []internal.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	next := l.lastIndexLocked() + 1
	for i, entry := range entries {
		if entry.Index != next+uint64(i) {
			return fmt.Errorf("log append out of order: entry %d has index %d, expected %d",
				i, entry.Index, next+uint64(i))
		}
	}

	if err := l.persistBatch(entries); err != nil {
		return fmt.Errorf("failed to persist log entries: %w", err)
	}

	l.entries = append(l.entries, entries...)

	return nil
}

// Get retrieves a log entry by index.
func (l *Log) Get(index uint64) (internal.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if index <= l.snapshotIndex {
		return internal.LogEntry{}, fmt.Errorf("log entry %d is in snapshot", index)
	}

	pos, ok := l.posLocked(index)
	if !ok {
		return internal.LogEntry{}, fmt.Errorf("log entry %d not found (last index %d)", index, l.lastIndexLocked())
	}

	return l.entries[pos], nil
}

// Term returns the term of the entry at index. The snapshot boundary reports
// the snapshot term and index 0 reports term 0. The second result is false
// when the log holds no entry at index.
func (l *Log) Term(index uint64) (uint64, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.termLocked(index)
}

func (l *Log) termLocked(index uint64) (uint64, bool) {
	if index == 0 {
		return 0, true
	}

	if index == l.snapshotIndex {
		return l.snapshotTerm, true
	}

	pos, ok := l.posLocked(index)
	if !ok {
		return 0, false
	}

	return l.entries[pos].Term, true
}

// GetRange retrieves the entries in [start, end].
func (l *Log) GetRange(start, end uint64) ([]internal.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if start > end {
		return nil, fmt.Errorf("invalid range: start %d > end %d", start, end)
	}

	if start <= l.snapshotIndex {
		return nil, fmt.Errorf("start index %d is in snapshot", start)
	}

	if end > l.lastIndexLocked() {
		return nil, fmt.Errorf("end index %d is past the last index %d", end, l.lastIndexLocked())
	}

	from, _ := l.posLocked(start)
	to, _ := l.posLocked(end)

	result := make([]internal.LogEntry, to-from+1)
	copy(result, l.entries[from:to+1])

	return result, nil
}

// LastIndex returns the index of the last log entry.
func (l *Log) LastIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.lastIndexLocked()
}

func (l *Log) lastIndexLocked() uint64 {
	return l.snapshotIndex + uint64(len(l.entries))
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

	return l.snapshotIndex + 1
}

// SnapshotIndex returns the last index covered by the snapshot.
func (l *Log) SnapshotIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.snapshotIndex
}

// SnapshotTerm returns the term of the last index covered by the snapshot.
func (l *Log) SnapshotTerm() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.snapshotTerm
}

// Count returns the number of log entries.
func (l *Log) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return len(l.entries)
}

// TruncateAfter removes every entry with an index greater than index, from
// storage as well as memory, so a reload never brings a deleted entry back.
func (l *Log) TruncateAfter(index uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index < l.snapshotIndex {
		return fmt.Errorf("cannot truncate before snapshot index %d", l.snapshotIndex)
	}

	last := l.lastIndexLocked()
	if index >= last {
		return nil
	}

	// Delete from the tail backwards so a backend that applies a batch
	// partially still leaves a contiguous prefix behind.
	ops := make([]internal.BatchOp, 0, last-index)
	for i := last; i > index; i-- {
		ops = append(ops, internal.BatchOp{Type: internal.BatchOpDelete, Key: logKey(i)})
	}

	if err := l.storage.Batch(ops); err != nil {
		return fmt.Errorf("failed to delete truncated log entries: %w", err)
	}

	l.entries = l.entries[:index-l.snapshotIndex]

	l.logger.Info("truncated log",
		forge.F("after_index", index),
		forge.F("entries_removed", last-index),
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

	pos, ok := l.posLocked(index)
	if !ok {
		return fmt.Errorf("log entry %d not found", index)
	}

	if l.entries[pos].Term != term {
		return fmt.Errorf("log entry %d has term %d, not snapshot term %d", index, l.entries[pos].Term, term)
	}

	oldSnapshot := l.snapshotIndex

	// Copy the tail so the compacted prefix can be garbage collected.
	remaining := make([]internal.LogEntry, len(l.entries)-pos-1)
	copy(remaining, l.entries[pos+1:])
	l.entries = remaining
	l.snapshotIndex = index
	l.snapshotTerm = term

	// The snapshot metadata is already durable, so stale log keys below it are
	// only wasted space and a failed delete is not fatal.
	ops := make([]internal.BatchOp, 0, index-oldSnapshot)
	for i := oldSnapshot + 1; i <= index; i++ {
		ops = append(ops, internal.BatchOp{Type: internal.BatchOpDelete, Key: logKey(i)})
	}

	if err := l.storage.Batch(ops); err != nil {
		l.logger.Warn("failed to delete compacted log entries from storage",
			forge.F("up_to_index", index),
			forge.F("error", err),
		)
	}

	l.logger.Info("compacted log",
		forge.F("up_to_index", index),
		forge.F("snapshot_term", term),
		forge.F("entries_remaining", len(l.entries)),
	)

	return nil
}

// InstallSnapshot moves the start of the log past a snapshot received from
// the leader that covers everything up to index. Entries after index are
// kept when the log holds index with the same term; otherwise the whole log is
// discarded, as section 7 of the Raft paper requires. Removed entries are
// deleted from storage first, so memory never claims less than storage.
func (l *Log) InstallSnapshot(index, term uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if index <= l.snapshotIndex {
		return nil
	}

	last := l.lastIndexLocked()
	deleteTo := last

	var keep []internal.LogEntry

	if t, ok := l.termLocked(index); ok && t == term {
		pos, _ := l.posLocked(index)
		keep = make([]internal.LogEntry, len(l.entries)-pos-1)
		copy(keep, l.entries[pos+1:])

		deleteTo = index
	}

	if deleteTo > l.snapshotIndex {
		ops := make([]internal.BatchOp, 0, deleteTo-l.snapshotIndex)
		for i := deleteTo; i > l.snapshotIndex; i-- {
			ops = append(ops, internal.BatchOp{Type: internal.BatchOpDelete, Key: logKey(i)})
		}

		if err := l.storage.Batch(ops); err != nil {
			return fmt.Errorf("failed to delete log entries covered by snapshot: %w", err)
		}
	}

	if keep == nil {
		keep = make([]internal.LogEntry, 0)
	}

	l.entries = keep
	l.snapshotIndex = index
	l.snapshotTerm = term

	l.logger.Info("log moved past installed snapshot",
		forge.F("snapshot_index", index),
		forge.F("snapshot_term", term),
		forge.F("entries_kept", len(keep)),
	)

	return nil
}

// load reads the snapshot metadata and every contiguous entry after it.
func (l *Log) load() error {
	snapshotMeta, err := l.storage.Get([]byte(snapshotMetaKey))

	switch {
	case err == nil && len(snapshotMeta) > 0:
		var meta struct {
			Index uint64 `json:"index"`
			Term  uint64 `json:"term"`
		}

		if err := json.Unmarshal(snapshotMeta, &meta); err != nil {
			return fmt.Errorf("failed to decode snapshot metadata: %w", err)
		}

		l.snapshotIndex = meta.Index
		l.snapshotTerm = meta.Term
	case err != nil && !isNotFound(err):
		return fmt.Errorf("failed to read snapshot metadata: %w", err)
	}

	// Entries are stored under decimal keys, which do not sort numerically,
	// so they are read by index until the first gap.
	for index := l.snapshotIndex + 1; ; index++ {
		data, err := l.storage.Get(logKey(index))
		if err != nil {
			if isNotFound(err) {
				break
			}

			return fmt.Errorf("failed to read log entry %d: %w", index, err)
		}

		var entry internal.LogEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return fmt.Errorf("failed to decode log entry %d: %w", index, err)
		}

		if entry.Index != index {
			return fmt.Errorf("log entry stored under index %d claims index %d", index, entry.Index)
		}

		l.entries = append(l.entries, entry)
	}

	l.logger.Info("loaded log",
		forge.F("snapshot_index", l.snapshotIndex),
		forge.F("snapshot_term", l.snapshotTerm),
		forge.F("entries", len(l.entries)),
	)

	return nil
}

// persistBatch writes entries to storage in a single batch.
func (l *Log) persistBatch(entries []internal.LogEntry) error {
	ops := make([]internal.BatchOp, len(entries))

	for i, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal entry %d: %w", entry.Index, err)
		}

		ops[i] = internal.BatchOp{
			Type:  internal.BatchOpSet,
			Key:   logKey(entry.Index),
			Value: data,
		}
	}

	if len(ops) == 1 {
		return l.storage.Set(ops[0].Key, ops[0].Value)
	}

	return l.storage.Batch(ops)
}

// posLocked maps a log index to its slice position.
func (l *Log) posLocked(index uint64) (int, bool) {
	if index <= l.snapshotIndex || index > l.lastIndexLocked() {
		return 0, false
	}

	return int(index - l.snapshotIndex - 1), true //nolint:gosec // G115: bounded by len(l.entries)
}

// MatchesTerm checks if the log entry at index has the given term.
func (l *Log) MatchesTerm(index, term uint64) bool {
	t, ok := l.Term(index)

	return ok && t == term
}

// GetEntriesAfter returns up to maxEntries entries after the given index.
func (l *Log) GetEntriesAfter(index uint64, maxEntries int) ([]internal.LogEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if index < l.snapshotIndex {
		return nil, fmt.Errorf("index %d is before snapshot index %d", index, l.snapshotIndex)
	}

	if index >= l.lastIndexLocked() || maxEntries <= 0 {
		return []internal.LogEntry{}, nil
	}

	from := int(index - l.snapshotIndex) //nolint:gosec // G115: bounded by len(l.entries)
	to := min(from+maxEntries, len(l.entries))

	result := make([]internal.LogEntry, to-from)
	copy(result, l.entries[from:to])

	return result, nil
}

// String returns a string representation of the log.
func (l *Log) String() string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Log[snapshot=%d:%d, entries=%d, first=%d, last=%d]",
		l.snapshotIndex, l.snapshotTerm, len(l.entries),
		l.snapshotIndex+1, l.lastIndexLocked())

	return buf.String()
}

func logKey(index uint64) []byte {
	return fmt.Appendf(nil, "log_%d", index)
}

// isNotFound reports whether a storage error means the key does not exist.
// Every bundled storage backend reports a missing key as ErrNodeNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, internal.ErrNodeNotFound)
}
