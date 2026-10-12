package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xraph/forge/extensions/conduit/core"
)

const placed = "orders.placed.v1"

func connect(t *testing.T, options Options) *Provider {
	t.Helper()

	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
			t.Fatal("CONDUIT_TEST_POSTGRES is required")
		}

		t.Skip("set CONDUIT_TEST_POSTGRES to run PostgreSQL provider tests")
	}

	if options.Pool == nil {
		options.URL = dsn
	}

	options.AutoMigrate = true
	if options.PollInterval == 0 {
		options.PollInterval = 100 * time.Millisecond
	}

	p := New(options)
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	return p
}

func newStream(t *testing.T, p *Provider) (string, core.StreamConfig) {
	t.Helper()

	ns := "pg-" + core.NewID()
	cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1}

	if err := p.EnsureStream(t.Context(), ns, cfg); err != nil {
		t.Fatal(err)
	}

	return ns, cfg
}

func envelope(ns, id, messageType string) core.Envelope {
	return core.Envelope{
		ID: id, Type: messageType, ContentType: "application/json", Data: []byte(`{"id": 42}`), CreatedAt: time.Now(),
		Source: core.Identity{Namespace: ns, ServiceID: "orders", InstanceID: "publisher"},
	}
}

func binding(ns string, cfg core.StreamConfig, mode core.DeliveryMode, sub, start string) core.Binding {
	return core.Binding{
		Identity: core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: "a"},
		Stream:   cfg,
		Subscription: core.SubscriptionConfig{
			ID: sub, Stream: cfg.Name, MessageType: placed, Mode: mode, Durable: true, Concurrency: 1, MaxInFlight: 8,
			Timeout: time.Second, MaxAttempts: 2, RetryDelay: time.Millisecond, StartAt: start, BroadcastID: "a",
		},
	}
}

func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func TestRejectsAnUnsafeSchema(t *testing.T) {
	p := New(Options{URL: "postgres://localhost/none", Schema: "conduit'; drop"})
	if err := p.Connect(t.Context()); err == nil {
		t.Fatal("an unsafe schema name was accepted")
	}
}

func TestEnsureStreamAgreesAcrossProviders(t *testing.T) {
	ctx := t.Context()
	p, q := connect(t, Options{}), connect(t, Options{})
	ns := "pg-" + core.NewID()
	cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1}

	errs := make(chan error, 2)

	for _, provider := range []*Provider{p, q} {
		go func() { errs <- provider.EnsureStream(ctx, ns, cfg) }()
	}

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent declaration failed: %v", err)
		}
	}

	changed := cfg
	changed.Subjects = []string{"other.>"}

	if err := p.EnsureStream(ctx, ns, changed); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("a conflicting declaration was accepted: %v", err)
	}
}

// TestMigratesWithoutTheDatabaseCreatePrivilege covers a least-privilege role: the schema
// exists, so migrating must not ask for the database-level CREATE that CREATE SCHEMA IF NOT
// EXISTS demands even when it has nothing to create.
func TestMigratesWithoutTheDatabaseCreatePrivilege(t *testing.T) {
	ctx := t.Context()

	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
			t.Fatal("CONDUIT_TEST_POSTGRES is required")
		}

		t.Skip("set CONDUIT_TEST_POSTGRES to run PostgreSQL provider tests")
	}

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(admin.Close)

	suffix := strings.ReplaceAll(strings.ToLower(core.NewID()), "-", "")
	role, schema, password := "conduit_lp_"+suffix, "conduit_lp_"+suffix, "pw"+suffix
	roleID, schemaID := pgx.Identifier{role}.Sanitize(), pgx.Identifier{schema}.Sanitize()

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_, _ = admin.Exec(cleanup, "DROP SCHEMA IF EXISTS "+schemaID+" CASCADE")
		_, _ = admin.Exec(cleanup, "DROP OWNED BY "+roleID)
		_, _ = admin.Exec(cleanup, "DROP ROLE IF EXISTS "+roleID)
	})

	// The role can use and create in the schema but holds no CREATE on the database. The
	// schema is its own rather than public: tables other tests created in public belong to
	// another user, and CREATE INDEX IF NOT EXISTS needs ownership even when it is a no-op.
	for _, statement := range []string{
		"CREATE ROLE " + roleID + " LOGIN PASSWORD '" + password + "' NOCREATEDB NOCREATEROLE NOSUPERUSER",
		"CREATE SCHEMA " + schemaID,
		"GRANT USAGE, CREATE ON SCHEMA " + schemaID + " TO " + roleID,
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	var hasCreate bool
	if err := admin.QueryRow(ctx, `SELECT has_database_privilege($1, current_database(), 'CREATE')`, role).Scan(&hasCreate); err != nil {
		t.Fatal(err)
	}

	if hasCreate {
		t.Fatal("the test role unexpectedly holds CREATE on the database")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	u.User = url.UserPassword(role, password)

	p := New(Options{URL: u.String(), Schema: schema, AutoMigrate: true})
	if err := p.Connect(ctx); err != nil {
		t.Fatalf("a role without CREATE on the database could not migrate an existing schema: %v", err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(ctx)) })

	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("migrating a second time failed: %v", err)
	}
}

