package streaming

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/internal"
)

// TestConnectionDeliversFramesInOrder checks the queue preserves ordering
// across both write paths - the JSON path used for normal delivery and the raw
// path used by the codec.
func TestConnectionDeliversFramesInOrder(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	const n = 20

	for i := range n {
		if i%2 == 0 {
			if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, fmt.Sprintf("f%d", i))); err != nil {
				t.Fatalf("WriteJSON %d: %v", i, err)
			}

			continue
		}

		if err := conn.Write([]byte(fmt.Sprintf(`{"id":"f%d"}`, i))); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	sqWaitFor(t, 2*time.Second, "every frame to reach the socket", func() bool {
		return mock.frameCount() == n
	})

	want := make([]string, 0, n)
	for i := range n {
		want = append(want, fmt.Sprintf("f%d", i))
	}

	if got := mock.frameIDs(t); !sqEqualStrings(got, want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
}

// TestConnectionWriteJSONPreservesPayload guards the encode-at-enqueue design:
// the frame is marshalled by the caller and replayed through the transport's
// own JSON path, so the bytes on the wire must be unchanged.
func TestConnectionWriteJSONPreservesPayload(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	sent := &internal.Message{
		ID:        "m1",
		Type:      internal.MessageTypeMessage,
		RoomID:    "room-1",
		UserID:    "u1",
		Data:      map[string]any{"text": "hello", "n": float64(3)},
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}

	if err := conn.WriteJSON(sent); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	sqWaitFor(t, 2*time.Second, "the frame to reach the socket", func() bool {
		return mock.frameCount() == 1
	})

	mock.mu.Lock()
	frame := mock.frames[0]
	mock.mu.Unlock()

	direct, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("marshal reference: %v", err)
	}

	if string(frame) != string(direct) {
		t.Fatalf("frame on the wire = %s, want %s", frame, direct)
	}
}

// TestConnectionFlushesQueuedFramesOnClose covers the write-then-close pattern
// the manager uses when kicking a connection: the notice has to reach the
// socket before the connection goes away.
func TestConnectionFlushesQueuedFramesOnClose(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	if err := conn.WriteJSON(sqMessage(internal.MessageTypeSystem, "kick")); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := mock.frameIDs(t); !sqEqualStrings(got, []string{"kick"}) {
		t.Fatalf("delivered %v, want the queued frame to be flushed before close", got)
	}
}

// TestConnectionBinaryFramesAreQueued checks binary frames go through the queue
// and out the transport's binary path. forge.Connection does not declare
// WriteBinary yet, so without this the parallel work adding it would promote the
// embedded method and bypass the queue entirely.
func TestConnectionBinaryFramesAreQueued(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	binary, ok := conn.(interface{ WriteBinary([]byte) error })
	if !ok {
		t.Fatal("connection does not expose WriteBinary")
	}

	if err := binary.WriteBinary([]byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("WriteBinary: %v", err)
	}

	sqWaitFor(t, 2*time.Second, "the binary frame to reach the socket", func() bool {
		return mock.binaryWriteCount() == 1
	})

	if got := sqStats(t, conn); got.Written != 1 {
		t.Fatalf("Written = %d, want 1 - the frame did not go through the queue", got.Written)
	}
}

// TestConnectionFlushAwaitsDelivery covers the affordance that replaces the
// synchronous write: a caller that needs to know a frame reached the wire can
// no longer read that off the write's return value.
func TestConnectionFlushAwaitsDelivery(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	flusher, ok := conn.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("connection does not expose Flush")
	}

	const n = 10

	for i := range n {
		if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, fmt.Sprintf("m%d", i))); err != nil {
			t.Fatalf("WriteJSON %d: %v", i, err)
		}
	}

	if err := flusher.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if got := mock.frameCount(); got != n {
		t.Fatalf("after Flush, %d frames reached the socket, want %d", got, n)
	}
}

// TestConnectionFlushHonoursContext checks Flush gives up on a consumer that
// has stopped reading, rather than becoming the synchronous block this queue
// exists to remove.
func TestConnectionFlushHonoursContext(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c").stalled()
	conn := NewConnection(mock, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	flusher, ok := conn.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("connection does not expose Flush")
	}

	sqParkWriter(t, conn, mock, "parked")

	if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, "m1")); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := flusher.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Flush on a stalled consumer = %v, want %v", err, context.DeadlineExceeded)
	}

	// A barrier must not count against capacity, and must not disconnect.
	if conn.IsClosed() {
		t.Fatal("Flush disconnected the connection")
	}
}

// TestConnectionFlushOnClosedConnection checks a flush after teardown reports
// closure instead of blocking forever on a barrier nobody will reach.
func TestConnectionFlushOnClosedConnection(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	flusher, ok := conn.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("connection does not expose Flush")
	}

	if err := flusher.Flush(context.Background()); !errors.Is(err, ErrSendQueueClosed) {
		t.Fatalf("Flush after close = %v, want %v", err, ErrSendQueueClosed)
	}
}

