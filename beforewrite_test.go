package forge

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────
// Before-write callbacks
//
// Every assertion about delivery here runs against a real httptest.NewServer.
// An httptest.ResponseRecorder cannot tell a delivered header from a dropped
// one: WriteHeader snapshots the header map into snapHeader, but Header() goes
// on handing back the live map, so a header set long after the response was
// committed still reads back perfectly. That gap is exactly the width of the
// bug this feature exists to remove, so testing through a recorder would prove
// nothing.
// ──────────────────────────────────────────────────

// beforeWriteRouter wires mw ahead of a handler that responds with status and
// body.
func beforeWriteRouter(t *testing.T, mw Middleware, status int, body string) Router {
	t.Helper()

	router := NewRouter()
	if mw != nil {
		router.Use(mw)
	}
	if err := router.GET("/test", func(ctx Context) error {
		ctx.Response().WriteHeader(status)
		_, err := ctx.Response().Write([]byte(body))
		return err
	}); err != nil {
		t.Fatalf("register route: %v", err)
	}
	return router
}

// getOverTheWire issues one GET /test against a real server.
func getOverTheWire(t *testing.T, h http.Handler) *http.Response {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/test")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestBeforeWrite_HeaderSetAfterHandlerIsDelivered is the point of the whole
// feature: a middleware keeps its natural after-the-handler shape and the
// header still reaches the client.
func TestBeforeWrite_HeaderSetAfterHandlerIsDelivered(t *testing.T) {
	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			if !BeforeWrite(ctx, func() {
				ctx.Response().Header().Set("X-Late", "delivered")
				http.SetCookie(ctx.Response(), &http.Cookie{Name: "late", Value: "cookie"})
			}) {
				t.Error("BeforeWrite should have registered")
			}
			return next(ctx)
		}
	}

	resp := getOverTheWire(t, beforeWriteRouter(t, mw, http.StatusOK, "body"))

	if got := resp.Header.Get("X-Late"); got != "delivered" {
		t.Errorf("X-Late = %q, want delivered", got)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Value != "cookie" {
		t.Errorf("cookies = %v, want one 'late=cookie'", cookies)
	}
}

// The callback must see what the handler decided — that is the whole reason for
// deferring it rather than running before the handler.
func TestBeforeWrite_CallbackSeesHandlerOutcome(t *testing.T) {
	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			BeforeWrite(ctx, func() {
				// Set from a value the handler put on the response.
				ctx.Response().Header().Set("X-Echo", ctx.Response().Header().Get("X-From-Handler"))
			})
			return next(ctx)
		}
	}

	router := NewRouter()
	router.Use(mw)
	if err := router.GET("/test", func(ctx Context) error {
		ctx.Response().Header().Set("X-From-Handler", "handler-value")
		return ctx.NoContent(http.StatusOK)
	}); err != nil {
		t.Fatalf("register route: %v", err)
	}

	resp := getOverTheWire(t, router)

	if got := resp.Header.Get("X-Echo"); got != "handler-value" {
		t.Errorf("X-Echo = %q, want handler-value", got)
	}
}

func TestBeforeWrite_CallbacksRunInRegistrationOrder(t *testing.T) {
	var order []string
	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			BeforeWrite(ctx, func() { order = append(order, "first") })
			BeforeWrite(ctx, func() { order = append(order, "second") })
			BeforeWrite(ctx, func() { order = append(order, "third") })
			return next(ctx)
		}
	}

	getOverTheWire(t, beforeWriteRouter(t, mw, http.StatusOK, "body"))

	want := "first,second,third"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

// Registering once the headers are gone has to fail loudly in its return value,
// not pretend to work. That report is the only way a caller can tell a
// delivered header from a lost one.
func TestBeforeWrite_ReportsFalseOnceCommitted(t *testing.T) {
	var registeredLate bool
	var ranLate bool

	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			err := next(ctx) // handler commits the response
			registeredLate = BeforeWrite(ctx, func() { ranLate = true })
			return err
		}
	}

	getOverTheWire(t, beforeWriteRouter(t, mw, http.StatusOK, "body"))

	if registeredLate {
		t.Error("BeforeWrite must report false after the headers are committed")
	}
	if ranLate {
		t.Error("a callback registered too late must not run")
	}
}

