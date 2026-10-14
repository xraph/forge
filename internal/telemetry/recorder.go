package telemetry

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

var errNoHijack = errors.New("telemetry: underlying ResponseWriter does not support hijacking")

// statusRecorder captures the response status without hiding the optional
// interfaces SSE and WebSocket handlers need.
type statusRecorder struct {
	http.ResponseWriter

	code    int
	written bool
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w}
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.code = code
		r.written = true
	}

	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.code = http.StatusOK
		r.written = true
	}

	return r.ResponseWriter.Write(b)
}

// Status returns the status sent, or 200 if the handler wrote nothing.
func (r *statusRecorder) Status() int {
	if r.code == 0 {
		return http.StatusOK
	}

	return r.code
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		if !r.written {
			r.code = http.StatusOK
			r.written = true
		}

		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errNoHijack
	}

	r.code = http.StatusSwitchingProtocols
	r.written = true

	return hj.Hijack()
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
