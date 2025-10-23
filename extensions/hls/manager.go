package hls

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge/extensions/hls/internal"
	"github.com/xraph/forge/extensions/hls/segmenter"
	"github.com/xraph/forge/extensions/hls/storage"
	"github.com/xraph/forge/extensions/hls/transcoder"
)

// Manager implements the HLS interface
type Manager struct {
	config     Config
	storage    *storage.HLSStorage
	generator  *PlaylistGenerator
	segmenter  *segmenter.Segmenter
	transcoder *transcoder.Transcoder

	streams   map[string]*Stream
	streamsMu sync.RWMutex

	// Segment tracking
	trackers   map[string]*internal.SegmentTracker
	trackersMu sync.RWMutex

	// Stats
	stats   map[string]*StreamStats
	statsMu sync.RWMutex

	// Cleanup
	cleanupStop chan struct{}
	cleanupDone chan struct{}
}

// NewManager creates a new HLS manager
func NewManager(config Config, store *storage.HLSStorage) (*Manager, error) {
	generator := NewPlaylistGenerator(config.BaseURL)

	// Create segmenter
	seg := segmenter.NewSegmenter(segmenter.Config{
		FFmpegPath:     config.FFmpegPath,
		TargetDuration: config.TargetDuration,
	})

	// Create transcoder
	trans := transcoder.NewTranscoder(transcoder.Config{
		FFmpegPath:          config.FFmpegPath,
		FFprobePath:         config.FFprobePath,
		MaxConcurrentJobs:   config.MaxConcurrentTranscodes,
		EnableHardwareAccel: false, // Can be made configurable
	})

	m := &Manager{
		config:      config,
		storage:     store,
		generator:   generator,
		segmenter:   seg,
		transcoder:  trans,
		streams:     make(map[string]*Stream),
		trackers:    make(map[string]*internal.SegmentTracker),
		stats:       make(map[string]*StreamStats),
		cleanupStop: make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}

	// Start cleanup routine if enabled
	if config.EnableAutoCleanup {
		go m.cleanupRoutine()
	}

	return m, nil
}

// CreateStream creates a new HLS stream
func (m *Manager) CreateStream(ctx context.Context, opts StreamOptions) (*Stream, error) {
	// Check stream limit
	m.streamsMu.RLock()
	if len(m.streams) >= m.config.MaxStreams {
		m.streamsMu.RUnlock()
		return nil, fmt.Errorf("maximum number of streams reached")
	}
	m.streamsMu.RUnlock()

	// Create stream
	stream := &Stream{
		ID:             uuid.New().String(),
		Type:           opts.Type,
		Status:         StreamStatusCreated,
		Title:          opts.Title,
		Description:    opts.Description,
		TargetDuration: opts.TargetDuration,
		DVRWindowSize:  opts.DVRWindowSize,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		Variants:       make([]*Variant, 0),
	}

	// Create variants based on transcode profiles
	for _, profile := range opts.TranscodeProfiles {
		variant := &Variant{
			ID:         uuid.New().String(),
			Bandwidth:  profile.Bitrate,
			Resolution: FormatResolution(profile.Width, profile.Height),
			Codecs:     GenerateCodecsString(profile.VideoCodec, profile.AudioCodec),
			FrameRate:  profile.FrameRate,
		}
		stream.Variants = append(stream.Variants, variant)
	}

	// Store stream
	m.streamsMu.Lock()
	m.streams[stream.ID] = stream
	m.streamsMu.Unlock()

	// Initialize tracker for live streams
	if stream.Type == StreamTypeLive || stream.Type == StreamTypeEvent {
		m.trackersMu.Lock()
		m.trackers[stream.ID] = internal.NewSegmentTracker(stream.ID)
		m.trackersMu.Unlock()
	}

	// Initialize stats
	m.statsMu.Lock()
	m.stats[stream.ID] = &StreamStats{
		StreamID:     stream.ID,
		VariantStats: make(map[string]*VariantStats),
	}
	m.statsMu.Unlock()

	// Save metadata
	metadata := map[string]interface{}{
		"id":          stream.ID,
		"type":        string(stream.Type),
		"status":      string(stream.Status),
		"title":       stream.Title,
		"description": stream.Description,
		"created_at":  stream.CreatedAt,
	}
	if err := m.storage.SaveStreamMetadata(ctx, stream.ID, metadata); err != nil {
		return nil, fmt.Errorf("failed to save stream metadata: %w", err)
	}

	return stream, nil
}

