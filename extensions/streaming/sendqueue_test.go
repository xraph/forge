package streaming

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// sqMockConn is a forge.Connection whose writes can be parked, so a test can
// simulate a consumer that has stopped reading.
type sqMockConn struct {
	id string

	// gate, when non-nil, makes every write wait for a token sent by step or
	// for the gate to be closed by openGate. A gate nobody feeds is a consumer
	// that never drains.
	gate chan struct{}

	// entered receives a token as each write begins, so a test can synchronise
	// on "the writer goroutine is now parked inside the socket write".
	entered chan struct{}

	closedCh  chan struct{}
	closeOnce sync.Once

	mu           sync.Mutex
	frames       [][]byte
	closes       int
	binaryWrites int
	writeErr     error
}

func newSQMockConn(id string) *sqMockConn {
	return &sqMockConn{
		id:       id,
		entered:  make(chan struct{}, 1024),
		closedCh: make(chan struct{}),
	}
}

// stalled returns a mock whose writes sqParkWriter until openGate is called. Nothing
// reaches the socket in the meantime.
func (m *sqMockConn) stalled() *sqMockConn {
	m.gate = make(chan struct{})

	return m
}

// openGate lets every parked and future write through.
func (m *sqMockConn) openGate() {
	close(m.gate)
}

func (m *sqMockConn) ID() string { return m.id }

func (m *sqMockConn) Read() ([]byte, error) { return nil, errors.New("not supported") }

func (m *sqMockConn) ReadJSON(any) error { return errors.New("not supported") }

func (m *sqMockConn) Write(data []byte) error {
	select {
	case m.entered <- struct{}{}:
	default:
	}

	// A closed connection fails fast, and takes priority over an open gate so
	// the outcome is not decided by a random select.
	select {
	case <-m.closedCh:
		return errors.New("write on closed connection")
	default:
	}

	if m.gate != nil {
		select {
		case <-m.gate:
		case <-m.closedCh:
			return errors.New("write on closed connection")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeErr != nil {
		return m.writeErr
	}

	m.frames = append(m.frames, append([]byte(nil), data...))

	return nil
}

// WriteBinary is not part of forge.Connection today. It is implemented so the
// mock keeps satisfying the interface once the parallel write-deadline work
// adds it, and so a test can prove binary frames take this path rather than
// falling back to Write.
func (m *sqMockConn) WriteBinary(data []byte) error {
	m.mu.Lock()
	m.binaryWrites++
	m.mu.Unlock()

	return m.Write(data)
}

func (m *sqMockConn) binaryWriteCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.binaryWrites
}

func (m *sqMockConn) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return m.Write(data)
}

func (m *sqMockConn) Close() error {
	m.closeOnce.Do(func() { close(m.closedCh) })

	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes++

	return nil
}

func (m *sqMockConn) Context() context.Context { return context.Background() }

func (m *sqMockConn) RemoteAddr() string { return "127.0.0.1:1234" }

func (m *sqMockConn) LocalAddr() string { return "127.0.0.1:80" }

func (m *sqMockConn) frameCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.frames)
}

func (m *sqMockConn) closeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.closes
}

// frameIDs decodes the message ID out of every frame that reached the socket,
// in the order it arrived.
func (m *sqMockConn) frameIDs(t *testing.T) []string {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make([]string, 0, len(m.frames))

	for _, frame := range m.frames {
		var msg internal.Message
		if err := json.Unmarshal(frame, &msg); err != nil {
			t.Fatalf("decode frame %q: %v", frame, err)
		}

		ids = append(ids, msg.ID)
	}

	return ids
}

var _ forge.Connection = (*sqMockConn)(nil)

// sqTestOpts keeps teardown fast: the production flush window would otherwise
// add a second to every test that closes a stalled connection.
func sqTestOpts(extra ...ConnOption) []ConnOption {
	return append([]ConnOption{
		WithSendQueueTimeouts(100*time.Millisecond, 2*time.Second),
	}, extra...)
}

func sqMessage(msgType, id string) *internal.Message {
	return &internal.Message{ID: id, Type: msgType, UserID: "u1"}
}

func sqStats(t *testing.T, conn Connection) SendQueueStats {
	t.Helper()

	s, ok := conn.(interface{ SendQueueStats() SendQueueStats })
	if !ok {
		t.Fatalf("connection %T does not expose SendQueueStats", conn)
	}

	return s.SendQueueStats()
}

func sqWaitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("timed out after %s waiting for %s", timeout, desc)
}

