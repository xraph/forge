package webrtc

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)

// recordingStreaming stands in for the streaming manager and records how
// signaling messages were sent. Every method not overridden panics through
// the nil embedded interface, which would flag any other delivery path.
type recordingStreaming struct {
	streaming.Manager

	mu         sync.Mutex
	toUser     []string
	broadcasts []string
}

func (r *recordingStreaming) SendToUser(_ context.Context, userID string, _ *streaming.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.toUser = append(r.toUser, userID)

	return nil
}

func (r *recordingStreaming) BroadcastToRoom(_ context.Context, roomID string, _ *streaming.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.broadcasts = append(r.broadcasts, roomID)

	return nil
}

// Regression: offers, answers and ICE candidates were broadcast to the whole
// room instead of being sent to the peer they were for.
func TestSignaling_SendsOnlyToTargetPeer(t *testing.T) {
	rec := &recordingStreaming{}
	s := &signalingManager{streaming: rec, logger: forge.NewNoopLogger()}
	ctx := context.Background()

	if err := s.SendOffer(ctx, "room-1", "bob", &SessionDescription{Type: SessionDescriptionTypeOffer, SDP: "o"}); err != nil {
		t.Fatal(err)
	}

	if err := s.SendAnswer(ctx, "room-1", "carol", &SessionDescription{Type: SessionDescriptionTypeAnswer, SDP: "a"}); err != nil {
		t.Fatal(err)
	}

	if err := s.SendICECandidate(ctx, "room-1", "dave", &ICECandidate{Candidate: "c"}); err != nil {
		t.Fatal(err)
	}

	if len(rec.broadcasts) != 0 {
		t.Errorf("signaling broadcast to rooms %v", rec.broadcasts)
	}

	want := []string{"bob", "carol", "dave"}
	if len(rec.toUser) != len(want) {
		t.Fatalf("sent to %v, want %v", rec.toUser, want)
	}

	for i := range want {
		if rec.toUser[i] != want[i] {
			t.Errorf("message %d went to %q, want %q", i, rec.toUser[i], want[i])
		}
	}

	var se *SignalingError
	if err := s.SendOffer(ctx, "room-1", "", &SessionDescription{}); !errors.As(err, &se) {
		t.Errorf("empty target: err = %v, want a SignalingError", err)
	}
}
