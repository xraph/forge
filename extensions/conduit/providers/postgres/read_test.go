package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
)

func TestDeliveriesCarryTheirPosition(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	receipt, err := p.Publish(ctx, ns, cfg, envelope(ns, "with-position", placed))
	if err != nil {
		t.Fatal(err)
	}

	s, err := p.Subscribe(ctx, binding(ns, cfg, core.Competing, "positions", "all"))
	if err != nil {
		t.Fatal(err)
	}

	d := next(t, s, 5*time.Second)

	got := d.Message().Headers[PositionHeader]

	var txid string
	if err := p.pool.QueryRow(ctx, "SELECT txid::text FROM "+p.t.messages+" WHERE stream_key = $1 AND seq = $2", streamKey(ns, cfg.Name), int64(receipt.Sequence)).Scan(&txid); err != nil {
		t.Fatal(err)
	}

	if want := txid + ":" + itoa(int64(receipt.Sequence)); got != want {
		t.Fatalf("position %q, want %q", got, want)
	}

	// A redelivered message carries the same position.
	if err := d.Retry(ctx, time.Millisecond); err != nil {
		t.Fatal(err)
	}

	again := next(t, s, 5*time.Second)
	if again.Message().Headers[PositionHeader] != got {
		t.Fatalf("redelivery position %q, want %q", again.Message().Headers[PositionHeader], got)
	}
}

func TestReadAfterReturnsWhatFollowsAPosition(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	publish := func(id, key, org string) {
		t.Helper()

		e := envelope(ns, id, placed)
		e.Key = key
		e.Headers = map[string]string{"org": org}

		if _, err := p.Publish(ctx, ns, cfg, e); err != nil {
			t.Fatal(err)
		}
	}

	publish("a1", "project-a", "org-1")
	publish("b1", "project-b", "org-1")
	publish("a2", "project-a", "org-1")
	publish("c1", "project-c", "org-2")

	all, resumable, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{Headers: map[string]string{"org": "org-1"}})
	if err != nil || !resumable {
		t.Fatalf("read: %v resumable=%v", err, resumable)
	}

	if ids(all) != "a1,b1,a2" {
		t.Fatalf("org filter returned %s", ids(all))
	}

	after, resumable, err := p.ReadAfter(ctx, ns, cfg, all[0].Headers[PositionHeader], ReadFilter{Key: "project-a"})
	if err != nil || !resumable {
		t.Fatalf("read: %v resumable=%v", err, resumable)
	}

	if ids(after) != "a2" {
		t.Fatalf("after a1 for project-a returned %s", ids(after))
	}

	limited, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{Limit: 2})
	if err != nil || len(limited) != 2 {
		t.Fatalf("limit: %d %v", len(limited), err)
	}

	if ids(limited) != "a1,b1" {
		t.Fatalf("limit 2 returned %s, want the first two in order", ids(limited))
	}

	typed, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{Types: []string{"orders.cancelled.v1"}})
	if err != nil || len(typed) != 0 {
		t.Fatalf("type filter: %d %v", len(typed), err)
	}

	matching, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{Types: []string{"orders.cancelled.v1", placed}})
	if err != nil || ids(matching) != "a1,b1,a2,c1" {
		t.Fatalf("a matching type returned %s %v", ids(matching), err)
	}

	// Nothing was pruned, so "0:0" is resumable.
	if _, resumable, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{}); err != nil || !resumable {
		t.Fatalf("a never-pruned stream from the start: %v %v", resumable, err)
	}
}

func TestReadAfterWaitsForAnOpenTransaction(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	hold, err := p.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := p.PublishTx(ctx, hold, ns, cfg, envelope(ns, "held", placed)); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "behind", placed)); err != nil {
		t.Fatal(err)
	}

	got, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || len(got) != 0 {
		t.Fatalf("read past an open transaction: %s %v", ids(got), err)
	}

	if err := hold.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got, _, err = p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || ids(got) != "held,behind" {
		t.Fatalf("after commit: %s %v", ids(got), err)
	}
}

// streamWith declares a stream with the given retention.
func streamWith(t *testing.T, p *Provider, cfg core.StreamConfig) (string, core.StreamConfig) {
	t.Helper()

	ns := "pg-" + core.NewID()
	cfg.Name, cfg.Subjects, cfg.Replicas = "orders", []string{"orders.>"}, 1

	if err := p.EnsureStream(t.Context(), ns, cfg); err != nil {
		t.Fatal(err)
	}

	return ns, cfg
}