// sqParkWriter fills the writer goroutine with one frame and waits until it is blocked
// inside the socket write, leaving the queue empty and drained by nobody.
func sqParkWriter(t *testing.T, conn Connection, mock *sqMockConn, id string) {
	t.Helper()

	if err := conn.WriteJSON(sqMessage(internal.MessageTypeTyping, id)); err != nil {
		t.Fatalf("priming write: %v", err)
	}

	select {
	case <-mock.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine never reached the socket")
	}
}

// TestSlowConsumerDoesNotBlockFastConsumer is the defect this queue exists to
// fix: delivery used to run straight into the socket, so a room member who had
// stopped reading stalled delivery to everyone behind them.
func TestSlowConsumerDoesNotBlockFastConsumer(t *testing.T) {
	t.Parallel()

	slowMock := newSQMockConn("slow").stalled()
	fastMock := newSQMockConn("fast")

	slow := NewConnection(slowMock, sqTestOpts()...)
	fast := NewConnection(fastMock, sqTestOpts()...)

	t.Cleanup(func() {
		_ = slow.Close()
		_ = fast.Close()
	})

	const n = 64

	// Deliver serially, exactly as BroadcastToRoom does for a small room, with
	// the stalled member first.
	errCh := make(chan error, 1)

	go func() {
		for i := range n {
			msg := sqMessage(internal.MessageTypeMessage, fmt.Sprintf("m%d", i))

			if err := slow.WriteJSON(msg); err != nil {
				errCh <- fmt.Errorf("slow member write %d: %w", i, err)

				return
			}

			if err := fast.WriteJSON(msg); err != nil {
				errCh <- fmt.Errorf("fast member write %d: %w", i, err)

				return
			}
		}

		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delivery blocked behind the stalled consumer")
	}

	sqWaitFor(t, 2*time.Second, "the fast consumer to receive every message", func() bool {
		return fastMock.frameCount() == n
	})

	// The stalled consumer is still parked in its first write, holding nothing
	// up: its frames are queued, not delivered.
	if got := slowMock.frameCount(); got != 0 {
		t.Fatalf("stalled consumer received %d frames, want 0", got)
	}

	if slow.IsClosed() {
		t.Fatal("stalled consumer was disconnected below capacity")
	}
}

// TestDroppableTypesEvictOldestSameType covers the state-snapshot half of the
// overflow policy: typing and presence frames are only ever the latest value,
// so under pressure the oldest is discarded and the connection survives.
func TestDroppableTypesEvictOldestSameType(t *testing.T) {
	t.Parallel()

	for _, msgType := range []string{internal.MessageTypeTyping, internal.MessageTypePresence} {
		t.Run(msgType, func(t *testing.T) {
			t.Parallel()

			const capacity = 4

			mock := newSQMockConn("c").stalled()
			conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(capacity))...)

			t.Cleanup(func() { _ = conn.Close() })

			sqParkWriter(t, conn, mock, "parked")

			for i := 1; i <= capacity; i++ {
				if err := conn.WriteJSON(sqMessage(msgType, fmt.Sprintf("s%d", i))); err != nil {
					t.Fatalf("filling queue at %d: %v", i, err)
				}
			}

			// The queue is now full. This frame must evict s1, not fail and not
			// disconnect.
			if err := conn.WriteJSON(sqMessage(msgType, "s5")); err != nil {
				t.Fatalf("overflow of %s must not fail: %v", msgType, err)
			}

			if conn.IsClosed() {
				t.Fatalf("overflow of %s must not disconnect", msgType)
			}

			stats := sqStats(t, conn)
			if stats.Dropped != 1 {
				t.Fatalf("Dropped = %d, want 1", stats.Dropped)
			}

			if stats.OverflowDisconnects != 0 {
				t.Fatalf("OverflowDisconnects = %d, want 0", stats.OverflowDisconnects)
			}

			if stats.Depth != capacity {
				t.Fatalf("Depth = %d, want %d", stats.Depth, capacity)
			}

			mock.openGate()

			sqWaitFor(t, 2*time.Second, "the queue to drain", func() bool {
				return mock.frameCount() == capacity+1
			})

			want := []string{"parked", "s2", "s3", "s4", "s5"}
			if got := mock.frameIDs(t); !sqEqualStrings(got, want) {
				t.Fatalf("delivered %v, want %v (oldest same-type frame evicted, order kept)", got, want)
			}
		})
	}
}