// TestWriteFrameKeepsTypeAcrossTheEncodeBoundary is the fix for the codec path.
// A connection that negotiated a non-JSON content type sends every frame as
// pre-encoded bytes, so before WriteFrame existed the queue saw no message type
// at all and had to treat a typing snapshot as undroppable - switching the
// per-type policy off wholesale for that client.
func TestWriteFrameKeepsTypeAcrossTheEncodeBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		binary bool
	}{
		{"text codec", false},
		{"binary codec", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const capacity = 4

			mock := newSQMockConn("c").stalled()
			conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(capacity))...)

			t.Cleanup(func() { _ = conn.Close() })

			writer, ok := conn.(interface{ WriteFrame(OutboundFrame) error })
			if !ok {
				t.Fatal("connection does not expose WriteFrame")
			}

			encoded := func(id string) []byte { return []byte(`{"id":"` + id + `"}`) }

			sqParkWriter(t, conn, mock, "parked")

			for i := 1; i <= capacity; i++ {
				err := writer.WriteFrame(OutboundFrame{
					Data:   encoded(fmt.Sprintf("t%d", i)),
					Type:   internal.MessageTypeTyping,
					Binary: tc.binary,
				})
				if err != nil {
					t.Fatalf("filling queue at %d: %v", i, err)
				}
			}

			// Full queue of typing frames. This one must evict the oldest, not
			// disconnect - which is what an untyped frame would have done.
			err := writer.WriteFrame(OutboundFrame{
				Data:   encoded("t5"),
				Type:   internal.MessageTypeTyping,
				Binary: tc.binary,
			})
			if err != nil {
				t.Fatalf("typing overflow must not fail: %v", err)
			}

			if conn.IsClosed() {
				t.Fatal("an encoded typing frame disconnected the connection")
			}

			if got := sqStats(t, conn); got.Dropped != 1 {
				t.Fatalf("Dropped = %d, want 1", got.Dropped)
			}

			// The same queue state with a message frame still disconnects: the
			// policy is per type, not per queue.
			err = writer.WriteFrame(OutboundFrame{
				Data:   encoded("m1"),
				Type:   internal.MessageTypeMessage,
				Binary: tc.binary,
			})
			if !errors.Is(err, ErrSendQueueOverflow) {
				t.Fatalf("encoded message overflow = %v, want %v", err, ErrSendQueueOverflow)
			}

			sqWaitFor(t, 2*time.Second, "the connection to report closed", conn.IsClosed)
		})
	}
}

// TestWriteFrameRoutesBinaryAndText checks the Binary flag picks the transport
// method. A binary payload sent as a text frame breaks the UTF-8 promise and a
// browser fails the connection with close code 1007.
func TestWriteFrameRoutesBinaryAndText(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	writer, ok := conn.(interface{ WriteFrame(OutboundFrame) error })
	if !ok {
		t.Fatal("connection does not expose WriteFrame")
	}

	if err := writer.WriteFrame(OutboundFrame{Data: []byte("text"), Type: internal.MessageTypeMessage}); err != nil {
		t.Fatalf("text WriteFrame: %v", err)
	}

	if err := writer.WriteFrame(OutboundFrame{Data: []byte{0xff, 0xfe}, Type: internal.MessageTypeMessage, Binary: true}); err != nil {
		t.Fatalf("binary WriteFrame: %v", err)
	}

	sqWaitFor(t, 2*time.Second, "both frames to reach the socket", func() bool {
		return mock.frameCount() == 2
	})

	if got := mock.binaryWriteCount(); got != 1 {
		t.Fatalf("binary writes = %d, want exactly 1 (the text frame must not take the binary path)", got)
	}
}

// TestConnectionCloseIsIdempotent covers repeated and concurrent teardown: the
// underlying connection is closed once and the error is stable.
func TestConnectionCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	for i := range 3 {
		if err := conn.Close(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}

	if got := mock.closeCount(); got != 1 {
		t.Fatalf("underlying connection closed %d times, want 1", got)
	}
}

// TestConnectionIsClosedTracksLifecycle pins the MarkClosed/IsClosed pair that
// had no caller before the writer goroutine's exit path used it.
func TestConnectionIsClosedTracksLifecycle(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts()...)

	if conn.IsClosed() {
		t.Fatal("a fresh connection reports closed")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !conn.IsClosed() {
		t.Fatal("connection does not report closed after Close")
	}
}

// TestConnectionWriteFailureClosesConnection checks a socket that has gone away
// takes the connection down with it, rather than leaving a writer goroutine
// spinning on a dead socket.
func TestConnectionWriteFailureClosesConnection(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	mock.writeErr = errors.New("broken pipe")

	conn := NewConnection(mock, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, "m1")); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	sqWaitFor(t, 2*time.Second, "the connection to report closed", conn.IsClosed)

	if got := mock.closeCount(); got == 0 {
		t.Fatal("underlying connection was never closed after a write failure")
	}

	stats := sqStats(t, conn)
	if stats.WriteErrors != 1 {
		t.Fatalf("WriteErrors = %d, want 1", stats.WriteErrors)
	}
}

