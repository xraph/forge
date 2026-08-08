package streaming

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/filters"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/extensions/streaming/ratelimit"
)

// --- Registration ----------------------------------------------------------

func TestManager_RegisterAndGetConnection(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	got, err := h.mgr.GetConnection("c1")
	assertNoError(t, err)

	if got.ID() != "c1" {
		t.Errorf("GetConnection().ID() = %q, want c1", got.ID())
	}

	if n := h.mgr.ConnectionCount(); n != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", n)
	}
}

func TestManager_GetConnectionUnknown(t *testing.T) {
	h := newTestManager(t, testConfig())

	_, err := h.mgr.GetConnection("nope")
	assertErrorIs(t, err, ErrConnectionNotFound)
}

func TestManager_GetUserConnections(t *testing.T) {
	h := newTestManager(t, testConfig())

	h.register(t,
		newTestConn("c1", "alice"),
		newTestConn("c2", "alice"),
		newTestConn("c3", "bob"),
	)

	tests := []struct {
		name   string
		userID string
		want   int
	}{
		{name: "user with two connections", userID: "alice", want: 2},
		{name: "user with one connection", userID: "bob", want: 1},
		{name: "unknown user", userID: "carol", want: 0},
		{name: "empty user ID", userID: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(h.mgr.GetUserConnections(tt.userID)); got != tt.want {
				t.Errorf("GetUserConnections(%q) = %d connections, want %d", tt.userID, got, tt.want)
			}
		})
	}
}

func TestManager_AnonymousConnectionsAreNotUserIndexed(t *testing.T) {
	h := newTestManager(t, testConfig())

	h.register(t, newTestConn("c1", ""))

	if n := h.mgr.ConnectionCount(); n != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", n)
	}

	if got := len(h.mgr.GetUserConnections("")); got != 0 {
		t.Errorf("GetUserConnections(\"\") = %d, want 0 — anonymous connections are not indexed by user", got)
	}

	if got := len(h.mgr.GetAllConnections()); got != 1 {
		t.Errorf("GetAllConnections() = %d, want 1", got)
	}
}

func TestManager_GetAllConnections(t *testing.T) {
	h := newTestManager(t, testConfig())

	h.register(t, newTestConn("c1", "u1"), newTestConn("c2", "u2"))

	all := h.mgr.GetAllConnections()
	if len(all) != 2 {
		t.Fatalf("GetAllConnections() = %d, want 2", len(all))
	}

	seen := map[string]bool{}
	for _, c := range all {
		seen[c.ID()] = true
	}

	for _, id := range []string{"c1", "c2"} {
		if !seen[id] {
			t.Errorf("GetAllConnections() missing %q", id)
		}
	}
}

func TestManager_PerUserConnectionLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		userID    string
		registers int
		// wantAccepted is how many Register calls are expected to succeed.
		wantAccepted int
	}{
		{name: "under the limit", limit: 5, userID: "u1", registers: 3, wantAccepted: 3},
		{name: "exactly at the limit", limit: 3, userID: "u1", registers: 3, wantAccepted: 3},
		{name: "over the limit", limit: 2, userID: "u1", registers: 5, wantAccepted: 2},
		{
			// The limit is only consulted when the user already has an entry in
			// the index, so the first connection is always admitted. With a
			// limit of zero that means one connection slips through.
			name:  "zero limit still admits the first connection",
			limit: 0, userID: "u1", registers: 3, wantAccepted: 1,
		},
		{
			// Anonymous connections skip the index entirely, so no limit applies.
			name:  "anonymous connections are unlimited",
			limit: 1, userID: "", registers: 5, wantAccepted: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.MaxConnectionsPerUser = tt.limit

			h := newTestManager(t, cfg)

			accepted := 0

			for i := 0; i < tt.registers; i++ {
				err := h.mgr.Register(newTestConn(fmt.Sprintf("c%d", i), tt.userID))
				if err == nil {
					accepted++

					continue
				}

				assertErrorIs(t, err, ErrConnectionLimitReached)
			}

			if accepted != tt.wantAccepted {
				t.Errorf("accepted %d registrations, want %d", accepted, tt.wantAccepted)
			}

			if got := h.mgr.ConnectionCount(); got != tt.wantAccepted {
				t.Errorf("ConnectionCount() = %d, want %d", got, tt.wantAccepted)
			}
		})
	}
}

func TestManager_RegisterSameConnIDTwice(t *testing.T) {
	// Characterizes current behavior: Register does not check whether connID is
	// already present. The connections map is keyed by ID so it holds one entry,
	// but the per-user index gains a second copy of the same ID, and
	// GetUserConnections then reports the connection twice. A re-register (for
	// example after a flaky reconnect that reuses the ID) therefore inflates the
	// user's connection count and consumes a slot against MaxConnectionsPerUser.
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")

	assertNoError(t, h.mgr.Register(conn))
	assertNoError(t, h.mgr.Register(conn))

	if got := h.mgr.ConnectionCount(); got != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", got)
	}

	if got := len(h.mgr.GetUserConnections("u1")); got != 2 {
		t.Errorf("GetUserConnections() = %d, want 2 (current double-index behavior)", got)
	}
}

// --- Unregistration --------------------------------------------------------

func TestManager_Unregister(t *testing.T) {
	h := newTestManager(t, testConfig())

	h.register(t, newTestConn("c1", "u1"), newTestConn("c2", "u1"))

	assertNoError(t, h.mgr.Unregister("c1"))

	if got := h.mgr.ConnectionCount(); got != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", got)
	}

	if got := len(h.mgr.GetUserConnections("u1")); got != 1 {
		t.Errorf("GetUserConnections(u1) = %d, want 1", got)
	}

	_, err := h.mgr.GetConnection("c1")
	assertErrorIs(t, err, ErrConnectionNotFound)
}

func TestManager_UnregisterUnknown(t *testing.T) {
	h := newTestManager(t, testConfig())

	assertErrorIs(t, h.mgr.Unregister("nope"), ErrConnectionNotFound)
}

func TestManager_UnregisterLastConnectionDropsUserIndex(t *testing.T) {
	h := newTestManager(t, testConfig())

	h.register(t, newTestConn("c1", "u1"))
	assertNoError(t, h.mgr.Unregister("c1"))

	if got := len(h.mgr.GetUserConnections("u1")); got != 0 {
		t.Errorf("GetUserConnections(u1) = %d, want 0", got)
	}

	// The user slot must be released so a fresh connection is not rejected by
	// the per-user limit.
	assertNoError(t, h.mgr.Register(newTestConn("c2", "u1")))
}

func TestManager_UnregisterFreesLimitSlots(t *testing.T) {
	cfg := testConfig()
	cfg.MaxConnectionsPerUser = 2

	h := newTestManager(t, cfg)

	h.register(t, newTestConn("c1", "u1"), newTestConn("c2", "u1"))
	assertErrorIs(t, h.mgr.Register(newTestConn("c3", "u1")), ErrConnectionLimitReached)

	assertNoError(t, h.mgr.Unregister("c1"))
	assertNoError(t, h.mgr.Register(newTestConn("c3", "u1")))
}

