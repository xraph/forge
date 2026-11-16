package streaming

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	streamauth "github.com/xraph/forge/extensions/streaming/auth"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	redisbackend "github.com/xraph/forge/extensions/streaming/backends/redis"
	"github.com/xraph/forge/extensions/streaming/coordinator"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// newClusterNode builds a started manager that shares a room store and a
// Redis coordinator with the rest of the cluster, as a real node does.
func newClusterNode(t *testing.T, addr, nodeID string, rooms streaming.RoomStore) Manager {
	t.Helper()

	client := goredis.NewClient(&goredis.Options{Addr: addr})

	t.Cleanup(func() { _ = client.Close() })

	cfg := DefaultConfig()
	cfg.NodeID = nodeID

	msgs := local.NewMessageStore()
	ra := streamauth.NewRoomAuthorizer(rooms)

	m := NewManager(cfg, rooms, local.NewChannelStore(), msgs,
		newFakePresenceTracker(), newFakeTypingTracker(), nil, nil, nil,
		WithRoomAuthorizer(ra),
		WithMessageAuthorizer(streamauth.NewMessageAuthorizer(ra, newAuthMessageStore(msgs))),
		WithCoordinator(coordinator.NewRedisCoordinator(client, nodeID)),
		WithManagerNodeID(nodeID),
	)

	ctx, cancel := context.WithCancel(context.Background())

	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = m.Stop(context.Background())

		cancel()
	})

	return m
}

func newMessageID() string { return uuid.NewString() }

// receivedFrames decodes every JSON frame the connection's transport got.
func receivedFrames(c *testConn) []Message {
	c.rec.mu.Lock()
	defer c.rec.mu.Unlock()

	out := make([]Message, 0, len(c.rec.jsonWrites))

	for _, w := range c.rec.jsonWrites {
		var raw []byte

		switch v := w.(type) {
		case json.RawMessage:
			raw = v
		case *Message:
			out = append(out, *v)

			continue
		default:
			continue
		}

		var msg Message
		if json.Unmarshal(raw, &msg) == nil {
			out = append(out, msg)
		}
	}

	return out
}

func countData(c *testConn, data string) int {
	n := 0

	for _, m := range receivedFrames(c) {
		if m.Data == data {
			n++
		}
	}

	return n
}

func receivedData(c *testConn, data string) bool { return countData(c, data) > 0 }

func countLifecycle(c *testConn, lifecycle string) int {
	n := 0

	frames := receivedFrames(c)
	for i := range frames {
		if lifecycleOf(&frames[i]) == lifecycle {
			n++
		}
	}

	return n
}

func receivedLifecycle(c *testConn, lifecycle string) bool { return countLifecycle(c, lifecycle) > 0 }

// eventually polls cond for up to two seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

