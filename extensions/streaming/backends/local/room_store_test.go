package local

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func newRoom(t *testing.T, id string) *LocalRoom {
	t.Helper()

	return NewRoom(streaming.RoomOptions{ID: id, Name: id, Owner: "owner-" + id})
}

// seedRoom creates a room in the store and returns it.
func seedRoom(t *testing.T, s streaming.RoomStore, id string) *LocalRoom {
	t.Helper()

	room := newRoom(t, id)
	if err := s.Create(context.Background(), room); err != nil {
		t.Fatalf("Create(%s): %v", id, err)
	}

	return room
}

func member(userID, role string) streaming.Member {
	return NewLocalMember(streaming.MemberOptions{UserID: userID, Role: role})
}

// --- CRUD ------------------------------------------------------------------

func TestRoomStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	room := seedRoom(t, s, "room-1")

	got, err := s.Get(ctx, "room-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.GetID() != room.GetID() {
		t.Errorf("Get returned %q, want %q", got.GetID(), room.GetID())
	}
}

func TestRoomStore_CreateDuplicate(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	if err := s.Create(ctx, newRoom(t, "room-1")); !errors.Is(err, streaming.ErrRoomAlreadyExists) {
		t.Errorf("Create duplicate = %v, want ErrRoomAlreadyExists", err)
	}
}

func TestRoomStore_GetMissing(t *testing.T) {
	s := NewRoomStore()

	if _, err := s.Get(context.Background(), "nope"); !errors.Is(err, streaming.ErrRoomNotFound) {
		t.Errorf("Get missing = %v, want ErrRoomNotFound", err)
	}
}

func TestRoomStore_Update(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	room := seedRoom(t, s, "room-1")
	before := room.GetUpdated()

	time.Sleep(time.Millisecond)

	updates := map[string]any{
		"name":        "renamed",
		"description": "a description",
		"metadata":    map[string]any{"k": "v"},
	}

	if err := s.Update(ctx, "room-1", updates); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if room.GetName() != "renamed" {
		t.Errorf("name = %q, want renamed", room.GetName())
	}

	if room.GetDescription() != "a description" {
		t.Errorf("description = %q, want %q", room.GetDescription(), "a description")
	}

	if room.GetMetadata()["k"] != "v" {
		t.Errorf("metadata = %v, want k=v", room.GetMetadata())
	}

	if !room.GetUpdated().After(before) {
		t.Error("Update did not advance the updated timestamp")
	}
}

func TestRoomStore_UpdateIgnoresUnknownAndMistypedKeys(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	room := seedRoom(t, s, "room-1")
	original := room.GetName()

	// A wrongly typed "name" and an unrecognised key are both silently ignored
	// rather than rejected, so callers get no signal that an update was a no-op.
	err := s.Update(ctx, "room-1", map[string]any{"name": 42, "unknown": "x"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if room.GetName() != original {
		t.Errorf("name = %q, want it unchanged (%q)", room.GetName(), original)
	}
}

func TestRoomStore_UpdateMissing(t *testing.T) {
	s := NewRoomStore()

	err := s.Update(context.Background(), "nope", map[string]any{"name": "x"})
	if !errors.Is(err, streaming.ErrRoomNotFound) {
		t.Errorf("Update missing = %v, want ErrRoomNotFound", err)
	}
}

func TestRoomStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	if err := s.Delete(ctx, "room-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Get(ctx, "room-1"); !errors.Is(err, streaming.ErrRoomNotFound) {
		t.Errorf("Get after Delete = %v, want ErrRoomNotFound", err)
	}

	if err := s.Delete(ctx, "room-1"); !errors.Is(err, streaming.ErrRoomNotFound) {
		t.Errorf("second Delete = %v, want ErrRoomNotFound", err)
	}
}

func TestRoomStore_DeleteClearsInvites(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	if err := s.SaveInvite(ctx, "room-1", &streaming.Invite{Code: "code-1", RoomID: "room-1"}); err != nil {
		t.Fatalf("SaveInvite: %v", err)
	}

	if err := s.Delete(ctx, "room-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.GetInvite(ctx, "code-1"); !errors.Is(err, streaming.ErrInviteNotFound) {
		t.Errorf("GetInvite after room deletion = %v, want ErrInviteNotFound", err)
	}
}

func TestRoomStore_DeleteManyClearsEverything(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	if err := s.SaveInvite(ctx, "room-1", &streaming.Invite{Code: "code-1", RoomID: "room-1"}); err != nil {
		t.Fatalf("SaveInvite: %v", err)
	}

	if err := s.DeleteMany(ctx, []string{"room-1"}); err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}

	if _, err := s.GetInvite(ctx, "code-1"); !errors.Is(err, streaming.ErrInviteNotFound) {
		t.Errorf("GetInvite = %v, want ErrInviteNotFound", err)
	}
}

