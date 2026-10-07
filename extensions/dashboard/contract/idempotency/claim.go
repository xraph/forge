package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/forge/middleware"
)

// Claimer is a Store that can also hold a key while a command runs, so two
// overlapping dispatches with the same key and identity never both run it.
// The dispatcher finds it by type assertion; a plain Store keeps working
// without it.
type Claimer interface {
	Store

	// Claim takes (key, identity) for the caller. While another caller holds
	// the key it waits for that claim to end, until ctx ends. Exactly one of
	// the returned Claim's fields is set: Cached when an entry is stored for
	// the key, End when the caller now holds it. When ctx ends with the key
	// still held, the error wraps ErrClaimHeld.
	Claim(ctx context.Context, key, identity string) (Claim, error)
}

// Claim is what Claimer.Claim found.
type Claim struct {
	// Cached is the entry already stored for the key.
	Cached *Cached
	// End ends a claim the caller holds. Pass the entry to store under the
	// claim, or nil to store nothing and give the key back. A caller that
	// holds a claim must call End exactly once. When the claim lapsed before
	// End, End stores nothing and its error wraps ErrClaimLost.
	End func(ctx context.Context, c *Cached) error
}

// ErrClaimHeld is what Claim's error wraps when ctx ended while another
// caller still held the key.
var ErrClaimHeld = errors.New("idempotency: key is held by a running command")

// ErrClaimLost is what End's error wraps when the claim's lease lapsed before
// End, so the claim was swept or passed to another caller and End stored
// nothing. The error also wraps middleware.ErrIdempotencyNotHolder.
var ErrClaimLost = errors.New("idempotency: claim lapsed before it ended")

// claimLease bounds how long a command's claim survives a holder that never
// ends it, such as one whose process died. It matches the HTTP idempotency
// middleware's default lease. A handler that runs longer loses the claim, and
// its End then stores nothing.
const claimLease = time.Minute

// claimPoll is how often Claim asks again while it waits on a store that
// cannot signal when a claim ends. It matches the HTTP middleware's interval.
const claimPoll = 25 * time.Millisecond

// Claim implements Claimer over the shared store's own Begin, Complete and
// Release, so a dashboard command's claim is the same kind of claim an HTTP
// request holds.
func (s *InMemoryStore) Claim(ctx context.Context, key, identity string) (Claim, error) {
	k := sharedKey(key, identity)

	// lastDone is the Done the previous InFlight carried. A store that hands
	// back the same Done already closed while the key is still held would
	// make awaitClaim return at once every time, so Claim would spin until
	// ctx ends; such a store is waited on by the timer instead.
	var lastDone <-chan struct{}

	for {
		begun, err := s.shared.Begin(ctx, k, "", s.lease)
		if err != nil {
			return Claim{}, err
		}

		switch begun.State {
		case middleware.IdempotencyAcquired:
			return Claim{End: s.ender(k, begun.Token)}, nil
		case middleware.IdempotencyReplay:
			if begun.Response == nil {
				return Claim{}, errors.New("idempotency: store returned Replay without a response")
			}

			c := fromResponse(*begun.Response)

			return Claim{Cached: &c}, nil
		case middleware.IdempotencyInFlight:
			done := begun.Done
			if done != nil && done == lastDone && isClosed(done) {
				done = nil
			}

			lastDone = begun.Done

			if err := awaitClaim(ctx, done); err != nil {
				return Claim{}, err
			}
		default:
			return Claim{}, fmt.Errorf("idempotency: store returned unknown state %d", begun.State)
		}
	}
}

// awaitClaim blocks until it is worth calling Begin again: the holder ended
// its claim (done closed), or one interval passed. The interval is
// lookupLease for a store that signals, because a store may close done only
// at its next sweep after a lease lapses, and claimPoll for one that cannot.
// When ctx ends first it returns an error wrapping ErrClaimHeld.
func awaitClaim(ctx context.Context, done <-chan struct{}) error {
	interval := lookupLease
	if done == nil {
		interval = claimPoll
	}

	retry := time.NewTimer(interval)
	defer retry.Stop()

	select {
	case <-done: // nil blocks forever, so a poll-only store uses the timer
	case <-retry.C:
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrClaimHeld, ctx.Err())
	}

	return nil
}

// isClosed reports whether done is closed, without blocking.
func isClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// ender returns the End for a claim held under token. Storing completes the
// claim with the entry. If that fails the claim is released, so a failure
// never leaves the key held until its lease lapses. ErrNotHolder means the
// lease already lapsed and the key moved on. Nothing is stored then, and the
// error wraps ErrClaimLost so the caller can store the entry another way.
func (s *InMemoryStore) ender(k middleware.IdempotencyKey, token middleware.IdempotencyToken) func(context.Context, *Cached) error {
	return func(ctx context.Context, c *Cached) error {
		if c == nil {
			return s.shared.Release(ctx, k, token)
		}

		err := s.shared.Complete(ctx, k, token, toResponse(*c))
		if err == nil {
			return nil
		}

		_ = s.shared.Release(ctx, k, token)

		if errors.Is(err, middleware.ErrIdempotencyNotHolder) {
			return fmt.Errorf("%w: %w", ErrClaimLost, err)
		}

		return err
	}
}

// Compile-time assertion.
var _ Claimer = (*InMemoryStore)(nil)
