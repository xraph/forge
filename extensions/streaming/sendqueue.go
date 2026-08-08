package streaming

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// Send queue defaults.
const (
	// DefaultSendQueueCapacity is the number of frames a connection may have
	// queued for the socket before the overflow policy kicks in.
	DefaultSendQueueCapacity = 256

	// DefaultSendQueueFlushTimeout bounds how long Close waits for already
	// queued frames to reach the socket before the connection is torn down.
	// It exists so a final frame (a kick notice, a close reason) is not
	// discarded, without letting one slow consumer stall connection cleanup.
	DefaultSendQueueFlushTimeout = time.Second

	// DefaultSendQueueExitTimeout bounds how long Close waits for the writer
	// goroutine to exit after the underlying connection has been closed.
	DefaultSendQueueExitTimeout = 5 * time.Second
)

// Send queue errors.
var (
	// ErrSendQueueClosed is returned when a frame is enqueued on a connection
	// whose writer has already stopped.
	ErrSendQueueClosed = errors.New("streaming: connection send queue closed")

	// ErrSendQueueOverflow is returned when a non-droppable frame arrives on a
	// full queue. The connection is disconnected; the client is expected to
	// reconnect and resynchronise.
	ErrSendQueueOverflow = errors.New("streaming: connection send queue overflow")
)

// overflowPolicy decides what happens to a frame that arrives on a full queue.
type overflowPolicy int

const (
	// policyDisconnect tears the connection down rather than silently losing
	// the frame. Used for anything whose loss the client cannot detect.
	policyDisconnect overflowPolicy = iota

	// policyDropOldest evicts the oldest queued frame of the same type and
	// takes the new one. Used for state snapshots, where only the latest
	// value carries information.
	policyDropOldest
)

// policyForType maps a streaming message type to its overflow policy.
//
// Typing and presence frames are state snapshots: a stale one is worthless, so
// under pressure the oldest is discarded and the connection survives.
// Everything else - notably MessageTypeMessage - is content whose loss the
// client cannot detect, so an overflow disconnects instead. The TypeScript
// client treats a reconnect as a recoverable gap (StreamBinder.recover
// invalidates tags and refetches live queries), so a disconnect converges;
// a silently dropped chat message does not.
func policyForType(msgType string) overflowPolicy {
	switch msgType {
	case internal.MessageTypeTyping, internal.MessageTypePresence:
		return policyDropOldest
	default:
		return policyDisconnect
	}
}

// frameKind records which transport write a queued frame came from, so the
// writer goroutine replays it down the same path.
type frameKind uint8

const (
	frameRaw     frameKind = iota // Connection.Write
	frameJSON                     // Connection.WriteJSON
	frameBinary                   // Connection.WriteBinary, where the transport has one
	frameBarrier                  // no payload; releases a flush waiter when the writer reaches it
)

// sendFrame is one serialised outbound frame waiting for the socket.
//
// The payload is serialised by the caller at enqueue time rather than by the
// writer goroutine. That keeps encoding errors synchronous, and it means the
// writer never touches a *Message the broadcaster may still be mutating.
type sendFrame struct {
	msgType string
	payload []byte
	kind    frameKind

	// done belongs to frameBarrier frames and is closed once the writer has
	// reached this point in the queue.
	done chan struct{}
}

// shutdownMode describes how the writer goroutine should wind down.
type shutdownMode int

const (
	shutdownNone  shutdownMode = iota // running
	shutdownDrain                     // flush what is queued, then exit
	shutdownAbort                     // discard what is queued and exit now
)

// SendQueueStats is a snapshot of one connection's outbound queue. The manager
// can read it off a connection with a type assertion:
//
//	if s, ok := conn.(interface{ SendQueueStats() streaming.SendQueueStats }); ok {
//		depth := s.SendQueueStats().Depth
//	}
type SendQueueStats struct {
	Capacity            int
	Depth               int
	Enqueued            uint64
	Written             uint64
	Dropped             uint64
	OverflowDisconnects uint64
	WriteErrors         uint64
	Closed              bool
}

// sendQueueMetrics holds metric handles resolved once at construction, so the
// hot enqueue path costs an interface call rather than a registry lookup.
type sendQueueMetrics struct {
	enqueued  forge.Counter
	dropped   forge.Counter
	overflows forge.Counter
	writeErrs forge.Counter
	depth     forge.Histogram
}

func newSendQueueMetrics(m forge.Metrics) *sendQueueMetrics {
	if m == nil {
		return nil
	}

	return &sendQueueMetrics{
		enqueued:  m.Counter("streaming.sendqueue.enqueued"),
		dropped:   m.Counter("streaming.sendqueue.dropped"),
		overflows: m.Counter("streaming.sendqueue.overflow_disconnects"),
		writeErrs: m.Counter("streaming.sendqueue.write_errors"),
		// Depth is a histogram rather than a gauge: every connection owns its
		// own queue, so a single shared gauge would just be the last writer to
		// win. A distribution aggregates correctly across connections.
		depth: m.Histogram("streaming.sendqueue.depth"),
	}
}

