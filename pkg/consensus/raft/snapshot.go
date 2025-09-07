package raft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/statemachine"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/logger"
)

// SnapshotManager manages snapshot creation and restoration
type SnapshotManager struct {
	storage      storage.Storage
	stateMachine statemachine.StateMachine
	logger       common.Logger
	metrics      common.Metrics
	config       SnapshotConfig
	mu           sync.RWMutex
	lastSnapshot *storage.Snapshot
	creating     bool
}

// SnapshotConfig contains configuration for snapshots
type SnapshotConfig struct {
	// Threshold for triggering snapshot creation
	LogSizeThreshold int64         `json:"log_size_threshold"`
	EntryThreshold   int64         `json:"entry_threshold"`
	TimeThreshold    time.Duration `json:"time_threshold"`

	// Compression settings
	EnableCompression bool `json:"enable_compression"`
	CompressionLevel  int  `json:"compression_level"`

	// Retention settings
	RetainCount int `json:"retain_count"`

	// Performance settings
	ChunkSize int `json:"chunk_size"`
}

// DefaultSnapshotConfig returns default snapshot configuration
func DefaultSnapshotConfig() SnapshotConfig {
	return SnapshotConfig{
		LogSizeThreshold:  10 * 1024 * 1024, // 10MB
		EntryThreshold:    1000,
		TimeThreshold:     1 * time.Hour,
		EnableCompression: true,
		CompressionLevel:  6,
		RetainCount:       3,
		ChunkSize:         1024 * 1024, // 1MB
	}
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(storage storage.Storage, stateMachine statemachine.StateMachine, config SnapshotConfig, logger common.Logger, metrics common.Metrics) *SnapshotManager {
	return &SnapshotManager{
		storage:      storage,
		stateMachine: stateMachine,
		logger:       logger,
		metrics:      metrics,
		config:       config,
		creating:     false,
	}
}

// CreateSnapshot creates a new snapshot
func (sm *SnapshotManager) CreateSnapshot(ctx context.Context, lastIncludedIndex, lastIncludedTerm uint64) (*storage.Snapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.creating {
		return nil, fmt.Errorf("snapshot creation already in progress")
	}

	sm.creating = true
	defer func() {
		sm.creating = false
	}()

	startTime := time.Now()

	if sm.logger != nil {
		sm.logger.Info("creating snapshot",
			logger.Uint64("last_included_index", lastIncludedIndex),
			logger.Uint64("last_included_term", lastIncludedTerm),
		)
	}

	// Create snapshot from state machine
	snapshot, err := sm.stateMachine.CreateSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot from state machine: %w", err)
	}

	// Set snapshot metadata
	snapshot.Index = lastIncludedIndex
	snapshot.Term = lastIncludedTerm
	snapshot.Timestamp = time.Now()

	// Compress if enabled
	if sm.config.EnableCompression {
		compressedData, err := sm.compressData(snapshot.Data)
		if err != nil {
			if sm.logger != nil {
				sm.logger.Warn("failed to compress snapshot data", logger.Error(err))
			}
		} else {
			snapshot.Data = compressedData
			if snapshot.Metadata == nil {
				snapshot.Metadata = make(map[string]interface{})
			}
			snapshot.Metadata["compressed"] = true
			snapshot.Metadata["compression_level"] = sm.config.CompressionLevel
		}
	}

	snapshot.Size = int64(len(snapshot.Data))

	// Store snapshot
	if err := sm.storage.StoreSnapshot(ctx, *snapshot); err != nil {
		return nil, fmt.Errorf("failed to store snapshot: %w", err)
	}

	sm.lastSnapshot = snapshot

	duration := time.Since(startTime)

	if sm.logger != nil {
		sm.logger.Info("snapshot created",
			logger.Uint64("last_included_index", lastIncludedIndex),
			logger.Uint64("last_included_term", lastIncludedTerm),
			logger.Int64("size", snapshot.Size),
			logger.Duration("duration", duration),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.consensus.raft.snapshots_created").Inc()
		sm.metrics.Histogram("forge.consensus.raft.snapshot_creation_duration").Observe(duration.Seconds())
		sm.metrics.Histogram("forge.consensus.raft.snapshot_size").Observe(float64(snapshot.Size))
	}

	// Clean up old snapshots
	go sm.cleanupOldSnapshots()

	return snapshot, nil
}

// RestoreSnapshot restores a snapshot
func (sm *SnapshotManager) RestoreSnapshot(ctx context.Context, snapshot *storage.Snapshot) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.creating {
		return fmt.Errorf("cannot restore snapshot while creating")
	}

	startTime := time.Now()

	if sm.logger != nil {
		sm.logger.Info("restoring snapshot",
			logger.Uint64("last_included_index", snapshot.Index),
			logger.Uint64("last_included_term", snapshot.Term),
			logger.Int64("size", snapshot.Size),
		)
	}

	// Decompress if needed
	data := snapshot.Data
	if sm.isCompressed(snapshot) {
		decompressedData, err := sm.decompressData(data)
		if err != nil {
			return fmt.Errorf("failed to decompress snapshot data: %w", err)
		}
		data = decompressedData
	}

	// Create a copy of the snapshot with decompressed data
	restoredSnapshot := &storage.Snapshot{
		Index:     snapshot.Index,
		Term:      snapshot.Term,
		Data:      data,
		Metadata:  snapshot.Metadata,
		Timestamp: snapshot.Timestamp,
		Size:      int64(len(data)),
	}

	// Restore state machine
	if err := sm.stateMachine.RestoreSnapshot(restoredSnapshot); err != nil {
		return fmt.Errorf("failed to restore state machine from snapshot: %w", err)
	}

	sm.lastSnapshot = snapshot

	duration := time.Since(startTime)

	if sm.logger != nil {
		sm.logger.Info("snapshot restored",
			logger.Uint64("last_included_index", snapshot.Index),
			logger.Uint64("last_included_term", snapshot.Term),
			logger.Duration("duration", duration),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.consensus.raft.snapshots_restored").Inc()
		sm.metrics.Histogram("forge.consensus.raft.snapshot_restoration_duration").Observe(duration.Seconds())
	}

	return nil
}

// LoadSnapshot loads the latest snapshot from storage
func (sm *SnapshotManager) LoadSnapshot(ctx context.Context) (*storage.Snapshot, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snapshot, err := sm.storage.GetSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load snapshot from storage: %w", err)
	}

	return snapshot, nil
}