// TestConnectionSendQueueStats checks the counters the manager reads when no
// metrics handle is wired in.
func TestConnectionSendQueueStats(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("c")
	conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(16))...)

	t.Cleanup(func() { _ = conn.Close() })

	if got := sqStats(t, conn); got.Capacity != 16 {
		t.Fatalf("Capacity = %d, want 16", got.Capacity)
	}

	const n = 5

	for i := range n {
		if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, fmt.Sprintf("m%d", i))); err != nil {
			t.Fatalf("WriteJSON %d: %v", i, err)
		}
	}

	sqWaitFor(t, 2*time.Second, "the queue to drain", func() bool {
		return sqStats(t, conn).Written == n
	})

	stats := sqStats(t, conn)
	if stats.Enqueued != n {
		t.Fatalf("Enqueued = %d, want %d", stats.Enqueued, n)
	}

	if stats.Depth != 0 {
		t.Fatalf("Depth = %d, want 0 once drained", stats.Depth)
	}

	if stats.Dropped != 0 || stats.OverflowDisconnects != 0 || stats.WriteErrors != 0 {
		t.Fatalf("unexpected failure counters: %+v", stats)
	}
}

// TestSendQueueCapacityDefaults checks a non-positive capacity falls back to the
// package default rather than producing a zero-length queue that overflows on
// the first frame.
func TestSendQueueCapacityDefaults(t *testing.T) {
	t.Parallel()

	for _, capacity := range []int{0, -1} {
		mock := newSQMockConn("c")
		conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(capacity))...)

		if got := sqStats(t, conn).Capacity; got != DefaultSendQueueCapacity {
			t.Fatalf("capacity %d produced Capacity = %d, want %d", capacity, got, DefaultSendQueueCapacity)
		}

		_ = conn.Close()
	}
}

// TestMessageTypeOfClassification pins the overflow policy's input. Anything the
// queue cannot identify has to fall through to the conservative policy - a frame
// misread as droppable would be a silently lost message.
func TestMessageTypeOfClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		payload    any
		wantType   string
		wantPolicy overflowPolicy
	}{
		{"pointer message", &internal.Message{Type: internal.MessageTypeMessage}, internal.MessageTypeMessage, policyDisconnect},
		{"value message", internal.Message{Type: internal.MessageTypeMessage}, internal.MessageTypeMessage, policyDisconnect},
		{"typing", &internal.Message{Type: internal.MessageTypeTyping}, internal.MessageTypeTyping, policyDropOldest},
		{"presence", &internal.Message{Type: internal.MessageTypePresence}, internal.MessageTypePresence, policyDropOldest},
		{"system", &internal.Message{Type: internal.MessageTypeSystem}, internal.MessageTypeSystem, policyDisconnect},
		{"join", &internal.Message{Type: internal.MessageTypeJoin}, internal.MessageTypeJoin, policyDisconnect},
		{"nil pointer", (*internal.Message)(nil), "", policyDisconnect},
		{"unrelated payload", map[string]any{"type": "typing"}, "", policyDisconnect},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := messageTypeOf(tt.payload); got != tt.wantType {
				t.Fatalf("messageTypeOf = %q, want %q", got, tt.wantType)
			}

			if got := policyForType(messageTypeOf(tt.payload)); got != tt.wantPolicy {
				t.Fatalf("policyForType = %v, want %v", got, tt.wantPolicy)
			}
		})
	}
}

// TestConnectionMetadataSurvivesQueueWiring is a guard on the wrapper: adding
// the send queue must not disturb what the manager reads off a connection.
func TestConnectionMetadataSurvivesQueueWiring(t *testing.T) {
	t.Parallel()

	mock := newSQMockConn("conn-1")
	conn := NewConnectionWithTransport(mock, TransportSSE, sqTestOpts()...)

	t.Cleanup(func() { _ = conn.Close() })

	conn.SetUserID("u1")
	conn.SetSessionID("s1")
	conn.SetContentType("application/json")
	conn.AddRoom("room-1")
	conn.AddSubscription("chan-1")

	if got := conn.ID(); got != "conn-1" {
		t.Fatalf("ID = %q, want conn-1", got)
	}

	if got := conn.GetTransport(); got != TransportSSE {
		t.Fatalf("GetTransport = %q, want %q", got, TransportSSE)
	}

	if got := conn.GetUserID(); got != "u1" {
		t.Fatalf("GetUserID = %q, want u1", got)
	}

	if got := conn.GetSessionID(); got != "s1" {
		t.Fatalf("GetSessionID = %q, want s1", got)
	}

	if got := conn.GetContentType(); got != "application/json" {
		t.Fatalf("GetContentType = %q, want application/json", got)
	}

	if !conn.IsInRoom("room-1") {
		t.Fatal("IsInRoom = false, want true")
	}

	if !conn.IsSubscribed("chan-1") {
		t.Fatal("IsSubscribed = false, want true")
	}
}
