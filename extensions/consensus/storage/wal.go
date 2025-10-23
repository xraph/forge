package storage

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// WAL implements Write-Ahead Logging for consensus operations
type WAL struct {
	nodeID string
	logger forge.Logger

	// File management
	dir      string
	file     *os.File
	fileMu   sync.Mutex
	fileSize int64

	// Configuration
	config WALConfig

	// Statistics
	stats   WALStatistics
	statsMu sync.RWMutex
}

// WALConfig contains WAL configuration
type WALConfig struct {
	Dir               string
	MaxFileSize       int64
	SyncInterval      time.Duration
	EnableCompression bool
	EnableChecksum    bool
}

// WALEntry represents a WAL entry
type WALEntry struct {
	Type      WALEntryType
	Index     uint64
	Term      uint64
	Data      []byte
	Timestamp time.Time
	Checksum  uint32
}

// WALEntryType represents the type of WAL entry
type WALEntryType uint8

const (
	// WALEntryTypeLog log entry
	WALEntryTypeLog WALEntryType = 1
	// WALEntryTypeMeta metadata entry
	WALEntryTypeMeta WALEntryType = 2
	// WALEntryTypeSnapshot snapshot entry
	WALEntryTypeSnapshot WALEntryType = 3
	// WALEntryTypeCheckpoint checkpoint entry
	WALEntryTypeCheckpoint WALEntryType = 4
)

// WALStatistics contains WAL statistics
type WALStatistics struct {
	TotalWrites   int64
	TotalReads    int64
	BytesWritten  int64
	BytesRead     int64
	SyncCount     int64
	RotationCount int64
	LastSync      time.Time
	LastRotation  time.Time
}

// NewWAL creates a new WAL
func NewWAL(nodeID string, config WALConfig, logger forge.Logger) (*WAL, error) {
	// Set defaults
	if config.Dir == "" {
		config.Dir = filepath.Join("data", "wal")
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 100 * 1024 * 1024 // 100MB
	}
	if config.SyncInterval == 0 {
		config.SyncInterval = 100 * time.Millisecond
	}

	// Create directory
	if err := os.MkdirAll(config.Dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	wal := &WAL{
		nodeID: nodeID,
		logger: logger,
		dir:    config.Dir,
		config: config,
	}

	// Open current WAL file
	if err := wal.openFile(); err != nil {
		return nil, err
	}

	// Start background sync
	go wal.backgroundSync()

	logger.Info("WAL initialized",
		forge.F("dir", config.Dir),
		forge.F("max_file_size", config.MaxFileSize),
	)

	return wal, nil
}

// Write writes an entry to the WAL
func (w *WAL) Write(ctx context.Context, entry *WALEntry) error {
	w.fileMu.Lock()
	defer w.fileMu.Unlock()

	// Check if rotation needed
	if w.fileSize >= w.config.MaxFileSize {
		if err := w.rotate(); err != nil {
			return fmt.Errorf("failed to rotate WAL: %w", err)
		}
	}

	// Serialize entry
	data, err := w.serializeEntry(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Write to file
	n, err := w.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}

	w.fileSize += int64(n)

	// Update statistics
	w.statsMu.Lock()
	w.stats.TotalWrites++
	w.stats.BytesWritten += int64(n)
	w.statsMu.Unlock()

	w.logger.Debug("WAL entry written",
		forge.F("type", entry.Type),
		forge.F("index", entry.Index),
		forge.F("size", n),
	)

	return nil
}

// Sync syncs the WAL to disk
func (w *WAL) Sync() error {
	w.fileMu.Lock()
	defer w.fileMu.Unlock()

	if w.file == nil {
		return fmt.Errorf("WAL file not open")
	}

	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	w.statsMu.Lock()
	w.stats.SyncCount++
	w.stats.LastSync = time.Now()
	w.statsMu.Unlock()

	return nil
}

// Read reads entries from the WAL
func (w *WAL) Read(ctx context.Context, startIndex uint64) ([]*WALEntry, error) {
	// Get all WAL files
	files, err := w.getWALFiles()
	if err != nil {
		return nil, err
	}

	var entries []*WALEntry

	for _, filename := range files {
		fileEntries, err := w.readFile(filepath.Join(w.dir, filename))
		if err != nil {
			w.logger.Warn("failed to read WAL file",
				forge.F("file", filename),
				forge.F("error", err),
			)
			continue
		}

		for _, entry := range fileEntries {
			if entry.Index >= startIndex {
				entries = append(entries, entry)
			}
		}
	}

	return entries, nil
}

// openFile opens the current WAL file
func (w *WAL) openFile() error {
	filename := w.currentFilename()
	path := filepath.Join(w.dir, filename)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open WAL file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to stat WAL file: %w", err)
	}

	w.file = file
	w.fileSize = stat.Size()

	return nil
}

