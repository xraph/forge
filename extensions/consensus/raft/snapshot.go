package raft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// SnapshotManager manages Raft snapshots.
type SnapshotManager struct {
	nodeID       string
	storage      internal.Storage
	stateMachine internal.StateMachine
	logger       forge.Logger

	// Snapshot state
	lastSnapshotIndex  uint64
	lastSnapshotTerm   uint64
	snapshotInProgress bool
	mu                 sync.RWMutex

	// Configuration
	snapshotInterval  time.Duration
	snapshotThreshold uint64
}

// SnapshotManagerConfig contains snapshot manager configuration.
type SnapshotManagerConfig struct {
	NodeID            string
	SnapshotInterval  time.Duration
	SnapshotThreshold uint64
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(
	config SnapshotManagerConfig,
	storage internal.Storage,
	stateMachine internal.StateMachine,
	logger forge.Logger,
) *SnapshotManager {
	if config.SnapshotInterval == 0 {
		config.SnapshotInterval = 5 * time.Minute
	}

	if config.SnapshotThreshold == 0 {
		config.SnapshotThreshold = 10000
	}

	return &SnapshotManager{
		nodeID:            config.NodeID,
		storage:           storage,
		stateMachine:      stateMachine,
		logger:            logger,
		snapshotInterval:  config.SnapshotInterval,
		snapshotThreshold: config.SnapshotThreshold,
	}
}

// CreateSnapshot creates a new snapshot.
func (sm *SnapshotManager) CreateSnapshot(ctx context.Context, lastIncludedIndex, lastIncludedTerm uint64) error {
	sm.mu.Lock()

	if sm.snapshotInProgress {
		sm.mu.Unlock()

		return errors.New("snapshot already in progress")
	}

	sm.snapshotInProgress = true
	sm.mu.Unlock()

	defer func() {
		sm.mu.Lock()
		sm.snapshotInProgress = false
		sm.mu.Unlock()
	}()

	startTime := time.Now()

	sm.logger.Info("creating snapshot",
		forge.F("node_id", sm.nodeID),
		forge.F("last_index", lastIncludedIndex),
		forge.F("last_term", lastIncludedTerm),
	)

	// Get state machine snapshot
	snap, err := sm.stateMachine.Snapshot()
	if err != nil {
		return fmt.Errorf("failed to create state machine snapshot: %w", err)
	}

	data := snap.Data

	// Calculate checksum
	checksum := fmt.Sprintf("%x", sha256.Sum256(data))

	// Create snapshot metadata
	metadata := internal.SnapshotMetadata{
		Index:    lastIncludedIndex,
		Term:     lastIncludedTerm,
		Size:     int64(len(data)),
		Checksum: checksum,
		Created:  time.Now(),
	}

	// Encode snapshot with metadata
	var buf bytes.Buffer

	encoder := gob.NewEncoder(&buf)

	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("failed to encode snapshot metadata: %w", err)
	}

	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode snapshot data: %w", err)
	}

	// Store snapshot
	snapshotKey := fmt.Appendf(nil, "snapshot/%d/%d", lastIncludedTerm, lastIncludedIndex)
	if err := sm.storage.Set(snapshotKey, buf.Bytes()); err != nil {
		return fmt.Errorf("failed to store snapshot: %w", err)
	}

	// Store snapshot metadata separately for quick access
	metadataKey := []byte("snapshot/latest/metadata")

	var metaBuf bytes.Buffer
	if err := gob.NewEncoder(&metaBuf).Encode(metadata); err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	if err := sm.storage.Set(metadataKey, metaBuf.Bytes()); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	// Update tracking
	sm.mu.Lock()
	sm.lastSnapshotIndex = lastIncludedIndex
	sm.lastSnapshotTerm = lastIncludedTerm
	sm.mu.Unlock()

	duration := time.Since(startTime)
	sm.logger.Info("snapshot created successfully",
		forge.F("node_id", sm.nodeID),
		forge.F("index", lastIncludedIndex),
		forge.F("term", lastIncludedTerm),
		forge.F("size_bytes", len(data)),
		forge.F("duration_ms", duration.Milliseconds()),
	)

	return nil
}

