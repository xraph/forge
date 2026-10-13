package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

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

// ReadAfter returns committed messages after a position, in the order a
// subscription would deliver them: (txid, seq), and only below the visibility
// horizon, so a transaction still open can't later appear behind the result.
//
// The rows always come back. resumable is false when after isn't "0:0" and no
// retained message is at or before it: messages after it may already have
// been removed, so a caller that must not miss one resynchronises instead of
// trusting the result. Each returned envelope carries its PositionHeader.
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

	anchored := after == "0:0"

	if !anchored {
		err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s m WHERE m.stream_key = $1
 AND (m.txid, m.seq) <= ($2::text::xid8, $3)
 AND ($4::bigint = 0 OR m.accepted_at > clock_timestamp() - $4::bigint * interval '1 millisecond'))`, p.t.messages),
			key, txid, seq, maxAge).Scan(&anchored)
		if err != nil {
			return nil, false, err
		}
	}

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

	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT m.txid::text AS txid_text, m.seq, m.envelope::text FROM %s m
WHERE m.stream_key = $1 AND (m.txid, m.seq) > ($2::text::xid8, $3)
 AND m.txid < pg_snapshot_xmin(pg_current_snapshot())
 AND ($4::bigint = 0 OR m.accepted_at > clock_timestamp() - $4::bigint * interval '1 millisecond')
 AND ($5::text = '' OR m.envelope::jsonb ->> 'key' = $5::text)
 AND COALESCE(m.envelope::jsonb -> 'headers', '{}'::jsonb) @> $6::jsonb
 AND (cardinality($7::text[]) = 0 OR m.message_type = ANY($7::text[]))
ORDER BY m.txid, m.seq LIMIT $8`, p.t.messages),
		key, txid, seq, maxAge, filter.Key, string(headerJSON), types, limit)
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

	return envelopes, anchored, rows.Err()
}

// Head returns the newest committed position below the horizon: the point a
// reader that wants only what happens from now on starts after. An empty
// stream's head is "0:0".
func (p *Provider) Head(ctx context.Context, namespace string, stream core.StreamConfig) (string, error) {
	pool, err := p.db()
	if err != nil {
		return "", err
	}

	var (
		txid string
		seq  int64
	)

	err = pool.QueryRow(ctx, fmt.Sprintf(`SELECT m.txid::text AS txid_text, m.seq FROM %s m
WHERE m.stream_key = $1 AND m.txid < pg_snapshot_xmin(pg_current_snapshot())
ORDER BY m.txid DESC, m.seq DESC LIMIT 1`, p.t.messages), streamKey(namespace, stream.Name)).Scan(&txid, &seq)
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
		return "", 0, fmt.Errorf("%w: invalid position %q", core.ErrConflict, s)
	}

	if _, err := strconv.ParseUint(txid, 10, 64); err != nil {
		return "", 0, fmt.Errorf("%w: invalid position %q", core.ErrConflict, s)
	}

	seq, err := strconv.ParseInt(seqText, 10, 64)
	if err != nil || seq < 0 {
		return "", 0, fmt.Errorf("%w: invalid position %q", core.ErrConflict, s)
	}

	return txid, seq, nil
}
