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
