package middleware

import (
	"bufio"
	"context"
	"maps"
	"net"
	"net/http"
	"sync"
	"time"

	forge "github.com/xraph/forge"
)

// DefaultTimeoutBufferLimit bounds how much of a response the Timeout
// middleware will hold in memory before it gives up on buffering and starts
// passing writes straight through. Buffering exists so a timeout can replace a
// partially written response; past this size that is no longer worth the memory,
// and an unbounded buffer would let any large response drive allocation.
const DefaultTimeoutBufferLimit = 1 << 20 // 1 MiB

// timeoutBufPool recycles the body buffers safeResponseWriter fills. Most
// responses are small, so without the pool every request through Timeout
// allocates and discards one. A buffer only goes back to the pool once nothing
// else can reach it: the body slice is never handed to the handler, and every
// access to it happens under the writer's mutex.
var timeoutBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 4096)

		return &b
	},
}

// maxPooledTimeoutBuf keeps the occasional large response from pinning a big
// buffer in the pool for the life of the process.
const maxPooledTimeoutBuf = 64 << 10

// safeResponseWriter wraps http.ResponseWriter and buffers the response so a
// timeout can substitute its own response instead of racing a half-written one.
//
// Buffering is abandoned, and writes go straight to the client, as soon as
// any of the following happens, because each means the response is already
// committed or must not be held:
//   - the buffer exceeds bufferLimit
//   - the handler calls Flush (streaming: the client is waiting on bytes now)
//   - the handler calls Hijack (WebSocket upgrade: we no longer own the conn)
//
// Once the deadline passes the writer is marked timed out. From then on every
// method returns http.ErrHandlerTimeout (or does nothing) and never touches the
// real ResponseWriter, because ServeHTTP has returned and net/http may already
// be reusing it. The handler goroutine can outlive the request, so this is the
// only thing standing between a slow handler and a data race on the response.
type safeResponseWriter struct {
	http.ResponseWriter

	mu          sync.Mutex
	header      http.Header
	code        int
	body        []byte
	bufp        *[]byte
	flushed     bool
	passthrough bool
	timedOut    bool
	bufferLimit int

	// panicVal carries a panic from the handler goroutine back to the
	// goroutine that called ServeHTTP. It is written before done is closed and
	// read after, so the channel close orders the two.
	panicVal any
}

func newSafeResponseWriter(w http.ResponseWriter) *safeResponseWriter {
	return &safeResponseWriter{
		ResponseWriter: w,
		code:           http.StatusOK,
		bufferLimit:    DefaultTimeoutBufferLimit,
	}
}

func (w *safeResponseWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()

	// After a timeout the real header map belongs to net/http again, so hand
	// out the private one; whatever the handler sets there goes nowhere.
	if w.passthrough && !w.timedOut {
		return w.ResponseWriter.Header()
	}

	// Allocated on first use: handlers that never touch headers skip it.
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *safeResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut {
		return
	}

	if w.passthrough {
		w.ResponseWriter.WriteHeader(code)

		return
	}

	if !w.flushed {
		w.code = code
	}
}

func (w *safeResponseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut {
		return 0, http.ErrHandlerTimeout
	}

	if w.flushed || w.passthrough {
		return w.ResponseWriter.Write(data)
	}

	// Buffering this write would exceed the cap: commit what we have and let
	// this and every later write go directly to the client.
	if len(w.body)+len(data) > w.bufferLimit {
		w.commitLocked()

		return w.ResponseWriter.Write(data)
	}

	if w.bufp == nil {
		w.bufp = timeoutBufPool.Get().(*[]byte)
		w.body = (*w.bufp)[:0]
	}

	w.body = append(w.body, data...)

	return len(data), nil
}

// releaseBufLocked drops the buffered body and returns its backing array to
// the pool. Caller must hold w.mu.
func (w *safeResponseWriter) releaseBufLocked() {
	if w.bufp == nil {
		w.body = nil

		return
	}

	if cap(w.body) <= maxPooledTimeoutBuf {
		*w.bufp = w.body[:0]
		timeoutBufPool.Put(w.bufp)
	}

	w.bufp = nil
	w.body = nil
}

