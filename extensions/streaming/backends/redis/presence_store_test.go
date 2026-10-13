package redis

import (
	"context"
	"slices"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// These score bounds used to be built with string(rune(unixSeconds)), which
// sends Redis one garbage character instead of a number. Every call failed with
// "min or max is not a float", so expired entries were never removed and
// history-since queries always errored.

func TestPresenceStore_CleanupExpiredRemovesOnlyExpired(t *testing.T) {
	ctx := context.Background()
	store := NewPresenceStore(newTestClient(t), "presence")

	if err := store.SetOnline(ctx, "stale", -time.Minute); err != nil {
		t.Fatalf("SetOnline stale: %v", err)
	}

	if err := store.SetOnline(ctx, "live", time.Hour); err != nil {
		t.Fatalf("SetOnline live: %v", err)
	}

	if err := store.CleanupExpired(ctx, 0); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}

	online, err := store.GetOnline(ctx)
	if err != nil {
		t.Fatalf("GetOnline: %v", err)
	}

	if !slices.Equal(online, []string{"live"}) {
		t.Errorf("online = %v, want [live]", online)
	}
}

func TestPresenceStore_GetHistorySince(t *testing.T) {
	ctx := context.Background()
	store := NewPresenceStore(newTestClient(t), "presence")
	now := time.Now()

	for _, event := range []*streaming.PresenceEvent{
		{UserID: "u1", Status: "away", Timestamp: now.Add(-time.Hour)},
		{UserID: "u1", Status: "online", Timestamp: now},
	} {
		if err := store.SaveHistory(ctx, "u1", event); err != nil {
			t.Fatalf("SaveHistory: %v", err)
		}
	}

	events, err := store.GetHistorySince(ctx, "u1", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("GetHistorySince: %v", err)
	}

	if len(events) != 1 || events[0].Status != "online" {
		t.Errorf("events = %+v, want only the recent online event", events)
	}
}

func TestTypingStore_CleanupExpiredRemovesOnlyExpired(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)
	store := NewTypingStore(client, "typing")

	if err := store.SetTyping(ctx, "stale", "room-1", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("SetTyping stale: %v", err)
	}

	if err := store.SetTyping(ctx, "live", "room-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SetTyping live: %v", err)
	}

	if err := store.CleanupExpired(ctx); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}

	// Read the raw set: GetTypingUsers already hides expired entries, so it
	// cannot tell whether cleanup actually removed anything.
	members, err := client.ZRange(ctx, "typing:room-1", 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRange: %v", err)
	}

	if !slices.Equal(members, []string{"live"}) {
		t.Errorf("members = %v, want [live]", members)
	}
}
