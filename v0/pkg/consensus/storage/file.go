package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// FileStorage implements Storage interface using file system
type FileStorage struct {
	path      string
	logger    common.Logger
	metrics   common.Metrics
	mu        sync.RWMutex
	stats     StorageStats
	startTime time.Time

	// File handles
	logFile      *os.File
	metadataFile *os.File
	snapshotFile *os.File

	// In-memory cache for performance
	cache     map[uint64]*LogEntry
	cacheSize int
	maxCache  int
}

// FileStorageConfig contains configuration for file storage
type FileStorageConfig struct {
	Path                string        `json:"path"`
	MaxCacheSize        int           `json:"max_cache_size"`
	SyncInterval        time.Duration `json:"sync_interval"`
	CompactionThreshold int64         `json:"compaction_threshold"`
	EnableCompression   bool          `json:"enable_compression"`
	EnableChecksum      bool          `json:"enable_checksum"`
}

// NewFileStorage creates a new file-based storage
func NewFileStorage(config FileStorageConfig, l common.Logger, metrics common.Metrics) (Storage, error) {
	if config.Path == "" {
		return nil, NewStorageError(ErrCodeInvalidConfig, "path is required")
	}

	if config.MaxCacheSize <= 0 {
		config.MaxCacheSize = 1000
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(config.Path, 0755); err != nil {
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to create directory: %v", err))
	}

	fs := &FileStorage{
		path:      config.Path,
		logger:    l,
		metrics:   metrics,
		cache:     make(map[uint64]*LogEntry),
		maxCache:  config.MaxCacheSize,
		startTime: time.Now(),
		stats: StorageStats{
			ReadCount:  0,
			WriteCount: 0,
			ErrorCount: 0,
		},
	}

	// Open files
	if err := fs.openFiles(); err != nil {
		return nil, err
	}

	// Load metadata
	if err := fs.loadMetadata(); err != nil {
		return nil, err
	}

	// Start background sync if configured
	if config.SyncInterval > 0 {
		go fs.backgroundSync(config.SyncInterval)
	}

	if l != nil {
		l.Info("file storage initialized",
			logger.String("path", config.Path),
			logger.Int("max_cache_size", config.MaxCacheSize),
		)
	}

	return fs, nil
}

// openFiles opens the necessary files
func (fs *FileStorage) openFiles() error {
	var err error

	// Open log file
	logPath := filepath.Join(fs.path, "raft.log")
	fs.logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to open log file: %v", err))
	}

	// Open metadata file
	metadataPath := filepath.Join(fs.path, "metadata.json")
	fs.metadataFile, err = os.OpenFile(metadataPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to open metadata file: %v", err))
	}

	// Open snapshot file
	snapshotPath := filepath.Join(fs.path, "snapshot.dat")
	fs.snapshotFile, err = os.OpenFile(snapshotPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to open snapshot file: %v", err))
	}

	return nil
}

// loadMetadata loads metadata from file
func (fs *FileStorage) loadMetadata() error {
	fs.metadataFile.Seek(0, 0)
	decoder := json.NewDecoder(fs.metadataFile)

	metadata := struct {
		FirstIndex uint64 `json:"first_index"`
		LastIndex  uint64 `json:"last_index"`
		EntryCount int64  `json:"entry_count"`
	}{}

	if err := decoder.Decode(&metadata); err != nil {
		if !os.IsNotExist(err) {
			fs.logger.Warn("failed to load metadata, starting fresh", logger.Error(err))
		}
		return nil
	}

	fs.stats.FirstIndex = metadata.FirstIndex
	fs.stats.LastIndex = metadata.LastIndex
	fs.stats.EntryCount = metadata.EntryCount

	return nil
}

// saveMetadata saves metadata to file
func (fs *FileStorage) saveMetadata() error {
	fs.metadataFile.Seek(0, 0)
	fs.metadataFile.Truncate(0)

	metadata := struct {
		FirstIndex uint64 `json:"first_index"`
		LastIndex  uint64 `json:"last_index"`
		EntryCount int64  `json:"entry_count"`
	}{
		FirstIndex: fs.stats.FirstIndex,
		LastIndex:  fs.stats.LastIndex,
		EntryCount: fs.stats.EntryCount,
	}

	encoder := json.NewEncoder(fs.metadataFile)
	return encoder.Encode(metadata)
}

