package postgres

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
