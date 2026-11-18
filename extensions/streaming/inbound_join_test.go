package streaming

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// A client "join" frame used to be rejected by ProcessInbound, because the
// gate demanded the connection already be in msg.RoomID for every message
// type. No WebSocket client could ever join a room.
func TestProcessInbound_JoinFrameReachesJoinRoom(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "public", Name: "public", Owner: "o"})))

	c := newTestConn("c1", "alice")
	assertNoError(t, m.Register(c))

	if _, err := m.ProcessInbound(ctx, &Message{Type: MessageTypeJoin, RoomID: "public"}, c); err != nil {
		t.Fatalf("join frame rejected by the inbound gate: %v", err)
	}
}

// Exempting joins from the membership check must not exempt them from join
// authorization.
func TestProcessInbound_JoinFrameStillAuthorized(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "secret", Name: "secret", Owner: "o", Private: true})))
	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "public", Name: "public", Owner: "o"})))
	assertNoError(t, rooms.BanMember(ctx, "public", "mallory", streaming.RoomBan{UserID: "mallory"}))

	alice := newTestConn("c1", "alice")
	mallory := newTestConn("c2", "mallory")
	anon := newTestConn("c3", "")

	for _, c := range []*testConn{alice, mallory, anon} {
		assertNoError(t, m.Register(c))
	}

	cases := []struct {
		name string
		conn *testConn
		room string
		want error
	}{
		{"private room", alice, "secret", ErrRoomAccessDenied},
		{"banned user", mallory, "public", ErrUserBanned},
		{"anonymous", anon, "public", ErrRoomAccessDenied},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.ProcessInbound(ctx, &Message{Type: MessageTypeJoin, RoomID: tc.room}, tc.conn)
			assertErrorIs(t, err, tc.want)
		})
	}
}

func TestProcessInbound_LeaveFrameNeedsNoMembership(t *testing.T) {
	m, _, _ := realManager(t)

	c := newTestConn("c1", "alice")
	assertNoError(t, m.Register(c))

	if _, err := m.ProcessInbound(context.Background(), &Message{Type: MessageTypeLeave, RoomID: "elsewhere"}, c); err != nil {
		t.Fatalf("leave frame rejected: %v", err)
	}
}

// Sends still need membership: the join exemption must not leak into them.
func TestProcessInbound_SendStillNeedsMembership(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "public", Name: "public", Owner: "o"})))

	c := newTestConn("c1", "alice")
	assertNoError(t, m.Register(c))

	_, err := m.ProcessInbound(ctx, &Message{Type: MessageTypeMessage, RoomID: "public", Data: "hi"}, c)
	assertErrorIs(t, err, ErrSendDenied)
}

// End to end through the real socket read loop, ProcessInbound and
// handleMessage: a client joins a room with a join frame and then receives
// the message it sends to that room.
func TestWebSocket_ClientJoinsRoomThroughInboundPath(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{Name: "join"})
	ext := NewExtension(WithLocalBackend())

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	// Stand-in for an auth middleware.
	app.Router().Use(func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if uid := ctx.Request().URL.Query().Get("uid"); uid != "" {
				ctx.Set("user_id", uid)
			}

			return next(ctx)
		}
	})

	if err := ext.RegisterRoutes(app.Router(), "/ws", "/sse"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(app.Router().(http.Handler))
	defer srv.Close()

	ctx := context.Background()
	if err := ext.Manager().CreateRoom(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})); err != nil {
		t.Fatal(err)
	}

	cli, _, _, err := ws.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws?uid=alice")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	send := func(m map[string]any) {
		t.Helper()

		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}

		if err := wsutil.WriteClientText(cli, data); err != nil {
			t.Fatal(err)
		}
	}

	send(map[string]any{"type": MessageTypeJoin, "room_id": "lobby"})
	send(map[string]any{"type": MessageTypeMessage, "room_id": "lobby", "data": "hello"})

	_ = cli.SetReadDeadline(time.Now().Add(3 * time.Second))

	for {
		data, err := wsutil.ReadServerText(cli)
		if err != nil {
			t.Fatalf("never received the room message: %v", err)
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("undecodable frame %s: %v", data, err)
		}

		if msg.Type == MessageTypeError {
			t.Fatalf("server rejected a frame: %s", data)
		}

		if msg.Type == MessageTypeMessage && msg.RoomID == "lobby" {
			if msg.Data != "hello" || msg.UserID != "alice" {
				t.Fatalf("unexpected room message: %s", data)
			}

			break
		}
	}

	if conns := ext.Manager().GetUserConnections("alice"); len(conns) != 1 || !conns[0].IsInRoom("lobby") {
		t.Fatal("connection is not in the room after the join frame")
	}
}
