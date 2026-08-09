package streaming

import (
	"context"
	"errors"
	"testing"
)

// Replaying the gap.
//
// This is the payoff for sequences and cursors: a reconnecting client is sent
// exactly what it missed, per room, instead of the client having to invalidate
// every tag it holds and refetch — which is what packages/client-core's
// StreamBinder.recover does today, and costs one request per mounted live query
// on every transient network blip.

func TestManager_ReplayDeliversOnlyMissedMessages(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)
	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))

	for i := 1; i <= 5; i++ {
		assertNoError(t, h.messages.Save(context.Background(), &Message{
			ID:       string(rune('a' + i)),
			RoomID:   "room-1",
			Sequence: int64(i),
		}))
	}

	n, err := h.mgr.Replay(context.Background(), "c1", encodeReplayCursor(replayCursor{"room-1": 3}))
	assertNoError(t, err)

	if n != 2 {
		t.Errorf("replayed %d messages, want 2", n)
	}

	assertWrites(t, conn, 2)
}

func TestManager_ReplayOnlyCoversRoomsTheConnectionIsIn(t *testing.T) {
	// A cursor is client-supplied. Honouring a room the connection has not
	// joined would let anyone read any room's history by naming it here.
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)
	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "joined-room"))

	assertNoError(t, h.messages.Save(context.Background(), &Message{
		ID: "secret", RoomID: "private-room", Sequence: 1,
	}))

	n, err := h.mgr.Replay(context.Background(), "c1", encodeReplayCursor(replayCursor{
		"private-room": 0,
	}))
	assertNoError(t, err)

	if n != 0 {
		t.Errorf("replayed %d messages from an unjoined room, want 0", n)
	}

	assertWrites(t, conn, 0)
}

func TestManager_ReplayWithNoCursorReplaysNothing(t *testing.T) {
	// A fresh connection has no cursor. Treating "no cursor" as "from the
	// beginning" would dump every room's entire history into a first connect.
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)
	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))

	assertNoError(t, h.messages.Save(context.Background(), &Message{
		ID: "m1", RoomID: "room-1", Sequence: 1,
	}))

	n, err := h.mgr.Replay(context.Background(), "c1", "")
	assertNoError(t, err)

	if n != 0 {
		t.Errorf("replayed %d messages for an empty cursor, want 0", n)
	}
}

func TestManager_ReplayRejectsAMalformedCursor(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	_, err := h.mgr.Replay(context.Background(), "c1", "not-a-valid-cursor!!")
	if err == nil {
		t.Fatal("Replay: want an error for a malformed cursor, got nil")
	}
}

func TestManager_ReplayOnUnknownConnectionErrors(t *testing.T) {
	h := newTestManager(t, testConfig())

	_, err := h.mgr.Replay(context.Background(), "nope", "")
	if !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("Replay error = %v, want ErrConnectionNotFound", err)
	}
}

func TestManager_ReplayIsDisabledWithoutMessageHistory(t *testing.T) {
	// Replay reads from the message store. With history off there is nothing to
	// read, and silently returning zero would look identical to "you missed
	// nothing" — so it says so instead.
	cfg := testConfig()
	cfg.EnableMessageHistory = false

	h := newTestManager(t, cfg)

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	_, err := h.mgr.Replay(context.Background(), "c1", encodeReplayCursor(replayCursor{"room-1": 1}))
	if !errors.Is(err, ErrHistoryDisabled) {
		t.Fatalf("Replay error = %v, want ErrHistoryDisabled", err)
	}
}