// rotate rotates the WAL file
func (w *WAL) rotate() error {
	// Close current file
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
	}

	// Open new file
	if err := w.openFile(); err != nil {
		return err
	}

	w.statsMu.Lock()
	w.stats.RotationCount++
	w.stats.LastRotation = time.Now()
	w.statsMu.Unlock()

	w.logger.Info("WAL rotated",
		forge.F("new_file", w.currentFilename()),
	)

	return nil
}

// currentFilename returns the current WAL filename
func (w *WAL) currentFilename() string {
	return fmt.Sprintf("wal-%s-%d.log", w.nodeID, time.Now().Unix())
}

// getWALFiles returns all WAL files sorted by timestamp
func (w *WAL) getWALFiles() ([]string, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".log" {
			files = append(files, entry.Name())
		}
	}

	return files, nil
}

// readFile reads entries from a WAL file
func (w *WAL) readFile(path string) ([]*WALEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []*WALEntry

	for {
		entry, err := w.deserializeEntry(file)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)

		w.statsMu.Lock()
		w.stats.TotalReads++
		w.statsMu.Unlock()
	}

	return entries, nil
}

// serializeEntry serializes a WAL entry
func (w *WAL) serializeEntry(entry *WALEntry) ([]byte, error) {
	// Format:
	// [4 bytes: total size]
	// [1 byte: entry type]
	// [8 bytes: index]
	// [8 bytes: term]
	// [4 bytes: data size]
	// [N bytes: data]
	// [4 bytes: checksum]

	dataSize := len(entry.Data)
	totalSize := 1 + 8 + 8 + 4 + dataSize + 4

	buf := make([]byte, 4+totalSize)

	// Write total size
	binary.BigEndian.PutUint32(buf[0:4], uint32(totalSize))

	// Write entry type
	buf[4] = byte(entry.Type)

	// Write index
	binary.BigEndian.PutUint64(buf[5:13], entry.Index)

	// Write term
	binary.BigEndian.PutUint64(buf[13:21], entry.Term)

	// Write data size
	binary.BigEndian.PutUint32(buf[21:25], uint32(dataSize))

	// Write data
	copy(buf[25:25+dataSize], entry.Data)

	// Calculate and write checksum
	if w.config.EnableChecksum {
		checksum := w.calculateChecksum(buf[4 : 25+dataSize])
		binary.BigEndian.PutUint32(buf[25+dataSize:], checksum)
	}

	return buf, nil
}

// deserializeEntry deserializes a WAL entry
func (w *WAL) deserializeEntry(r io.Reader) (*WALEntry, error) {
	// Read total size
	var sizeBytes [4]byte
	if _, err := io.ReadFull(r, sizeBytes[:]); err != nil {
		return nil, err
	}
	totalSize := binary.BigEndian.Uint32(sizeBytes[:])

	// Read entry data
	data := make([]byte, totalSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	entry := &WALEntry{
		Type:  WALEntryType(data[0]),
		Index: binary.BigEndian.Uint64(data[1:9]),
		Term:  binary.BigEndian.Uint64(data[9:17]),
	}

	dataSize := binary.BigEndian.Uint32(data[17:21])
	entry.Data = make([]byte, dataSize)
	copy(entry.Data, data[21:21+dataSize])

	// Verify checksum
	if w.config.EnableChecksum {
		storedChecksum := binary.BigEndian.Uint32(data[21+dataSize:])
		calculatedChecksum := w.calculateChecksum(data[:21+dataSize])
		if storedChecksum != calculatedChecksum {
			return nil, fmt.Errorf("checksum mismatch")
		}
	}

	w.statsMu.Lock()
	w.stats.BytesRead += int64(4 + totalSize)
	w.statsMu.Unlock()

	return entry, nil
}

// calculateChecksum calculates a simple checksum
func (w *WAL) calculateChecksum(data []byte) uint32 {
	var sum uint32
	for _, b := range data {
		sum += uint32(b)
	}
	return sum
}

// backgroundSync performs periodic syncs
func (w *WAL) backgroundSync() {
	ticker := time.NewTicker(w.config.SyncInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := w.Sync(); err != nil {
			w.logger.Warn("background sync failed",
				forge.F("error", err),
			)
		}
	}
}

// Truncate truncates the WAL up to the given index
func (w *WAL) Truncate(ctx context.Context, index uint64) error {
	w.logger.Info("truncating WAL",
		forge.F("up_to_index", index),
	)

	// In a real implementation, this would:
	// 1. Read all WAL files
	// 2. Delete files with all entries < index
	// 3. Rewrite partial files

	return nil
}

// Close closes the WAL
func (w *WAL) Close() error {
	w.fileMu.Lock()
	defer w.fileMu.Unlock()

	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			w.logger.Warn("failed to sync on close",
				forge.F("error", err),
			)
		}
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}

	w.logger.Info("WAL closed")
	return nil
}

// GetStatistics returns WAL statistics
func (w *WAL) GetStatistics() WALStatistics {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	return w.stats
}