func TestManager_UnregisterSavesSessionSnapshot(t *testing.T) {
	cfg := testConfig()
	cfg.EnableSessionResumption = true
	cfg.SessionResumptionTTL = time.Minute

	store := NewInMemorySessionStore()
	h := newTestManager(t, cfg, WithSessionStore(store))

	conn := newTestConn("c1", "u1")
	conn.SetSessionID("sess-1")
	conn.AddRoom("room-1")
	conn.AddSubscription("chan-1")

	h.register(t, conn)
	assertNoError(t, h.mgr.Unregister("c1"))

	snap, err := store.Get(context.Background(), "sess-1")
	assertNoError(t, err)

	if snap.UserID != "u1" {
		t.Errorf("snapshot UserID = %q, want u1", snap.UserID)
	}

	if len(snap.Rooms) != 1 || snap.Rooms[0] != "room-1" {
		t.Errorf("snapshot Rooms = %v, want [room-1]", snap.Rooms)
	}

	if len(snap.Channels) != 1 || snap.Channels[0] != "chan-1" {
		t.Errorf("snapshot Channels = %v, want [chan-1]", snap.Channels)
	}
}

func TestManager_UnregisterSkipsSnapshotWhenResumptionDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.EnableSessionResumption = false

	store := NewInMemorySessionStore()
	h := newTestManager(t, cfg, WithSessionStore(store))

	conn := newTestConn("c1", "u1")
	conn.SetSessionID("sess-1")

	h.register(t, conn)
	assertNoError(t, h.mgr.Unregister("c1"))

	if _, err := store.Get(context.Background(), "sess-1"); err == nil {
		t.Error("snapshot was saved even though session resumption is disabled")
	}
}

// --- Broadcast fan-out -----------------------------------------------------

func TestManager_Broadcast(t *testing.T) {
	h := newTestManager(t, testConfig())

	a := newTestConn("c1", "u1")
	b := newTestConn("c2", "u2")
	c := newTestConn("c3", "u3")

	h.register(t, a, b, c)

	assertNoError(t, h.mgr.Broadcast(context.Background(), &Message{ID: "m1", Data: "hi"}))

	for _, conn := range []*testConn{a, b, c} {
		assertWrites(t, conn, 1)
	}
}

func TestManager_BroadcastWithNoConnections(t *testing.T) {
	h := newTestManager(t, testConfig())

	assertNoError(t, h.mgr.Broadcast(context.Background(), &Message{ID: "m1"}))
}

func TestManager_BroadcastSurvivesWriteErrors(t *testing.T) {
	// A dead connection must not stop delivery to the healthy ones, and
	// Broadcast reports success regardless.
	h := newTestManager(t, testConfig())

	bad := newTestConn("bad", "u1")
	bad.rec.writeErr = errFake

	good := newTestConn("good", "u2")

	h.register(t, bad, good)

	assertNoError(t, h.mgr.Broadcast(context.Background(), &Message{ID: "m1"}))

	assertWrites(t, good, 1)
	assertWrites(t, bad, 0)
}

func TestManager_BroadcastToRoom(t *testing.T) {
	h := newTestManager(t, testConfig())

	inRoom := newTestConn("c1", "u1")
	otherRoom := newTestConn("c2", "u2")
	noRoom := newTestConn("c3", "u3")

	h.register(t, inRoom, otherRoom, noRoom)
	h.join(t, inRoom, "room-1")
	h.join(t, otherRoom, "room-2")

	assertNoError(t, h.mgr.BroadcastToRoom(context.Background(), "room-1", &Message{ID: "m1", RoomID: "room-1"}))

	assertWrites(t, inRoom, 1)
	assertWrites(t, otherRoom, 0)
	assertWrites(t, noRoom, 0)
}

func TestManager_BroadcastToRoomConcurrentPath(t *testing.T) {
	// Rooms with more than 8 members take the concurrent (goroutine-per-member)
	// delivery path; every member must still receive exactly one message.
	h := newTestManager(t, testConfig())

	const members = 12

	conns := make([]*testConn, 0, members)

	for i := 0; i < members; i++ {
		c := newTestConn(fmt.Sprintf("c%d", i), fmt.Sprintf("u%d", i))
		conns = append(conns, c)
	}

	outsider := newTestConn("outsider", "u-out")

	h.register(t, append(conns, outsider)...)

	for _, c := range conns {
		h.join(t, c, "big-room")
	}

	assertNoError(t, h.mgr.BroadcastToRoom(context.Background(), "big-room", &Message{ID: "m1"}))

	for _, c := range conns {
		assertWrites(t, c, 1)
	}

	assertWrites(t, outsider, 0)
}

func TestManager_BroadcastToChannel(t *testing.T) {
	h := newTestManager(t, testConfig())

	subscribed := newTestConn("c1", "u1")
	otherChannel := newTestConn("c2", "u2")
	unsubscribed := newTestConn("c3", "u3")

	h.register(t, subscribed, otherChannel, unsubscribed)
	h.subscribe(t, subscribed, "chan-1")
	h.subscribe(t, otherChannel, "chan-2")

	assertNoError(t, h.mgr.BroadcastToChannel(context.Background(), "chan-1", &Message{ID: "m1", ChannelID: "chan-1"}))

	assertWrites(t, subscribed, 1)
	assertWrites(t, otherChannel, 0)
	assertWrites(t, unsubscribed, 0)
}

func TestManager_SendToUser(t *testing.T) {
	h := newTestManager(t, testConfig())

	a1 := newTestConn("c1", "alice")
	a2 := newTestConn("c2", "alice")
	b1 := newTestConn("c3", "bob")

	h.register(t, a1, a2, b1)

	assertNoError(t, h.mgr.SendToUser(context.Background(), "alice", &Message{ID: "m1"}))

	assertWrites(t, a1, 1)
	assertWrites(t, a2, 1)
	assertWrites(t, b1, 0)
}

func TestManager_SendToUserUnknownUserIsNotAnError(t *testing.T) {
	h := newTestManager(t, testConfig())

	// No local connections for the user: the message is still relayed onward
	// (other nodes may hold the user), so this is not an error.
	assertNoError(t, h.mgr.SendToUser(context.Background(), "ghost", &Message{ID: "m1"}))
}

func TestManager_SendToConnection(t *testing.T) {
	h := newTestManager(t, testConfig())

	target := newTestConn("c1", "u1")
	other := newTestConn("c2", "u2")

	h.register(t, target, other)

	assertNoError(t, h.mgr.SendToConnection(context.Background(), "c1", &Message{ID: "m1"}))

	assertWrites(t, target, 1)
	assertWrites(t, other, 0)
}

func TestManager_SendToConnectionErrors(t *testing.T) {
	h := newTestManager(t, testConfig())

	failing := newTestConn("c1", "u1")
	failing.rec.writeErr = errFake

	h.register(t, failing)

	t.Run("unknown connection", func(t *testing.T) {
		assertErrorIs(t, h.mgr.SendToConnection(context.Background(), "nope", &Message{}), ErrConnectionNotFound)
	})

	// Delivery is asynchronous: SendToConnection enqueues, and the connection's
	// writer goroutine performs the socket write later. A transport-level write
	// failure therefore cannot be reported through this return value, and the
	// connection tears itself down instead — which is the observable contract a
	// caller can rely on.
	t.Run("write failure tears the connection down rather than returning", func(t *testing.T) {
		assertNoError(t, h.mgr.SendToConnection(context.Background(), "c1", &Message{}))

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if failing.IsClosed() {
				return
			}

			time.Sleep(time.Millisecond)
		}

		t.Error("connection still open after a failing write; want it closed")
	})
}

