package postgres

import (
	"context"
	"net/url"
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
	l := newListener(nil)

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
