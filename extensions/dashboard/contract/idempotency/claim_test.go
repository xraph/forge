package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/middleware"
)

func mustClaim(t *testing.T, s *InMemoryStore, ctx context.Context) Claim {
	t.Helper()

	c, err := s.Claim(ctx, "k", "u")
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if c.End == nil || c.Cached != nil {
		t.Fatalf("Claim = %+v, want a held claim", c)
	}

	return c
}

func TestClaimHoldsTheKeyUntilEnd(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	held := mustClaim(t, s, ctx)

	// A Lookup sees a running command, not an entry.
	if _, ok := s.Lookup(ctx, "k", "u"); ok {
		t.Fatal("Lookup hit while the key was claimed")
	}

	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()

	_, err := s.Claim(short, "k", "u")
	if !errors.Is(err, ErrClaimHeld) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Claim = %v, want ErrClaimHeld wrapping the deadline", err)
	}

	stored := time.Now()
	if err := held.End(ctx, &Cached{Status: 200, WireBody: json.RawMessage(`{"n":1}`), StoredAt: stored, TTL: time.Hour}); err != nil {
		t.Fatalf("End = %v", err)
	}

	again, err := s.Claim(ctx, "k", "u")
	if err != nil {
		t.Fatalf("Claim after End = %v", err)
	}

	if again.End != nil || again.Cached == nil {
		t.Fatalf("Claim after End = %+v, want the stored entry", again)
	}

	if got := again.Cached; got.Status != 200 || string(got.WireBody) != `{"n":1}` || got.TTL != time.Hour || !got.StoredAt.Equal(stored) {
		t.Fatalf("Cached = %+v, want what End stored", got)
	}

	if got, ok := s.Lookup(ctx, "k", "u"); !ok || string(got.WireBody) != `{"n":1}` {
		t.Fatalf("Lookup = %+v, %v; want the stored entry", got, ok)
	}
}

func TestClaimEndWithNilGivesTheKeyBack(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	if err := mustClaim(t, s, ctx).End(ctx, nil); err != nil {
		t.Fatalf("End(nil) = %v", err)
	}

	if _, ok := s.Lookup(ctx, "k", "u"); ok {
		t.Fatal("a released claim left an entry behind")
	}

	mustClaim(t, s, ctx)
}

// signalInFlight reports each Begin that found the key held, so a test knows
// a Claim is waiting.
type signalInFlight struct {
	middleware.IdempotencyStore

	inFlight chan struct{}
}

func (s signalInFlight) Begin(ctx context.Context, k middleware.IdempotencyKey, fp string, lease time.Duration) (middleware.IdempotencyBegun, error) {
	b, err := s.IdempotencyStore.Begin(ctx, k, fp, lease)
	if err == nil && b.State == middleware.IdempotencyInFlight {
		select {
		case s.inFlight <- struct{}{}:
		default:
		}
	}

	return b, err
}

type claimResult struct {
	claim Claim
	err   error
}

func TestClaimWaiterGetsTheEntryTheHolderStores(t *testing.T) {
	shared := signalInFlight{middleware.NewMemoryIdempotencyStore(), make(chan struct{}, 1)}
	s := NewSharedStore(shared)
	ctx := context.Background()

	held := mustClaim(t, s, ctx)

	waiter := make(chan claimResult, 1)

	go func() {
		c, err := s.Claim(ctx, "k", "u")
		waiter <- claimResult{c, err}
	}()

	waitFor(t, shared.inFlight, "the waiter to find the key claimed")

	if err := held.End(ctx, &Cached{Status: 409, StoredAt: time.Now(), TTL: time.Hour}); err != nil {
		t.Fatalf("End = %v", err)
	}

	select {
	case r := <-waiter:
		if r.err != nil {
			t.Fatalf("waiter Claim = %v", r.err)
		}

		if c := r.claim; c.Cached == nil || c.Cached.Status != 409 || c.End != nil {
			t.Fatalf("waiter Claim = %+v, want the holder's tombstone", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never woke")
	}
}

func TestClaimWaiterTakesTheKeyAfterARelease(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	held := mustClaim(t, s, ctx)

	go func() {
		time.Sleep(10 * time.Millisecond)

		_ = held.End(ctx, nil)
	}()

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	mustClaim(t, s, waitCtx)
}

func TestClaimPollsWhenTheStoreCannotSignal(t *testing.T) {
	inner := middleware.NewMemoryIdempotencyStore()
	s := NewSharedStore(pollingStore{inner})
	ctx := context.Background()

	held, _ := inner.Begin(ctx, sharedKey("k", "u"), "", time.Hour)
	if held.State != middleware.IdempotencyAcquired {
		t.Fatalf("setup Begin = %+v", held)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)

		_ = inner.Release(ctx, sharedKey("k", "u"), held.Token)
	}()

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	mustClaim(t, s, waitCtx)
}

// A holder whose lease lapsed must not overwrite the claim that replaced it.
func TestClaimEndAfterTheLeaseLapsedStoresNothing(t *testing.T) {
	var (
		mu  sync.Mutex
		now = time.Now()
	)

	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()

		return now
	}

	s := NewSharedStore(middleware.NewMemoryIdempotencyStore(middleware.MemoryIdempotencyClock(clock)))
	ctx := context.Background()

	stale := mustClaim(t, s, ctx)

	mu.Lock()
	now = now.Add(2 * claimLease)
	mu.Unlock()

	fresh := mustClaim(t, s, ctx) // the lapsed claim is swept

	err := stale.End(ctx, &Cached{Status: 200, WireBody: json.RawMessage(`"stale"`), StoredAt: clock(), TTL: time.Hour})
	if !errors.Is(err, middleware.ErrIdempotencyNotHolder) || !errors.Is(err, ErrClaimLost) {
		t.Fatalf("stale End = %v, want ErrClaimLost wrapping ErrIdempotencyNotHolder", err)
	}

	if err := fresh.End(ctx, &Cached{Status: 200, WireBody: json.RawMessage(`"fresh"`), StoredAt: clock(), TTL: time.Hour}); err != nil {
		t.Fatalf("fresh End = %v, want the stale holder to have left its claim alone", err)
	}

	if got, ok := s.Lookup(ctx, "k", "u"); !ok || string(got.WireBody) != `"fresh"` {
		t.Fatalf("Lookup = %+v, %v; want the fresh holder's entry", got, ok)
	}
}