func TestManager_BroadcastToUsers(t *testing.T) {
	h := newTestManager(t, testConfig())

	alice := newTestConn("c1", "alice")
	bob := newTestConn("c2", "bob")
	carol := newTestConn("c3", "carol")

	h.register(t, alice, bob, carol)

	assertNoError(t, h.mgr.BroadcastToUsers(context.Background(), []string{"alice", "bob"}, &Message{ID: "m1"}))

	assertWrites(t, alice, 1)
	assertWrites(t, bob, 1)
	assertWrites(t, carol, 0)
}

func TestManager_BroadcastToRooms(t *testing.T) {
	h := newTestManager(t, testConfig())

	r1 := newTestConn("c1", "u1")
	r2 := newTestConn("c2", "u2")
	r3 := newTestConn("c3", "u3")

	h.register(t, r1, r2, r3)
	h.join(t, r1, "room-1")
	h.join(t, r2, "room-2")
	h.join(t, r3, "room-3")

	assertNoError(t, h.mgr.BroadcastToRooms(context.Background(), []string{"room-1", "room-2"}, &Message{ID: "m1"}))

	assertWrites(t, r1, 1)
	assertWrites(t, r2, 1)
	assertWrites(t, r3, 0)
}

func TestManager_BroadcastToRoomsBothMemberships(t *testing.T) {
	// A connection in two of the targeted rooms receives the message once per
	// room, since BroadcastToRooms loops over rooms independently.
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")

	h.register(t, conn)
	h.join(t, conn, "room-1", "room-2")

	assertNoError(t, h.mgr.BroadcastToRooms(context.Background(), []string{"room-1", "room-2"}, &Message{ID: "m1"}))

	assertWrites(t, conn, 2)
}

func TestManager_BroadcastExceptHonoursUserIDs(t *testing.T) {
	// C2 fixed: the parameter is user IDs, as the Manager interface and both
	// Room implementations have always declared. It was matched against
	// connection IDs, so the ordinary "everyone except the sender" call
	// excluded nobody and echoed each message back to its author.
	h := newTestManager(t, testConfig())

	a := newTestConn("conn-a", "alice")
	b := newTestConn("conn-b", "bob")

	h.register(t, a, b)

	assertNoError(t, h.mgr.BroadcastExcept(context.Background(), &Message{ID: "m1"}, []string{"alice"}))

	assertWrites(t, a, 0)
	assertWrites(t, b, 1)
}

func TestManager_BroadcastExceptIgnoresConnectionIDs(t *testing.T) {
	// The mirror of the above: a connection ID is not a user ID and must not
	// be honoured as one, or the fix would merely have moved the ambiguity.
	h := newTestManager(t, testConfig())

	a := newTestConn("conn-a", "alice")
	b := newTestConn("conn-b", "bob")

	h.register(t, a, b)

	assertNoError(t, h.mgr.BroadcastExcept(context.Background(), &Message{ID: "m2"}, []string{"conn-a"}))

	assertWrites(t, a, 1)
	assertWrites(t, b, 1)
}

func TestManager_BroadcastExceptExcludesAllOfAUsersConnections(t *testing.T) {
	// Excluding a user must exclude every socket they hold, not just one.
	h := newTestManager(t, testConfig())

	tab1 := newTestConn("conn-a1", "alice")
	tab2 := newTestConn("conn-a2", "alice")
	bob := newTestConn("conn-b", "bob")

	h.register(t, tab1, tab2, bob)

	assertNoError(t, h.mgr.BroadcastExcept(context.Background(), &Message{ID: "m3"}, []string{"alice"}))

	assertWrites(t, tab1, 0)
	assertWrites(t, tab2, 0)
	assertWrites(t, bob, 1)
}

func TestManager_DeliverToConnectionContentTypeRouting(t *testing.T) {
	tests := []struct {
		name            string
		msgContentType  string
		connContentType string
		wantJSONWrites  int
		wantRawWrites   int
		wantRawPayload  string
	}{
		{
			name:           "empty content type uses WriteJSON",
			wantJSONWrites: 1,
		},
		{
			name:           "explicit JSON content type uses WriteJSON",
			msgContentType: ContentTypeJSON,
			wantJSONWrites: 1,
		},
		{
			name:           "text content type is encoded and written as bytes",
			msgContentType: ContentTypeText,
			wantRawWrites:  1,
			wantRawPayload: "payload",
		},
		{
			name:            "connection preference applies when the message has none",
			connContentType: ContentTypeText,
			wantRawWrites:   1,
			wantRawPayload:  "payload",
		},
		{
			name:            "message content type wins over connection preference",
			msgContentType:  ContentTypeJSON,
			connContentType: ContentTypeText,
			wantJSONWrites:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestManager(t, testConfig(), WithCodecRegistry(NewCodecRegistry()))

			conn := newTestConn("c1", "u1")
			if tt.connContentType != "" {
				conn.SetContentType(tt.connContentType)
			}

			h.register(t, conn)

			msg := &Message{ID: "m1", Data: "payload", ContentType: tt.msgContentType}
			assertNoError(t, h.mgr.SendToConnection(context.Background(), "c1", msg))

			awaitCount(t, "WriteJSON calls", conn.rec.jsonWriteCount, tt.wantJSONWrites)
			awaitCount(t, "Write calls", conn.rec.rawWriteCount, tt.wantRawWrites)

			if tt.wantRawPayload != "" {
				if got := string(conn.rec.lastRaw(t)); got != tt.wantRawPayload {
					t.Errorf("raw payload = %q, want %q", got, tt.wantRawPayload)
				}
			}
		})
	}
}

func TestManager_DeliverFallsBackToJSONWithoutCodecRegistry(t *testing.T) {
	// No codec registry is wired, so even a non-JSON content type must not drop
	// the message — it falls back to WriteJSON.
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.SendToConnection(
		context.Background(),
		"c1",
		&Message{ID: "m1", ContentType: ContentTypeText, Data: "x"},
	))

	awaitCount(t, "WriteJSON calls", conn.rec.jsonWriteCount, 1)
}

func TestManager_DeliverPropagatesCodecErrors(t *testing.T) {
	registry := NewCodecRegistry()
	// Content type with no registered codec: Encode fails and delivery errors.
	h := newTestManager(t, testConfig(), WithCodecRegistry(registry))

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	err := h.mgr.SendToConnection(
		context.Background(),
		"c1",
		&Message{ID: "m1", ContentType: ContentTypeProtobuf},
	)
	if err == nil {
		t.Fatal("SendToConnection: want error for unregistered content type, got nil")
	}

	assertWrites(t, conn, 0)
}

// --- Filter chain ----------------------------------------------------------

func TestManager_FilterChainBlocksPerRecipient(t *testing.T) {
	chain := filters.NewFilterChain()
	chain.Add(filters.NewSimpleFilter("block-bob", 10,
		func(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
			if recipient.GetUserID() == "bob" {
				return nil, nil
			}

			return msg, nil
		},
	))

	h := newTestManager(t, testConfig(), WithFilterChain(chain))

	alice := newTestConn("c1", "alice")
	bob := newTestConn("c2", "bob")

	h.register(t, alice, bob)

	assertNoError(t, h.mgr.Broadcast(context.Background(), &Message{ID: "m1"}))

	assertWrites(t, alice, 1)
	assertWrites(t, bob, 0)
}