func TestPublishDeduplicatesConcurrentDuplicates(t *testing.T) {
	ctx := t.Context()
	p, q := connect(t, Options{}), connect(t, Options{})
	ns, cfg := newStream(t, p)

	start := make(chan struct{})
	receipts := make(chan core.Receipt, 2)
	errs := make(chan error, 2)

	for _, provider := range []*Provider{p, q} {
		go func() {
			<-start

			r, err := provider.Publish(ctx, ns, cfg, envelope(ns, "same", placed))
			if err != nil {
				errs <- err

				return
			}

			receipts <- r
		}()
	}

	close(start)

	var got []core.Receipt

	for range 2 {
		select {
		case err := <-errs:
			t.Fatal(err)
		case r := <-receipts:
			got = append(got, r)
		}
	}

	if got[0].Sequence != got[1].Sequence || got[0].Duplicate == got[1].Duplicate {
		t.Fatalf("duplicate publication not collapsed: %+v", got)
	}

	pool, _ := p.db()

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+p.t.messages+" WHERE stream_key = $1", streamKey(ns, cfg.Name)).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("stored %d rows: %v", rows, err)
	}
}

func TestPublishTxWakesSubscribersOnlyOnCommit(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	waitFor(t, "listener", p.ListenerConnected)

	signal, unwatch := p.watch(streamChannel(streamKey(ns, cfg.Name)))
	defer unwatch()

	time.Sleep(400 * time.Millisecond) // the listener issues LISTEN on its next 250ms turn

	pool, _ := p.db()

	rolled, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.PublishTx(ctx, rolled, ns, cfg, envelope(ns, "rolled-back", placed)); err != nil {
		t.Fatal(err)
	}

	if err := rolled.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := p.PublishTx(ctx, tx, ns, cfg, envelope(ns, "committed", placed))
	if err != nil || receipt.Sequence == 0 || !receipt.Persisted {
		t.Fatalf("receipt %+v %v", receipt, err)
	}

	select {
	case <-signal:
		t.Fatal("a subscriber woke before the commit")
	case <-time.After(400 * time.Millisecond):
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("no wakeup after the commit")
	}

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+p.t.messages+" WHERE stream_key = $1", streamKey(ns, cfg.Name)).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("stored %d rows: %v", rows, err)
	}
}

func TestSharedPoolIsLeftOpen(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set CONDUIT_TEST_POSTGRES to run PostgreSQL provider tests")
	}

	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	p := New(Options{Pool: pool, AutoMigrate: true, PollInterval: 100 * time.Millisecond})
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "listener built from the pool's settings", p.ListenerConnected)

	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := pool.Ping(t.Context()); err != nil {
		t.Fatalf("closing the provider closed your pool: %v", err)
	}
}

