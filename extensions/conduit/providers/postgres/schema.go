package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations returns the provider's schema as ordered, idempotent statements, for an
// application that runs every migration through one central runner. The schema name must
// be a lowercase identifier; Migrations panics on any other name.
func Migrations(schema string) []string {
	schema = migrationSchema(schema)
	t := newTables(schema)
	quoted := pgx.Identifier{schema}.Sanitize()

	return []string{
		// CREATE SCHEMA IF NOT EXISTS checks the database CREATE privilege before it checks
		// whether the schema exists, so a least-privilege role would fail on a schema that is
		// already there. Create it only when it is missing.
		fmt.Sprintf(`DO $$ BEGIN IF to_regnamespace('%s') IS NULL THEN CREATE SCHEMA %s; END IF; END $$`,
			strings.ReplaceAll(quoted, "'", "''"), quoted),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 stream_key TEXT PRIMARY KEY, namespace TEXT NOT NULL, name TEXT NOT NULL,
 config JSONB NOT NULL, max_age_ms BIGINT NOT NULL, max_messages BIGINT NOT NULL)`, t.streams),
		// The prune watermark: the highest (txid, seq) any retention path has deleted from
		// the stream. NULL means nothing was ever pruned. ReadAfter compares a reader's
		// position with it to tell whether messages after that position may be gone.
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS pruned_txid XID8`, t.streams),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS pruned_seq BIGINT`, t.streams),
		// Envelopes are json, not jsonb, so the bytes you publish are the bytes you read.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 stream_key TEXT NOT NULL, seq BIGINT NOT NULL, txid XID8 NOT NULL DEFAULT pg_current_xact_id(),
 dedup_key TEXT NOT NULL, message_type TEXT NOT NULL, target_consumer TEXT NOT NULL,
 envelope JSON NOT NULL, accepted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (stream_key, seq), UNIQUE (stream_key, dedup_key))`, t.messages),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS forge_conduit_messages_cursor ON %s (stream_key, txid, seq)`, t.messages),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS forge_conduit_messages_age ON %s (stream_key, accepted_at)`, t.messages),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 consumer_id TEXT PRIMARY KEY, stream_key TEXT NOT NULL, fingerprint TEXT NOT NULL, durable BOOLEAN NOT NULL,
 cursor_txid XID8 NOT NULL, cursor_seq BIGINT NOT NULL, min_seq BIGINT NOT NULL DEFAULT 0,
 start_snapshot PG_SNAPSHOT, paused BOOLEAN NOT NULL DEFAULT false)`, t.consumers),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 consumer_id TEXT NOT NULL, seq BIGINT NOT NULL, stream_key TEXT NOT NULL, token TEXT NOT NULL,
 attempt BIGINT NOT NULL, visible_at TIMESTAMPTZ NOT NULL, PRIMARY KEY (consumer_id, seq))`, t.claims),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 namespace TEXT NOT NULL, service_id TEXT NOT NULL, subscription_id TEXT NOT NULL, id TEXT NOT NULL,
 letter JSON NOT NULL, replayed BOOLEAN NOT NULL DEFAULT false,
 PRIMARY KEY (namespace, service_id, subscription_id, id))`, t.deadLetters),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 namespace TEXT NOT NULL, service_id TEXT NOT NULL, id TEXT NOT NULL, job JSON NOT NULL, claim_at TIMESTAMPTZ,
 PRIMARY KEY (namespace, service_id, id))`, t.backfills),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 namespace TEXT NOT NULL, service_id TEXT NOT NULL, instance_id TEXT NOT NULL, record JSON NOT NULL,
 owner TEXT NOT NULL, generation BIGINT NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (namespace, service_id, instance_id))`, t.instances),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
 id TEXT PRIMARY KEY, channel TEXT NOT NULL, request JSON NOT NULL, reply_channel TEXT NOT NULL,
 deadline TIMESTAMPTZ NOT NULL, claimed_by TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp())`, t.rpcRequests),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS forge_conduit_rpc_pending ON %s (channel, created_at) WHERE claimed_by IS NULL`, t.rpcRequests),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, response JSON NOT NULL, expires_at TIMESTAMPTZ NOT NULL)`, t.rpcResponses),
	}
}

func migrationSchema(schema string) string {
	if schema == "" {
		schema = "public"
	}

	if !validSchema.MatchString(schema) {
		panic(fmt.Sprintf("conduit/postgres: schema %q must be a lowercase identifier", schema))
	}

	return schema
}

// migrationRelations lists every table and index Migrations creates, schema-qualified
// and quoted for to_regclass.
func migrationRelations(schema string) []string {
	t := newTables(schema)
	index := func(name string) string { return pgx.Identifier{schema, name}.Sanitize() }

	return []string{
		t.streams, t.messages, t.consumers, t.claims, t.deadLetters, t.backfills, t.instances, t.rpcRequests, t.rpcResponses,
		index("forge_conduit_messages_cursor"), index("forge_conduit_messages_age"), index("forge_conduit_rpc_pending"),
	}
}

// migrate applies Migrations under an advisory lock, so replicas starting together don't
// race each other's CREATE statements. When everything already exists it runs no DDL at
// all: CREATE INDEX IF NOT EXISTS locks the table even when it has nothing to do, and
// would queue behind every open publishing transaction.
func migrate(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	schema = migrationSchema(schema)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('forge_conduit_migrate'))`); err != nil {
		return err
	}

	// A schema created before the prune watermark has every relation but not its columns.
	var current bool
	if err := tx.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL AND
 (SELECT bool_and(to_regclass(relation) IS NOT NULL) FROM unnest($2::text[]) AS relation) AND
 (SELECT count(*) FROM pg_attribute WHERE attrelid = to_regclass($3) AND attname IN ('pruned_txid', 'pruned_seq') AND NOT attisdropped) = 2`,
		pgx.Identifier{schema}.Sanitize(), migrationRelations(schema), newTables(schema).streams).Scan(&current); err != nil {
		return fmt.Errorf("conduit/postgres: migrate: %w", err)
	}

	if current {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`); err != nil {
		return fmt.Errorf("conduit/postgres: migrate: %w", err)
	}

	for _, statement := range Migrations(schema) {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("conduit/postgres: migrate: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// Migrate creates the provider's tables. Call it after Connect, or set AutoMigrate.
func (p *Provider) Migrate(ctx context.Context) error {
	pool, err := p.db()
	if err != nil {
		return err
	}

	return migrate(ctx, pool, p.options.Schema)
}