func TestManager_FilterChainTransformsPerRecipient(t *testing.T) {
	chain := filters.NewFilterChain()
	chain.Add(filters.NewSimpleFilter("tag", 10,
		func(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
			out := *msg
			out.ID = msg.ID + "-" + recipient.GetUserID()

			return &out, nil
		},
	))

	h := newTestManager(t, testConfig(), WithFilterChain(chain))

	alice := newTestConn("c1", "alice")
	h.register(t, alice)

	assertNoError(t, h.mgr.SendToConnection(context.Background(), "c1", &Message{ID: "m"}))

	awaitCount(t, "alice JSON writes", alice.rec.jsonWriteCount, 1)

	if got := alice.rec.lastJSON(t).ID; got != "m-alice" {
		t.Errorf("delivered message ID = %q, want m-alice", got)
	}
}

func TestManager_FilterChainErrorFailsDelivery(t *testing.T) {
	chain := filters.NewFilterChain()
	chain.Add(filters.NewSimpleFilter("boom", 10,
		func(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
			return nil, errFake
		},
	))

	h := newTestManager(t, testConfig(), WithFilterChain(chain))

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	if err := h.mgr.SendToConnection(context.Background(), "c1", &Message{ID: "m"}); err == nil {
		t.Fatal("SendToConnection: want error from failing filter, got nil")
	}

	assertWrites(t, conn, 0)
}

// --- Coordinator relay -----------------------------------------------------

func TestManager_BroadcastRelaysToCoordinator(t *testing.T) {
	coord := newFakeCoordinator()
	h := newTestManager(t, testConfig(), WithCoordinator(coord), WithManagerNodeID("node-a"))

	conn := newTestConn("c1", "u1")
	conn.AddRoom("room-1")
	conn.AddSubscription("chan-1")

	h.register(t, conn)

	ctx := context.Background()

	assertNoError(t, h.mgr.Broadcast(ctx, &Message{ID: "m1"}))
	assertNoError(t, h.mgr.BroadcastToRoom(ctx, "room-1", &Message{ID: "m2"}))
	assertNoError(t, h.mgr.SendToUser(ctx, "u1", &Message{ID: "m3"}))
	assertNoError(t, h.mgr.BroadcastToChannel(ctx, "chan-1", &Message{ID: "m4"}))

	if got := coord.globalBroadcastCount(); got != 1 {
		t.Errorf("global relays = %d, want 1", got)
	}

	if got := coord.roomBroadcastTargets(); len(got) != 1 || got[0] != "room-1" {
		t.Errorf("room relays = %v, want [room-1]", got)
	}

	if got := coord.userBroadcastTargets(); len(got) != 1 || got[0] != "u1" {
		t.Errorf("user relays = %v, want [u1]", got)
	}

	// Characterizes current behavior: channel broadcasts are node-local only.
	// A subscriber connected to another node never sees them.
	coord.mu.Lock()
	channelRelays := len(coord.nodeBroadcasts)
	coord.mu.Unlock()

	if channelRelays != 0 {
		t.Errorf("channel relays = %d, want 0 (channels are not relayed today)", channelRelays)
	}
}

func TestManager_CoordinatorBroadcastTagsOriginNode(t *testing.T) {
	coord := newFakeCoordinator()
	h := newTestManager(t, testConfig(), WithCoordinator(coord), WithManagerNodeID("node-a"))

	msg := &Message{ID: "m1"}
	assertNoError(t, h.mgr.Broadcast(context.Background(), msg))

	coord.mu.Lock()
	defer coord.mu.Unlock()

	if len(coord.globalBroadcasts) != 1 {
		t.Fatalf("global relays = %d, want 1", len(coord.globalBroadcasts))
	}

	relayed := coord.globalBroadcasts[0]
	if got := relayed.Metadata["_origin_node"]; got != "node-a" {
		t.Errorf("_origin_node = %v, want node-a", got)
	}
}

func TestManager_CoordinatorErrorsDoNotFailBroadcast(t *testing.T) {
	coord := newFakeCoordinator()
	coord.broadcastErr = errFake

	h := newTestManager(t, testConfig(), WithCoordinator(coord), WithManagerNodeID("node-a"))

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.Broadcast(context.Background(), &Message{ID: "m1"}))

	// Local delivery still happened.
	assertWrites(t, conn, 1)
}

func TestManager_NoCoordinatorIsSafe(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	conn.AddRoom("room-1")
	h.register(t, conn)

	ctx := context.Background()

	assertNoError(t, h.mgr.Broadcast(ctx, &Message{ID: "m1"}))
	assertNoError(t, h.mgr.BroadcastToRoom(ctx, "room-1", &Message{ID: "m2"}))
	assertNoError(t, h.mgr.SendToUser(ctx, "u1", &Message{ID: "m3"}))
}

// --- Rooms -----------------------------------------------------------------

func TestManager_CreateRoom(t *testing.T) {
	h := newTestManager(t, testConfig())

	room := newFakeRoom("room-1")
	assertNoError(t, h.mgr.CreateRoom(context.Background(), room))

	got, err := h.mgr.GetRoom(context.Background(), "room-1")
	assertNoError(t, err)

	if got.GetID() != "room-1" {
		t.Errorf("GetRoom().GetID() = %q, want room-1", got.GetID())
	}
}

func TestManager_CreateRoomDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.EnableRooms = false

	h := newTestManager(t, cfg)

	if err := h.mgr.CreateRoom(context.Background(), newFakeRoom("room-1")); err == nil {
		t.Fatal("CreateRoom: want error when rooms are disabled, got nil")
	}
}

func TestManager_CreateRoomHookCanReject(t *testing.T) {
	hooks := NewHookRegistry()
	hooks.Add(&roomHookDouble{baseHook: baseHook{name: "gate"}, createErr: errFake})

	h := newTestManager(t, testConfig(), WithHookRegistry(hooks))

	assertErrorIs(t, h.mgr.CreateRoom(context.Background(), newFakeRoom("room-1")), errFake)

	if _, err := h.mgr.GetRoom(context.Background(), "room-1"); err == nil {
		t.Error("room was created despite the hook rejecting it")
	}
}

func TestManager_JoinRoomUpdatesConnectionState(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))

	if !conn.IsInRoom("room-1") {
		t.Error("connection is not marked as being in room-1")
	}
}

func TestManager_JoinRoomPopulatesTheRoomStore(t *testing.T) {
	// C1 fixed: JoinRoom writes through to the store. Previously it mutated only
	// connection-local state, so GetRoomMembers stayed empty and every
	// store-backed query — member counts, MaxRoomsPerUser, cross-node presence —
	// saw an empty room.
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))

	if got := h.rooms.addMemberCallCount(); got != 1 {
		t.Errorf("roomStore.AddMember called %d times, want 1", got)
	}

	members, err := h.mgr.GetRoomMembers(context.Background(), "room-1")
	assertNoError(t, err)

	if len(members) != 1 {
		t.Fatalf("GetRoomMembers = %d members, want 1", len(members))
	}

	if got := members[0].GetUserID(); got != "u1" {
		t.Errorf("member user ID = %q, want u1", got)
	}
}

