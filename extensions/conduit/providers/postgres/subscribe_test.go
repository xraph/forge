package postgres

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

// TestLargeNonMatchingBacklogIsWalkedInWindows covers a stream with a huge backlog of
// other message types: one statement that scans all of it can outrun statement_timeout
// and leave the cursor where it was forever, so a take walks at most one window.
func TestLargeNonMatchingBacklogIsWalkedInWindows(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	key := streamKey(ns, cfg.Name)

	pool, err := p.db()
	if err != nil {
		t.Fatal(err)
	}

	const others, batches = 10_050, 5

	for batch := range batches {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}

		for i := range others / batches {
			id := "other-" + strconv.Itoa(batch) + "-" + strconv.Itoa(i)
			if _, err := p.PublishTx(ctx, tx, ns, cfg, envelope(ns, id, "orders.cancelled.v1")); err != nil {
				t.Fatal(err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "wanted", placed)); err != nil {
		t.Fatal(err)
	}

	// Every row has to be below the horizon, or the window would end early.
	waitFor(t, "the horizon to pass the backlog", func() bool {
		var passed bool

		err := pool.QueryRow(ctx, "SELECT pg_snapshot_xmin(pg_current_snapshot()) > max(txid) FROM "+p.t.messages+" WHERE stream_key = $1", key).Scan(&passed)

		return err == nil && passed
	})

	var windowEnd int64
	if err := pool.QueryRow(ctx, "SELECT m.seq FROM "+p.t.messages+" m WHERE m.stream_key = $1 ORDER BY m.txid, m.seq OFFSET 9999 LIMIT 1", key).Scan(&windowEnd); err != nil {
		t.Fatal(err)
	}

	stepping, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "stepping", "all"))
	if err != nil {
		t.Fatal(err)
	}

	d, wait, err := stepping.(*subscription).take(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if d != nil {
		t.Fatalf("one take walked past its window and delivered %s", d.Message().ID)
	}

	if wait != 0 {
		t.Fatalf("a take that stopped at its window end waits %v before the next", wait)
	}

	var cursor int64
	if err := pool.QueryRow(ctx, "SELECT cursor_seq FROM "+p.t.consumers+" WHERE consumer_id = $1", stepping.(*subscription).consumer).Scan(&cursor); err != nil {
		t.Fatal(err)
	}

	if cursor != windowEnd {
		t.Fatalf("cursor at seq %d, want the window end %d", cursor, windowEnd)
	}

	picky, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "picky", "all"))
	if err != nil {
		t.Fatal(err)
	}

	if d := next(t, picky, 10*time.Second); d.Message().ID != "wanted" {
		t.Fatalf("delivered %s", d.Message().ID)
	}
}

// TestIdleSubscriberAssignsNoTransactionIDs covers an idle subscriber's polls: a take that
// finds nothing must not lock or rewrite its consumer row. Either would assign a
// transaction id on every poll and hold the cluster's horizon back for nothing.
//
// The database is shared with other tests, so the cluster's xid counter is too noisy to
// assert on. The consumer row is not: a row lock records the locker in xmax, and a
// rewrite gives the row a new xmin, so both staying put shows the takes wrote nothing.
func TestIdleSubscriberAssignsNoTransactionIDs(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{PollInterval: 100 * time.Millisecond})
	ns, cfg := newStream(t, p)

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "idle", "all"))
	if err != nil {
		t.Fatal(err)
	}

	pool, err := p.db()
	if err != nil {
		t.Fatal(err)
	}

	sub := s.(*subscription)
	version := func() string {
		t.Helper()

		var xmin, xmax string
		if err := pool.QueryRow(ctx, "SELECT xmin::text, xmax::text FROM "+p.t.consumers+" WHERE consumer_id = $1", sub.consumer).Scan(&xmin, &xmax); err != nil {
			t.Fatal(err)
		}

		return xmin + "/" + xmax
	}

	before := version()

	for range 20 {
		d, _, err := sub.take(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if d != nil {
			t.Fatalf("an idle subscription delivered %s", d.Message().ID)
		}
	}

	if after := version(); after != before {
		t.Fatalf("idle takes wrote the consumer row (xmin/xmax %s, then %s)", before, after)
	}
}

