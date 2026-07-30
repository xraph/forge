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

// safeResponseWriter wraps http.ResponseWriter and buffers the response so a
// timeout can substitute its own response instead of racing a half-written one.
//
// Buffering is abandoned — and writes go straight to the client — as soon as
// any of the following happens, because each means the response is already
// committed or must not be held:
//   - the buffer exceeds bufferLimit
//   - the handler calls Flush (streaming: the client is waiting on bytes now)
//   - the handler calls Hijack (WebSocket upgrade: we no longer own the conn)
type safeResponseWriter struct {
	http.ResponseWriter

	mu          sync.Mutex
	header      http.Header
	code        int
	body        []byte
	flushed     bool
	passthrough bool
	bufferLimit int
}

func newSafeResponseWriter(w http.ResponseWriter) *safeResponseWriter {
	return &safeResponseWriter{
		ResponseWriter: w,
		header:         make(http.Header),
		code:           http.StatusOK,
		bufferLimit:    DefaultTimeoutBufferLimit,
	}
}

func (w *safeResponseWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.passthrough {
		return w.ResponseWriter.Header()
	}

	return w.header
}

func (w *safeResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()

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

	if w.flushed || w.passthrough {
		return w.ResponseWriter.Write(data)
	}

	// Buffering this write would exceed the cap: commit what we have and let
	// this and every later write go directly to the client.
	if len(w.body)+len(data) > w.bufferLimit {
		w.commitLocked()

		return w.ResponseWriter.Write(data)
	}

	w.body = append(w.body, data...)

	return len(data), nil
}

// commitLocked writes the buffered head of the response to the client and
// switches to passthrough. Caller must hold w.mu.
func (w *safeResponseWriter) commitLocked() {
	if w.passthrough || w.flushed {
		return
	}

	maps.Copy(w.ResponseWriter.Header(), w.header)
	w.ResponseWriter.WriteHeader(w.code)

	if len(w.body) > 0 {
		_, _ = w.ResponseWriter.Write(w.body)
		w.body = nil
	}

	w.passthrough = true
}

// Flush implements http.Flusher. A handler that flushes is streaming, so the
// response must stop being buffered — otherwise SSE and chunked responses stall
// until the handler returns.
func (w *safeResponseWriter) Flush() {
	w.mu.Lock()
	w.commitLocked()
	w.mu.Unlock()

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker so WebSocket upgrades survive this middleware.
// Buffering is committed first: once the connection is hijacked this wrapper can
// no longer write to it.
func (w *safeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	w.mu.Lock()
	w.passthrough = true
	w.body = nil
	w.mu.Unlock()

	return hijacker.Hijack()
}

// Push implements http.Pusher.
func (w *safeResponseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}

	return http.ErrNotSupported
}

// committed reports whether anything has already reached the client, in which
// case the timeout path must not try to write its own status line.
func (w *safeResponseWriter) committed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.passthrough || w.flushed
}

func (w *safeResponseWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.passthrough || w.flushed {
		return
	}

	// Copy headers to underlying writer
	maps.Copy(w.ResponseWriter.Header(), w.header)

	w.ResponseWriter.WriteHeader(w.code)

	if len(w.body) > 0 {
		_, _ = w.ResponseWriter.Write(w.body)
	}

	w.flushed = true
}

// Timeout middleware enforces a timeout on request handling
// Returns http.StatusGatewayTimeout if request exceeds duration
// Note: This middleware uses http.Handler pattern due to goroutine requirements.
func Timeout(duration time.Duration, logger forge.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create context with timeout
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			// Wrap response writer to prevent race conditions
			safeW := newSafeResponseWriter(w)

			// Channel to signal completion
			done := make(chan struct{})

			// Run handler in goroutine
			go func() {
				defer close(done)

				next.ServeHTTP(safeW, r.WithContext(ctx))
			}()

			// Wait for completion or timeout
			select {
			case <-done:
				// Request completed successfully, flush any buffered data
				safeW.flush()

				return
			case <-ctx.Done():
				if logger != nil {
					logger.Warn("request timeout")
				}

				// If the handler already committed bytes to the client (it
				// streamed, hijacked, or overflowed the buffer) there is no
				// status line left to write — doing so would emit a second
				// header and corrupt the response. The context deadline is the
				// handler's signal to stop.
				if safeW.committed() {
					return
				}

				// Write timeout response directly to avoid race with buffered response
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("Gateway Timeout"))
			}
		})
	}
}