func TestManager_JoinRoomIsIdempotent(t *testing.T) {
	// A rejoin must succeed: a second tab and a resumed session both replay a
	// join for a room the user is already in, and ErrAlreadyRoomMember from the
	// store is not a failure.
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))
	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))
}
func TestManager_JoinRoomAddsMemberToStore(t *testing.T) {

	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))

	members, err := h.mgr.GetRoomMembers(context.Background(), "room-1")
	assertNoError(t, err)

	if len(members) != 1 || members[0].GetUserID() != "u1" {
		t.Fatalf("GetRoomMembers = %v, want one member u1", members)
	}
}

func TestManager_JoinRoomRequiresUserID(t *testing.T) {
	h := newTestManager(t, testConfig())

	h.register(t, newTestConn("c1", ""))

	if err := h.mgr.JoinRoom(context.Background(), "c1", "room-1"); err == nil {
		t.Fatal("JoinRoom: want error for a connection with no user ID, got nil")
	}
}

func TestManager_JoinRoomUnknownConnection(t *testing.T) {
	h := newTestManager(t, testConfig())

	assertErrorIs(t, h.mgr.JoinRoom(context.Background(), "nope", "room-1"), ErrConnectionNotFound)
}

func TestManager_JoinRoomHookCanReject(t *testing.T) {
	hooks := NewHookRegistry()
	hooks.Add(&roomHookDouble{baseHook: baseHook{name: "gate"}, joinErr: errFake})

	h := newTestManager(t, testConfig(), WithHookRegistry(hooks))

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertErrorIs(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"), errFake)

	if conn.IsInRoom("room-1") {
		t.Error("connection joined the room despite the hook rejecting it")
	}
}

func TestManager_MaxRoomsPerUserTriggersFromJoins(t *testing.T) {
	// C1 fixed: the limit counts store membership, which joins now populate, so
	// it is actually reachable. Before, GetUserRooms was always empty and a
	// connection could join unbounded rooms — each one holding an index entry.
	cfg := testConfig()
	cfg.MaxRoomsPerUser = 1

	h := newTestManager(t, cfg)

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))

	err := h.mgr.JoinRoom(context.Background(), "c1", "room-2")
	if !errors.Is(err, ErrRoomLimitReached) {
		t.Fatalf("JoinRoom(room-2) error = %v, want ErrRoomLimitReached", err)
	}
}

func TestManager_RoomLimitDoesNotBlockRejoiningAnExistingRoom(t *testing.T) {
	// At the cap, a rejoin of a room already held is not a new room and must
	// still succeed — otherwise session resumption fails for any user at their
	// limit, which is precisely the heavy user most likely to reconnect.
	cfg := testConfig()
	cfg.MaxRoomsPerUser = 1

	h := newTestManager(t, cfg)

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))
	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))
}
func TestManager_MaxRoomsPerUserLimitsJoins(t *testing.T) {

	cfg := testConfig()
	cfg.MaxRoomsPerUser = 2

	h := newTestManager(t, cfg)
	h.register(t, newTestConn("c1", "u1"))

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))
	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-2"))
	assertErrorIs(t, h.mgr.JoinRoom(context.Background(), "c1", "room-3"), ErrRoomLimitReached)
}

func TestManager_LeaveRoom(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.JoinRoom(context.Background(), "c1", "room-1"))
	assertNoError(t, h.mgr.LeaveRoom(context.Background(), "c1", "room-1"))

	if conn.IsInRoom("room-1") {
		t.Error("connection is still in room-1 after LeaveRoom")
	}
}

func TestManager_LeaveRoomUnknownConnection(t *testing.T) {
	h := newTestManager(t, testConfig())

	assertErrorIs(t, h.mgr.LeaveRoom(context.Background(), "nope", "room-1"), ErrConnectionNotFound)
}

func TestManager_LeaveRoomFiresHook(t *testing.T) {
	hooks := NewHookRegistry()

	hook := &roomHookDouble{baseHook: baseHook{name: "audit"}}
	hooks.Add(hook)

	h := newTestManager(t, testConfig(), WithHookRegistry(hooks))
	h.register(t, newTestConn("c1", "u1"))

	assertNoError(t, h.mgr.LeaveRoom(context.Background(), "c1", "room-1"))

	hook.mu.Lock()
	defer hook.mu.Unlock()

	if len(hook.leaves) != 1 || hook.leaves[0] != "room-1" {
		t.Errorf("OnRoomLeave calls = %v, want [room-1]", hook.leaves)
	}
}

func TestManager_DeleteRoom(t *testing.T) {
	hooks := NewHookRegistry()

	hook := &roomHookDouble{baseHook: baseHook{name: "audit"}}
	hooks.Add(hook)

	h := newTestManager(t, testConfig(), WithHookRegistry(hooks))

	assertNoError(t, h.mgr.CreateRoom(context.Background(), newFakeRoom("room-1")))
	assertNoError(t, h.mgr.DeleteRoom(context.Background(), "room-1"))

	if _, err := h.mgr.GetRoom(context.Background(), "room-1"); err == nil {
		t.Error("room still present after DeleteRoom")
	}

	hook.mu.Lock()
	defer hook.mu.Unlock()

	if len(hook.deletes) != 1 {
		t.Errorf("OnRoomDelete calls = %d, want 1", len(hook.deletes))
	}
}

func TestManager_ListRooms(t *testing.T) {
	h := newTestManager(t, testConfig())

	ctx := context.Background()
	assertNoError(t, h.mgr.CreateRoom(ctx, newFakeRoom("room-1")))
	assertNoError(t, h.mgr.CreateRoom(ctx, newFakeRoom("room-2")))

	rooms, err := h.mgr.ListRooms(ctx)
	assertNoError(t, err)

	if len(rooms) != 2 {
		t.Errorf("ListRooms = %d rooms, want 2", len(rooms))
	}
}

// --- Channels --------------------------------------------------------------

func TestManager_SubscribeAndUnsubscribe(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.Subscribe(context.Background(), "c1", "chan-1", nil))

	if !conn.IsSubscribed("chan-1") {
		t.Fatal("connection is not subscribed to chan-1")
	}

	assertNoError(t, h.mgr.Unsubscribe(context.Background(), "c1", "chan-1"))

	if conn.IsSubscribed("chan-1") {
		t.Error("connection is still subscribed after Unsubscribe")
	}
}

func TestManager_SubscribeUnknownConnection(t *testing.T) {
	h := newTestManager(t, testConfig())

	assertErrorIs(t, h.mgr.Subscribe(context.Background(), "nope", "chan-1", nil), ErrConnectionNotFound)
}

