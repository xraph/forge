package webrtc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/xraph/forge/extensions/streaming"
)

// mockSignalingManager for testing
type mockSignalingManager struct{}

func (m *mockSignalingManager) SendOffer(ctx context.Context, roomID, peerID string, offer *SessionDescription) error {
	return nil
}

func (m *mockSignalingManager) SendAnswer(ctx context.Context, roomID, peerID string, answer *SessionDescription) error {
	return nil
}

func (m *mockSignalingManager) SendICECandidate(ctx context.Context, roomID, peerID string, candidate *ICECandidate) error {
	return nil
}

func (m *mockSignalingManager) OnOffer(handler OfferHandler)                    {}
func (m *mockSignalingManager) OnAnswer(handler AnswerHandler)                  {}
func (m *mockSignalingManager) OnICECandidate(handler ICECandidateReceivedHandler) {}
func (m *mockSignalingManager) Start(ctx context.Context) error                  { return nil }
func (m *mockSignalingManager) Stop(ctx context.Context) error                   { return nil }

func TestCallRoom_JoinLeave(t *testing.T) {
	// Create streaming extension
	streamingExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
	).(*streaming.Extension)

	// Register streaming extension to initialize manager
	app := newMockApp()
	err := streamingExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	// Create WebRTC extension
	config := DefaultConfig()
	webrtcExt, err := New(streamingExt, config)
	if err != nil {
		t.Fatalf("failed to create WebRTC extension: %v", err)
	}

	// Register WebRTC extension
	err = webrtcExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register WebRTC extension: %v", err)
	}

	ctx := context.Background()

	// Create call room
	opts := streaming.RoomOptions{
		ID:          "test-room",
		Name:        "Test Room",
		Description: "Test room for WebRTC",
		Owner:       "test-user",
		MaxMembers:  10,
	}

	room, err := webrtcExt.CreateCallRoom(ctx, "test-room", opts)
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}

	// Test joining
	joinOpts := &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
		DisplayName:  "Test User",
	}

	peer1, err := room.JoinCall(ctx, "user1", joinOpts)
	if err != nil {
		t.Fatalf("failed to join call: %v", err)
	}

	if peer1.ID() == "" {
		t.Error("peer ID should not be empty")
	}

	if peer1.UserID() != "user1" {
		t.Errorf("expected user ID 'user1', got %s", peer1.UserID())
	}

	// Test getting peer
	retrievedPeer, err := room.GetPeer("user1")
	if err != nil {
		t.Fatalf("failed to get peer: %v", err)
	}

	if retrievedPeer.ID() != peer1.ID() {
		t.Error("retrieved peer ID mismatch")
	}

	// Test getting all peers
	peers := room.GetPeers()
	if len(peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(peers))
	}

	// Test participants
	participants := room.GetParticipants()
	if len(participants) != 1 {
		t.Errorf("expected 1 participant, got %d", len(participants))
	}

	if participants[0].UserID != "user1" {
		t.Errorf("expected participant user ID 'user1', got %s", participants[0].UserID)
	}

	// Test leaving
	err = room.Leave(ctx, "user1")
	if err != nil {
		t.Fatalf("failed to leave call: %v", err)
	}

	// Verify peer is gone
	peers = room.GetPeers()
	if len(peers) != 0 {
		t.Errorf("expected 0 peers after leaving, got %d", len(peers))
	}

	// Test getting non-existent peer
	_, err = room.GetPeer("user1")
	if !errors.Is(err, ErrPeerNotFound) {
		t.Errorf("expected ErrPeerNotFound, got %v", err)
	}

	// Test leaving non-existent user
	err = room.Leave(ctx, "nonexistent")
	if !errors.Is(err, ErrNotInRoom) {
		t.Errorf("expected ErrNotInRoom, got %v", err)
	}

	// Cleanup
	webrtcExt.DeleteCallRoom(ctx, "test-room")
}