// GetStream retrieves a stream by ID
func (m *Manager) GetStream(ctx context.Context, streamID string) (*Stream, error) {
	m.streamsMu.RLock()
	defer m.streamsMu.RUnlock()

	stream, exists := m.streams[streamID]
	if !exists {
		return nil, fmt.Errorf("stream not found")
	}

	return stream, nil
}

// DeleteStream deletes a stream
func (m *Manager) DeleteStream(ctx context.Context, streamID string) error {
	m.streamsMu.Lock()
	delete(m.streams, streamID)
	m.streamsMu.Unlock()

	m.statsMu.Lock()
	delete(m.stats, streamID)
	m.statsMu.Unlock()

	// Clear tracker
	m.trackersMu.Lock()
	if tracker, exists := m.trackers[streamID]; exists {
		tracker.ClearAll()
		delete(m.trackers, streamID)
	}
	m.trackersMu.Unlock()

	// Delete from storage
	if err := m.storage.DeleteStream(ctx, streamID); err != nil {
		return fmt.Errorf("failed to delete stream from storage: %w", err)
	}

	return nil
}

// ListStreams lists all streams
func (m *Manager) ListStreams(ctx context.Context) ([]*Stream, error) {
	m.streamsMu.RLock()
	defer m.streamsMu.RUnlock()

	streams := make([]*Stream, 0, len(m.streams))
	for _, stream := range m.streams {
		streams = append(streams, stream)
	}

	return streams, nil
}

// StartLiveStream starts a live stream
func (m *Manager) StartLiveStream(ctx context.Context, streamID string) error {
	stream, err := m.GetStream(ctx, streamID)
	if err != nil {
		return err
	}

	if stream.Type != StreamTypeLive && stream.Type != StreamTypeEvent {
		return fmt.Errorf("stream is not a live stream")
	}

	m.streamsMu.Lock()
	stream.Status = StreamStatusActive
	now := time.Now()
	stream.StartedAt = &now
	stream.UpdatedAt = now
	m.streamsMu.Unlock()

	return nil
}

// StopLiveStream stops a live stream
func (m *Manager) StopLiveStream(ctx context.Context, streamID string) error {
	stream, err := m.GetStream(ctx, streamID)
	if err != nil {
		return err
	}

	m.streamsMu.Lock()
	stream.Status = StreamStatusStopped
	now := time.Now()
	stream.EndedAt = &now
	stream.UpdatedAt = now
	m.streamsMu.Unlock()

	return nil
}

// IngestSegment ingests a new segment for a live stream
func (m *Manager) IngestSegment(ctx context.Context, streamID string, segment *Segment) error {
	stream, err := m.GetStream(ctx, streamID)
	if err != nil {
		return err
	}

	if stream.Status != StreamStatusActive {
		return fmt.Errorf("stream is not active")
	}

	// Save segment to storage
	if err := m.storage.SaveSegment(ctx, streamID, segment.VariantID, segment.SequenceNum, segment.Data); err != nil {
		return fmt.Errorf("failed to save segment: %w", err)
	}

	// Update tracker
	m.trackersMu.RLock()
	if tracker, exists := m.trackers[streamID]; exists {
		tracker.AddSegment(segment.VariantID, segment.SequenceNum, segment.Duration, int64(len(segment.Data)))
	}
	m.trackersMu.RUnlock()

	// Update variant info
	m.streamsMu.Lock()
	for _, variant := range stream.Variants {
		if variant.ID == segment.VariantID {
			variant.SegmentCount++
			variant.LastSegment = segment.SequenceNum
			break
		}
	}
	stream.UpdatedAt = time.Now()
	m.streamsMu.Unlock()

	// Update stats
	m.statsMu.Lock()
	if stats, exists := m.stats[streamID]; exists {
		stats.SegmentsServed++
		if variantStats, exists := stats.VariantStats[segment.VariantID]; exists {
			variantStats.BytesServed += int64(len(segment.Data))
		}
	}
	m.statsMu.Unlock()

	// Cleanup old segments if DVR window is set
	if m.config.DVRWindowSize > 0 {
		m.trackersMu.RLock()
		if tracker, exists := m.trackers[streamID]; exists {
			removed := tracker.RemoveOldSegments(segment.VariantID, m.config.DVRWindowSize)
			// Delete removed segments from storage
			for _, segNum := range removed {
				m.storage.DeleteSegment(ctx, streamID, segment.VariantID, segNum)
			}
		}
		m.trackersMu.RUnlock()
	}

	return nil
}