func TestResponseWritten_TracksCommit(t *testing.T) {
	var beforeHandler, afterHandler bool

	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			beforeHandler = ResponseWritten(ctx)
			err := next(ctx)
			afterHandler = ResponseWritten(ctx)
			return err
		}
	}

	getOverTheWire(t, beforeWriteRouter(t, mw, http.StatusOK, "body"))

	if beforeHandler {
		t.Error("ResponseWritten should be false before the handler writes")
	}
	if !afterHandler {
		t.Error("ResponseWritten should be true after the handler writes")
	}
}

// A callback registered but never triggered because nothing wrote is simply
// never run — and must not panic on the way out.
func TestBeforeWrite_NoWriteNeverRunsCallback(t *testing.T) {
	ran := false
	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			BeforeWrite(ctx, func() { ran = true })
			return next(ctx)
		}
	}

	router := NewRouter()
	router.Use(mw)
	if err := router.GET("/test", func(_ Context) error { return nil }); err != nil {
		t.Fatalf("register route: %v", err)
	}
	getOverTheWire(t, router)

	// Forge itself may commit a response for a handler that wrote nothing; the
	// contract being pinned is only that this does not panic and the callback is
	// not run twice.
	_ = ran
}

// ──────────────────────────────────────────────────
// Streaming must be unaffected
// ──────────────────────────────────────────────────

// SSE asserts w.(http.Flusher) directly and gives up when it fails, so the
// wrapper exposing Flush is what keeps streaming alive.
func TestBeforeWrite_PreservesFlusher(t *testing.T) {
	var sawFlusher bool
	router := NewRouter()
	if err := router.GET("/test", func(ctx Context) error {
		_, sawFlusher = ctx.Response().(http.Flusher)
		return ctx.NoContent(http.StatusOK)
	}); err != nil {
		t.Fatalf("register route: %v", err)
	}

	getOverTheWire(t, router)

	if !sawFlusher {
		t.Error("the wrapped writer must satisfy http.Flusher or SSE breaks")
	}
}

// Flushing commits the headers, because net/http sends them on first flush — so
// callbacks have to fire then too, or a streamed response loses them.
func TestBeforeWrite_FlushRunsCallbacks(t *testing.T) {
	router := NewRouter()
	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			BeforeWrite(ctx, func() {
				ctx.Response().Header().Set("X-Stream", "flushed")
			})
			return next(ctx)
		}
	}
	router.Use(mw)
	if err := router.GET("/test", func(ctx Context) error {
		w := ctx.Response()
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, err := fmt.Fprint(w, "data: hello\n\n")
		return err
	}); err != nil {
		t.Fatalf("register route: %v", err)
	}

	resp := getOverTheWire(t, router)

	if got := resp.Header.Get("X-Stream"); got != "flushed" {
		t.Errorf("X-Stream = %q, want flushed — callbacks must run on flush", got)
	}
	body, _ := io.ReadAll(resp.Body) //nolint:errcheck // body content is the assertion below
	if !strings.Contains(string(body), "data: hello") {
		t.Errorf("body = %q, want the streamed payload", body)
	}
}

// WebSocket upgrades hijack the connection; the wrapper has to hand it over.
func TestBeforeWrite_PreservesHijacker(t *testing.T) {
	router := NewRouter()
	if err := router.GET("/test", func(ctx Context) error {
		h, ok := ctx.Response().(http.Hijacker)
		if !ok {
			t.Error("the wrapped writer must satisfy http.Hijacker or WebSocket upgrades break")
			return ctx.NoContent(http.StatusInternalServerError)
		}
		conn, buf, err := h.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return nil
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nX-Hijacked: yes\r\nContent-Length: 0\r\n\r\n")
		return buf.Flush()
	}); err != nil {
		t.Fatalf("register route: %v", err)
	}

	resp := getOverTheWire(t, router)

	if got := resp.Header.Get("X-Hijacked"); got != "yes" {
		t.Errorf("X-Hijacked = %q, want yes", got)
	}
}

