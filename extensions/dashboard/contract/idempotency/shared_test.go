package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/middleware"
)

func TestSharedStoreWritesThroughToTheSharedBackend(t *testing.T) {
	shared := middleware.NewMemoryIdempotencyStore()
	s := NewSharedStore(shared)
	ctx := context.Background()

	if err := s.Store(ctx, "k", "alice:orders.create", Cached{
		Status:   200,
		WireBody: json.RawMessage(`{"ok":true}`),
		StoredAt: time.Now(),
		TTL:      time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := shared.Begin(ctx, middleware.IdempotencyKey{
		Principal: "alice:orders.create",
		Scope:     SharedScope,
		Value:     "k",
	}, "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if got.State != middleware.IdempotencyReplay || string(got.Response.Body) != `{"ok":true}` {
		t.Fatalf("shared backend = %+v, want a replay of the dashboard's envelope", got)
	}
}

func TestSharedStoreLookupLeavesNoClaimBehind(t *testing.T) {
	shared := middleware.NewMemoryIdempotencyStore()
	s := NewSharedStore(shared)
	ctx := context.Background()

	if _, ok := s.Lookup(ctx, "k", "u"); ok {
		t.Fatal("expected a miss")
	}

	got, _ := shared.Begin(ctx, middleware.IdempotencyKey{Principal: "u", Scope: SharedScope, Value: "k"}, "", time.Minute)
	if got.State != middleware.IdempotencyAcquired {
		t.Fatalf("a missed Lookup left the key in state %v, want it free", got.State)
	}
}

func TestSharedStoreKeepsTheTTLItWasGiven(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	stored := time.Now()

	_ = s.Store(ctx, "k", "u", Cached{Status: 200, WireBody: json.RawMessage(`1`), StoredAt: stored, TTL: 2 * time.Hour})

	got, ok := s.Lookup(ctx, "k", "u")
	if !ok {
		t.Fatal("expected a hit")
	}

	if got.TTL != 2*time.Hour || !got.StoredAt.Equal(stored) {
		t.Fatalf("Cached = %+v, want StoredAt %v and TTL 2h", got, stored)
	}
}

func TestSharedStoreZeroTTLNeverExpires(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	_ = s.Store(ctx, "k", "u", Cached{Status: 200, WireBody: json.RawMessage(`1`), StoredAt: time.Now().Add(-1000 * time.Hour)})

	got, ok := s.Lookup(ctx, "k", "u")
	if !ok || got.TTL != 0 {
		t.Fatalf("got %+v, %v; want a hit with TTL 0", got, ok)
	}
}

func TestSharedStoreOverwritesAnExistingEntry(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now()

	_ = s.Store(ctx, "k", "u", Cached{Status: 200, WireBody: json.RawMessage(`1`), StoredAt: now, TTL: time.Hour})

	if err := s.Store(ctx, "k", "u", Cached{Status: 201, WireBody: json.RawMessage(`2`), StoredAt: now, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}

	got, _ := s.Lookup(ctx, "k", "u")
	if got == nil || got.Status != 201 || string(got.WireBody) != `2` {
		t.Fatalf("got %+v, want the second Store to win, as the old store did", got)
	}
}

// gatedRelease is a shared store whose Release waits on a gate and whose
// Complete reports the errors it returns, so a test can hold a Lookup's
// momentary claim open while a Store arrives.
type gatedRelease struct {
	middleware.IdempotencyStore

	released chan struct{} // closed when Release is entered
	gate     chan struct{} // Release proceeds when this closes
	rejected chan struct{} // receives once per ErrNotHolder from Complete
	once     sync.Once
}

func newGatedRelease(inner middleware.IdempotencyStore) *gatedRelease {
	return &gatedRelease{
		IdempotencyStore: inner,
		released:         make(chan struct{}),
		gate:             make(chan struct{}),
		rejected:         make(chan struct{}, 64),
	}
}

func (g *gatedRelease) Release(ctx context.Context, k middleware.IdempotencyKey, tok middleware.IdempotencyToken) error {
	g.once.Do(func() { close(g.released) })
	<-g.gate

	return g.IdempotencyStore.Release(ctx, k, tok)
}

func (g *gatedRelease) Complete(ctx context.Context, k middleware.IdempotencyKey, tok middleware.IdempotencyToken, r middleware.IdempotentResponse) error {
	err := g.IdempotencyStore.Complete(ctx, k, tok, r)
	if errors.Is(err, middleware.ErrIdempotencyNotHolder) {
		select {
		case g.rejected <- struct{}{}:
		default:
		}
	}

	return err
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// A Store that arrives while a concurrent Lookup holds its momentary claim
// must still land. The old store let Store always win, so the dispatcher
// relies on it never being silently dropped.
func TestSharedStoreSurvivesALookupClaimOnTheSameKey(t *testing.T) {
	gated := newGatedRelease(middleware.NewMemoryIdempotencyStore())
	s := NewSharedStore(gated)
	ctx := context.Background()

	lookupDone := make(chan struct{})

	go func() {
		defer close(lookupDone)

		s.Lookup(ctx, "k", "u")
	}()

	waitFor(t, gated.released, "the Lookup to reach Release")

	storeErr := make(chan error, 1)

	go func() {
		storeErr <- s.Store(ctx, "k", "u", Cached{Status: 200, WireBody: json.RawMessage(`{"n":1}`), StoredAt: time.Now(), TTL: time.Hour})
	}()

	// The Store has now met the live claim and been refused by the backend.
	waitFor(t, gated.rejected, "the backend to refuse Store over the claim")

	close(gated.gate)
	waitFor(t, lookupDone, "the Lookup to finish")

	select {
	case err := <-storeErr:
		if err != nil {
			t.Fatalf("Store = %v, want nil once the claim ended", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Store never returned")
	}

	got, ok := s.Lookup(ctx, "k", "u")
	if !ok || string(got.WireBody) != `{"n":1}` {
		t.Fatalf("Lookup = %+v, %v; the Store that raced a Lookup was lost", got, ok)
	}
}

func TestSharedStoreGivesUpOnAForeignClaimWithTheContext(t *testing.T) {
	shared := middleware.NewMemoryIdempotencyStore()
	s := NewSharedStore(shared)

	held, err := shared.Begin(context.Background(), sharedKey("k", "u"), "", time.Hour)
	if err != nil || held.State != middleware.IdempotencyAcquired {
		t.Fatalf("setup Begin = %+v, %v", held, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = s.Store(ctx, "k", "u", Cached{Status: 200, WireBody: json.RawMessage(`1`), StoredAt: time.Now(), TTL: time.Hour})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Store = %v, want the context's deadline", err)
	}
}

func TestSharedStoreGivesUpOnAForeignClaimAfterItsWait(t *testing.T) {
	shared := middleware.NewMemoryIdempotencyStore()
	s := NewSharedStore(shared)
	s.wait = 30 * time.Millisecond

	held, _ := shared.Begin(context.Background(), sharedKey("k", "u"), "", time.Hour)
	if held.State != middleware.IdempotencyAcquired {
		t.Fatalf("setup Begin = %+v", held)
	}

	err := s.Store(context.Background(), "k", "u", Cached{Status: 200, WireBody: json.RawMessage(`1`), StoredAt: time.Now(), TTL: time.Hour})
	if !errors.Is(err, middleware.ErrIdempotencyNotHolder) {
		t.Fatalf("Store = %v, want ErrIdempotencyNotHolder", err)
	}

	// The foreign claim is untouched.
	again, _ := shared.Begin(context.Background(), sharedKey("k", "u"), "", time.Minute)
	if again.State != middleware.IdempotencyInFlight {
		t.Fatalf("foreign claim state = %v, want it still in flight", again.State)
	}
}

func TestSharedStoreMissedLookupDoesNotReleaseSomeoneElsesClaim(t *testing.T) {
	shared := middleware.NewMemoryIdempotencyStore()
	s := NewSharedStore(shared)

	held, _ := shared.Begin(context.Background(), sharedKey("k", "u"), "", time.Hour)
	if held.State != middleware.IdempotencyAcquired {
		t.Fatalf("setup Begin = %+v", held)
	}

	// The key is in flight, so Lookup misses without a claim of its own.
	if _, ok := s.Lookup(context.Background(), "k", "u"); ok {
		t.Fatal("expected a miss")
	}

	if err := shared.Complete(context.Background(), sharedKey("k", "u"), held.Token, middleware.IdempotentResponse{Status: 200, Body: []byte(`1`)}); err != nil {
		t.Fatalf("the holder's Complete = %v, a Lookup must not disturb a claim it does not own", err)
	}
}
