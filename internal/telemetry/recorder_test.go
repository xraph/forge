package telemetry

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// codeLog records every WriteHeader call it receives.
type codeLog struct {
	http.ResponseWriter

	codes []int
}

func (c *codeLog) WriteHeader(code int) { c.codes = append(c.codes, code) }

// unwrapOnly hides every optional interface of the writer it wraps and only
// offers Unwrap, the way many third-party middleware wrappers do.
type unwrapOnly struct{ w http.ResponseWriter }

func (u unwrapOnly) Header() http.Header         { return u.w.Header() }
func (u unwrapOnly) Write(b []byte) (int, error) { return u.w.Write(b) }
func (u unwrapOnly) WriteHeader(code int)        { u.w.WriteHeader(code) }
func (u unwrapOnly) Unwrap() http.ResponseWriter { return u.w }

// fakeHijacker reports a successful hijack with a pipe.
type fakeHijacker struct {
	http.ResponseWriter

	hijacked bool
}

func (f *fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijacked = true
	c, _ := net.Pipe()

	return c, bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c)), nil
}

func TestStatusRecorderSkipsInformationalStatus(t *testing.T) {
	inner := &codeLog{ResponseWriter: httptest.NewRecorder()}
	rec := NewStatusRecorder(inner)

	rec.WriteHeader(http.StatusEarlyHints)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.Status() != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 after 103 then 500", rec.Status())
	}

	if want := []int{http.StatusEarlyHints, http.StatusInternalServerError}; !slices.Equal(inner.codes, want) {
		t.Fatalf("inner writer got %v, want %v", inner.codes, want)
	}
}

func TestStatusRecorderInformationalThenBodyIs200(t *testing.T) {
	rec := NewStatusRecorder(&codeLog{ResponseWriter: httptest.NewRecorder()})

	rec.WriteHeader(http.StatusEarlyHints)
	_, _ = rec.Write([]byte("ok"))

	if rec.Status() != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Status())
	}
}

func TestStatusRecorderSwitchingProtocolsIsFinal(t *testing.T) {
	rec := NewStatusRecorder(&codeLog{ResponseWriter: httptest.NewRecorder()})

	rec.WriteHeader(http.StatusSwitchingProtocols)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.Status() != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", rec.Status())
	}
}

func TestStatusRecorderCountsBytes(t *testing.T) {
	rec := NewStatusRecorder(httptest.NewRecorder())

	_, _ = rec.Write([]byte("hello"))
	_, _ = rec.Write([]byte(" world"))

	if rec.BytesWritten() != 11 {
		t.Fatalf("bytes = %d, want 11", rec.BytesWritten())
	}
}

func TestStatusRecorderFlushesThroughUnwrap(t *testing.T) {
	under := httptest.NewRecorder()
	rec := NewStatusRecorder(unwrapOnly{w: under})

	rec.Flush()

	if !under.Flushed {
		t.Fatal("flush did not reach the writer behind an Unwrap-only wrapper")
	}

	if rec.Status() != http.StatusOK {
		t.Fatalf("status = %d, want 200 after a flush", rec.Status())
	}
}

func TestStatusRecorderHijacksThroughUnwrap(t *testing.T) {
	hj := &fakeHijacker{ResponseWriter: httptest.NewRecorder()}
	rec := NewStatusRecorder(unwrapOnly{w: hj})

	conn, _, err := rec.Hijack()
	if err != nil {
		t.Fatal(err)
	}

	_ = conn.Close()

	if !hj.hijacked || rec.Status() != http.StatusSwitchingProtocols {
		t.Fatalf("hijacked = %v, status = %d, want true and 101", hj.hijacked, rec.Status())
	}
}

func TestStatusRecorderFailedHijackLeavesStatusUnset(t *testing.T) {
	rec := NewStatusRecorder(httptest.NewRecorder())

	if _, _, err := rec.Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("err = %v, want one wrapping http.ErrNotSupported", err)
	}

	// The handler falls back to a normal response; that status must win.
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.Status() != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: the failed hijack latched a status", rec.Status())
	}
}
