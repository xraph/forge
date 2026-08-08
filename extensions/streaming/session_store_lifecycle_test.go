package streaming

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func newLifecycleSnapshot() *SessionSnapshot {
	return &SessionSnapshot{
		SessionID:      "session-1",
		UserID:         "user-1",
		Rooms:          []string{"room-1", "room-2"},
		Channels:       []string{"chan-1"},
		Metadata:       map[string]string{"device": "phone"},
		DisconnectedAt: time.Unix(1700000000, 0),
	}
}

func TestInMemorySessionStore_SaveAndGet(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()

	if err := store.Save(ctx, newLifecycleSnapshot(), time.Minute); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	got, err := store.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}

	want := newLifecycleSnapshot()
	if got.SessionID != want.SessionID || got.UserID != want.UserID {
		t.Errorf("Get() = %+v, want %+v", got, want)
	}

	if len(got.Rooms) != len(want.Rooms) || len(got.Channels) != len(want.Channels) {
		t.Errorf("Get() rooms/channels = %v/%v, want %v/%v", got.Rooms, got.Channels, want.Rooms, want.Channels)
	}
}

func TestInMemorySessionStore_GetMissingAndExpired(t *testing.T) {
	tests := []struct {
		name      string
		ttl       time.Duration
		save      bool
		sessionID string
	}{
		{name: "unknown session id", ttl: time.Minute, save: false, sessionID: "nope"},
		{name: "expired session", ttl: -time.Second, save: true, sessionID: "session-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemorySessionStore()
			defer store.Close()

			ctx := context.Background()

			if tt.save {
				if err := store.Save(ctx, newLifecycleSnapshot(), tt.ttl); err != nil {
					t.Fatalf("Save() = %v, want nil", err)
				}
			}

			_, err := store.Get(ctx, tt.sessionID)
			if !errors.Is(err, ErrConnectionNotFound) {
				t.Errorf("Get() error = %v, want %v", err, ErrConnectionNotFound)
			}
		})
	}
}

// TestInMemorySessionStore_GetReturnsDeepCopy pins C8: Get must not hand back the
// stored pointer, or two concurrent resumes end up mutating one another's state.
func TestInMemorySessionStore_GetReturnsDeepCopy(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SessionSnapshot)
		inspect func(*SessionSnapshot) any
		want    any
	}{
		{
			name:    "overwriting a Rooms element leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { s.Rooms[0] = "hijacked" },
			inspect: func(s *SessionSnapshot) any { return s.Rooms[0] },
			want:    "room-1",
		},
		{
			name:    "appending to Rooms leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { s.Rooms = append(s.Rooms, "extra") },
			inspect: func(s *SessionSnapshot) any { return len(s.Rooms) },
			want:    2,
		},
		{
			name:    "truncating Rooms leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { s.Rooms = s.Rooms[:1] },
			inspect: func(s *SessionSnapshot) any { return len(s.Rooms) },
			want:    2,
		},
		{
			name:    "overwriting a Channels element leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { s.Channels[0] = "hijacked" },
			inspect: func(s *SessionSnapshot) any { return s.Channels[0] },
			want:    "chan-1",
		},
		{
			name:    "overwriting a Metadata value leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { s.Metadata["device"] = "hijacked" },
			inspect: func(s *SessionSnapshot) any { return s.Metadata["device"] },
			want:    "phone",
		},
		{
			name:    "adding a Metadata key leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { s.Metadata["injected"] = "x" },
			inspect: func(s *SessionSnapshot) any { return len(s.Metadata) },
			want:    1,
		},
		{
			name:    "deleting a Metadata key leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { delete(s.Metadata, "device") },
			inspect: func(s *SessionSnapshot) any { return len(s.Metadata) },
			want:    1,
		},
		{
			name:    "overwriting a scalar field leaves the store untouched",
			mutate:  func(s *SessionSnapshot) { s.UserID = "hijacked" },
			inspect: func(s *SessionSnapshot) any { return s.UserID },
			want:    "user-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemorySessionStore()
			defer store.Close()

			ctx := context.Background()

			if err := store.Save(ctx, newLifecycleSnapshot(), time.Minute); err != nil {
				t.Fatalf("Save() = %v, want nil", err)
			}

			first, err := store.Get(ctx, "session-1")
			if err != nil {
				t.Fatalf("first Get() = %v, want nil", err)
			}

			tt.mutate(first)

			second, err := store.Get(ctx, "session-1")
			if err != nil {
				t.Fatalf("second Get() = %v, want nil", err)
			}

			if got := tt.inspect(second); got != tt.want {
				t.Errorf("after mutating the first copy, second Get() saw %v, want %v", got, tt.want)
			}

			if first == second {
				t.Error("Get() returned the same pointer twice, want independent copies")
			}
		})
	}
}

// TestInMemorySessionStore_ConcurrentResumesDoNotShareState is the -race guard for
// C8: without a deep copy, these goroutines all write the same backing array.
func TestInMemorySessionStore_ConcurrentResumesDoNotShareState(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()

	if err := store.Save(ctx, newLifecycleSnapshot(), time.Minute); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)

		go func(worker int) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				got, err := store.Get(ctx, "session-1")
				if err != nil {
					t.Errorf("worker %d: Get() = %v, want nil", worker, err)
					return
				}

				// A resuming connection rewrites its own view of the session.
				got.Rooms[0] = fmt.Sprintf("room-%d-%d", worker, j)
				got.Channels[0] = fmt.Sprintf("chan-%d-%d", worker, j)
				got.Metadata["device"] = fmt.Sprintf("device-%d", worker)
			}
		}(i)
	}

	wg.Wait()

	final, err := store.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("final Get() = %v, want nil", err)
	}

	if final.Rooms[0] != "room-1" || final.Channels[0] != "chan-1" || final.Metadata["device"] != "phone" {
		t.Errorf("stored snapshot was mutated by resumers: %+v", final)
	}
}

func TestInMemorySessionStore_Delete(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()

	if err := store.Save(ctx, newLifecycleSnapshot(), time.Minute); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	if err := store.Delete(ctx, "session-1"); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}

	if _, err := store.Get(ctx, "session-1"); !errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("Get() after Delete = %v, want %v", err, ErrConnectionNotFound)
	}
}

func TestInMemorySessionStore_CloseStopsCleanupLoop(t *testing.T) {
	before := runtime.NumGoroutine()

	store := NewInMemorySessionStore()

	if err := store.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	if n := waitForGoroutineCount(before); n > before {
		t.Errorf("goroutines after Close = %d, want <= %d (cleanup loop leaked)", n, before)
	}
}

func TestInMemorySessionStore_CloseIsIdempotent(t *testing.T) {
	store := NewInMemorySessionStore()

	for i := 0; i < 3; i++ {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() call %d = %v, want nil", i, err)
		}
	}
}

func TestInMemorySessionStore_ConcurrentCloseIsSafe(t *testing.T) {
	store := NewInMemorySessionStore()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := store.Close(); err != nil {
				t.Errorf("Close() = %v, want nil", err)
			}
		}()
	}

	wg.Wait()
}

func TestInMemorySessionStore_StartStopCyclesDoNotLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		store := NewInMemorySessionStore()

		if err := store.Close(); err != nil {
			t.Fatalf("cycle %d: Close() = %v, want nil", i, err)
		}
	}

	if n := waitForGoroutineCount(before); n > before {
		t.Errorf("goroutines after 20 start/stop cycles = %d, want <= %d", n, before)
	}
}