// sendQueue is a bounded outbound frame queue drained by a single dedicated
// writer goroutine. Only that goroutine touches the socket, so writes never
// contend and one slow consumer cannot stall a broadcast to everyone else.
//
// It is a mutex-guarded deque rather than a buffered channel because the
// drop-oldest-of-same-type policy has to remove a frame from the middle of the
// queue, which a channel cannot do.
type sendQueue struct {
	capacity     int
	flushTimeout time.Duration
	exitTimeout  time.Duration

	// write performs the actual socket write. Called only from the writer
	// goroutine.
	write func(sendFrame) error

	// closeConn tears the socket down. It must be idempotent: the writer
	// goroutine, close, and the overflow path may all reach it.
	closeConn func()

	// onExit runs once, on the writer goroutine, after it has stopped. It is
	// where the connection is marked closed.
	onExit func(error)

	metrics *sendQueueMetrics

	mu     sync.Mutex
	frames []sendFrame
	mode   shutdownMode
	cause  error

	notify chan struct{} // capacity 1; a pending token means "look again"
	done   chan struct{} // closed once the writer goroutine has returned

	enqueued  atomic.Uint64
	written   atomic.Uint64
	dropped   atomic.Uint64
	overflows atomic.Uint64
	writeErrs atomic.Uint64
}

// newSendQueue creates a queue. The writer goroutine does not run until start
// is called, so the owner can finish wiring itself up first.
func newSendQueue(capacity int, flushTimeout, exitTimeout time.Duration,
	metrics forge.Metrics, write func(sendFrame) error, closeConn func(), onExit func(error),
) *sendQueue {
	if capacity <= 0 {
		capacity = DefaultSendQueueCapacity
	}

	if flushTimeout <= 0 {
		flushTimeout = DefaultSendQueueFlushTimeout
	}

	if exitTimeout <= 0 {
		exitTimeout = DefaultSendQueueExitTimeout
	}

	q := &sendQueue{
		capacity:     capacity,
		flushTimeout: flushTimeout,
		exitTimeout:  exitTimeout,
		write:        write,
		closeConn:    closeConn,
		onExit:       onExit,
		metrics:      newSendQueueMetrics(metrics),
		notify:       make(chan struct{}, 1),
		done:         make(chan struct{}),
	}

	return q
}

// start launches the dedicated writer goroutine.
func (q *sendQueue) start() {
	go q.run()
}

// enqueue adds a frame to the queue, applying the overflow policy for its
// message type when the queue is full.
//
// It returns ErrSendQueueClosed if the writer has stopped, and
// ErrSendQueueOverflow if this frame overflowed a full queue under
// policyDisconnect - in which case the connection is being torn down. A frame
// dropped under policyDropOldest reports success: dropping a stale snapshot is
// the intended outcome, not a delivery failure.
func (q *sendQueue) enqueue(f sendFrame) error {
	q.mu.Lock()

	if q.mode != shutdownNone {
		q.mu.Unlock()

		return ErrSendQueueClosed
	}

	evicted := false

	if len(q.frames) >= q.capacity {
		if policyForType(f.msgType) == policyDisconnect {
			q.beginShutdownLocked(shutdownAbort, ErrSendQueueOverflow)
			q.mu.Unlock()

			q.overflows.Add(1)
			q.signal()

			// Tear the socket down rather than only asking the writer to
			// stop. The writer is, by definition of a full queue, parked in a
			// write to a consumer that is not reading; it would not observe
			// the shutdown until that write returned. Closing is done on its
			// own goroutine so a slow Close cannot stall the broadcaster.
			// This branch runs at most once per queue - every later enqueue
			// short-circuits on the shutdown check above.
			if q.closeConn != nil {
				go q.closeConn()
			}

			if q.metrics != nil {
				q.metrics.overflows.Inc()
			}

			return ErrSendQueueOverflow
		}

		// Droppable type: evict the oldest queued frame of the same type. If
		// there is none, the queue is full of frames we may not drop, so the
		// incoming snapshot is discarded instead - never the durable frames,
		// and never the connection.
		idx := q.indexOfTypeLocked(f.msgType)
		if idx < 0 {
			q.mu.Unlock()
			q.recordDrop()

			return nil
		}

		q.frames = append(q.frames[:idx], q.frames[idx+1:]...)
		evicted = true
	}

	q.frames = append(q.frames, f)
	depth := len(q.frames)
	q.mu.Unlock()

	if evicted {
		q.recordDrop()
	}

	q.enqueued.Add(1)
	q.signal()

	if q.metrics != nil {
		q.metrics.enqueued.Inc()
		q.metrics.depth.Observe(float64(depth))
	}

	return nil
}

// indexOfTypeLocked returns the index of the oldest queued frame with the given
// message type, or -1. Caller holds q.mu.
func (q *sendQueue) indexOfTypeLocked(msgType string) int {
	for i := range q.frames {
		if q.frames[i].msgType == msgType {
			return i
		}
	}

	return -1
}

func (q *sendQueue) recordDrop() {
	q.dropped.Add(1)

	if q.metrics != nil {
		q.metrics.dropped.Inc()
	}
}

