package shared

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
)

// BeforeWriter is a response writer that can run callbacks at the moment the
// response headers are committed, and report whether that has happened.
//
// It exists because forge streams responses: middleware is handed the raw
// http.ResponseWriter and a handler's first write puts the status line on the
// connection. Anything a middleware sets after next() returns has already
// missed the wire, so any middleware that needs to add a header derived from
// the handler's outcome previously had to do its work before the handler and
// guess, or lose the header silently.
//
// Registering a callback instead lets that middleware keep its natural
// after-the-handler shape: the callback fires just late enough to see the
// handler's result and just early enough to still be delivered.
type BeforeWriter interface {
	http.ResponseWriter

	// Before registers fn to run immediately before the response headers are
	// committed. Callbacks run once, in registration order, and may mutate
	// Header(). Reports false when the headers have already been committed, in
	// which case fn is never run — the caller is too late and should treat the
	// header as undeliverable rather than assume success.
	Before(fn func()) bool

	// Written reports whether the response headers have been committed.
	Written() bool
}

// beforeWriteWriter is the BeforeWriter implementation forge wraps every
// request in.
type beforeWriteWriter struct {
	http.ResponseWriter

	mu        sync.Mutex
	hooks     []func()
	committed bool
}

// WrapBeforeWrite returns w as a BeforeWriter, wrapping it if needed.
//
// Idempotent: a writer that is already a *beforeWriteWriter is returned
// unchanged, so nested routers and mounted sub-handlers share one hook list
// rather than stacking a layer each.
func WrapBeforeWrite(w http.ResponseWriter) http.ResponseWriter {
	if w == nil {
		return nil
	}
	if _, ok := w.(*beforeWriteWriter); ok {
		return w
	}
	return &beforeWriteWriter{ResponseWriter: w}
}

// BeforeWrite registers fn against w's hook list when w supports it. Reports
// false when w is not a BeforeWriter, or when the headers are already out.
//
// Middleware should use this rather than asserting, so it degrades on a writer
// that was never wrapped (a hand-rolled test double, say) instead of panicking.
func BeforeWrite(w http.ResponseWriter, fn func()) bool {
	bw, ok := w.(BeforeWriter)
	if !ok {
		return false
	}
	return bw.Before(fn)
}

func (w *beforeWriteWriter) Before(fn func()) bool {
	if fn == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.committed {
		return false
	}
	w.hooks = append(w.hooks, fn)
	return true
}

func (w *beforeWriteWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.committed
}

// commit runs the registered callbacks exactly once, then marks the headers as
// gone. Every path that can put bytes or a status line on the connection calls
// it first.
func (w *beforeWriteWriter) commit() {
	w.mu.Lock()
	if w.committed {
		w.mu.Unlock()
		return
	}
	// Mark committed before running anything: a callback that writes to the
	// response (or that calls Before again) then takes the already-committed
	// path instead of recursing into commit forever.
	w.committed = true
	hooks := w.hooks
	w.hooks = nil
	w.mu.Unlock()

	// Run outside the lock so a callback is free to touch Header() or call
	// Written() without deadlocking on a mutex it cannot see.
	for _, fn := range hooks {
		fn()
	}
}

// discard marks the headers as gone without running the callbacks. Used when
// the response will not be written by us at all.
func (w *beforeWriteWriter) discard() {
	w.mu.Lock()
	w.committed = true
	w.hooks = nil
	w.mu.Unlock()
}

func (w *beforeWriteWriter) WriteHeader(code int) {
	w.commit()
	// Deliberately not guarded against a second call. net/http already logs a
	// superfluous WriteHeader, and swallowing it here would hide a handler bug
	// that is visible without this wrapper in the chain.
	w.ResponseWriter.WriteHeader(code)
}

func (w *beforeWriteWriter) Write(b []byte) (int, error) {
	w.commit()
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher.
//
// Implemented unconditionally, and that is load-bearing: SSE asserts
// w.(http.Flusher) and gives up when it fails, so a wrapper that only
// conditionally exposed Flush would take streaming responses down. Flushing
// commits the headers, because net/http sends them on the first flush.
func (w *beforeWriteWriter) Flush() {
	w.commit()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker so WebSocket upgrades survive the wrapper.
//
// The callbacks are dropped rather than run: a hijacked connection is no longer
// ours to write an HTTP response on, and the upgrade handshake is built by
// whoever took the connection. Running header callbacks into a response that
// will never be sent would look like they succeeded.
func (w *beforeWriteWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := hijacker.Hijack()
	if err == nil {
		w.discard()
	}
	return conn, rw, err
}

// ReadFrom implements io.ReaderFrom so the sendfile fast path is not lost to
// the wrapper. Commits first, since the copy is a write.
func (w *beforeWriteWriter) ReadFrom(src io.Reader) (int64, error) {
	rf, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		w.commit()
		return io.Copy(w.ResponseWriter, src)
	}
	w.commit()
	return rf.ReadFrom(src)
}

// Push implements http.Pusher.
func (w *beforeWriteWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap exposes the wrapped writer to http.ResponseController, which walks
// Unwrap to find Flush/SetWriteDeadline/etc. Without it, a ResponseController
// built over this wrapper loses the deadline controls entirely.
func (w *beforeWriteWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
