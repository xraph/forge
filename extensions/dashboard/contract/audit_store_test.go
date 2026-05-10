package contract

import (
	"context"
	"testing"
	"time"
)

func mkRec(intent string, t time.Time) AuditRecord {
	return AuditRecord{
		Time: t, Contributor: "core-contract", Intent: intent, Result: "ok",
	}
}

func TestAuditStore_AppendList(t *testing.T) {
	s := NewInMemoryAuditStore(0)
	now := time.Now()
	s.Append(mkRec("a", now.Add(-1*time.Second)))
	s.Append(mkRec("b", now))
	got := s.List(AuditFilter{})
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Intent != "b" || got[1].Intent != "a" {
		t.Errorf("expected newest-first ordering, got %+v", got)
	}
}

func TestAuditStore_FilterIntent(t *testing.T) {
	s := NewInMemoryAuditStore(0)
	s.Append(mkRec("a", time.Now()))
	s.Append(mkRec("b", time.Now()))
	s.Append(mkRec("a", time.Now()))
	got := s.List(AuditFilter{Intent: "a"})
	if len(got) != 2 {
		t.Errorf("expected 2 intent=a records, got %d", len(got))
	}
}

func TestAuditStore_LimitClamping(t *testing.T) {
	s := NewInMemoryAuditStore(0)
	for i := 0; i < 50; i++ {
		s.Append(mkRec("x", time.Now()))
	}
	if got := s.List(AuditFilter{Limit: 5}); len(got) != 5 {
		t.Errorf("limit=5 -> %d records", len(got))
	}
	if got := s.List(AuditFilter{Limit: -1}); len(got) != 50 {
		t.Errorf("limit=-1 -> %d records (want default 50 since cap < default)", len(got))
	}
}

func TestAuditStore_RingTruncates(t *testing.T) {
	s := NewInMemoryAuditStore(3)
	for i := 0; i < 5; i++ {
		s.Append(AuditRecord{Time: time.Now(), Intent: "x", User: string(rune('a' + i))})
	}
	got := s.List(AuditFilter{})
	if len(got) != 3 {
		t.Fatalf("expected 3 (cap), got %d", len(got))
	}
	// Newest is "e", oldest kept is "c"; "a" and "b" dropped.
	if got[0].User != "e" || got[2].User != "c" {
		t.Errorf("ring kept wrong records: %+v", got)
	}
}

func TestAuditStore_SubscribeBroadcasts(t *testing.T) {
	s := NewInMemoryAuditStore(0)
	ch, cancel := s.Subscribe()
	defer cancel()
	go s.Append(mkRec("hello", time.Now()))
	select {
	case rec := <-ch:
		if rec.Intent != "hello" {
			t.Errorf("got %+v", rec)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
}

func TestAuditStore_CancelClosesChannel(t *testing.T) {
	s := NewInMemoryAuditStore(0)
	ch, cancel := s.Subscribe()
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close")
	}
}

func TestRecordingAuditEmitter_FansOut(t *testing.T) {
	store := NewInMemoryAuditStore(0)
	captured := []AuditRecord{}
	innerCalled := false
	inner := auditEmitterFunc(func(_ context.Context, rec AuditRecord) {
		innerCalled = true
		captured = append(captured, rec)
	})
	em := NewRecordingAuditEmitter(inner, store)
	em.Emit(context.Background(), mkRec("x", time.Now()))
	if !innerCalled {
		t.Errorf("inner emitter not called")
	}
	if len(captured) != 1 {
		t.Errorf("inner saw %d records", len(captured))
	}
	if got := store.List(AuditFilter{}); len(got) != 1 {
		t.Errorf("store has %d records", len(got))
	}
}

// auditEmitterFunc is a tiny test-only adapter from func to AuditEmitter.
type auditEmitterFunc func(ctx context.Context, rec AuditRecord)

func (f auditEmitterFunc) Emit(ctx context.Context, rec AuditRecord) { f(ctx, rec) }