func TestListenerIgnoresTheAppsNotificationHandler(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set CONDUIT_TEST_POSTGRES to run PostgreSQL provider tests")
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}

	var seen atomic.Int64

	config.ConnConfig.OnNotification = func(*pgconn.PgConn, *pgconn.Notification) { seen.Add(1) }

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	p := New(Options{Pool: pool, AutoMigrate: true, PollInterval: 100 * time.Millisecond})
	if err := p.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(t.Context())) })

	ns, cfg := newStream(t, p)

	waitFor(t, "listener", p.ListenerConnected)

	signal, unwatch := p.watch(streamChannel(streamKey(ns, cfg.Name)))
	defer unwatch()

	time.Sleep(400 * time.Millisecond) // the listener issues LISTEN on its next 250ms turn

	if _, err := p.Publish(t.Context(), ns, cfg, envelope(ns, "wake", placed)); err != nil {
		t.Fatal(err)
	}

	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("no wakeup when the pool config carries an OnNotification handler")
	}

	if n := seen.Load(); n != 0 {
		t.Fatalf("the app's handler saw %d conduit notifications", n)
	}
}

func next(t *testing.T, s core.Subscription, within time.Duration) core.Delivery {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), within)
	defer cancel()

	d, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func quiet(t *testing.T, s core.Subscription) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	if d, err := s.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			t.Fatalf("unexpected delivery %s", d.Message().ID)
		}

		t.Fatal(err)
	}
}

func TestLateCommitIsNotSkipped(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Broadcast, "late", "all"))
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := p.db()

	slow, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = slow.Rollback(context.WithoutCancel(ctx)) }()

	first, err := p.PublishTx(ctx, slow, ns, cfg, envelope(ns, "first", placed))
	if err != nil {
		t.Fatal(err)
	}

	second, err := p.Publish(ctx, ns, cfg, envelope(ns, "second", placed))
	if err != nil {
		t.Fatal(err)
	}

	if first.Sequence >= second.Sequence {
		t.Fatalf("expected the open transaction to hold the lower sequence: %d, %d", first.Sequence, second.Sequence)
	}

	// The open transaction holds the horizon, so the committed row waits behind it.
	quiet(t, s)

	if err := slow.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var got []string

	for range 2 {
		d := next(t, s, 5*time.Second)
		got = append(got, d.Message().ID)

		if err := d.Ack(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if !slices.Contains(got, "first") || !slices.Contains(got, "second") {
		t.Fatalf("a late commit was skipped: %v", got)
	}
}

func TestStartAtNewIgnoresCommittedMessagesWhileATransactionIsOpen(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	pool, _ := p.db()

	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := hold.Exec(ctx, `SELECT pg_current_xact_id()`); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "before", placed)); err != nil {
		t.Fatal(err)
	}

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Broadcast, "fresh", "new"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "after", placed)); err != nil {
		t.Fatal(err)
	}

	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	d := next(t, s, 5*time.Second)
	if d.Message().ID != "after" {
		t.Fatalf("a new subscriber received %s", d.Message().ID)
	}

	if err := d.Ack(ctx); err != nil {
		t.Fatal(err)
	}

	quiet(t, s)
}

func TestNonMatchingBacklogDoesNotStall(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	for i := range 250 {
		if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "other-"+strconv.Itoa(i), "orders.cancelled.v1")); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "wanted", placed)); err != nil {
		t.Fatal(err)
	}

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "picky", "all"))
	if err != nil {
		t.Fatal(err)
	}

	if d := next(t, s, 5*time.Second); d.Message().ID != "wanted" {
		t.Fatalf("delivered %s", d.Message().ID)
	}
}

