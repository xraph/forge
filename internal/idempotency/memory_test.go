package idempotency

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
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

	if got.Token == 0 {
		t.Fatal("Acquired Begin returned the zero token")
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

	if again.Token != 0 {
		t.Fatalf("InFlight Begin returned token %d; only Acquired carries one", again.Token)
	}
}

func TestCompleteStoresAResponseThatReplaysAsACopy(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	first, err := s.Begin(ctx, orderKey, "fp", time.Minute)
	if err != nil {
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
	if err := s.Complete(ctx, orderKey, first.Token, resp); err != nil {
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

func TestCompleteCopiesTheHeader(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	first, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	resp := Response{Status: 200, Header: http.Header{"X-Order": {"7"}}}
	_ = s.Complete(ctx, orderKey, first.Token, resp)

	resp.Header.Set("X-Order", "tampered")
	resp.Header.Set("X-Added", "yes")

	got, _ := s.Begin(ctx, orderKey, "", time.Minute)
	if got.Response.Header.Get("X-Order") != "7" || got.Response.Header.Get("X-Added") != "" {
		t.Fatalf("the caller's header leaked into the store: %v", got.Response.Header)
	}
}

func TestAReplayIsACopyOfTheStoredHeaderAndBody(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	first, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	_ = s.Complete(ctx, orderKey, first.Token, Response{
		Status: 200,
		Header: http.Header{"X-Order": {"7"}},
		Body:   []byte("body"),
	})

	one, _ := s.Begin(ctx, orderKey, "", time.Minute)
	one.Response.Header.Set("X-Order", "tampered")
	one.Response.Body[0] = 'X'

	two, _ := s.Begin(ctx, orderKey, "", time.Minute)
	if two.Response.Header.Get("X-Order") != "7" || string(two.Response.Body) != "body" {
		t.Fatalf("a replay mutated the stored response: %v %s", two.Response.Header, two.Response.Body)
	}
}

func TestDoneClosesOnCompleteAndOnRelease(t *testing.T) {
	for _, finish := range []string{"complete", "release"} {
		t.Run(finish, func(t *testing.T) {
			s, _ := newTestStore()
			ctx := context.Background()

			first, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
			waiting, _ := s.Begin(ctx, orderKey, "fp", time.Minute)

			if finish == "complete" {
				_ = s.Complete(ctx, orderKey, first.Token, Response{Status: 200})
			} else {
				_ = s.Release(ctx, orderKey, first.Token)
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

	first, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if err := s.Release(ctx, orderKey, first.Token); err != nil {
		t.Fatal(err)
	}

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

	if got.Token == first.Token {
		t.Fatalf("the takeover reused token %d", got.Token)
	}
}

func TestAnExpiredResponseIsForgotten(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	_ = s.Complete(ctx, orderKey, 0, Response{Status: 200, StoredAt: clock.Now(), ExpiresAt: clock.Now().Add(time.Hour)})
	clock.Advance(2 * time.Hour)

	got, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if got.State != Acquired {
		t.Fatalf("Begin after expiry = %v, want Acquired", got.State)
	}
}

func TestAZeroExpiryNeverExpires(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	_ = s.Complete(ctx, orderKey, 0, Response{Status: 200, StoredAt: clock.Now()})
	clock.Advance(1000 * time.Hour)

	got, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if got.State != Replay {
		t.Fatalf("Begin = %v, want Replay for a response with no expiry", got.State)
	}
}

func TestCompleteWithoutBeginStoresTheResponse(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	if err := s.Complete(ctx, orderKey, 0, Response{Status: 204}); err != nil {
		t.Fatal(err)
	}

	got, _ := s.Begin(ctx, orderKey, "", time.Minute)
	if got.State != Replay || got.Response.Status != 204 {
		t.Fatalf("Begin = %+v, want a Replay of 204", got)
	}
}

func TestEvictionCountsOnlyCompletedEntries(t *testing.T) {
	s, _ := newTestStore(WithMaxEntries(2))
	ctx := context.Background()
	key := func(v string) Key { return Key{Principal: "u", Scope: "s", Value: v} }

	_ = s.Complete(ctx, key("k1"), 0, Response{Status: 200})
	_ = s.Complete(ctx, key("k2"), 0, Response{Status: 200})
	_ = s.Complete(ctx, key("k3"), 0, Response{Status: 200})

	// Claiming a fresh key must not push a completed one out.
	got, _ := s.Begin(ctx, key("k1"), "", time.Minute)
	if got.State != Acquired {
		t.Fatalf("k1 = %v, want Acquired (it was the oldest and was evicted)", got.State)
	}

	_ = s.Release(ctx, key("k1"), got.Token)

	for _, v := range []string{"k2", "k3"} {
		if got, _ := s.Begin(ctx, key(v), "", time.Minute); got.State != Replay {
			t.Fatalf("%s = %v, want Replay", v, got.State)
		}
	}
}

func TestEvictionIsLeastRecentlyUsedNotFirstIn(t *testing.T) {
	key := func(v string) Key { return Key{Principal: "u", Scope: "s", Value: v} }

	cases := map[string]func(t *testing.T, s *MemoryStore){
		"a replay touches the entry": func(t *testing.T, s *MemoryStore) {
			if got, _ := s.Begin(context.Background(), key("k1"), "", time.Minute); got.State != Replay {
				t.Fatalf("k1 = %v, want Replay", got.State)
			}
		},
		"a second Complete touches the entry": func(t *testing.T, s *MemoryStore) {
			if err := s.Complete(context.Background(), key("k1"), 0, Response{Status: 200}); err != nil {
				t.Fatal(err)
			}
		},
	}

	for name, touch := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := newTestStore(WithMaxEntries(2))
			ctx := context.Background()

			_ = s.Complete(ctx, key("k1"), 0, Response{Status: 200})
			_ = s.Complete(ctx, key("k2"), 0, Response{Status: 200})

			touch(t, s)

			// k1 is now the most recently used, so k2 must be the one pushed out.
			_ = s.Complete(ctx, key("k3"), 0, Response{Status: 200})

			if got, _ := s.Begin(ctx, key("k1"), "", time.Minute); got.State != Replay {
				t.Fatalf("k1 = %v, want Replay (it was touched, so it is not the eviction victim)", got.State)
			}

			if got, _ := s.Begin(ctx, key("k2"), "", time.Minute); got.State != Acquired {
				t.Fatalf("k2 = %v, want Acquired (it was least recently used)", got.State)
			}
		})
	}
}

func TestDifferentPrincipalsAndScopesAreIndependent(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	_ = s.Complete(ctx, orderKey, 0, Response{Status: 200})

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

// A's lease lapses and B takes over. A then finishes late. A must not be able
// to overwrite B's claim with a response, close B's waiters, or let a third
// request replay A's (possibly timed-out) outcome while B is still running.
func TestALateCompleteFromALapsedHolderIsRefused(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	a, _ := s.Begin(ctx, orderKey, "fp", time.Minute)

	clock.Advance(2 * time.Minute)

	b, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if b.State != Acquired {
		t.Fatalf("B = %v, want Acquired after A's lease lapsed", b.State)
	}

	err := s.Complete(ctx, orderKey, a.Token, Response{Status: http.StatusGatewayTimeout, Body: []byte("A timed out")})
	if !errors.Is(err, ErrNotHolder) {
		t.Fatalf("A's late Complete = %v, want ErrNotHolder", err)
	}

	third, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if third.State != InFlight {
		t.Fatalf("third Begin = %+v, want InFlight while B runs (not a replay of A's response)", third)
	}

	select {
	case <-third.Done:
		t.Fatal("A's late Complete closed B's Done")
	default:
	}

	if err := s.Complete(ctx, orderKey, b.Token, Response{Status: http.StatusCreated, Body: []byte("B")}); err != nil {
		t.Fatalf("B's Complete = %v", err)
	}

	got, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if got.State != Replay || got.Response.Status != http.StatusCreated {
		t.Fatalf("after B completes, Begin = %+v, want a Replay of B's 201", got)
	}
}

// Same setup, but A's late call is Release. It must not drop B's claim, which
// would let a third request acquire the key while B still runs.
func TestALateReleaseFromALapsedHolderIsRefused(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	a, _ := s.Begin(ctx, orderKey, "fp", time.Minute)

	clock.Advance(2 * time.Minute)

	b, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if b.State != Acquired {
		t.Fatalf("B = %v, want Acquired after A's lease lapsed", b.State)
	}

	if err := s.Release(ctx, orderKey, a.Token); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("A's late Release = %v, want ErrNotHolder", err)
	}

	third, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	if third.State != InFlight {
		t.Fatalf("third Begin = %+v, want InFlight: two requests must never hold one key", third)
	}

	if err := s.Release(ctx, orderKey, b.Token); err != nil {
		t.Fatalf("B's Release = %v", err)
	}
}

func TestFencingRefusesAStaleOrForeignToken(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	held, _ := s.Begin(ctx, orderKey, "fp", time.Minute)

	// The zero token means "no claim". It cannot finish somebody else's.
	if err := s.Complete(ctx, orderKey, 0, Response{Status: 200}); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("zero-token Complete over a live claim = %v, want ErrNotHolder", err)
	}

	if err := s.Release(ctx, orderKey, 0); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("zero-token Release of a live claim = %v, want ErrNotHolder", err)
	}

	if err := s.Complete(ctx, orderKey, held.Token+1, Response{Status: 200}); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("wrong-token Complete = %v, want ErrNotHolder", err)
	}

	if again, _ := s.Begin(ctx, orderKey, "fp", time.Minute); again.State != InFlight {
		t.Fatalf("refused calls disturbed the claim: %+v", again)
	}

	if err := s.Complete(ctx, orderKey, held.Token, Response{Status: 200}); err != nil {
		t.Fatal(err)
	}

	// The holder is done. A stale token cannot rewrite or release the response.
	if err := s.Complete(ctx, orderKey, held.Token, Response{Status: 500}); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("second Complete with a spent token = %v, want ErrNotHolder", err)
	}

	if err := s.Release(ctx, orderKey, held.Token); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("Release with a spent token = %v, want ErrNotHolder", err)
	}

	if got, _ := s.Begin(ctx, orderKey, "fp", time.Minute); got.State != Replay || got.Response.Status != 200 {
		t.Fatalf("stored response was disturbed: %+v", got)
	}
}

