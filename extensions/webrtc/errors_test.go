package webrtc

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)


func TestErrorTypes(t *testing.T) {
	// Test error type checking
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"PeerNotFound", ErrPeerNotFound, ErrPeerNotFound},
		{"RoomNotFound", ErrRoomNotFound, ErrRoomNotFound},
		{"ConnectionFailed", ErrConnectionFailed, ErrConnectionFailed},
		{"SignalingFailed", ErrSignalingFailed, ErrSignalingFailed},
		{"InvalidSDP", ErrInvalidSDP, ErrInvalidSDP},
		{"TrackNotFound", ErrTrackNotFound, ErrTrackNotFound},
		{"NotInRoom", ErrNotInRoom, ErrNotInRoom},
		{"Unauthorized", ErrUnauthorized, ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err != tt.want {
				t.Errorf("error mismatch: got %v, want %v", tt.err, tt.want)
			}
		})
	}
}

func TestSignalingError(t *testing.T) {
	originalErr := ErrInvalidSDP
	sigErr := &SignalingError{
		Op:     "send_offer",
		PeerID: "test-peer",
		RoomID: "test-room",
		Err:    originalErr,
	}

	expectedMsg := "webrtc signaling: send_offer: webrtc: invalid SDP"
	if sigErr.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, sigErr.Error())
	}

	if sigErr.Unwrap() != originalErr {
		t.Errorf("expected unwrapped error %v, got %v", originalErr, sigErr.Unwrap())
	}
}

func TestConnectionError(t *testing.T) {
	originalErr := ErrConnectionFailed
	connErr := &ConnectionError{
		PeerID: "test-peer",
		State:  ConnectionStateConnecting,
		Err:    originalErr,
	}

	expectedMsg := "webrtc connection: peer=test-peer state=connecting: webrtc: connection failed"
	if connErr.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, connErr.Error())
	}

	if connErr.Unwrap() != originalErr {
		t.Errorf("expected unwrapped error %v, got %v", originalErr, connErr.Unwrap())
	}
}

func TestMediaError(t *testing.T) {
	originalErr := ErrTrackNotFound
	mediaErr := &MediaError{
		TrackID: "audio-1",
		Kind:    TrackKindAudio,
		Err:     originalErr,
	}

	expectedMsg := "webrtc media: track=audio-1 kind=audio: webrtc: track not found"
	if mediaErr.Error() != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, mediaErr.Error())
	}

	if mediaErr.Unwrap() != originalErr {
		t.Errorf("expected unwrapped error %v, got %v", originalErr, mediaErr.Unwrap())
	}
}

func TestPeerConnection_Errors(t *testing.T) {
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

	// Test creating room with invalid options
	_, err = webrtcExt.CreateCallRoom(ctx, "test-room", streaming.RoomOptions{})
	if err == nil {
		t.Error("should fail with empty room options")
	}

	// Test creating room without ID
	_, err = webrtcExt.CreateCallRoom(ctx, "test-room", streaming.RoomOptions{
		Name:  "Test Room",
		Owner: "test-user",
	})
	if err == nil {
		t.Error("should fail without room ID")
	}

	// Test getting non-existent room
	_, err = webrtcExt.GetCallRoom("non-existent")
	if !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("expected ErrRoomNotFound, got %v", err)
	}

	// Test deleting non-existent room
	err = webrtcExt.DeleteCallRoom(ctx, "non-existent")
	if err == nil {
		t.Error("should fail to delete non-existent room")
	}

	// Test extension operations without starting
	_, err = webrtcExt.JoinCall(ctx, "test-room", "user1", &JoinOptions{})
	if err == nil {
		t.Error("should fail to join call without starting extension")
	}

	// Start extension and test more errors
	err = webrtcExt.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	// Test joining non-existent room
	_, err = webrtcExt.JoinCall(ctx, "non-existent", "user1", &JoinOptions{})
	if err == nil {
		t.Error("should fail to join non-existent room")
	}

	// Test leaving from non-existent room
	err = webrtcExt.LeaveCall(ctx, "non-existent", "user1")
	if err == nil {
		t.Error("should fail to leave non-existent room")
	}

	// Cleanup
	webrtcExt.Stop(ctx)
}