func TestDeliveryContinuesWhenTheListenerDrops(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{PollInterval: 200 * time.Millisecond})
	ns, cfg := newStream(t, p)

	waitFor(t, "listener", p.ListenerConnected)

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "drop", "all"))
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := p.db()

	var killed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = $1) AS terminated`, listenerName(p.owner)).Scan(&killed); err != nil || killed != 1 {
		t.Fatalf("terminated %d listeners: %v", killed, err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "after-drop", placed)); err != nil {
		t.Fatal(err)
	}

	if d := next(t, s, 3*time.Second); d.Message().ID != "after-drop" {
		t.Fatalf("delivered %s", d.Message().ID)
	}

	waitFor(t, "listener reconnect", p.ListenerConnected)
}

func TestIdleWaitIsThePollInterval(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{PollInterval: time.Second})
	ns, cfg := newStream(t, p)

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "idle", "all"))
	if err != nil {
		t.Fatal(err)
	}

	d, wait, err := s.(*subscription).take(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if d != nil {
		t.Fatalf("unexpected delivery %s", d.envelope.ID)
	}

	if wait != time.Second {
		t.Fatalf("an idle subscription waits %s, want the poll interval", wait)
	}
}

func TestRowsAboveTheHorizonRepollQuickly(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{PollInterval: time.Second})
	ns, cfg := newStream(t, p)
	pool, _ := p.db()

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "horizon", "all"))
	if err != nil {
		t.Fatal(err)
	}

	sub := s.(*subscription)

	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := hold.Exec(ctx, `SELECT pg_current_xact_id()`); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "behind-the-horizon", placed)); err != nil {
		t.Fatal(err)
	}

	d, wait, err := sub.take(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if d != nil {
		t.Fatalf("delivered %s past an open transaction", d.envelope.ID)
	}

	if wait > 50*time.Millisecond {
		t.Fatalf("rows above the horizon wait %s, want at most 50ms", wait)
	}

	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Other tests may hold transactions of their own, so give the horizon time to clear.
	var got *delivery

	waitFor(t, "the row to pass the horizon", func() bool {
		var err error

		got, _, err = sub.take(ctx)
		if err != nil {
			t.Fatal(err)
		}

		return got != nil
	})

	if got.envelope.ID != "behind-the-horizon" {
		t.Fatalf("delivered %s", got.envelope.ID)
	}
}

func TestCloseRemovesANonDurableBroadcastConsumer(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	pool, _ := p.db()

	b := binding(ns, cfg, core.Broadcast, "ephemeral", "all")
	b.Subscription.Durable = false

	s, err := p.Subscribe(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "claimed", placed)); err != nil {
		t.Fatal(err)
	}

	d := next(t, s, 5*time.Second)
	if d.Message().ID != "claimed" {
		t.Fatalf("delivered %s", d.Message().ID)
	}

	count := func(table string) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE consumer_id = $1`, b.ConsumerID()).Scan(&n); err != nil {
			t.Fatal(err)
		}

		return n
	}

	if count(p.t.consumers) != 1 || count(p.t.claims) != 1 {
		t.Fatal("expected a consumer and a claim before Close")
	}

	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if c, l := count(p.t.consumers), count(p.t.claims); c != 0 || l != 0 {
		t.Fatalf("Close left %d consumers and %d claims", c, l)
	}
}

func TestSweepClearsEveryAgedOutMessageInOnePass(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})

	ns := "pg-" + core.NewID()
	cfg := core.StreamConfig{Name: "orders", Subjects: []string{"orders.>"}, Replicas: 1, MaxAge: time.Second}

	if err := p.EnsureStream(ctx, ns, cfg); err != nil {
		t.Fatal(err)
	}

	pool, _ := p.db()

	const total, perTx = 2500, 500

	for batch := range total / perTx {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}

		for i := range perTx {
			if _, err := p.PublishTx(ctx, tx, ns, cfg, envelope(ns, "m-"+strconv.Itoa(batch*perTx+i), placed)); err != nil {
				t.Fatal(err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(1200 * time.Millisecond)

	p.sweep(ctx, pool)

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+p.t.messages+" WHERE stream_key = $1", streamKey(ns, cfg.Name)).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	if rows != 0 {
		t.Fatalf("%d aged-out messages remain after one sweep", rows)
	}
}

func TestVisibilityLagReportsAnOpenTransaction(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	pool, _ := p.db()

	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := hold.Exec(ctx, `SELECT pg_current_xact_id()`); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)

	lag, err := p.VisibilityLag(ctx)
	if err != nil || lag < 300*time.Millisecond {
		t.Fatalf("lag %v while a transaction has been open for 300ms: %v", lag, err)
	}
}