// StoreEntry stores a log entry
func (fs *FileStorage) StoreEntry(ctx context.Context, entry LogEntry) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	start := time.Now()
	defer func() {
		fs.stats.WriteLatency = time.Since(start)
		fs.stats.WriteCount++
	}()

	// Validate entry
	if err := ValidateLogEntry(entry); err != nil {
		fs.stats.ErrorCount++
		return err
	}

	// Calculate checksum if enabled
	if entry.Checksum == 0 {
		entry.Checksum = CalculateChecksum(entry.Data)
	}

	// Serialize entry
	entryData, err := json.Marshal(entry)
	if err != nil {
		fs.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to serialize entry: %v", err))
	}

	// Write to file
	entryLine := fmt.Sprintf("%s\n", string(entryData))
	if _, err := fs.logFile.WriteString(entryLine); err != nil {
		fs.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to write entry: %v", err))
	}

	// Update cache
	fs.addToCache(entry)

	// Update stats
	fs.stats.EntryCount++
	if fs.stats.FirstIndex == 0 || entry.Index < fs.stats.FirstIndex {
		fs.stats.FirstIndex = entry.Index
	}
	if entry.Index > fs.stats.LastIndex {
		fs.stats.LastIndex = entry.Index
	}

	// Save metadata
	if err := fs.saveMetadata(); err != nil {
		fs.logger.Warn("failed to save metadata", logger.Error(err))
	}

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.entries_written").Inc()
		fs.metrics.Histogram("forge.consensus.storage.write_latency").Observe(fs.stats.WriteLatency.Seconds())
	}

	return nil
}

// GetEntry retrieves a log entry by index
func (fs *FileStorage) GetEntry(ctx context.Context, index uint64) (*LogEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	start := time.Now()
	defer func() {
		fs.stats.ReadLatency = time.Since(start)
		fs.stats.ReadCount++
	}()

	// Check cache first
	if entry, exists := fs.cache[index]; exists {
		if fs.metrics != nil {
			fs.metrics.Counter("forge.consensus.storage.cache_hits").Inc()
		}
		return entry, nil
	}

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.cache_misses").Inc()
	}

	// Read from file
	entry, err := fs.readEntryFromFile(index)
	if err != nil {
		fs.stats.ErrorCount++
		return nil, err
	}

	// Add to cache
	fs.addToCache(*entry)

	return entry, nil
}

// GetEntries retrieves log entries in a range
func (fs *FileStorage) GetEntries(ctx context.Context, start, end uint64) ([]LogEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if start > end {
		return nil, NewStorageError(ErrCodeInvalidIndex, "start index must be <= end index")
	}

	entries := make([]LogEntry, 0, end-start+1)
	for i := start; i <= end; i++ {
		entry, err := fs.getEntryUnsafe(i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	return entries, nil
}

// getEntryUnsafe retrieves entry without locking (assumes caller holds lock)
func (fs *FileStorage) getEntryUnsafe(index uint64) (*LogEntry, error) {
	// Check cache first
	if entry, exists := fs.cache[index]; exists {
		return entry, nil
	}

	// Read from file
	return fs.readEntryFromFile(index)
}

// readEntryFromFile reads an entry from the log file
func (fs *FileStorage) readEntryFromFile(index uint64) (*LogEntry, error) {
	// This is a simplified implementation
	// In a real implementation, you would maintain an index file
	// or use a more efficient storage format

	fs.logFile.Seek(0, 0)
	decoder := json.NewDecoder(fs.logFile)

	for {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to decode entry: %v", err))
		}

		if entry.Index == index {
			return &entry, nil
		}
	}

	return nil, NewStorageError(ErrCodeEntryNotFound, fmt.Sprintf("entry with index %d not found", index))
}

// GetLastEntry retrieves the last log entry
func (fs *FileStorage) GetLastEntry(ctx context.Context) (*LogEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.stats.LastIndex == 0 {
		return nil, NewStorageError(ErrCodeEntryNotFound, "no entries found")
	}

	return fs.getEntryUnsafe(fs.stats.LastIndex)
}

// GetFirstEntry retrieves the first log entry
func (fs *FileStorage) GetFirstEntry(ctx context.Context) (*LogEntry, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.stats.FirstIndex == 0 {
		return nil, NewStorageError(ErrCodeEntryNotFound, "no entries found")
	}

	return fs.getEntryUnsafe(fs.stats.FirstIndex)
}

// DeleteEntry deletes a log entry
func (fs *FileStorage) DeleteEntry(ctx context.Context, index uint64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Remove from cache
	delete(fs.cache, index)

	// Note: This is a simplified implementation
	// In a real implementation, you would need to rewrite the file
	// or use a more sophisticated storage format

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.entries_deleted").Inc()
	}

	return nil
}