// TestDroppableTypeDropsItselfWhenNothingSameTypeIsQueued pins the branch where
// a full queue holds only frames we may not drop. The snapshot is discarded
// rather than evicting a durable frame or killing the connection.
func TestDroppableTypeDropsItselfWhenNothingSameTypeIsQueued(t *testing.T) {
	t.Parallel()

	const capacity = 3

	mock := newSQMockConn("c").stalled()
	conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(capacity))...)

	t.Cleanup(func() { _ = conn.Close() })

	sqParkWriter(t, conn, mock, "parked")

	for i := 1; i <= capacity; i++ {
		if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, fmt.Sprintf("m%d", i))); err != nil {
			t.Fatalf("filling queue at %d: %v", i, err)
		}
	}

	if err := conn.WriteJSON(sqMessage(internal.MessageTypeTyping, "t1")); err != nil {
		t.Fatalf("typing overflow must not fail: %v", err)
	}

	if conn.IsClosed() {
		t.Fatal("typing overflow must not disconnect")
	}

	stats := sqStats(t, conn)
	if stats.Dropped != 1 {
		t.Fatalf("Dropped = %d, want 1", stats.Dropped)
	}

	mock.openGate()

	sqWaitFor(t, 2*time.Second, "the queue to drain", func() bool {
		return mock.frameCount() == capacity+1
	})

	want := []string{"parked", "m1", "m2", "m3"}
	if got := mock.frameIDs(t); !sqEqualStrings(got, want) {
		t.Fatalf("delivered %v, want %v (durable frames kept, snapshot dropped)", got, want)
	}
}

// TestMessageOverflowDisconnects covers the other half of the policy: a chat
// message dropped on the floor is invisible to the client, so the connection is
// torn down instead and the client's reconnect resynchronises.
func TestMessageOverflowDisconnects(t *testing.T) {
	t.Parallel()

	const capacity = 4

	mock := newSQMockConn("c").stalled()
	conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(capacity))...)

	t.Cleanup(func() { _ = conn.Close() })

	sqParkWriter(t, conn, mock, "parked")

	for i := 1; i <= capacity; i++ {
		if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, fmt.Sprintf("m%d", i))); err != nil {
			t.Fatalf("filling queue at %d: %v", i, err)
		}
	}

	err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, "m5"))
	if !errors.Is(err, ErrSendQueueOverflow) {
		t.Fatalf("overflow error = %v, want %v", err, ErrSendQueueOverflow)
	}

	sqWaitFor(t, 2*time.Second, "the connection to report closed", conn.IsClosed)

	if got := mock.closeCount(); got == 0 {
		t.Fatal("underlying connection was never closed")
	}

	// Once shut down the queue refuses further work rather than panicking on a
	// closed channel.
	if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, "m6")); !errors.Is(err, ErrSendQueueClosed) {
		t.Fatalf("post-close write error = %v, want %v", err, ErrSendQueueClosed)
	}

	if err := conn.Write([]byte(`{"type":"raw"}`)); !errors.Is(err, ErrSendQueueClosed) {
		t.Fatalf("post-close raw write error = %v, want %v", err, ErrSendQueueClosed)
	}

	stats := sqStats(t, conn)
	if stats.OverflowDisconnects != 1 {
		t.Fatalf("OverflowDisconnects = %d, want 1", stats.OverflowDisconnects)
	}

	if !stats.Closed {
		t.Fatal("stats report the queue as open after an overflow disconnect")
	}
}

// TestUntypedFrameOverflowDisconnects covers the codec path. Write carries no
// message type, so it takes the conservative policy rather than being silently
// treated as droppable.
func TestUntypedFrameOverflowDisconnects(t *testing.T) {
	t.Parallel()

	const capacity = 2

	mock := newSQMockConn("c").stalled()
	conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(capacity))...)

	t.Cleanup(func() { _ = conn.Close() })

	sqParkWriter(t, conn, mock, "parked")

	for i := range capacity {
		if err := conn.Write([]byte(fmt.Sprintf(`{"id":"b%d"}`, i))); err != nil {
			t.Fatalf("filling queue at %d: %v", i, err)
		}
	}

	if err := conn.Write([]byte(`{"id":"overflow"}`)); !errors.Is(err, ErrSendQueueOverflow) {
		t.Fatalf("overflow error = %v, want %v", err, ErrSendQueueOverflow)
	}

	sqWaitFor(t, 2*time.Second, "the connection to report closed", conn.IsClosed)
}

// TestTypingOverflowIsPerTypeNotPerQueue guards the policy against being read
// as "a full queue is droppable". The queue is full of typing frames, but the
// incoming frame is a message, so it still disconnects.
func TestTypingOverflowIsPerTypeNotPerQueue(t *testing.T) {
	t.Parallel()

	const capacity = 3

	mock := newSQMockConn("c").stalled()
	conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(capacity))...)

	t.Cleanup(func() { _ = conn.Close() })

	sqParkWriter(t, conn, mock, "parked")

	for i := 1; i <= capacity; i++ {
		if err := conn.WriteJSON(sqMessage(internal.MessageTypeTyping, fmt.Sprintf("t%d", i))); err != nil {
			t.Fatalf("filling queue at %d: %v", i, err)
		}
	}

	if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, "m1")); !errors.Is(err, ErrSendQueueOverflow) {
		t.Fatalf("message overflow error = %v, want %v", err, ErrSendQueueOverflow)
	}

	sqWaitFor(t, 2*time.Second, "the connection to report closed", conn.IsClosed)
}