// failingComplete refuses every Complete with an error of its own.
type failingComplete struct{ middleware.IdempotencyStore }

var errBackend = errors.New("backend write failed")

func (failingComplete) Complete(context.Context, middleware.IdempotencyKey, middleware.IdempotencyToken, middleware.IdempotentResponse) error {
	return errBackend
}

func TestClaimEndReleasesWhenTheStoreFails(t *testing.T) {
	s := NewSharedStore(failingComplete{middleware.NewMemoryIdempotencyStore()})
	ctx := context.Background()

	err := mustClaim(t, s, ctx).End(ctx, &Cached{Status: 200, StoredAt: time.Now(), TTL: time.Hour})
	if !errors.Is(err, errBackend) {
		t.Fatalf("End = %v, want the backend's error", err)
	}

	// The key is free again rather than held until the lease lapses.
	mustClaim(t, s, ctx)
}

// replayWithoutResponse answers every Begin with a Replay that carries
// nothing.
type replayWithoutResponse struct{ middleware.IdempotencyStore }

func (replayWithoutResponse) Begin(context.Context, middleware.IdempotencyKey, string, time.Duration) (middleware.IdempotencyBegun, error) {
	return middleware.IdempotencyBegun{State: middleware.IdempotencyReplay}, nil
}

func TestClaimRefusesAReplayWithoutAResponse(t *testing.T) {
	s := NewSharedStore(replayWithoutResponse{middleware.NewMemoryIdempotencyStore()})

	if _, err := s.Claim(context.Background(), "k", "u"); err == nil {
		t.Fatal("Claim = nil error, want a refusal of the empty Replay")
	}
}

func TestClaimIsPerIdentity(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	mustClaim(t, s, ctx)

	other, err := s.Claim(ctx, "k", "someone-else")
	if err != nil || other.End == nil {
		t.Fatalf("Claim for another identity = %+v, %v; want its own claim", other, err)
	}
}

// closedDoneInFlight answers every Begin with InFlight and the same Done,
// already closed: a store bug that would make a naive waiter spin.
type closedDoneInFlight struct {
	middleware.IdempotencyStore

	done   chan struct{}
	begins atomic.Int64
}

func (s *closedDoneInFlight) Begin(context.Context, middleware.IdempotencyKey, string, time.Duration) (middleware.IdempotencyBegun, error) {
	s.begins.Add(1)

	return middleware.IdempotencyBegun{State: middleware.IdempotencyInFlight, Done: s.done}, nil
}

func TestClaimDoesNotSpinOnAClosedDone(t *testing.T) {
	store := &closedDoneInFlight{IdempotencyStore: middleware.NewMemoryIdempotencyStore(), done: make(chan struct{})}
	close(store.done)

	s := NewSharedStore(store)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := s.Claim(ctx, "k", "u")
	if !errors.Is(err, ErrClaimHeld) {
		t.Fatalf("Claim = %v, want ErrClaimHeld once the context ends", err)
	}

	// At claimPoll (25ms) over 100ms that is a handful of Begins. Spinning on
	// the closed Done would make thousands.
	if n := store.begins.Load(); n > 10 {
		t.Fatalf("Claim made %d Begin calls against a closed Done, want a handful", n)
	}
}