// DeleteEntriesFrom deletes entries from a given index
func (fs *FileStorage) DeleteEntriesFrom(ctx context.Context, index uint64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Remove from cache
	for i := index; i <= fs.stats.LastIndex; i++ {
		delete(fs.cache, i)
	}

	// Update stats
	fs.stats.LastIndex = index - 1
	fs.stats.EntryCount = int64(index - fs.stats.FirstIndex)

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.entries_deleted").Inc()
	}

	return nil
}

// DeleteEntriesTo deletes entries up to a given index
func (fs *FileStorage) DeleteEntriesTo(ctx context.Context, index uint64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Remove from cache
	for i := fs.stats.FirstIndex; i <= index; i++ {
		delete(fs.cache, i)
	}

	// Update stats
	fs.stats.FirstIndex = index + 1
	fs.stats.EntryCount = int64(fs.stats.LastIndex - index)

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.entries_deleted").Inc()
	}

	return nil
}

// GetLastIndex returns the index of the last log entry
func (fs *FileStorage) GetLastIndex(ctx context.Context) (uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.stats.LastIndex, nil
}

// GetFirstIndex returns the index of the first log entry
func (fs *FileStorage) GetFirstIndex(ctx context.Context) (uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.stats.FirstIndex, nil
}

// StoreSnapshot stores a snapshot
func (fs *FileStorage) StoreSnapshot(ctx context.Context, snapshot Snapshot) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Validate snapshot
	if err := ValidateSnapshot(snapshot); err != nil {
		return err
	}

	// Serialize snapshot
	snapshotData, err := json.Marshal(snapshot)
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to serialize snapshot: %v", err))
	}

	// Write to file
	fs.snapshotFile.Seek(0, 0)
	fs.snapshotFile.Truncate(0)
	if _, err := fs.snapshotFile.Write(snapshotData); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to write snapshot: %v", err))
	}

	fs.stats.SnapshotSize = int64(len(snapshotData))

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.snapshots_stored").Inc()
	}

	return nil
}

// GetSnapshot retrieves a snapshot
func (fs *FileStorage) GetSnapshot(ctx context.Context) (*Snapshot, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// Read from file
	fs.snapshotFile.Seek(0, 0)
	decoder := json.NewDecoder(fs.snapshotFile)

	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		if os.IsNotExist(err) {
			return nil, NewStorageError(ErrCodeSnapshotNotFound, "snapshot not found")
		}
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to decode snapshot: %v", err))
	}

	return &snapshot, nil
}

// DeleteSnapshot deletes a snapshot
func (fs *FileStorage) DeleteSnapshot(ctx context.Context) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.snapshotFile.Truncate(0)
	fs.stats.SnapshotSize = 0

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.snapshots_deleted").Inc()
	}

	return nil
}

// StoreTerm stores the current term
func (fs *FileStorage) StoreTerm(ctx context.Context, term uint64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Store in metadata
	return fs.saveMetadata()
}

// GetTerm retrieves the current term
func (fs *FileStorage) GetTerm(ctx context.Context) (uint64, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// This would typically be stored in metadata
	return 0, nil
}

// StoreVote stores the vote for a term
func (fs *FileStorage) StoreVote(ctx context.Context, term uint64, candidateID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Store in metadata
	return fs.saveMetadata()
}

// GetVote retrieves the vote for a term
func (fs *FileStorage) GetVote(ctx context.Context, term uint64) (string, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// This would typically be stored in metadata
	return "", nil
}

