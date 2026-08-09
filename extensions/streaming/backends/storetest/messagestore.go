// Package storetest holds the behavioural contract every MessageStore backend
// must satisfy, so the backends can be proven to agree rather than merely to
// each work.
//
// Agreement is the property that actually matters for replay. A resume cursor
// is issued by whichever node served the previous connection and redeemed by
// whichever node serves the next one, and in a load-balanced deployment those
// are routinely different processes — potentially running different backends
// during a migration. If "sequence 7" means one message in the local store and
// another in Redis, a reconnecting client is silently handed the wrong slice of
// history, and nothing in either backend's own test suite would catch it.
//
// Written as an exported suite rather than duplicated per backend for the same
// reason: two copies of a contract drift, and the drift is invisible until it
// reaches production.
package storetest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// NewMessageStore builds a connected, empty store for one test.
type NewMessageStore func(t *testing.T) streaming.MessageStore

// RunMessageStoreContract asserts the sequencing and replay behaviour every
// backend must provide.
func RunMessageStoreContract(t *testing.T, newStore NewMessageStore) {
	t.Helper()

	t.Run("Save assigns a monotonic per-room sequence", func(t *testing.T) {
		store := newStore(t)
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
	})

	t.Run("sequences are independent per room", func(t *testing.T) {
		// A shared counter would let a busy room advance a quiet room's cursor,
		// so the quiet room would resume from a sequence it never reached and
		// its backlog would be skipped entirely.
		store := newStore(t)
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
			t.Errorf("sequences = (%d, %d), want (1, 1)", a.Sequence, b.Sequence)
		}
	})

	t.Run("concurrent saves get unique sequences", func(t *testing.T) {
		// Two messages sharing a sequence makes one of them permanently
		// invisible to any client resuming from it.
		store := newStore(t)
		ctx := context.Background()

		const writers = 30

		var (
			wg sync.WaitGroup
			mu sync.Mutex
		)

		seqs := make([]int64, 0, writers)

		for i := range writers {
			wg.Add(1)

			go func(i int) {
				defer wg.Done()

				msg := &streaming.Message{ID: fmt.Sprintf("m%d", i), RoomID: "room-1"}
				if err := store.Save(ctx, msg); err != nil {
					return
				}

				mu.Lock()
				seqs = append(seqs, msg.Sequence)
				mu.Unlock()
			}(i)
		}

		wg.Wait()

		seen := make(map[int64]bool, len(seqs))
		for _, s := range seqs {
			if seen[s] {
				t.Fatalf("sequence %d assigned more than once", s)
			}

			seen[s] = true
		}

		if len(seen) != writers {
			t.Errorf("got %d distinct sequences from %d writers", len(seen), writers)
		}
	})

	t.Run("an explicit sequence is preserved", func(t *testing.T) {
		// A replicated message already carries its origin node's number.
		// Renumbering it would give one message different sequences on
		// different nodes, so a cursor would mean something else after a
		// reconnect landed elsewhere — the exact load-balanced case.
		store := newStore(t)

		msg := &streaming.Message{ID: "replicated", RoomID: "room-1", Sequence: 99}

		if err := store.Save(context.Background(), msg); err != nil {
			t.Fatalf("Save: %v", err)
		}

		if msg.Sequence != 99 {
			t.Errorf("sequence = %d, want 99 (preserved)", msg.Sequence)
		}
	})

	t.Run("a local sequence never collides with a replicated one", func(t *testing.T) {
		// After accepting an explicit sequence, the counter must be at least
		// that high, or the next locally assigned message reuses a number the
		// client has already seen.
		store := newStore(t)
		ctx := context.Background()

		if err := store.Save(ctx, &streaming.Message{ID: "r", RoomID: "room-1", Sequence: 50}); err != nil {
			t.Fatalf("Save(replicated): %v", err)
		}

		local := &streaming.Message{ID: "local", RoomID: "room-1"}
		if err := store.Save(ctx, local); err != nil {
			t.Fatalf("Save(local): %v", err)
		}

		if local.Sequence <= 50 {
			t.Errorf("local sequence = %d, want > 50", local.Sequence)
		}
	})

	t.Run("GetSince returns later messages only, oldest first", func(t *testing.T) {
		store := newStore(t)
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
			t.Fatalf("GetSince(after 2) returned %d, want 3", len(got))
		}

		for i, msg := range got {
			want := int64(i + 3)
			if msg.Sequence != want {
				t.Errorf("position %d has sequence %d, want %d", i, msg.Sequence, want)
			}
		}
	})

	t.Run("GetSince takes the oldest unseen when limited", func(t *testing.T) {
		// The limit bounds a long gap. Taking the NEWEST would leave a hole in
		// the middle that the client's next cursor would skip straight past.
		store := newStore(t)
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
			t.Fatalf("GetSince(limit 4) returned %d, want 4", len(got))
		}

		if got[0].Sequence != 1 {
			t.Errorf("first sequence = %d, want 1 (oldest unseen)", got[0].Sequence)
		}
	})

	t.Run("GetSince isolates rooms", func(t *testing.T) {
		store := newStore(t)
		ctx := context.Background()

		if err := store.Save(ctx, &streaming.Message{ID: "a", RoomID: "room-a"}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		if err := store.Save(ctx, &streaming.Message{ID: "b", RoomID: "room-b"}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		got, err := store.GetSince(ctx, "room-a", 0, 10)
		if err != nil {
			t.Fatalf("GetSince: %v", err)
		}

		if len(got) != 1 || got[0].ID != "a" {
			t.Errorf("GetSince(room-a) = %v, want exactly the room-a message", got)
		}
	})

	t.Run("GetSince past the end is empty", func(t *testing.T) {
		store := newStore(t)
		ctx := context.Background()

		if err := store.Save(ctx, &streaming.Message{ID: "m1", RoomID: "room-1"}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		got, err := store.GetSince(ctx, "room-1", 999, 10)
		if err != nil {
			t.Fatalf("GetSince: %v", err)
		}

		if len(got) != 0 {
			t.Errorf("got %d messages, want 0", len(got))
		}
	})

	t.Run("GetSince on an unknown room is empty, not an error", func(t *testing.T) {
		// A client may resume into a room that has since been deleted. That
		// should degrade to "nothing missed" rather than failing the whole
		// reconnect and taking every other room down with it.
		got, err := newStore(t).GetSince(context.Background(), "never-existed", 0, 10)
		if err != nil {
			t.Fatalf("GetSince: unexpected error %v", err)
		}

		if len(got) != 0 {
			t.Errorf("got %d messages, want 0", len(got))
		}
	})

	t.Run("a message with no room is left unsequenced", func(t *testing.T) {
		// Direct and system messages belong to no room's ordered history.
		// Giving them a sequence would advance a cursor that names no room.
		store := newStore(t)

		msg := &streaming.Message{ID: "direct"}
		if err := store.Save(context.Background(), msg); err != nil {
			t.Fatalf("Save: %v", err)
		}

		if msg.Sequence != 0 {
			t.Errorf("sequence = %d, want 0 for a roomless message", msg.Sequence)
		}
	})
}
