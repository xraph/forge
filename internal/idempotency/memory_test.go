package idempotency

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newTestStore(opts ...MemoryOption) (*MemoryStore, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}

	return NewMemoryStore(append([]MemoryOption{WithClock(clock.Now)}, opts...)...), clock
}

var orderKey = Key{Principal: "alice", Scope: "POST /orders", Value: "k1"}

func TestBeginAcquiresAFreeKeyAndReportsItInFlight(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	got, err := s.Begin(ctx, orderKey, "fp", time.Minute)
	if err != nil || got.State != Acquired {
		t.Fatalf("first Begin = %+v, %v; want Acquired", got, err)
	}

	again, err := s.Begin(ctx, orderKey, "fp2", time.Minute)
	if err != nil || again.State != InFlight {
		t.Fatalf("second Begin = %+v, %v; want InFlight", again, err)
	}

	if again.Fingerprint != "fp" {
		t.Fatalf("InFlight fingerprint = %q, want the holder's %q", again.Fingerprint, "fp")
	}

	if again.Done == nil {
		t.Fatal("InFlight Done is nil; the memory store can signal")
	}
}

func TestCompleteStoresAResponseThatReplaysAsACopy(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	if _, err := s.Begin(ctx, orderKey, "fp", time.Minute); err != nil {
		t.Fatal(err)
	}

	resp := Response{
		Status:      http.StatusCreated,
		Header:      http.Header{"X-Order": {"7"}},
		Body:        []byte(`{"id":"7"}`),
		Fingerprint: "fp",
		StoredAt:    clock.Now(),
		ExpiresAt:   clock.Now().Add(time.Hour),
	}
	if err := s.Complete(ctx, orderKey, resp); err != nil {
		t.Fatal(err)
	}

	resp.Body[0] = 'X' // the store must have copied

	got, err := s.Begin(ctx, orderKey, "ignored", time.Minute)
	if err != nil || got.State != Replay {
		t.Fatalf("Begin after Complete = %+v, %v; want Replay", got, err)
	}

	if string(got.Response.Body) != `{"id":"7"}` || got.Response.Status != http.StatusCreated {
		t.Fatalf("replayed %d %s", got.Response.Status, got.Response.Body)
	}

	if got.Fingerprint != "fp" || got.Response.Header.Get("X-Order") != "7" {
		t.Fatalf("replay lost fingerprint or header: %+v", got)
	}
}

func TestDoneClosesOnCompleteAndOnRelease(t *testing.T) {
	for _, finish := range []string{"complete", "release"} {
		t.Run(finish, func(t *testing.T) {
			s, _ := newTestStore()
			ctx := context.Background()

			_, _ = s.Begin(ctx, orderKey, "fp", time.Minute)
			waiting, _ := s.Begin(ctx, orderKey, "fp", time.Minute)

			if finish == "complete" {
				_ = s.Complete(ctx, orderKey, Response{Status: 200})
			} else {
				_ = s.Release(ctx, orderKey)
			}

			select {
			case <-waiting.Done:
			case <-time.After(time.Second):
				t.Fatal("Done was not closed")
			}
		})
	}
}

func TestReleaseFreesTheKey(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	_, _ = s.Begin(ctx, orderKey, "fp", time.Minute)
	_ = s.Release(ctx, orderKey)

	got, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if got.State != Acquired {
		t.Fatalf("Begin after Release = %v, want Acquired", got.State)
	}
}

func TestALapsedLeaseCanBeTakenOver(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	first, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if first.State != Acquired {
		t.Fatal("first Begin did not acquire")
	}

	clock.Advance(2 * time.Minute)

	got, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if got.State != Acquired {
		t.Fatalf("Begin after the lease lapsed = %v, want Acquired", got.State)
	}
}

func TestAnExpiredResponseIsForgotten(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	_ = s.Complete(ctx, orderKey, Response{Status: 200, StoredAt: clock.Now(), ExpiresAt: clock.Now().Add(time.Hour)})
	clock.Advance(2 * time.Hour)

	got, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if got.State != Acquired {
		t.Fatalf("Begin after expiry = %v, want Acquired", got.State)
	}
}

func TestAZeroExpiryNeverExpires(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	_ = s.Complete(ctx, orderKey, Response{Status: 200, StoredAt: clock.Now()})
	clock.Advance(1000 * time.Hour)

	got, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if got.State != Replay {
		t.Fatalf("Begin = %v, want Replay for a response with no expiry", got.State)
	}
}

func TestCompleteWithoutBeginStoresTheResponse(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	_ = s.Complete(ctx, orderKey, Response{Status: 204})

	got, _ := s.Begin(ctx, orderKey, "", time.Minute)
	if got.State != Replay || got.Response.Status != 204 {
		t.Fatalf("Begin = %+v, want a Replay of 204", got)
	}
}

func TestEvictionCountsOnlyCompletedEntries(t *testing.T) {
	s, _ := newTestStore(WithMaxEntries(2))
	ctx := context.Background()
	key := func(v string) Key { return Key{Principal: "u", Scope: "s", Value: v} }

	_ = s.Complete(ctx, key("k1"), Response{Status: 200})
	_ = s.Complete(ctx, key("k2"), Response{Status: 200})
	_ = s.Complete(ctx, key("k3"), Response{Status: 200})

	// Claiming a fresh key must not push a completed one out.
	if got, _ := s.Begin(ctx, key("k1"), "", time.Minute); got.State != Acquired {
		t.Fatalf("k1 = %v, want Acquired (it was the oldest and was evicted)", got.State)
	}

	_ = s.Release(ctx, key("k1"))

	for _, v := range []string{"k2", "k3"} {
		if got, _ := s.Begin(ctx, key(v), "", time.Minute); got.State != Replay {
			t.Fatalf("%s = %v, want Replay", v, got.State)
		}
	}
}

func TestDifferentPrincipalsAndScopesAreIndependent(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	_ = s.Complete(ctx, orderKey, Response{Status: 200})

	for _, k := range []Key{
		{Principal: "bob", Scope: orderKey.Scope, Value: orderKey.Value},
		{Principal: orderKey.Principal, Scope: "POST /orders/8", Value: orderKey.Value},
	} {
		if got, _ := s.Begin(ctx, k, "", time.Minute); got.State != Acquired {
			t.Fatalf("%+v = %v, want Acquired", k, got.State)
		}
	}
}

func TestDefaultReturnsOneSharedStore(t *testing.T) {
	a, b := Default(), Default()
	if a != b {
		t.Fatal("Default returned two different stores")
	}
}