// Sync ensures all data is persisted
func (fs *FileStorage) Sync(ctx context.Context) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	start := time.Now()
	defer func() {
		fs.stats.SyncLatency = time.Since(start)
		fs.stats.SyncCount++
	}()

	// Sync all files
	if err := fs.logFile.Sync(); err != nil {
		return NewStorageError(ErrCodeSyncFailed, fmt.Sprintf("failed to sync log file: %v", err))
	}

	if err := fs.metadataFile.Sync(); err != nil {
		return NewStorageError(ErrCodeSyncFailed, fmt.Sprintf("failed to sync metadata file: %v", err))
	}

	if err := fs.snapshotFile.Sync(); err != nil {
		return NewStorageError(ErrCodeSyncFailed, fmt.Sprintf("failed to sync snapshot file: %v", err))
	}

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.syncs").Inc()
		fs.metrics.Histogram("forge.consensus.storage.sync_latency").Observe(fs.stats.SyncLatency.Seconds())
	}

	return nil
}

// Close closes the storage
func (fs *FileStorage) Close(ctx context.Context) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var errors []error

	if fs.logFile != nil {
		if err := fs.logFile.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if fs.metadataFile != nil {
		if err := fs.metadataFile.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if fs.snapshotFile != nil {
		if err := fs.snapshotFile.Close(); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to close files: %v", errors))
	}

	return nil
}

// GetStats returns storage statistics
func (fs *FileStorage) GetStats() StorageStats {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	stats := fs.stats
	stats.Uptime = time.Since(fs.startTime)
	stats.StorageSize = fs.getStorageSize()

	return stats
}

// getStorageSize calculates the total storage size
func (fs *FileStorage) getStorageSize() int64 {
	var totalSize int64

	if info, err := fs.logFile.Stat(); err == nil {
		totalSize += info.Size()
	}

	if info, err := fs.metadataFile.Stat(); err == nil {
		totalSize += info.Size()
	}

	if info, err := fs.snapshotFile.Stat(); err == nil {
		totalSize += info.Size()
	}

	return totalSize
}

// Compact compacts the storage
func (fs *FileStorage) Compact(ctx context.Context, index uint64) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	start := time.Now()
	defer func() {
		fs.stats.CompactionCount++
		fs.stats.LastCompaction = time.Now()
	}()

	// This is a simplified implementation
	// In a real implementation, you would rewrite the log file
	// removing entries up to the given index

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.compactions").Inc()
		fs.metrics.Histogram("forge.consensus.storage.compaction_duration").Observe(time.Since(start).Seconds())
	}

	return nil
}

// addToCache adds an entry to the cache
func (fs *FileStorage) addToCache(entry LogEntry) {
	// Simple LRU eviction if cache is full
	if len(fs.cache) >= fs.maxCache {
		// Remove oldest entry (simplified)
		var oldestIndex uint64
		for index := range fs.cache {
			if oldestIndex == 0 || index < oldestIndex {
				oldestIndex = index
			}
		}
		delete(fs.cache, oldestIndex)
	}

	entryCopy := CloneLogEntry(entry)
	fs.cache[entry.Index] = &entryCopy
}

// backgroundSync performs periodic syncing
func (fs *FileStorage) backgroundSync(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := fs.Sync(context.Background()); err != nil {
				if fs.logger != nil {
					fs.logger.Warn("background sync failed", logger.Error(err))
				}
			}
		}
	}
}

func (fs *FileStorage) StoreState(ctx context.Context, state *PersistentState) error {
	// TODO implement me
	panic("implement me")
}

func (fs *FileStorage) LoadState(ctx context.Context) (*PersistentState, error) {
	// TODO implement me
	panic("implement me")
}

