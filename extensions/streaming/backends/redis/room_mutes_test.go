package redis

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// The Redis room's mute methods returned ErrInvalidRoom, so MuteUser failed
// and a muted user could keep sending.
func TestRedisRoom_Mutes(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t)
	s := NewRoomStore(client, "test:rooms")

	if err := s.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})); err != nil {
		t.Fatal(err)
	}

	room, err := s.Get(ctx, "lobby")
	if err != nil {
		t.Fatal(err)
	}

	if err := room.MuteMember(ctx, "alice", time.Hour); err != nil {
		t.Fatalf("MuteMember: %v", err)
	}

	if muted, err := room.IsMuted(ctx, "alice"); err != nil || !muted {
		t.Fatalf("IsMuted(alice) = %v, %v; want true", muted, err)
	}

	if muted, _ := room.IsMuted(ctx, "bob"); muted {
		t.Fatal("bob reported muted without a mute")
	}

	// Visible through a second Room handle, as on another node.
	other, err := s.Get(ctx, "lobby")
	if err != nil {
		t.Fatal(err)
	}

	if muted, _ := other.IsMuted(ctx, "alice"); !muted {
		t.Fatal("mute not visible through another handle on the same room")
	}

	if err := room.UnmuteMember(ctx, "alice"); err != nil {
		t.Fatalf("UnmuteMember: %v", err)
	}

	if muted, _ := room.IsMuted(ctx, "alice"); muted {
		t.Fatal("alice still muted after UnmuteMember")
	}

	// An elapsed mute is no mute, and is cleaned up when read.
	if err := room.MuteMember(ctx, "carol", -time.Second); err != nil {
		t.Fatal(err)
	}

	if muted, _ := room.IsMuted(ctx, "carol"); muted {
		t.Fatal("elapsed mute still reported")
	}

	if n, _ := client.HLen(ctx, "test:rooms:lobby:mutes").Result(); n != 0 {
		t.Fatalf("elapsed mute left %d entries behind", n)
	}

	// Deleting the room drops its mutes, so a room recreated under the same
	// id does not inherit them.
	if err := room.MuteMember(ctx, "dave", time.Hour); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, "lobby"); err != nil {
		t.Fatal(err)
	}

	if n, _ := client.Exists(ctx, "test:rooms:lobby:mutes").Result(); n != 0 {
		t.Fatal("room delete left its mutes behind")
	}
}
