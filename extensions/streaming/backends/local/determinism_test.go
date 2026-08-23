package local

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// The local stores keep rooms, members, channels, subscriptions and presence in
// maps. Go randomises map iteration, so every read accessor that walks one of
// those maps into a slice returns a different order per call unless it sorts.
// These lists are handed to callers and serialised to clients, so the order is
// observable.
//
// Twelve entries rather than two or three: a map small enough to live in one
// bucket (<= 8 entries) only rotates its iteration order, which lands on the
// right answer often enough to pass by luck.
const (
	determinismRuns = 64
	seedCount       = 12
)

// seedIDs returns ids in an order that is deliberately not their sorted order,
// so a store that echoed insertion order would still be caught.
func seedIDs(prefix string) []string {
	out := make([]string, 0, seedCount)
	for i := range seedCount {
		out = append(out, fmt.Sprintf("%s-%02d", prefix, (i*7)%seedCount))
	}

	return out
}

// assertStableSorted runs read repeatedly and requires every call to return the
// same sorted slice.
func assertStableSorted(t *testing.T, name string, read func() []string) {
	t.Helper()

	want := read()
	if len(want) != seedCount {
		t.Fatalf("%s: got %d entries, want %d", name, len(want), seedCount)
	}

	if !slices.IsSorted(want) {
		t.Errorf("%s is not sorted: %v", name, want)
	}

	for run := range determinismRuns {
		if got := read(); !slices.Equal(got, want) {
			t.Fatalf("%s: run %d is not stable\n got: %v\nwant: %v", name, run, got, want)
		}
	}
}

func TestRoomStore_ReadsAreDeterministic(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	for _, id := range seedIDs("room") {
		seedRoom(t, s, id)
	}

	assertStableSorted(t, "RoomStore.List", func() []string {
		rooms, err := s.List(ctx, nil)
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		ids := make([]string, 0, len(rooms))
		for _, r := range rooms {
			ids = append(ids, r.GetID())
		}

		return ids
	})

	// Members of one room, which is a separate map keyed by user.
	for _, uid := range seedIDs("user") {
		if err := s.AddMember(ctx, "room-00", member(uid, streaming.RoleMember)); err != nil {
			t.Fatalf("AddMember(%s): %v", uid, err)
		}
	}

	assertStableSorted(t, "RoomStore.GetMembers", func() []string {
		members, err := s.GetMembers(ctx, "room-00")
		if err != nil {
			t.Fatalf("GetMembers: %v", err)
		}

		ids := make([]string, 0, len(members))
		for _, m := range members {
			ids = append(ids, m.GetUserID())
		}

		return ids
	})
}

func TestChannelStore_ReadsAreDeterministic(t *testing.T) {
	ctx := context.Background()
	s := NewChannelStore()

	for _, id := range seedIDs("chan") {
		seedChannel(t, s, id)
	}

	assertStableSorted(t, "ChannelStore.List", func() []string {
		channels, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		ids := make([]string, 0, len(channels))
		for _, c := range channels {
			ids = append(ids, c.GetID())
		}

		return ids
	})

	for _, connID := range seedIDs("conn") {
		if err := s.AddSubscription(ctx, "chan-00", subscription(connID, "u1", nil)); err != nil {
			t.Fatalf("AddSubscription(%s): %v", connID, err)
		}
	}

	assertStableSorted(t, "ChannelStore.GetSubscriptions", func() []string {
		subs, err := s.GetSubscriptions(ctx, "chan-00")
		if err != nil {
			t.Fatalf("GetSubscriptions: %v", err)
		}

		ids := make([]string, 0, len(subs))
		for _, sub := range subs {
			ids = append(ids, sub.GetConnID())
		}

		return ids
	})
}

func TestPresenceStore_GetOnlineIsDeterministic(t *testing.T) {
	ctx := context.Background()
	s := NewPresenceStore()

	for _, uid := range seedIDs("user") {
		if err := s.SetOnline(ctx, uid, time.Minute); err != nil {
			t.Fatalf("SetOnline(%s): %v", uid, err)
		}
	}

	assertStableSorted(t, "PresenceStore.GetOnline", func() []string {
		users, err := s.GetOnline(ctx)
		if err != nil {
			t.Fatalf("GetOnline: %v", err)
		}

		return users
	})
}

func TestTypingStore_GetTypingUsersIsDeterministic(t *testing.T) {
	ctx := context.Background()
	s := NewTypingStore()

	expires := time.Now().Add(time.Minute)
	for _, uid := range seedIDs("user") {
		if err := s.SetTyping(ctx, uid, "room-1", expires); err != nil {
			t.Fatalf("SetTyping(%s): %v", uid, err)
		}
	}

	assertStableSorted(t, "TypingStore.GetTypingUsers", func() []string {
		users, err := s.GetTypingUsers(ctx, "room-1")
		if err != nil {
			t.Fatalf("GetTypingUsers: %v", err)
		}

		return users
	})
}
