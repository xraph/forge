package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	nats "github.com/nats-io/nats.go"
	streamauth "github.com/xraph/forge/extensions/streaming/auth"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/coordinator"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

func runEmbeddedNATS(t *testing.T) string {
	t.Helper()

	srv, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	go srv.Start()

	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("embedded NATS server did not start")
	}

	t.Cleanup(srv.Shutdown)

	return srv.ClientURL()
}

// newNATSNode builds a started manager on a NATS coordinator. The room store
// is shared, standing in for the shared store a real cluster uses.
func newNATSNode(t *testing.T, url, nodeID string, rooms streaming.RoomStore) Manager {
	t.Helper()

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(conn.Close)

	coord, err := coordinator.NewNATSCoordinator(conn, nodeID)
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.NodeID = nodeID

	msgs := local.NewMessageStore()
	ra := streamauth.NewRoomAuthorizer(rooms)

	m := NewManager(cfg, rooms, local.NewChannelStore(), msgs,
		newFakePresenceTracker(), newFakeTypingTracker(), nil, nil, nil,
		WithRoomAuthorizer(ra),
		WithMessageAuthorizer(streamauth.NewMessageAuthorizer(ra, newAuthMessageStore(msgs))),
		WithCoordinator(coord),
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

// End to end on NATS: a global broadcast reaches clients on every node
// exactly once, including the sender's own clients, and a ban issued on one
// node takes the user out of the room on another.
func TestNATSCluster_GlobalBroadcastAndBan(t *testing.T) {
	url := runEmbeddedNATS(t)
	ctx := context.Background()

	rooms := local.NewRoomStore()
	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	nodeA := newNATSNode(t, url, "node-a", rooms)
	nodeB := newNATSNode(t, url, "node-b", rooms)

	bob := newTestConn("bob-1", "bob")
	alice := newTestConn("alice-1", "alice")
	carol := newTestConn("carol-1", "carol")

	assertNoError(t, nodeA.Register(bob))
	assertNoError(t, nodeA.JoinRoom(ctx, bob.ID(), "lobby"))

	for _, c := range []*testConn{alice, carol} {
		assertNoError(t, nodeB.Register(c))
		assertNoError(t, nodeB.JoinRoom(ctx, c.ID(), "lobby"))
	}

	assertNoError(t, nodeA.Broadcast(ctx, &Message{ID: newMessageID(), Type: MessageTypeMessage, Data: "everyone"}))

	eventually(t, "node B clients to get the global broadcast", func() bool {
		return receivedData(alice, "everyone") && receivedData(carol, "everyone")
	})

	// Give any echo of node A's own message time to arrive.
	time.Sleep(200 * time.Millisecond)

	for _, c := range []*testConn{bob, alice, carol} {
		c.waitQuiet(t)

		if n := countData(c, "everyone"); n != 1 {
			t.Errorf("%s received the global broadcast %d times, want 1", c.ID(), n)
		}
	}

	assertNoError(t, nodeA.BanUser(ctx, "alice", "lobby", "spam", nil))

	eventually(t, "node B to take alice out of the room", func() bool { return !alice.IsInRoom("lobby") })
	eventually(t, "alice's room.banned notice", func() bool { return receivedLifecycle(alice, "room.banned") })

	if !carol.IsInRoom("lobby") {
		t.Fatal("the ban took an unrelated user out of the room")
	}
}
