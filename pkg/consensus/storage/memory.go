package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// MemoryStorage implements Storage interface using in-memory storage
type MemoryStorage struct {
	logger    common.Logger
	metrics   common.Metrics
	mu        sync.RWMutex
	stats     StorageStats
	startTime time.Time

	// Storage
	entries  map[uint64]*LogEntry
	snapshot *Snapshot
	metadata map[string]string

	// Configuration
	maxEntries  int64
	maxSize     int64
	currentSize int64
}

// MemoryStorageConfig contains configuration for memory storage
type MemoryStorageConfig struct {
	MaxEntries int64 `json:"max_entries"`
	MaxSize    int64 `json:"max_size"`
}

// NewMemoryStorage creates a new in-memory storage
func NewMemoryStorage(config MemoryStorageConfig, l common.Logger, metrics common.Metrics) Storage {
	if config.MaxEntries <= 0 {
		config.MaxEntries = 100000 // Default to 100K entries
	}
	if config.MaxSize <= 0 {
		config.MaxSize = 1024 * 1024 * 1024 // Default to 1GB
	}

	ms := &MemoryStorage{
		logger:     l,
		metrics:    metrics,
		entries:    make(map[uint64]*LogEntry),
		metadata:   make(map[string]string),
		maxEntries: config.MaxEntries,
		maxSize:    config.MaxSize,
		startTime:  time.Now(),
		stats: StorageStats{
			ReadCount:  0,
			WriteCount: 0,
			ErrorCount: 0,
		},
	}

	if l != nil {
		l.Info("memory storage initialized",
			logger.Int64("max_entries", config.MaxEntries),
			logger.Int64("max_size", config.MaxSize),
		)
	}

	return ms
}

// StoreEntry stores a log entry
func (ms *MemoryStorage) StoreEntry(ctx context.Context, entry LogEntry) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	start := time.Now()
	defer func() {
		ms.stats.WriteLatency = time.Since(start)
		ms.stats.WriteCount++
	}()

	// Validate entry
	if err := ValidateLogEntry(entry); err != nil {
		ms.stats.ErrorCount++
		return err
	}

	// Check size limits
	entrySize := LogEntrySize(entry)
	if ms.currentSize+int64(entrySize) > ms.maxSize {
		ms.stats.ErrorCount++
		return NewStorageError(ErrCodeStorageFull, "storage size limit exceeded")
	}

	// Check entry count limits
	if int64(len(ms.entries)) >= ms.maxEntries {
		ms.stats.ErrorCount++
		return NewStorageError(ErrCodeStorageFull, "entry count limit exceeded")
	}

	// Calculate checksum if not provided
	if entry.Checksum == 0 {
		entry.Checksum = CalculateChecksum(entry.Data)
	}

	// Store entry (create a copy to avoid mutations)
	entryCopy := CloneLogEntry(entry)
	ms.entries[entry.Index] = &entryCopy

	// Update stats
	ms.currentSize += int64(entrySize)
	ms.stats.EntryCount++
	ms.stats.StorageSize = ms.currentSize

	if ms.stats.FirstIndex == 0 || entry.Index < ms.stats.FirstIndex {
		ms.stats.FirstIndex = entry.Index
	}
	if entry.Index > ms.stats.LastIndex {
		ms.stats.LastIndex = entry.Index
	}

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.entries_written").Inc()
		ms.metrics.Histogram("forge.consensus.storage.write_latency").Observe(ms.stats.WriteLatency.Seconds())
		ms.metrics.Gauge("forge.consensus.storage.memory_usage").Set(float64(ms.currentSize))
	}

	return nil
}

// GetEntry retrieves a log entry by index
func (ms *MemoryStorage) GetEntry(ctx context.Context, index uint64) (*LogEntry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	start := time.Now()
	defer func() {
		ms.stats.ReadLatency = time.Since(start)
		ms.stats.ReadCount++
	}()

	entry, exists := ms.entries[index]
	if !exists {
		return nil, NewStorageError(ErrCodeEntryNotFound, fmt.Sprintf("entry with index %d not found", index))
	}

	// Return a copy to avoid mutations
	entryCopy := CloneLogEntry(*entry)

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.entries_read").Inc()
		ms.metrics.Histogram("forge.consensus.storage.read_latency").Observe(ms.stats.ReadLatency.Seconds())
	}

	return &entryCopy, nil
}

