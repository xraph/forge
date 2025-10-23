package webrtc

import (
	"context"
	"testing"
)

func TestNewSFURouter(t *testing.T) {
	logger := &nullLogger{}

	router := NewSFURouter("test-room", logger, nil)
	if router == nil {
		t.Fatal("router is nil")
	}
}

func TestSFURouter_AddPublisher(t *testing.T) {
	logger := &nullLogger{}
	router := NewSFURouter("test-room", logger, nil)
	ctx := context.Background()

	config := DefaultConfig()
	peer, err := NewPeerConnection("peer-1", "user-1", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer: %v", err)
	}
	defer peer.Close(ctx)

	// Add publisher
	if err := router.AddPublisher(ctx, "user-1", peer); err != nil {
		t.Fatalf("failed to add publisher: %v", err)
	}

	// Try adding same publisher again (should fail)
	if err := router.AddPublisher(ctx, "user-1", peer); err == nil {
		t.Error("expected error when adding duplicate publisher")
	}
}

func TestSFURouter_AddSubscriber(t *testing.T) {
	logger := &nullLogger{}
	router := NewSFURouter("test-room", logger, nil)
	ctx := context.Background()

	config := DefaultConfig()
	peer, err := NewPeerConnection("peer-2", "user-2", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer: %v", err)
	}
	defer peer.Close(ctx)

	// Add subscriber
	if err := router.AddSubscriber(ctx, "user-2", peer); err != nil {
		t.Fatalf("failed to add subscriber: %v", err)
	}

	// Try adding same subscriber again (should fail)
	if err := router.AddSubscriber(ctx, "user-2", peer); err == nil {
		t.Error("expected error when adding duplicate subscriber")
	}
}

func TestSFURouter_RemovePublisher(t *testing.T) {
	logger := &nullLogger{}
	router := NewSFURouter("test-room", logger, nil)
	ctx := context.Background()

	config := DefaultConfig()
	peer, err := NewPeerConnection("peer-3", "user-3", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer: %v", err)
	}
	defer peer.Close(ctx)

	// Add then remove publisher
	router.AddPublisher(ctx, "user-3", peer)

	if err := router.RemovePublisher(ctx, "user-3"); err != nil {
		t.Fatalf("failed to remove publisher: %v", err)
	}

	// Try removing non-existent publisher
	if err := router.RemovePublisher(ctx, "user-3"); err == nil {
		t.Error("expected error when removing non-existent publisher")
	}
}

func TestSFURouter_GetStats(t *testing.T) {
	logger := &nullLogger{}
	router := NewSFURouter("test-room", logger, nil)
	ctx := context.Background()

	// Get initial stats
	stats, err := router.GetStats(ctx)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.RoomID != "test-room" {
		t.Errorf("expected room ID 'test-room', got %s", stats.RoomID)
	}

	if stats.PublisherCount != 0 {
		t.Errorf("expected 0 publishers, got %d", stats.PublisherCount)
	}

	if stats.SubscriberCount != 0 {
		t.Errorf("expected 0 subscribers, got %d", stats.SubscriberCount)
	}
}

func TestSFURouter_GetAvailableTracks(t *testing.T) {
	logger := &nullLogger{}
	router := NewSFURouter("test-room", logger, nil)

	// Get tracks (should be empty initially)
	tracks := router.GetAvailableTracks()
	if len(tracks) != 0 {
		t.Errorf("expected 0 tracks, got %d", len(tracks))
	}
}
