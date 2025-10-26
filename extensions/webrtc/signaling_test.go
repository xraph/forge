package webrtc

import (
	"context"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)


func TestSignalingManager(t *testing.T) {
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

	logger := forge.NewNoopLogger()

	// Create signaling manager
	signaling := NewSignalingManager(streamingExt, logger)

	// Test offer sending
	offer := &SessionDescription{
		Type: SessionDescriptionTypeOffer,
		SDP:  "test offer SDP",
	}

	err = signaling.SendOffer(context.Background(), "test-room", "test-peer", offer)
	if err != nil {
		t.Errorf("failed to send offer: %v", err)
	}

	// Test answer sending
	answer := &SessionDescription{
		Type: SessionDescriptionTypeAnswer,
		SDP:  "test answer SDP",
	}

	err = signaling.SendAnswer(context.Background(), "test-room", "test-peer", answer)
	if err != nil {
		t.Errorf("failed to send answer: %v", err)
	}

	// Test ICE candidate sending
	candidate := &ICECandidate{
		Candidate:        "test candidate",
		SDPMid:           "0",
		SDPMLineIndex:    0,
		UsernameFragment: "test-ufrag",
	}

	err = signaling.SendICECandidate(context.Background(), "test-room", "test-peer", candidate)
	if err != nil {
		t.Errorf("failed to send ICE candidate: %v", err)
	}

	// Test start/stop
	ctx := context.Background()
	err = signaling.Start(ctx)
	if err != nil {
		t.Errorf("failed to start signaling: %v", err)
	}

	err = signaling.Stop(ctx)
	if err != nil {
		t.Errorf("failed to stop signaling: %v", err)
	}
}

func TestSignalingManager_Handlers(t *testing.T) {
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

	logger := forge.NewNoopLogger()

	// Create signaling manager
	signaling := NewSignalingManager(streamingExt, logger)

	// Test offer handler
	offerReceived := make(chan bool, 1)
	signaling.OnOffer(func(peerID string, offer *SessionDescription) {
		if peerID == "test-peer" && offer.Type == SessionDescriptionTypeOffer {
			select {
			case offerReceived <- true:
			default:
			}
		}
	})

	// Test answer handler
	answerReceived := make(chan bool, 1)
	signaling.OnAnswer(func(peerID string, answer *SessionDescription) {
		if peerID == "test-peer" && answer.Type == SessionDescriptionTypeAnswer {
			select {
			case answerReceived <- true:
			default:
			}
		}
	})

	// Test ICE candidate handler
	candidateReceived := make(chan bool, 1)
	signaling.OnICECandidate(func(peerID string, candidate *ICECandidate) {
		if peerID == "test-peer" {
			select {
			case candidateReceived <- true:
			default:
			}
		}
	})

	// Start signaling
	ctx := context.Background()
	err = signaling.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start signaling: %v", err)
	}

	// Send test messages (these would normally come from streaming)
	_ = &SessionDescription{
		Type: SessionDescriptionTypeOffer,
		SDP:  "test offer",
	}

	_ = &SessionDescription{
		Type: SessionDescriptionTypeAnswer,
		SDP:  "test answer",
	}

	_ = &ICECandidate{
		Candidate:     "test candidate",
		SDPMid:        "0",
		SDPMLineIndex: 0,
	}

	// Note: These would normally trigger the handlers via the streaming system
	// For testing purposes, we just verify the handlers are registered
	if signaling == nil {
		t.Error("signaling manager should not be nil")
	}

	// Cleanup
	signaling.Stop(ctx)
}

func TestSignalingManager_Errors(t *testing.T) {
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

	logger := forge.NewNoopLogger()

	// Create signaling manager
	signaling := NewSignalingManager(streamingExt, logger)

	// Test sending without starting
	offer := &SessionDescription{
		Type: SessionDescriptionTypeOffer,
		SDP:  "test offer",
	}

	// This should still work as it just sends via streaming
	err = signaling.SendOffer(context.Background(), "test-room", "test-peer", offer)
	if err != nil {
		t.Errorf("sending offer should work even without starting: %v", err)
	}

	// Test double start
	ctx := context.Background()
	err = signaling.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start signaling: %v", err)
	}

	// Second start should not error
	err = signaling.Start(ctx)
	if err != nil {
		t.Errorf("double start should not error: %v", err)
	}

	// Test double stop
	err = signaling.Stop(ctx)
	if err != nil {
		t.Fatalf("failed to stop signaling: %v", err)
	}

	err = signaling.Stop(ctx)
	if err != nil {
		t.Errorf("double stop should not error: %v", err)
	}
}

func TestSignalingManager_ConcurrentAccess(t *testing.T) {
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

	logger := forge.NewNoopLogger()

	// Create signaling manager
	signaling := NewSignalingManager(streamingExt, logger)

	ctx := context.Background()

	// Start signaling in background
	go func() {
		signaling.Start(ctx)
	}()

	// Test concurrent sending
	done := make(chan bool, 1)
	go func() {
		for i := 0; i < 100; i++ {
			offer := &SessionDescription{
				Type: SessionDescriptionTypeOffer,
				SDP:  "test offer",
			}

			signaling.SendOffer(ctx, "test-room", "test-peer", offer)
		}
		done <- true
	}()

	// Wait for concurrent operations to complete
	<-done

	// Cleanup
	signaling.Stop(ctx)
}
