package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/xraph/forge/extensions/conduit/core"
)

// claimGrace keeps a slow acknowledgement from racing a redelivery, as in the Redis provider.
const claimGrace = 5 * time.Second

type subscription struct {
	provider *Provider
	binding  core.Binding
	key      string
	consumer string
	closed   chan struct{}
	once     sync.Once
}

// Subscribe creates the logical consumer on first use, and refuses a replica whose
// settings disagree with the group's.
func (p *Provider) Subscribe(ctx context.Context, b core.Binding) (core.Subscription, error) {
	pool, err := p.db()
	if err != nil {
		return nil, err
	}

	policy := b.Subscription
	policy.Concurrency = 0

	if policy.Mode == core.Competing {
		policy.BroadcastID = ""
	}

	data, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}

	fingerprint := hash(string(data))
	key := streamKey(b.Identity.Namespace, b.Stream.Name)
	consumer := b.ConsumerID()

	var minSeq int64
	if b.Subscription.StartAt == "sequence" {
		minSeq = int64(b.Subscription.StartSequence) //nolint:gosec // Sequences come from BIGINT columns.
	}

	// StartAt "new" keeps the snapshot it subscribed under. Rows visible in that snapshot
	// were committed before the subscription and are never delivered to it, even when an
	// unrelated open transaction holds the cursor's starting point further back.
	_, err = pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (consumer_id, stream_key, fingerprint, durable, cursor_txid, cursor_seq, min_seq, start_snapshot)
SELECT $1, $2, $3, $4,
 CASE WHEN $5::text = 'new' THEN pg_snapshot_xmin(pg_current_snapshot()) ELSE '0'::xid8 END,
 0, $6,
 CASE WHEN $5::text = 'new' THEN pg_current_snapshot() END
WHERE EXISTS (SELECT 1 FROM %s WHERE stream_key = $2)
ON CONFLICT (consumer_id) DO NOTHING`, p.t.consumers, p.t.streams),
		consumer, key, fingerprint, b.Subscription.Durable, b.Subscription.StartAt, minSeq)
	if err != nil {
		return nil, err
	}

	var stored string

	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT fingerprint FROM %s WHERE consumer_id = $1`, p.t.consumers), consumer).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, core.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	if stored != fingerprint {
		return nil, fmt.Errorf("%w: consumer %s", core.ErrConflict, consumer)
	}

	return &subscription{provider: p, binding: b, key: key, consumer: consumer, closed: make(chan struct{})}, nil
}

