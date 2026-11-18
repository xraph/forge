package shared

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// brokenConn fails every body write the way a connection does once the
// client is gone or a write deadline fired, and records WriteHeader calls.
type brokenConn struct {
	*httptest.ResponseRecorder

	headers int
}

func (b *brokenConn) WriteHeader(code int) {
	b.headers++
	b.ResponseRecorder.WriteHeader(code)
}

func (b *brokenConn) Write([]byte) (int, error) {
	return 0, errors.New("write: broken pipe")
}

// After a failed write an error handler typically sends a 500. That reached
// net/http as a superfluous WriteHeader and was logged against this wrapper.
func TestBeforeWrite_DropsWriteHeaderAfterFailedWrite(t *testing.T) {
	conn := &brokenConn{ResponseRecorder: httptest.NewRecorder()}
	w := WrapBeforeWrite(conn)

	if _, err := w.Write([]byte("body")); err == nil {
		t.Fatal("expected the write to fail")
	}

	w.WriteHeader(http.StatusInternalServerError)

	if conn.headers != 0 {
		t.Errorf("WriteHeader reached the broken connection %d times, want 0", conn.headers)
	}
}

// A second WriteHeader with no failed write in between still goes through, so
// net/http keeps reporting the handler bug.
func TestBeforeWrite_KeepsDuplicateWriteHeaderVisible(t *testing.T) {
	conn := &brokenConn{ResponseRecorder: httptest.NewRecorder()}
	w := WrapBeforeWrite(conn)

	w.WriteHeader(http.StatusOK)
	w.WriteHeader(http.StatusInternalServerError)

	if conn.headers != 2 {
		t.Errorf("WriteHeader reached the writer %d times, want 2", conn.headers)
	}
}