func TestReadAfterReportsAPositionTheTrimPassed(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := streamWith(t, p, core.StreamConfig{MaxMessages: 1})

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "old", placed)); err != nil {
		t.Fatal(err)
	}

	// Nothing is pruned yet, so every position, "0:0" included, is resumable.
	all, resumable, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || !resumable || ids(all) != "old" {
		t.Fatalf("before the trim: %s resumable=%v %v", ids(all), resumable, err)
	}

	// The second message trims the first.
	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "kept", placed)); err != nil {
		t.Fatal(err)
	}

	got, resumable, err := p.ReadAfter(ctx, ns, cfg, all[0].Headers[PositionHeader], ReadFilter{})
	if err != nil || !resumable || ids(got) != "kept" {
		t.Fatalf("from the pruned message itself: %s resumable=%v %v", ids(got), resumable, err)
	}

	// The rows still come back from "0:0", but a message before them is gone.
	got, resumable, err = p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || resumable || ids(got) != "kept" {
		t.Fatalf("from the start after a trim: %s resumable=%v %v", ids(got), resumable, err)
	}

	kept := got[0].Headers[PositionHeader]

	got, resumable, err = p.ReadAfter(ctx, ns, cfg, kept, ReadFilter{})
	if err != nil || !resumable || len(got) != 0 {
		t.Fatalf("from the retained message: %d resumable=%v %v", len(got), resumable, err)
	}
}

// TestReadAfterSeesATrimThatIgnoresPositionOrder places three rows whose seq order
// differs from their transaction order, then trims by seq. The reader's own row and one
// after it are removed while an earlier row survives, so "a retained row sits at or
// before the position" would wrongly say nothing was lost.
func TestReadAfterSeesATrimThatIgnoresPositionOrder(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := streamWith(t, p, core.StreamConfig{MaxMessages: 10})
	pool, _ := p.db()

	ensureXIDAbove(t, pool, 200)

	insertAt(t, p, ns, cfg, "100", 12, "r1", placed)
	insertAt(t, p, ns, cfg, "101", 9, "r2", placed)
	insertAt(t, p, ns, cfg, "102", 10, "r3", placed)

	if _, err := pool.Exec(ctx, "SELECT setval($1::regclass, 19)", p.sequence(streamKey(ns, cfg.Name))); err != nil {
		t.Fatal(err)
	}

	// Seq 20 trims everything at or below seq 10: r2 and r3, not r1.
	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "r4", placed)); err != nil {
		t.Fatal(err)
	}

	left, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || ids(left) != "r1,r4" {
		t.Fatalf("retained %s %v", ids(left), err)
	}

	for position, want := range map[string]bool{
		"100:12": false, // r1 survived, yet r2 and r3 after it are gone
		"101:9":  false, // r2's own position: r3 after it is gone
		"102:10": true,  // the newest removed message
	} {
		_, resumable, err := p.ReadAfter(ctx, ns, cfg, position, ReadFilter{})
		if err != nil || resumable != want {
			t.Fatalf("position %s: resumable=%v, want %v (%v)", position, resumable, want, err)
		}
	}
}

func TestSweepRaisesTheWatermarkAndHeadFollowsIt(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := streamWith(t, p, core.StreamConfig{MaxAge: 600 * time.Millisecond})

	for _, id := range []string{"first", "second"} {
		if _, err := p.Publish(ctx, ns, cfg, envelope(ns, id, placed)); err != nil {
			t.Fatal(err)
		}
	}

	all, resumable, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || !resumable || len(all) != 2 {
		t.Fatalf("read: %d resumable=%v %v", len(all), resumable, err)
	}

	last := all[1].Headers[PositionHeader]

	time.Sleep(800 * time.Millisecond)

	// Aged out but not yet swept: hidden from the rows, and the gap is reported.
	got, resumable, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || resumable || len(got) != 0 {
		t.Fatalf("before the sweep: %d resumable=%v %v", len(got), resumable, err)
	}

	pool, _ := p.db()
	p.sweep(ctx, pool)

	got, resumable, err = p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || resumable || len(got) != 0 {
		t.Fatalf("after the sweep, from the start: %d resumable=%v %v", len(got), resumable, err)
	}

	// The stream is empty now, but its head is where the pruning stopped, not "0:0".
	head, err := p.Head(ctx, ns, cfg)
	if err != nil || head != last {
		t.Fatalf("head after full expiry %q, want the watermark %q (%v)", head, last, err)
	}

	got, resumable, err = p.ReadAfter(ctx, ns, cfg, head, ReadFilter{})
	if err != nil || !resumable || len(got) != 0 {
		t.Fatalf("from the head: %d resumable=%v %v", len(got), resumable, err)
	}
}

func TestRebaseBringsTheWatermarkBackWithTheRows(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := streamWith(t, p, core.StreamConfig{MaxMessages: 1})
	key := streamKey(ns, cfg.Name)
	pool, _ := p.db()

	for _, id := range []string{"gone", "kept"} {
		if _, err := p.Publish(ctx, ns, cfg, envelope(ns, id, placed)); err != nil {
			t.Fatal(err)
		}
	}

	restore(t, p, key)

	if _, err := pool.Exec(ctx, "UPDATE "+p.t.streams+" SET pruned_txid = (pg_snapshot_xmax(pg_current_snapshot())::text::bigint + 1000000 + pruned_seq)::text::xid8 WHERE stream_key = $1", key); err != nil {
		t.Fatal(err)
	}

	if err := p.Rebase(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "after", placed)); err != nil {
		t.Fatal(err)
	}

	got, resumable, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || resumable || ids(got) != "after" {
		t.Fatalf("from the start after a rebase: %s resumable=%v %v", ids(got), resumable, err)
	}

	head, err := p.Head(ctx, ns, cfg)
	if err != nil || head != got[0].Headers[PositionHeader] {
		t.Fatalf("head %q, want the newest message %q (%v)", head, got[0].Headers[PositionHeader], err)
	}

	if _, resumable, err := p.ReadAfter(ctx, ns, cfg, head, ReadFilter{}); err != nil || !resumable {
		t.Fatalf("from the head after a rebase: %v %v", resumable, err)
	}
}

