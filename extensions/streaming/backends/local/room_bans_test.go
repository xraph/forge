package local

import (
	"context"
	"errors"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// LocalRoom's ban methods returned ErrInvalidRoom unconditionally, so
// Manager.BanUser, which calls them, failed for every local room.
func TestLocalRoom_BanMethodsUseTheStore(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()
	room := NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})

	// Before it is stored a room has nowhere to keep bans.
	if err := room.BanMember(ctx, "alice", "", nil); !errors.Is(err, streaming.ErrInvalidRoom) {
		t.Fatalf("BanMember on an unstored room = %v, want ErrInvalidRoom", err)
	}

	if err := s.Create(ctx, room); err != nil {
		t.Fatal(err)
	}

	if err := s.AddMember(ctx, "lobby", NewLocalMember(streaming.MemberOptions{UserID: "alice", Role: streaming.RoleMember})); err != nil {
		t.Fatal(err)
	}

	if err := room.BanMember(ctx, "alice", "spam", nil); err != nil {
		t.Fatalf("BanMember: %v", err)
	}

	if member, _ := s.IsMember(ctx, "lobby", "alice"); member {
		t.Error("alice is still a member after being banned")
	}

	if banned, err := room.IsBanned(ctx, "alice"); err != nil || !banned {
		t.Errorf("IsBanned = %v, %v; want true", banned, err)
	}

	bans, err := room.GetBannedMembers(ctx)
	if err != nil || len(bans) != 1 || bans[0].Reason != "spam" {
		t.Errorf("GetBannedMembers = %+v, %v", bans, err)
	}

	if err := room.UnbanMember(ctx, "alice"); err != nil {
		t.Fatal(err)
	}

	if banned, _ := room.IsBanned(ctx, "alice"); banned {
		t.Error("alice still banned after UnbanMember")
	}

	past := time.Now().Add(-time.Second)
	if err := room.BanMember(ctx, "bob", "", &past); err != nil {
		t.Fatal(err)
	}

	if banned, _ := room.IsBanned(ctx, "bob"); banned {
		t.Error("expired ban still reported")
	}
}
