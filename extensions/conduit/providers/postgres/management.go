package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xraph/forge/extensions/conduit/core"
)

// Inspect reports the retained messages that haven't aged out, and the stream's consumers.
func (p *Provider) Inspect(ctx context.Context, namespace string, cfg core.StreamConfig) (core.StreamInfo, error) {
	pool, err := p.db()
	if err != nil {
		return core.StreamInfo{}, err
	}

	key := streamKey(namespace, cfg.Name)

	var exists bool
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE stream_key = $1)`, p.t.streams), key).Scan(&exists); err != nil {
		return core.StreamInfo{}, err
	}

	if !exists {
		return core.StreamInfo{}, core.ErrNotFound
	}

	var messages, bytes int64

	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*), COALESCE(sum(octet_length(envelope::text)), 0)::bigint FROM %s
WHERE stream_key = $1 AND ($2::bigint = 0 OR accepted_at > clock_timestamp() - $2::bigint * interval '1 millisecond')`, p.t.messages), key, cfg.MaxAge.Milliseconds()).Scan(&messages, &bytes)
	if err != nil {
		return core.StreamInfo{}, err
	}

	var consumers int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE stream_key = $1`, p.t.consumers), key).Scan(&consumers); err != nil {
		return core.StreamInfo{}, err
	}

	return core.StreamInfo{Config: cfg, Messages: uint64(messages), Bytes: uint64(bytes), Consumers: consumers}, nil //nolint:gosec // Counts are nonnegative.
}

// StoreDeadLetter keeps the first failure recorded for this ID.
func (p *Provider) StoreDeadLetter(ctx context.Context, namespace string, letter core.DeadLetter) error {
	pool, err := p.db()
	if err != nil {
		return err
	}

	data, err := json.Marshal(letter)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (namespace, service_id, subscription_id, id, letter) VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, p.t.deadLetters),
		namespace, letter.Delivery.Destination.ServiceID, letter.Delivery.SubscriptionID, letter.ID, string(data))

	return err
}

// ListDeadLetters pages one service's failures by ID.
func (p *Provider) ListDeadLetters(ctx context.Context, identity core.Identity, subscription, cursor string, limit int) ([]core.DeadLetter, string, error) {
	if limit < 1 || limit > 100 {
		return nil, "", core.ErrConflict
	}

	pool, err := p.db()
	if err != nil {
		return nil, "", err
	}

	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT letter::text, replayed FROM %s
WHERE namespace = $1 AND service_id = $2 AND ($3 = '' OR subscription_id = $3) AND id > $4 ORDER BY id LIMIT $5`, p.t.deadLetters),
		identity.Namespace, identity.ServiceID, subscription, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	letters := make([]core.DeadLetter, 0, limit+1)

	for rows.Next() {
		var (
			raw      []byte
			replayed bool
			letter   core.DeadLetter
		)

		if err := rows.Scan(&raw, &replayed); err != nil {
			return nil, "", err
		}

		if err := json.Unmarshal(raw, &letter); err != nil {
			return nil, "", err
		}

		letter.Replayed = replayed
		letters = append(letters, letter)
	}

	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""

	if len(letters) > limit {
		letters = letters[:limit]
		next = letters[len(letters)-1].ID
	}

	return letters, next, nil
}

// ReplayDeadLetter republishes to the original consumer and marks the letter replayed in
// the same transaction, so a replay happens once.
func (p *Provider) ReplayDeadLetter(ctx context.Context, identity core.Identity, subscription, id string) (core.Receipt, error) {
	pool, err := p.db()
	if err != nil {
		return core.Receipt{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return core.Receipt{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var (
		raw      []byte
		replayed bool
	)

	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT letter::text, replayed FROM %s WHERE namespace = $1 AND service_id = $2 AND subscription_id = $3 AND id = $4 FOR UPDATE`, p.t.deadLetters),
		identity.Namespace, identity.ServiceID, subscription, id).Scan(&raw, &replayed)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.Receipt{}, core.ErrNotFound
	}

	if err != nil {
		return core.Receipt{}, err
	}

	if replayed {
		return core.Receipt{}, core.ErrConflict
	}

	var letter core.DeadLetter
	if err := json.Unmarshal(raw, &letter); err != nil {
		return core.Receipt{}, err
	}

	var config []byte
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT config::text FROM %s WHERE stream_key = $1`, p.t.streams), streamKey(identity.Namespace, letter.Delivery.Stream)).Scan(&config); err != nil {
		return core.Receipt{}, classify(err)
	}

	var cfg core.StreamConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return core.Receipt{}, err
	}

	msg := letter.Message.Clone()
	msg.TargetConsumer = letter.Delivery.ConsumerID

	receipt, err := p.publishIn(ctx, tx, identity.Namespace, cfg, msg, hash(msg.ID, msg.TargetConsumer))
	if err != nil {
		return core.Receipt{}, err
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET replayed = true WHERE namespace = $1 AND service_id = $2 AND subscription_id = $3 AND id = $4`, p.t.deadLetters),
		identity.Namespace, identity.ServiceID, subscription, id); err != nil {
		return core.Receipt{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return core.Receipt{}, err
		}

		return core.Receipt{}, core.ErrOutcomeUnknown
	}

	return receipt, nil
}

// maintain sweeps on RetentionInterval until ctx ends.
func (p *Provider) maintain(ctx context.Context) {
	ticker := time.NewTicker(p.options.RetentionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		pool, err := p.db()
		if err != nil {
			continue
		}

		work, cancel := context.WithTimeout(ctx, p.options.RetentionInterval)
		p.sweep(work, pool)
		cancel()
	}
}

// sweep deletes aged-out messages in batches, the claims they leave behind, and expired
// leases and RPC rows. A failed statement only delays cleanup until the next sweep.
func (p *Provider) sweep(ctx context.Context, pool *pgxpool.Pool) {
	statements := []string{
		fmt.Sprintf(`DELETE FROM %[1]s WHERE ctid IN (SELECT m.ctid FROM %[1]s m JOIN %[2]s s ON s.stream_key = m.stream_key
 WHERE s.max_age_ms > 0 AND m.accepted_at <= clock_timestamp() - s.max_age_ms * interval '1 millisecond' LIMIT 1000)`, p.t.messages, p.t.streams),
		fmt.Sprintf(`DELETE FROM %s c WHERE NOT EXISTS (SELECT 1 FROM %s m WHERE m.stream_key = c.stream_key AND m.seq = c.seq)`, p.t.claims, p.t.messages),
		fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= clock_timestamp()`, p.t.instances),
		fmt.Sprintf(`DELETE FROM %s WHERE deadline <= clock_timestamp()`, p.t.rpcRequests),
		fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= clock_timestamp()`, p.t.rpcResponses),
	}

	for _, statement := range statements {
		_, _ = pool.Exec(ctx, statement)
	}
}

var _ core.Provider = (*Provider)(nil)
var _ core.Management = (*Provider)(nil)
