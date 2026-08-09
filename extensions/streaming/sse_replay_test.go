package streaming

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/xraph/forge"
)

// The SSE half of gap-free reconnect.
//
// EventSource resumes by echoing the last `id:` it saw back as Last-Event-ID.
// So the id a stream emits IS the resume token, and it has to describe the
// client's position in every room the stream carries — not just the room the
// last message happened to belong to. The connection therefore keeps a running
// cursor and emits the whole vector each time.
//
// Emitting only the latest room's sequence would look correct in a single-room
// test and lose messages the moment a second room shared the connection, which
// is why the multi-room case below is the one that matters.

// fakeStream captures what a connection emits, including event ids.
type fakeStream struct {
	forge.Stream

	mu     sync.Mutex
	events []fakeEvent
}

type fakeEvent struct {
	id   string
	name string
	data []byte
}

func (s *fakeStream) Send(event string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, fakeEvent{name: event, data: append([]byte(nil), data...)})

	return nil
}

func (s *fakeStream) SendJSON(event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return s.Send(event, data)
}

func (s *fakeStream) SendWithID(id, event string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, fakeEvent{id: id, name: event, data: append([]byte(nil), data...)})

	return nil
}

func (s *fakeStream) SendJSONWithID(id, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return s.SendWithID(id, event, data)
}

func (s *fakeStream) Context() context.Context { return context.Background() }

func (s *fakeStream) Close() error { return nil }

func (s *fakeStream) snapshot() []fakeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]fakeEvent(nil), s.events...)
}

func TestSSEConnection_EmitsCursorAsEventID(t *testing.T) {
	stream := &fakeStream{}
	conn := NewSSEConnection(stream, "1.2.3.4:1", "")

	if err := conn.WriteJSON(&Message{ID: "m1", RoomID: "room-1", Sequence: 7}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	events := stream.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if events[0].id == "" {
		t.Fatal("event id is empty; a client has nothing to resume from")
	}

	cursor, err := decodeReplayCursor(events[0].id)
	if err != nil {
		t.Fatalf("event id is not a valid cursor: %v", err)
	}

	if cursor["room-1"] != 7 {
		t.Errorf("cursor[room-1] = %d, want 7", cursor["room-1"])
	}
}

func TestSSEConnection_CursorAccumulatesAcrossRooms(t *testing.T) {
	// The case a single-room test cannot see: after a message in room-2, the id
	// must still say where the client got to in room-1, or a reconnect replays
	// room-1 from the beginning — or not at all.
	stream := &fakeStream{}
	conn := NewSSEConnection(stream, "1.2.3.4:1", "")

	for _, msg := range []*Message{
		{ID: "a", RoomID: "room-1", Sequence: 3},
		{ID: "b", RoomID: "room-2", Sequence: 9},
	} {
		if err := conn.WriteJSON(msg); err != nil {
			t.Fatalf("WriteJSON: %v", err)
		}
	}

	events := stream.snapshot()

	cursor, err := decodeReplayCursor(events[len(events)-1].id)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}

	if cursor["room-1"] != 3 {
		t.Errorf("cursor[room-1] = %d, want 3 — an earlier room must survive a later one", cursor["room-1"])
	}

	if cursor["room-2"] != 9 {
		t.Errorf("cursor[room-2] = %d, want 9", cursor["room-2"])
	}
}

func TestSSEConnection_CursorNeverGoesBackwards(t *testing.T) {
	// Out-of-order delivery is normal in distributed mode. If a late message
	// with a lower sequence rewound the cursor, a reconnect would redeliver
	// everything between — duplicates the client has no way to detect.
	stream := &fakeStream{}
	conn := NewSSEConnection(stream, "1.2.3.4:1", "")

	for _, seq := range []int64{5, 2} {
		if err := conn.WriteJSON(&Message{ID: "m", RoomID: "room-1", Sequence: seq}); err != nil {
			t.Fatalf("WriteJSON: %v", err)
		}
	}

	events := stream.snapshot()

	cursor, err := decodeReplayCursor(events[len(events)-1].id)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}

	if cursor["room-1"] != 5 {
		t.Errorf("cursor[room-1] = %d, want 5 — the high-water mark, not the last seen", cursor["room-1"])
	}
}

func TestSSEConnection_UnsequencedMessagesCarryNoID(t *testing.T) {
	// Presence, typing and system frames are not persisted and have no
	// sequence. Tagging them with the current cursor would be harmless, but
	// tagging them with a fabricated one would corrupt the client's resume
	// point, so they simply carry no id.
	stream := &fakeStream{}
	conn := NewSSEConnection(stream, "1.2.3.4:1", "")

	if err := conn.WriteJSON(&Message{ID: "t1", Type: MessageTypeTyping}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	events := stream.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	if events[0].id != "" {
		t.Errorf("event id = %q, want empty for an unsequenced frame", events[0].id)
	}
}

// A stream that owns its own event IDs and refuses caller-supplied ones.
//
// This is what forge.WithEventLog produces: the router's event log assigns
// positions, so it rejects an ID from the handler rather than let the log and
// the wire disagree about where a client is. That refusal is correct on its own
// terms — but this connection supplies an ID for every sequenced room message,
// so without a fallback the two features combine into total message loss on any
// SSE route registered with both.
type idRefusingStream struct {
	fakeStream
}

func (s *idRefusingStream) SendWithID(_, _ string, _ []byte) error {
	return forge.ErrEventIDAssignedByLog
}

func (s *idRefusingStream) SendJSONWithID(_, _ string, _ any) error {
	return forge.ErrEventIDAssignedByLog
}

func TestSSEConnection_DeliversWhenTheStreamOwnsEventIDs(t *testing.T) {
	stream := &idRefusingStream{}
	conn := NewSSEConnection(stream, "1.2.3.4:1", "")

	// A sequenced room message: exactly the case that supplies an ID.
	if err := conn.WriteJSON(&Message{ID: "m1", RoomID: "room-1", Sequence: 7}); err != nil {
		t.Fatalf("WriteJSON returned %v; the message must still be delivered without its cursor", err)
	}

	events := stream.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 — the message was dropped", len(events))
	}

	if events[0].id != "" {
		t.Errorf("event id = %q, want empty; the stream assigns its own", events[0].id)
	}
}
