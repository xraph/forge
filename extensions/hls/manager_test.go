package hls

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/hls/storage"
	forgestorage "github.com/xraph/forge/extensions/storage"
)

// Mock storage for testing
type mockStorage struct{}

func (m *mockStorage) Upload(ctx context.Context, key string, data io.Reader, opts ...forgestorage.UploadOption) error {
	return nil
}

func (m *mockStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	return nil
}

func (m *mockStorage) List(ctx context.Context, prefix string, opts ...forgestorage.ListOption) ([]forgestorage.Object, error) {
	return nil, nil
}

func (m *mockStorage) Metadata(ctx context.Context, key string) (*forgestorage.ObjectMetadata, error) {
	return nil, nil
}

func (m *mockStorage) Exists(ctx context.Context, key string) (bool, error) {
	return true, nil
}

func (m *mockStorage) Copy(ctx context.Context, srcKey, dstKey string) error {
	return nil
}

func (m *mockStorage) Move(ctx context.Context, srcKey, dstKey string) error {
	return nil
}

func (m *mockStorage) PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", nil
}

func (m *mockStorage) PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", nil
}

func TestNewManager(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, err := NewManager(config, hlsStorage)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if manager == nil {
		t.Fatal("expected manager to be created")
	}
}

func TestCreateStream(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	opts := StreamOptions{
		Title:          "Test Stream",
		Description:    "Test Description",
		Type:           StreamTypeLive,
		TargetDuration: 6,
		DVRWindowSize:  10,
		TranscodeProfiles: []TranscodeProfile{
			Profile720p,
		},
	}

	stream, err := manager.CreateStream(ctx, opts)
	if err != nil {
		t.Fatalf("failed to create stream: %v", err)
	}

	if stream.ID == "" {
		t.Error("expected stream to have an ID")
	}

	if stream.Title != opts.Title {
		t.Errorf("expected title '%s', got '%s'", opts.Title, stream.Title)
	}

	if stream.Type != opts.Type {
		t.Errorf("expected type '%s', got '%s'", opts.Type, stream.Type)
	}

	if stream.Status != StreamStatusCreated {
		t.Errorf("expected status '%s', got '%s'", StreamStatusCreated, stream.Status)
	}

	if len(stream.Variants) != 1 {
		t.Errorf("expected 1 variant, got %d", len(stream.Variants))
	}
}

func TestGetStream(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	// Create a stream first
	opts := StreamOptions{
		Title: "Test Stream",
		Type:  StreamTypeLive,
	}
	created, _ := manager.CreateStream(ctx, opts)

	// Get the stream
	stream, err := manager.GetStream(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get stream: %v", err)
	}

	if stream.ID != created.ID {
		t.Errorf("expected stream ID '%s', got '%s'", created.ID, stream.ID)
	}
}

func TestListStreams(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	// Create multiple streams
	for i := 0; i < 3; i++ {
		opts := StreamOptions{
			Title: "Test Stream",
			Type:  StreamTypeLive,
		}
		manager.CreateStream(ctx, opts)
	}

	streams, err := manager.ListStreams(ctx)
	if err != nil {
		t.Fatalf("failed to list streams: %v", err)
	}

	if len(streams) != 3 {
		t.Errorf("expected 3 streams, got %d", len(streams))
	}
}

func TestDeleteStream(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	// Create a stream
	opts := StreamOptions{
		Title: "Test Stream",
		Type:  StreamTypeLive,
	}
	stream, _ := manager.CreateStream(ctx, opts)

	// Delete the stream
	err := manager.DeleteStream(ctx, stream.ID)
	if err != nil {
		t.Fatalf("failed to delete stream: %v", err)
	}

	// Verify it's deleted
	_, err = manager.GetStream(ctx, stream.ID)
	if err == nil {
		t.Error("expected error when getting deleted stream")
	}
}

func TestStartLiveStream(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	// Create a live stream
	opts := StreamOptions{
		Title: "Live Stream",
		Type:  StreamTypeLive,
	}
	stream, _ := manager.CreateStream(ctx, opts)

	// Start the stream
	err := manager.StartLiveStream(ctx, stream.ID)
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	// Verify status changed
	updated, _ := manager.GetStream(ctx, stream.ID)
	if updated.Status != StreamStatusActive {
		t.Errorf("expected status '%s', got '%s'", StreamStatusActive, updated.Status)
	}

	if updated.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestStopLiveStream(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	// Create and start a stream
	opts := StreamOptions{
		Title: "Live Stream",
		Type:  StreamTypeLive,
	}
	stream, _ := manager.CreateStream(ctx, opts)
	manager.StartLiveStream(ctx, stream.ID)

	// Stop the stream
	err := manager.StopLiveStream(ctx, stream.ID)
	if err != nil {
		t.Fatalf("failed to stop stream: %v", err)
	}

	// Verify status changed
	updated, _ := manager.GetStream(ctx, stream.ID)
	if updated.Status != StreamStatusStopped {
		t.Errorf("expected status '%s', got '%s'", StreamStatusStopped, updated.Status)
	}

	if updated.EndedAt == nil {
		t.Error("expected EndedAt to be set")
	}
}

func TestGetStreamStats(t *testing.T) {
	config := DefaultConfig()
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	// Create a stream
	opts := StreamOptions{
		Title: "Test Stream",
		Type:  StreamTypeLive,
	}
	stream, _ := manager.CreateStream(ctx, opts)

	// Get stats
	stats, err := manager.GetStreamStats(ctx, stream.ID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.StreamID != stream.ID {
		t.Errorf("expected stream ID '%s', got '%s'", stream.ID, stats.StreamID)
	}

	if stats.VariantStats == nil {
		t.Error("expected variant stats to be initialized")
	}
}

func TestMaxStreamsLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxStreams = 2
	hlsStorage := storage.NewHLSStorage(&mockStorage{}, "test")

	manager, _ := NewManager(config, hlsStorage)
	ctx := context.Background()

	opts := StreamOptions{
		Title: "Test Stream",
		Type:  StreamTypeLive,
	}

	// Create max streams
	for i := 0; i < config.MaxStreams; i++ {
		_, err := manager.CreateStream(ctx, opts)
		if err != nil {
			t.Fatalf("failed to create stream %d: %v", i, err)
		}
	}

	// Try to create one more (should fail)
	_, err := manager.CreateStream(ctx, opts)
	if err == nil {
		t.Error("expected error when exceeding max streams")
	}
}
