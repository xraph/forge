package postgres

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestListenerUsesTheURLBesideASharedPool covers a pool that goes through PgBouncer: the
// listener must connect where URL points, not copy the pool's settings.
func TestListenerUsesTheURLBesideASharedPool(t *testing.T) {
	dsn := testDSN(t)

	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	direct, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	query := direct.Query()
	query.Set("search_path", "listener_marker")
	query.Set("application_name", "the-apps-name")
	direct.RawQuery = query.Encode()

	p := New(Options{Pool: pool, URL: direct.String(), AutoMigrate: true, PollInterval: 100 * time.Millisecond})
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	config := p.listener.config
	if got := config.RuntimeParams["search_path"]; got != "listener_marker" {
		t.Fatalf("the listener copied the pool's settings instead of using URL (search_path %q)", got)
	}

	if got := config.RuntimeParams["application_name"]; got != listenerName(p.owner) {
		t.Fatalf("the listener's application_name is %q", got)
	}

	if config.OnNotification != nil {
		t.Fatal("the listener kept a notification handler")
	}

	waitFor(t, "listener connected through URL", p.ListenerConnected)
}

// TestListenerURLMayCarryPoolSettings covers a listener URL shared with the app's pool
// config: pgxpool-only parameters such as pool_max_conns are not server settings, and
// sending them to PostgreSQL would keep the listener from ever connecting.
func TestListenerURLMayCarryPoolSettings(t *testing.T) {
	dsn := testDSN(t)

	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	withPool, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	query := withPool.Query()
	query.Set("pool_max_conns", "4")
	withPool.RawQuery = query.Encode()

	p := New(Options{Pool: pool, URL: withPool.String(), AutoMigrate: true, PollInterval: 100 * time.Millisecond})
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	if _, ok := p.listener.config.RuntimeParams["pool_max_conns"]; ok {
		t.Fatal("the listener sends pool_max_conns to the server")
	}

	waitFor(t, "listener connected through a URL with pool settings", p.ListenerConnected)
}

func TestInvalidListenerURLFailsConnect(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	p := New(Options{Pool: pool, URL: "postgres://%zz", PollInterval: 100 * time.Millisecond})
	if err := p.Connect(t.Context()); err == nil {
		_ = p.Close(t.Context())

		t.Fatal("an unparsable listener URL was accepted")
	}
}

func signalled(signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	default:
		return false
	}
}

// TestReplyWakesOnlyItsCaller covers many calls waiting on one provider's reply channel:
// each reply must wake the call it answers, not every caller.
func TestReplyWakesOnlyItsCaller(t *testing.T) {
	l := newListener(nil, nil)

	first, unwatchFirst := l.watchID("fcp_reply", "first")
	defer unwatchFirst()

	second, unwatchSecond := l.watchID("fcp_reply", "second")
	defer unwatchSecond()

	stream, unwatchStream := l.watch("fc_stream")
	defer unwatchStream()

	l.notify("fcp_reply", "first")

	if !signalled(first) {
		t.Fatal("the reply did not wake its caller")
	}

	if signalled(second) {
		t.Fatal("a reply woke a caller it does not belong to")
	}

	if signalled(stream) {
		t.Fatal("a reply woke a watcher on another channel")
	}

	// Watchers on the channel alone still hear every notification on it.
	l.notify("fc_stream", "")
	l.notify("fc_stream", "anything")

	if !signalled(stream) {
		t.Fatal("a stream watcher missed a notification")
	}

	if got := l.channels(); len(got) != 2 {
		t.Fatalf("listening on %v, want each channel once", got)
	}

	// A reconnect still wakes everyone.
	l.notifyAll()

	if !signalled(first) || !signalled(second) || !signalled(stream) {
		t.Fatal("a reconnect did not wake every watcher")
	}
}