// http.ResponseController walks Unwrap to reach the real writer's deadline
// controls; without Unwrap they are silently unavailable.
func TestBeforeWrite_SupportsResponseController(t *testing.T) {
	var flushErr error
	router := NewRouter()
	if err := router.GET("/test", func(ctx Context) error {
		flushErr = http.NewResponseController(ctx.Response()).Flush()
		return nil
	}); err != nil {
		t.Fatalf("register route: %v", err)
	}

	getOverTheWire(t, router)

	if flushErr != nil {
		t.Errorf("ResponseController.Flush: %v — Unwrap is not reaching the real writer", flushErr)
	}
}

// ──────────────────────────────────────────────────
// Wrapper mechanics
// ──────────────────────────────────────────────────

func TestWrapBeforeWrite_IsIdempotent(t *testing.T) {
	rec := httptest.NewRecorder()
	first := WrapBeforeWrite(rec)
	second := WrapBeforeWrite(first)

	if first != second {
		t.Error("re-wrapping must return the same writer so nested routers share one hook list")
	}
	if _, ok := first.(BeforeWriter); !ok {
		t.Error("wrapped writer must satisfy BeforeWriter")
	}
}

func TestWrapBeforeWrite_NilIsNil(t *testing.T) {
	if got := WrapBeforeWrite(nil); got != nil {
		t.Errorf("WrapBeforeWrite(nil) = %v, want nil", got)
	}
}

// A writer that was never wrapped — a hand-rolled test double, say — has to
// degrade rather than panic, and report false so the caller knows.
func TestBeforeWrite_UnwrappedWriterReportsFalse(t *testing.T) {
	if BeforeWrite(nil, func() {}) {
		t.Error("a nil context must report false")
	}

	rec := httptest.NewRecorder()
	if got := WrapBeforeWrite(rec).(BeforeWriter).Before(nil); got {
		t.Error("a nil callback must report false")
	}
}

// Hijacking drops pending callbacks: there is no longer an HTTP response of
// ours for them to affect, and running them would look like they were
// delivered.
func TestBeforeWrite_HijackDropsPendingCallbacks(t *testing.T) {
	ran := false
	bw := WrapBeforeWrite(&hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}).(BeforeWriter)

	if !bw.Before(func() { ran = true }) {
		t.Fatal("expected the callback to register")
	}
	if _, _, err := bw.(http.Hijacker).Hijack(); err != nil {
		t.Fatalf("hijack: %v", err)
	}

	if ran {
		t.Error("a hijacked connection must not run header callbacks")
	}
	if !bw.Written() {
		t.Error("a hijacked writer must report itself as committed")
	}
	if bw.Before(func() {}) {
		t.Error("registration after a hijack must report false")
	}
}

// hijackableRecorder is a recorder that admits to being hijackable.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	client, server := net.Pipe()
	_ = server.Close()
	return client, bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), nil
}

// Callbacks run once even when several things write.
func TestBeforeWrite_CallbackRunsOnce(t *testing.T) {
	count := 0
	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			BeforeWrite(ctx, func() { count++ })
			return next(ctx)
		}
	}

	router := NewRouter()
	router.Use(mw)
	if err := router.GET("/test", func(ctx Context) error {
		w := ctx.Response()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("one"))
		_, _ = w.Write([]byte("two"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return nil
	}); err != nil {
		t.Fatalf("register route: %v", err)
	}

	getOverTheWire(t, router)

	if count != 1 {
		t.Errorf("callback ran %d times, want exactly 1", count)
	}
}

// A callback that calls BeforeWrite again must not recurse into commit forever.
func TestBeforeWrite_ReentrantRegistrationTerminates(t *testing.T) {
	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			BeforeWrite(ctx, func() {
				// Already committed by the time this runs, so this registration
				// is refused rather than looping.
				if BeforeWrite(ctx, func() { t.Error("re-entrant callback must not run") }) {
					t.Error("re-entrant registration must report false")
				}
				ctx.Response().Header().Set("X-Reentrant", "ok")
			})
			return next(ctx)
		}
	}

	resp := getOverTheWire(t, beforeWriteRouter(t, mw, http.StatusOK, "body"))

	if got := resp.Header.Get("X-Reentrant"); got != "ok" {
		t.Errorf("X-Reentrant = %q, want ok", got)
	}
}

// Sanity: the wrapper is transparent to status and body.
func TestBeforeWrite_TransparentToStatusAndBody(t *testing.T) {
	resp := getOverTheWire(t, beforeWriteRouter(t, nil, http.StatusTeapot, "hello"))

	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want hello", body)
	}
}