func TestManager_MaxChannelsPerUserTriggersFromSubscribes(t *testing.T) {
	// C1 fixed, channel side: Subscribe writes through to the channel store, so
	// the per-user channel cap is reachable.
	cfg := testConfig()
	cfg.MaxChannelsPerUser = 1

	h := newTestManager(t, cfg)

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.Subscribe(context.Background(), "c1", "chan-1", nil))

	err := h.mgr.Subscribe(context.Background(), "c1", "chan-2", nil)
	if !errors.Is(err, ErrChannelLimitReached) {
		t.Fatalf("Subscribe(chan-2) error = %v, want ErrChannelLimitReached", err)
	}
}
func TestManager_MaxChannelsPerUserLimitsSubscribes(t *testing.T) {

	cfg := testConfig()
	cfg.MaxChannelsPerUser = 2

	h := newTestManager(t, cfg)
	h.register(t, newTestConn("c1", "u1"))

	assertNoError(t, h.mgr.Subscribe(context.Background(), "c1", "chan-1", nil))
	assertNoError(t, h.mgr.Subscribe(context.Background(), "c1", "chan-2", nil))

	if err := h.mgr.Subscribe(context.Background(), "c1", "chan-3", nil); err == nil {
		t.Error("Subscribe: want channel limit error, got nil")
	}
}

func TestManager_CreateChannelDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.EnableChannels = false

	h := newTestManager(t, cfg)

	if err := h.mgr.CreateChannel(context.Background(), newFakeChannel("chan-1")); err == nil {
		t.Fatal("CreateChannel: want error when channels are disabled, got nil")
	}
}

func TestManager_CreateAndListChannels(t *testing.T) {
	h := newTestManager(t, testConfig())

	ctx := context.Background()
	assertNoError(t, h.mgr.CreateChannel(ctx, newFakeChannel("chan-1")))

	channels, err := h.mgr.ListChannels(ctx)
	assertNoError(t, err)

	if len(channels) != 1 {
		t.Fatalf("ListChannels = %d, want 1", len(channels))
	}

	assertNoError(t, h.mgr.DeleteChannel(ctx, "chan-1"))

	channels, err = h.mgr.ListChannels(ctx)
	assertNoError(t, err)

	if len(channels) != 0 {
		t.Errorf("ListChannels = %d after delete, want 0", len(channels))
	}
}

// --- Presence and typing ---------------------------------------------------

func TestManager_SetPresence(t *testing.T) {
	h := newTestManager(t, testConfig())

	assertNoError(t, h.mgr.SetPresence(context.Background(), "u1", StatusOnline))

	calls := h.presence.setPresenceCalls()
	if len(calls) != 1 || calls[0].userID != "u1" || calls[0].status != StatusOnline {
		t.Errorf("SetPresence calls = %v, want one {u1 online}", calls)
	}
}

func TestManager_SetPresenceDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.EnablePresence = false

	h := newTestManager(t, cfg)

	if err := h.mgr.SetPresence(context.Background(), "u1", StatusOnline); err == nil {
		t.Fatal("SetPresence: want error when presence is disabled, got nil")
	}

	if got := len(h.presence.setPresenceCalls()); got != 0 {
		t.Errorf("tracker received %d SetPresence calls, want 0", got)
	}
}

func TestManager_TypingRouting(t *testing.T) {
	h := newTestManager(t, testConfig())

	ctx := context.Background()
	assertNoError(t, h.mgr.StartTyping(ctx, "u1", "room-1"))

	users, err := h.mgr.GetTypingUsers(ctx, "room-1")
	assertNoError(t, err)

	if len(users) != 1 || users[0] != "u1" {
		t.Errorf("GetTypingUsers = %v, want [u1]", users)
	}

	assertNoError(t, h.mgr.StopTyping(ctx, "u1", "room-1"))

	users, err = h.mgr.GetTypingUsers(ctx, "room-1")
	assertNoError(t, err)

	if len(users) != 0 {
		t.Errorf("GetTypingUsers = %v, want empty after StopTyping", users)
	}
}

// --- Message pipeline ------------------------------------------------------

func TestManager_ProcessMessage(t *testing.T) {
	tests := []struct {
		name        string
		rateLimiter ratelimit.RateLimiter
		validator   *stubValidator
		sender      *testConn
		wantErr     bool
	}{
		{
			name:   "no pipeline components passes through",
			sender: newTestConn("c1", "u1"),
		},
		{
			name:        "rate limiter allows",
			rateLimiter: &stubRateLimiter{allow: true},
			sender:      newTestConn("c1", "u1"),
		},
		{
			name:        "rate limiter denies",
			rateLimiter: &stubRateLimiter{allow: false},
			sender:      newTestConn("c1", "u1"),
			wantErr:     true,
		},
		{
			name:        "rate limiter error is logged, not fatal",
			rateLimiter: &stubRateLimiter{allow: false, err: errFake},
			sender:      newTestConn("c1", "u1"),
		},
		{
			name:        "rate limiter is skipped for anonymous senders",
			rateLimiter: &stubRateLimiter{allow: false},
			sender:      newTestConn("c1", ""),
		},
		{
			name:      "validator rejects",
			validator: &stubValidator{err: errFake},
			sender:    newTestConn("c1", "u1"),
			wantErr:   true,
		},
		{
			name:      "validator accepts",
			validator: &stubValidator{},
			sender:    newTestConn("c1", "u1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []ManagerOption{}
			if tt.rateLimiter != nil {
				opts = append(opts, WithRateLimiter(tt.rateLimiter))
			}

			if tt.validator != nil {
				opts = append(opts, WithValidator(tt.validator))
			}

			h := newTestManager(t, testConfig(), opts...)

			got, err := h.impl.ProcessInbound(context.Background(), &Message{ID: "m1"}, tt.sender)

			if tt.wantErr {
				if err == nil {
					t.Fatal("ProcessInbound: want error, got nil")
				}

				if got != nil {
					t.Errorf("message = %+v, want nil on rejection", got)
				}

				return
			}

			assertNoError(t, err)

			if got == nil {
				t.Fatal("message = nil, want it passed through")
			}
		})
	}
}

// Broadcast is a SERVER-initiated call and is deliberately not gated.
//
// Defect S6 was that nothing gated client-originated messages, and the fix put
// the gate on the inbound boundary (ProcessInbound, called from the socket read
// loop) rather than on Broadcast. That distinction is the design, not an
// oversight: a server broadcasting a system notice must not be refused because
// some user's token bucket is empty, and applying a per-user rate limit to a
// call that has no user is meaningless. This test pins that boundary so a later
// change cannot quietly move the gate and make server broadcasts fail under
// client load.
func TestManager_BroadcastIsNotSubjectToClientLimits(t *testing.T) {
	h := newTestManager(t, testConfig(),
		WithValidator(&stubValidator{err: errFake}),
		WithRateLimiter(&stubRateLimiter{allow: false}))

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	assertNoError(t, h.mgr.Broadcast(context.Background(), &Message{ID: "m1"}))

	assertWrites(t, conn, 1)
}

// The inbound gate, which is where S6's fix lives.
func TestManager_ProcessInboundRejectsOnValidationFailure(t *testing.T) {
	h := newTestManager(t, testConfig(), WithValidator(&stubValidator{err: errFake}))

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	got, err := h.impl.ProcessInbound(context.Background(), &Message{ID: "m1"}, conn)
	if err == nil {
		t.Fatal("ProcessInbound: want validation error, got nil")
	}

	if got != nil {
		t.Errorf("message = %+v, want nil on rejection", got)
	}
}

