package transaction_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
	"github.com/xraph/forge/extensions/conduit/transaction"
)

func TestPostgresOutboxAndInbox(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
	if dsn == "" {
		if os.Getenv("CONDUIT_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("CONDUIT_TEST_POSTGRES is required by the integration gate")
		}

		t.Skip("set CONDUIT_TEST_POSTGRES to run PostgreSQL transaction integration")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	store := &transaction.Store{DB: db}
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(t.Context(), `CREATE TABLE IF NOT EXISTS conduit_test_effects(namespace TEXT NOT NULL,message_id TEXT NOT NULL,PRIMARY KEY(namespace,message_id))`); err != nil {
		t.Fatal(err)
	}

	namespace := core.NewID()

	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}

	go srv.Start()

	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("broker not ready")
	}

	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })

	r, err := core.New(core.Config{Identity: core.Identity{Namespace: namespace, ServiceID: "orders", InstanceID: "one"}, Streams: map[string]core.StreamConfig{"orders": {Provider: "nats", Subjects: []string{"orders.>"}}}}, core.WithProvider("nats", jetstream.New(jetstream.Options{URL: srv.ClientURL()})))
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = r.Stop(context.WithoutCancel(t.Context())) })

	msg, err := r.Prepare(t.Context(), "orders", core.Envelope{Type: "orders.placed.v1", ContentType: "application/json", Data: json.RawMessage(`{"id":"42"}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Enqueue(t.Context(), tx, "orders", msg); err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if sent, err := store.FlushOne(t.Context(), r); err != nil || sent {
		t.Fatalf("rolled back message published: %v", err)
	}

	tx, err = db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Enqueue(t.Context(), tx, "orders", msg); err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if sent, err := store.FlushOne(t.Context(), r); err != nil || !sent {
		t.Fatalf("committed message not published: %v", err)
	}

	if sent, err := store.FlushOne(t.Context(), r); err != nil || sent {
		t.Fatalf("outbox row not removed: %v", err)
	}

	var effects atomic.Int32

	handler := store.Inbox(func(ctx context.Context, tx *sql.Tx, e core.Envelope, info core.DeliveryInfo) error {
		effects.Add(1)

		_, err := tx.ExecContext(ctx, `INSERT INTO conduit_test_effects(namespace,message_id) VALUES($1,$2)`, info.Destination.Namespace, e.ID)

		return err
	})

	info := core.DeliveryInfo{Destination: core.Identity{Namespace: namespace, ServiceID: "billing", InstanceID: "one"}, SubscriptionID: "process"}
	if err := handler(t.Context(), msg, info); err != nil {
		t.Fatal(err)
	}

	info.Destination.InstanceID = "two"
	if err := handler(t.Context(), msg, info); err != nil {
		t.Fatal(err)
	}

	if effects.Load() != 1 {
		t.Fatal("replica duplicate caused a second effect")
	}

	rollbackMsg := msg.Clone()
	rollbackMsg.ID = "rollback"

	fail := store.Inbox(func(ctx context.Context, tx *sql.Tx, message core.Envelope, _ core.DeliveryInfo) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conduit_test_effects(namespace,message_id) VALUES($1,$2)`, namespace, message.ID); err != nil {
			return err
		}

		return errors.New("business write failed")
	})
	if err := fail(t.Context(), rollbackMsg, info); err == nil {
		t.Fatal("failure was hidden")
	}

	if err := handler(t.Context(), rollbackMsg, info); err != nil {
		t.Fatal(err)
	}

	if effects.Load() != 2 {
		t.Fatal("rolled back inbox marker prevented retry")
	}

	var committed int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM conduit_test_effects WHERE namespace=$1`, namespace).Scan(&committed); err != nil {
		t.Fatal(err)
	}

	if committed != 2 {
		t.Fatalf("expected two committed effects, got %d", committed)
	}
}