// CreateVOD creates a VOD stream from a source file
func (m *Manager) CreateVOD(ctx context.Context, source io.Reader, opts VODOptions) (*Stream, error) {
	// For VOD from a reader, we need to save it to a temp file first
	// In production, you'd handle this more efficiently
	streamOpts := StreamOptions{
		Title:             opts.Title,
		Description:       opts.Description,
		Type:              StreamTypeVOD,
		TargetDuration:    opts.TargetDuration,
		TranscodeProfiles: opts.TranscodeProfiles,
	}

	stream, err := m.CreateStream(ctx, streamOpts)
	if err != nil {
		return nil, err
	}

	// Note: Actual VOD creation would:
	// 1. Save source to temporary location
	// 2. Transcode to each profile if transcoding is enabled
	// 3. Segment each transcoded version
	// 4. Upload segments to storage
	// 5. Generate playlists
	// This is a placeholder that would be completed based on requirements

	return stream, nil
}

// CreateVODFromFile creates a VOD stream from a file path
func (m *Manager) CreateVODFromFile(ctx context.Context, sourcePath string, opts VODOptions) (*Stream, error) {
	// Create stream
	streamOpts := StreamOptions{
		Title:             opts.Title,
		Description:       opts.Description,
		Type:              StreamTypeVOD,
		TargetDuration:    opts.TargetDuration,
		TranscodeProfiles: opts.TranscodeProfiles,
	}

	stream, err := m.CreateStream(ctx, streamOpts)
	if err != nil {
		return nil, err
	}

	// If transcoding is enabled and profiles are specified
	if m.config.EnableTranscoding && len(opts.TranscodeProfiles) > 0 {
		// Transcode to each profile
		for _, profile := range opts.TranscodeProfiles {
			transProfile := transcoder.Profile{
				Name:       profile.Name,
				Width:      profile.Width,
				Height:     profile.Height,
				Bitrate:    profile.Bitrate,
				FrameRate:  profile.FrameRate,
				VideoCodec: profile.VideoCodec,
				AudioCodec: profile.AudioCodec,
				Preset:     profile.Preset,
			}

			// Start transcoding job
			variantID := uuid.New().String()
			jobID := fmt.Sprintf("%s_%s", stream.ID, variantID)
			outputPath := fmt.Sprintf("/tmp/hls_%s_%s.mp4", stream.ID, variantID)

			_, err := m.transcoder.StartJob(ctx, jobID, sourcePath, outputPath, transProfile)
			if err != nil {
				return nil, fmt.Errorf("failed to start transcoding job: %w", err)
			}

			// Note: In production, you'd wait for jobs to complete,
			// then segment and upload. This is async.
		}
	} else {
		// No transcoding, just segment the source
		// This would use m.segmenter.SegmentFile()
	}

	return stream, nil
}

