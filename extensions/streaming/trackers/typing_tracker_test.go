package trackers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

var errStore = errors.New("store failure")

// failingTypingStore returns errStore from the write paths so the tracker's
// error propagation can be exercised. Unimplemented methods panic, which is a
// loud signal that the tracker reached a path this fake does not model.
type failingTypingStore struct {
	streaming.TypingStore

	setErr     error
	removeErr  error
	getErr     error
	cleanupErr error

	mu       sync.Mutex
	cleanups int
}

func (s *failingTypingStore) SetTyping(ctx context.Context, userID, roomID string, expiresAt time.Time) error {
	return s.setErr
}

func (s *failingTypingStore) RemoveTyping(ctx context.Context, userID, roomID string) error {
	return s.removeErr
}

func (s *failingTypingStore) GetTypingUsers(ctx context.Context, roomID string) ([]string, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}

	return nil, nil
}

func (s *failingTypingStore) IsTyping(ctx context.Context, userID, roomID string) (bool, error) {
	return false, s.getErr
}

func (s *failingTypingStore) CleanupExpired(ctx context.Context) error {
	s.mu.Lock()
	s.cleanups++
	s.mu.Unlock()

	return s.cleanupErr
}

func (s *failingTypingStore) cleanupCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cleanups
}

func newTypingTrackerWithStore(store streaming.TypingStore, opts streaming.TypingOptions) streaming.TypingTracker {
	return NewTypingTracker(store, opts, nil, nil)
}

func TestTypingTracker_StartAndStop(t *testing.T) {
	ctx := context.Background()
	store := local.NewTypingStore()
	tracker := newTypingTrackerWithStore(store, streaming.DefaultTypingOptions())

	if err := tracker.StartTyping(ctx, "alice", "room-1"); err != nil {
		t.Fatalf("StartTyping: %v", err)
	}

	typing, err := tracker.IsTyping(ctx, "alice", "room-1")
	if err != nil {
		t.Fatalf("IsTyping: %v", err)
	}

	if !typing {
		t.Error("IsTyping = false after StartTyping, want true")
	}

	users, err := tracker.GetTypingUsers(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetTypingUsers: %v", err)
	}

	if !slices.Contains(users, "alice") {
		t.Errorf("GetTypingUsers = %v, want it to contain alice", users)
	}

	if err := tracker.StopTyping(ctx, "alice", "room-1"); err != nil {
		t.Fatalf("StopTyping: %v", err)
	}

	if typing, _ := tracker.IsTyping(ctx, "alice", "room-1"); typing {
		t.Error("IsTyping = true after StopTyping, want false")
	}
}

func TestTypingTracker_IndicatorExpiresAfterTheConfiguredTimeout(t *testing.T) {
	ctx := context.Background()
	store := local.NewTypingStore()

	opts := streaming.DefaultTypingOptions()
	opts.TypingTimeout = 50 * time.Millisecond

	tracker := newTypingTrackerWithStore(store, opts)

	if err := tracker.StartTyping(ctx, "alice", "room-1"); err != nil {
		t.Fatalf("StartTyping: %v", err)
	}

	if typing, _ := tracker.IsTyping(ctx, "alice", "room-1"); !typing {
		t.Fatal("IsTyping = false immediately after StartTyping")
	}

	time.Sleep(opts.TypingTimeout + 40*time.Millisecond)

	if typing, _ := tracker.IsTyping(ctx, "alice", "room-1"); typing {
		t.Error("IsTyping = true past the typing timeout, want false")
	}
}

func TestTypingTracker_GetTypingUsersRespectsMaxTypingUsers(t *testing.T) {
	ctx := context.Background()
	store := local.NewTypingStore()

	opts := streaming.DefaultTypingOptions()
	opts.MaxTypingUsers = 3

	tracker := newTypingTrackerWithStore(store, opts)

	for i := range 10 {
		if err := tracker.StartTyping(ctx, fmt.Sprintf("u%d", i), "room-1"); err != nil {
			t.Fatalf("StartTyping: %v", err)
		}
	}

	users, err := tracker.GetTypingUsers(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetTypingUsers: %v", err)
	}

	if len(users) != 3 {
		t.Errorf("GetTypingUsers = %d users, want 3 (the configured cap)", len(users))
	}
}