// A ban made on one node used to evict only that node's connections. A
// banned user connected to another node stayed in that node's room index and
// kept receiving the room until they reconnected.
func TestBanUser_EvictsOnEveryNode(t *testing.T) {
	srv := miniredis.RunT(t)
	ctx := context.Background()

	shared := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})

	t.Cleanup(func() { _ = shared.Close() })

	rooms := redisbackend.NewRoomStore(shared, "cluster:rooms")
	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	nodeA := newClusterNode(t, srv.Addr(), "node-a", rooms)
	nodeB := newClusterNode(t, srv.Addr(), "node-b", rooms)

	bob := newTestConn("bob-1", "bob")       // on node A, where the ban is issued
	alice := newTestConn("alice-1", "alice") // on node B, the one being banned
	carol := newTestConn("carol-1", "carol") // on node B, a control

	assertNoError(t, nodeA.Register(bob))
	assertNoError(t, nodeA.JoinRoom(ctx, bob.ID(), "lobby"))

	for _, c := range []*testConn{alice, carol} {
		assertNoError(t, nodeB.Register(c))
		assertNoError(t, nodeB.JoinRoom(ctx, c.ID(), "lobby"))
	}

	// Wait until node B hears node A, which also proves the relay works.
	eventually(t, "node B to receive node A's room messages", func() bool {
		_ = nodeA.BroadcastToRoom(ctx, "lobby", &Message{ID: newMessageID(), Type: MessageTypeMessage, RoomID: "lobby", Data: "warmup"})

		return receivedData(alice, "warmup")
	})

	assertNoError(t, nodeA.BanUser(ctx, "alice", "lobby", "spam", nil))

	eventually(t, "node B to take alice out of the room", func() bool { return !alice.IsInRoom("lobby") })

	eventually(t, "alice's room.banned notice", func() bool { return receivedLifecycle(alice, "room.banned") })

	before := countData(alice, "after the ban")

	assertNoError(t, nodeA.BroadcastToRoom(ctx, "lobby",
		&Message{ID: newMessageID(), Type: MessageTypeMessage, RoomID: "lobby", Data: "after the ban"}))

	// Carol is on the same node as alice: once she has the message, node B has
	// done its delivery, so alice's count is final.
	eventually(t, "carol to receive the post-ban message", func() bool { return receivedData(carol, "after the ban") })
	alice.waitQuiet(t)

	if countData(alice, "after the ban") != before {
		t.Fatal("alice, banned on node A, still received the room message on node B")
	}

	// Idempotent: a duplicate event changes nothing and sends no second notice.
	notices := countLifecycle(alice, "room.banned")

	assertNoError(t, nodeB.(*manager).handleCoordinatorMessage(ctx, &coordinator.CoordinatorMessage{
		Type: coordinator.MessageTypeRoomBan, NodeID: "node-a", UserID: "alice", RoomID: "lobby",
		Payload: &coordinator.RoomBanEvent{Reason: "spam"},
	}))
	alice.waitQuiet(t)

	if countLifecycle(alice, "room.banned") != notices {
		t.Fatal("a duplicate ban event sent a second notice")
	}
}

// The event can beat the store write, for instance on a lagging replica. The
// node evicts on receipt anyway, and if the user rejoins in the gap, before
// JoinRoom can see the ban, a later recheck takes them out again.
func TestHandleRoomBan_BeforeStoreWrite(t *testing.T) {
	m, rooms, _ := realManager(t)
	mgr := m.(*manager)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	alice := newTestConn("alice-1", "alice")
	assertNoError(t, m.Register(alice))
	assertNoError(t, m.JoinRoom(ctx, alice.ID(), "lobby"))

	event := &coordinator.CoordinatorMessage{
		Type: coordinator.MessageTypeRoomBan, NodeID: "elsewhere", UserID: "alice", RoomID: "lobby",
		Payload: &coordinator.RoomBanEvent{Reason: "spam"},
	}

	assertNoError(t, mgr.handleCoordinatorMessage(ctx, event))

	if alice.IsInRoom("lobby") {
		t.Fatal("not evicted on an event that arrived before the store write")
	}

	// Rejoin in the gap: the store has no ban yet, so this succeeds.
	assertNoError(t, m.JoinRoom(ctx, alice.ID(), "lobby"))

	// The origin's store write lands.
	assertNoError(t, rooms.BanMember(ctx, "lobby", "alice", streaming.RoomBan{UserID: "alice", RoomID: "lobby"}))

	eventually(t, "the recheck to evict the rejoined connection", func() bool { return !alice.IsInRoom("lobby") })
}

// An event for a ban that has already run out does nothing.
func TestHandleRoomBan_IgnoresExpiredBan(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	alice := newTestConn("alice-1", "alice")
	assertNoError(t, m.Register(alice))
	assertNoError(t, m.JoinRoom(ctx, alice.ID(), "lobby"))

	past := time.Now().Add(-time.Minute)

	assertNoError(t, m.(*manager).handleCoordinatorMessage(ctx, &coordinator.CoordinatorMessage{
		Type: coordinator.MessageTypeRoomBan, NodeID: "elsewhere", UserID: "alice", RoomID: "lobby",
		Payload: &coordinator.RoomBanEvent{ExpiresAt: &past},
	}))

	if !alice.IsInRoom("lobby") {
		t.Fatal("an expired ban event evicted the user")
	}
}
