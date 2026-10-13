package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/xraph/forge/extensions/conduit/core"
)

// Tx is the part of a transaction PublishTx uses. pgx.Tx satisfies it, and SQLTx adapts
// a database/sql transaction.
type Tx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type sqlTx struct{ tx *sql.Tx }

// SQLTx adapts a database/sql transaction, such as one opened through pgx's stdlib driver.
func SQLTx(tx *sql.Tx) Tx { return sqlTx{tx: tx} }

func (s sqlTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	_, err := s.tx.ExecContext(ctx, query, args...)

	return pgconn.CommandTag{}, err
}

func (s sqlTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return s.tx.QueryRowContext(ctx, query, args...)
}

// Publish stores the message in its own transaction.
func (p *Provider) Publish(ctx context.Context, namespace string, cfg core.StreamConfig, msg core.Envelope) (core.Receipt, error) {
	return p.publishWithKey(ctx, namespace, cfg, msg, hash(msg.ID, msg.TargetConsumer))
}

// PublishTx stores a prepared message in your transaction. Subscribers see it, and wake
// for it, only when you commit; a rollback leaves no trace. Fill the envelope's ID, type,
// source, content type and creation time yourself: this path skips the runtime's hooks.
func (p *Provider) PublishTx(ctx context.Context, tx Tx, namespace string, cfg core.StreamConfig, msg core.Envelope) (core.Receipt, error) {
	if tx == nil {
		return core.Receipt{}, errors.New("conduit/postgres: transaction is required")
	}

	if err := core.ValidateMessageID(msg.ID); err != nil {
		return core.Receipt{}, err
	}

	if err := msg.Source.Validate(); err != nil {
		return core.Receipt{}, err
	}

	return p.publishIn(ctx, tx, namespace, cfg, msg, hash(msg.ID, msg.TargetConsumer))
}

func (p *Provider) publishWithKey(ctx context.Context, namespace string, cfg core.StreamConfig, msg core.Envelope, dedup string) (core.Receipt, error) {
	pool, err := p.db()
	if err != nil {
		return core.Receipt{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return core.Receipt{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	receipt, err := p.publishIn(ctx, tx, namespace, cfg, msg, dedup)
	if err != nil {
		return core.Receipt{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return core.Receipt{}, err
		}

		// The commit may have reached the server before the connection failed.
		return core.Receipt{}, core.ErrOutcomeUnknown
	}

	return receipt, nil
}

// publishIn inserts the row and queues the wakeup in tx. NOTIFY is delivered only after
// tx commits, so nobody wakes for a row they can't see.
func (p *Provider) publishIn(ctx context.Context, tx Tx, namespace string, cfg core.StreamConfig, msg core.Envelope, dedup string) (core.Receipt, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return core.Receipt{}, err
	}

	key := streamKey(namespace, cfg.Name)
	receipt := core.Receipt{MessageID: msg.ID, Persisted: true}

	var seq int64

	err = tx.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %s (stream_key, seq, dedup_key, message_type, target_consumer, envelope)
VALUES ($1, nextval('%s'), $2, $3, $4, $5) ON CONFLICT (stream_key, dedup_key) DO NOTHING RETURNING seq`, p.t.messages, p.sequence(key)),
		key, dedup, msg.Type, msg.TargetConsumer, string(data)).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT seq FROM %s WHERE stream_key = $1 AND dedup_key = $2`, p.t.messages), key, dedup).Scan(&seq); err != nil {
			return core.Receipt{}, classify(err)
		}

		receipt.Sequence, receipt.Duplicate = uint64(seq), true //nolint:gosec // Sequences are positive.

		return receipt, nil
	}

	if err != nil {
		return core.Receipt{}, classify(err)
	}

	if cfg.MaxMessages > 0 {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE stream_key = $1 AND seq <= $2`, p.t.messages), key, seq-cfg.MaxMessages); err != nil {
			return core.Receipt{}, err
		}
	}

	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, '')`, streamChannel(key)); err != nil {
		return core.Receipt{}, err
	}

	receipt.Sequence = uint64(seq) //nolint:gosec // Sequences are positive.

	return receipt, nil
}