// TranscodeVideo transcodes a video to multiple profiles
func (m *Manager) TranscodeVideo(ctx context.Context, streamID string, profiles []TranscodeProfile) error {
	stream, err := m.GetStream(ctx, streamID)
	if err != nil {
		return err
	}

	// For each profile, start a transcoding job
	for _, profile := range profiles {
		transProfile := transcoder.Profile{
			Name:       profile.Name,
			Width:      profile.Width,
			Height:     profile.Height,
			Bitrate:    profile.Bitrate,
			FrameRate:  profile.FrameRate,
			VideoCodec: profile.VideoCodec,
			AudioCodec: profile.AudioCodec,
			Preset:     profile.Preset,
		}

		// Create variant
		variant := &Variant{
			ID:         uuid.New().String(),
			Bandwidth:  profile.Bitrate,
			Resolution: FormatResolution(profile.Width, profile.Height),
			Codecs:     GenerateCodecsString(profile.VideoCodec, profile.AudioCodec),
			FrameRate:  profile.FrameRate,
		}

		m.streamsMu.Lock()
		stream.Variants = append(stream.Variants, variant)
		m.streamsMu.Unlock()

		// Start transcoding job
		jobID := fmt.Sprintf("%s_%s", streamID, variant.ID)
		sourcePath := fmt.Sprintf("/tmp/hls_source_%s.mp4", streamID) // Placeholder
		outputPath := fmt.Sprintf("/tmp/hls_%s_%s.mp4", streamID, variant.ID)

		_, err := m.transcoder.StartJob(ctx, jobID, sourcePath, outputPath, transProfile)
		if err != nil {
			return fmt.Errorf("failed to start transcoding job for profile %s: %w", profile.Name, err)
		}
	}

	return nil
}

// GetMasterPlaylist generates and returns the master playlist
func (m *Manager) GetMasterPlaylist(ctx context.Context, streamID string) (*MasterPlaylist, error) {
	stream, err := m.GetStream(ctx, streamID)
	if err != nil {
		return nil, err
	}

	// Generate playlist content
	content := m.generator.GenerateMasterPlaylist(stream)

	// Save to storage
	if err := m.storage.SaveMasterPlaylist(ctx, streamID, content); err != nil {
		return nil, fmt.Errorf("failed to save master playlist: %w", err)
	}

	// Build variant info
	variants := make([]*VariantInfo, len(stream.Variants))
	for i, v := range stream.Variants {
		variants[i] = &VariantInfo{
			Bandwidth:  v.Bandwidth,
			Resolution: v.Resolution,
			Codecs:     v.Codecs,
			FrameRate:  v.FrameRate,
			URI:        fmt.Sprintf("%s/%s/variants/%s/playlist.m3u8", m.config.BaseURL, streamID, v.ID),
		}
	}

	return &MasterPlaylist{
		StreamID: streamID,
		Variants: variants,
		Content:  content,
	}, nil
}

// GetMediaPlaylist generates and returns a media playlist for a variant
func (m *Manager) GetMediaPlaylist(ctx context.Context, streamID string, variantID string) (*MediaPlaylist, error) {
	stream, err := m.GetStream(ctx, streamID)
	if err != nil {
		return nil, err
	}

	// Find variant
	var variant *Variant
	for _, v := range stream.Variants {
		if v.ID == variantID {
			variant = v
			break
		}
	}
	if variant == nil {
		return nil, fmt.Errorf("variant not found")
	}

	// Get segments from tracker
	var segments []*SegmentInfo

	m.trackersMu.RLock()
	if tracker, exists := m.trackers[streamID]; exists {
		// For live streams, get DVR window
		if stream.Type == StreamTypeLive || stream.Type == StreamTypeEvent {
			entries := tracker.GetSegmentsWindow(variantID, stream.DVRWindowSize)
			segments = make([]*SegmentInfo, len(entries))
			for i, entry := range entries {
				segments[i] = &SegmentInfo{
					Index:    entry.Index,
					Duration: entry.Duration,
					URI:      fmt.Sprintf("%s/%s/variants/%s/segment_%d.ts", m.config.BaseURL, streamID, variantID, entry.Index),
				}
			}
		} else {
			// For VOD, get all segments
			entries := tracker.GetSegments(variantID)
			segments = make([]*SegmentInfo, len(entries))
			for i, entry := range entries {
				segments[i] = &SegmentInfo{
					Index:    entry.Index,
					Duration: entry.Duration,
					URI:      fmt.Sprintf("%s/%s/variants/%s/segment_%d.ts", m.config.BaseURL, streamID, variantID, entry.Index),
				}
			}
		}
	}
	m.trackersMu.RUnlock()

	// Fallback if no tracker (e.g., VOD without tracking)
	if len(segments) == 0 {
		segments = make([]*SegmentInfo, 0)
	}

	// Generate playlist content
	var content string
	ended := stream.Status == StreamStatusStopped || stream.Status == StreamStatusEnded

	if stream.Type == StreamTypeLive {
		content = m.generator.GenerateLiveMediaPlaylist(stream, variant, segments)
	} else {
		content = m.generator.GenerateMediaPlaylist(stream, variant, segments, ended)
	}

	// Save to storage
	if err := m.storage.SavePlaylist(ctx, streamID, variantID, content); err != nil {
		return nil, fmt.Errorf("failed to save playlist: %w", err)
	}

	return &MediaPlaylist{
		StreamID:       streamID,
		VariantID:      variantID,
		TargetDuration: stream.TargetDuration,
		Segments:       segments,
		Ended:          ended,
		Content:        content,
	}, nil
}

