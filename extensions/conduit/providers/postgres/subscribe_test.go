package postgres

import (
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
	if err := pool.QueryRow(ctx, "SELECT seq FROM "+p.t.messages+" WHERE stream_key = $1 ORDER BY txid, seq OFFSET 9999 LIMIT 1", key).Scan(&windowEnd); err != nil {
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