func TestConflictingBackfillDoesNotLockOutTheOriginalRun(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	pool, _ := p.db()

	ns, cfg := newStream(t, p)
	b := binding(ns, cfg, core.Competing, "sub", "new")
	original := core.BackfillInput{ID: "fill-1", Subscription: "sub", Start: 1, End: 5}

	// An interrupted run: stored, not complete, and not claimed.
	seed, err := json.Marshal(core.Backfill{
		MessageType: placed, Input: original, ConsumerID: b.ConsumerID(), Stream: cfg.Name, Provider: cfg.Provider,
		Next: original.Start, State: "failed", UpdatedAt: time.Now().UTC(), Persisted: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (namespace, service_id, id, job) VALUES ($1, $2, $3, $4)`, p.t.backfills),
		ns, b.Identity.ServiceID, original.ID, string(seed)); err != nil {
		t.Fatal(err)
	}

	reused := original
	reused.End = 9

	if _, err := p.RunBackfill(ctx, b, reused); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("reusing the ID with another range: %v, want ErrConflict", err)
	}

	job, err := p.RunBackfill(ctx, b, original)
	if err != nil || job.State != "complete" {
		t.Fatalf("resume with the original range after a conflicting request: state %q, %v", job.State, err)
	}
}

func TestReplacementKeepsRegistration(t *testing.T) {
	ctx := t.Context()
	old := connect(t, Options{})

	time.Sleep(5 * time.Millisecond) // the replacement process starts later

	replacement := connect(t, Options{})
	ns := "pg-" + core.NewID()
	instance := core.Instance{Identity: core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: "pod-0"}, Version: "1", Ready: true}

	if err := old.Register(ctx, instance); err != nil {
		t.Fatal(err)
	}

	if err := replacement.Register(ctx, instance); err != nil {
		t.Fatal(err)
	}

	stopping := instance
	stopping.Ready = false

	if err := old.Register(ctx, stopping); err != nil {
		t.Fatal(err)
	}

	if err := old.Deregister(ctx, instance.Identity); err != nil {
		t.Fatal(err)
	}

	resolved, err := replacement.Resolve(ctx, ns, "billing")
	if err != nil || len(resolved) != 1 || !resolved[0].Ready {
		t.Fatalf("the old process removed its replacement: %+v %v", resolved, err)
	}
}

func TestAbandonedCallIsWithdrawn(t *testing.T) {
	caller, server := connect(t, Options{}), connect(t, Options{})
	ns := "pg-" + core.NewID()
	identity := core.Identity{Namespace: ns, ServiceID: "web", InstanceID: "caller"}

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	// The request itself would stay valid for a minute; only withdrawal stops it running.
	request := core.RPCRequest{
		Service: "pricing", Method: "quote", Deadline: time.Now().Add(time.Minute),
		Envelope: core.Envelope{ID: core.NewID(), Type: "quote", Source: identity, ContentType: "application/json", Data: []byte(`{}`), CreatedAt: time.Now()},
	}

	if _, err := caller.RequestRPC(ctx, identity, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the caller to time out: %v", err)
	}

	var ran atomic.Int64

	binding := core.RPCBinding{
		Identity: core.Identity{Namespace: ns, ServiceID: "pricing", InstanceID: "a"}, Method: "quote",
		Config: core.RPCConfig{Timeout: 5 * time.Second, Concurrency: 1, MaxInFlight: 1}, MaxPayloadBytes: 1 << 20,
	}

	srv, err := server.ServeRPC(t.Context(), binding, func(_ context.Context, r core.RPCRequest) core.RPCResponse {
		ran.Add(1)

		return core.RPCResponse{ID: r.Envelope.ID, Source: binding.Identity}
	})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = srv.Close(context.WithoutCancel(t.Context())) }()

	time.Sleep(time.Second)

	if ran.Load() != 0 {
		t.Fatal("a request its caller abandoned still ran")
	}
}

func TestRetryWithTheSameIDGetsItsOwnReply(t *testing.T) {
	caller, server := connect(t, Options{}), connect(t, Options{})
	ns := "pg-" + core.NewID()
	identity := core.Identity{Namespace: ns, ServiceID: "web", InstanceID: "caller"}

	var (
		calls    atomic.Int64
		finished = make(chan struct{})
	)

	binding := core.RPCBinding{
		Identity: core.Identity{Namespace: ns, ServiceID: "pricing", InstanceID: "a"}, Method: "quote",
		Config: core.RPCConfig{Timeout: 5 * time.Second, Concurrency: 2, MaxInFlight: 2}, MaxPayloadBytes: 1 << 20,
	}

	srv, err := server.ServeRPC(t.Context(), binding, func(_ context.Context, r core.RPCRequest) core.RPCResponse {
		attempt := calls.Add(1)
		if attempt == 1 {
			time.Sleep(800 * time.Millisecond) // answers after its caller has given up

			defer close(finished)
		}

		return core.RPCResponse{ID: r.Envelope.ID, Source: binding.Identity, Data: fmt.Appendf(nil, `{"attempt":%d}`, attempt)}
	})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = srv.Close(context.WithoutCancel(t.Context())) }()

	request := core.RPCRequest{
		Service: "pricing", Method: "quote", Deadline: time.Now().Add(time.Minute),
		Envelope: core.Envelope{ID: core.NewID(), Type: "quote", Source: identity, ContentType: "application/json", Data: []byte(`{}`), CreatedAt: time.Now()},
	}

	first, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	if _, err := caller.RequestRPC(first, identity, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the first attempt to time out: %v", err)
	}

	<-finished

	// The late reply is committed by the time the handler's response transaction ends.
	pool, err := caller.db()
	if err != nil {
		t.Fatal(err)
	}

	for range 50 {
		var stored int
		if err := pool.QueryRow(t.Context(), fmt.Sprintf(`SELECT count(*) FROM %s WHERE id = $1`, caller.t.rpcResponses),
			hash(identity.Namespace, identity.ServiceID, identity.InstanceID, request.Envelope.ID)).Scan(&stored); err != nil {
			t.Fatal(err)
		}

		if stored > 0 {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	second, cancelSecond := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelSecond()

	response, err := caller.RequestRPC(second, identity, request)
	if err != nil {
		t.Fatal(err)
	}

	if string(response.Data) != `{"attempt":2}` {
		t.Fatalf("the retry received %s instead of its own reply", response.Data)
	}
}

// TestCursorAheadOfTheClusterFailsSubscribe covers a database restored from a dump into
// a cluster whose transaction IDs are lower: new rows would sit behind every cursor and
// never be delivered, so subscribing must say so.
func TestCursorAheadOfTheClusterFailsSubscribe(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	b := binding(ns, cfg, core.Competing, "restored", "all")

	if _, err := p.Subscribe(ctx, b); err != nil {
		t.Fatal(err)
	}

	pool, _ := p.db()

	if _, err := pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET cursor_txid = (pg_current_xact_id()::text::bigint + 1000000000)::text::xid8 WHERE consumer_id = $1`, p.t.consumers), b.ConsumerID()); err != nil {
		t.Fatal(err)
	}

	_, err := p.Subscribe(ctx, b)
	if err == nil || !strings.Contains(err.Error(), "ahead of this cluster's transaction IDs") {
		t.Fatalf("a cursor ahead of the cluster was accepted: %v", err)
	}
}