func TestManager_ProcessInboundRejectsWhenRateLimited(t *testing.T) {
	h := newTestManager(t, testConfig(), WithRateLimiter(&stubRateLimiter{allow: false}))

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	_, err := h.impl.ProcessInbound(context.Background(), &Message{ID: "m1"}, conn)
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("ProcessInbound error = %v, want ErrRateLimitExceeded", err)
	}
}

// S2: a client may not publish to a room it has not joined, whatever room id it
// puts in the message.
func TestManager_ProcessInboundRejectsUnjoinedRoom(t *testing.T) {
	h := newTestManager(t, testConfig())

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	_, err := h.impl.ProcessInbound(context.Background(),
		&Message{ID: "m1", RoomID: "room-the-client-never-joined"}, conn)

	if !errors.Is(err, ErrSendDenied) {
		t.Fatalf("ProcessInbound error = %v, want ErrSendDenied", err)
	}
}

// S6/W4: the configured message size cap is enforced, not merely displayed.
func TestManager_ProcessInboundRejectsOversizeMessage(t *testing.T) {
	cfg := testConfig()
	cfg.MaxMessageSize = 32

	h := newTestManager(t, cfg)

	conn := newTestConn("c1", "u1")
	h.register(t, conn)

	_, err := h.impl.ProcessInbound(context.Background(),
		&Message{ID: "m1", Data: strings.Repeat("x", 512)}, conn)

	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("ProcessInbound error = %v, want ErrMessageTooLarge", err)
	}
}

// --- Session resumption ----------------------------------------------------

func TestManager_ResumeSession(t *testing.T) {
	cfg := testConfig()
	cfg.EnableSessionResumption = true
	cfg.SessionResumptionTTL = time.Minute

	store := NewInMemorySessionStore()
	h := newTestManager(t, cfg, WithSessionStore(store))

	assertNoError(t, store.Save(context.Background(), &SessionSnapshot{
		SessionID: "sess-1",
		UserID:    "u1",
		Rooms:     []string{"room-1"},
		Channels:  []string{"chan-1"},
	}, time.Minute))

	conn := newTestConn("c-new", "u1")
	h.register(t, conn)

	ok, err := h.mgr.ResumeSession(context.Background(), "c-new", "sess-1")
	assertNoError(t, err)

	if !ok {
		t.Fatal("ResumeSession = false, want true")
	}

	if !conn.IsInRoom("room-1") {
		t.Error("room membership was not restored")
	}

	if !conn.IsSubscribed("chan-1") {
		t.Error("channel subscription was not restored")
	}

	// The snapshot is consumed so it cannot be replayed.
	if _, err := store.Get(context.Background(), "sess-1"); err == nil {
		t.Error("snapshot survived resumption; it must be single-use")
	}
}

func TestManager_ResumeSessionMisses(t *testing.T) {
	cfg := testConfig()
	cfg.EnableSessionResumption = true

	store := NewInMemorySessionStore()

	tests := []struct {
		name      string
		enable    bool
		withStore bool
		sessionID string
	}{
		{name: "unknown session ID", enable: true, withStore: true, sessionID: "nope"},
		{name: "empty session ID", enable: true, withStore: true, sessionID: ""},
		{name: "resumption disabled", enable: false, withStore: true, sessionID: "sess-1"},
		{name: "no session store", enable: true, withStore: false, sessionID: "sess-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			c.EnableSessionResumption = tt.enable

			opts := []ManagerOption{}
			if tt.withStore {
				opts = append(opts, WithSessionStore(store))
			}

			h := newTestManager(t, c, opts...)
			h.register(t, newTestConn("c1", "u1"))

			ok, err := h.mgr.ResumeSession(context.Background(), "c1", tt.sessionID)
			assertNoError(t, err)

			if ok {
				t.Error("ResumeSession = true, want false")
			}
		})
	}
}

func TestManager_ResumeSessionRejectsAnotherUsersSnapshot(t *testing.T) {
	// S4 fixed: a snapshot is bound to the user who created it. Without the
	// check, presenting a known session id on a fresh connection inherited the
	// victim's rooms and channels — and every message broadcast to them.
	cfg := testConfig()
	cfg.EnableSessionResumption = true
	cfg.SessionResumptionTTL = time.Minute

	store := NewInMemorySessionStore()
	h := newTestManager(t, cfg, WithSessionStore(store))

	assertNoError(t, store.Save(context.Background(), &SessionSnapshot{
		SessionID: "victim-session",
		UserID:    "victim",
		Rooms:     []string{"victim-private-room"},
	}, time.Minute))

	attacker := newTestConn("c-attacker", "attacker")
	h.register(t, attacker)

	ok, err := h.mgr.ResumeSession(context.Background(), "c-attacker", "victim-session")

	if !errors.Is(err, ErrSessionNotOwned) {
		t.Fatalf("ResumeSession error = %v, want ErrSessionNotOwned", err)
	}

	if ok {
		t.Error("ResumeSession = true, want false")
	}

	if attacker.IsInRoom("victim-private-room") {
		t.Error("attacker inherited the victim's room membership")
	}
}

// --- Stats -----------------------------------------------------------------

func TestManager_GetStatsCounts(t *testing.T) {
	h := newTestManager(t, testConfig())

	ctx := context.Background()

	h.register(t, newTestConn("c1", "u1"), newTestConn("c2", "u2"))
	assertNoError(t, h.mgr.CreateRoom(ctx, newFakeRoom("room-1")))
	assertNoError(t, h.mgr.CreateChannel(ctx, newFakeChannel("chan-1")))

	h.presence.online = []string{"u1", "u2"}

	stats, err := h.mgr.GetStats(ctx)
	assertNoError(t, err)

	if stats.TotalConnections != 2 {
		t.Errorf("TotalConnections = %d, want 2", stats.TotalConnections)
	}

	if stats.TotalRooms != 1 {
		t.Errorf("TotalRooms = %d, want 1", stats.TotalRooms)
	}

	if stats.TotalChannels != 1 {
		t.Errorf("TotalChannels = %d, want 1", stats.TotalChannels)
	}

	if stats.OnlineUsers != 2 {
		t.Errorf("OnlineUsers = %d, want 2", stats.OnlineUsers)
	}
}

func TestManager_GetStatsReportsRealUptime(t *testing.T) {
	// C9 fixed: the manager records its start time and counts delivered frames,
	// so uptime, throughput and memory are measurements rather than constants.
	h := newTestManager(t, testConfig())
	assertNoError(t, h.mgr.Start(context.Background()))

	time.Sleep(20 * time.Millisecond)

	stats, err := h.mgr.GetStats(context.Background())
	assertNoError(t, err)

	if stats.Uptime < 10*time.Millisecond {
		t.Errorf("Uptime = %v, want at least 10ms after Start", stats.Uptime)
	}
}

// --- Lifecycle -------------------------------------------------------------

func TestManager_StartConnectsStoresAndTrackers(t *testing.T) {
	h := newTestManager(t, testConfig())

	assertNoError(t, h.mgr.Start(context.Background()))

	h.rooms.mu.Lock()
	roomConnects := h.rooms.connectCalls
	h.rooms.mu.Unlock()

	if roomConnects != 1 {
		t.Errorf("roomStore.Connect calls = %d, want 1", roomConnects)
	}

	h.channels.mu.Lock()
	channelConnects := h.channels.connectCalls
	h.channels.mu.Unlock()

	if channelConnects != 1 {
		t.Errorf("channelStore.Connect calls = %d, want 1", channelConnects)
	}

	h.presence.mu.Lock()
	presenceStarted := h.presence.started
	h.presence.mu.Unlock()

	if !presenceStarted {
		t.Error("presence tracker was not started")
	}

	h.typing.mu.Lock()
	typingStarted := h.typing.started
	h.typing.mu.Unlock()

	if !typingStarted {
		t.Error("typing tracker was not started")
	}
}

