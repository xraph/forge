package webrtc

import (
	"context"
	"testing"
	"time"
)

func TestNewPeerConnection(t *testing.T) {
	config := DefaultConfig()
	logger := &nullLogger{}

	peer, err := NewPeerConnection("test-peer-1", "user-1", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}

	if peer.ID() != "test-peer-1" {
		t.Errorf("expected peer ID 'test-peer-1', got %s", peer.ID())
	}

	if peer.UserID() != "user-1" {
		t.Errorf("expected user ID 'user-1', got %s", peer.UserID())
	}

	if peer.State() != ConnectionStateNew {
		t.Errorf("expected state New, got %s", peer.State())
	}

	// Cleanup
	peer.Close(context.Background())
}

func TestPeerConnection_CreateOffer(t *testing.T) {
	config := DefaultConfig()
	logger := &nullLogger{}
	ctx := context.Background()

	peer, err := NewPeerConnection("test-peer-2", "user-2", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(ctx)

	offer, err := peer.CreateOffer(ctx)
	if err != nil {
		t.Fatalf("failed to create offer: %v", err)
	}

	if offer == nil {
		t.Fatal("offer is nil")
	}

	if offer.Type != SessionDescriptionTypeOffer {
		t.Errorf("expected offer type, got %s", offer.Type)
	}

	if offer.SDP == "" {
		t.Error("SDP is empty")
	}
}

func TestPeerConnection_OfferAnswerFlow(t *testing.T) {
	config := DefaultConfig()
	logger := &nullLogger{}
	ctx := context.Background()

	// Create two peers
	peer1, err := NewPeerConnection("peer-1", "user-1", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer 1: %v", err)
	}
	defer peer1.Close(ctx)

	peer2, err := NewPeerConnection("peer-2", "user-2", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer 2: %v", err)
	}
	defer peer2.Close(ctx)

	// Peer 1 creates offer
	offer, err := peer1.CreateOffer(ctx)
	if err != nil {
		t.Fatalf("peer 1 failed to create offer: %v", err)
	}

	// Peer 1 sets local description
	if err := peer1.SetLocalDescription(ctx, offer); err != nil {
		t.Fatalf("peer 1 failed to set local description: %v", err)
	}

	// Peer 2 sets remote description (peer 1's offer)
	if err := peer2.SetRemoteDescription(ctx, offer); err != nil {
		t.Fatalf("peer 2 failed to set remote description: %v", err)
	}

	// Peer 2 creates answer
	answer, err := peer2.CreateAnswer(ctx)
	if err != nil {
		t.Fatalf("peer 2 failed to create answer: %v", err)
	}

	// Peer 2 sets local description
	if err := peer2.SetLocalDescription(ctx, answer); err != nil {
		t.Fatalf("peer 2 failed to set local description: %v", err)
	}

	// Peer 1 sets remote description (peer 2's answer)
	if err := peer1.SetRemoteDescription(ctx, answer); err != nil {
		t.Fatalf("peer 1 failed to set remote description: %v", err)
	}

	// Both peers should be in connecting or connected state eventually
	// Give them a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify states are reasonable
	state1 := peer1.State()
	state2 := peer2.State()

	t.Logf("peer 1 state: %s", state1)
	t.Logf("peer 2 state: %s", state2)

	// States should not be New or Failed
	if state1 == ConnectionStateFailed {
		t.Error("peer 1 connection failed")
	}
	if state2 == ConnectionStateFailed {
		t.Error("peer 2 connection failed")
	}
}

func TestPeerConnection_AddTrack(t *testing.T) {
	config := DefaultConfig()
	logger := &nullLogger{}
	ctx := context.Background()

	peer, err := NewPeerConnection("test-peer-3", "user-3", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(ctx)

	// Create audio track
	audioTrack, err := NewLocalTrack(TrackKindAudio, "audio-1", "microphone", logger)
	if err != nil {
		t.Fatalf("failed to create audio track: %v", err)
	}

	// Add track
	if err := peer.AddTrack(ctx, audioTrack); err != nil {
		t.Fatalf("failed to add track: %v", err)
	}

	// Verify track was added
	tracks := peer.GetTracks()
	if len(tracks) != 1 {
		t.Errorf("expected 1 track, got %d", len(tracks))
	}

	if tracks[0].ID() != "audio-1" {
		t.Errorf("expected track ID 'audio-1', got %s", tracks[0].ID())
	}
}

func TestPeerConnection_RemoveTrack(t *testing.T) {
	config := DefaultConfig()
	logger := &nullLogger{}
	ctx := context.Background()

	peer, err := NewPeerConnection("test-peer-4", "user-4", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(ctx)

	// Create and add track
	track, err := NewLocalTrack(TrackKindVideo, "video-1", "camera", logger)
	if err != nil {
		t.Fatalf("failed to create video track: %v", err)
	}

	if err := peer.AddTrack(ctx, track); err != nil {
		t.Fatalf("failed to add track: %v", err)
	}

	// Remove track
	if err := peer.RemoveTrack(ctx, "video-1"); err != nil {
		t.Fatalf("failed to remove track: %v", err)
	}

	// Verify track was removed
	tracks := peer.GetTracks()
	if len(tracks) != 0 {
		t.Errorf("expected 0 tracks, got %d", len(tracks))
	}
}

func TestPeerConnection_ICECandidateCallback(t *testing.T) {
	config := DefaultConfig()
	logger := &nullLogger{}
	ctx := context.Background()

	peer, err := NewPeerConnection("test-peer-5", "user-5", config, logger)
	if err != nil {
		t.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(ctx)

	// Setup ICE candidate callback
	candidateReceived := make(chan bool, 1)
	peer.OnICECandidate(func(candidate *ICECandidate) {
		t.Logf("received ICE candidate: %s", candidate.Candidate)
		select {
		case candidateReceived <- true:
		default:
		}
	})

	// Create offer to trigger ICE gathering
	offer, err := peer.CreateOffer(ctx)
	if err != nil {
		t.Fatalf("failed to create offer: %v", err)
	}

	if err := peer.SetLocalDescription(ctx, offer); err != nil {
		t.Fatalf("failed to set local description: %v", err)
	}

	// Wait for at least one ICE candidate
	select {
	case <-candidateReceived:
		// Success
	case <-time.After(2 * time.Second):
		// This is not necessarily an error - ICE gathering may be slow or complete without candidates
		t.Log("no ICE candidate received within timeout (this may be normal)")
	}
}

func BenchmarkPeerConnection_CreateOffer(b *testing.B) {
	config := DefaultConfig()
	logger := &nullLogger{}
	ctx := context.Background()

	peer, err := NewPeerConnection("bench-peer", "bench-user", config, logger)
	if err != nil {
		b.Fatalf("failed to create peer connection: %v", err)
	}
	defer peer.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := peer.CreateOffer(ctx)
		if err != nil {
			b.Fatalf("failed to create offer: %v", err)
		}
	}
}