func TestCallRoom_MultipleUsers(t *testing.T) {
	// Create streaming extension
	streamingExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
	).(*streaming.Extension)

	// Register streaming extension to initialize manager
	app := newMockApp()
	err := streamingExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	// Create WebRTC extension
	config := DefaultConfig()
	webrtcExt, err := New(streamingExt, config)
	if err != nil {
		t.Fatalf("failed to create WebRTC extension: %v", err)
	}

	// Register WebRTC extension
	err = webrtcExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register WebRTC extension: %v", err)
	}

	ctx := context.Background()

	// Create call room
	opts := streaming.RoomOptions{
		ID:         "multi-user-room",
		Name:       "Multi User Room",
		Owner:      "owner",
		MaxMembers: 5,
	}

	room, err := webrtcExt.CreateCallRoom(ctx, "multi-user-room", opts)
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}

	// Join multiple users
	joinOpts := &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
	}

	users := []string{"user1", "user2", "user3"}
	peers := make([]PeerConnection, 0, len(users))

	for _, userID := range users {
		peer, err := room.JoinCall(ctx, userID, joinOpts)
		if err != nil {
			t.Fatalf("failed to join user %s: %v", userID, err)
		}
		peers = append(peers, peer)
	}

	// Verify all users are in room
	allPeers := room.GetPeers()
	if len(allPeers) != len(users) {
		t.Errorf("expected %d peers, got %d", len(users), len(allPeers))
	}

	// Verify all participants
	participants := room.GetParticipants()
	if len(participants) != len(users) {
		t.Errorf("expected %d participants, got %d", len(users), len(participants))
	}

	// Leave one user and verify
	err = room.Leave(ctx, "user2")
	if err != nil {
		t.Fatalf("failed to leave user2: %v", err)
	}

	allPeers = room.GetPeers()
	if len(allPeers) != len(users)-1 {
		t.Errorf("expected %d peers after leaving, got %d", len(users)-1, len(allPeers))
	}

	// Cleanup
	for _, userID := range users {
		if userID != "user2" { // Already left
			room.Leave(ctx, userID)
		}
	}

	webrtcExt.DeleteCallRoom(ctx, "multi-user-room")
}

func TestCallRoom_Close(t *testing.T) {
	// Create streaming extension
	streamingExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
	).(*streaming.Extension)

	// Register streaming extension to initialize manager
	app := newMockApp()
	err := streamingExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	// Create WebRTC extension
	config := DefaultConfig()
	webrtcExt, err := New(streamingExt, config)
	if err != nil {
		t.Fatalf("failed to create WebRTC extension: %v", err)
	}

	// Register WebRTC extension
	err = webrtcExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register WebRTC extension: %v", err)
	}

	ctx := context.Background()

	// Create call room
	opts := streaming.RoomOptions{
		ID:         "close-test-room",
		Name:       "Close Test Room",
		Owner:      "owner",
		MaxMembers: 5,
	}

	room, err := webrtcExt.CreateCallRoom(ctx, "close-test-room", opts)
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}

	// Join users
	joinOpts := &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
	}

	_, err = room.JoinCall(ctx, "user1", joinOpts)
	if err != nil {
		t.Fatalf("failed to join user1: %v", err)
	}

	// Close room
	err = room.Close(ctx)
	if err != nil {
		t.Fatalf("failed to close room: %v", err)
	}

	// Verify room is closed (should not be able to join)
	_, err = room.JoinCall(ctx, "user2", joinOpts)
	if err == nil {
		t.Error("should not be able to join closed room")
	}

	// Test double close
	err = room.Close(ctx)
	if err != nil {
		t.Errorf("double close should not error: %v", err)
	}
}