// GetEntries retrieves log entries in a range
func (ms *MemoryStorage) GetEntries(ctx context.Context, start, end uint64) ([]LogEntry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if start > end {
		return nil, NewStorageError(ErrCodeInvalidIndex, "start index must be <= end index")
	}

	entries := make([]LogEntry, 0, end-start+1)
	for i := start; i <= end; i++ {
		if entry, exists := ms.entries[i]; exists {
			entryCopy := CloneLogEntry(*entry)
			entries = append(entries, entryCopy)
		}
	}

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.entries_read").Add(float64(len(entries)))
	}

	return entries, nil
}

// GetLastEntry retrieves the last log entry
func (ms *MemoryStorage) GetLastEntry(ctx context.Context) (*LogEntry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.stats.LastIndex == 0 {
		return nil, NewStorageError(ErrCodeEntryNotFound, "no entries found")
	}

	return ms.GetEntry(ctx, ms.stats.LastIndex)
}

// GetFirstEntry retrieves the first log entry
func (ms *MemoryStorage) GetFirstEntry(ctx context.Context) (*LogEntry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.stats.FirstIndex == 0 {
		return nil, NewStorageError(ErrCodeEntryNotFound, "no entries found")
	}

	return ms.GetEntry(ctx, ms.stats.FirstIndex)
}

// DeleteEntry deletes a log entry
func (ms *MemoryStorage) DeleteEntry(ctx context.Context, index uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	entry, exists := ms.entries[index]
	if !exists {
		return NewStorageError(ErrCodeEntryNotFound, fmt.Sprintf("entry with index %d not found", index))
	}

	// Update size
	entrySize := LogEntrySize(*entry)
	ms.currentSize -= int64(entrySize)
	ms.stats.StorageSize = ms.currentSize

	// Delete entry
	delete(ms.entries, index)
	ms.stats.EntryCount--

	// Update first/last index if needed
	if index == ms.stats.FirstIndex {
		ms.updateFirstIndex()
	}
	if index == ms.stats.LastIndex {
		ms.updateLastIndex()
	}

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.entries_deleted").Inc()
		ms.metrics.Gauge("forge.consensus.storage.memory_usage").Set(float64(ms.currentSize))
	}

	return nil
}

// DeleteEntriesFrom deletes entries from a given index
func (ms *MemoryStorage) DeleteEntriesFrom(ctx context.Context, index uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var deletedCount int
	var deletedSize int64

	// Delete all entries from index onwards
	for i := index; i <= ms.stats.LastIndex; i++ {
		if entry, exists := ms.entries[i]; exists {
			entrySize := LogEntrySize(*entry)
			deletedSize += int64(entrySize)
			delete(ms.entries, i)
			deletedCount++
		}
	}

	// Update stats
	ms.currentSize -= deletedSize
	ms.stats.StorageSize = ms.currentSize
	ms.stats.EntryCount -= int64(deletedCount)

	if index <= ms.stats.LastIndex {
		ms.stats.LastIndex = index - 1
	}

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.entries_deleted").Add(float64(deletedCount))
		ms.metrics.Gauge("forge.consensus.storage.memory_usage").Set(float64(ms.currentSize))
	}

	return nil
}

// DeleteEntriesTo deletes entries up to a given index
func (ms *MemoryStorage) DeleteEntriesTo(ctx context.Context, index uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var deletedCount int
	var deletedSize int64

	// Delete all entries up to index
	for i := ms.stats.FirstIndex; i <= index; i++ {
		if entry, exists := ms.entries[i]; exists {
			entrySize := LogEntrySize(*entry)
			deletedSize += int64(entrySize)
			delete(ms.entries, i)
			deletedCount++
		}
	}

	// Update stats
	ms.currentSize -= deletedSize
	ms.stats.StorageSize = ms.currentSize
	ms.stats.EntryCount -= int64(deletedCount)

	if index >= ms.stats.FirstIndex {
		ms.stats.FirstIndex = index + 1
	}

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.entries_deleted").Add(float64(deletedCount))
		ms.metrics.Gauge("forge.consensus.storage.memory_usage").Set(float64(ms.currentSize))
	}

	return nil
}

// updateFirstIndex updates the first index after deletions
func (ms *MemoryStorage) updateFirstIndex() {
	if len(ms.entries) == 0 {
		ms.stats.FirstIndex = 0
		return
	}

	var indices []uint64
	for index := range ms.entries {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	ms.stats.FirstIndex = indices[0]
}

// updateLastIndex updates the last index after deletions
func (ms *MemoryStorage) updateLastIndex() {
	if len(ms.entries) == 0 {
		ms.stats.LastIndex = 0
		return
	}

	var indices []uint64
	for index := range ms.entries {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] > indices[j] })
	ms.stats.LastIndex = indices[0]
}