// GetLastSnapshot returns the last snapshot
func (sm *SnapshotManager) GetLastSnapshot() *storage.Snapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.lastSnapshot
}

// ShouldCreateSnapshot checks if a snapshot should be created
func (sm *SnapshotManager) ShouldCreateSnapshot(log *Log) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.creating {
		return false
	}

	logStats := log.GetLogStats()

	// Check size threshold
	if sm.config.LogSizeThreshold > 0 && logStats.Size > sm.config.LogSizeThreshold {
		return true
	}

	// Check entry count threshold
	if sm.config.EntryThreshold > 0 && logStats.EntryCount > sm.config.EntryThreshold {
		return true
	}

	// Check time threshold
	if sm.config.TimeThreshold > 0 && sm.lastSnapshot != nil {
		if time.Since(sm.lastSnapshot.Timestamp) > sm.config.TimeThreshold {
			return true
		}
	}

	return false
}

// IsCreating returns true if snapshot creation is in progress
func (sm *SnapshotManager) IsCreating() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.creating
}

// GetSnapshotInfo returns information about snapshots
func (sm *SnapshotManager) GetSnapshotInfo() SnapshotInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats, err := sm.storage.GetSnapshot(context.Background())
	if err != nil {
		return SnapshotInfo{}
	}

	info := SnapshotInfo{
		Size:  stats.Size,
		Count: 1, // This would be more sophisticated in a real implementation
	}

	if sm.lastSnapshot != nil {
		info.LastIncludedIndex = sm.lastSnapshot.Index
		info.LastIncludedTerm = sm.lastSnapshot.Term
		info.CreatedAt = sm.lastSnapshot.Timestamp
	}

	return info
}