func TestCallRoom_MediaControls(t *testing.T) {
	// Create streaming extension
	streamingExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
	).(*streaming.Extension)

	// Register streaming extension to initialize manager
	app := newMockApp()
	err := streamingExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	// Create WebRTC extension
	config := DefaultConfig()
	webrtcExt, err := New(streamingExt, config)
	if err != nil {
		t.Fatalf("failed to create WebRTC extension: %v", err)
	}

	// Register WebRTC extension
	err = webrtcExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register WebRTC extension: %v", err)
	}

	ctx := context.Background()

	// Create call room
	opts := streaming.RoomOptions{
		ID:         "media-test-room",
		Name:       "Media Test Room",
		Owner:      "owner",
		MaxMembers: 5,
	}

	room, err := webrtcExt.CreateCallRoom(ctx, "media-test-room", opts)
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}

	// Join user
	joinOpts := &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
	}

	_, err = room.JoinCall(ctx, "user1", joinOpts)
	if err != nil {
		t.Fatalf("failed to join user1: %v", err)
	}

	// Test mute/unmute
	err = room.MuteUser(ctx, "user1")
	if err != nil {
		t.Fatalf("failed to mute user: %v", err)
	}

	err = room.UnmuteUser(ctx, "user1")
	if err != nil {
		t.Fatalf("failed to unmute user: %v", err)
	}

	// Test video enable/disable
	err = room.DisableVideo(ctx, "user1")
	if err != nil {
		t.Fatalf("failed to disable video: %v", err)
	}

	err = room.EnableVideo(ctx, "user1")
	if err != nil {
		t.Fatalf("failed to enable video: %v", err)
	}

	// Test operations on non-existent user
	err = room.MuteUser(ctx, "nonexistent")
	if !errors.Is(err, ErrPeerNotFound) {
		t.Errorf("expected ErrPeerNotFound for mute, got %v", err)
	}

	// Cleanup
	room.Leave(ctx, "user1")
	webrtcExt.DeleteCallRoom(ctx, "media-test-room")
}

func TestCallRoom_DuplicateRoomCreation(t *testing.T) {
	// Create streaming extension
	streamingExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
	).(*streaming.Extension)

	// Register streaming extension to initialize manager
	app := newMockApp()
	err := streamingExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register streaming extension: %v", err)
	}

	// Create WebRTC extension
	config := DefaultConfig()
	webrtcExt, err := New(streamingExt, config)
	if err != nil {
		t.Fatalf("failed to create WebRTC extension: %v", err)
	}

	// Register WebRTC extension
	err = webrtcExt.Register(app)
	if err != nil {
		t.Fatalf("failed to register WebRTC extension: %v", err)
	}

	ctx := context.Background()

	// Create call room
	opts := streaming.RoomOptions{
		ID:         "duplicate-test",
		Name:       "Duplicate Test",
		Owner:      "owner",
		MaxMembers: 5,
	}

	_, err = webrtcExt.CreateCallRoom(ctx, "duplicate-test", opts)
	if err != nil {
		t.Fatalf("failed to create first room: %v", err)
	}

	// Try to create duplicate
	_, err = webrtcExt.CreateCallRoom(ctx, "duplicate-test", opts)
	if err == nil {
		t.Error("should not be able to create duplicate room")
	}

	// Cleanup
	webrtcExt.DeleteCallRoom(ctx, "duplicate-test")
}

func BenchmarkCallRoom_JoinLeave(b *testing.B) {
	// Create streaming extension
	streamingExt := streaming.NewExtension(
		streaming.WithLocalBackend(),
	).(*streaming.Extension)

	// Register streaming extension to initialize manager
	app := newMockApp()
	err := streamingExt.Register(app)
	if err != nil {
		b.Fatalf("failed to register streaming extension: %v", err)
	}

	// Create WebRTC extension
	config := DefaultConfig()
	webrtcExt, err := New(streamingExt, config)
	if err != nil {
		b.Fatalf("failed to create WebRTC extension: %v", err)
	}

	// Register WebRTC extension
	err = webrtcExt.Register(app)
	if err != nil {
		b.Fatalf("failed to register WebRTC extension: %v", err)
	}

	ctx := context.Background()

	// Create call room
	opts := streaming.RoomOptions{
		ID:         "bench-room",
		Name:       "Benchmark Room",
		Owner:      "owner",
		MaxMembers: 100,
	}

	room, err := webrtcExt.CreateCallRoom(ctx, "bench-room", opts)
	if err != nil {
		b.Fatalf("failed to create call room: %v", err)
	}

	joinOpts := &JoinOptions{
		AudioEnabled: true,
		VideoEnabled: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		userID := fmt.Sprintf("bench-user-%d", i)
		peer, err := room.JoinCall(ctx, userID, joinOpts)
		if err != nil {
			b.Fatalf("failed to join in benchmark: %v", err)
		}
		room.Leave(ctx, userID)
		peer.Close(ctx)
	}

	// Cleanup
	webrtcExt.DeleteCallRoom(ctx, "bench-room")
}
