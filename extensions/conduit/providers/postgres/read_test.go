package postgres

import (
	"context"
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

	typed, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{Types: []string{"orders.cancelled.v1"}})
	if err != nil || len(typed) != 0 {
		t.Fatalf("type filter: %d %v", len(typed), err)
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

func TestReadAfterReportsAPositionThatAgedOut(t *testing.T) {
	ctx := t.Context()
	p := connect(t, Options{})
	ns, cfg := newStream(t, p)

	for _, id := range []string{"old", "kept"} {
		if _, err := p.Publish(ctx, ns, cfg, envelope(ns, id, placed)); err != nil {
			t.Fatal(err)
		}
	}

	all, _, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("read: %d %v", len(all), err)
	}

	// Retention removes the oldest message. A reader whose position was that
	// message can't be told it missed nothing, though the rows still come back.
	if _, err := p.pool.Exec(ctx, "DELETE FROM "+p.t.messages+" WHERE stream_key = $1 AND seq = (SELECT min(seq) FROM "+p.t.messages+" WHERE stream_key = $1)", streamKey(ns, cfg.Name)); err != nil {
		t.Fatal(err)
	}

	got, resumable, err := p.ReadAfter(ctx, ns, cfg, all[0].Headers[PositionHeader], ReadFilter{})
	if err != nil || resumable || ids(got) != "kept" {
		t.Fatalf("a pruned position: %s resumable=%v %v", ids(got), resumable, err)
	}

	got, resumable, err = p.ReadAfter(ctx, ns, cfg, all[1].Headers[PositionHeader], ReadFilter{})
	if err != nil || !resumable || len(got) != 0 {
		t.Fatalf("a retained position: %d %v %v", len(got), resumable, err)
	}

	// "0:0" asks for everything retained and is always resumable.
	if _, resumable, err := p.ReadAfter(ctx, ns, cfg, "0:0", ReadFilter{}); err != nil || !resumable {
		t.Fatalf("from the start: %v %v", resumable, err)
	}

	if _, _, err := p.ReadAfter(ctx, ns, cfg, "not-a-position", ReadFilter{}); err == nil {
		t.Fatal("a malformed position was accepted")
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