// RestoreSnapshot restores from a snapshot.
func (sm *SnapshotManager) RestoreSnapshot(ctx context.Context, snapshotData []byte) error {
	sm.mu.Lock()

	if sm.snapshotInProgress {
		sm.mu.Unlock()

		return errors.New("snapshot operation already in progress")
	}

	sm.snapshotInProgress = true
	sm.mu.Unlock()

	defer func() {
		sm.mu.Lock()
		sm.snapshotInProgress = false
		sm.mu.Unlock()
	}()

	startTime := time.Now()

	sm.logger.Info("restoring snapshot",
		forge.F("node_id", sm.nodeID),
		forge.F("size_bytes", len(snapshotData)),
	)

	// Decode snapshot
	buf := bytes.NewReader(snapshotData)
	decoder := gob.NewDecoder(buf)

	var metadata internal.SnapshotMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return fmt.Errorf("failed to decode snapshot metadata: %w", err)
	}

	var data []byte
	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("failed to decode snapshot data: %w", err)
	}

	// Verify checksum
	checksum := fmt.Sprintf("%x", sha256.Sum256(data))
	if checksum != metadata.Checksum {
		return errors.New("snapshot checksum mismatch")
	}

	// Restore to state machine
	snapshot := &internal.Snapshot{
		Index:    metadata.Index,
		Term:     metadata.Term,
		Data:     data,
		Size:     metadata.Size,
		Created:  metadata.Created,
		Checksum: metadata.Checksum,
	}
	if err := sm.stateMachine.Restore(snapshot); err != nil {
		return fmt.Errorf("failed to restore state machine snapshot: %w", err)
	}

	// Update tracking
	sm.mu.Lock()
	sm.lastSnapshotIndex = metadata.Index
	sm.lastSnapshotTerm = metadata.Term
	sm.mu.Unlock()

	duration := time.Since(startTime)
	sm.logger.Info("snapshot restored successfully",
		forge.F("node_id", sm.nodeID),
		forge.F("index", metadata.Index),
		forge.F("term", metadata.Term),
		forge.F("duration_ms", duration.Milliseconds()),
	)

	return nil
}

// GetLatestSnapshot retrieves the latest snapshot.
func (sm *SnapshotManager) GetLatestSnapshot() (*internal.SnapshotMetadata, []byte, error) {
	// Get metadata
	metadataKey := []byte("snapshot/latest/metadata")

	metaBytes, err := sm.storage.Get(metadataKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get snapshot metadata: %w", err)
	}

	var metadata internal.SnapshotMetadata
	if err := gob.NewDecoder(bytes.NewReader(metaBytes)).Decode(&metadata); err != nil {
		return nil, nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	// Get snapshot data
	snapshotKey := fmt.Appendf(nil, "snapshot/%d/%d", metadata.Term, metadata.Index)

	snapshotData, err := sm.storage.Get(snapshotKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get snapshot data: %w", err)
	}

	return &metadata, snapshotData, nil
}

// ShouldCreateSnapshot checks if a snapshot should be created.
func (sm *SnapshotManager) ShouldCreateSnapshot(currentIndex uint64) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.snapshotInProgress {
		return false
	}

	// Check if we've crossed the threshold
	if currentIndex-sm.lastSnapshotIndex >= sm.snapshotThreshold {
		return true
	}

	return false
}

// GetLastSnapshotIndex returns the last snapshot index.
func (sm *SnapshotManager) GetLastSnapshotIndex() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.lastSnapshotIndex
}

// GetLastSnapshotTerm returns the last snapshot term.
func (sm *SnapshotManager) GetLastSnapshotTerm() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.lastSnapshotTerm
}

