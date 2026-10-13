package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

// restore moves a stream's rows and the given consumers' cursors a million transaction
// IDs past the cluster's counter, the way a dump restored into a younger cluster leaves
// them.
func restore(t *testing.T, p *Provider, key string, consumers ...string) {
	t.Helper()

	pool, err := p.db()
	if err != nil {
		t.Fatal(err)
	}

	ahead := `(pg_snapshot_xmax(pg_current_snapshot())::text::bigint + 1000000 + %s)::text::xid8`

	if _, err := pool.Exec(t.Context(), fmt.Sprintf(`UPDATE %s SET txid = `+fmt.Sprintf(ahead, "seq")+` WHERE stream_key = $1`, p.t.messages), key); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), fmt.Sprintf(`UPDATE %s SET cursor_txid = `+fmt.Sprintf(ahead, "cursor_seq")+` WHERE consumer_id = ANY($1)`, p.t.consumers), consumers); err != nil {
		t.Fatal(err)
	}
}

type consumerState struct {
	cursor string
	seq    int64
	claims int
}

func stateOf(t *testing.T, p *Provider, consumer string) consumerState {
	t.Helper()

	pool, err := p.db()
	if err != nil {
		t.Fatal(err)
	}

	var state consumerState
	if err := pool.QueryRow(t.Context(), fmt.Sprintf(`SELECT c.cursor_txid::text, c.cursor_seq, (SELECT count(*) FROM %s WHERE consumer_id = c.consumer_id) FROM %s c WHERE c.consumer_id = $1`, p.t.claims, p.t.consumers), consumer).
		Scan(&state.cursor, &state.seq, &state.claims); err != nil {
		t.Fatal(err)
	}

	return state
}

func txids(t *testing.T, p *Provider, key string) string {
	t.Helper()

	pool, err := p.db()
	if err != nil {
		t.Fatal(err)
	}

	var ids string
	if err := pool.QueryRow(t.Context(), fmt.Sprintf(`SELECT coalesce(string_agg(txid::text, ',' ORDER BY seq), '') FROM %s WHERE stream_key = $1`, p.t.messages), key).Scan(&ids); err != nil {
		t.Fatal(err)
	}

	return ids
}

func publishN(t *testing.T, p *Provider, ns string, cfg core.StreamConfig, prefix string, n int) {
	t.Helper()

	for i := range n {
		if _, err := p.Publish(t.Context(), ns, cfg, envelope(ns, fmt.Sprintf("%s-%d", prefix, i+1), placed)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRebaseRepairsARestoredDatabase(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	key := streamKey(ns, cfg.Name)
	b := binding(ns, cfg, core.Competing, "restored", "all")

	publishN(t, p, ns, cfg, "m", 3)

	s, err := p.Subscribe(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	if err := next(t, s, 3*time.Second).Ack(ctx); err != nil {
		t.Fatal(err)
	}

	// m-2 is in flight when the dump is taken.
	_ = next(t, s, 3*time.Second)

	_ = s.Close(ctx)

	restore(t, p, key, b.ConsumerID())

	if _, err := p.Subscribe(ctx, b); err == nil || !strings.Contains(err.Error(), "ahead of this cluster's transaction IDs") {
		t.Fatalf("a restored cursor was accepted before Rebase: %v", err)
	}

	if err := p.Rebase(ctx); err != nil {
		t.Fatal(err)
	}

	if state := stateOf(t, p, b.ConsumerID()); state != (consumerState{cursor: "0"}) {
		t.Fatalf("Rebase left the consumer at %+v", state)
	}

	s, err = p.Subscribe(ctx, b)
	if err != nil {
		t.Fatalf("Subscribe after Rebase: %v", err)
	}

	defer func() { _ = s.Close(context.WithoutCancel(ctx)) }()

	// Every retained message comes again, then one published after the repair.
	publishN(t, p, ns, cfg, "after", 1)

	for _, want := range []string{"m-1", "m-2", "m-3", "after-1"} {
		d := next(t, s, 3*time.Second)
		if d.Message().ID != want {
			t.Fatalf("delivered %s, want %s", d.Message().ID, want)
		}

		if err := d.Ack(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRebaseRepairsAConsumerBehindRestoredRows covers a restore from a cluster younger
// than this one. The consumer's cursor is a valid ID here, so Subscribe accepts it, but
// the rows it has not read yet move behind it when Rebase resets their IDs.
func TestRebaseRepairsAConsumerBehindRestoredRows(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	b := binding(ns, cfg, core.Competing, "behind", "all")

	publishN(t, p, ns, cfg, "m", 3)

	s, err := p.Subscribe(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = s.Close(context.WithoutCancel(ctx)) }()

	if err := next(t, s, 3*time.Second).Ack(ctx); err != nil {
		t.Fatal(err)
	}

	restore(t, p, streamKey(ns, cfg.Name))

	if err := p.Rebase(ctx); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"m-1", "m-2", "m-3"} {
		d := next(t, s, 3*time.Second)
		if d.Message().ID != want {
			t.Fatalf("delivered %s, want %s", d.Message().ID, want)
		}

		if err := d.Ack(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRebaseLeavesAHealthyDatabaseAlone(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	key := streamKey(ns, cfg.Name)
	b := binding(ns, cfg, core.Competing, "healthy", "all")

	publishN(t, p, ns, cfg, "m", 3)

	s, err := p.Subscribe(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = s.Close(context.WithoutCancel(ctx)) }()

	if err := next(t, s, 3*time.Second).Ack(ctx); err != nil {
		t.Fatal(err)
	}

	held := next(t, s, 3*time.Second)
	before, rows := stateOf(t, p, b.ConsumerID()), txids(t, p, key)

	if err := p.Rebase(ctx); err != nil {
		t.Fatal(err)
	}

	if after := stateOf(t, p, b.ConsumerID()); after != before {
		t.Fatalf("Rebase moved a healthy consumer from %+v to %+v", before, after)
	}

	if after := txids(t, p, key); after != rows {
		t.Fatalf("Rebase changed healthy rows from %s to %s", rows, after)
	}

	if err := held.Ack(ctx); err != nil {
		t.Fatalf("the claim in flight did not survive Rebase: %v", err)
	}

	if d := next(t, s, 3*time.Second); d.Message().ID != "m-3" {
		t.Fatalf("delivered %s after Rebase, want m-3", d.Message().ID)
	}
}

// TestRebaseClearsAStartSnapshotFromTheOldCluster covers a StartAt "new" consumer whose
// snapshot came from the old cluster. Every ID this cluster hands out is below that
// snapshot, so every new row looks committed before the subscription and is filtered
// out, though the cursor itself is valid here.
func TestRebaseClearsAStartSnapshotFromTheOldCluster(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)
	b := binding(ns, cfg, core.Competing, "new", "new")

	s, err := p.Subscribe(ctx, b)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = s.Close(context.WithoutCancel(ctx)) }()

	pool, _ := p.db()

	if _, err := pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET start_snapshot = (x || ':' || x || ':')::pg_snapshot
FROM (SELECT (pg_snapshot_xmax(pg_current_snapshot())::text::bigint + 1000000)::text AS x) AS old WHERE consumer_id = $1`, p.t.consumers), b.ConsumerID()); err != nil {
		t.Fatal(err)
	}

	if err := p.Rebase(ctx); err != nil {
		t.Fatal(err)
	}

	publishN(t, p, ns, cfg, "after", 1)

	if d := next(t, s, 3*time.Second); d.Message().ID != "after-1" {
		t.Fatalf("delivered %s, want after-1", d.Message().ID)
	}
}
