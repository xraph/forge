package webrtc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/streaming"
)

// TestIntegration_PeerConnection_FullSetup tests the complete lifecycle of peer connections.
func TestIntegration_PeerConnection_FullSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup streaming extension and WebRTC extension
	app := newMockApp()

	streamingExt := streaming.NewExtension()
	if err := streamingExt.Register(app); err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	if err := streamingExt.Start(ctx); err != nil {
		t.Fatalf("failed to start streaming extension: %v", err)
	}
	defer streamingExt.Stop(ctx)

	// Type assert to get concrete type
	streamingExtConcrete, ok := streamingExt.(*streaming.Extension)
	if !ok {
		t.Fatal("failed to type assert streaming extension")
	}

	webrtcExt, err := New(streamingExtConcrete, DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create webrtc extension: %v", err)
	}

	if err := webrtcExt.Register(app); err != nil {
		t.Fatalf("failed to register webrtc extension: %v", err)
	}

	if err := webrtcExt.Start(ctx); err != nil {
		t.Fatalf("failed to start webrtc extension: %v", err)
	}
	defer webrtcExt.Stop(ctx)

	// Create a call room
	room, err := webrtcExt.CreateCallRoom(ctx, "integration-test-room", streaming.RoomOptions{
		ID:         "integration-test-room",
		Name:       "Integration Test Room",
		MaxMembers: 10,
	})
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}
	defer webrtcExt.DeleteCallRoom(ctx, "integration-test-room")

	// Create two peer connections
	peer1, err := room.JoinCall(ctx, "user1", &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
	})
	if err != nil {
		t.Fatalf("failed to join peer1: %v", err)
	}

	peer2, err := room.JoinCall(ctx, "user2", &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
	})
	if err != nil {
		t.Fatalf("failed to join peer2: %v", err)
	}

	// Verify peers are in the room
	peers := room.GetPeers()
	if len(peers) != 2 {
		t.Errorf("expected 2 peers, got %d", len(peers))
	}

	// Test peer connection state
	t.Logf("peer1 state: %s", peer1.State())
	t.Logf("peer2 state: %s", peer2.State())

	// Test media controls
	if err := room.MuteUser(ctx, "user1"); err != nil {
		t.Errorf("failed to mute user1: %v", err)
	}

	if err := room.UnmuteUser(ctx, "user1"); err != nil {
		t.Errorf("failed to unmute user1: %v", err)
	}

	if err := room.DisableVideo(ctx, "user2"); err != nil {
		t.Errorf("failed to disable video for user2: %v", err)
	}

	if err := room.EnableVideo(ctx, "user2"); err != nil {
		t.Errorf("failed to enable video for user2: %v", err)
	}

	// Test leaving and cleanup
	if err := room.Leave(ctx, "user1"); err != nil {
		t.Errorf("failed to leave user1: %v", err)
	}

	if err := room.Leave(ctx, "user2"); err != nil {
		t.Errorf("failed to leave user2: %v", err)
	}

	// Verify room is empty
	peers = room.GetPeers()
	if len(peers) != 0 {
		t.Errorf("expected 0 peers after leaving, got %d", len(peers))
	}
}