// InstallSnapshot installs a snapshot received from leader.
func (sm *SnapshotManager) InstallSnapshot(req internal.InstallSnapshotRequest) error {
	sm.logger.Info("installing snapshot",
		forge.F("node_id", sm.nodeID),
		forge.F("term", req.Term),
		forge.F("last_index", req.LastIncludedIndex),
		forge.F("offset", req.Offset),
		forge.F("done", req.Done),
	)

	// TODO: Implement chunked snapshot transfer
	// For now, assume we get the full snapshot in one request

	if req.Done {
		// Restore the complete snapshot
		return sm.RestoreSnapshot(context.Background(), req.Data)
	}

	return nil
}

// CompactLog compacts the log up to the snapshot point.
func (sm *SnapshotManager) CompactLog(snapshotIndex uint64) error {
	// Remove log entries up to snapshotIndex
	logPrefix := []byte("log/")
	// Use GetRange to list keys (ListKeys not available in Storage interface)
	endKey := []byte("log/~") // ~ is after all ASCII characters

	kvPairs, err := sm.storage.GetRange(logPrefix, endKey)
	if err != nil {
		return fmt.Errorf("failed to list log keys: %w", err)
	}

	// Extract keys from key-value pairs
	keys := make([][]byte, len(kvPairs))
	for i, kv := range kvPairs {
		keys[i] = kv.Key
	}

	var compacted int

	for _, key := range keys {
		// Parse index from key (format: "log/{index}")
		var index uint64
		if _, err := fmt.Sscanf(string(key), "log/%d", &index); err != nil {
			continue
		}

		if index <= snapshotIndex {
			if err := sm.storage.Delete(key); err != nil {
				sm.logger.Error("failed to delete log entry",
					forge.F("key", string(key)),
					forge.F("error", err),
				)

				continue
			}

			compacted++
		}
	}

	sm.logger.Info("log compacted",
		forge.F("snapshot_index", snapshotIndex),
		forge.F("entries_removed", compacted),
	)

	return nil
}

// StreamSnapshot streams a snapshot to a writer (for sending to peers).
func (sm *SnapshotManager) StreamSnapshot(writer io.Writer) error {
	metadata, snapshotData, err := sm.GetLatestSnapshot()
	if err != nil {
		return err
	}

	// Write metadata
	encoder := gob.NewEncoder(writer)
	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("failed to write snapshot metadata: %w", err)
	}

	// Write snapshot data
	if _, err := writer.Write(snapshotData); err != nil {
		return fmt.Errorf("failed to write snapshot data: %w", err)
	}

	sm.logger.Debug("snapshot streamed",
		forge.F("index", metadata.Index),
		forge.F("term", metadata.Term),
		forge.F("size_bytes", metadata.Size),
	)

	return nil
}

// ListSnapshots lists all available snapshots.
func (sm *SnapshotManager) ListSnapshots() ([]internal.SnapshotMetadata, error) {
	snapshotPrefix := []byte("snapshot/")
	// Use GetRange to list keys (ListKeys not available in Storage interface)
	endKey := []byte("snapshot/~") // ~ is after all ASCII characters

	kvPairs, err := sm.storage.GetRange(snapshotPrefix, endKey)
	if err != nil {
		return nil, err
	}

	// Extract keys from key-value pairs
	keys := make([][]byte, len(kvPairs))
	for i, kv := range kvPairs {
		keys[i] = kv.Key
	}

	var snapshots []internal.SnapshotMetadata

	seen := make(map[string]bool)

	for _, key := range keys {
		keyStr := string(key)
		if keyStr == "snapshot/latest/metadata" {
			continue
		}

		// Parse term and index from key
		var term, index uint64
		if _, err := fmt.Sscanf(keyStr, "snapshot/%d/%d", &term, &index); err != nil {
			continue
		}

		// Skip duplicates
		id := fmt.Sprintf("%d-%d", term, index)
		if seen[id] {
			continue
		}

		seen[id] = true

		snapshots = append(snapshots, internal.SnapshotMetadata{
			Index: index,
			Term:  term,
		})
	}

	return snapshots, nil
}