func TestRoomStore_DeleteManyStopsAtTheFirstMissingRoom(t *testing.T) {
	// DeleteMany is not atomic: rooms before the missing one are already gone
	// when the error is returned.
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")
	seedRoom(t, s, "room-2")

	err := s.DeleteMany(ctx, []string{"room-1", "missing", "room-2"})
	if !errors.Is(err, streaming.ErrRoomNotFound) {
		t.Fatalf("DeleteMany = %v, want ErrRoomNotFound", err)
	}

	if ok, _ := s.Exists(ctx, "room-1"); ok {
		t.Error("room-1 survived; expected the partial delete to have removed it")
	}

	if ok, _ := s.Exists(ctx, "room-2"); !ok {
		t.Error("room-2 was deleted; expected DeleteMany to stop at the missing room")
	}
}

func TestRoomStore_ListAndExists(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")
	seedRoom(t, s, "room-2")

	rooms, err := s.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(rooms) != 2 {
		t.Errorf("List = %d rooms, want 2", len(rooms))
	}

	if ok, _ := s.Exists(ctx, "room-1"); !ok {
		t.Error("Exists(room-1) = false, want true")
	}

	if ok, _ := s.Exists(ctx, "nope"); ok {
		t.Error("Exists(nope) = true, want false")
	}
}

func TestRoomStore_CreateManyStopsAtFirstError(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	rooms := []streaming.Room{newRoom(t, "a"), newRoom(t, "b"), newRoom(t, "a")}

	if err := s.CreateMany(ctx, rooms); !errors.Is(err, streaming.ErrRoomAlreadyExists) {
		t.Fatalf("CreateMany = %v, want ErrRoomAlreadyExists", err)
	}

	count, _ := s.GetRoomCount(ctx)
	if count != 2 {
		t.Errorf("room count = %d, want 2 (the rooms created before the failure)", count)
	}
}

// --- Membership ------------------------------------------------------------

func TestRoomStore_Membership(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	if err := s.AddMember(ctx, "room-1", member("alice", streaming.RoleMember)); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	ok, err := s.IsMember(ctx, "room-1", "alice")
	if err != nil {
		t.Fatalf("IsMember: %v", err)
	}

	if !ok {
		t.Error("IsMember(alice) = false, want true")
	}

	count, err := s.MemberCount(ctx, "room-1")
	if err != nil {
		t.Fatalf("MemberCount: %v", err)
	}

	if count != 1 {
		t.Errorf("MemberCount = %d, want 1", count)
	}

	got, err := s.GetMember(ctx, "room-1", "alice")
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}

	if got.GetRole() != streaming.RoleMember {
		t.Errorf("role = %q, want %q", got.GetRole(), streaming.RoleMember)
	}

	if err := s.RemoveMember(ctx, "room-1", "alice"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	if ok, _ := s.IsMember(ctx, "room-1", "alice"); ok {
		t.Error("IsMember(alice) = true after RemoveMember, want false")
	}
}

