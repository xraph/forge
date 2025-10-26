package webrtc

import (
	"context"
	"fmt"
	"testing"

	"github.com/xraph/forge"
)

func TestLocalTrack(t *testing.T) {
	logger := forge.NewNoopLogger()

	// Test audio track creation
	audioTrack, err := NewLocalTrack(TrackKindAudio, "audio-1", "microphone", logger)
	if err != nil {
		t.Fatalf("failed to create audio track: %v", err)
	}

	if audioTrack.ID() != "audio-1" {
		t.Errorf("expected track ID 'audio-1', got %s", audioTrack.ID())
	}

	if audioTrack.Kind() != TrackKindAudio {
		t.Errorf("expected audio kind, got %s", audioTrack.Kind())
	}

	if audioTrack.Label() == "" {
		t.Error("track label should not be empty")
	}

	// Test video track creation
	videoTrack, err := NewLocalTrack(TrackKindVideo, "video-1", "camera", logger)
	if err != nil {
		t.Fatalf("failed to create video track: %v", err)
	}

	if videoTrack.ID() != "video-1" {
		t.Errorf("expected track ID 'video-1', got %s", videoTrack.ID())
	}

	if videoTrack.Kind() != TrackKindVideo {
		t.Errorf("expected video kind, got %s", videoTrack.Kind())
	}

	// Test invalid track kind
	_, err = NewLocalTrack("invalid", "test", "test", logger)
	if err == nil {
		t.Error("should fail with invalid track kind")
	}

	// Test track settings
	audioSettings := audioTrack.GetSettings()
	if audioSettings.SampleRate == 0 {
		t.Error("audio settings should have sample rate")
	}

	videoSettings := videoTrack.GetSettings()
	if videoSettings.Width == 0 || videoSettings.Height == 0 {
		t.Error("video settings should have dimensions")
	}

	// Test track enable/disable
	if !audioTrack.Enabled() {
		t.Error("track should be enabled by default")
	}

	audioTrack.SetEnabled(false)
	if audioTrack.Enabled() {
		t.Error("track should be disabled after SetEnabled(false)")
	}

	audioTrack.SetEnabled(true)
	if !audioTrack.Enabled() {
		t.Error("track should be enabled after SetEnabled(true)")
	}

	// Test track stats
	ctx := context.Background()
	stats, err := audioTrack.GetStats(ctx)
	if err != nil {
		t.Errorf("failed to get track stats: %v", err)
	}

	if stats.TrackID != "audio-1" {
		t.Errorf("expected track ID 'audio-1', got %s", stats.TrackID)
	}

	if stats.Kind != TrackKindAudio {
		t.Errorf("expected audio kind in stats, got %s", stats.Kind)
	}

	// Test close
	err = audioTrack.Close()
	if err != nil {
		t.Errorf("failed to close track: %v", err)
	}

	// Test double close
	err = audioTrack.Close()
	if err != nil {
		t.Errorf("double close should not error: %v", err)
	}
}

func TestRemoteTrack(t *testing.T) {
	logger := forge.NewNoopLogger()

	// Create a mock remote track (we can't easily create a real pion remote track in tests)
	// So we'll test the interface implementation

	// Test that we can call all methods without panicking
	remoteTrack := &remoteMediaTrack{
		track:   nil, // Would be a real pion track in real usage
		enabled: true,
		logger:  logger,
	}

	// These methods should not panic even with nil track
	_ = remoteTrack.ID()
	_ = remoteTrack.Kind()
	_ = remoteTrack.Label()
	_ = remoteTrack.Enabled()
	remoteTrack.SetEnabled(false)
	_ = remoteTrack.GetSettings()

	ctx := context.Background()
	stats, err := remoteTrack.GetStats(ctx)
	if err != nil {
		t.Errorf("failed to get remote track stats: %v", err)
	}

	if stats.TrackID == "" {
		t.Error("remote track should have an ID")
	}

	err = remoteTrack.Close()
	if err != nil {
		t.Errorf("failed to close remote track: %v", err)
	}
}

func TestTrackSettings(t *testing.T) {
	logger := forge.NewNoopLogger()

	// Test audio track settings
	audioTrack, err := NewLocalTrack(TrackKindAudio, "audio-settings", "mic", logger)
	if err != nil {
		t.Fatalf("failed to create audio track: %v", err)
	}

	settings := audioTrack.GetSettings()
	if settings.Channels == 0 {
		t.Error("audio settings should have channels")
	}

	if settings.SampleRate == 0 {
		t.Error("audio settings should have sample rate")
	}

	// Test video track settings
	videoTrack, err := NewLocalTrack(TrackKindVideo, "video-settings", "cam", logger)
	if err != nil {
		t.Fatalf("failed to create video track: %v", err)
	}

	videoSettings := videoTrack.GetSettings()
	if videoSettings.Width == 0 || videoSettings.Height == 0 {
		t.Error("video settings should have dimensions")
	}

	if videoSettings.FrameRate == 0 {
		t.Error("video settings should have frame rate")
	}

	if videoSettings.Bitrate == 0 {
		t.Error("video settings should have bitrate")
	}
}

func TestTrackStats(t *testing.T) {
	logger := forge.NewNoopLogger()
	ctx := context.Background()

	// Test audio track stats
	audioTrack, err := NewLocalTrack(TrackKindAudio, "audio-stats", "mic", logger)
	if err != nil {
		t.Fatalf("failed to create audio track: %v", err)
	}

	stats, err := audioTrack.GetStats(ctx)
	if err != nil {
		t.Errorf("failed to get audio track stats: %v", err)
	}

	if stats.Kind != TrackKindAudio {
		t.Errorf("expected audio kind in stats, got %s", stats.Kind)
	}

	// Test video track stats
	videoTrack, err := NewLocalTrack(TrackKindVideo, "video-stats", "cam", logger)
	if err != nil {
		t.Fatalf("failed to create video track: %v", err)
	}

	videoStats, err := videoTrack.GetStats(ctx)
	if err != nil {
		t.Errorf("failed to get video track stats: %v", err)
	}

	if videoStats.Kind != TrackKindVideo {
		t.Errorf("expected video kind in stats, got %s", videoStats.Kind)
	}
}

func BenchmarkTrackCreation(b *testing.B) {
	logger := forge.NewNoopLogger()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trackID := fmt.Sprintf("bench-track-%d", i)
		_, err := NewLocalTrack(TrackKindAudio, trackID, "benchmark-mic", logger)
		if err != nil {
			b.Fatalf("failed to create track in benchmark: %v", err)
		}
	}
}

func BenchmarkTrackStats(b *testing.B) {
	logger := forge.NewNoopLogger()
	track, err := NewLocalTrack(TrackKindVideo, "bench-stats", "benchmark-cam", logger)
	if err != nil {
		b.Fatalf("failed to create track: %v", err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := track.GetStats(ctx)
		if err != nil {
			b.Fatalf("failed to get stats in benchmark: %v", err)
		}
	}
}