// commitLocked writes the buffered head of the response to the client and
// switches to passthrough. Caller must hold w.mu.
func (w *safeResponseWriter) commitLocked() {
	if w.passthrough || w.flushed || w.timedOut {
		return
	}

	maps.Copy(w.ResponseWriter.Header(), w.header)
	w.ResponseWriter.WriteHeader(w.code)

	if len(w.body) > 0 {
		_, _ = w.ResponseWriter.Write(w.body)
	}

	w.releaseBufLocked()
	w.passthrough = true
}

// Flush implements http.Flusher. A handler that flushes is streaming, so the
// response must stop being buffered, otherwise SSE and chunked responses stall
// until the handler returns. The real Flush runs under the lock so it cannot
// overlap the timeout path or land after ServeHTTP has returned.
func (w *safeResponseWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut {
		return
	}

	w.commitLocked()

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker so WebSocket upgrades survive this middleware.
// Buffering is dropped first: once the connection is hijacked this wrapper can
// no longer write to it.
func (w *safeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut {
		return nil, nil, http.ErrHandlerTimeout
	}

	w.passthrough = true
	w.releaseBufLocked()

	return hijacker.Hijack()
}

// Push implements http.Pusher.
func (w *safeResponseWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut {
		return http.ErrHandlerTimeout
	}

	return p.Push(target, opts)
}

// flush sends the buffered response once the handler has returned in time.
func (w *safeResponseWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.passthrough || w.flushed || w.timedOut {
		return
	}

	maps.Copy(w.ResponseWriter.Header(), w.header)

	w.ResponseWriter.WriteHeader(w.code)

	if len(w.body) > 0 {
		_, _ = w.ResponseWriter.Write(w.body)
	}

	w.releaseBufLocked()
	w.flushed = true
}

// discard drops a buffered response the handler will never finish, so that
// recovery middleware further out can write its own error response.
func (w *safeResponseWriter) discard() {
	w.mu.Lock()
	w.releaseBufLocked()
	w.mu.Unlock()
}

// timeout marks the writer as timed out and, if nothing has reached the client
// yet, writes the 504. Checking for a committed response and writing the 504
// happen under one lock so the handler cannot slip a commit in between, and
// once the flag is set the handler can no longer reach the real writer.
func (w *safeResponseWriter) timeout() {
	w.mu.Lock()
	defer w.mu.Unlock()

	committed := w.passthrough || w.flushed
	w.timedOut = true
	w.releaseBufLocked()

	// If the handler already committed bytes to the client (it streamed,
	// hijacked, or overflowed the buffer) there is no status line left to
	// write; doing so would emit a second header and corrupt the response.
	if committed {
		return
	}

	w.ResponseWriter.WriteHeader(http.StatusGatewayTimeout)
	_, _ = w.ResponseWriter.Write([]byte("Gateway Timeout"))
}

// Timeout middleware enforces a timeout on request handling.
// Returns http.StatusGatewayTimeout if request exceeds duration.
//
// The handler runs on its own goroutine so the deadline can be enforced even
// when the handler ignores its context. Two consequences follow:
//   - A panic in the handler is caught on that goroutine and re-raised on the
//     goroutine that called ServeHTTP, with the original value, so recovery
//     middleware wrapped around Timeout still sees it. Without this, recovery
//     middleware cannot help and the panic takes down the whole process. A
//     panic that happens after the deadline has nobody left to receive it and
//     is dropped, as net/http.TimeoutHandler does.
//   - A handler still running after the deadline gets http.ErrHandlerTimeout
//     from Write and can no longer touch the response. The context deadline
//     is its signal to stop.
//
// Note: This middleware uses http.Handler pattern due to goroutine requirements.
func Timeout(duration time.Duration, logger forge.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			safeW := newSafeResponseWriter(w)
			req := r.WithContext(ctx)
			done := make(chan struct{})

			go func() {
				defer func() {
					if p := recover(); p != nil {
						safeW.panicVal = p
					}

					close(done)
				}()

				next.ServeHTTP(safeW, req)
			}()

			select {
			case <-done:
				if p := safeW.panicVal; p != nil {
					safeW.discard()
					panic(p)
				}

				safeW.flush()
			case <-ctx.Done():
				safeW.timeout()

				if logger != nil {
					logger.Warn("request timeout")
				}
			}
		})
	}
}