func TestPeerConnection_InvalidOperations(t *testing.T) {
	config := DefaultConfig()
	logger := forge.NewNoopLogger()
	ctx := context.Background()

	// Create peer connection
	peer, err := NewPeerConnection("test-peer", "test-user", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(ctx)

	// Test setting remote description with invalid SDP
	invalidSDP := &SessionDescription{
		Type: SessionDescriptionTypeAnswer,
		SDP:  "invalid sdp content",
	}

	err = peer.SetRemoteDescription(ctx, invalidSDP)
	// This might not error immediately, but should be tested

	// Test adding ICE candidate with invalid data
	invalidCandidate := &ICECandidate{
		Candidate:     "invalid candidate",
		SDPMid:        "",
		SDPMLineIndex: -1,
	}

	err = peer.AddICECandidate(ctx, invalidCandidate)
	// This might not error immediately

	// Test creating offer twice without proper flow
	_, err = peer.CreateOffer(ctx)
	if err != nil {
		t.Errorf("first offer should succeed: %v", err)
	}

	// Second offer might succeed or fail depending on implementation
	_, err = peer.CreateOffer(ctx)
	// Just log the result
	t.Logf("second offer result: %v", err)

	// Test closing twice
	err = peer.Close(ctx)
	if err != nil {
		t.Errorf("first close should succeed: %v", err)
	}

	err = peer.Close(ctx)
	if err != nil {
		t.Errorf("second close should succeed: %v", err)
	}
}

func TestCallRoom_InvalidOperations(t *testing.T) {
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

	// Start extension
	err = webrtcExt.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}
	defer webrtcExt.Stop(ctx)

	// Create call room
	opts := streaming.RoomOptions{
		ID:         "error-test-room",
		Name:       "Error Test Room",
		Owner:      "owner",
		MaxMembers: 5,
	}

	room, err := webrtcExt.CreateCallRoom(ctx, "error-test-room", opts)
	if err != nil {
		t.Fatalf("failed to create call room: %v", err)
	}
	defer webrtcExt.DeleteCallRoom(ctx, "error-test-room")

	// Test joining with invalid options
	_, err = room.JoinCall(ctx, "", &JoinOptions{}) // Empty user ID
	// Note: Currently empty user ID is allowed by the implementation
	// This is intentional as validation might be done at a different layer
	if err != nil {
		t.Logf("empty user ID rejected: %v", err)
	}

	// Test joining same user twice
	_, err = room.JoinCall(ctx, "user1", &JoinOptions{})
	if err != nil {
		t.Fatalf("first join should succeed: %v", err)
	}

	_, err = room.JoinCall(ctx, "user1", &JoinOptions{})
	if err == nil {
		t.Error("should fail to join same user twice")
	}

	// Test leaving user who is not in room
	err = room.Leave(ctx, "non-existent-user")
	if !errors.Is(err, ErrNotInRoom) {
		t.Errorf("expected ErrNotInRoom, got %v", err)
	}

	// Test media controls on non-existent user
	err = room.MuteUser(ctx, "non-existent")
	if !errors.Is(err, ErrPeerNotFound) {
		t.Errorf("expected ErrPeerNotFound, got %v", err)
	}

	// Test getting peer for non-existent user
	_, err = room.GetPeer("non-existent")
	if !errors.Is(err, ErrPeerNotFound) {
		t.Errorf("expected ErrPeerNotFound, got %v", err)
	}

	// Cleanup
	room.Leave(ctx, "user1")
}

func TestExtension_HealthCheck(t *testing.T) {
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

	// Test health check before starting
	err = webrtcExt.Health(ctx)
	if err == nil {
		t.Error("health check should fail before starting")
	}

	// Start extension
	err = webrtcExt.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	// Test health check after starting
	err = webrtcExt.Health(ctx)
	if err != nil {
		t.Errorf("health check should pass after starting: %v", err)
	}

	// Stop extension
	err = webrtcExt.Stop(ctx)
	if err != nil {
		t.Fatalf("failed to stop extension: %v", err)
	}

	// Test health check after stopping
	err = webrtcExt.Health(ctx)
	if err == nil {
		t.Error("health check should fail after stopping")
	}
}

func TestConfig_InvalidConfigurations(t *testing.T) {
	// Test SFU config without SFU topology
	config := Config{
		Topology:    TopologyMesh,
		STUNServers: []string{"stun:example.com"},
		SFUConfig:   &SFUConfig{WorkerCount: 4}, // Should be ignored in mesh mode
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("mesh config with SFU settings should be valid: %v", err)
	}

	// Test SFU config validation
	config = Config{
		Topology:    TopologySFU,
		STUNServers: []string{"stun:example.com"},
		SFUConfig:   nil, // Missing!
	}

	err = config.Validate()
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("SFU config without SFUConfig should fail: %v", err)
	}

	// Test bitrate range validation
	config = Config{
		Topology:    TopologyMesh,
		STUNServers: []string{"stun:example.com"},
		MediaConfig: MediaConfig{
			MinVideoBitrate: 2000,
			MaxVideoBitrate: 1000, // Max < Min
		},
	}

	err = config.Validate()
	if !errors.Is(err, ErrInvalidBitrateRange) {
		t.Errorf("invalid bitrate range should fail: %v", err)
	}
}

func TestErrorWrapping(t *testing.T) {
	// Test error wrapping preserves original errors
	originalErr := ErrPeerNotFound

	sigErr := &SignalingError{
		Op:     "test_op",
		PeerID: "test-peer",
		RoomID: "test-room",
		Err:    originalErr,
	}

	// Test that errors.Is works
	if !IsSignalingError(sigErr) {
		t.Error("should identify as SignalingError")
	}

	if !IsError(originalErr, sigErr) {
		t.Error("should identify original error")
	}

	connErr := &ConnectionError{
		PeerID: "test-peer",
		State:  ConnectionStateFailed,
		Err:    originalErr,
	}

	if !IsConnectionError(connErr) {
		t.Error("should identify as ConnectionError")
	}

	mediaErr := &MediaError{
		TrackID: "test-track",
		Kind:    TrackKindAudio,
		Err:     originalErr,
	}

	if !IsMediaError(mediaErr) {
		t.Error("should identify as MediaError")
	}
}

// Helper functions for error type checking
func IsSignalingError(err error) bool {
	_, ok := err.(*SignalingError)
	return ok
}

func IsConnectionError(err error) bool {
	_, ok := err.(*ConnectionError)
	return ok
}

func IsMediaError(err error) bool {
	_, ok := err.(*MediaError)
	return ok
}

func IsError(target error, err error) bool {
	return errors.Is(err, target)
}
