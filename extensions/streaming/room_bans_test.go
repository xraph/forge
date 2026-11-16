package streaming

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func lifecycleOf(msg *Message) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}

	s, _ := msg.Metadata[LifecycleMetadataKey].(string)

	return s
}

// BanUser failed on every local room, because it went through the local
// Room's ban methods, which only returned ErrInvalidRoom. A ban must now drop
// the membership, take the user's live connections out of the room, keep them
// out, and refuse their sends.
func TestBanUser_RemovesEvictsAndKeepsOut(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	tab1 := newTestConn("a1", "alice")
	tab2 := newTestConn("a2", "alice")
	bob := newTestConn("b1", "bob")

	for _, c := range []*testConn{tab1, tab2, bob} {
		assertNoError(t, m.Register(c))
		assertNoError(t, m.JoinRoom(ctx, c.ID(), "lobby"))
	}

	assertNoError(t, m.BanUser(ctx, "alice", "lobby", "spam", nil))

	if member, _ := rooms.IsMember(ctx, "lobby", "alice"); member {
		t.Error("alice is still a room member after the ban")
	}

	for _, c := range []*testConn{tab1, tab2} {
		if c.IsInRoom("lobby") {
			t.Errorf("%s still in the room after the ban", c.ID())
		}

		c.waitQuiet(t)

		if got := lifecycleOf(c.rec.lastJSON(t)); got != "room.banned" {
			t.Errorf("%s: last frame lifecycle = %q, want room.banned", c.ID(), got)
		}
	}

	before := tab1.rec.totalWrites()

	assertNoError(t, m.BroadcastToRoom(ctx, "lobby", &Message{ID: "m", Type: MessageTypeMessage, RoomID: "lobby", Data: "hi"}))
	tab1.waitQuiet(t)
	bob.waitQuiet(t)

	if tab1.rec.totalWrites() != before {
		t.Error("banned user still receives the room's messages")
	}

	if bob.rec.lastJSON(t).ID != "m" {
		t.Error("bob, who was not banned, missed the room message")
	}

	// Cannot rejoin, by the server API or by a client frame.
	assertErrorIs(t, m.JoinRoom(ctx, tab1.ID(), "lobby"), ErrUserBanned)

	_, err := m.ProcessInbound(ctx, &Message{Type: MessageTypeJoin, RoomID: "lobby"}, tab1)
	assertErrorIs(t, err, ErrUserBanned)

	// Cannot send.
	_, err = m.ProcessInbound(ctx, &Message{Type: MessageTypeMessage, RoomID: "lobby", Data: "x"}, tab1)
	if err == nil {
		t.Fatal("banned user's message to the room was accepted")
	}

	bans, err := rooms.GetBans(ctx, "lobby")
	assertNoError(t, err)

	if len(bans) != 1 || bans[0].UserID != "alice" || bans[0].Reason != "spam" {
		t.Errorf("GetBans = %+v, want alice for spam", bans)
	}

	assertNoError(t, m.UnbanUser(ctx, "alice", "lobby"))
	assertNoError(t, m.JoinRoom(ctx, tab1.ID(), "lobby"))
}

// A connection can still be in the room locally when the ban was made
// elsewhere, on another node in distributed mode. The send check reads the
// shared store, so it refuses the send anyway. It used to ask the local Room,
// which answered with an error that the check read as "not banned".
func TestProcessInbound_RefusesSendFromBannedMemberStillInRoom(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	c := newTestConn("a1", "alice")
	assertNoError(t, m.Register(c))
	assertNoError(t, m.JoinRoom(ctx, c.ID(), "lobby"))

	assertNoError(t, rooms.BanMember(ctx, "lobby", "alice", streaming.RoomBan{UserID: "alice", RoomID: "lobby"}))

	if !c.IsInRoom("lobby") {
		t.Fatal("setup: the connection should still be in the room locally")
	}

	_, err := m.ProcessInbound(ctx, &Message{Type: MessageTypeMessage, RoomID: "lobby", Data: "x"}, c)
	assertErrorIs(t, err, ErrUserBanned)
}

func TestBanUser_ExpiresAtUntil(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	c := newTestConn("a1", "alice")
	assertNoError(t, m.Register(c))

	until := time.Now().Add(100 * time.Millisecond)
	assertNoError(t, m.BanUser(ctx, "alice", "lobby", "cool off", &until))
	assertErrorIs(t, m.JoinRoom(ctx, c.ID(), "lobby"), ErrUserBanned)

	time.Sleep(time.Until(until) + 20*time.Millisecond)

	assertNoError(t, m.JoinRoom(ctx, c.ID(), "lobby"))
}

func TestBanUser_UnknownRoom(t *testing.T) {
	m, _, _ := realManager(t)

	assertErrorIs(t, m.BanUser(context.Background(), "alice", "nowhere", "", nil), ErrRoomNotFound)
}

// End to end over a real WebSocket: a banned client is told so, stops getting
// the room's messages, cannot send to it, and cannot join it again until it
// is unbanned.
func TestWebSocket_BannedClient(t *testing.T) {
	ext, srv := newPresenceServer(t)
	ctx := context.Background()

	if err := ext.Manager().CreateRoom(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})); err != nil {
		t.Fatal(err)
	}

	cli, _, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws?uid=alice")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	send := func(frame map[string]any) {
		t.Helper()

		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}

		if err := wsutil.WriteClientText(cli, data); err != nil {
			t.Fatal(err)
		}
	}

	// next returns the next frame that is not a heartbeat.
	next := func() *Message {
		t.Helper()

		_ = cli.SetReadDeadline(time.Now().Add(3 * time.Second))

		for {
			data, err := wsutil.ReadServerText(cli)
			if err != nil {
				t.Fatalf("reading from the server: %v", err)
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("undecodable frame %s: %v", data, err)
			}

			if lifecycleOf(&msg) == "ping" {
				continue
			}

			return &msg
		}
	}

	errorCode := func(msg *Message) string {
		data, _ := msg.Data.(map[string]any)
		code, _ := data["code"].(string)

		return code
	}

	send(map[string]any{"type": MessageTypeJoin, "room_id": "lobby"})
	send(map[string]any{"type": MessageTypeMessage, "room_id": "lobby", "data": "before"})

	if got := next(); got.Type != MessageTypeMessage || got.Data != "before" {
		t.Fatalf("expected own room message before the ban, got %+v", got)
	}

	if err := ext.Manager().BanUser(ctx, "alice", "lobby", "spam", nil); err != nil {
		t.Fatal(err)
	}

	if got := next(); lifecycleOf(got) != "room.banned" || got.RoomID != "lobby" {
		t.Fatalf("expected a room.banned notice, got %+v", got)
	}

	send(map[string]any{"type": MessageTypeMessage, "room_id": "lobby", "data": "after"})

	if got := next(); got.Type != MessageTypeError {
		t.Fatalf("send after the ban was not refused: %+v", got)
	}

	send(map[string]any{"type": MessageTypeJoin, "room_id": "lobby"})

	if got := next(); got.Type != MessageTypeError || errorCode(got) != "banned" {
		t.Fatalf("rejoin after the ban: got %+v, want a banned error", got)
	}

	if err := ext.Manager().UnbanUser(ctx, "alice", "lobby"); err != nil {
		t.Fatal(err)
	}

	send(map[string]any{"type": MessageTypeJoin, "room_id": "lobby"})
	send(map[string]any{"type": MessageTypeMessage, "room_id": "lobby", "data": "again"})

	if got := next(); got.Type != MessageTypeMessage || got.Data != "again" {
		t.Fatalf("expected own room message after the unban, got %+v", got)
	}
}