// logBuffer collects log output from the listener's goroutine for the test to read.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func (b *logBuffer) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestListenerLogsAFailedConnect(t *testing.T) {
	dsn := testDSN(t)

	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	// A port that was free a moment ago refuses the listener's connection.
	closed, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := closed.Addr().String()
	_ = closed.Close()

	unreachable, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	unreachable.Host = address

	var logs logBuffer

	p := New(Options{Pool: pool, URL: unreachable.String(), Logger: logs.logger()})
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "level=WARN") {
		if time.Now().After(deadline) {
			t.Fatalf("no warning within 2s of an unreachable listener: %q", logs.String())
		}

		time.Sleep(20 * time.Millisecond)
	}

	if password, ok := unreachable.User.Password(); ok && strings.Contains(logs.String(), password) {
		t.Fatalf("the log carries the password: %q", logs.String())
	}

	t.Log(logs.String())
}

// TestConnectWarningsAreRateLimited covers a database that stays down: the backoff stops
// growing at five seconds, and a warning every attempt would then fill the log.
func TestConnectWarningsAreRateLimited(t *testing.T) {
	var limit warnLimit

	start := time.Now()
	steps := []struct {
		after   time.Duration
		backoff time.Duration
		warn    bool
	}{
		{0, 100 * time.Millisecond, true},
		{100 * time.Millisecond, 200 * time.Millisecond, true},
		{300 * time.Millisecond, 400 * time.Millisecond, true},
		{5 * time.Second, 5 * time.Second, true},
		{10 * time.Second, 5 * time.Second, false},
		{30 * time.Second, 5 * time.Second, false},
		{35 * time.Second, 5 * time.Second, true},
		{40 * time.Second, 5 * time.Second, false},
	}

	for i, step := range steps {
		if got := limit.allow(start.Add(step.after), step.backoff); got != step.warn {
			t.Fatalf("step %d (%v at %v backoff): warn=%v, want %v", i, step.after, step.backoff, got, step.warn)
		}
	}

	// A connection that comes up starts the next outage afresh.
	limit = warnLimit{}
	if !limit.allow(start.Add(41*time.Second), 5*time.Second) {
		t.Fatal("the first failure of a new outage was not logged")
	}
}

func TestListenerLogsADropAndTheReconnect(t *testing.T) {
	var logs logBuffer

	p := connect(t, Options{Logger: logs.logger()})

	waitFor(t, "listener", p.ListenerConnected)

	pool, _ := p.db()

	var killed int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = $1) AS terminated`, listenerName(p.owner)).Scan(&killed); err != nil || killed != 1 {
		t.Fatalf("terminated %d listeners: %v", killed, err)
	}

	waitFor(t, "a dropped-connection warning", func() bool { return strings.Contains(logs.String(), "level=WARN") })
	waitFor(t, "a reconnect record", func() bool { return strings.Contains(logs.String(), "level=INFO") })
	t.Log(logs.String())
}

// TestListenerBacksOffAConnectionThatKeepsDropping covers a connection that opens and
// then dies straight away, as on a hot standby where LISTEN fails. Reconnecting at once
// would spin, logging two records and waking every subscriber each time.
func TestListenerBacksOffAConnectionThatKeepsDropping(t *testing.T) {
	dsn := testDSN(t)

	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	short, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	query := short.Query()
	query.Set("idle_session_timeout", "20") // the server ends the idle listener after 20ms
	short.RawQuery = query.Encode()

	var logs logBuffer

	p := connect(t, Options{Pool: pool, URL: short.String(), Logger: logs.logger()})

	time.Sleep(2 * time.Second)

	p.mu.RLock()
	attempts := p.listener.attempts.Load()
	p.mu.RUnlock()

	records := strings.Count(logs.String(), "level=")
	if attempts > 8 || records > 12 {
		t.Fatalf("%d connection attempts and %d log records in 2s:\n%s", attempts, records, logs.String())
	}

	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("the dropping connection was never reported: %q", logs.String())
	}

	t.Logf("%d attempts, %d records:\n%s", attempts, records, logs.String())
}