func (s *subscription) Next(ctx context.Context) (core.Delivery, error) {
	signal, unwatch := s.provider.watch(streamChannel(s.key))
	defer unwatch()

	for {
		select {
		case <-s.closed:
			return nil, context.Canceled
		default:
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		d, wait, err := s.take(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			return nil, err
		}

		if d != nil {
			return d, nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()

			return nil, ctx.Err()
		case <-s.closed:
			timer.Stop()

			return nil, context.Canceled
		case <-signal:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func retryable(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

func (s *subscription) take(ctx context.Context) (*delivery, time.Duration, error) {
	for attempt := 0; ; attempt++ {
		d, wait, err := s.takeOnce(ctx)
		if err != nil && retryable(err) && attempt < 3 {
			continue
		}

		return d, wait, err
	}
}

// takeOnce runs one short transaction. An advisory lock on the consumer serializes the
// group's replicas, so pause, MaxInFlight and the cursor are always checked and moved
// together. The lock, unlike a row lock, assigns no transaction id, so an idle poll
// writes nothing and does not hold the cluster's horizon back.
func (s *subscription) takeOnce(ctx context.Context) (*delivery, time.Duration, error) {
	p := s.provider

	pool, err := p.db()
	if err != nil {
		return nil, 0, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := lockConsumer(ctx, tx, s.consumer); err != nil {
		return nil, 0, err
	}

	sub := s.binding.Subscription
	maxAge := s.binding.Stream.MaxAge.Milliseconds()
	visibility := (sub.Timeout + claimGrace).Milliseconds()
	token := core.NewID()

	// Read the horizon once, after the lock, so every statement below agrees on it.
	var horizon string
	if err := tx.QueryRow(ctx, `SELECT pg_snapshot_xmin(pg_current_snapshot())::text`).Scan(&horizon); err != nil {
		return nil, 0, err
	}

	var (
		paused            bool
		cursorTxid        string
		cursorSeq, minSeq int64
		snapshot          *string
	)

	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT paused, cursor_txid::text, cursor_seq, min_seq, start_snapshot::text FROM %s WHERE consumer_id = $1`, p.t.consumers), s.consumer).
		Scan(&paused, &cursorTxid, &cursorSeq, &minSeq, &snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, core.ErrNotFound
	}

	if err != nil {
		return nil, 0, err
	}

	if paused {
		return nil, p.options.PollInterval, tx.Commit(ctx)
	}

	// Claims on messages that retention removed can never be delivered. A DELETE that
	// matches nothing assigns no transaction id.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s c WHERE c.consumer_id = $1 AND NOT EXISTS (SELECT 1 FROM %s m WHERE m.stream_key = c.stream_key AND m.seq = c.seq)`, p.t.claims, p.t.messages), s.consumer); err != nil {
		return nil, 0, err
	}

	// Redeliver a claim whose visibility ran out: a due retry, or a delivery whose worker went away.
	for {
		var (
			seq, attempt int64
			raw          []byte
			expired      bool
		)

		err = tx.QueryRow(ctx, fmt.Sprintf(`UPDATE %[1]s c SET token = $2, attempt = c.attempt + 1, visible_at = clock_timestamp() + $3::bigint * interval '1 millisecond'
FROM %[2]s m
WHERE c.consumer_id = $1 AND m.stream_key = c.stream_key AND m.seq = c.seq
 AND c.seq = (SELECT c2.seq FROM %[1]s c2 WHERE c2.consumer_id = $1 AND c2.visible_at <= clock_timestamp() ORDER BY c2.visible_at, c2.seq LIMIT 1)
RETURNING c.seq, c.attempt, m.envelope::text, ($4::bigint > 0 AND m.accepted_at <= clock_timestamp() - $4::bigint * interval '1 millisecond')`, p.t.claims, p.t.messages),
			s.consumer, token, visibility, maxAge).Scan(&seq, &attempt, &raw, &expired)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}

		if err != nil {
			return nil, 0, err
		}

		if expired {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE consumer_id = $1 AND seq = $2`, p.t.claims), s.consumer, seq); err != nil {
				return nil, 0, err
			}

			continue
		}

		d, err := s.delivery(seq, attempt, token, raw)
		if err != nil {
			return nil, 0, err
		}

		return d, 0, tx.Commit(ctx)
	}

	var inFlight int
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE consumer_id = $1`, p.t.claims), s.consumer).Scan(&inFlight); err != nil {
		return nil, 0, err
	}

	if inFlight >= sub.MaxInFlight {
		wait, err := s.wait(ctx, tx)
		if err != nil {
			return nil, 0, err
		}

		return nil, wait, tx.Commit(ctx)
	}

	// Walk the log in (txid, seq) order and stop at the oldest open transaction. Every
	// transaction below that horizon has finished, and any later commit lands above it,
	// so no row can appear behind the cursor once the cursor has passed.
	//
	// The filters run in SQL, so a backlog of other message types is skipped by the
	// database and only the one matching envelope is read.
	var (
		txid, raw string
		seq       int64
	)

	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT txid::text, seq, envelope::text FROM %s
WHERE stream_key = $1 AND (txid, seq) > ($2::text::xid8, $3) AND txid < $4::text::xid8
 AND message_type = $5 AND (target_consumer = '' OR target_consumer = $6) AND seq >= $7
 AND ($8::bigint = 0 OR accepted_at > clock_timestamp() - $8::bigint * interval '1 millisecond')
 AND ($9::text IS NULL OR NOT pg_visible_in_snapshot(txid, $9::text::pg_snapshot))
ORDER BY txid, seq LIMIT 1`, p.t.messages), s.key, cursorTxid, cursorSeq, horizon, sub.MessageType, s.consumer, minSeq, maxAge, snapshot).
		Scan(&txid, &seq, &raw)

	switch {
	case err == nil:
		found, err := s.delivery(seq, 1, token, []byte(raw))
		if err != nil {
			return nil, 0, err
		}

		if err := s.moveCursor(ctx, tx, txid, seq); err != nil {
			return nil, 0, err
		}

		if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (consumer_id, seq, stream_key, token, attempt, visible_at)
VALUES ($1, $2, $3, $4, 1, clock_timestamp() + $5::bigint * interval '1 millisecond')`, p.t.claims), s.consumer, seq, s.key, token, visibility); err != nil {
			return nil, 0, err
		}

		return found, 0, tx.Commit(ctx)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, 0, err
	}

	// Nothing matches below the horizon, so step over everything that was scanned.
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT txid::text, seq FROM %s
WHERE stream_key = $1 AND (txid, seq) > ($2::text::xid8, $3) AND txid < $4::text::xid8
ORDER BY txid DESC, seq DESC LIMIT 1`, p.t.messages), s.key, cursorTxid, cursorSeq, horizon).Scan(&txid, &seq)

	switch {
	case err == nil:
		if err := s.moveCursor(ctx, tx, txid, seq); err != nil {
			return nil, 0, err
		}

		cursorTxid, cursorSeq = txid, seq
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, 0, err
	}

	wait, err := s.wait(ctx, tx)
	if err != nil {
		return nil, 0, err
	}

	// Rows committed above the horizon become deliverable when an unrelated transaction
	// ends, and that sends no notification, so look again soon.
	var pending bool
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE stream_key = $1 AND (txid, seq) > ($2::text::xid8, $3) AND txid >= $4::text::xid8)`, p.t.messages), s.key, cursorTxid, cursorSeq, horizon).Scan(&pending); err != nil {
		return nil, 0, err
	}

	if pending {
		wait = min(wait, 50*time.Millisecond)
	}

	return nil, wait, tx.Commit(ctx)
}