// TestAboveHorizonRepollsBackOff covers a transaction left open for a long time: the
// quick re-poll must back off to PollInterval, not hit the database every 50ms.
func TestAboveHorizonRepollsBackOff(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{PollInterval: time.Second})
	ns, cfg := newStream(t, p)
	pool, _ := p.db()

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "backoff", "all"))
	if err != nil {
		t.Fatal(err)
	}

	sub := s.(*subscription)

	holdHorizon := func(id string) pgx.Tx {
		hold, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = hold.Rollback(context.WithoutCancel(ctx)) })

		if _, err := hold.Exec(ctx, `SELECT pg_current_xact_id()`); err != nil {
			t.Fatal(err)
		}

		if _, err := p.Publish(ctx, ns, cfg, envelope(ns, id, placed)); err != nil {
			t.Fatal(err)
		}

		return hold
	}

	take := func() time.Duration {
		d, wait, err := sub.take(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if d != nil {
			t.Fatalf("delivered %s past an open transaction", d.envelope.ID)
		}

		return wait
	}

	hold := holdHorizon("held")

	for _, ms := range []int64{50, 100, 200, 400, 800, 1000, 1000} {
		if got, want := take(), time.Duration(ms)*time.Millisecond; got != want {
			t.Fatalf("re-poll waited %s, want %s", got, want)
		}
	}

	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the held row to be delivered", func() bool {
		d, _, err := sub.take(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if d == nil {
			return false
		}

		if err := d.Ack(ctx); err != nil {
			t.Fatal(err)
		}

		return true
	})

	holdHorizon("held-again")

	if got := take(); got != 50*time.Millisecond {
		t.Fatalf("after a delivery the re-poll waited %s, want it reset to 50ms", got)
	}
}