// GetLastIndex returns the index of the last log entry
func (ms *MemoryStorage) GetLastIndex(ctx context.Context) (uint64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return ms.stats.LastIndex, nil
}

// GetFirstIndex returns the index of the first log entry
func (ms *MemoryStorage) GetFirstIndex(ctx context.Context) (uint64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return ms.stats.FirstIndex, nil
}

// StoreSnapshot stores a snapshot
func (ms *MemoryStorage) StoreSnapshot(ctx context.Context, snapshot Snapshot) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Validate snapshot
	if err := ValidateSnapshot(snapshot); err != nil {
		return err
	}

	// Calculate checksum if not provided
	if snapshot.Checksum == 0 {
		snapshot.Checksum = CalculateChecksum(snapshot.Data)
	}

	// Store snapshot (create a copy to avoid mutations)
	snapshotCopy := CloneSnapshot(snapshot)
	ms.snapshot = &snapshotCopy

	ms.stats.SnapshotSize = int64(SnapshotSize(snapshot))

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.snapshots_stored").Inc()
		ms.metrics.Gauge("forge.consensus.storage.snapshot_size").Set(float64(ms.stats.SnapshotSize))
	}

	return nil
}

// GetSnapshot retrieves a snapshot
func (ms *MemoryStorage) GetSnapshot(ctx context.Context) (*Snapshot, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.snapshot == nil {
		return nil, NewStorageError(ErrCodeSnapshotNotFound, "snapshot not found")
	}

	// Return a copy to avoid mutations
	snapshotCopy := CloneSnapshot(*ms.snapshot)
	return &snapshotCopy, nil
}

// DeleteSnapshot deletes a snapshot
func (ms *MemoryStorage) DeleteSnapshot(ctx context.Context) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.snapshot = nil
	ms.stats.SnapshotSize = 0

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.snapshots_deleted").Inc()
		ms.metrics.Gauge("forge.consensus.storage.snapshot_size").Set(0)
	}

	return nil
}

// StoreTerm stores the current term
func (ms *MemoryStorage) StoreTerm(ctx context.Context, term uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.metadata["current_term"] = fmt.Sprintf("%d", term)

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.terms_stored").Inc()
	}

	return nil
}

// GetTerm retrieves the current term
func (ms *MemoryStorage) GetTerm(ctx context.Context) (uint64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	termStr, exists := ms.metadata["current_term"]
	if !exists {
		return 0, nil // Default term is 0
	}

	var term uint64
	if _, err := fmt.Sscanf(termStr, "%d", &term); err != nil {
		return 0, NewStorageError(ErrCodeCorruptedData, fmt.Sprintf("failed to parse term: %v", err))
	}

	return term, nil
}

// StoreVote stores the vote for a term
func (ms *MemoryStorage) StoreVote(ctx context.Context, term uint64, candidateID string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	voteKey := fmt.Sprintf("vote_term_%d", term)
	ms.metadata[voteKey] = candidateID

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.votes_stored").Inc()
	}

	return nil
}

// GetVote retrieves the vote for a term
func (ms *MemoryStorage) GetVote(ctx context.Context, term uint64) (string, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	voteKey := fmt.Sprintf("vote_term_%d", term)
	candidateID, exists := ms.metadata[voteKey]
	if !exists {
		return "", nil // No vote cast
	}

	return candidateID, nil
}

// Sync ensures all data is persisted
func (ms *MemoryStorage) Sync(ctx context.Context) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	start := time.Now()
	defer func() {
		ms.stats.SyncLatency = time.Since(start)
		ms.stats.SyncCount++
	}()

	// For memory storage, sync is a no-op since everything is already in memory

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.syncs").Inc()
		ms.metrics.Histogram("forge.consensus.storage.sync_latency").Observe(ms.stats.SyncLatency.Seconds())
	}

	return nil
}

// Close closes the storage
func (ms *MemoryStorage) Close(ctx context.Context) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Clear all data
	ms.entries = nil
	ms.snapshot = nil
	ms.metadata = nil

	if ms.logger != nil {
		ms.logger.Info("memory storage closed")
	}

	return nil
}

// GetStats returns storage statistics
func (ms *MemoryStorage) GetStats() StorageStats {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	stats := ms.stats
	stats.Uptime = time.Since(ms.startTime)

	return stats
}

