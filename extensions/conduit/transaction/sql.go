// Package transaction coordinates PostgreSQL business writes with event delivery.
package transaction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

// Store uses the same database and transactions as the application's business records.
type Store struct{ DB *sql.DB }

// Migrate creates outbox and inbox tables. Applications control when migrations run.
func (s *Store) Migrate(ctx context.Context) error {
	if s.DB == nil {
		return errors.New("conduit/transaction: database is required")
	}

	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS forge_conduit_outbox (
 namespace TEXT NOT NULL, service_id TEXT NOT NULL, message_id TEXT NOT NULL,
 stream TEXT NOT NULL, envelope JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(namespace, service_id, message_id)
);
CREATE TABLE IF NOT EXISTS forge_conduit_inbox (
 namespace TEXT NOT NULL, service_id TEXT NOT NULL, subscription_id TEXT NOT NULL,
 message_id TEXT NOT NULL, processed_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(namespace, service_id, subscription_id, message_id)
);`)

	return err
}

// Enqueue writes a prepared envelope into the caller's business transaction.
func (s *Store) Enqueue(ctx context.Context, tx *sql.Tx, stream string, message core.Envelope) error {
	if tx == nil || message.ID == "" || stream == "" {
		return errors.New("conduit/transaction: transaction, stream and prepared message are required")
	}

	if err := message.Source.Validate(); err != nil {
		return err
	}

	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO forge_conduit_outbox(namespace,service_id,message_id,stream,envelope,created_at) VALUES($1,$2,$3,$4,$5,$6)`, message.Source.Namespace, message.Source.ServiceID, message.ID, stream, string(data), time.Now().UTC())

	return err
}

// FlushOne locks one pending row until persisted publication and commit complete.
// A crash between acknowledgement and commit can republish the same message ID.
func (s *Store) FlushOne(ctx context.Context, runtime *core.Runtime) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	identity := runtime.Identity()

	var (
		stream, id string
		raw        []byte
	)

	err = tx.QueryRowContext(ctx, `SELECT stream,message_id,envelope FROM forge_conduit_outbox WHERE namespace=$1 AND service_id=$2 ORDER BY created_at,message_id FOR UPDATE SKIP LOCKED LIMIT 1`, identity.Namespace, identity.ServiceID).Scan(&stream, &id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if !runtime.DurableStream(stream) {
		return false, core.ErrUnsupported
	}

	var message core.Envelope
	if err := json.Unmarshal(raw, &message); err != nil {
		return false, err
	}

	receipt, err := runtime.Send(ctx, stream, message)
	if err != nil {
		return false, err
	}

	if !receipt.Persisted {
		return false, core.ErrUnsupported
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM forge_conduit_outbox WHERE namespace=$1 AND service_id=$2 AND message_id=$3`, identity.Namespace, identity.ServiceID, id); err != nil {
		return false, err
	}

	return true, tx.Commit()
}

// Relay polls the outbox. Run it under the application's lifecycle context.
// OnError receives failures while retries keep the original envelope and ID.
func (s *Store) Relay(ctx context.Context, runtime *core.Runtime, onError func(error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		flushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		flushed, err := s.FlushOne(flushCtx, runtime)

		cancel()

		if err == nil && flushed {
			continue
		}

		if err != nil && onError != nil {
			onError(err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Handler commits business writes and the inbox marker in one transaction.
type Handler func(context.Context, *sql.Tx, core.Envelope, core.DeliveryInfo) error

// Inbox prevents repeated database effects for one service subscription and message.
// External side effects still need their own idempotency keys.
func (s *Store) Inbox(handler Handler) core.Handler {
	return func(ctx context.Context, message core.Envelope, info core.DeliveryInfo) error {
		if handler == nil {
			return core.Permanent(errors.New("conduit/transaction: inbox handler is required"))
		}

		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		defer func() { _ = tx.Rollback() }()

		result, err := tx.ExecContext(ctx, `INSERT INTO forge_conduit_inbox(namespace,service_id,subscription_id,message_id,processed_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, info.Destination.Namespace, info.Destination.ServiceID, info.SubscriptionID, message.ID, time.Now().UTC())
		if err != nil {
			return err
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if rows == 0 {
			return nil
		}

		if err := handler(ctx, tx, message.Clone(), info); err != nil {
			return err
		}

		return tx.Commit()
	}
}
