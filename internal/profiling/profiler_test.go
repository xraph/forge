package profiling

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

type ingest struct {
	mu    sync.Mutex
	names []string
	users []string
	got   chan struct{}
}

func newIngest() *ingest { return &ingest{got: make(chan struct{}, 64)} }

func (in *ingest) handler(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)

	user, _, _ := r.BasicAuth()

	in.mu.Lock()
	in.names = append(in.names, r.URL.Query().Get("name"))
	in.users = append(in.users, user)
	in.mu.Unlock()

	select {
	case in.got <- struct{}{}:
	default:
	}
}

func (in *ingest) wait(t *testing.T) {
	t.Helper()

	select {
	case <-in.got:
	case <-time.After(10 * time.Second):
		t.Fatal("no upload reached the fake Pyroscope")
	}
}

func pushSettings(addr string) Settings {
	return Resolve(Inputs{
		Code:    shared.ProfilingConfig{ServerAddress: addr, BasicAuthUser: "u", BasicAuthPassword: "p"},
		AppName: "orders", Environment: "test", TracingEnabled: true,
	})
}

func TestProfilerPushesProfiles(t *testing.T) {
	in := newIngest()
	srv := httptest.NewServer(http.HandlerFunc(in.handler))
	t.Cleanup(srv.Close)

	p := New(pushSettings(srv.URL), logger.NewTestLogger())
	p.uploadRate = 200 * time.Millisecond

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	in.wait(t)

	if err := p.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	in.mu.Lock()
	defer in.mu.Unlock()

	if !strings.HasPrefix(in.names[0], "orders") || !strings.Contains(in.names[0], "environment=test") {
		t.Fatalf("name = %q, want orders with environment=test", in.names[0])
	}

	if in.users[0] != "u" {
		t.Fatalf("basic auth user = %q, want u", in.users[0])
	}

	if p.Running() {
		t.Fatal("Running after Stop")
	}
}

func TestProfilerPushesUnderSanitizedName(t *testing.T) {
	in := newIngest()
	srv := httptest.NewServer(http.HandlerFunc(in.handler))
	t.Cleanup(srv.Close)

	s := Resolve(Inputs{Code: shared.ProfilingConfig{ServerAddress: srv.URL}, AppName: "My App", TracingEnabled: true})

	p := New(s, logger.NewTestLogger())
	p.uploadRate = 200 * time.Millisecond

	if err := p.Start(); err != nil {
		t.Fatalf("Start with a sanitized name: %v", err)
	}

	in.wait(t)

	_ = p.Stop(t.Context())

	in.mu.Lock()
	defer in.mu.Unlock()

	if !strings.HasPrefix(in.names[0], "My_App") {
		t.Fatalf("name = %q, want My_App", in.names[0])
	}
}

func TestProfilerStopHonoursContext(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{}, 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}

		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs before srv.Close, so the handler can return

	p := New(pushSettings(srv.URL), logger.NewTestLogger())
	p.uploadRate = 200 * time.Millisecond

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("no upload arrived")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.Stop(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want context.DeadlineExceeded", err)
	}

	if time.Since(start) > 3*time.Second {
		t.Fatalf("Stop took %v, want it to return near the deadline", time.Since(start))
	}
}

func TestProfilerWithoutPushDoesNothing(t *testing.T) {
	p := New(Resolve(Inputs{AppName: "orders"}), logger.NewTestLogger())

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	if p.Running() {
		t.Fatal("Running without a server address")
	}

	if err := p.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestProfilerStopBeforeStartAndTwice(t *testing.T) {
	p := New(pushSettings("http://127.0.0.1:1"), logger.NewTestLogger())

	if err := p.Stop(t.Context()); err != nil {
		t.Fatalf("Stop before Start = %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("second Start = %v", err)
	}

	_ = p.Stop(t.Context())

	if err := p.Stop(t.Context()); err != nil {
		t.Fatalf("second Stop = %v", err)
	}
}

func TestSDKLoggerRateLimitsErrors(t *testing.T) {
	capture, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("NewTestLogger did not return *logger.TestLogger")
	}

	l := newSDKLogger(capture)
	l.Infof("starting %s", "x")
	l.Debugf("tick")
	l.Errorf("upload failed: %v", errors.New("refused"))
	l.Errorf("upload failed: %v", errors.New("refused"))

	warns := capture.GetLogsByLevel("WARN")
	if len(warns) != 1 {
		t.Fatalf("warn lines = %d, want 1", len(warns))
	}

	if warns[0].Message != "profiling: pyroscope client error" {
		t.Fatalf("warn message = %q, want %q", warns[0].Message, "profiling: pyroscope client error")
	}

	if n := len(capture.GetLogsByLevel("INFO")); n != 0 {
		t.Fatalf("info lines = %d, want 0 (SDK info goes to debug)", n)
	}
}