func TestReleasingAKeyThatIsGoneIsANoOp(t *testing.T) {
	s, _ := newTestStore()

	if err := s.Release(context.Background(), orderKey, 7); err != nil {
		t.Fatalf("Release of an unknown key = %v, want nil", err)
	}
}

func TestAHolderThatLostItsClaimToTheSweepCannotComplete(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	a, _ := s.Begin(ctx, orderKey, "fp", time.Minute)

	clock.Advance(2 * time.Minute)

	// An unrelated Begin sweeps A's lapsed claim away. Nobody retook the key.
	_, _ = s.Begin(ctx, Key{Principal: "bob", Scope: "s", Value: "other"}, "", time.Minute)

	if err := s.Complete(ctx, orderKey, a.Token, Response{Status: 200}); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("Complete after the claim was swept = %v, want ErrNotHolder", err)
	}
}

func TestAbandonedClaimsAreReclaimed(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	const abandoned = 1000

	for i := range abandoned {
		k := Key{Principal: "u", Scope: "s", Value: fmt.Sprintf("k%d", i)}
		// Differing leases, so the sweep cannot lean on insertion order.
		if got, _ := s.Begin(ctx, k, "", time.Duration(1+i%7)*time.Minute); got.State != Acquired {
			t.Fatalf("%v = %v, want Acquired", k, got.State)
		}
	}

	watched, _ := s.Begin(ctx, Key{Principal: "u", Scope: "s", Value: "k0"}, "", time.Minute)
	if watched.State != InFlight {
		t.Fatalf("k0 = %v, want InFlight before the leases lapse", watched.State)
	}

	if n := len(s.entries); n != abandoned {
		t.Fatalf("entries = %d before expiry, want %d", n, abandoned)
	}

	clock.Advance(time.Hour)

	live, _ := s.Begin(ctx, Key{Principal: "u", Scope: "s", Value: "fresh"}, "", time.Minute)
	if live.State != Acquired {
		t.Fatalf("fresh key = %v, want Acquired", live.State)
	}

	s.mu.Lock()
	entries, claims := len(s.entries), s.claims.Len()
	s.mu.Unlock()

	if entries != 1 || claims != 1 {
		t.Fatalf("after the sweep: %d entries, %d claims; want 1 and 1", entries, claims)
	}

	select {
	case <-watched.Done:
	default:
		t.Fatal("sweeping an abandoned claim did not close its Done")
	}
}