// TestNoGoroutineLeakAfterClose checks that every connection's writer goroutine
// is joined by Close - the queue adds one goroutine per connection, so a leak
// here would be a leak per connection.
func TestNoGoroutineLeakAfterClose(t *testing.T) {
	before := runtime.NumGoroutine()

	const connections = 50

	for i := range connections {
		mock := newSQMockConn(fmt.Sprintf("c%d", i))
		conn := NewConnection(mock, sqTestOpts()...)

		if err := conn.WriteJSON(sqMessage(internal.MessageTypeMessage, "m")); err != nil {
			t.Fatalf("write on connection %d: %v", i, err)
		}

		if err := conn.Close(); err != nil {
			t.Fatalf("close connection %d: %v", i, err)
		}
	}

	sqWaitFor(t, 5*time.Second, "writer goroutines to exit", func() bool {
		return runtime.NumGoroutine() <= before+2
	})
}

// TestNoGoroutineLeakAfterStalledClose does the same for the harder case: a
// consumer parked mid-write, where Close has to break the socket to get its
// writer back.
func TestNoGoroutineLeakAfterStalledClose(t *testing.T) {
	before := runtime.NumGoroutine()

	const connections = 20

	for i := range connections {
		mock := newSQMockConn(fmt.Sprintf("c%d", i)).stalled()
		conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(4))...)

		for j := range 8 {
			_ = conn.WriteJSON(sqMessage(internal.MessageTypeTyping, fmt.Sprintf("t%d", j)))
		}

		if err := conn.Close(); err != nil {
			t.Fatalf("close connection %d: %v", i, err)
		}

		if !conn.IsClosed() {
			t.Fatalf("connection %d not marked closed after Close", i)
		}
	}

	sqWaitFor(t, 10*time.Second, "writer goroutines to exit", func() bool {
		return runtime.NumGoroutine() <= before+2
	})
}

// TestConcurrentEnqueueAndClose is the race-detector workout: enqueues racing a
// Close must never send on a closed channel or trip the detector.
func TestConcurrentEnqueueAndClose(t *testing.T) {
	t.Parallel()

	for iter := range 20 {
		mock := newSQMockConn(fmt.Sprintf("c%d", iter))
		conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(8))...)

		var wg sync.WaitGroup

		for w := range 8 {
			wg.Add(1)

			go func(w int) {
				defer wg.Done()

				for i := range 50 {
					_ = conn.WriteJSON(sqMessage(internal.MessageTypeTyping, fmt.Sprintf("t%d-%d", w, i)))
					_ = conn.WriteJSON(sqMessage(internal.MessageTypeMessage, fmt.Sprintf("m%d-%d", w, i)))
					_ = conn.Write([]byte(`{"id":"raw"}`))
				}
			}(w)
		}

		for range 3 {
			wg.Add(1)

			go func() {
				defer wg.Done()

				_ = conn.Close()
			}()
		}

		wg.Wait()

		if err := conn.Close(); err != nil {
			t.Fatalf("iteration %d: repeat close: %v", iter, err)
		}

		if !conn.IsClosed() {
			t.Fatalf("iteration %d: connection not marked closed", iter)
		}
	}
}

// TestConcurrentEnqueueOnStalledConnection races enqueues against the overflow
// disconnect, which is raised from an enqueueing goroutine rather than by Close.
func TestConcurrentEnqueueOnStalledConnection(t *testing.T) {
	t.Parallel()

	for iter := range 10 {
		mock := newSQMockConn(fmt.Sprintf("c%d", iter)).stalled()
		conn := NewConnection(mock, sqTestOpts(WithSendQueueCapacity(4))...)

		var wg sync.WaitGroup

		for w := range 8 {
			wg.Add(1)

			go func(w int) {
				defer wg.Done()

				for i := range 30 {
					_ = conn.WriteJSON(sqMessage(internal.MessageTypeMessage, fmt.Sprintf("m%d-%d", w, i)))
					_ = conn.WriteJSON(sqMessage(internal.MessageTypePresence, fmt.Sprintf("p%d-%d", w, i)))
				}
			}(w)
		}

		wg.Wait()
		mock.openGate()

		if err := conn.Close(); err != nil {
			t.Fatalf("iteration %d: close: %v", iter, err)
		}

		sqWaitFor(t, 2*time.Second, "the connection to report closed", conn.IsClosed)
	}
}

func sqEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