// beginShutdownLocked records a shutdown request. The first request wins, so
// an abort raised by the writer is not downgraded to a drain by a later Close.
// Caller holds q.mu.
func (q *sendQueue) beginShutdownLocked(mode shutdownMode, cause error) {
	if q.mode != shutdownNone {
		return
	}

	q.mode = mode
	q.cause = cause
}

// signal wakes the writer goroutine. The notify channel has capacity 1, so a
// token already in flight is enough - the writer re-reads the queue after every
// wakeup, and cannot miss an append.
func (q *sendQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// close stops the queue and waits, within bounds, for the writer to finish.
//
// Frames already queued are flushed first (up to flushTimeout) so a final
// message - a kick notice, a close reason - is not discarded. Then the socket
// is torn down, which is what unblocks a writer stuck mid-write, and the
// goroutine is joined (up to exitTimeout) so Close does not leak it.
//
// Both waits are bounded on purpose: one unresponsive consumer must not be
// able to stall connection cleanup. It is idempotent and safe to call
// concurrently with enqueue.
func (q *sendQueue) close() {
	q.mu.Lock()
	q.beginShutdownLocked(shutdownDrain, nil)
	q.mu.Unlock()

	q.signal()

	// Give the writer a bounded window to flush what is already queued.
	select {
	case <-q.done:
		return
	case <-time.After(q.flushTimeout):
	}

	// Still writing. Tear the socket down so the in-flight write fails, then
	// wait for the goroutine to unwind.
	if q.closeConn != nil {
		q.closeConn()
	}

	select {
	case <-q.done:
	case <-time.After(q.exitTimeout):
	}
}

// run is the writer goroutine: the only place that touches the socket.
func (q *sendQueue) run() {
	cause := q.loop()

	q.mu.Lock()
	q.beginShutdownLocked(shutdownAbort, cause)

	// A recorded cause wins over the write error that followed from it: when
	// an overflow tears the socket down, the interesting reason is the
	// overflow, not the "use of closed connection" it produced.
	if q.cause != nil {
		cause = q.cause
	}

	// Frames still queued at shutdown are lost. Barriers are not messages, so
	// they do not count as drops; their waiters are released by done closing.
	discarded := 0

	for i := range q.frames {
		if q.frames[i].kind != frameBarrier {
			discarded++
		}
	}

	q.frames = nil
	q.mu.Unlock()

	for range discarded {
		q.recordDrop()
	}

	if q.onExit != nil {
		q.onExit(cause)
	}

	close(q.done)
}

// loop drains frames until the queue is shut down or a write fails. It returns
// the write error, if any; nil means the queue was shut down normally.
func (q *sendQueue) loop() error {
	for {
		q.mu.Lock()
		mode := q.mode

		if mode == shutdownAbort {
			q.mu.Unlock()

			return nil
		}

		frames := q.frames
		q.frames = nil
		q.mu.Unlock()

		for i := range frames {
			if frames[i].kind == frameBarrier {
				close(frames[i].done)

				continue
			}

			if err := q.write(frames[i]); err != nil {
				q.writeErrs.Add(1)

				if q.metrics != nil {
					q.metrics.writeErrs.Inc()
				}

				return err
			}

			q.written.Add(1)
		}

		if mode == shutdownDrain && len(frames) == 0 {
			return nil
		}

		if len(frames) == 0 {
			<-q.notify
		}
	}
}

// flush blocks until every frame queued before the call has been handed to the
// socket, the context is done, or the queue shuts down.
//
// It exists because delivery is asynchronous: a caller that needs to know a
// frame actually reached the wire - a test, or a shutdown path sending a final
// notice - can no longer learn that from the write's return value.
func (q *sendQueue) flush(ctx context.Context) error {
	done := make(chan struct{})

	q.mu.Lock()

	if q.mode != shutdownNone {
		q.mu.Unlock()

		return ErrSendQueueClosed
	}

	// A barrier deliberately skips the capacity check. It carries no payload,
	// and it must never be the frame that trips an overflow disconnect.
	q.frames = append(q.frames, sendFrame{kind: frameBarrier, done: done})
	q.mu.Unlock()

	q.signal()

	select {
	case <-done:
		return nil
	case <-q.done:
		return ErrSendQueueClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// depth returns the number of frames currently waiting for the socket.
func (q *sendQueue) depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.frames)
}

// closed reports whether the queue has begun shutting down.
func (q *sendQueue) closed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.mode != shutdownNone
}

// stats returns a snapshot of the queue's counters.
func (q *sendQueue) stats() SendQueueStats {
	q.mu.Lock()
	depth := len(q.frames)
	closed := q.mode != shutdownNone
	q.mu.Unlock()

	return SendQueueStats{
		Capacity:            q.capacity,
		Depth:               depth,
		Enqueued:            q.enqueued.Load(),
		Written:             q.written.Load(),
		Dropped:             q.dropped.Load(),
		OverflowDisconnects: q.overflows.Load(),
		WriteErrors:         q.writeErrs.Load(),
		Closed:              closed,
	}
}