func TestRoomStore_MembershipErrors(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	if err := s.AddMember(ctx, "room-1", member("alice", streaming.RoleMember)); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	tests := []struct {
		name string
		call func() error
		want error
	}{
		{
			name: "add member to missing room",
			call: func() error { return s.AddMember(ctx, "nope", member("bob", "member")) },
			want: streaming.ErrRoomNotFound,
		},
		{
			name: "add the same member twice",
			call: func() error { return s.AddMember(ctx, "room-1", member("alice", "member")) },
			want: streaming.ErrAlreadyRoomMember,
		},
		{
			name: "remove member from missing room",
			call: func() error { return s.RemoveMember(ctx, "nope", "alice") },
			want: streaming.ErrRoomNotFound,
		},
		{
			name: "remove a non-member",
			call: func() error { return s.RemoveMember(ctx, "room-1", "carol") },
			want: streaming.ErrNotRoomMember,
		},
		{
			name: "get members of a missing room",
			call: func() error { _, err := s.GetMembers(ctx, "nope"); return err },
			want: streaming.ErrRoomNotFound,
		},
		{
			name: "get a non-member",
			call: func() error { _, err := s.GetMember(ctx, "room-1", "carol"); return err },
			want: streaming.ErrNotRoomMember,
		},
		{
			name: "member count of a missing room",
			call: func() error { _, err := s.MemberCount(ctx, "nope"); return err },
			want: streaming.ErrRoomNotFound,
		},
		{
			name: "is-member on a missing room",
			call: func() error { _, err := s.IsMember(ctx, "nope", "alice"); return err },
			want: streaming.ErrRoomNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRoomStore_GetUserRooms(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	for _, id := range []string{"room-1", "room-2", "room-3"} {
		seedRoom(t, s, id)
	}

	if err := s.AddMember(ctx, "room-1", member("alice", streaming.RoleOwner)); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if err := s.AddMember(ctx, "room-2", member("alice", streaming.RoleMember)); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if err := s.AddMember(ctx, "room-3", member("bob", streaming.RoleMember)); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	rooms, err := s.GetUserRooms(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserRooms: %v", err)
	}

	if len(rooms) != 2 {
		t.Errorf("GetUserRooms(alice) = %d rooms, want 2", len(rooms))
	}

	byRole, err := s.GetUserRoomsByRole(ctx, "alice", streaming.RoleOwner)
	if err != nil {
		t.Fatalf("GetUserRoomsByRole: %v", err)
	}

	if len(byRole) != 1 || byRole[0].GetID() != "room-1" {
		t.Errorf("GetUserRoomsByRole(alice, owner) = %v, want [room-1]", byRole)
	}

	common, err := s.GetCommonRooms(ctx, "alice", "bob")
	if err != nil {
		t.Fatalf("GetCommonRooms: %v", err)
	}

	if len(common) != 0 {
		t.Errorf("GetCommonRooms = %d, want 0", len(common))
	}

	if err := s.AddMember(ctx, "room-1", member("bob", streaming.RoleMember)); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	common, _ = s.GetCommonRooms(ctx, "alice", "bob")
	if len(common) != 1 || common[0].GetID() != "room-1" {
		t.Errorf("GetCommonRooms = %v, want [room-1]", common)
	}
}

func TestRoomStore_TotalMembers(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")
	seedRoom(t, s, "room-2")

	for _, u := range []string{"a", "b"} {
		if err := s.AddMember(ctx, "room-1", member(u, "member")); err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}

	if err := s.AddMember(ctx, "room-2", member("c", "member")); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	total, err := s.GetTotalMembers(ctx)
	if err != nil {
		t.Fatalf("GetTotalMembers: %v", err)
	}

	if total != 3 {
		t.Errorf("GetTotalMembers = %d, want 3", total)
	}
}

// --- Search and discovery --------------------------------------------------

func TestRoomStore_Search(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	general := NewRoom(streaming.RoomOptions{ID: "r1", Name: "General Chat", Owner: "alice"})
	random := NewRoom(streaming.RoomOptions{ID: "r2", Name: "Random", Description: "general nonsense", Owner: "bob"})
	private := NewRoom(streaming.RoomOptions{ID: "r3", Name: "General Secrets", Owner: "alice", Private: true})

	for _, r := range []*LocalRoom{general, random, private} {
		if err := s.Create(ctx, r); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	tests := []struct {
		name    string
		query   string
		filters map[string]any
		want    int
	}{
		{name: "matches name case-insensitively", query: "general", want: 3},
		{name: "matches description", query: "nonsense", want: 1},
		{name: "no match", query: "zzz", want: 0},
		{name: "empty query matches everything", query: "", want: 3},
		{name: "filter by owner", query: "general", filters: map[string]any{"owner": "alice"}, want: 2},
		{name: "filter by private", query: "general", filters: map[string]any{"private": true}, want: 1},
		{name: "filter by public", query: "general", filters: map[string]any{"private": false}, want: 2},
		{name: "filter by archived", query: "general", filters: map[string]any{"archived": true}, want: 0},
		{name: "filter by category", query: "general", filters: map[string]any{"category": "none"}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Search(ctx, tt.query, tt.filters)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}

			if len(got) != tt.want {
				t.Errorf("Search(%q, %v) = %d rooms, want %d", tt.query, tt.filters, len(got), tt.want)
			}
		})
	}
}

func TestRoomStore_FindByTagAndCategory(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	room := seedRoom(t, s, "room-1")

	if err := room.AddTag(ctx, "gaming"); err != nil {
		t.Fatalf("AddTag: %v", err)
	}

	if err := room.SetCategory(ctx, "social"); err != nil {
		t.Fatalf("SetCategory: %v", err)
	}

	seedRoom(t, s, "room-2")

	tagged, err := s.FindByTag(ctx, "gaming")
	if err != nil {
		t.Fatalf("FindByTag: %v", err)
	}

	if len(tagged) != 1 || tagged[0].GetID() != "room-1" {
		t.Errorf("FindByTag(gaming) = %v, want [room-1]", tagged)
	}

	if none, _ := s.FindByTag(ctx, "absent"); len(none) != 0 {
		t.Errorf("FindByTag(absent) = %d, want 0", len(none))
	}

	categorised, err := s.FindByCategory(ctx, "social")
	if err != nil {
		t.Fatalf("FindByCategory: %v", err)
	}

	if len(categorised) != 1 || categorised[0].GetID() != "room-1" {
		t.Errorf("FindByCategory(social) = %v, want [room-1]", categorised)
	}
}

func TestRoomStore_GetPublicRooms(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	public := seedRoom(t, s, "public")

	private := NewRoom(streaming.RoomOptions{ID: "private", Name: "private", Private: true})
	if err := s.Create(ctx, private); err != nil {
		t.Fatalf("Create: %v", err)
	}

	archived := seedRoom(t, s, "archived")
	if err := archived.Archive(ctx); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	rooms, err := s.GetPublicRooms(ctx, 0)
	if err != nil {
		t.Fatalf("GetPublicRooms: %v", err)
	}

	if len(rooms) != 1 || rooms[0].GetID() != public.GetID() {
		t.Errorf("GetPublicRooms = %v, want only the public, unarchived room", rooms)
	}

	// The limit is applied after sorting newest-first.
	seedRoom(t, s, "public-2")

	limited, err := s.GetPublicRooms(ctx, 1)
	if err != nil {
		t.Fatalf("GetPublicRooms: %v", err)
	}

	if len(limited) != 1 {
		t.Errorf("GetPublicRooms(limit 1) = %d rooms, want 1", len(limited))
	}
}

func TestRoomStore_GetArchivedRooms(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	archived := seedRoom(t, s, "archived")
	seedRoom(t, s, "active")

	if err := archived.Archive(ctx); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	for _, id := range []string{"archived", "active"} {
		if err := s.AddMember(ctx, id, member("alice", "member")); err != nil {
			t.Fatalf("AddMember: %v", err)
		}
	}

	rooms, err := s.GetArchivedRooms(ctx, "alice")
	if err != nil {
		t.Fatalf("GetArchivedRooms: %v", err)
	}

	if len(rooms) != 1 || rooms[0].GetID() != "archived" {
		t.Errorf("GetArchivedRooms(alice) = %v, want [archived]", rooms)
	}

	// A non-member sees nothing, even though the room is archived.
	if none, _ := s.GetArchivedRooms(ctx, "bob"); len(none) != 0 {
		t.Errorf("GetArchivedRooms(bob) = %d, want 0", len(none))
	}
}

// --- Bans ------------------------------------------------------------------

func TestRoomStore_BanRemovesMembership(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	if err := s.AddMember(ctx, "room-1", member("alice", "member")); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	ban := streaming.RoomBan{UserID: "alice", RoomID: "room-1", Reason: "spam", BannedBy: "mod"}
	if err := s.BanMember(ctx, "room-1", "alice", ban); err != nil {
		t.Fatalf("BanMember: %v", err)
	}

	if ok, _ := s.IsMember(ctx, "room-1", "alice"); ok {
		t.Error("banned user is still a member")
	}

	banned, err := s.IsBanned(ctx, "room-1", "alice")
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}

	if !banned {
		t.Error("IsBanned = false, want true")
	}

	bans, err := s.GetBans(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetBans: %v", err)
	}

	if len(bans) != 1 || bans[0].Reason != "spam" {
		t.Errorf("GetBans = %v, want one ban with reason spam", bans)
	}
}

func TestRoomStore_Unban(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	ban := streaming.RoomBan{UserID: "alice", RoomID: "room-1", BannedBy: "mod"}
	if err := s.BanMember(ctx, "room-1", "alice", ban); err != nil {
		t.Fatalf("BanMember: %v", err)
	}

	if err := s.UnbanMember(ctx, "room-1", "alice"); err != nil {
		t.Fatalf("UnbanMember: %v", err)
	}

	if banned, _ := s.IsBanned(ctx, "room-1", "alice"); banned {
		t.Error("IsBanned = true after unban, want false")
	}

	// Unbanning someone who is not banned is a no-op, not an error.
	if err := s.UnbanMember(ctx, "room-1", "carol"); err != nil {
		t.Errorf("UnbanMember(non-banned) = %v, want nil", err)
	}
}

func TestRoomStore_ExpiredBansAreNotReported(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	past := time.Now().Add(-time.Hour)
	ban := streaming.RoomBan{UserID: "alice", RoomID: "room-1", BannedBy: "mod", ExpiresAt: &past}

	if err := s.BanMember(ctx, "room-1", "alice", ban); err != nil {
		t.Fatalf("BanMember: %v", err)
	}

	banned, err := s.IsBanned(ctx, "room-1", "alice")
	if err != nil {
		t.Fatalf("IsBanned: %v", err)
	}

	if banned {
		t.Error("IsBanned = true for an expired ban, want false")
	}

	bans, _ := s.GetBans(ctx, "room-1")
	if len(bans) != 0 {
		t.Errorf("GetBans = %v, want no active bans", bans)
	}
}

func TestRoomStore_IsBannedIsSafeUnderConcurrentReads(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	past := time.Now().Add(-time.Hour)

	for i := range 32 {
		userID := fmt.Sprintf("u%d", i)
		ban := streaming.RoomBan{UserID: userID, RoomID: "room-1", BannedBy: "mod", ExpiresAt: &past}

		if err := s.BanMember(ctx, "room-1", userID, ban); err != nil {
			t.Fatalf("BanMember: %v", err)
		}
	}

	var wg sync.WaitGroup

	for range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range 32 {
				_, _ = s.IsBanned(ctx, "room-1", fmt.Sprintf("u%d", i))
			}
		}()
	}

	wg.Wait()
}

