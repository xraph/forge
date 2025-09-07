package statemachine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/logger"
)

// PersistentStateMachine implements a persistent state machine with disk storage
type PersistentStateMachine struct {
	*MemoryStateMachine
	storagePath  string
	stateFile    string
	snapshotDir  string
	syncInterval time.Duration
	autoSync     bool
	lastSyncTime time.Time
	syncCount    int64
	logger       common.Logger
	metrics      common.Metrics
	syncMu       sync.Mutex
}

// PersistentStateMachineFactory creates persistent state machines
type PersistentStateMachineFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewPersistentStateMachineFactory creates a new persistent state machine factory
func NewPersistentStateMachineFactory(logger common.Logger, metrics common.Metrics) *PersistentStateMachineFactory {
	return &PersistentStateMachineFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new persistent state machine
func (f *PersistentStateMachineFactory) Create(config StateMachineConfig) (StateMachine, error) {
	// Create memory state machine first
	memoryFactory := NewMemoryStateMachineFactory()
	memorySM, err := memoryFactory.Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory state machine: %w", err)
	}

	// Get storage path from config
	storagePath, ok := config.Options["storage_path"].(string)
	if !ok {
		storagePath = config.StoragePath
	}
	if storagePath == "" {
		storagePath = "./data/statemachine"
	}

	// Get sync interval from config
	syncInterval := 5 * time.Second
	if interval, ok := config.Options["sync_interval"].(time.Duration); ok {
		syncInterval = interval
	}

	// Get auto sync setting from config
	autoSync := true
	if auto, ok := config.Options["auto_sync"].(bool); ok {
		autoSync = auto
	}

	// Create storage directory
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	snapshotDir := filepath.Join(storagePath, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	psm := &PersistentStateMachine{
		MemoryStateMachine: memorySM.(*MemoryStateMachine),
		storagePath:        storagePath,
		stateFile:          filepath.Join(storagePath, "state.json"),
		snapshotDir:        snapshotDir,
		syncInterval:       syncInterval,
		autoSync:           autoSync,
		lastSyncTime:       time.Now(),
		syncCount:          0,
		logger:             f.logger,
		metrics:            f.metrics,
	}

	// Load existing state if available
	if err := psm.loadState(); err != nil {
		if f.logger != nil {
			f.logger.Warn("failed to load existing state", logger.String("error", err.Error()))
		}
	}

	// Start auto sync if enabled
	if autoSync {
		go psm.autoSyncLoop()
	}

	return psm, nil
}

// Name returns the factory name
func (f *PersistentStateMachineFactory) Name() string {
	return StateMachineTypePersistent
}

// Version returns the factory version
func (f *PersistentStateMachineFactory) Version() string {
	return "1.0.0"
}

// Apply applies a log entry to the state machine and persists changes
func (psm *PersistentStateMachine) Apply(ctx context.Context, entry storage.LogEntry) error {
	// Apply to memory state machine first
	if err := psm.MemoryStateMachine.Apply(ctx, entry); err != nil {
		return err
	}

	// Persist state if auto sync is disabled
	if !psm.autoSync {
		if err := psm.persistState(); err != nil {
			if psm.logger != nil {
				psm.logger.Error("failed to persist state after apply", logger.Error(err))
			}
			if psm.metrics != nil {
				psm.metrics.Counter("forge.consensus.statemachine.persist_errors").Inc()
			}
		}
	}

	return nil
}

// CreateSnapshot creates a snapshot and persists it to disk
func (psm *PersistentStateMachine) CreateSnapshot() (*storage.Snapshot, error) {
	// Create snapshot in memory first
	snapshot, err := psm.MemoryStateMachine.CreateSnapshot()
	if err != nil {
		return nil, err
	}

	// Persist snapshot to disk
	if err := psm.persistSnapshot(snapshot); err != nil {
		if psm.logger != nil {
			psm.logger.Error("failed to persist snapshot", logger.Error(err))
		}
		if psm.metrics != nil {
			psm.metrics.Counter("forge.consensus.statemachine.snapshot_persist_errors").Inc()
		}
		return nil, fmt.Errorf("failed to persist snapshot: %w", err)
	}

	return snapshot, nil
}

// RestoreSnapshot restores the state machine from a snapshot
func (psm *PersistentStateMachine) RestoreSnapshot(snapshot *storage.Snapshot) error {
	// Restore to memory state machine
	if err := psm.MemoryStateMachine.RestoreSnapshot(snapshot); err != nil {
		return err
	}

	// Persist the restored state
	if err := psm.persistState(); err != nil {
		if psm.logger != nil {
			psm.logger.Error("failed to persist state after restore", logger.Error(err))
		}
		if psm.metrics != nil {
			psm.metrics.Counter("forge.consensus.statemachine.persist_errors").Inc()
		}
		return fmt.Errorf("failed to persist restored state: %w", err)
	}

	return nil
}

// Reset resets the state machine and clears persistent storage
func (psm *PersistentStateMachine) Reset() error {
	// Reset memory state machine
	if err := psm.MemoryStateMachine.Reset(); err != nil {
		return err
	}

	// Clear persistent storage
	if err := psm.clearPersistentStorage(); err != nil {
		if psm.logger != nil {
			psm.logger.Error("failed to clear persistent storage", logger.Error(err))
		}
		return fmt.Errorf("failed to clear persistent storage: %w", err)
	}

	return nil
}

// Close closes the state machine and stops auto sync
func (psm *PersistentStateMachine) Close(ctx context.Context) error {
	// Stop auto sync
	psm.autoSync = false

	// Perform final sync
	if err := psm.persistState(); err != nil {
		if psm.logger != nil {
			psm.logger.Error("failed to persist state during close", logger.Error(err))
		}
	}

	// Close memory state machine
	return psm.MemoryStateMachine.Close(ctx)
}

// Sync forces a synchronization of state to disk
func (psm *PersistentStateMachine) Sync() error {
	return psm.persistState()
}

// GetSyncStats returns synchronization statistics
func (psm *PersistentStateMachine) GetSyncStats() SyncStats {
	psm.syncMu.Lock()
	defer psm.syncMu.Unlock()

	return SyncStats{
		LastSyncTime: psm.lastSyncTime,
		SyncCount:    psm.syncCount,
		AutoSync:     psm.autoSync,
		SyncInterval: psm.syncInterval,
	}
}

// SyncStats contains synchronization statistics
type SyncStats struct {
	LastSyncTime time.Time     `json:"last_sync_time"`
	SyncCount    int64         `json:"sync_count"`
	AutoSync     bool          `json:"auto_sync"`
	SyncInterval time.Duration `json:"sync_interval"`
}

// persistState persists the current state to disk
func (psm *PersistentStateMachine) persistState() error {
	psm.syncMu.Lock()
	defer psm.syncMu.Unlock()

	start := time.Now()
	defer func() {
		if psm.metrics != nil {
			psm.metrics.Histogram("forge.consensus.statemachine.persist_duration").Observe(time.Since(start).Seconds())
		}
	}()

	// Get current state
	state := psm.MemoryStateMachine.GetState()

	// Create persistent state structure
	persistentState := PersistentState{
		State:          state,
		AppliedEntries: psm.MemoryStateMachine.appliedEntries,
		LastApplied:    psm.MemoryStateMachine.lastApplied,
		SnapshotCount:  psm.MemoryStateMachine.snapshotCount,
		LastSnapshot:   psm.MemoryStateMachine.lastSnapshot,
		UpdatedAt:      time.Now(),
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(persistentState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temporary file first
	tempFile := psm.stateFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary state file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, psm.stateFile); err != nil {
		os.Remove(tempFile) // Clean up temporary file
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	psm.lastSyncTime = time.Now()
	psm.syncCount++

	if psm.metrics != nil {
		psm.metrics.Counter("forge.consensus.statemachine.persist_operations").Inc()
	}

	return nil
}

// loadState loads state from disk
func (psm *PersistentStateMachine) loadState() error {
	// Check if state file exists
	if _, err := os.Stat(psm.stateFile); os.IsNotExist(err) {
		return nil // No existing state to load
	}

	// Read state file
	data, err := os.ReadFile(psm.stateFile)
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	// Unmarshal persistent state
	var persistentState PersistentState
	if err := json.Unmarshal(data, &persistentState); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Restore state to memory state machine
	psm.MemoryStateMachine.mu.Lock()
	psm.MemoryStateMachine.state = persistentState.State
	psm.MemoryStateMachine.appliedEntries = persistentState.AppliedEntries
	psm.MemoryStateMachine.lastApplied = persistentState.LastApplied
	psm.MemoryStateMachine.snapshotCount = persistentState.SnapshotCount
	psm.MemoryStateMachine.lastSnapshot = persistentState.LastSnapshot
	psm.MemoryStateMachine.mu.Unlock()

	if psm.logger != nil {
		psm.logger.Info("loaded state from disk",
			logger.Uint64("last_applied", persistentState.LastApplied),
			logger.Int64("applied_entries", persistentState.AppliedEntries),
			logger.Time("updated_at", persistentState.UpdatedAt),
		)
	}

	return nil
}

// persistSnapshot persists a snapshot to disk
func (psm *PersistentStateMachine) persistSnapshot(snapshot *storage.Snapshot) error {
	// Create snapshot filename
	filename := fmt.Sprintf("snapshot_%d_%d.json", snapshot.Index, snapshot.Term)
	snapshotFile := filepath.Join(psm.snapshotDir, filename)

	// Marshal snapshot to JSON
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write to file
	if err := os.WriteFile(snapshotFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}

	if psm.logger != nil {
		psm.logger.Info("persisted snapshot to disk",
			logger.String("file", snapshotFile),
			logger.Uint64("index", snapshot.Index),
			logger.Uint64("term", snapshot.Term),
		)
	}

	if psm.metrics != nil {
		psm.metrics.Counter("forge.consensus.statemachine.snapshots_persisted").Inc()
	}

	return nil
}

// clearPersistentStorage clears all persistent storage
func (psm *PersistentStateMachine) clearPersistentStorage() error {
	// Remove state file
	if err := os.Remove(psm.stateFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	// Remove snapshot directory
	if err := os.RemoveAll(psm.snapshotDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove snapshot directory: %w", err)
	}

	// Recreate snapshot directory
	if err := os.MkdirAll(psm.snapshotDir, 0755); err != nil {
		return fmt.Errorf("failed to recreate snapshot directory: %w", err)
	}

	return nil
}

// autoSyncLoop runs the auto sync loop
func (psm *PersistentStateMachine) autoSyncLoop() {
	ticker := time.NewTicker(psm.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !psm.autoSync {
				return
			}

			if err := psm.persistState(); err != nil {
				if psm.logger != nil {
					psm.logger.Error("failed to auto sync state", logger.Error(err))
				}
				if psm.metrics != nil {
					psm.metrics.Counter("forge.consensus.statemachine.auto_sync_errors").Inc()
				}
			}
		}
	}
}

// listSnapshots lists all snapshots in the snapshot directory
func (psm *PersistentStateMachine) listSnapshots() ([]string, error) {
	files, err := os.ReadDir(psm.snapshotDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot directory: %w", err)
	}

	var snapshots []string
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".json" {
			snapshots = append(snapshots, file.Name())
		}
	}

	return snapshots, nil
}

// loadSnapshot loads a snapshot from disk
func (psm *PersistentStateMachine) loadSnapshot(filename string) (*storage.Snapshot, error) {
	snapshotFile := filepath.Join(psm.snapshotDir, filename)

	// Read snapshot file
	data, err := os.ReadFile(snapshotFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	// Unmarshal snapshot
	var snapshot storage.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// cleanupOldSnapshots removes old snapshots keeping only the most recent ones
func (psm *PersistentStateMachine) cleanupOldSnapshots(keepCount int) error {
	snapshots, err := psm.listSnapshots()
	if err != nil {
		return err
	}

	// Sort snapshots by modification time (most recent first)
	// This is a simplified approach - in practice, you'd want to parse the filename
	// to get the index and term for proper sorting

	if len(snapshots) <= keepCount {
		return nil
	}

	// Remove oldest snapshots
	for i := keepCount; i < len(snapshots); i++ {
		snapshotFile := filepath.Join(psm.snapshotDir, snapshots[i])
		if err := os.Remove(snapshotFile); err != nil {
			if psm.logger != nil {
				psm.logger.Warn("failed to remove old snapshot", logger.String("file", snapshotFile), logger.Error(err))
			}
		}
	}

	return nil
}

// GetStorageStats returns storage statistics
func (psm *PersistentStateMachine) GetStorageStats() (StorageStats, error) {
	stats := StorageStats{
		StoragePath:   psm.storagePath,
		SnapshotDir:   psm.snapshotDir,
		StateFile:     psm.stateFile,
		SnapshotCount: 0,
		TotalSize:     0,
	}

	// Get state file size
	if info, err := os.Stat(psm.stateFile); err == nil {
		stats.StateFileSize = info.Size()
		stats.TotalSize += info.Size()
	}

	// Get snapshot statistics
	snapshots, err := psm.listSnapshots()
	if err != nil {
		return stats, err
	}

	stats.SnapshotCount = int64(len(snapshots))

	for _, snapshot := range snapshots {
		snapshotFile := filepath.Join(psm.snapshotDir, snapshot)
		if info, err := os.Stat(snapshotFile); err == nil {
			stats.TotalSize += info.Size()
		}
	}

	return stats, nil
}

// StorageStats contains storage statistics
type StorageStats struct {
	StoragePath   string `json:"storage_path"`
	SnapshotDir   string `json:"snapshot_dir"`
	StateFile     string `json:"state_file"`
	StateFileSize int64  `json:"state_file_size"`
	SnapshotCount int64  `json:"snapshot_count"`
	TotalSize     int64  `json:"total_size"`
}

// PersistentState represents the persistent state structure
type PersistentState struct {
	State          interface{} `json:"state"`
	AppliedEntries int64       `json:"applied_entries"`
	LastApplied    uint64      `json:"last_applied"`
	SnapshotCount  int64       `json:"snapshot_count"`
	LastSnapshot   time.Time   `json:"last_snapshot"`
	UpdatedAt      time.Time   `json:"updated_at"`
}