func TestCompletedEntriesLeaveTheLeaseQueue(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	a, _ := s.Begin(ctx, orderKey, "fp", time.Minute)
	b, _ := s.Begin(ctx, Key{Principal: "u", Scope: "s", Value: "b"}, "fp", time.Minute)
	_ = s.Complete(ctx, orderKey, a.Token, Response{Status: 200})
	_ = s.Release(ctx, Key{Principal: "u", Scope: "s", Value: "b"}, b.Token)

	s.mu.Lock()
	claims := s.claims.Len()
	s.mu.Unlock()

	if claims != 0 {
		t.Fatalf("lease queue holds %d claims after both finished, want 0", claims)
	}
}

func TestACompletedResponseWithoutAFingerprintInheritsTheHolders(t *testing.T) {
	s, _ := newTestStore()
	ctx := context.Background()

	first, _ := s.Begin(ctx, orderKey, "request-fp", time.Minute)
	_ = s.Complete(ctx, orderKey, first.Token, Response{Status: 200})

	got, _ := s.Begin(ctx, orderKey, "other", time.Minute)
	if got.State != Replay || got.Fingerprint != "request-fp" || got.Response.Fingerprint != "request-fp" {
		t.Fatalf("replay = %+v, want the holder's fingerprint %q", got, "request-fp")
	}

	// An explicit fingerprint wins over the holder's.
	s2, _ := newTestStore()
	first, _ = s2.Begin(ctx, orderKey, "request-fp", time.Minute)
	_ = s2.Complete(ctx, orderKey, first.Token, Response{Status: 200, Fingerprint: "explicit"})

	got, _ = s2.Begin(ctx, orderKey, "other", time.Minute)
	if got.Fingerprint != "explicit" {
		t.Fatalf("replay fingerprint = %q, want %q", got.Fingerprint, "explicit")
	}
}