func TestRoomStore_BanErrorsOnMissingRoom(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "ban", call: func() error { return s.BanMember(ctx, "nope", "u", streaming.RoomBan{}) }},
		{name: "unban", call: func() error { return s.UnbanMember(ctx, "nope", "u") }},
		{name: "get bans", call: func() error { _, err := s.GetBans(ctx, "nope"); return err }},
		{name: "is banned", call: func() error { _, err := s.IsBanned(ctx, "nope", "u"); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, streaming.ErrRoomNotFound) {
				t.Errorf("got %v, want ErrRoomNotFound", err)
			}
		})
	}
}

// --- Invites ---------------------------------------------------------------

func TestRoomStore_InviteLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	invite := &streaming.Invite{Code: "abc", RoomID: "room-1", CreatedBy: "owner", MaxUses: 5}
	if err := s.SaveInvite(ctx, "room-1", invite); err != nil {
		t.Fatalf("SaveInvite: %v", err)
	}

	got, err := s.GetInvite(ctx, "abc")
	if err != nil {
		t.Fatalf("GetInvite: %v", err)
	}

	if got.Code != "abc" {
		t.Errorf("invite code = %q, want abc", got.Code)
	}

	list, err := s.ListInvites(ctx, "room-1")
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}

	if len(list) != 1 {
		t.Errorf("ListInvites = %d, want 1", len(list))
	}

	if err := s.DeleteInvite(ctx, "abc"); err != nil {
		t.Fatalf("DeleteInvite: %v", err)
	}

	if _, err := s.GetInvite(ctx, "abc"); !errors.Is(err, streaming.ErrInviteNotFound) {
		t.Errorf("GetInvite after delete = %v, want ErrInviteNotFound", err)
	}

	if err := s.DeleteInvite(ctx, "abc"); !errors.Is(err, streaming.ErrInviteNotFound) {
		t.Errorf("second DeleteInvite = %v, want ErrInviteNotFound", err)
	}
}

