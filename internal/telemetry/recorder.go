package telemetry

import (
	"bufio"
	"net"
	"net/http"
)

// StatusRecorder captures the response status and body size without hiding
// the optional interfaces SSE and WebSocket handlers need. The tracing
// middleware and the HTTP server metrics middleware both wrap the writer in
// one.
type StatusRecorder struct {
	http.ResponseWriter

	code    int
	written bool
	bytes   int64
}

// NewStatusRecorder wraps w.
func NewStatusRecorder(w http.ResponseWriter) *StatusRecorder {
	return &StatusRecorder{ResponseWriter: w}
}

// WriteHeader records the first final status and forwards every call. A 1xx
// informational response such as 103 Early Hints is passed on but not
// recorded, because the handler sends the real status after it. 101 is the
// exception: it ends the HTTP exchange, so it is final.
func (r *StatusRecorder) WriteHeader(code int) {
	if !r.written && (code >= http.StatusOK || code == http.StatusSwitchingProtocols) {
		r.code = code
		r.written = true
	}

	r.ResponseWriter.WriteHeader(code)
}

func (r *StatusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.code = http.StatusOK
		r.written = true
	}

	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)

	return n, err
}

// Status returns the status sent, or 200 if the handler wrote nothing.
func (r *StatusRecorder) Status() int {
	if r.code == 0 {
		return http.StatusOK
	}

	return r.code
}

// BytesWritten returns the response body bytes written so far.
func (r *StatusRecorder) BytesWritten() int64 { return r.bytes }

// Flush flushes through http.ResponseController, so a writer below that only
// exposes Unwrap still gets flushed. A successful flush sends the headers, so
// it commits 200 when no status was written yet.
func (r *StatusRecorder) Flush() {
	if err := http.NewResponseController(r.ResponseWriter).Flush(); err != nil {
		return
	}

	if !r.written {
		r.code = http.StatusOK
		r.written = true
	}
}

// Hijack hijacks through http.ResponseController. It records 101 only when
// the hijack succeeded. When no writer below supports it, the error wraps
// http.ErrNotSupported.
func (r *StatusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}

	r.code = http.StatusSwitchingProtocols
	r.written = true

	return conn, rw, nil
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *StatusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