func TestANonPositiveLeaseIsClamped(t *testing.T) {
	for _, lease := range []time.Duration{0, -time.Second} {
		t.Run(lease.String(), func(t *testing.T) {
			s, clock := newTestStore()
			ctx := context.Background()

			if got, _ := s.Begin(ctx, orderKey, "fp", lease); got.State != Acquired {
				t.Fatalf("first Begin = %v, want Acquired", got.State)
			}

			if got, _ := s.Begin(ctx, orderKey, "fp", time.Minute); got.State != InFlight {
				t.Fatalf("second Begin = %v, want InFlight: a claim must not be retakeable at once", got.State)
			}

			clock.Advance(DefaultLease + time.Second)

			if got, _ := s.Begin(ctx, orderKey, "fp", time.Minute); got.State != Acquired {
				t.Fatalf("Begin after DefaultLease = %v, want Acquired", got.State)
			}
		})
	}
}

func TestConcurrentBeginsOnOneKeyHaveExactlyOneWinner(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	const callers = 200

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		acquired int
		inFlight int
		other    int
		start    = make(chan struct{})
	)

	for range callers {
		wg.Go(func() {
			<-start

			got, err := s.Begin(ctx, orderKey, "fp", time.Minute)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err != nil:
				other++
			case got.State == Acquired:
				acquired++
			case got.State == InFlight:
				inFlight++
			default:
				other++
			}
		})
	}

	close(start)
	wg.Wait()

	if acquired != 1 || inFlight != callers-1 || other != 0 {
		t.Fatalf("acquired=%d inFlight=%d other=%d; want 1, %d, 0", acquired, inFlight, other, callers-1)
	}
}