func (s *subscription) moveCursor(ctx context.Context, tx pgx.Tx, txid string, seq int64) error {
	_, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET cursor_txid = $2::text::xid8, cursor_seq = $3 WHERE consumer_id = $1`, s.provider.t.consumers), s.consumer, txid, seq)

	return err
}

// lockConsumer serializes everything that changes one consumer's state until the
// transaction ends. It assigns no transaction id.
func lockConsumer(ctx context.Context, tx pgx.Tx, consumer string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, consumer)

	return err
}

// wait is how long to sleep before trying again: until the next claim becomes visible,
// and never longer than PollInterval.
func (s *subscription) wait(ctx context.Context, tx pgx.Tx) (time.Duration, error) {
	var ms *int64

	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT CEIL(EXTRACT(EPOCH FROM min(visible_at) - clock_timestamp()) * 1000)::bigint FROM %s WHERE consumer_id = $1`, s.provider.t.claims), s.consumer).Scan(&ms)
	if err != nil {
		return 0, err
	}

	wait := s.provider.options.PollInterval
	if ms != nil {
		wait = min(wait, max(5*time.Millisecond, time.Duration(*ms)*time.Millisecond))
	}

	return wait, nil
}

func (s *subscription) delivery(seq, attempt int64, token string, raw []byte) (*delivery, error) {
	var envelope core.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, errors.New("conduit/postgres: invalid retained envelope")
	}

	return &delivery{sub: s, seq: seq, attempt: uint64(attempt), token: token, envelope: envelope}, nil //nolint:gosec // Attempts are positive.
}

// Close stops this subscription. A non-durable broadcast consumer is removed with its claims.
func (s *subscription) Close(ctx context.Context) error {
	s.once.Do(func() { close(s.closed) })

	if s.binding.Subscription.Mode != core.Broadcast || s.binding.Subscription.Durable {
		return nil
	}

	pool, err := s.provider.db()
	if err != nil {
		return err
	}

	// Take the same lock as takeOnce, then delete the consumer before its claims, so a
	// take in flight cannot add a claim after the cleanup.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := lockConsumer(ctx, tx, s.consumer); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE consumer_id = $1`, s.provider.t.consumers), s.consumer); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE consumer_id = $1`, s.provider.t.claims), s.consumer); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

type delivery struct {
	sub      *subscription
	seq      int64
	attempt  uint64
	token    string
	envelope core.Envelope
}

func (d *delivery) Message() core.Envelope { return d.envelope.Clone() }

func (d *delivery) Info() core.DeliveryInfo {
	b := d.sub.binding

	return core.DeliveryInfo{Stream: b.Stream.Name, ConsumerID: b.ConsumerID(), SubscriptionID: b.Subscription.ID, Destination: b.Identity, Mode: b.Subscription.Mode, Attempt: d.attempt, Sequence: uint64(d.seq)} //nolint:gosec // Sequences are positive.
}

func (d *delivery) Ack(ctx context.Context) error {
	return d.settle(ctx, `DELETE FROM %s WHERE consumer_id = $1 AND seq = $2 AND token = $3`)
}

func (d *delivery) Reject(ctx context.Context) error { return d.Ack(ctx) }

func (d *delivery) Retry(ctx context.Context, delay time.Duration) error {
	return d.settle(ctx, `UPDATE %s SET token = '', visible_at = clock_timestamp() + $4::bigint * interval '1 millisecond' WHERE consumer_id = $1 AND seq = $2 AND token = $3`, delay.Milliseconds())
}

// settle changes the claim only if this delivery still holds it.
func (d *delivery) settle(ctx context.Context, query string, extra ...any) error {
	pool, err := d.sub.provider.db()
	if err != nil {
		return err
	}

	tag, err := pool.Exec(ctx, fmt.Sprintf(query, d.sub.provider.t.claims), append([]any{d.sub.consumer, d.seq, d.token}, extra...)...)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: stale delivery", core.ErrConflict)
	}

	return nil
}