func TestRoomStore_InviteExpiry(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	seedRoom(t, s, "room-1")

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	tests := []struct {
		name    string
		invite  *streaming.Invite
		wantErr error
	}{
		{
			name:    "expired by time",
			invite:  &streaming.Invite{Code: "expired", RoomID: "room-1", ExpiresAt: &past},
			wantErr: streaming.ErrInviteExpired,
		},
		{
			name:   "not yet expired",
			invite: &streaming.Invite{Code: "live", RoomID: "room-1", ExpiresAt: &future},
		},
		{
			name:    "exhausted by max uses",
			invite:  &streaming.Invite{Code: "used-up", RoomID: "room-1", MaxUses: 2, UsedCount: 2},
			wantErr: streaming.ErrInviteExpired,
		},
		{
			name:   "uses remaining",
			invite: &streaming.Invite{Code: "has-uses", RoomID: "room-1", MaxUses: 2, UsedCount: 1},
		},
		{
			name:   "unlimited uses",
			invite: &streaming.Invite{Code: "unlimited", RoomID: "room-1", MaxUses: 0, UsedCount: 99},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.SaveInvite(ctx, "room-1", tt.invite); err != nil {
				t.Fatalf("SaveInvite: %v", err)
			}

			_, err := s.GetInvite(ctx, tt.invite.Code)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("GetInvite = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Errorf("GetInvite = %v, want nil", err)
			}
		})
	}

	// ListInvites applies the same expiry rules as GetInvite.
	list, err := s.ListInvites(ctx, "room-1")
	if err != nil {
		t.Fatalf("ListInvites: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("ListInvites = %d, want the 3 usable invites", len(list))
	}
}

