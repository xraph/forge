package local

import (
	"context"
	"fmt"
	"sync"
	"testing"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// Per-room sequences, the foundation of gap-free reconnect.
//
// A client that drops and returns needs to say "I had everything up to N in
// this room"; the server then sends exactly what it missed. That requires a
// monotonic per-room counter assigned by the store — not by the caller, and not
// derived from a timestamp. Timestamps collide within a clock tick and go
// backwards under NTP correction, either of which silently drops or duplicates
// a message on resume.
//
// Assignment belongs in Save because Save is already the single funnel every
// persisted message goes through, and because only the store can make the
// increment atomic with respect to concurrent writers.

func newSeqStore(t *testing.T) *MessageStore {
	t.Helper()

	store, ok := NewMessageStore().(*MessageStore)
	if !ok {
		t.Fatal("NewMessageStore did not return *MessageStore")
	}

	if err := store.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

	return store
}

func TestMessageStore_SaveAssignsMonotonicPerRoomSequence(t *testing.T) {
	store := newSeqStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		msg := &streaming.Message{ID: fmt.Sprintf("m%d", i), RoomID: "room-1"}

		if err := store.Save(ctx, msg); err != nil {
			t.Fatalf("Save: %v", err)
		}

		if msg.Sequence != int64(i) {
			t.Errorf("message %d sequence = %d, want %d", i, msg.Sequence, i)
		}
	}
}

func TestMessageStore_SequencesAreIndependentPerRoom(t *testing.T) {
	// A shared counter would make one busy room advance every other room's
	// cursor, so a quiet room would replay from a sequence it never reached.
	store := newSeqStore(t)
	ctx := context.Background()

	a := &streaming.Message{ID: "a1", RoomID: "room-a"}
	b := &streaming.Message{ID: "b1", RoomID: "room-b"}

	if err := store.Save(ctx, a); err != nil {
		t.Fatalf("Save(a): %v", err)
	}

	if err := store.Save(ctx, b); err != nil {
		t.Fatalf("Save(b): %v", err)
	}

	if a.Sequence != 1 || b.Sequence != 1 {
		t.Errorf("sequences = (%d, %d), want (1, 1) — each room counts on its own", a.Sequence, b.Sequence)
	}
}

func TestMessageStore_ConcurrentSavesGetUniqueSequences(t *testing.T) {
	// Two messages sharing a sequence is the failure that matters: a resuming
	// client asking for "everything after 7" would never receive the second 7.
	store := newSeqStore(t)
	ctx := context.Background()

	const writers = 50

	var wg sync.WaitGroup

	seqs := make([]int64, writers)

	for i := range writers {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			msg := &streaming.Message{ID: fmt.Sprintf("m%d", i), RoomID: "room-1"}
			if err := store.Save(ctx, msg); err != nil {
				t.Errorf("Save: %v", err)

				return
			}

			seqs[i] = msg.Sequence
		}(i)
	}

	wg.Wait()

	seen := make(map[int64]bool, writers)
	for _, s := range seqs {
		if seen[s] {
			t.Fatalf("sequence %d assigned more than once", s)
		}

		seen[s] = true
	}

	if len(seen) != writers {
		t.Errorf("got %d distinct sequences, want %d", len(seen), writers)
	}
}

func TestMessageStore_SaveDoesNotOverwriteAnExplicitSequence(t *testing.T) {
	// A message replicated from another node already carries its origin's
	// sequence. Reassigning it here would renumber the room differently on
	// every node, and a cursor from one node would then mean something else on
	// the next — which is precisely the case a load-balanced deployment hits.
	store := newSeqStore(t)

	msg := &streaming.Message{ID: "replicated", RoomID: "room-1", Sequence: 99}

	if err := store.Save(context.Background(), msg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if msg.Sequence != 99 {
		t.Errorf("sequence = %d, want 99 (preserved)", msg.Sequence)
	}
}

func TestMessageStore_GetSinceReturnsOnlyLaterMessagesInOrder(t *testing.T) {
	store := newSeqStore(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if err := store.Save(ctx, &streaming.Message{
			ID:     fmt.Sprintf("m%d", i),
			RoomID: "room-1",
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got, err := store.GetSince(ctx, "room-1", 2, 100)
	if err != nil {
		t.Fatalf("GetSince: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("GetSince(after 2) returned %d messages, want 3", len(got))
	}

	for i, msg := range got {
		want := int64(i + 3)
		if msg.Sequence != want {
			t.Errorf("message %d sequence = %d, want %d (ascending, exclusive of the cursor)", i, msg.Sequence, want)
		}
	}
}

func TestMessageStore_GetSinceRespectsLimit(t *testing.T) {
	// The limit is what stops a client resuming from a very old cursor pulling
	// an entire room's history into memory in one response.
	store := newSeqStore(t)
	ctx := context.Background()

	for i := 1; i <= 10; i++ {
		if err := store.Save(ctx, &streaming.Message{
			ID:     fmt.Sprintf("m%d", i),
			RoomID: "room-1",
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got, err := store.GetSince(ctx, "room-1", 0, 4)
	if err != nil {
		t.Fatalf("GetSince: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("GetSince(limit 4) returned %d messages, want 4", len(got))
	}

	if got[0].Sequence != 1 {
		t.Errorf("first sequence = %d, want 1 — the limit must take the OLDEST unseen, not the newest", got[0].Sequence)
	}
}

func TestMessageStore_GetSinceOnUnknownRoomIsEmptyNotError(t *testing.T) {
	got, err := newSeqStore(t).GetSince(context.Background(), "never-existed", 0, 10)
	if err != nil {
		t.Fatalf("GetSince: unexpected error %v", err)
	}

	if len(got) != 0 {
		t.Errorf("got %d messages, want 0", len(got))
	}
}