// Compact compacts the storage
func (ms *MemoryStorage) Compact(ctx context.Context, index uint64) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	start := time.Now()
	defer func() {
		ms.stats.CompactionCount++
		ms.stats.LastCompaction = time.Now()
	}()

	// For memory storage, compaction means deleting entries up to the given index
	var deletedCount int
	var deletedSize int64

	for i := ms.stats.FirstIndex; i <= index; i++ {
		if entry, exists := ms.entries[i]; exists {
			entrySize := LogEntrySize(*entry)
			deletedSize += int64(entrySize)
			delete(ms.entries, i)
			deletedCount++
		}
	}

	// Update stats
	ms.currentSize -= deletedSize
	ms.stats.StorageSize = ms.currentSize
	ms.stats.EntryCount -= int64(deletedCount)

	if index >= ms.stats.FirstIndex {
		ms.stats.FirstIndex = index + 1
	}

	if ms.metrics != nil {
		ms.metrics.Counter("forge.consensus.storage.compactions").Inc()
		ms.metrics.Counter("forge.consensus.storage.entries_deleted").Add(float64(deletedCount))
		ms.metrics.Histogram("forge.consensus.storage.compaction_duration").Observe(time.Since(start).Seconds())
		ms.metrics.Gauge("forge.consensus.storage.memory_usage").Set(float64(ms.currentSize))
	}

	return nil
}

// Clear clears all data (useful for testing)
func (ms *MemoryStorage) Clear() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.entries = make(map[uint64]*LogEntry)
	ms.snapshot = nil
	ms.metadata = make(map[string]string)
	ms.currentSize = 0
	ms.stats = StorageStats{
		ReadCount:  0,
		WriteCount: 0,
		ErrorCount: 0,
	}
}

// GetEntryCount returns the number of entries
func (ms *MemoryStorage) GetEntryCount() int64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return int64(len(ms.entries))
}

// GetMemoryUsage returns the current memory usage
func (ms *MemoryStorage) GetMemoryUsage() int64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return ms.currentSize
}

// GetMaxSize returns the maximum allowed size
func (ms *MemoryStorage) GetMaxSize() int64 {
	return ms.maxSize
}

// GetMaxEntries returns the maximum allowed entries
func (ms *MemoryStorage) GetMaxEntries() int64 {
	return ms.maxEntries
}

// HasEntry checks if an entry exists
func (ms *MemoryStorage) HasEntry(index uint64) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	_, exists := ms.entries[index]
	return exists
}

// GetAllIndices returns all entry indices
func (ms *MemoryStorage) GetAllIndices() []uint64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	indices := make([]uint64, 0, len(ms.entries))
	for index := range ms.entries {
		indices = append(indices, index)
	}

	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}

// GetMetadata returns all metadata
func (ms *MemoryStorage) GetMetadata() map[string]string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	metadata := make(map[string]string)
	for k, v := range ms.metadata {
		metadata[k] = v
	}

	return metadata
}

func (ms *MemoryStorage) StoreState(ctx context.Context, state *PersistentState) error {
	// TODO implement me
	panic("implement me")
}

func (ms *MemoryStorage) LoadState(ctx context.Context) (*PersistentState, error) {
	// TODO implement me
	panic("implement me")
}

func (ms *MemoryStorage) HealthCheck(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

// MemoryStorageFactory creates memory storage instances
type MemoryStorageFactory struct{}

// Create creates a new memory storage instance
func (f *MemoryStorageFactory) Create(config StorageConfig, logger common.Logger, metrics common.Metrics) (Storage, error) {
	memoryConfig := MemoryStorageConfig{
		MaxEntries: config.MaxEntries,
		MaxSize:    config.MaxSize,
	}

	if memoryConfig.MaxEntries <= 0 {
		memoryConfig.MaxEntries = 100000
	}
	if memoryConfig.MaxSize <= 0 {
		memoryConfig.MaxSize = 1024 * 1024 * 1024 // 1GB
	}

	return NewMemoryStorage(memoryConfig, logger, metrics), nil
}

// Name returns the factory name
func (f *MemoryStorageFactory) Name() string {
	return "memory"
}

// Version returns the factory version
func (f *MemoryStorageFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *MemoryStorageFactory) ValidateConfig(config StorageConfig) error {
	if config.MaxEntries < 0 {
		return NewStorageError(ErrCodeInvalidConfig, "max_entries must be >= 0")
	}

	if config.MaxSize < 0 {
		return NewStorageError(ErrCodeInvalidConfig, "max_size must be >= 0")
	}

	return nil
}