func TestRoomStore_InviteErrorsOnMissingRoom(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	if err := s.SaveInvite(ctx, "nope", &streaming.Invite{Code: "x"}); !errors.Is(err, streaming.ErrRoomNotFound) {
		t.Errorf("SaveInvite = %v, want ErrRoomNotFound", err)
	}

	if _, err := s.ListInvites(ctx, "nope"); !errors.Is(err, streaming.ErrRoomNotFound) {
		t.Errorf("ListInvites = %v, want ErrRoomNotFound", err)
	}

	if _, err := s.GetInvite(ctx, "nope"); !errors.Is(err, streaming.ErrInviteNotFound) {
		t.Errorf("GetInvite = %v, want ErrInviteNotFound", err)
	}
}

// --- LocalRoom -------------------------------------------------------------

func TestLocalRoom_Accessors(t *testing.T) {
	created := time.Now()

	room := NewRoom(streaming.RoomOptions{
		ID:          "r1",
		Name:        "Room One",
		Description: "desc",
		Owner:       "alice",
		Metadata:    map[string]any{"k": "v"},
	})

	if room.GetID() != "r1" || room.GetName() != "Room One" || room.GetDescription() != "desc" {
		t.Errorf("identity accessors = %q/%q/%q", room.GetID(), room.GetName(), room.GetDescription())
	}

	if room.GetOwner() != "alice" {
		t.Errorf("GetOwner = %q, want alice", room.GetOwner())
	}

	if room.GetCreated().Before(created.Add(-time.Second)) {
		t.Errorf("GetCreated = %v, want roughly now", room.GetCreated())
	}

	if room.GetMetadata()["k"] != "v" {
		t.Errorf("GetMetadata = %v, want k=v", room.GetMetadata())
	}
}

func TestLocalRoom_NewRoomHonoursOptions(t *testing.T) {
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "Room One", Private: true, MaxMembers: 10})

	if !room.IsPrivate() {
		t.Error("IsPrivate = false, want true")
	}

	if room.GetMaxMembers() != 10 {
		t.Errorf("GetMaxMembers = %d, want 10", room.GetMaxMembers())
	}
}

func TestLocalRoom_StateToggles(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1"})

	if err := room.SetPrivate(ctx, true); err != nil {
		t.Fatalf("SetPrivate: %v", err)
	}

	if !room.IsPrivate() {
		t.Error("IsPrivate = false after SetPrivate(true)")
	}

	if err := room.SetMaxMembers(ctx, 42); err != nil {
		t.Fatalf("SetMaxMembers: %v", err)
	}

	if room.GetMaxMembers() != 42 {
		t.Errorf("GetMaxMembers = %d, want 42", room.GetMaxMembers())
	}

	if err := room.Archive(ctx); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if !room.IsArchived() {
		t.Error("IsArchived = false after Archive")
	}

	if err := room.Unarchive(ctx); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}

	if room.IsArchived() {
		t.Error("IsArchived = true after Unarchive")
	}

	if err := room.Lock(ctx, "maintenance"); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if !room.IsLocked() {
		t.Error("IsLocked = false after Lock")
	}

	if err := room.Unlock(ctx); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if room.IsLocked() {
		t.Error("IsLocked = true after Unlock")
	}
}

func TestLocalRoom_Tags(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1"})

	if err := room.AddTag(ctx, "a"); err != nil {
		t.Fatalf("AddTag: %v", err)
	}

	if err := room.AddTag(ctx, "b"); err != nil {
		t.Fatalf("AddTag: %v", err)
	}

	if got := room.GetTags(); len(got) != 2 {
		t.Errorf("GetTags = %v, want two tags", got)
	}

	if err := room.RemoveTag(ctx, "a"); err != nil {
		t.Fatalf("RemoveTag: %v", err)
	}

	got := room.GetTags()
	if len(got) != 1 || got[0] != "b" {
		t.Errorf("GetTags = %v, want [b]", got)
	}

	// Removing an absent tag is a no-op.
	if err := room.RemoveTag(ctx, "absent"); err != nil {
		t.Errorf("RemoveTag(absent) = %v, want nil", err)
	}
}

