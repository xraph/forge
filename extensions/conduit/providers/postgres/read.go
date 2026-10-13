package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/xraph/forge/extensions/conduit/core"
)

// ReadFilter narrows ReadAfter. A zero field matches everything.
type ReadFilter struct {
	// Key matches the envelope's Key.
	Key string
	// Headers must all be present on the envelope with these values.
	Headers map[string]string
	// Types matches any of these message types.
	Types []string
	// Limit caps the result. Default 100, at most 10,000.
	Limit int
}

// ErrInvalidPosition is returned for a position that isn't "<txid>:<seq>".
var ErrInvalidPosition = errors.New("conduit/postgres: invalid position")

// ReadAfter returns committed messages after a position, in the order a
// subscription would deliver them: (txid, seq), and only below the visibility
// horizon, so a transaction still open can't later appear behind the result.
//
// The rows always come back. resumable is false when retention may have removed
// a message after the given position: the stream's prune watermark, the highest
// (txid, seq) any retention path has deleted, is above it. A caller that must
// not miss a message resynchronises instead of trusting the result. A message
// past the stream's MaxAge that the sweeper hasn't removed yet is hidden from
// the rows, so it counts as pruned too, and a position in the future (a
// transaction that hasn't started) is never resumable. "0:0" is resumable exactly when nothing
// was ever pruned. The watermark and the rows
// are read in one snapshot, so a prune can't land between them. Each returned
// envelope carries its PositionHeader.
func (p *Provider) ReadAfter(ctx context.Context, namespace string, stream core.StreamConfig, after string, filter ReadFilter) ([]core.Envelope, bool, error) {
	pool, err := p.db()
	if err != nil {
		return nil, false, err
	}

	txid, seq, err := parsePosition(after)
	if err != nil {
		return nil, false, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	limit = min(limit, 10000)

	key := streamKey(namespace, stream.Name)
	maxAge := stream.MaxAge.Milliseconds()

	headers := filter.Headers
	if headers == nil {
		headers = map[string]string{}
	}

	headerJSON, err := json.Marshal(headers)
	if err != nil {
		return nil, false, err
	}

	types := filter.Types
	if types == nil {
		types = []string{}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// One cutoff for both statements, so a message can't age out between the check and
	// the read. A message past MaxAge that the sweeper hasn't removed yet is hidden from
	// the result, so it counts as pruned too.
	//
	// A position at or past the snapshot's xmax names a transaction that hasn't started.
	// Nothing legitimate hands one out, but a client can hold one from before a Rebase,
	// so it is not resumable.
	var (
		cutoff time.Time
		future bool
	)

	if err := tx.QueryRow(ctx, `SELECT clock_timestamp() - $1::bigint * interval '1 millisecond', $2::text::xid8 >= pg_snapshot_xmax(pg_current_snapshot())`,
		maxAge, txid).Scan(&cutoff, &future); err != nil {
		return nil, false, err
	}

	aged := maxAge > 0

	// Positions compare as (xid8, bigint) pairs in SQL, never as strings.
	resumable := true

	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT (s.pruned_txid IS NULL OR ($2::text::xid8, $3::bigint) >= (s.pruned_txid, s.pruned_seq))
 AND NOT ($4::bool AND EXISTS (SELECT 1 FROM %s m WHERE m.stream_key = $1 AND (m.txid, m.seq) > ($2::text::xid8, $3::bigint) AND m.accepted_at <= $5::timestamptz))
FROM %s s WHERE s.stream_key = $1`, p.t.messages, p.t.streams), key, txid, seq, aged, cutoff).Scan(&resumable)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}

	resumable = resumable && !future

	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT m.txid::text AS txid_text, m.seq, m.envelope::text FROM %s m
WHERE m.stream_key = $1 AND (m.txid, m.seq) > ($2::text::xid8, $3)
 AND m.txid < pg_snapshot_xmin(pg_current_snapshot())
 AND (NOT $4::bool OR m.accepted_at > $9::timestamptz)
 AND ($5::text = '' OR m.envelope::jsonb ->> 'key' = $5::text)
 AND COALESCE(m.envelope::jsonb -> 'headers', '{}'::jsonb) @> $6::jsonb
 AND (cardinality($7::text[]) = 0 OR m.message_type = ANY($7::text[]))
ORDER BY m.txid, m.seq LIMIT $8`, p.t.messages),
		key, txid, seq, aged, filter.Key, string(headerJSON), types, limit, cutoff)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	envelopes := make([]core.Envelope, 0)

	for rows.Next() {
		var (
			rowTxid string
			rowSeq  int64
			raw     string
		)

		if err := rows.Scan(&rowTxid, &rowSeq, &raw); err != nil {
			return nil, false, err
		}

		var envelope core.Envelope
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			return nil, false, errors.New("conduit/postgres: invalid retained envelope")
		}

		if envelope.Headers == nil {
			envelope.Headers = map[string]string{}
		}

		envelope.Headers[PositionHeader] = position(rowTxid, rowSeq)
		envelopes = append(envelopes, envelope)
	}

	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	return envelopes, resumable, nil
}

// Head returns the newest position a reader can start after and stay resumable:
// the greater of the newest committed message below the visibility horizon and the
// stream's prune watermark. A reader that wants only what happens from now on
// starts after it. A stream with no messages and nothing ever pruned has head "0:0".
func (p *Provider) Head(ctx context.Context, namespace string, stream core.StreamConfig) (string, error) {
	pool, err := p.db()
	if err != nil {
		return "", err
	}

	var (
		txid string
		seq  int64
	)

	// One statement, so the newest message and the watermark share a snapshot.
	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT h.txid::text AS txid_text, h.seq FROM (
 (SELECT m.txid, m.seq FROM %s m WHERE m.stream_key = $1 AND m.txid < pg_snapshot_xmin(pg_current_snapshot())
  ORDER BY m.txid DESC, m.seq DESC LIMIT 1)
 UNION ALL
 (SELECT s.pruned_txid, s.pruned_seq FROM %s s WHERE s.stream_key = $1 AND s.pruned_txid IS NOT NULL)
) h ORDER BY h.txid DESC, h.seq DESC LIMIT 1`, p.t.messages, p.t.streams), streamKey(namespace, stream.Name)).Scan(&txid, &seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return "0:0", nil
	}

	if err != nil {
		return "", err
	}

	return position(txid, seq), nil
}

// parsePosition reads "<txid>:<seq>". "0:0" is the position before everything.
func parsePosition(s string) (string, int64, error) {
	txid, seqText, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, fmt.Errorf("%w %q", ErrInvalidPosition, s)
	}

	if _, err := strconv.ParseUint(txid, 10, 64); err != nil {
		return "", 0, fmt.Errorf("%w %q", ErrInvalidPosition, s)
	}

	seq, err := strconv.ParseInt(seqText, 10, 64)
	if err != nil || seq < 0 {
		return "", 0, fmt.Errorf("%w %q", ErrInvalidPosition, s)
	}

	return txid, seq, nil
}
