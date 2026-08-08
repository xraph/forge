package local

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestTypingStore_SetAndGet(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	expires := time.Now().Add(time.Minute)

	if err := s.SetTyping(ctx, "alice", "room-1", expires); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if err := s.SetTyping(ctx, "bob", "room-1", expires); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	users, err := s.GetTypingUsers(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetTypingUsers: %v", err)
	}

	if len(users) != 2 {
		t.Fatalf("GetTypingUsers = %v, want two users", users)
	}

	for _, want := range []string{"alice", "bob"} {
		if !slices.Contains(users, want) {
			t.Errorf("GetTypingUsers = %v, missing %q", users, want)
		}
	}

	typing, err := s.IsTyping(ctx, "alice", "room-1")
	if err != nil {
		t.Fatalf("IsTyping: %v", err)
	}

	if !typing {
		t.Error("IsTyping(alice) = false, want true")
	}
}

func TestTypingStore_UnknownRoomAndUser(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	users, err := s.GetTypingUsers(ctx, "nope")
	if err != nil {
		t.Fatalf("GetTypingUsers: %v", err)
	}

	if len(users) != 0 {
		t.Errorf("GetTypingUsers(unknown room) = %v, want empty", users)
	}

	typing, err := s.IsTyping(ctx, "alice", "nope")
	if err != nil {
		t.Fatalf("IsTyping: %v", err)
	}

	if typing {
		t.Error("IsTyping in an unknown room = true, want false")
	}

	if err := s.SetTyping(ctx, "alice", "room-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if typing, _ := s.IsTyping(ctx, "bob", "room-1"); typing {
		t.Error("IsTyping(bob) = true, want false")
	}
}

func TestTypingStore_ExpiredIndicatorsAreHidden(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Minute)

	if err := s.SetTyping(ctx, "stale", "room-1", past); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if err := s.SetTyping(ctx, "fresh", "room-1", future); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	users, err := s.GetTypingUsers(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetTypingUsers: %v", err)
	}

	if len(users) != 1 || users[0] != "fresh" {
		t.Errorf("GetTypingUsers = %v, want [fresh]", users)
	}

	if typing, _ := s.IsTyping(ctx, "stale", "room-1"); typing {
		t.Error("IsTyping(stale) = true for an expired indicator, want false")
	}
}

func TestTypingStore_SetTypingRefreshesTheDeadline(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	if err := s.SetTyping(ctx, "alice", "room-1", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if typing, _ := s.IsTyping(ctx, "alice", "room-1"); typing {
		t.Fatal("IsTyping = true for an already-expired indicator")
	}

	// A fresh keystroke replaces the stale deadline rather than being ignored.
	if err := s.SetTyping(ctx, "alice", "room-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if typing, _ := s.IsTyping(ctx, "alice", "room-1"); !typing {
		t.Error("IsTyping = false after refreshing the deadline, want true")
	}
}

func TestTypingStore_RemoveTyping(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	expires := time.Now().Add(time.Minute)

	for _, u := range []string{"alice", "bob"} {
		if err := s.SetTyping(ctx, u, "room-1", expires); err != nil {
			t.Fatalf("SetTyping: %v", err)
		}
	}

	if err := s.RemoveTyping(ctx, "alice", "room-1"); err != nil {
		t.Fatalf("RemoveTyping: %v", err)
	}

	users, _ := s.GetTypingUsers(ctx, "room-1")
	if len(users) != 1 || users[0] != "bob" {
		t.Errorf("GetTypingUsers = %v, want [bob]", users)
	}

	// Removing an absent user, or removing from an unknown room, is a no-op.
	if err := s.RemoveTyping(ctx, "carol", "room-1"); err != nil {
		t.Errorf("RemoveTyping(absent user) = %v, want nil", err)
	}

	if err := s.RemoveTyping(ctx, "alice", "nope"); err != nil {
		t.Errorf("RemoveTyping(unknown room) = %v, want nil", err)
	}
}

func TestTypingStore_CleanupExpired(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Minute)

	if err := s.SetTyping(ctx, "stale", "room-1", past); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if err := s.SetTyping(ctx, "fresh", "room-1", future); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if err := s.SetTyping(ctx, "stale", "room-2", past); err != nil {
		t.Fatalf("SetTyping: %v", err)
	}

	if err := s.CleanupExpired(ctx); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}

	users, _ := s.GetTypingUsers(ctx, "room-1")
	if len(users) != 1 || users[0] != "fresh" {
		t.Errorf("room-1 typing users = %v, want [fresh]", users)
	}

	// room-2 held only an expired entry, so the room itself is reclaimed.
	empty, err := s.GetTypingUsers(ctx, "room-2")
	if err != nil {
		t.Fatalf("GetTypingUsers: %v", err)
	}

	if len(empty) != 0 {
		t.Errorf("room-2 typing users = %v, want empty", empty)
	}
}

func TestTypingStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	for _, call := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"Connect", s.Connect},
		{"Ping", s.Ping},
		{"Disconnect", s.Disconnect},
	} {
		if err := call.fn(ctx); err != nil {
			t.Errorf("%s = %v, want nil (no-op for the local backend)", call.name, err)
		}
	}
}

func TestTypingStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			for i := range 100 {
				roomID := fmt.Sprintf("room-%d", i%3)
				userID := fmt.Sprintf("u%d", w)

				_ = s.SetTyping(ctx, userID, roomID, time.Now().Add(time.Minute))
				_, _ = s.GetTypingUsers(ctx, roomID)
				_, _ = s.IsTyping(ctx, userID, roomID)
				_ = s.CleanupExpired(ctx)
				_ = s.RemoveTyping(ctx, userID, roomID)
			}
		}(w)
	}

	wg.Wait()
}