// GetConfig returns the snapshot configuration
func (sm *SnapshotManager) GetConfig() SnapshotConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.config
}

// SetConfig updates the snapshot configuration
func (sm *SnapshotManager) SetConfig(config SnapshotConfig) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.config = config
}

// SendSnapshot sends a snapshot to a peer in chunks
func (sm *SnapshotManager) SendSnapshot(ctx context.Context, peerID string, snapshot *storage.Snapshot, rpc *RaftRPC) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.logger != nil {
		sm.logger.Info("sending snapshot to peer",
			logger.String("peer_id", peerID),
			logger.Uint64("last_included_index", snapshot.Index),
			logger.Uint64("last_included_term", snapshot.Term),
			logger.Int64("size", snapshot.Size),
		)
	}

	chunkSize := sm.config.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1024 * 1024 // Default 1MB
	}

	data := snapshot.Data
	offset := uint64(0)

	for offset < uint64(len(data)) {
		// Calculate chunk size
		remainingSize := uint64(len(data)) - offset
		currentChunkSize := uint64(chunkSize)
		if currentChunkSize > remainingSize {
			currentChunkSize = remainingSize
		}

		// Create chunk
		chunk := data[offset : offset+currentChunkSize]
		done := offset+currentChunkSize >= uint64(len(data))

		// Create install snapshot request
		req := &InstallSnapshotRequest{
			Term:              0,  // This would be set by the leader
			LeaderID:          "", // This would be set by the leader
			LastIncludedIndex: snapshot.Index,
			LastIncludedTerm:  snapshot.Term,
			Offset:            offset,
			Data:              chunk,
			Done:              done,
		}

		// Send chunk
		resp, err := rpc.SendInstallSnapshot(ctx, peerID, req)
		if err != nil {
			return fmt.Errorf("failed to send snapshot chunk: %w", err)
		}

		if resp.Term > 0 {
			// Higher term detected, should step down
			return fmt.Errorf("higher term detected: %d", resp.Term)
		}

		offset += currentChunkSize

		if sm.logger != nil {
			sm.logger.Debug("sent snapshot chunk",
				logger.String("peer_id", peerID),
				logger.Uint64("offset", offset),
				logger.Uint64("size", currentChunkSize),
				logger.Bool("done", done),
			)
		}
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.consensus.raft.snapshots_sent").Inc()
	}

	return nil
}

// ReceiveSnapshot receives a snapshot from a peer
func (sm *SnapshotManager) ReceiveSnapshot(ctx context.Context, chunks []*InstallSnapshotRequest) (*storage.Snapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks received")
	}

	// Sort chunks by offset
	sortedChunks := make([]*InstallSnapshotRequest, len(chunks))
	copy(sortedChunks, chunks)

	// Simple sorting by offset
	for i := 0; i < len(sortedChunks); i++ {
		for j := i + 1; j < len(sortedChunks); j++ {
			if sortedChunks[i].Offset > sortedChunks[j].Offset {
				sortedChunks[i], sortedChunks[j] = sortedChunks[j], sortedChunks[i]
			}
		}
	}

	// Reconstruct snapshot data
	var data []byte
	expectedOffset := uint64(0)

	for _, chunk := range sortedChunks {
		if chunk.Offset != expectedOffset {
			return nil, fmt.Errorf("missing chunk at offset %d", expectedOffset)
		}

		data = append(data, chunk.Data...)
		expectedOffset += uint64(len(chunk.Data))
	}

	// Create snapshot
	snapshot := &storage.Snapshot{
		Index:     chunks[0].LastIncludedIndex,
		Term:      chunks[0].Term,
		Data:      data,
		Timestamp: time.Now(),
		Size:      int64(len(data)),
	}

	if sm.logger != nil {
		sm.logger.Info("received snapshot",
			logger.Uint64("last_included_index", snapshot.Index),
			logger.Uint64("last_included_term", snapshot.Term),
			logger.Int64("size", snapshot.Size),
			logger.Int("chunks", len(chunks)),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("forge.consensus.raft.snapshots_received").Inc()
	}

	return snapshot, nil
}

