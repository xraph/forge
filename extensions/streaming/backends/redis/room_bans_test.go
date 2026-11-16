package redis

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// The Redis backend recorded a ban but left the user a member, ignored
// ExpiresAt so a temporary ban never ran out, and its Room's ban methods
// returned ErrInvalidRoom.
func TestRoomStore_Bans(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore(newTestClient(t), "test:rooms")

	if err := s.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})); err != nil {
		t.Fatal(err)
	}

	for _, uid := range []string{"alice", "bob"} {
		if err := s.AddMember(ctx, "lobby", local.NewLocalMember(streaming.MemberOptions{UserID: uid, Role: streaming.RoleMember})); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.BanMember(ctx, "lobby", "alice", streaming.RoomBan{UserID: "alice", RoomID: "lobby", BannedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if member, _ := s.IsMember(ctx, "lobby", "alice"); member {
		t.Error("alice is still a member after being banned")
	}

	if banned, err := s.IsBanned(ctx, "lobby", "alice"); err != nil || !banned {
		t.Errorf("IsBanned(alice) = %v, %v; want true", banned, err)
	}

	// An expired ban is no ban: not reported, not listed.
	past := time.Now().Add(-time.Minute)
	if err := s.BanMember(ctx, "lobby", "bob", streaming.RoomBan{UserID: "bob", RoomID: "lobby", ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}

	if banned, _ := s.IsBanned(ctx, "lobby", "bob"); banned {
		t.Error("expired ban on bob still reported")
	}

	bans, err := s.GetBans(ctx, "lobby")
	if err != nil {
		t.Fatal(err)
	}

	if len(bans) != 1 || bans[0].UserID != "alice" {
		t.Errorf("GetBans = %+v, want only alice", bans)
	}

	// The Room object answers through the store.
	room, err := s.Get(ctx, "lobby")
	if err != nil {
		t.Fatal(err)
	}

	if banned, err := room.IsBanned(ctx, "alice"); err != nil || !banned {
		t.Errorf("room.IsBanned(alice) = %v, %v; want true", banned, err)
	}

	until := time.Now().Add(time.Hour)
	if err := room.BanMember(ctx, "carol", "spam", &until); err != nil {
		t.Fatalf("room.BanMember: %v", err)
	}

	if err := room.UnbanMember(ctx, "alice"); err != nil {
		t.Fatalf("room.UnbanMember: %v", err)
	}

	listed, err := room.GetBannedMembers(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(listed) != 1 || listed[0].UserID != "carol" || listed[0].Reason != "spam" {
		t.Errorf("GetBannedMembers = %+v, want only carol", listed)
	}
}
