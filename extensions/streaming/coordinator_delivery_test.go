package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/coordinator"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// A room message relayed from another node reaches the local members of that
// room and nobody else, whichever payload shape the coordinator produced.
func TestHandleCoordinatorMessage_RoomDeliveryUsesMembership(t *testing.T) {
	m, rooms, _ := realManager(t)
	mgr := m.(*manager)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	conns := make([]*testConn, 12)
	for i := range conns {
		conns[i] = newTestConn(fmt.Sprintf("c%d", i), fmt.Sprintf("u%d", i))
		assertNoError(t, m.Register(conns[i]))

		if i%2 == 0 {
			assertNoError(t, m.JoinRoom(ctx, conns[i].ID(), "lobby"))
		}
	}

	msg := &Message{ID: "remote-1", Type: MessageTypeMessage, RoomID: "lobby", Data: "hi",
		Metadata: map[string]any{"_origin_node": "other"}}

	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	wire, err := json.Marshal(coordinator.CoordinatorMessage{
		Type: coordinator.MessageTypeBroadcast, NodeID: "other", RoomID: "lobby", Payload: msg,
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := coordinator.DecodeMessage(wire)
	if err != nil {
		t.Fatal(err)
	}

	payloads := []any{decoded.Payload, json.RawMessage(raw)}

	for i, p := range payloads {
		cm := &coordinator.CoordinatorMessage{Type: coordinator.MessageTypeBroadcast, NodeID: "other", RoomID: "lobby", Payload: p}

		// Distinct ids, so dedup does not swallow the second delivery.
		if pm, ok := p.(*streaming.Message); ok {
			pm.ID = fmt.Sprintf("remote-%d", i)
		} else {
			cm.Payload = json.RawMessage(fmt.Appendf(nil, `{"id":"remote-%d","type":"message","room_id":"lobby","data":"hi"}`, i))
		}

		assertNoError(t, mgr.handleCoordinatorMessage(ctx, cm))
	}

	for i, c := range conns {
		c.waitQuiet(t)

		want := 0
		if i%2 == 0 {
			want = len(payloads)
		}

		if got := c.rec.totalWrites(); got != want {
			t.Errorf("conn %d: %d frames, want %d", i, got, want)
		}
	}
}