// insertAt stores a message with a chosen transaction ID, so a test can place rows on
// either side of a power of ten without running thousands of transactions.
func insertAt(t *testing.T, p *Provider, ns string, cfg core.StreamConfig, txid string, seq int64, id, messageType string) {
	t.Helper()

	data, err := json.Marshal(envelope(ns, id, messageType))
	if err != nil {
		t.Fatal(err)
	}

	pool, err := p.db()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), "INSERT INTO "+p.t.messages+" (stream_key, seq, txid, dedup_key, message_type, target_consumer, envelope) VALUES ($1, $2, $3::text::xid8, $4, $5, '', $6)",
		streamKey(ns, cfg.Name), seq, txid, id, messageType, string(data)); err != nil {
		t.Fatal(err)
	}
}

// TestTextOrderDoesNotSkipAMessage covers transaction IDs on either side of a power of
// ten. As text, 1000 sorts before 999, and a take that delivered 1000 first would move
// the cursor past 999 for good.
func TestTextOrderDoesNotSkipAMessage(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	insertAt(t, p, ns, cfg, "999", 1, "at-999", placed)
	insertAt(t, p, ns, cfg, "1000", 2, "at-1000", placed)

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "ordered", "all"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"at-999", "at-1000"} {
		d := next(t, s, 5*time.Second)
		if d.Message().ID != want {
			t.Fatalf("delivered %s, want %s", d.Message().ID, want)
		}

		if err := d.Ack(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBacklogWindowFollowsTransactionOrder covers a backlog whose transaction IDs cross a
// power of ten: each take stops at the 10,000th row in transaction order, never at a row
// picked by sorting the IDs as text.
func TestBacklogWindowFollowsTransactionOrder(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	key := streamKey(ns, cfg.Name)

	pool, err := p.db()
	if err != nil {
		t.Fatal(err)
	}

	// 30,000 rows of another type, a thousand per transaction ID from 990 to 1019, then
	// the one matching row at 1020. Every ID is far below the horizon of a cluster that
	// has run this suite.
	if _, err := pool.Exec(ctx, "INSERT INTO "+p.t.messages+` (stream_key, seq, txid, dedup_key, message_type, target_consumer, envelope)
SELECT $1, i + 1, (990 + i / 1000)::text::xid8, 'other-' || i, 'orders.cancelled.v1', '', '{}'::json FROM generate_series(0, 29999) AS i`, key); err != nil {
		t.Fatal(err)
	}

	insertAt(t, p, ns, cfg, "1020", 30_001, "wanted", placed)

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "windows", "all"))
	if err != nil {
		t.Fatal(err)
	}

	sub := s.(*subscription)
	cursor := func() (txid string, seq int64) {
		t.Helper()

		if err := pool.QueryRow(ctx, "SELECT cursor_txid::text, cursor_seq FROM "+p.t.consumers+" WHERE consumer_id = $1", sub.consumer).Scan(&txid, &seq); err != nil {
			t.Fatal(err)
		}

		return txid, seq
	}

	for _, want := range []struct {
		txid string
		seq  int64
	}{{"999", 10_000}, {"1009", 20_000}, {"1019", 30_000}} {
		d, wait, err := sub.take(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if d != nil {
			t.Fatalf("a take walked past its window and delivered %s", d.Message().ID)
		}

		if txid, seq := cursor(); txid != want.txid || seq != want.seq {
			t.Fatalf("cursor at (%s, %d), want the window end (%s, %d)", txid, seq, want.txid, want.seq)
		}

		if wait != 0 {
			t.Fatalf("a take that stopped at its window end waits %v before the next", wait)
		}
	}

	d, _, err := sub.take(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if d == nil || d.Message().ID != "wanted" {
		t.Fatalf("the fourth take delivered %v, want the matching row", d)
	}
}
