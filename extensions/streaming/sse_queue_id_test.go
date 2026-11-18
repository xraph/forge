package streaming

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// flushConn waits for everything queued on an enhanced connection to reach
// its transport.
func flushConn(t *testing.T, conn Connection) {
	t.Helper()

	f, ok := conn.(interface {
		Flush(ctx context.Context) error
	})
	if !ok {
		t.Fatal("connection has no Flush")
	}

	if err := f.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Every production write goes through the send queue, which encodes on the
// caller's goroutine and gives the writer bytes. sseConnection.WriteJSON only
// emitted an id for a *Message, so through the queue no event ever had one and
// Last-Event-ID resume could not work.
func TestSSE_SequencedMessageThroughQueueCarriesID(t *testing.T) {
	stream := &fakeStream{}
	conn := NewConnectionWithTransport(NewSSEConnection(stream, "", ""), TransportSSE)

	assertNoError(t, conn.WriteJSON(&Message{ID: "a", Type: MessageTypeMessage, RoomID: "room-1", Sequence: 3}))
	assertNoError(t, conn.WriteJSON(&Message{ID: "b", Type: MessageTypeMessage, RoomID: "room-2", Sequence: 9}))
	assertNoError(t, conn.WriteJSON(&Message{ID: "c", Type: MessageTypePresence}))
	flushConn(t, conn)

	events := stream.snapshot()
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	cursor, err := decodeReplayCursor(events[1].id)
	if err != nil {
		t.Fatalf("second event id %q is not a cursor: %v", events[1].id, err)
	}

	if cursor["room-1"] != 3 || cursor["room-2"] != 9 {
		t.Fatalf("cursor = %v, want room-1:3 room-2:9", cursor)
	}

	if events[2].id != "" {
		t.Fatalf("unsequenced presence event got id %q", events[2].id)
	}
}

// The same holds when a broadcast fans one shared encoding out to many SSE
// connections.
func TestSSE_RoomBroadcastCarriesID(t *testing.T) {
	m, rooms, _ := realManager(t)
	ctx := context.Background()

	assertNoError(t, rooms.Create(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})))

	const n = 12 // above the serial fan-out threshold, so the worker pool runs

	streams := make([]*fakeStream, n)
	conns := make([]Connection, n)

	for i := range n {
		streams[i] = &fakeStream{}
		conns[i] = NewConnectionWithTransport(NewSSEConnection(streams[i], "", ""), TransportSSE)
	}

	for i, c := range conns {
		c.SetUserID(fmt.Sprintf("user-%d", i))
		assertNoError(t, m.Register(c))
		assertNoError(t, m.JoinRoom(ctx, c.ID(), "lobby"))
	}

	assertNoError(t, m.BroadcastToRoom(ctx, "lobby", &Message{ID: "m", Type: MessageTypeMessage, RoomID: "lobby", Sequence: 4, Data: "hi"}))

	for i, c := range conns {
		flushConn(t, c)

		events := streams[i].snapshot()
		if len(events) != 1 {
			t.Fatalf("conn %d: got %d events, want 1", i, len(events))
		}

		cursor, err := decodeReplayCursor(events[0].id)
		if err != nil || cursor["lobby"] != 4 {
			t.Fatalf("conn %d: event id %q, want a cursor at lobby:4 (err %v)", i, events[0].id, err)
		}
	}
}

// End to end over HTTP: a client that reconnects with the id it last saw gets
// the message it missed while away.
func TestHandleSSE_LastEventIDResumesMissedMessages(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{Name: "sse-resume"})
	ext := NewExtension(WithLocalBackend())

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

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
	mgr := ext.Manager()

	if err := mgr.CreateRoom(ctx, local.NewRoom(streaming.RoomOptions{ID: "lobby", Name: "lobby", Owner: "o"})); err != nil {
		t.Fatal(err)
	}

	publish := func(data string) {
		t.Helper()

		msg := &Message{ID: data, Type: MessageTypeMessage, RoomID: "lobby", Data: data, Timestamp: time.Now()}
		assertNoError(t, mgr.SaveMessage(ctx, msg))
		assertNoError(t, mgr.BroadcastToRoom(ctx, "lobby", msg))
	}

	// open connects and returns a function yielding the next event's id and
	// data lines.
	open := func(lastEventID string) (func() (string, string), func()) {
		t.Helper()

		// Bounded, so a missing event fails the test instead of hanging it.
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, srv.URL+"/sse?rooms=lobby&uid=alice", nil)
		if err != nil {
			t.Fatal(err)
		}

		if lastEventID != "" {
			req.Header.Set("Last-Event-ID", lastEventID)
		}

		resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed by the returned stop function
		if err != nil {
			t.Fatal(err)
		}

		sc := bufio.NewScanner(resp.Body)

		next := func() (string, string) {
			var id, data string

			for sc.Scan() {
				line := sc.Text()

				switch {
				case strings.HasPrefix(line, "id: "):
					id = strings.TrimPrefix(line, "id: ")
				case strings.HasPrefix(line, "data: "):
					data += strings.TrimPrefix(line, "data: ")
				case line == "":
					if data != "" {
						return id, data
					}
				}
			}

			t.Fatalf("stream ended: %v", sc.Err())

			return "", ""
		}

		return next, func() { cancel(); _ = resp.Body.Close() }
	}

	waitFor := func(next func() (string, string), text string) string {
		t.Helper()

		for {
			id, data := next()
			if strings.Contains(data, `"data":"`+text+`"`) {
				return id
			}
		}
	}

	next, closeFirst := open("")
	defer closeFirst()

	next() // the connected event; rooms are joined right after it

	// The join from ?rooms= runs after the connected event is written.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conns := mgr.GetUserConnections("alice"); len(conns) == 1 && conns[0].IsInRoom("lobby") {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	publish("one")

	id := waitFor(next, "one")
	if id == "" {
		t.Fatal("room message arrived with no SSE id; the client has nothing to resume from")
	}

	closeFirst()

	for time.Now().Before(deadline.Add(2*time.Second)) && mgr.ConnectionCount() > 0 {
		time.Sleep(5 * time.Millisecond)
	}

	publish("two") // missed: nobody is connected

	next2, closeSecond := open(id)
	defer closeSecond()

	done := make(chan struct{})

	go func() {
		defer close(done)

		waitFor(next2, "two")
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnecting with Last-Event-ID did not replay the missed message")
	}
}
