package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	streamauth "github.com/xraph/forge/extensions/streaming/auth"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/filters"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// routerLikeConn costs what the router's wsConnection costs to hand a frame
// to, minus the socket: WriteJSON marshals what it is given, except a
// json.RawMessage, which it writes as is.
type routerLikeConn struct {
	recordingConn
}

func (c *routerLikeConn) WriteJSON(v any) error {
	if _, ok := v.(json.RawMessage); ok {
		return nil
	}

	_, err := json.Marshal(v)

	return err
}

func (c *routerLikeConn) Write([]byte) error { return nil }

func benchManager(b *testing.B) (Manager, streaming.RoomStore) {
	b.Helper()

	rooms := local.NewRoomStore()
	msgs := local.NewMessageStore()
	ra := streamauth.NewRoomAuthorizer(rooms)

	m := NewManager(DefaultConfig(), rooms, local.NewChannelStore(), msgs,
		newFakePresenceTracker(), newFakeTypingTracker(), nil, nil, nil,
		WithRoomAuthorizer(ra),
		WithMessageAuthorizer(streamauth.NewMessageAuthorizer(ra, newAuthMessageStore(msgs))),
		WithFilterChain(filters.NewFilterChain()),
		WithHookRegistry(NewHookRegistry()),
		WithCodecRegistry(NewCodecRegistry()),
	)

	return m, rooms
}

func benchMessage() *Message {
	return &Message{
		ID: "m", Type: MessageTypeMessage, RoomID: "room", UserID: "u1",
		Data:      map[string]any{"text": strings.Repeat("hello world ", 40), "n": 1, "tags": []string{"a", "b", "c"}},
		Timestamp: time.Now(),
	}
}

// BenchmarkBroadcastToRoom_1000 measures one message to a 1000-member room.
// Each member used to marshal the message itself and the router marshalled
// the result again; now the fan-out encodes once and the bytes pass through.
func BenchmarkBroadcastToRoom_1000(b *testing.B) {
	m, rooms := benchManager(b)
	ctx := context.Background()

	if err := rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "room", Name: "room", Owner: "o"})); err != nil {
		b.Fatal(err)
	}

	conns := make([]Connection, 0, 1000)

	for i := range 1000 {
		ec := NewConnection(&routerLikeConn{recordingConn: recordingConn{id: fmt.Sprintf("c%d", i)}},
			WithSendQueueCapacity(1<<20))
		ec.SetUserID(fmt.Sprintf("u%d", i))

		if err := m.Register(ec); err != nil {
			b.Fatal(err)
		}

		if err := m.JoinRoom(ctx, ec.ID(), "room"); err != nil {
			b.Fatal(err)
		}

		conns = append(conns, ec)
	}

	b.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close()
		}
	})

	msg := benchMessage()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = m.BroadcastToRoom(ctx, "room", msg)
	}
}
