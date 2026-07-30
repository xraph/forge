package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/vessel"
)

type sizedPayload struct {
	Blob string `json:"blob"`
}

// Request bodies were decoded with no cap, so a single unauthenticated request
// could drive allocation as large as the client chose to send.
func TestRequestBodyIsCapped(t *testing.T) {
	r := NewRouter(WithContainer(vessel.New()), WithMaxRequestBodySize(1024))

	if err := r.POST("/echo", func(_ Context, req *sizedPayload) (*sizedPayload, error) {
		return req, nil
	}); err != nil {
		t.Fatal(err)
	}

	body := `{"blob":"` + strings.Repeat("A", 64*1024) + `"}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("64 KiB body accepted against a 1 KiB cap (status %d)", rec.Code)
	}
}

func TestRequestBodyUnderCapSucceeds(t *testing.T) {
	r := NewRouter(WithContainer(vessel.New()), WithMaxRequestBodySize(64*1024))

	if err := r.POST("/echo", func(_ Context, req *sizedPayload) (*sizedPayload, error) {
		return req, nil
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"blob":"small"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("small body rejected: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestResolveMaxBodySizePrecedence(t *testing.T) {
	cases := []struct {
		name       string
		routerWide int64
		route      int64
		want       int64
	}{
		{"falls back to the default", 0, 0, DefaultMaxRequestBodySize},
		{"router-wide setting applies", 4096, 0, 4096},
		{"route overrides router", 4096, 8192, 8192},
		{"route opts out", 4096, -1, 0},
		{"router opts out", -1, 0, 0},
		{"route raises an opted-out router", -1, 2048, 2048},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &router{maxBodySize: c.routerWide}
			if got := r.resolveMaxBodySize(c.route); got != c.want {
				t.Errorf("resolveMaxBodySize(route=%d) with router=%d = %d, want %d",
					c.route, c.routerWide, got, c.want)
			}
		})
	}
}

// SSE fields are newline-delimited. An unescaped newline in the event name or a
// multi-line data payload lets the remainder be parsed as further SSE fields —
// event forgery when any part of the value is user-influenced.
func TestSSESendRejectsNewlineInEventName(t *testing.T) {
	stream, rec := newTestSSEStream(t)

	err := stream.Send("update\n\nevent: admin", []byte("payload"))
	if err == nil {
		t.Fatal("event name containing a newline was accepted")
	}

	if body := rec.Body.String(); strings.Contains(body, "event: admin") {
		t.Errorf("forged event reached the wire: %q", body)
	}
}

func TestSSESendCommentRejectsNewline(t *testing.T) {
	stream, _ := newTestSSEStream(t)

	if err := stream.SendComment("keepalive\n\ndata: forged"); err == nil {
		t.Fatal("comment containing a newline was accepted")
	}
}

// Multi-line data must be emitted as one "data: " line per line, per the SSE
// grammar — otherwise the payload is both corrupted and injectable.
func TestSSEEncodesMultiLineData(t *testing.T) {
	stream, rec := newTestSSEStream(t)

	if err := stream.Send("message", []byte("line one\nline two\r\nline three")); err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()

	for _, want := range []string{
		"event: message\n",
		"data: line one\n",
		"data: line two\n",
		"data: line three\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}

	// A bare "line two" without its data: prefix would mean the payload escaped
	// its field.
	if strings.Contains(body, "\nline two") {
		t.Errorf("data line emitted without its field prefix:\n%s", body)
	}
}

func newTestSSEStream(t *testing.T) (*sseStream, *httptest.ResponseRecorder) {
	t.Helper()

	rec := httptest.NewRecorder()

	stream, err := newSSEStream(rec, httptest.NewRequest(http.MethodGet, "/events", nil), 0)
	if err != nil {
		t.Fatal(err)
	}

	return stream, rec
}

// Connection IDs were derived from time.Now().UnixNano(), which collides for
// upgrades landing in the same clock tick. Two live connections sharing an ID
// means cross-talk anywhere connections are tracked by ID.
func TestConnectionIDsAreUnique(t *testing.T) {
	const n = 10_000

	seen := make(map[string]bool, n)

	for range n {
		id := generateConnectionID()
		if seen[id] {
			t.Fatalf("duplicate connection ID generated: %q", id)
		}

		seen[id] = true
	}
}

// A 500 must not echo internal error text unless explicitly enabled; the wrapped
// message can carry driver output, paths and internal hostnames.
func TestInternalErrorDetailIsGatedByEnvironment(t *testing.T) {
	t.Cleanup(func() { SetExposeInternalErrors(false) })

	const secret = "pq: relation \"internal_billing\" does not exist"

	run := func(expose bool) string {
		SetExposeInternalErrors(expose)

		r := NewRouter(WithContainer(vessel.New()))

		if err := r.GET("/boom", func(Context) error {
			return InternalError(errors.New(secret))
		}); err != nil {
			t.Fatal(err)
		}

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

		return rec.Body.String()
	}

	if body := run(false); strings.Contains(body, "internal_billing") {
		t.Errorf("production response leaked internal error detail: %s", body)
	}

	if body := run(true); !strings.Contains(body, "internal_billing") {
		t.Errorf("development response should include detail, got: %s", body)
	}
}