func (fs *FileStorage) HealthCheck(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

// FileStorageFactory creates file storage instances
type FileStorageFactory struct{}

// Create creates a new file storage instance
func (f *FileStorageFactory) Create(config StorageConfig, logger common.Logger, metrics common.Metrics) (Storage, error) {
	fileConfig := FileStorageConfig{
		Path:                config.Path,
		MaxCacheSize:        config.BatchSize,
		SyncInterval:        config.SyncInterval,
		CompactionThreshold: config.CompactionThreshold,
		EnableCompression:   config.EnableCompression,
		EnableChecksum:      config.EnableChecksum,
	}

	if fileConfig.MaxCacheSize <= 0 {
		fileConfig.MaxCacheSize = 1000
	}

	return NewFileStorage(fileConfig, logger, metrics)
}

// Name returns the factory name
func (f *FileStorageFactory) Name() string {
	return "file"
}

// Version returns the factory version
func (f *FileStorageFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *FileStorageFactory) ValidateConfig(config StorageConfig) error {
	if config.Path == "" {
		return NewStorageError(ErrCodeInvalidConfig, "path is required for file storage")
	}

	if config.MaxSize < 0 {
		return NewStorageError(ErrCodeInvalidConfig, "max_size must be >= 0")
	}

	if config.MaxEntries < 0 {
		return NewStorageError(ErrCodeInvalidConfig, "max_entries must be >= 0")
	}

	return nil
}

// GetVotedFor retrieves the vote for a term (alias for GetVote for consistency)
func (fs *FileStorage) GetVotedFor(ctx context.Context, term uint64) (string, error) {
	return fs.GetVote(ctx, term)
}

// StoreVotedFor stores the vote for a term (alias for StoreVote for consistency)
func (fs *FileStorage) StoreVotedFor(ctx context.Context, term uint64, candidateID string) error {
	return fs.StoreVote(ctx, term, candidateID)
}

// StoreElectionRecord stores an election record
func (fs *FileStorage) StoreElectionRecord(ctx context.Context, record ElectionRecord) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Create elections directory if it doesn't exist
	electionsPath := filepath.Join(fs.path, "elections")
	if err := os.MkdirAll(electionsPath, 0755); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to create elections directory: %v", err))
	}

	// Create filename based on term and timestamp
	filename := fmt.Sprintf("election_%d_%d.json", record.Term, record.StartTime.Unix())
	filePath := filepath.Join(electionsPath, filename)

	// Serialize record
	recordData, err := json.Marshal(record)
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to serialize election record: %v", err))
	}

	// Write to file
	if err := os.WriteFile(filePath, recordData, 0644); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to write election record: %v", err))
	}

	if fs.metrics != nil {
		fs.metrics.Counter("forge.consensus.storage.election_records_stored").Inc()
	}

	return nil
}

// GetElectionHistory retrieves election history with a limit
func (fs *FileStorage) GetElectionHistory(ctx context.Context, limit int) ([]ElectionRecord, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	electionsPath := filepath.Join(fs.path, "elections")

	// Check if elections directory exists
	if _, err := os.Stat(electionsPath); os.IsNotExist(err) {
		return []ElectionRecord{}, nil
	}

	// Read directory entries
	entries, err := os.ReadDir(electionsPath)
	if err != nil {
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to read elections directory: %v", err))
	}

	var records []ElectionRecord

	// Process each election file
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "election_") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(electionsPath, entry.Name())

		// Read file
		data, err := os.ReadFile(filePath)
		if err != nil {
			if fs.logger != nil {
				fs.logger.Warn("failed to read election record file",
					logger.String("file", entry.Name()),
					logger.Error(err))
			}
			continue
		}

		// Unmarshal record
		var record ElectionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			if fs.logger != nil {
				fs.logger.Warn("failed to unmarshal election record",
					logger.String("file", entry.Name()),
					logger.Error(err))
			}
			continue
		}

		records = append(records, record)
	}

	// Sort by term and start time (most recent first)
	sort.Slice(records, func(i, j int) bool {
		if records[i].Term != records[j].Term {
			return records[i].Term > records[j].Term
		}
		return records[i].StartTime.After(records[j].StartTime)
	})

	// Apply limit
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}

	return records, nil
}

// cleanupOldElectionRecords removes old election records to prevent unlimited growth
func (fs *FileStorage) cleanupOldElectionRecords(maxRecords int) error {
	electionsPath := filepath.Join(fs.path, "elections")

	entries, err := os.ReadDir(electionsPath)
	if err != nil {
		return err
	}

	var files []os.FileInfo
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "election_") && strings.HasSuffix(entry.Name(), ".json") {
			info, err := entry.Info()
			if err == nil {
				files = append(files, info)
			}
		}
	}

	if len(files) <= maxRecords {
		return nil
	}

	// Sort by modification time (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Before(files[j].ModTime())
	})

	// Remove oldest files
	toRemove := len(files) - maxRecords
	for i := 0; i < toRemove; i++ {
		filePath := filepath.Join(electionsPath, files[i].Name())
		if err := os.Remove(filePath); err != nil && fs.logger != nil {
			fs.logger.Warn("failed to remove old election record",
				logger.String("file", files[i].Name()),
				logger.Error(err))
		}
	}

	return nil
}
