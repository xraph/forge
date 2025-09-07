package storage

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// Storage defines the interface for log storage
type Storage interface {
	// StoreEntry stores a log entry
	StoreEntry(ctx context.Context, entry LogEntry) error

	// GetEntry retrieves a log entry by index
	GetEntry(ctx context.Context, index uint64) (*LogEntry, error)

	// GetEntries retrieves log entries in a range
	GetEntries(ctx context.Context, start, end uint64) ([]LogEntry, error)

	// GetLastEntry retrieves the last log entry
	GetLastEntry(ctx context.Context) (*LogEntry, error)

	// GetFirstEntry retrieves the first log entry
	GetFirstEntry(ctx context.Context) (*LogEntry, error)

	// DeleteEntry deletes a log entry
	DeleteEntry(ctx context.Context, index uint64) error

	// DeleteEntriesFrom deletes entries from a given index
	DeleteEntriesFrom(ctx context.Context, index uint64) error

	// DeleteEntriesTo deletes entries up to a given index
	DeleteEntriesTo(ctx context.Context, index uint64) error

	// GetLastIndex returns the index of the last log entry
	GetLastIndex(ctx context.Context) (uint64, error)

	// GetFirstIndex returns the index of the first log entry
	GetFirstIndex(ctx context.Context) (uint64, error)

	// StoreSnapshot stores a snapshot
	StoreSnapshot(ctx context.Context, snapshot Snapshot) error

	// GetSnapshot retrieves a snapshot
	GetSnapshot(ctx context.Context) (*Snapshot, error)

	// DeleteSnapshot deletes a snapshot
	DeleteSnapshot(ctx context.Context) error

	// StoreTerm stores the current term
	StoreTerm(ctx context.Context, term uint64) error

	// GetTerm retrieves the current term
	GetTerm(ctx context.Context) (uint64, error)

	// StoreVote stores the vote for a term
	StoreVote(ctx context.Context, term uint64, candidateID string) error

	// GetVote retrieves the vote for a term
	GetVote(ctx context.Context, term uint64) (string, error)

	// StoreState saves the given persistent state to the underlying storage and returns an error if the operation fails.
	StoreState(ctx context.Context, state *PersistentState) error

	// LoadState retrieves the persisted state from storage, including the term, vote, and other metadata.
	LoadState(ctx context.Context) (*PersistentState, error)

	// // StoreConfiguration stores the given cluster configuration in persistent storage.
	// StoreConfiguration(ctx context.Context, config Configuration) error
	//
	// // GetConfiguration retrieves the current cluster configuration from storage.
	// // It returns a pointer to the Configuration and an error if the operation fails.
	// GetConfiguration(ctx context.Context) (*Configuration, error)

	// Sync ensures all data is persisted
	Sync(ctx context.Context) error

	// Close closes the storage
	Close(ctx context.Context) error

	// GetStats returns storage statistics
	GetStats() StorageStats

	// Compact compacts the storage
	Compact(ctx context.Context, index uint64) error

	// HealthCheck check consensus health
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

// StorageError represents a storage error
type StorageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Index   uint64 `json:"index,omitempty"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *StorageError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *StorageError) Unwrap() error {
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
func NewStorageError(code, message string) *StorageError {
	return &StorageError{
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