// TestIntegration_RoomClose tests room closure and cleanup.
func TestIntegration_RoomClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup extensions
	app := newMockApp()

	streamingExt := streaming.NewExtension()
	if err := streamingExt.Register(app); err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	if err := streamingExt.Start(ctx); err != nil {
		t.Fatalf("failed to start streaming extension: %v", err)
	}
	defer streamingExt.Stop(ctx)

	streamingExtConcrete, ok := streamingExt.(*streaming.Extension)
	if !ok {
		t.Fatal("failed to type assert streaming extension")
	}

	webrtcExt, err := New(streamingExtConcrete, DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create webrtc extension: %v", err)
	}

	if err := webrtcExt.Register(app); err != nil {
		t.Fatalf("failed to register webrtc extension: %v", err)
	}

	if err := webrtcExt.Start(ctx); err != nil {
		t.Fatalf("failed to start webrtc extension: %v", err)
	}
	defer webrtcExt.Stop(ctx)

	// Create room with multiple peers
	room, err := webrtcExt.CreateCallRoom(ctx, "close-test-room", streaming.RoomOptions{
		ID:         "close-test-room",
		Name:       "Close Test Room",
		MaxMembers: 5,
	})
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}

	// Add multiple users
	for i := 1; i <= 3; i++ {
		userID := fmt.Sprintf("user%d", i)

		_, err := room.JoinCall(ctx, userID, &JoinOptions{})
		if err != nil {
			t.Fatalf("failed to join %s: %v", userID, err)
		}
	}

	// Verify peers are in room
	if len(room.GetPeers()) != 3 {
		t.Errorf("expected 3 peers, got %d", len(room.GetPeers()))
	}

	// Close the room
	if err := room.Close(ctx); err != nil {
		t.Fatalf("failed to close room: %v", err)
	}

	// Try to join after close - should fail
	_, err = room.JoinCall(ctx, "user4", &JoinOptions{})
	if err == nil {
		t.Error("expected error when joining closed room")
	}

	if !errors.Is(err, ErrConnectionClosed) {
		t.Errorf("expected ErrConnectionClosed, got %v", err)
	}

	// Verify room can be deleted
	if err := webrtcExt.DeleteCallRoom(ctx, "close-test-room"); err != nil {
		t.Errorf("failed to delete closed room: %v", err)
	}
}

// TestIntegration_Concurrent_Operations tests concurrent room operations.
func TestIntegration_Concurrent_Operations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Setup extensions
	app := newMockApp()

	streamingExt := streaming.NewExtension()
	if err := streamingExt.Register(app); err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	if err := streamingExt.Start(ctx); err != nil {
		t.Fatalf("failed to start streaming extension: %v", err)
	}
	defer streamingExt.Stop(ctx)

	streamingExtConcrete, ok := streamingExt.(*streaming.Extension)
	if !ok {
		t.Fatal("failed to type assert streaming extension")
	}

	webrtcExt, err := New(streamingExtConcrete, DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create webrtc extension: %v", err)
	}

	if err := webrtcExt.Register(app); err != nil {
		t.Fatalf("failed to register webrtc extension: %v", err)
	}

	if err := webrtcExt.Start(ctx); err != nil {
		t.Fatalf("failed to start webrtc extension: %v", err)
	}
	defer webrtcExt.Stop(ctx)

	// Create room
	room, err := webrtcExt.CreateCallRoom(ctx, "concurrent-test-room", streaming.RoomOptions{
		ID:         "concurrent-test-room",
		Name:       "Concurrent Test Room",
		MaxMembers: 20,
	})
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}
	defer webrtcExt.DeleteCallRoom(ctx, "concurrent-test-room")

	// Concurrently join multiple users
	var wg sync.WaitGroup

	numUsers := 10
	errs := make(chan error, numUsers)

	for i := range numUsers {
		wg.Add(1)

		go func(userID int) {
			defer wg.Done()

			userIDStr := fmt.Sprintf("user%d", userID)

			_, err := room.JoinCall(ctx, userIDStr, &JoinOptions{})
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	// Check for errors
	errorCount := 0

	for err := range errs {
		t.Errorf("error during concurrent join: %v", err)

		errorCount++
	}

	if errorCount > 0 {
		t.Fatalf("encountered %d errors during concurrent operations", errorCount)
	}

	// Verify all users joined
	peers := room.GetPeers()
	if len(peers) != numUsers {
		t.Errorf("expected %d peers, got %d", numUsers, len(peers))
	}

	// Concurrently leave
	wg = sync.WaitGroup{}
	errs = make(chan error, numUsers)

	for i := range numUsers {
		wg.Add(1)

		go func(userID int) {
			defer wg.Done()

			userIDStr := fmt.Sprintf("user%d", userID)
			if err := room.Leave(ctx, userIDStr); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	// Check for errors
	errorCount = 0

	for err := range errs {
		t.Errorf("error during concurrent leave: %v", err)

		errorCount++
	}

	if errorCount > 0 {
		t.Fatalf("encountered %d errors during concurrent leave", errorCount)
	}

	// Verify room is empty
	peers = room.GetPeers()
	if len(peers) != 0 {
		t.Errorf("expected 0 peers after concurrent leave, got %d", len(peers))
	}
}