func TestReadAfterRefusesAMalformedPosition(t *testing.T) {
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	for _, position := range []string{"", "1", "x:1", "1:-1", "1:2:3", "not-a-position"} {
		if _, _, err := p.ReadAfter(t.Context(), ns, cfg, position, ReadFilter{}); !errors.Is(err, ErrInvalidPosition) {
			t.Fatalf("position %q: %v, want ErrInvalidPosition", position, err)
		}
	}
}

func TestHeadIsTheNewestCommittedPosition(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	head, err := p.Head(ctx, ns, cfg)
	if err != nil || head != "0:0" {
		t.Fatalf("empty stream head %q %v", head, err)
	}

	for _, id := range []string{"first", "second"} {
		if _, err := p.Publish(ctx, ns, cfg, envelope(ns, id, placed)); err != nil {
			t.Fatal(err)
		}
	}

	all, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("read: %d %v", len(all), err)
	}

	hold, err := p.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := p.PublishTx(ctx, hold, ns, cfg, envelope(ns, "open", placed)); err != nil {
		t.Fatal(err)
	}

	head, err = p.Head(ctx, ns, cfg)
	if err != nil || head != all[1].Headers[PositionHeader] {
		t.Fatalf("head %q, want the last committed %q (%v)", head, all[1].Headers[PositionHeader], err)
	}
}

func ids(envelopes []core.Envelope) string {
	out := make([]string, 0, len(envelopes))

	for _, e := range envelopes {
		out = append(out, e.ID)
	}

	return strings.Join(out, ",")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestTrimLeavesRowsAboveTheHorizon is the open-transaction race: while T0 is open, a trim
// that removed T1's committed row would raise the watermark above T0, and T0's row,
// committing behind it, would be lost to a reader that started at Head.
func TestTrimLeavesRowsAboveTheHorizon(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := streamWith(t, p, core.StreamConfig{MaxMessages: 1})

	t0, err := p.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = t0.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := p.PublishTx(ctx, t0, ns, cfg, envelope(ns, "t0", placed)); err != nil {
		t.Fatal(err)
	}

	// T1 and T2 commit while T0 is open; T2's trim would delete T1's row.
	for _, id := range []string{"t1", "t2"} {
		if _, err := p.Publish(ctx, ns, cfg, envelope(ns, id, placed)); err != nil {
			t.Fatal(err)
		}
	}

	var rows int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM "+p.t.messages+" WHERE stream_key = $1", streamKey(ns, cfg.Name)).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	if rows != 2 {
		t.Fatalf("%d committed rows retained while a transaction is open, want both", rows)
	}

	head, err := p.Head(ctx, ns, cfg)
	if err != nil || head != "0:0" {
		t.Fatalf("head %q while the horizon is held, want 0:0 (%v)", head, err)
	}

	if err := t0.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got, resumable, err := p.ReadAfter(ctx, ns, cfg, head, ReadFilter{})
	if err != nil || !resumable || ids(got) != "t0,t1,t2" {
		t.Fatalf("from the head read earlier: %s resumable=%v %v", ids(got), resumable, err)
	}

	// With nothing open, the next trim catches up.
	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "t3", placed)); err != nil {
		t.Fatal(err)
	}

	got, resumable, err = p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || resumable || ids(got) != "t3" {
		t.Fatalf("after the trim caught up: %s resumable=%v %v", ids(got), resumable, err)
	}
}

func TestSweepLeavesRowsAboveTheHorizon(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := streamWith(t, p, core.StreamConfig{MaxAge: 300 * time.Millisecond})

	hold, err := p.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := p.PublishTx(ctx, hold, ns, cfg, envelope(ns, "held", placed)); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Publish(ctx, ns, cfg, envelope(ns, "behind", placed)); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	p.sweep(ctx, p.pool)

	var rows int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM "+p.t.messages+" WHERE stream_key = $1", streamKey(ns, cfg.Name)).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	if rows != 1 {
		t.Fatalf("%d rows after a sweep with a transaction open, want the one above the horizon", rows)
	}

	head, err := p.Head(ctx, ns, cfg)
	if err != nil || head != "0:0" {
		t.Fatalf("head %q, want 0:0 (%v)", head, err)
	}
}

func TestReadAfterRefusesAFuturePosition(t *testing.T) {
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	if _, err := p.Publish(t.Context(), ns, cfg, envelope(ns, "now", placed)); err != nil {
		t.Fatal(err)
	}

	got, resumable, err := p.ReadAfter(t.Context(), ns, cfg, "999999999999:1", ReadFilter{})
	if err != nil || resumable || len(got) != 0 {
		t.Fatalf("a future position: %d resumable=%v %v", len(got), resumable, err)
	}
}