func TestManager_StartIsIdempotent(t *testing.T) {
	h := newTestManager(t, testConfig())

	ctx := context.Background()
	assertNoError(t, h.mgr.Start(ctx))
	assertNoError(t, h.mgr.Start(ctx))

	h.rooms.mu.Lock()
	defer h.rooms.mu.Unlock()

	if h.rooms.connectCalls != 1 {
		t.Errorf("roomStore.Connect calls = %d, want 1 — Start must be idempotent", h.rooms.connectCalls)
	}
}

func TestManager_StartSkipsDisabledSubsystems(t *testing.T) {
	cfg := testConfig()
	cfg.EnablePresence = false
	cfg.EnableTypingIndicators = false
	cfg.EnableMessageHistory = false

	h := newTestManager(t, cfg)

	assertNoError(t, h.mgr.Start(context.Background()))

	h.presence.mu.Lock()
	presenceStarted := h.presence.started
	h.presence.mu.Unlock()

	if presenceStarted {
		t.Error("presence tracker started even though presence is disabled")
	}

	h.messages.mu.Lock()
	messageConnects := h.messages.connects
	h.messages.mu.Unlock()

	if messageConnects != 0 {
		t.Error("message store connected even though history is disabled")
	}
}

func TestManager_StartRegistersWithCoordinator(t *testing.T) {
	coord := newFakeCoordinator()
	h := newTestManager(t, testConfig(), WithCoordinator(coord), WithManagerNodeID("node-a"))

	assertNoError(t, h.mgr.Start(context.Background()))

	coord.mu.Lock()
	defer coord.mu.Unlock()

	if !coord.started {
		t.Error("coordinator was not started")
	}

	if coord.handler == nil {
		t.Error("manager did not subscribe to coordinator messages")
	}

	if len(coord.registered) != 1 || coord.registered[0] != "node-a" {
		t.Errorf("registered nodes = %v, want [node-a]", coord.registered)
	}
}

// --- Race coverage ---------------------------------------------------------

func TestManager_ConcurrentRegisterUnregisterBroadcast(t *testing.T) {
	// Run with -race: exercises the connection registry under simultaneous
	// mutation and read-heavy fan-out.
	h := newTestManager(t, testConfig())

	const (
		workers    = 8
		iterations = 60
	)

	// A stable set of connections so broadcasts always have recipients.
	for i := 0; i < 4; i++ {
		conn := newTestConn(fmt.Sprintf("stable-%d", i), fmt.Sprintf("stable-user-%d", i))
		conn.AddRoom("room-1")
		conn.AddSubscription("chan-1")
		h.register(t, conn)
	}

	var wg sync.WaitGroup

	// Churn: register and unregister short-lived connections.
	for w := 0; w < workers; w++ {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("churn-%d-%d", w, i)

				conn := newTestConn(id, fmt.Sprintf("churn-user-%d", w))
				conn.AddRoom("room-1")
				conn.AddSubscription("chan-1")

				if err := h.mgr.Register(conn); err != nil && !errors.Is(err, ErrConnectionLimitReached) {
					t.Errorf("Register(%s): %v", id, err)

					return
				}

				_, _ = h.mgr.GetConnection(id)
				_ = h.mgr.GetUserConnections(fmt.Sprintf("churn-user-%d", w))

				if err := h.mgr.Unregister(id); err != nil && !errors.Is(err, ErrConnectionNotFound) {
					t.Errorf("Unregister(%s): %v", id, err)

					return
				}
			}
		}(w)
	}

	// Fan-out: broadcast across every path while the registry churns.
	for w := 0; w < workers; w++ {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			ctx := context.Background()

			for i := 0; i < iterations; i++ {
				msg := &Message{ID: fmt.Sprintf("m-%d-%d", w, i), Data: "x"}

				_ = h.mgr.Broadcast(ctx, msg)
				_ = h.mgr.BroadcastToRoom(ctx, "room-1", msg)
				_ = h.mgr.BroadcastToChannel(ctx, "chan-1", msg)
				_ = h.mgr.SendToUser(ctx, "stable-user-0", msg)
				_ = h.mgr.BroadcastExcept(ctx, msg, []string{"stable-0"})
				_ = h.mgr.ConnectionCount()
				_ = h.mgr.GetAllConnections()
			}
		}(w)
	}

	wg.Wait()
}

func TestManager_ConcurrentRoomAndChannelMutation(t *testing.T) {
	// Run with -race: joins, leaves, subscribes and unsubscribes all mutate
	// per-connection state while broadcasts read it.
	h := newTestManager(t, testConfig())

	conns := make([]*testConn, 0, 6)

	for i := 0; i < 6; i++ {
		c := newTestConn(fmt.Sprintf("c%d", i), fmt.Sprintf("u%d", i))
		conns = append(conns, c)
		h.register(t, c)
	}

	var wg sync.WaitGroup

	ctx := context.Background()

	for _, c := range conns {
		wg.Add(1)

		go func(c *testConn) {
			defer wg.Done()

			for i := 0; i < 100; i++ {
				roomID := fmt.Sprintf("room-%d", i%3)

				_ = h.mgr.JoinRoom(ctx, c.ID(), roomID)
				_ = h.mgr.Subscribe(ctx, c.ID(), "chan-1", nil)
				_ = h.mgr.LeaveRoom(ctx, c.ID(), roomID)
				_ = h.mgr.Unsubscribe(ctx, c.ID(), "chan-1")
			}
		}(c)
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < 300; i++ {
			msg := &Message{ID: fmt.Sprintf("m%d", i)}

			_ = h.mgr.BroadcastToRoom(ctx, "room-0", msg)
			_ = h.mgr.BroadcastToChannel(ctx, "chan-1", msg)
		}
	}()

	wg.Wait()
}

// --- Pipeline stubs --------------------------------------------------------

type stubValidator struct {
	err error
}

func (v *stubValidator) Validate(ctx context.Context, msg *streaming.Message, sender streaming.EnhancedConnection) error {
	return v.err
}

func (v *stubValidator) ValidateContent(content any) error { return v.err }

func (v *stubValidator) ValidateMetadata(metadata map[string]any) error { return v.err }

type stubRateLimiter struct {
	allow bool
	err   error
}

func (l *stubRateLimiter) Allow(ctx context.Context, key, action string) (bool, error) {
	return l.allow, l.err
}

func (l *stubRateLimiter) AllowN(ctx context.Context, key, action string, n int) (bool, error) {
	return l.allow, l.err
}

func (l *stubRateLimiter) GetStatus(ctx context.Context, key, action string) (*ratelimit.RateLimitStatus, error) {
	return &ratelimit.RateLimitStatus{Allowed: l.allow}, l.err
}

func (l *stubRateLimiter) Reset(ctx context.Context, key, action string) error { return nil }
