package streaming

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
)

// The heartbeat's liveness contract, pinned.
//
// These are characterization tests: they assert what the server already does,
// because what it does is the other half of a contract the browser client was
// not holding up. `heartbeat` judges liveness by GetLastActivity, and
// UpdateActivity is called in exactly one place -- the read loop, once
// conn.Read() has returned an application message. Nothing else moves it. Not
// the ping the server itself writes, and not a WebSocket control frame, because
// there is no pong handler anywhere in the tree.
//
// So a client that only ever subscribes and listens is closed on an interval,
// and at the defaults that interval is PingInterval + PongTimeout = 40s. That
// is correct behaviour for detecting a peer an intermediary dropped, and it is
// also why a purely-listening client has to answer the ping. These tests are
// what makes the second half of that sentence fail loudly if the timing rule
// is ever changed without the client being changed with it.

// newHeartbeatExtension builds the smallest Extension `heartbeat` reads from.
//
// The logger is real rather than nil: the close path logs, and Logger() returns
// the field unguarded, so a nil logger turns the very branch under test into a
// panic.
func newHeartbeatExtension(t *testing.T, ping, pong time.Duration) *Extension {
	t.Helper()

	base := forge.NewBaseExtension("streaming", "test", "heartbeat tests")
	base.SetLogger(forge.NewNoopLogger())

	cfg := Config{PingInterval: ping, PongTimeout: pong}

	return &Extension{BaseExtension: base, config: cfg}
}

// waitClosed polls until the connection is closed or the deadline passes.
//
// Polled rather than slept: the heartbeat runs on its own goroutine against a
// real ticker, so the moment of close is only bounded, not exact. A fixed sleep
// long enough to be reliable is also long enough to hide a regression that
// merely doubles the interval.
func waitClosed(conn Connection, within time.Duration) bool {
	deadline := time.Now().Add(within)

	for time.Now().Before(deadline) {
		if conn.IsClosed() {
			return true
		}

		time.Sleep(time.Millisecond)
	}

	return conn.IsClosed()
}

func TestHeartbeat_ClosesConnectionWithNoInboundActivity(t *testing.T) {
	t.Parallel()

	const (
		ping = 20 * time.Millisecond
		pong = 10 * time.Millisecond
	)

	ext := newHeartbeatExtension(t, ping, pong)
	conn := newTestConn("idle-1", "user-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ext.heartbeat(ctx, conn, conn)

	// Nothing ever calls UpdateActivity, which is exactly a browser that
	// subscribed and then only listened. The connection must not survive it.
	if !waitClosed(conn, 10*(ping+pong)) {
		t.Fatalf("connection with no inbound activity was not closed within %v", 10*(ping+pong))
	}
}

func TestHeartbeat_InboundActivityKeepsConnectionOpen(t *testing.T) {
	t.Parallel()

	const (
		ping = 20 * time.Millisecond
		pong = 10 * time.Millisecond
	)

	ext := newHeartbeatExtension(t, ping, pong)
	conn := newTestConn("busy-1", "user-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ext.heartbeat(ctx, conn, conn)

	// A client answering the ping is indistinguishable, to the heartbeat, from
	// one sending application traffic: both land in the read loop and both call
	// UpdateActivity. This is the behaviour the client fix relies on.
	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 40 {
			conn.UpdateActivity()
			time.Sleep(ping / 4)
		}
	}()

	<-done

	if conn.IsClosed() {
		t.Fatal("connection was closed despite continuous inbound activity")
	}
}

func TestHeartbeat_SendsPingOnTheConfiguredInterval(t *testing.T) {
	t.Parallel()

	const (
		ping = 20 * time.Millisecond
		pong = 10 * time.Millisecond
	)

	ext := newHeartbeatExtension(t, ping, pong)
	conn := newTestConn("pinged-1", "user-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Kept alive so the run is about what gets written, not about the close.
	stop := make(chan struct{})

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				conn.UpdateActivity()
				time.Sleep(ping / 4)
			}
		}
	}()

	go ext.heartbeat(ctx, conn, conn)

	time.Sleep(3 * ping)
	close(stop)

	if got := conn.rec.jsonWriteCount(); got == 0 {
		t.Fatal("heartbeat wrote no ping messages")
	}

	msg := conn.rec.lastJSON(t)
	if msg.Event != "ping" {
		t.Errorf("heartbeat wrote event %q, want %q", msg.Event, "ping")
	}

	// The ping is an application message, not a WebSocket control frame. That
	// is the whole reason a browser cannot answer it for free: browsers reply
	// to control pings automatically and have no API to send one, so this
	// payload has to be answered by application code or not at all.
	if msg.Type != MessageTypeSystem {
		t.Errorf("ping type = %q, want %q", msg.Type, MessageTypeSystem)
	}
}

func TestHeartbeat_DisabledWhenIntervalNotPositive(t *testing.T) {
	t.Parallel()

	ext := newHeartbeatExtension(t, 0, 10*time.Millisecond)
	conn := newTestConn("disabled-1", "user-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ext.heartbeat(ctx, conn, conn)

	time.Sleep(30 * time.Millisecond)

	if conn.IsClosed() {
		t.Fatal("connection was closed with the heartbeat disabled")
	}

	if got := conn.rec.jsonWriteCount(); got != 0 {
		t.Errorf("disabled heartbeat wrote %d messages, want 0", got)
	}
}

func TestHeartbeat_StopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()

	const ping = 20 * time.Millisecond

	ext := newHeartbeatExtension(t, ping, 10*time.Millisecond)
	conn := newTestConn("cancelled-1", "user-1")

	ctx, cancel := context.WithCancel(context.Background())

	go ext.heartbeat(ctx, conn, conn)

	cancel()

	// Past the point where an un-cancelled heartbeat would have closed it.
	time.Sleep(4 * ping)

	if conn.IsClosed() {
		t.Fatal("heartbeat closed the connection after its context was cancelled")
	}
}

// The other half of the contract: the answer has to be harmless.
//
// A client answering the ping sends `{"type":"system","event":"pong"}` up the
// socket, where it lands in the read loop and reaches handleMessage. The read
// loop calls UpdateActivity before routing, so liveness is already recorded by
// the time this runs and the only thing left to check is that routing does not
// object. It does not: the type switch has no case for system messages and no
// default, so the message falls through and returns nil.
//
// This is a regression guard rather than an observation. Adding a default that
// errors on unrecognised types is an entirely reasonable-looking change, and it
// would turn every keepalive answer from every browser into an error.
func TestHandleMessage_AcceptsSystemPong(t *testing.T) {
	t.Parallel()

	ext := &Extension{config: Config{}}
	conn := newTestConn("pong-1", "user-1")

	msg := &Message{
		ID:    "hb-1",
		Type:  MessageTypeSystem,
		Event: "pong",
	}

	if err := ext.handleMessage(context.Background(), conn, msg); err != nil {
		t.Fatalf("handleMessage rejected a keepalive answer: %v", err)
	}
}
