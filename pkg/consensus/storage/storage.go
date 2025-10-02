package storage

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// Storage defines the interface for log storage
type Storage interface {

	// StoreEntry saves a log entry to the storage layer, ensuring it persists across system restarts or failures.
	StoreEntry(ctx context.Context, entry LogEntry) error

	// GetEntry retrieves the log entry at the specified index from storage or in-memory cache.
	GetEntry(ctx context.Context, index uint64) (*LogEntry, error)

	// GetEntries retrieves a slice of log entries between the provided start and end indices from the storage.
	GetEntries(ctx context.Context, start, end uint64) ([]LogEntry, error)

	// GetLastEntry retrieves the most recent log entry from the storage.
	// Returns the log entry and an error, if any issue occurs during retrieval.
	GetLastEntry(ctx context.Context) (*LogEntry, error)

	// GetFirstEntry retrieves the first log entry from the storage. Returns an error if no entries are available or access fails.
	GetFirstEntry(ctx context.Context) (*LogEntry, error)

	// DeleteEntry removes a log entry at the specified index.
	// It returns an error if the operation fails or the index does not exist.
	DeleteEntry(ctx context.Context, index uint64) error

	// DeleteEntriesFrom removes all log entries starting from the specified index (inclusive) to the most recent entry.
	DeleteEntriesFrom(ctx context.Context, index uint64) error

	// DeleteEntriesTo removes all log entries with an index less than or equal to the specified index. Returns an error on failure.
	DeleteEntriesTo(ctx context.Context, index uint64) error

	// GetLastIndex retrieves the index of the most recently appended log entry in the storage.
	// Returns the last index as a uint64 and an error if the retrieval operation fails.
	GetLastIndex(ctx context.Context) (uint64, error)

	// GetFirstIndex retrieves the index of the first log entry stored in the storage.
	// Returns the index as a uint64 or an error if the operation fails.
	GetFirstIndex(ctx context.Context) (uint64, error)

	// StoreSnapshot stores the given snapshot, which represents a point-in-time state of the system, into persistent storage.
	StoreSnapshot(ctx context.Context, snapshot Snapshot) error

	// GetSnapshot retrieves the most recent snapshot of the state machine from storage.
	GetSnapshot(ctx context.Context) (*Snapshot, error)

	// DeleteSnapshot deletes the current snapshot from storage. Returns an error if the operation fails.
	DeleteSnapshot(ctx context.Context) error

	// StoreTerm stores the specified term in the persistent storage and returns an error if the operation fails.
	StoreTerm(ctx context.Context, term uint64) error

	// GetTerm retrieves the current term of the node from the storage.
	// It returns the term as uint64 and an error if the operation fails.
	GetTerm(ctx context.Context) (uint64, error)

	// StoreVote persists the vote for a given term and candidate ID in the storage.
	// Returns an error if the operation fails.
	StoreVote(ctx context.Context, term uint64, candidateID string) error

	// GetVote retrieves the candidate ID voted for in the specified term from persistent storage. Returns an error if not found.
	GetVote(ctx context.Context, term uint64) (string, error)

	// GetVotedFor retrieves the candidate ID the node voted for in the specified election term. Returns an error if retrieval fails.
	GetVotedFor(ctx context.Context, term uint64) (string, error)

	// StoreVotedFor records the candidate ID voted for in a specific term in the persistent storage.
	StoreVotedFor(ctx context.Context, term uint64, candidateID string) error

	// StoreState persists the given PersistentState data to storage, ensuring it is available for future recovery.
	StoreState(ctx context.Context, state *PersistentState) error

	// LoadState retrieves the persisted state of the Raft node from storage for recovery or initialization purposes.
	LoadState(ctx context.Context) (*PersistentState, error)

	// StoreElectionRecord saves an election record into persistent storage for historical tracking and retrieval purposes.
	StoreElectionRecord(ctx context.Context, record ElectionRecord) error

	// GetElectionHistory retrieves historical election records, limited by the provided count. Returns records or an error.
	GetElectionHistory(ctx context.Context, limit int) ([]ElectionRecord, error)

	// Sync flushes any pending changes to the underlying storage to ensure durability and consistency.
	Sync(ctx context.Context) error

	// Close releases all resources associated with the storage and ensures data is persisted before shutting down.
	Close(ctx context.Context) error

	// GetStats retrieves storage statistics such as entry count, size, and performance metrics. Returns a StorageStats struct.
	GetStats() StorageStats

	// Compact removes all log entries up to and including the specified index to free up storage space.
	Compact(ctx context.Context, index uint64) error

	// HealthCheck checks the health of the storage and ensures consistency in state or logs.
	HealthCheck(ctx context.Context) error
}

