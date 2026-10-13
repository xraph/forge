package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xraph/forge/extensions/conduit/core"
)

func testDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
			t.Fatal("CONDUIT_TEST_POSTGRES is required")
		}

		t.Skip("set CONDUIT_TEST_POSTGRES to run PostgreSQL provider tests")
	}

	return dsn
}

// TestMigrateOnACurrentSchemaDoesNotWaitForPublishers covers a replica starting while
// another one publishes: CREATE INDEX IF NOT EXISTS would queue behind the publisher's
// open transaction, and every later publisher would queue behind it.
func TestMigrateOnACurrentSchemaDoesNotWaitForPublishers(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	pool, _ := p.db()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := p.PublishTx(ctx, tx, ns, cfg, envelope(ns, "open", placed)); err != nil {
		t.Fatal(err)
	}

	migrating, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	started := time.Now()

	if err := p.Migrate(migrating); err != nil {
		t.Fatalf("migrating a current schema waited on an open publish: %v", err)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("migrating a current schema took %s", elapsed)
	}
}

func TestMigrationsRejectsAnUnsafeSchema(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Migrations accepted an unsafe schema name")
		}
	}()

	Migrations("conduit'; drop")
}

// TestLeastPrivilegeRoleRunsAnExistingStream covers the runtime role an operator grants
// after migrating as the owner: USAGE on the schema, DML on the tables and USAGE plus
// UPDATE on the stream's sequence, with no CREATE anywhere.
func TestLeastPrivilegeRoleRunsAnExistingStream(t *testing.T) {
	ctx := t.Context()
	dsn := testDSN(t)

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(admin.Close)

	suffix := strings.ReplaceAll(strings.ToLower(core.NewID()), "-", "")
	role, schema, password := "conduit_rt_"+suffix, "conduit_rt_"+suffix, "pw"+suffix
	roleID, schemaID := pgx.Identifier{role}.Sanitize(), pgx.Identifier{schema}.Sanitize()

	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		_, _ = admin.Exec(cleanup, "DROP SCHEMA IF EXISTS "+schemaID+" CASCADE")
		_, _ = admin.Exec(cleanup, "DROP OWNED BY "+roleID)
		_, _ = admin.Exec(cleanup, "DROP ROLE IF EXISTS "+roleID)
	})

	owner := connect(t, Options{Schema: schema})
	ns, cfg := newStream(t, owner)

	for _, statement := range []string{
		"CREATE ROLE " + roleID + " LOGIN PASSWORD '" + password + "' NOCREATEDB NOCREATEROLE NOSUPERUSER",
		"GRANT USAGE ON SCHEMA " + schemaID + " TO " + roleID,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA " + schemaID + " TO " + roleID,
		"GRANT USAGE, UPDATE ON " + owner.sequence(streamKey(ns, cfg.Name)) + " TO " + roleID,
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	var canCreate bool
	if err := admin.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, 'CREATE')`, role, schema).Scan(&canCreate); err != nil {
		t.Fatal(err)
	}

	if canCreate {
		t.Fatal("the test role unexpectedly holds CREATE on the schema")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	u.User = url.UserPassword(role, password)

	p := New(Options{URL: u.String(), Schema: schema, PollInterval: 100 * time.Millisecond})
	if err := p.Connect(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(ctx)) })

	if err := p.EnsureStream(ctx, ns, cfg); err != nil {
		t.Fatalf("the runtime role could not declare an existing stream: %v", err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "least-privilege", placed)); err != nil {
		t.Fatal(err)
	}

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "runtime", "all"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Close(context.WithoutCancel(ctx)) })

	d := next(t, s, 5*time.Second)
	if d.Message().ID != "least-privilege" {
		t.Fatalf("delivered %s", d.Message().ID)
	}

	if err := d.Ack(ctx); err != nil {
		t.Fatal(err)
	}
}