func TestReplayForAMissingStreamIsNotFound(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns := "pg-" + core.NewID()
	identity := core.Identity{Namespace: ns, ServiceID: "billing", InstanceID: "a"}

	letter, err := json.Marshal(core.DeadLetter{
		ID: "lost", Message: envelope(ns, "lost", placed),
		Delivery: core.DeliveryInfo{Stream: "retired", ConsumerID: "consumer", SubscriptionID: "process"},
	})
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := p.db()

	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (namespace, service_id, subscription_id, id, letter) VALUES ($1, $2, $3, $4, $5)`, p.t.deadLetters),
		ns, identity.ServiceID, "process", "lost", string(letter)); err != nil {
		t.Fatal(err)
	}

	if _, err := p.ReplayDeadLetter(ctx, identity, "process", "lost"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("replaying into a missing stream returned %v, want ErrNotFound", err)
	}
}

// TestStaleHandlerDoesNotAnswerARetry covers a request swept after its deadline and
// stored again, unclaimed, by a retry with the same ID: the handler still working on the
// first copy must not answer or remove the second.
func TestStaleHandlerDoesNotAnswerARetry(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	pool, _ := p.db()
	s := &rpcServer{provider: p, base: ctx, claimant: "server-" + core.NewID()}

	store := func(id string, claimedBy any) {
		t.Helper()

		if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (id, channel, request, reply_channel, deadline, claimed_by) VALUES ($1, 'fcr_test', '{}', $2, $3, $4)`, p.t.rpcRequests),
			id, p.replyChannel(), time.Now().Add(time.Minute), claimedBy); err != nil {
			t.Fatal(err)
		}
	}

	count := func(table, id string) int {
		t.Helper()

		var n int
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE id = $1`, table), id).Scan(&n); err != nil {
			t.Fatal(err)
		}

		return n
	}

	retried := core.NewID()
	store(retried, nil)
	s.respond(retried, p.replyChannel(), time.Now().Add(time.Minute), core.RPCResponse{ID: retried})

	if count(p.t.rpcRequests, retried) != 1 || count(p.t.rpcResponses, retried) != 0 {
		t.Fatal("a stale handler answered a request it no longer holds")
	}

	held := core.NewID()
	store(held, s.claimant)
	s.respond(held, p.replyChannel(), time.Now().Add(time.Minute), core.RPCResponse{ID: held})

	if count(p.t.rpcRequests, held) != 0 || count(p.t.rpcResponses, held) != 1 {
		t.Fatal("the handler holding the request could not answer it")
	}
}

func TestEmptyAndNilSubjectsAgree(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns := "pg-" + core.NewID()

	if err := p.EnsureStream(ctx, ns, core.StreamConfig{Name: "orders", Replicas: 1}); err != nil {
		t.Fatal(err)
	}

	if err := p.EnsureStream(ctx, ns, core.StreamConfig{Name: "orders", Subjects: []string{}, Replicas: 1}); err != nil {
		t.Fatalf("an empty subject list conflicted with a nil one: %v", err)
	}
}
