package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	forgestorage "github.com/xraph/forge/extensions/storage"
)

// HLSStorage wraps the forge storage extension for HLS-specific operations
type HLSStorage struct {
	storage forgestorage.Storage
	prefix  string // Base prefix for all HLS content
}

// NewHLSStorage creates a new HLS storage wrapper
func NewHLSStorage(storage forgestorage.Storage, prefix string) *HLSStorage {
	if prefix == "" {
		prefix = "hls"
	}
	return &HLSStorage{
		storage: storage,
		prefix:  prefix,
	}
}

// Segment operations

func (s *HLSStorage) SaveSegment(ctx context.Context, streamID, variantID string, segmentNum int, data []byte) error {
	key := s.segmentKey(streamID, variantID, segmentNum)
	return s.storage.Upload(ctx, key, bytes.NewReader(data),
		forgestorage.WithContentType("video/MP2T"),
	)
}

func (s *HLSStorage) GetSegment(ctx context.Context, streamID, variantID string, segmentNum int) ([]byte, error) {
	key := s.segmentKey(streamID, variantID, segmentNum)
	reader, err := s.storage.Download(ctx, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func (s *HLSStorage) GetSegmentStream(ctx context.Context, streamID, variantID string, segmentNum int) (io.ReadCloser, error) {
	key := s.segmentKey(streamID, variantID, segmentNum)
	return s.storage.Download(ctx, key)
}

func (s *HLSStorage) DeleteSegment(ctx context.Context, streamID, variantID string, segmentNum int) error {
	key := s.segmentKey(streamID, variantID, segmentNum)
	return s.storage.Delete(ctx, key)
}

func (s *HLSStorage) SegmentExists(ctx context.Context, streamID, variantID string, segmentNum int) (bool, error) {
	key := s.segmentKey(streamID, variantID, segmentNum)
	return s.storage.Exists(ctx, key)
}

// Playlist operations

func (s *HLSStorage) SavePlaylist(ctx context.Context, streamID, variantID, content string) error {
	key := s.playlistKey(streamID, variantID)
	return s.storage.Upload(ctx, key, strings.NewReader(content),
		forgestorage.WithContentType("application/vnd.apple.mpegurl"),
	)
}

func (s *HLSStorage) GetPlaylist(ctx context.Context, streamID, variantID string) (string, error) {
	key := s.playlistKey(streamID, variantID)
	reader, err := s.storage.Download(ctx, key)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *HLSStorage) DeletePlaylist(ctx context.Context, streamID, variantID string) error {
	key := s.playlistKey(streamID, variantID)
	return s.storage.Delete(ctx, key)
}

// Master playlist operations

func (s *HLSStorage) SaveMasterPlaylist(ctx context.Context, streamID, content string) error {
	key := s.masterPlaylistKey(streamID)
	return s.storage.Upload(ctx, key, strings.NewReader(content),
		forgestorage.WithContentType("application/vnd.apple.mpegurl"),
	)
}

func (s *HLSStorage) GetMasterPlaylist(ctx context.Context, streamID string) (string, error) {
	key := s.masterPlaylistKey(streamID)
	reader, err := s.storage.Download(ctx, key)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *HLSStorage) DeleteMasterPlaylist(ctx context.Context, streamID string) error {
	key := s.masterPlaylistKey(streamID)
	return s.storage.Delete(ctx, key)
}

// Stream metadata

func (s *HLSStorage) SaveStreamMetadata(ctx context.Context, streamID string, metadata map[string]interface{}) error {
	key := s.metadataKey(streamID)
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return s.storage.Upload(ctx, key, bytes.NewReader(data),
		forgestorage.WithContentType("application/json"),
	)
}

func (s *HLSStorage) GetStreamMetadata(ctx context.Context, streamID string) (map[string]interface{}, error) {
	key := s.metadataKey(streamID)
	reader, err := s.storage.Download(ctx, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}

func (s *HLSStorage) DeleteStreamMetadata(ctx context.Context, streamID string) error {
	key := s.metadataKey(streamID)
	return s.storage.Delete(ctx, key)
}

// Cleanup

func (s *HLSStorage) DeleteStream(ctx context.Context, streamID string) error {
	prefix := s.streamPrefix(streamID)

	// List all objects with this prefix
	objects, err := s.storage.List(ctx, prefix, forgestorage.WithRecursive(true))
	if err != nil {
		return fmt.Errorf("failed to list stream objects: %w", err)
	}

	// Delete all objects
	for _, obj := range objects {
		if err := s.storage.Delete(ctx, obj.Key); err != nil {
			// Log but continue
			continue
		}
	}

	return nil
}

func (s *HLSStorage) CleanupOldSegments(ctx context.Context, streamID string, keepLast int) error {
	// List all segments for this stream
	prefix := s.streamPrefix(streamID)
	objects, err := s.storage.List(ctx, prefix, forgestorage.WithRecursive(true))
	if err != nil {
		return fmt.Errorf("failed to list segments: %w", err)
	}

	// Filter for .ts files only
	segments := make([]forgestorage.Object, 0)
	for _, obj := range objects {
		if strings.HasSuffix(obj.Key, ".ts") {
			segments = append(segments, obj)
		}
	}

	// Sort by modification time and delete old ones
	// Simplified: delete if more than keepLast
	if len(segments) > keepLast {
		toDelete := len(segments) - keepLast
		for i := 0; i < toDelete; i++ {
			if err := s.storage.Delete(ctx, segments[i].Key); err != nil {
				// Log but continue
				continue
			}
		}
	}

	return nil
}

// Health and stats

func (s *HLSStorage) Healthy(ctx context.Context) error {
	// Check if we can perform a basic operation
	_, err := s.storage.List(ctx, s.prefix, forgestorage.WithLimit(1))
	return err
}

func (s *HLSStorage) GetStorageStats(ctx context.Context) (*StorageStats, error) {
	// List all objects under HLS prefix
	objects, err := s.storage.List(ctx, s.prefix, forgestorage.WithRecursive(true))
	if err != nil {
		return nil, err
	}

	stats := &StorageStats{}
	streams := make(map[string]bool)

	for _, obj := range objects {
		if strings.HasSuffix(obj.Key, ".ts") {
			stats.TotalSegments++
		}
		stats.TotalSize += obj.Size

		// Extract stream ID from path
		parts := strings.Split(obj.Key, "/")
		if len(parts) >= 2 {
			streamID := parts[1]
			streams[streamID] = true
		}
	}

	stats.TotalStreams = int64(len(streams))

	return stats, nil
}

// Key generation helpers

func (s *HLSStorage) segmentKey(streamID, variantID string, segmentNum int) string {
	return path.Join(s.prefix, streamID, variantID, fmt.Sprintf("segment_%d.ts", segmentNum))
}

func (s *HLSStorage) playlistKey(streamID, variantID string) string {
	return path.Join(s.prefix, streamID, variantID, "playlist.m3u8")
}

func (s *HLSStorage) masterPlaylistKey(streamID string) string {
	return path.Join(s.prefix, streamID, "master.m3u8")
}

func (s *HLSStorage) metadataKey(streamID string) string {
	return path.Join(s.prefix, streamID, "metadata.json")
}

func (s *HLSStorage) streamPrefix(streamID string) string {
	return path.Join(s.prefix, streamID) + "/"
}

// StorageStats contains storage backend statistics
type StorageStats struct {
	TotalStreams   int64
	TotalSegments  int64
	TotalSize      int64
	AvailableSpace int64
}