func TestTypingTracker_ZeroMaxTypingUsersMeansUnlimited(t *testing.T) {
	ctx := context.Background()
	tracker := newTypingTrackerWithStore(local.NewTypingStore(), streaming.TypingOptions{
		TypingTimeout:   time.Minute,
		CleanupInterval: time.Minute,
		MaxTypingUsers:  0,
	})

	if err := tracker.StartTyping(ctx, "alice", "room-1"); err != nil {
		t.Fatalf("StartTyping: %v", err)
	}

	users, err := tracker.GetTypingUsers(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetTypingUsers: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("GetTypingUsers = %v, want [alice]", users)
	}
}

func TestTypingTracker_PropagatesStoreErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		store *failingTypingStore
		call  func(streaming.TypingTracker) error
	}{
		{
			name:  "StartTyping",
			store: &failingTypingStore{setErr: errStore},
			call:  func(tr streaming.TypingTracker) error { return tr.StartTyping(ctx, "alice", "room-1") },
		},
		{
			name:  "StopTyping",
			store: &failingTypingStore{removeErr: errStore},
			call:  func(tr streaming.TypingTracker) error { return tr.StopTyping(ctx, "alice", "room-1") },
		},
		{
			name:  "GetTypingUsers",
			store: &failingTypingStore{getErr: errStore},
			call: func(tr streaming.TypingTracker) error {
				_, err := tr.GetTypingUsers(ctx, "room-1")

				return err
			},
		},
		{
			name:  "IsTyping",
			store: &failingTypingStore{getErr: errStore},
			call: func(tr streaming.TypingTracker) error {
				_, err := tr.IsTyping(ctx, "alice", "room-1")

				return err
			},
		},
		{
			name:  "CleanupExpired",
			store: &failingTypingStore{cleanupErr: errStore},
			call:  func(tr streaming.TypingTracker) error { return tr.CleanupExpired(ctx) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTypingTrackerWithStore(tt.store, streaming.DefaultTypingOptions())

			if err := tt.call(tracker); !errors.Is(err, errStore) {
				t.Errorf("got %v, want it to wrap errStore", err)
			}
		})
	}
}

func TestTypingTracker_BroadcastTypingIsAPlaceholder(t *testing.T) {
	// BroadcastTyping is deliberately inert: broadcasting is the manager's job.
	// Pinned so that wiring it up later is a deliberate change, not a surprise.
	tracker := newTypingTrackerWithStore(local.NewTypingStore(), streaming.DefaultTypingOptions())

	if err := tracker.BroadcastTyping(context.Background(), "room-1", "alice", true); err != nil {
		t.Errorf("BroadcastTyping = %v, want nil", err)
	}
}

func TestTypingTracker_CleanupLoopRunsAndStops(t *testing.T) {
	ctx := context.Background()

	store := &failingTypingStore{}

	tracker := newTypingTrackerWithStore(store, streaming.TypingOptions{
		TypingTimeout:   time.Minute,
		CleanupInterval: 10 * time.Millisecond,
		MaxTypingUsers:  10,
	})

	if err := tracker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && store.cleanupCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	if store.cleanupCount() == 0 {
		t.Fatal("cleanup loop never ran")
	}

	if err := tracker.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// After Stop the loop must quiesce.
	settled := store.cleanupCount()
	time.Sleep(60 * time.Millisecond)

	if got := store.cleanupCount(); got > settled+1 {
		t.Errorf("cleanup ran %d more times after Stop, want it halted", got-settled)
	}
}

func TestTypingTracker_StopIsIdempotent(t *testing.T) {
	tracker := newTypingTrackerWithStore(local.NewTypingStore(), streaming.DefaultTypingOptions())

	ctx := context.Background()

	if err := tracker.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}

	if err := tracker.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestTypingTracker_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	tracker := newTypingTrackerWithStore(local.NewTypingStore(), streaming.DefaultTypingOptions())

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			userID := fmt.Sprintf("u%d", w)

			for i := range 100 {
				roomID := fmt.Sprintf("room-%d", i%3)

				_ = tracker.StartTyping(ctx, userID, roomID)
				_, _ = tracker.GetTypingUsers(ctx, roomID)
				_, _ = tracker.IsTyping(ctx, userID, roomID)
				_ = tracker.CleanupExpired(ctx)
				_ = tracker.StopTyping(ctx, userID, roomID)
			}
		}(w)
	}

	wg.Wait()
}