// compressData compresses snapshot data
func (sm *SnapshotManager) compressData(data []byte) ([]byte, error) {
	// This is a placeholder for compression implementation
	// In a real implementation, you'd use a compression library like gzip
	return data, nil
}

// decompressData decompresses snapshot data
func (sm *SnapshotManager) decompressData(data []byte) ([]byte, error) {
	// This is a placeholder for decompression implementation
	// In a real implementation, you'd use a compression library like gzip
	return data, nil
}

// isCompressed checks if snapshot data is compressed
func (sm *SnapshotManager) isCompressed(snapshot *storage.Snapshot) bool {
	if snapshot.Metadata == nil {
		return false
	}

	compressed, ok := snapshot.Metadata["compressed"]
	if !ok {
		return false
	}

	return compressed.(bool)
}

// cleanupOldSnapshots removes old snapshots
func (sm *SnapshotManager) cleanupOldSnapshots() {
	// This is a placeholder for cleanup implementation
	// In a real implementation, you'd remove old snapshots based on retention policy
}

// ValidateSnapshot validates a snapshot
func (sm *SnapshotManager) ValidateSnapshot(snapshot *storage.Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}

	if snapshot.Index == 0 {
		return fmt.Errorf("invalid last included index: 0")
	}

	if snapshot.Term == 0 {
		return fmt.Errorf("invalid last included term: 0")
	}

	if len(snapshot.Data) == 0 {
		return fmt.Errorf("snapshot data is empty")
	}

	if snapshot.Size != int64(len(snapshot.Data)) {
		return fmt.Errorf("snapshot size mismatch: expected %d, got %d", snapshot.Size, len(snapshot.Data))
	}

	return nil
}

// GetSnapshotStats returns snapshot statistics
func (sm *SnapshotManager) GetSnapshotStats() SnapshotStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := SnapshotStats{
		Creating: sm.creating,
		Config:   sm.config,
	}

	if sm.lastSnapshot != nil {
		stats.LastSnapshot = &SnapshotMetadata{
			Index:     sm.lastSnapshot.Index,
			Term:      sm.lastSnapshot.Term,
			Size:      sm.lastSnapshot.Size,
			CreatedAt: sm.lastSnapshot.Timestamp,
		}
	}

	return stats
}

// SnapshotStats contains snapshot statistics
type SnapshotStats struct {
	Creating     bool              `json:"creating"`
	LastSnapshot *SnapshotMetadata `json:"last_snapshot"`
	Config       SnapshotConfig    `json:"config"`
}

// SnapshotMetadata contains snapshot metadata
type SnapshotMetadata struct {
	Index     uint64    `json:"last_included_index"`
	Term      uint64    `json:"last_included_term"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// HealthCheck performs a health check on the snapshot manager
func (sm *SnapshotManager) HealthCheck(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Check if creation is stuck
	if sm.creating {
		// In a real implementation, you'd check if creation has been running too long
		if sm.logger != nil {
			sm.logger.Debug("snapshot creation in progress")
		}
	}

	// Check storage health
	if err := sm.storage.HealthCheck(ctx); err != nil {
		return fmt.Errorf("storage unhealthy: %w", err)
	}

	// Check state machine health
	if err := sm.stateMachine.HealthCheck(ctx); err != nil {
		return fmt.Errorf("state machine unhealthy: %w", err)
	}

	return nil
}

// Close closes the snapshot manager
func (sm *SnapshotManager) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Wait for any ongoing snapshot creation to complete
	// In a real implementation, you'd have a proper cancellation mechanism

	if sm.logger != nil {
		sm.logger.Info("snapshot manager closed")
	}

	return nil
}