// LogEntry represents a log entry
type LogEntry struct {
	Index     uint64                 `json:"index"`
	Term      uint64                 `json:"term"`
	Type      EntryType              `json:"type"`
	Data      []byte                 `json:"data"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
	Checksum  uint32                 `json:"checksum"`
}

// EntryType represents the type of log entry
type EntryType string

const (
	EntryTypeApplication   EntryType = "application"
	EntryTypeConfiguration EntryType = "configuration"
	EntryTypeNoOp          EntryType = "noop"
	EntryTypeBarrier       EntryType = "barrier"
	EntryTypeLogCompaction EntryType = "log_compaction"
)

// Snapshot represents a snapshot of the state machine
type Snapshot struct {
	Index              uint64                 `json:"index"`
	Term               uint64                 `json:"term"`
	Data               []byte                 `json:"data"`
	Metadata           map[string]interface{} `json:"metadata"`
	Timestamp          time.Time              `json:"timestamp"`
	Checksum           uint32                 `json:"checksum"`
	Configuration      []byte                 `json:"configuration"`
	ConfigurationIndex uint64                 `json:"configuration_index"`
	Size               int64                  `json:"size"`
}

// StorageStats contains storage statistics
type StorageStats struct {
	EntryCount      int64         `json:"entry_count"`
	FirstIndex      uint64        `json:"first_index"`
	LastIndex       uint64        `json:"last_index"`
	StorageSize     int64         `json:"storage_size"`
	SnapshotSize    int64         `json:"snapshot_size"`
	CompactionCount int64         `json:"compaction_count"`
	LastCompaction  time.Time     `json:"last_compaction"`
	ReadCount       int64         `json:"read_count"`
	WriteCount      int64         `json:"write_count"`
	ReadLatency     time.Duration `json:"read_latency"`
	WriteLatency    time.Duration `json:"write_latency"`
	SyncCount       int64         `json:"sync_count"`
	SyncLatency     time.Duration `json:"sync_latency"`
	ErrorCount      int64         `json:"error_count"`
	Uptime          time.Duration `json:"uptime"`
}

// PersistentState represents the persistent state of a Raft node
type PersistentState struct {
	CurrentTerm uint64    `json:"current_term"`
	VotedFor    string    `json:"voted_for"`
	NodeID      string    `json:"node_id"`
	ClusterID   string    `json:"cluster_id"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// StorageConfig contains configuration for storage
type StorageConfig struct {
	Type                string                 `json:"type"`
	Path                string                 `json:"path"`
	MaxEntries          int64                  `json:"max_entries"`
	MaxSize             int64                  `json:"max_size"`
	SyncInterval        time.Duration          `json:"sync_interval"`
	CompactionInterval  time.Duration          `json:"compaction_interval"`
	CompactionThreshold int64                  `json:"compaction_threshold"`
	RetentionPeriod     time.Duration          `json:"retention_period"`
	EnableCompression   bool                   `json:"enable_compression"`
	EnableChecksum      bool                   `json:"enable_checksum"`
	BatchSize           int                    `json:"batch_size"`
	Options             map[string]interface{} `json:"options"`
}

// StorageFactory creates storage instances
type StorageFactory interface {
	// Create creates a new storage instance
	Create(config StorageConfig, logger common.Logger, metrics common.Metrics) (Storage, error)

	// Name returns the factory name
	Name() string

	// Version returns the factory version
	Version() string

	// ValidateConfig validates the configuration
	ValidateConfig(config StorageConfig) error
}

// StorageManager manages multiple storage instances
type StorageManager interface {
	// RegisterFactory registers a storage factory
	RegisterFactory(factory StorageFactory) error

	// CreateStorage creates a storage instance
	CreateStorage(config StorageConfig) (Storage, error)

	// GetStorage returns a storage instance by name
	GetStorage(name string) (Storage, error)

	// GetFactories returns all registered factories
	GetFactories() map[string]StorageFactory

	// Close closes all storage instances
	Close(ctx context.Context) error
}

// ElectionRecord represents a historical election record
type ElectionRecord struct {
	Term         uint64        `json:"term"`
	Winner       string        `json:"winner"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`
	VoteCount    int           `json:"vote_count"`
	Participants []string      `json:"participants"`
	Reason       string        `json:"reason"`
}

// Error represents a storage error
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Index   uint64 `json:"index,omitempty"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// Common error codes
const (
	ErrCodeEntryNotFound    = "entry_not_found"
	ErrCodeSnapshotNotFound = "snapshot_not_found"
	ErrCodeInvalidIndex     = "invalid_index"
	ErrCodeStorageFull      = "storage_full"
	ErrCodeCorruptedData    = "corrupted_data"
	ErrCodeIOError          = "io_error"
	ErrCodeInvalidConfig    = "invalid_config"
	ErrCodeCompactionFailed = "compaction_failed"
	ErrCodeSyncFailed       = "sync_failed"
)

// NewStorageError creates a new storage error
func NewStorageError(code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// Common storage types
const (
	StorageTypeMemory   = "memory"
	StorageTypeFile     = "file"
	StorageTypeDatabase = "database"
)

// LogEntryBuilder helps build log entries
type LogEntryBuilder struct {
	entry LogEntry
}

// NewLogEntryBuilder creates a new log entry builder
func NewLogEntryBuilder() *LogEntryBuilder {
	return &LogEntryBuilder{
		entry: LogEntry{
			Metadata:  make(map[string]interface{}),
			Timestamp: time.Now(),
		},
	}
}

// WithIndex sets the index
func (b *LogEntryBuilder) WithIndex(index uint64) *LogEntryBuilder {
	b.entry.Index = index
	return b
}

// WithTerm sets the term
func (b *LogEntryBuilder) WithTerm(term uint64) *LogEntryBuilder {
	b.entry.Term = term
	return b
}

// WithType sets the type
func (b *LogEntryBuilder) WithType(entryType EntryType) *LogEntryBuilder {
	b.entry.Type = entryType
	return b
}

// WithData sets the data
func (b *LogEntryBuilder) WithData(data []byte) *LogEntryBuilder {
	b.entry.Data = data
	return b
}

// WithMetadata adds metadata
func (b *LogEntryBuilder) WithMetadata(key string, value interface{}) *LogEntryBuilder {
	b.entry.Metadata[key] = value
	return b
}

// WithChecksum sets the checksum
func (b *LogEntryBuilder) WithChecksum(checksum uint32) *LogEntryBuilder {
	b.entry.Checksum = checksum
	return b
}

// Build builds the log entry
func (b *LogEntryBuilder) Build() LogEntry {
	return b.entry
}

// SnapshotBuilder helps build snapshots
type SnapshotBuilder struct {
	snapshot Snapshot
}

// NewSnapshotBuilder creates a new snapshot builder
func NewSnapshotBuilder() *SnapshotBuilder {
	return &SnapshotBuilder{
		snapshot: Snapshot{
			Metadata:  make(map[string]interface{}),
			Timestamp: time.Now(),
		},
	}
}

// WithIndex sets the index
func (b *SnapshotBuilder) WithIndex(index uint64) *SnapshotBuilder {
	b.snapshot.Index = index
	return b
}

// WithTerm sets the term
func (b *SnapshotBuilder) WithTerm(term uint64) *SnapshotBuilder {
	b.snapshot.Term = term
	return b
}

// WithData sets the data
func (b *SnapshotBuilder) WithData(data []byte) *SnapshotBuilder {
	b.snapshot.Data = data
	return b
}

// WithMetadata adds metadata
func (b *SnapshotBuilder) WithMetadata(key string, value interface{}) *SnapshotBuilder {
	b.snapshot.Metadata[key] = value
	return b
}

// WithConfiguration sets the configuration
func (b *SnapshotBuilder) WithConfiguration(config []byte, index uint64) *SnapshotBuilder {
	b.snapshot.Configuration = config
	b.snapshot.ConfigurationIndex = index
	return b
}

// Build builds the snapshot
func (b *SnapshotBuilder) Build() Snapshot {
	return b.snapshot
}

// Utility functions

// ValidateLogEntry validates a log entry
func ValidateLogEntry(entry LogEntry) error {
	if entry.Index == 0 {
		return NewStorageError(ErrCodeInvalidIndex, "index cannot be zero")
	}
	if entry.Term == 0 {
		return NewStorageError(ErrCodeInvalidIndex, "term cannot be zero")
	}
	if entry.Type == "" {
		return NewStorageError(ErrCodeInvalidIndex, "type is required")
	}
	if entry.Data == nil {
		return NewStorageError(ErrCodeInvalidIndex, "data is required")
	}
	return nil
}

// ValidateSnapshot validates a snapshot
func ValidateSnapshot(snapshot Snapshot) error {
	if snapshot.Index == 0 {
		return NewStorageError(ErrCodeInvalidIndex, "index cannot be zero")
	}
	if snapshot.Term == 0 {
		return NewStorageError(ErrCodeInvalidIndex, "term cannot be zero")
	}
	if snapshot.Data == nil {
		return NewStorageError(ErrCodeInvalidIndex, "data is required")
	}
	return nil
}

// CalculateChecksum calculates a checksum for data
func CalculateChecksum(data []byte) uint32 {
	// Simple CRC32 checksum
	// In practice, you'd use a proper CRC32 implementation
	var checksum uint32
	for _, b := range data {
		checksum = checksum*31 + uint32(b)
	}
	return checksum
}

// IsValidEntryType checks if an entry type is valid
func IsValidEntryType(entryType EntryType) bool {
	switch entryType {
	case EntryTypeApplication, EntryTypeConfiguration, EntryTypeNoOp, EntryTypeBarrier, EntryTypeLogCompaction:
		return true
	default:
		return false
	}
}

// CloneLogEntry creates a deep copy of a log entry
func CloneLogEntry(original LogEntry) LogEntry {
	clone := LogEntry{
		Index:     original.Index,
		Term:      original.Term,
		Type:      original.Type,
		Data:      make([]byte, len(original.Data)),
		Metadata:  make(map[string]interface{}),
		Timestamp: original.Timestamp,
		Checksum:  original.Checksum,
	}

	copy(clone.Data, original.Data)
	for k, v := range original.Metadata {
		clone.Metadata[k] = v
	}

	return clone
}

// CloneSnapshot creates a deep copy of a snapshot
func CloneSnapshot(original Snapshot) Snapshot {
	clone := Snapshot{
		Index:              original.Index,
		Term:               original.Term,
		Data:               make([]byte, len(original.Data)),
		Metadata:           make(map[string]interface{}),
		Timestamp:          original.Timestamp,
		Checksum:           original.Checksum,
		Configuration:      make([]byte, len(original.Configuration)),
		ConfigurationIndex: original.ConfigurationIndex,
	}

	copy(clone.Data, original.Data)
	copy(clone.Configuration, original.Configuration)
	for k, v := range original.Metadata {
		clone.Metadata[k] = v
	}

	return clone
}

// LogEntrySize calculates the size of a log entry in bytes
func LogEntrySize(entry LogEntry) int {
	size := 8 + 8 + 4 + len(entry.Data) + 4 // index + term + type + data + checksum

	// Approximate size of metadata
	for k, v := range entry.Metadata {
		size += len(k)
		if s, ok := v.(string); ok {
			size += len(s)
		} else {
			size += 8 // Approximate size for other types
		}
	}

	return size
}

// SnapshotSize calculates the size of a snapshot in bytes
func SnapshotSize(snapshot Snapshot) int {
	size := 8 + 8 + len(snapshot.Data) + 4 + len(snapshot.Configuration) + 8 // index + term + data + checksum + config + config_index

	// Approximate size of metadata
	for k, v := range snapshot.Metadata {
		size += len(k)
		if s, ok := v.(string); ok {
			size += len(s)
		} else {
			size += 8 // Approximate size for other types
		}
	}

	return size
}
