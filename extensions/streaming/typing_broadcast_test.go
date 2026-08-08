package streaming

import (
	"context"
	"testing"

	"github.com/xraph/forge/extensions/streaming/internal"
)

// The typing indicator was fully wired inbound and never published. handleMessage
// accepted a typing frame and set tracker state; typingTracker.BroadcastTyping
// was a stub returning nil and no other path put anything on the wire, so no
// client ever saw who was typing. These tests pin the publish half.

func TestStartTypingBroadcastsToTheRoom(t *testing.T) {
	h := newTestManager(t, testConfig())

	typist := newTestConn("c1", "u1")
	peer := newTestConn("c2", "u2")
	elsewhere := newTestConn("c3", "u3")

	h.register(t, typist, peer, elsewhere)
	h.join(t, typist, "room-1")
	h.join(t, peer, "room-1")
	h.join(t, elsewhere, "room-2")

	assertNoError(t, h.mgr.StartTyping(context.Background(), "u1", "room-1"))

	assertWrites(t, peer, 1)
	assertWrites(t, elsewhere, 0)

	// The room fan-out includes the author; a client ignores the echo of its own
	// indicator, and has to anyway because the same frame arrives from peer nodes.
	assertWrites(t, typist, 1)

	got := peer.rec.lastJSON(t)

	if got.Type != internal.MessageTypeTyping {
		t.Errorf("Type = %q, want %q", got.Type, internal.MessageTypeTyping)
	}

	if got.RoomID != "room-1" {
		t.Errorf("RoomID = %q, want room-1", got.RoomID)
	}

	if got.UserID != "u1" {
		t.Errorf("UserID = %q, want u1", got.UserID)
	}

	// Data is the boolean, matching what the inbound handler parses and what the
	// AsyncAPI TypingStart payload documents.
	if isTyping, ok := got.Data.(bool); !ok || !isTyping {
		t.Errorf("Data = %#v, want true", got.Data)
	}
}

func TestStopTypingBroadcastsToTheRoom(t *testing.T) {
	h := newTestManager(t, testConfig())

	typist := newTestConn("c1", "u1")
	peer := newTestConn("c2", "u2")

	h.register(t, typist, peer)
	h.join(t, typist, "room-1")
	h.join(t, peer, "room-1")

	assertNoError(t, h.mgr.StopTyping(context.Background(), "u1", "room-1"))

	assertWrites(t, peer, 1)

	got := peer.rec.lastJSON(t)

	if got.Type != internal.MessageTypeTyping {
		t.Errorf("Type = %q, want %q", got.Type, internal.MessageTypeTyping)
	}

	if isTyping, ok := got.Data.(bool); !ok || isTyping {
		t.Errorf("Data = %#v, want false", got.Data)
	}
}

// TestTypingFramesCarryDistinctIDs guards the cross-node path: inbound relayed
// messages are dropped by message-ID dedup, so two indicators sharing an ID
// would leave the second one silently undelivered on every peer node.
func TestTypingFramesCarryDistinctIDs(t *testing.T) {
	h := newTestManager(t, testConfig())

	typist := newTestConn("c1", "u1")
	peer := newTestConn("c2", "u2")

	h.register(t, typist, peer)
	h.join(t, typist, "room-1")
	h.join(t, peer, "room-1")

	assertNoError(t, h.mgr.StartTyping(context.Background(), "u1", "room-1"))
	assertWrites(t, peer, 1)

	first := peer.rec.lastJSON(t).ID

	assertNoError(t, h.mgr.StopTyping(context.Background(), "u1", "room-1"))
	assertWrites(t, peer, 2)

	second := peer.rec.lastJSON(t).ID

	if first == "" || second == "" {
		t.Fatalf("typing frames need IDs for dedup, got %q and %q", first, second)
	}

	if first == second {
		t.Fatalf("both typing frames share ID %q; the second would be deduped away on peer nodes", first)
	}
}

// TestTypingBroadcastRespectsTheFeatureFlag checks the publish half stays behind
// the same switch as the tracking half.
func TestTypingBroadcastRespectsTheFeatureFlag(t *testing.T) {
	cfg := testConfig()
	cfg.EnableTypingIndicators = false

	h := newTestManager(t, cfg)

	typist := newTestConn("c1", "u1")
	peer := newTestConn("c2", "u2")

	h.register(t, typist, peer)
	h.join(t, typist, "room-1")
	h.join(t, peer, "room-1")

	if err := h.mgr.StartTyping(context.Background(), "u1", "room-1"); err == nil {
		t.Fatal("StartTyping: want an error when typing indicators are disabled")
	}

	// StopTyping is deliberately lenient - it is called on cleanup paths - but it
	// must not publish either.
	assertNoError(t, h.mgr.StopTyping(context.Background(), "u1", "room-1"))

	assertWrites(t, peer, 0)
}
