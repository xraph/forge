package coordinator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	nats "github.com/nats-io/nats.go"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// runNATS starts an embedded NATS server with JetStream on a random port.
func runNATS(t *testing.T) string {
	t.Helper()

	srv, err := server.NewServer(&server.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
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

// natsNode is one coordinator plus everything its handler received.
type natsNode struct {
	id    string
	coord StreamCoordinator

	mu   sync.Mutex
	msgs []*CoordinatorMessage
}

func (n *natsNode) received() []*CoordinatorMessage {
	n.mu.Lock()
	defer n.mu.Unlock()

	return append([]*CoordinatorMessage(nil), n.msgs...)
}

// count returns how many received messages match.
func (n *natsNode) count(match func(*CoordinatorMessage) bool) int {
	c := 0

	for _, m := range n.received() {
		if match(m) {
			c++
		}
	}

	return c
}

func startNATSNode(t *testing.T, url, nodeID string) *natsNode {
	t.Helper()

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(conn.Close)

	coord, err := NewNATSCoordinator(conn, nodeID)
	if err != nil {
		t.Fatal(err)
	}

	n := &natsNode{id: nodeID, coord: coord}

	if err := coord.Subscribe(context.Background(), func(_ context.Context, msg *CoordinatorMessage) error {
		n.mu.Lock()
		defer n.mu.Unlock()

		n.msgs = append(n.msgs, msg)

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := coord.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = coord.Stop(context.Background()) })

	return n
}

func payloadID(m *CoordinatorMessage) string {
	if sm, ok := m.Payload.(*streaming.Message); ok {
		return sm.ID
	}

	return ""
}

// waitCount polls until every node has exactly want matching messages, then
// holds a moment longer to catch late duplicates.
func waitCount(t *testing.T, what string, nodes []*natsNode, want int, match func(*CoordinatorMessage) bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		done := true

		for _, n := range nodes {
			if n.count(match) < want {
				done = false
			}
		}

		if done {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)

	for _, n := range nodes {
		if got := n.count(match); got != want {
			t.Errorf("%s: node %s received %d, want %d", what, n.id, got, want)
		}
	}
}

// The NATS coordinator consumed every subject through one queue group, so a
// global broadcast or ban event reached one node instead of all of them, and
// BroadcastGlobal carried no sender, so the sender handled its own message.
func TestNATSCoordinator_FanOutReachesEveryNode(t *testing.T) {
	url := runNATS(t)
	ctx := context.Background()

	a := startNATSNode(t, url, "node-a")
	b := startNATSNode(t, url, "node-b")
	c := startNATSNode(t, url, "node-c")
	others := []*natsNode{b, c}

	isID := func(id string) func(*CoordinatorMessage) bool {
		return func(m *CoordinatorMessage) bool { return payloadID(m) == id }
	}

	// Global broadcast: every other node once, the sender never.
	if err := a.coord.BroadcastGlobal(ctx, &streaming.Message{ID: "g1", Type: streaming.MessageTypeMessage}); err != nil {
		t.Fatal(err)
	}

	waitCount(t, "global broadcast", others, 1, isID("g1"))
	waitCount(t, "global broadcast echo", []*natsNode{a}, 0, isID("g1"))

	for _, m := range b.received() {
		if payloadID(m) == "g1" && m.NodeID != "node-a" {
			t.Errorf("global broadcast NodeID = %q, want the sender node-a", m.NodeID)
		}
	}

	// Ban event: same.
	pub, ok := a.coord.(EventPublisher)
	if !ok {
		t.Fatal("NATS coordinator does not implement EventPublisher")
	}

	if err := pub.PublishEvent(ctx, &CoordinatorMessage{
		Type: MessageTypeRoomBan, UserID: "alice", RoomID: "lobby", Payload: &RoomBanEvent{Reason: "spam"},
	}); err != nil {
		t.Fatal(err)
	}

	isBan := func(m *CoordinatorMessage) bool {
		ev, ok := m.Payload.(*RoomBanEvent)

		return m.Type == MessageTypeRoomBan && ok && ev.Reason == "spam" && m.UserID == "alice" && m.RoomID == "lobby"
	}

	waitCount(t, "ban event", others, 1, isBan)
	waitCount(t, "ban event echo", []*natsNode{a}, 0, isBan)

	// Room broadcasts had no subscriber at all.
	if err := a.coord.BroadcastToRoom(ctx, "lobby", &streaming.Message{ID: "r1", RoomID: "lobby"}); err != nil {
		t.Fatal(err)
	}

	waitCount(t, "room broadcast", others, 1, isID("r1"))
	waitCount(t, "room broadcast echo", []*natsNode{a}, 0, isID("r1"))

	// A node message reaches its target only. The target used to drop it,
	// because NodeID carried the target and matched its own id.
	if err := a.coord.BroadcastToNode(ctx, "node-b", &streaming.Message{ID: "n1"}); err != nil {
		t.Fatal(err)
	}

	waitCount(t, "node message", []*natsNode{b}, 1, isID("n1"))
	waitCount(t, "node message to others", []*natsNode{a, c}, 0, isID("n1"))
}

// A node that starts late must not replay the retained backlog as live.
func TestNATSCoordinator_NewNodeDoesNotReplayBacklog(t *testing.T) {
	url := runNATS(t)
	ctx := context.Background()

	a := startNATSNode(t, url, "node-a")

	if err := a.coord.BroadcastGlobal(ctx, &streaming.Message{ID: "old"}); err != nil {
		t.Fatal(err)
	}

	late := startNATSNode(t, url, "node-late")

	if err := a.coord.BroadcastGlobal(ctx, &streaming.Message{ID: "new"}); err != nil {
		t.Fatal(err)
	}

	waitCount(t, "live message", []*natsNode{late}, 1, func(m *CoordinatorMessage) bool { return payloadID(m) == "new" })
	waitCount(t, "backlog", []*natsNode{late}, 0, func(m *CoordinatorMessage) bool { return payloadID(m) == "old" })
}