func TestLocalRoom_SlowMode(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1"})

	if got := room.GetSlowMode(ctx); got != 0 {
		t.Errorf("GetSlowMode = %d, want 0 by default", got)
	}

	if err := room.SetSlowMode(ctx, 30); err != nil {
		t.Fatalf("SetSlowMode: %v", err)
	}

	if got := room.GetSlowMode(ctx); got != 30 {
		t.Errorf("GetSlowMode = %d, want 30", got)
	}
}

func TestLocalRoom_PinnedMessages(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1"})

	if err := room.PinMessage(ctx, "m1"); err != nil {
		t.Fatalf("PinMessage: %v", err)
	}

	if err := room.PinMessage(ctx, "m2"); err != nil {
		t.Fatalf("PinMessage: %v", err)
	}

	pinned, err := room.GetPinnedMessages(ctx)
	if err != nil {
		t.Fatalf("GetPinnedMessages: %v", err)
	}

	if len(pinned) != 2 {
		t.Errorf("GetPinnedMessages = %v, want two entries", pinned)
	}

	if err := room.UnpinMessage(ctx, "m1"); err != nil {
		t.Fatalf("UnpinMessage: %v", err)
	}

	pinned, _ = room.GetPinnedMessages(ctx)
	if len(pinned) != 1 || pinned[0] != "m2" {
		t.Errorf("GetPinnedMessages = %v, want [m2]", pinned)
	}
}

func TestLocalRoom_Mute(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1"})

	if err := room.MuteMember(ctx, "alice", time.Hour); err != nil {
		t.Fatalf("MuteMember: %v", err)
	}

	muted, err := room.IsMuted(ctx, "alice")
	if err != nil {
		t.Fatalf("IsMuted: %v", err)
	}

	if !muted {
		t.Error("IsMuted(alice) = false, want true")
	}

	if muted, _ := room.IsMuted(ctx, "bob"); muted {
		t.Error("IsMuted(bob) = true, want false")
	}

	if err := room.UnmuteMember(ctx, "alice"); err != nil {
		t.Fatalf("UnmuteMember: %v", err)
	}

	if muted, _ := room.IsMuted(ctx, "alice"); muted {
		t.Error("IsMuted(alice) = true after unmute, want false")
	}
}

func TestLocalRoom_MuteExpires(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1"})

	if err := room.MuteMember(ctx, "alice", -time.Hour); err != nil {
		t.Fatalf("MuteMember: %v", err)
	}

	muted, err := room.IsMuted(ctx, "alice")
	if err != nil {
		t.Fatalf("IsMuted: %v", err)
	}

	if muted {
		t.Error("IsMuted = true for an elapsed mute, want false")
	}
}

func TestLocalRoom_ReadMarkers(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1"})

	if got, err := room.GetLastReadMessage(ctx, "alice"); err != nil || got != "" {
		t.Errorf("GetLastReadMessage before any read = %q/%v, want \"\"/nil", got, err)
	}

	if err := room.MarkAsRead(ctx, "alice", "m5"); err != nil {
		t.Fatalf("MarkAsRead: %v", err)
	}

	got, err := room.GetLastReadMessage(ctx, "alice")
	if err != nil {
		t.Fatalf("GetLastReadMessage: %v", err)
	}

	if got != "m5" {
		t.Errorf("GetLastReadMessage = %q, want m5", got)
	}
}

func TestLocalRoom_TransferOwnership(t *testing.T) {
	ctx := context.Background()
	room := NewRoom(streaming.RoomOptions{ID: "r1", Name: "r1", Owner: "alice"})

	if err := room.TransferOwnership(ctx, "bob"); err != nil {
		t.Fatalf("TransferOwnership: %v", err)
	}

	if room.GetOwner() != "bob" {
		t.Errorf("GetOwner = %q, want bob", room.GetOwner())
	}
}

// --- LocalMember -----------------------------------------------------------

