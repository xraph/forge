package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// HLSStorage maps HLS concepts (streams, variants, segments, playlists) onto
// flat object keys. It holds a Backend rather than any particular storage
// library, so the two are replaceable independently.
type HLSStorage struct {
	storage Backend
	prefix  string // Base prefix for all HLS content
}

// NewHLSStorage creates a new HLS storage wrapper
func NewHLSStorage(storage Backend, prefix string) *HLSStorage {
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
	return s.storage.Put(ctx, key, bytes.NewReader(data), "video/MP2T")
}

func (s *HLSStorage) GetSegment(ctx context.Context, streamID, variantID string, segmentNum int) ([]byte, error) {
	key := s.segmentKey(streamID, variantID, segmentNum)
	reader, err := s.storage.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func (s *HLSStorage) GetSegmentStream(ctx context.Context, streamID, variantID string, segmentNum int) (io.ReadCloser, error) {
	key := s.segmentKey(streamID, variantID, segmentNum)
	return s.storage.Get(ctx, key)
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
	return s.storage.Put(ctx, key, strings.NewReader(content), "application/vnd.apple.mpegurl")
}

func (s *HLSStorage) GetPlaylist(ctx context.Context, streamID, variantID string) (string, error) {
	key := s.playlistKey(streamID, variantID)
	reader, err := s.storage.Get(ctx, key)
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
	return s.storage.Put(ctx, key, strings.NewReader(content), "application/vnd.apple.mpegurl")
}

func (s *HLSStorage) GetMasterPlaylist(ctx context.Context, streamID string) (string, error) {
	key := s.masterPlaylistKey(streamID)
	reader, err := s.storage.Get(ctx, key)
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

	return s.storage.Put(ctx, key, bytes.NewReader(data), "application/json")
}

func (s *HLSStorage) GetStreamMetadata(ctx context.Context, streamID string) (map[string]interface{}, error) {
	key := s.metadataKey(streamID)
	reader, err := s.storage.Get(ctx, key)
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
	objects, err := s.storage.List(ctx, prefix)
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
	objects, err := s.storage.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("failed to list segments: %w", err)
	}

	if keepLast < 0 {
		keepLast = 0
	}

	// Group by variant. keepLast is a per-variant window everywhere else in
	// this extension: the tracker trims per variant and a media playlist
	// advertises the last N segments of one variant. Spending a single budget
	// across every variant deletes segments the playlists still reference.
	byVariant := make(map[string][]segmentRef)
	for _, obj := range objects {
		ref, ok := s.parseSegmentKey(streamID, obj.Key)
		if !ok {
			// Playlists, metadata, and anything this package did not write.
			// Deleting a file we cannot order is worse than keeping it.
			continue
		}

		byVariant[ref.variantID] = append(byVariant[ref.variantID], ref)
	}

	for _, segments := range byVariant {
		// Order by sequence number rather than by the order the backend
		// listed them in. Listing is lexicographic, which puts segment_10
		// ahead of segment_2 and would delete the newest segments first.
		// Numbers also beat modification time, which drifts between nodes
		// when several of them are writing the same stream.
		sort.Slice(segments, func(i, j int) bool {
			return segments[i].number < segments[j].number
		})

		if len(segments) <= keepLast {
			continue
		}

		for _, seg := range segments[:len(segments)-keepLast] {
			if err := s.storage.Delete(ctx, seg.key); err != nil {
				// Best effort. A segment that will not delete now comes back
				// round on the next cleanup pass.
				continue
			}
		}
	}

	return nil
}

// segmentRef is a segment key with its variant and sequence number recovered.
type segmentRef struct {
	key       string
	variantID string
	number    int
}

// parseSegmentKey recovers the variant and sequence number from a key that
// segmentKey produced. It reports false for everything else under the stream
// prefix, so cleanup only ever removes keys this package generated.
func (s *HLSStorage) parseSegmentKey(streamID, key string) (segmentRef, bool) {
	rest, ok := strings.CutPrefix(key, s.streamPrefix(streamID))
	if !ok {
		return segmentRef{}, false
	}

	variantID, file, ok := strings.Cut(rest, "/")
	if !ok || variantID == "" || strings.Contains(file, "/") {
		return segmentRef{}, false
	}

	digits, ok := strings.CutPrefix(file, "segment_")
	if !ok {
		return segmentRef{}, false
	}

	digits, ok = strings.CutSuffix(digits, ".ts")
	if !ok {
		return segmentRef{}, false
	}

	number, err := strconv.Atoi(digits)
	if err != nil {
		return segmentRef{}, false
	}

	// Atoi accepts forms segmentKey never emits ("+7", "-0", "007"). Round
	// tripping rejects them, so an unfamiliar key stays unfamiliar.
	if strconv.Itoa(number) != digits {
		return segmentRef{}, false
	}

	return segmentRef{key: key, variantID: variantID, number: number}, true
}

// Health and stats

func (s *HLSStorage) Healthy(ctx context.Context) error {
	return s.storage.Health(ctx)
}

func (s *HLSStorage) GetStorageStats(ctx context.Context) (*StorageStats, error) {
	// List all objects under HLS prefix
	objects, err := s.storage.List(ctx, s.prefix)
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
