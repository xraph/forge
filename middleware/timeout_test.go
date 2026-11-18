package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forge "github.com/xraph/forge"
)

func TestTimeout_NoTimeout(t *testing.T) {
	logger := &mockLogger{}
	handler := Timeout(100*time.Millisecond, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestTimeout_WithTimeout(t *testing.T) {
	logger := &mockLogger{}
	handler := Timeout(50*time.Millisecond, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 504, rec.Code)
	assert.Contains(t, rec.Body.String(), "Gateway Timeout")
	assert.Len(t, logger.messages, 1)
	assert.Contains(t, logger.messages[0], "request timeout")
}

func TestTimeout_NilLogger(t *testing.T) {
	handler := Timeout(50*time.Millisecond, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 504, rec.Code)
}

// A panic on the handler goroutine used to escape every recover, because
// recovery middleware runs on the goroutine that called ServeHTTP. It must now
// be re-raised there with its original value.
func TestTimeout_PanicIsRaisedOnCallerGoroutine(t *testing.T) {
	sentinel := errors.New("handler bug")

	h := Timeout(time.Second, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))

		panic(sentinel)
	}))

	rec := httptest.NewRecorder()

	got := func() (p any) {
		defer func() { p = recover() }()

		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

		return nil
	}()

	assert.Equal(t, sentinel, got)
	// The buffered partial body is dropped so recovery further out can still
	// write a clean error response.
	assert.Empty(t, rec.Body.String())
	assert.False(t, rec.Flushed)
}

// timeoutCrashChildEnv marks the subprocess TestTimeout_PanicDoesNotKillProcess
// starts. The child runs the panicking request for real; if the panic escapes,
// the child dies and the parent sees a non-zero exit.
const timeoutCrashChildEnv = "FORGE_TIMEOUT_PANIC_CHILD"

func TestTimeout_PanicDoesNotKillProcess(t *testing.T) {
	if os.Getenv(timeoutCrashChildEnv) == "1" {
		router := forge.NewRouter(forge.WithRecovery())
		router.Use(Recovery(nil))

		panicky := Timeout(time.Second, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("handler bug")
		}))

		if err := router.GET("/panic", panicky); err != nil {
			fmt.Fprintln(os.Stderr, "register:", err)
			os.Exit(3)
		}

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/panic", nil))

		fmt.Fprintf(os.Stdout, "survived status=%d\n", rec.Code)

		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTimeout_PanicDoesNotKillProcess$", "-test.v")

	cmd.Env = append(os.Environ(), timeoutCrashChildEnv+"=1")

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child process died:\n%s", out)
	assert.Contains(t, string(out), "survived status=500")
}

// After the deadline the handler goroutine keeps running. Anything it writes
// must not reach the real ResponseWriter, which net/http owns again once
// ServeHTTP has returned. Run with -race: the old code raced here.
func TestTimeout_NoWritesAfterDeadline(t *testing.T) {
	served := make(chan struct{})
	handlerDone := make(chan struct{})

	var (
		writeErr error
		written  int
	)

	h := Timeout(20*time.Millisecond, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)

		<-r.Context().Done()
		<-served

		w.Header().Set("X-Late", "1")
		w.WriteHeader(http.StatusTeapot)
		// Larger than the buffer limit, which used to force a commit to the
		// real writer.
		written, writeErr = w.Write(make([]byte, 2<<20))

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	close(served)
	<-handlerDone

	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
	assert.Equal(t, "Gateway Timeout", rec.Body.String())
	assert.Empty(t, rec.Header().Get("X-Late"))
	assert.False(t, rec.Flushed)
	assert.ErrorIs(t, writeErr, http.ErrHandlerTimeout)
	assert.Zero(t, written)
}

// A streaming handler that has already committed its response keeps what it
// sent, gets no 504 appended, and is cut off at the deadline.
func TestTimeout_CommittedResponseIsCutOffAtDeadline(t *testing.T) {
	served := make(chan struct{})
	handlerDone := make(chan struct{})

	var lateErr error

	h := Timeout(20*time.Millisecond, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)

		_, _ = w.Write([]byte("chunk"))
		w.(http.Flusher).Flush()

		<-r.Context().Done()
		<-served

		_, lateErr = w.Write([]byte("late"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	close(served)
	<-handlerDone

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "chunk", rec.Body.String())
	assert.ErrorIs(t, lateErr, http.ErrHandlerTimeout)
}

// Body buffers are pooled; a recycled buffer must never leak bytes from an
// earlier response.
func TestTimeout_PooledBufferDoesNotLeakBetweenRequests(t *testing.T) {
	for i, body := range []string{strings.Repeat("a", 3000), "b", strings.Repeat("c", 10), "d"} {
		h := Timeout(time.Second, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

		assert.Equal(t, body, rec.Body.String(), "request %d", i)
	}
}