func TestLocalMember_Accessors(t *testing.T) {
	m := NewLocalMember(streaming.MemberOptions{
		UserID:      "alice",
		Role:        streaming.RoleAdmin,
		Permissions: []string{streaming.PermissionSendMessage},
		Metadata:    map[string]any{"k": "v"},
	})

	if m.GetUserID() != "alice" {
		t.Errorf("GetUserID = %q, want alice", m.GetUserID())
	}

	if m.GetRole() != streaming.RoleAdmin {
		t.Errorf("GetRole = %q, want %q", m.GetRole(), streaming.RoleAdmin)
	}

	if !m.HasPermission(streaming.PermissionSendMessage) {
		t.Error("HasPermission(send_message) = false, want true")
	}

	if m.HasPermission(streaming.PermissionManageRoom) {
		t.Error("HasPermission(manage_room) = true, want false")
	}

	if m.GetMetadata()["k"] != "v" {
		t.Errorf("GetMetadata = %v, want k=v", m.GetMetadata())
	}

	if m.GetJoinedAt().IsZero() {
		t.Error("GetJoinedAt is zero, want the construction time")
	}
}

func TestLocalMember_GetPermissionsReturnsACopy(t *testing.T) {
	m := NewLocalMember(streaming.MemberOptions{
		UserID:      "alice",
		Permissions: []string{"a", "b"},
	})

	perms := m.GetPermissions()
	perms[0] = "mutated"

	if m.HasPermission("mutated") {
		t.Error("GetPermissions handed out the internal slice; mutating it changed the member")
	}
}

func TestLocalMember_GetMetadataReturnsACopy(t *testing.T) {
	m := NewLocalMember(streaming.MemberOptions{
		UserID:   "alice",
		Metadata: map[string]any{"k": "v"},
	})

	meta := m.GetMetadata()
	meta["k"] = "mutated"

	if m.GetMetadata()["k"] != "v" {
		t.Error("GetMetadata handed out the internal map; mutating it changed the member")
	}
}

func TestLocalMember_SetRoleAndMetadata(t *testing.T) {
	m := NewLocalMember(streaming.MemberOptions{UserID: "alice", Role: streaming.RoleMember})

	m.SetRole(streaming.RoleAdmin)

	if m.GetRole() != streaming.RoleAdmin {
		t.Errorf("GetRole = %q, want %q", m.GetRole(), streaming.RoleAdmin)
	}

	// SetMetadata lazily creates the map when the member was built without one.
	m.SetMetadata("k", "v")

	if m.GetMetadata()["k"] != "v" {
		t.Errorf("GetMetadata = %v, want k=v", m.GetMetadata())
	}
}

func TestLocalMember_RevokePermission(t *testing.T) {
	m := NewLocalMember(streaming.MemberOptions{
		UserID:      "alice",
		Permissions: []string{"a", "b", "c"},
	})

	m.RevokePermission("b")

	if m.HasPermission("b") {
		t.Error("HasPermission(b) = true after revoke")
	}

	for _, p := range []string{"a", "c"} {
		if !m.HasPermission(p) {
			t.Errorf("HasPermission(%s) = false, want the other permissions untouched", p)
		}
	}

	// Revoking something the member never had is a no-op.
	m.RevokePermission("absent")

	if got := len(m.GetPermissions()); got != 2 {
		t.Errorf("permissions = %d after revoking an absent one, want 2", got)
	}
}

func TestLocalMember_GrantPermission(t *testing.T) {
	m := NewLocalMember(streaming.MemberOptions{UserID: "alice"})

	m.GrantPermission(streaming.PermissionSendMessage)

	if !m.HasPermission(streaming.PermissionSendMessage) {
		t.Error("HasPermission = false after GrantPermission")
	}

	// Granting twice must not duplicate the entry.
	m.GrantPermission(streaming.PermissionSendMessage)

	if got := len(m.GetPermissions()); got != 1 {
		t.Errorf("permissions = %d after a duplicate grant, want 1", got)
	}
}

// --- Concurrency -----------------------------------------------------------

func TestRoomStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	s := NewRoomStore()

	for i := range 8 {
		seedRoom(t, s, fmt.Sprintf("room-%d", i))
	}

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			for i := range 50 {
				roomID := fmt.Sprintf("room-%d", i%8)
				userID := fmt.Sprintf("u%d-%d", w, i)

				_ = s.AddMember(ctx, roomID, member(userID, "member"))
				_, _ = s.GetMembers(ctx, roomID)
				_, _ = s.MemberCount(ctx, roomID)
				_, _ = s.GetUserRooms(ctx, userID)
				_, _ = s.List(ctx, nil)
				_, _ = s.Search(ctx, "room", nil)
				_ = s.RemoveMember(ctx, roomID, userID)
			}
		}(w)
	}

	wg.Wait()
}
