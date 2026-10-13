package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xraph/forge/extensions/conduit/core"
)

// InspectConsumer counts matching messages after the cursor, open claims and redeliveries.
func (p *Provider) InspectConsumer(ctx context.Context, binding core.Binding) (core.ConsumerInfo, error) {
	pool, err := p.db()
	if err != nil {
		return core.ConsumerInfo{}, err
	}

	consumer := binding.ConsumerID()

	var (
		paused            bool
		cursorTxid        string
		cursorSeq, minSeq int64
		snapshot          *string
	)

	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT paused, cursor_txid::text, cursor_seq, min_seq, start_snapshot::text FROM %s WHERE consumer_id = $1`, p.t.consumers), consumer).
		Scan(&paused, &cursorTxid, &cursorSeq, &minSeq, &snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ConsumerInfo{}, core.ErrNotFound
	}

	if err != nil {
		return core.ConsumerInfo{}, err
	}

	var pending int64

	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s
WHERE stream_key = $1 AND (txid, seq) > ($2::text::xid8, $3) AND seq >= $4 AND message_type = $5
 AND (target_consumer = '' OR target_consumer = $6)
 AND ($7::text IS NULL OR NOT pg_visible_in_snapshot(txid, $7::text::pg_snapshot))
 AND ($8::bigint = 0 OR accepted_at > clock_timestamp() - $8::bigint * interval '1 millisecond')`, p.t.messages),
		streamKey(binding.Identity.Namespace, binding.Stream.Name), cursorTxid, cursorSeq, minSeq, binding.Subscription.MessageType, consumer, snapshot, binding.Stream.MaxAge.Milliseconds()).Scan(&pending)
	if err != nil {
		return core.ConsumerInfo{}, err
	}

	var ackPending, redelivered int64
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*), count(*) FILTER (WHERE attempt > 1) FROM %s WHERE consumer_id = $1`, p.t.claims), consumer).Scan(&ackPending, &redelivered); err != nil {
		return core.ConsumerInfo{}, err
	}

	return core.ConsumerInfo{
		Subscription: binding.Subscription, ConsumerID: consumer, Paused: paused,
		Pending: uint64(pending), AckPending: uint64(ackPending), Redelivered: uint64(redelivered), //nolint:gosec // Counts are nonnegative.
	}, nil
}

// PauseConsumer holds or releases every replica of the logical consumer. A replica in the
// middle of taking a message finishes first, because both sides take the consumer's
// advisory lock, so a pause lands between takes.
func (p *Provider) PauseConsumer(ctx context.Context, binding core.Binding, paused bool) error {
	pool, err := p.db()
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := lockConsumer(ctx, tx, binding.ConsumerID()); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET paused = $2 WHERE consumer_id = $1`, p.t.consumers), binding.ConsumerID(), paused)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}

	// Waiting replicas wake and see the change straight away.
	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, '')`, streamChannel(streamKey(binding.Identity.Namespace, binding.Stream.Name))); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// RunBackfill republishes a retained range to one consumer, resumable by operation ID.
func (p *Provider) RunBackfill(ctx context.Context, binding core.Binding, in core.BackfillInput) (core.Backfill, error) {
	if err := in.Validate(); err != nil {
		return core.Backfill{}, err
	}

	pool, err := p.db()
	if err != nil {
		return core.Backfill{}, err
	}

	ns, service := binding.Identity.Namespace, binding.Identity.ServiceID
	initial := core.Backfill{
		MessageType: binding.Subscription.MessageType, Input: in, ConsumerID: binding.ConsumerID(), Stream: binding.Stream.Name,
		Provider: binding.Stream.Provider, Next: in.Start, State: "running", UpdatedAt: time.Now().UTC(), Persisted: true,
	}

	data, err := json.Marshal(initial)
	if err != nil {
		return core.Backfill{}, err
	}

	job, err := p.claimBackfill(ctx, ns, service, initial, string(data))
	if err != nil {
		return job, err
	}

	if job.State == "complete" {
		return job, nil
	}

	key := streamKey(ns, binding.Stream.Name)

	read := func(ctx context.Context, sequence uint64) (core.Envelope, error) {
		var raw []byte

		err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT envelope::text FROM %s WHERE stream_key = $1 AND seq = $2`, p.t.messages), key, int64(sequence)).Scan(&raw) //nolint:gosec // Backfill ranges are validated.
		if errors.Is(err, pgx.ErrNoRows) {
			return core.Envelope{}, core.ErrNotFound
		}

		if err != nil {
			return core.Envelope{}, err
		}

		var msg core.Envelope
		if err := json.Unmarshal(raw, &msg); err != nil {
			return core.Envelope{}, errors.New("conduit/postgres: invalid retained envelope")
		}

		return msg, nil
	}

	publish := func(ctx context.Context, msg core.Envelope, id string) error {
		_, err := p.publishWithKey(ctx, ns, binding.Stream, msg, hash("backfill", id))

		return err
	}

	save := func(ctx context.Context, job core.Backfill) error {
		data, err := json.Marshal(job)
		if err != nil {
			return err
		}

		_, err = pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET job = $4, claim_at = CASE WHEN $5::boolean THEN NULL ELSE claim_at END
WHERE namespace = $1 AND service_id = $2 AND id = $3`, p.t.backfills), ns, service, in.ID, string(data), job.State != "running")

		return err
	}

	return core.ExecuteBackfill(ctx, binding, job, read, publish, save)
}

// claimBackfill records the operation on first use and claims it for this run. It checks,
// in order, that the request matches the stored operation, whether it is already complete,
// and whether another run holds it (a claim younger than a minute). Only then does it stamp
// the claim, so a conflicting request never locks out the operation's real owner.
func (p *Provider) claimBackfill(ctx context.Context, ns, service string, want core.Backfill, initial string) (core.Backfill, error) {
	id := want.Input.ID

	pool, err := p.db()
	if err != nil {
		return core.Backfill{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return core.Backfill{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (namespace, service_id, id, job) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, p.t.backfills), ns, service, id, initial); err != nil {
		return core.Backfill{}, err
	}

	var (
		raw  []byte
		busy bool
		job  core.Backfill
	)

	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT job::text, COALESCE(claim_at > clock_timestamp() - interval '1 minute', false) FROM %s
WHERE namespace = $1 AND service_id = $2 AND id = $3 FOR UPDATE`, p.t.backfills), ns, service, id).Scan(&raw, &busy)
	if err != nil {
		return core.Backfill{}, err
	}

	if err := json.Unmarshal(raw, &job); err != nil {
		return core.Backfill{}, err
	}

	if job.Input != want.Input || job.ConsumerID != want.ConsumerID || job.Stream != want.Stream || job.MessageType != want.MessageType {
		return job, core.ErrConflict
	}

	if job.State == "complete" {
		return job, nil
	}

	if busy {
		return job, core.ErrConflict
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET claim_at = clock_timestamp() WHERE namespace = $1 AND service_id = $2 AND id = $3`, p.t.backfills), ns, service, id); err != nil {
		return core.Backfill{}, err
	}

	return job, tx.Commit(ctx)
}

// ListBackfills returns one service's operations ordered by ID.
func (p *Provider) ListBackfills(ctx context.Context, identity core.Identity, cursor string, limit int) ([]core.Backfill, string, error) {
	if limit < 1 || limit > 100 {
		return nil, "", core.ErrConflict
	}

	pool, err := p.db()
	if err != nil {
		return nil, "", err
	}

	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT job::text FROM %s WHERE namespace = $1 AND service_id = $2 AND id > $3 ORDER BY id LIMIT $4`, p.t.backfills),
		identity.Namespace, identity.ServiceID, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	jobs := []core.Backfill{}

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, "", err
		}

		var job core.Backfill
		if err := json.Unmarshal(raw, &job); err != nil {
			return nil, "", err
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""

	if len(jobs) > limit {
		jobs = jobs[:limit]
		next = jobs[len(jobs)-1].Input.ID
	}

	return jobs, next, nil
}

// VisibilityLag is how long the oldest open transaction holding a transaction ID has been
// running anywhere in the cluster. Delivery can't pass it, so a large value names the
// transaction you need to find.
func (p *Provider) VisibilityLag(ctx context.Context) (time.Duration, error) {
	pool, err := p.db()
	if err != nil {
		return 0, err
	}

	var ms int64

	err = pool.QueryRow(ctx, `SELECT COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(xact_start)) * 1000, 0)::bigint
FROM pg_stat_activity WHERE backend_xid IS NOT NULL`).Scan(&ms)

	return time.Duration(ms) * time.Millisecond, err
}

var _ core.Operations = (*Provider)(nil)