// GetSegment retrieves a segment with streaming
func (m *Manager) GetSegment(ctx context.Context, streamID, variantID string, segmentNum int) (io.ReadCloser, error) {
	// Verify stream exists
	stream, err := m.GetStream(ctx, streamID)
	if err != nil {
		return nil, err
	}

	// Verify variant exists
	variantExists := false
	for _, v := range stream.Variants {
		if v.ID == variantID {
			variantExists = true
			break
		}
	}
	if !variantExists {
		return nil, fmt.Errorf("variant not found")
	}

	// Get segment from storage with streaming
	reader, err := m.storage.GetSegmentStream(ctx, streamID, variantID, segmentNum)
	if err != nil {
		return nil, fmt.Errorf("failed to get segment: %w", err)
	}

	// Update stats
	m.statsMu.Lock()
	if stats, exists := m.stats[streamID]; exists {
		stats.SegmentsServed++
		if variantStats, exists := stats.VariantStats[variantID]; exists {
			variantStats.RequestCount++
		} else {
			stats.VariantStats[variantID] = &VariantStats{
				VariantID:    variantID,
				RequestCount: 1,
			}
		}
	}
	m.statsMu.Unlock()

	return reader, nil
}

// GetSegmentURL returns the URL for a segment
func (m *Manager) GetSegmentURL(ctx context.Context, streamID, variantID string, segmentNum int) (string, error) {
	return fmt.Sprintf("%s/%s/variants/%s/segment_%d.ts", m.config.BaseURL, streamID, variantID, segmentNum), nil
}

// GetStreamStats returns statistics for a stream
func (m *Manager) GetStreamStats(ctx context.Context, streamID string) (*StreamStats, error) {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()

	stats, exists := m.stats[streamID]
	if !exists {
		return nil, fmt.Errorf("stats not found")
	}

	return stats, nil
}

// GetActiveViewers returns the number of active viewers for a stream
func (m *Manager) GetActiveViewers(ctx context.Context, streamID string) (int, error) {
	stats, err := m.GetStreamStats(ctx, streamID)
	if err != nil {
		return 0, err
	}

	return stats.CurrentViewers, nil
}

// cleanupRoutine periodically cleans up old segments
func (m *Manager) cleanupRoutine() {
	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()
	defer close(m.cleanupDone)

	for {
		select {
		case <-ticker.C:
			m.performCleanup()
		case <-m.cleanupStop:
			return
		}
	}
}

// performCleanup performs cleanup of old segments
func (m *Manager) performCleanup() {
	ctx := context.Background()

	m.streamsMu.RLock()
	streamIDs := make([]string, 0, len(m.streams))
	for id := range m.streams {
		streamIDs = append(streamIDs, id)
	}
	m.streamsMu.RUnlock()

	for _, streamID := range streamIDs {
		// Clean up old segments for each stream
		if err := m.storage.CleanupOldSegments(ctx, streamID, m.config.DVRWindowSize); err != nil {
			// Log error but continue
			continue
		}
	}
}

// Stop stops the manager
func (m *Manager) Stop() {
	if m.config.EnableAutoCleanup {
		close(m.cleanupStop)
		<-m.cleanupDone
	}

	// Stop transcoder
	if m.transcoder != nil {
		m.transcoder.Stop()
	}
}
