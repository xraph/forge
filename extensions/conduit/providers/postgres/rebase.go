package postgres

import (
	"context"
	"fmt"
)

// Rebase repairs a database restored from pg_dump, DMS or logical replication. Run it
// once on the copy before you start subscribers. The copy can start its transaction IDs
// below the ones stored in messages and cursors, and Subscribe then refuses with "consumer
// cursor is ahead of this cluster's transaction IDs".
//
// Rows written under the old cluster's counter move to transaction ID 0, so they sort
// before anything written since. Every consumer whose cursor is ahead of this cluster,
// or that has unread rows among the moved ones, goes back to the oldest retained message
// and loses its claims, so it receives its retained messages again. A StartAt "new"
// consumer whose start snapshot came from the old cluster loses that snapshot too. On a
// healthy database nothing is ahead and Rebase changes nothing.
//
// Rebase takes every consumer's delivery lock in one transaction, so with thousands of
// consumers raise max_locks_per_transaction first. It can fail on a deadlock with the
// retention sweep. A failed Rebase changes nothing, so it is safe to run again.
func (p *Provider) Rebase(ctx context.Context) error {
	pool, err := p.db()
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('forge_conduit_rebase'))`); err != nil {
		return err
	}

	// Take every consumer's delivery lock, so no take is moving a cursor while the cursors
	// are compared with this cluster's IDs. A cursor committed before then is below the
	// next statement's horizon.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`SELECT count(pg_advisory_xact_lock(hashtextextended(consumer_id, 0)))
FROM (SELECT consumer_id FROM %s ORDER BY consumer_id) AS c`, p.t.consumers)); err != nil {
		return err
	}

	// A consumer whose cursor is still a valid ID here, after a restore from a younger
	// cluster, had not read the moved rows yet. They now sort behind its cursor, so it is
	// reset with the others.
	var moved []string
	if err := tx.QueryRow(ctx, fmt.Sprintf(`WITH moved AS (
 UPDATE %s SET txid = '0'::xid8 WHERE txid > pg_snapshot_xmax(pg_current_snapshot()) RETURNING stream_key)
SELECT coalesce(array_agg(DISTINCT stream_key), '{}') FROM moved`, p.t.messages)).Scan(&moved); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`WITH reset AS (
 UPDATE %s SET cursor_txid = '0'::xid8, cursor_seq = 0, start_snapshot = NULL
 WHERE cursor_txid > pg_snapshot_xmax(pg_current_snapshot()) OR (stream_key = ANY($1) AND cursor_txid > '0'::xid8)
 RETURNING consumer_id)
DELETE FROM %s WHERE consumer_id IN (SELECT consumer_id FROM reset)`, p.t.consumers, p.t.claims), moved); err != nil {
		return err
	}

	// A StartAt "new" consumer keeps the snapshot it subscribed under. One taken on the old
	// cluster is above every ID this cluster hands out, so it would hide every new row
	// even where the cursor itself is valid here.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET start_snapshot = NULL
WHERE pg_snapshot_xmax(start_snapshot) > pg_snapshot_xmax(pg_current_snapshot())`, p.t.consumers)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
