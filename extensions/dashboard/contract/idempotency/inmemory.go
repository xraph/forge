package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/forge/middleware"
)

// DefaultMaxEntries is the default cap for the store NewInMemoryStore builds.
const DefaultMaxEntries = middleware.DefaultIdempotencyMaxEntries

// SharedScope namespaces dashboard command keys inside a shared store, so a
// dashboard command and an HTTP route never collide even on one backend.
const SharedScope = "dashboard.command"

// lookupLease bounds the claim Lookup takes for an instant to read a key.
const lookupLease = time.Second

// storeWait is how long Store waits for a claim on its key to end.
const storeWait = 2 * lookupLease

// pollInterval is how often Store re-checks a claim whose store cannot signal
// when it ends.
const pollInterval = time.Millisecond

// Option configures NewInMemoryStore.
type Option func(*options)

type options struct{ maxEntries int }

// WithMaxEntries caps the number of cached entries; oldest are evicted first.
func WithMaxEntries(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.maxEntries = n
		}
	}
}

// InMemoryStore is the dashboard's Store as a view over a
// middleware.IdempotencyStore. NewInMemoryStore gives it a private in-memory
// backend; NewSharedStore lets it share one with the HTTP idempotency
// middleware. Safe for concurrent use.
type InMemoryStore struct {
	shared middleware.IdempotencyStore
	wait   time.Duration
}

// NewInMemoryStore returns a Store backed by its own in-memory
// middleware.IdempotencyStore.
func NewInMemoryStore(opts ...Option) *InMemoryStore {
	o := options{maxEntries: DefaultMaxEntries}
	for _, opt := range opts {
		opt(&o)
	}

	return NewSharedStore(middleware.NewMemoryIdempotencyStore(middleware.MemoryIdempotencyMaxEntries(o.maxEntries)))
}

// NewSharedStore returns a Store that keeps its entries in shared.
func NewSharedStore(shared middleware.IdempotencyStore) *InMemoryStore {
	return &InMemoryStore{shared: shared, wait: storeWait}
}

func sharedKey(key, identity string) middleware.IdempotencyKey {
	return middleware.IdempotencyKey{Principal: identity, Scope: SharedScope, Value: key}
}

// Lookup implements Store. A miss briefly claims the key and releases it at
// once, so it leaves nothing behind.
func (s *InMemoryStore) Lookup(ctx context.Context, key, identity string) (*Cached, bool) {
	k := sharedKey(key, identity)

	begun, err := s.shared.Begin(ctx, k, "", lookupLease)
	if err != nil {
		return nil, false
	}

	switch begun.State {
	case middleware.IdempotencyReplay:
		r := begun.Response
		c := Cached{Status: r.Status, WireBody: json.RawMessage(r.Body), StoredAt: r.StoredAt}

		if !r.ExpiresAt.IsZero() {
			c.TTL = r.ExpiresAt.Sub(r.StoredAt)
		}

		return &c, true
	case middleware.IdempotencyAcquired:
		_ = s.shared.Release(ctx, k, begun.Token)
	}

	return nil, false
}

// Store implements Store. A TTL of zero or less never expires, matching
// Cached.Expired.
func (s *InMemoryStore) Store(ctx context.Context, key, identity string, c Cached) error {
	var expires time.Time
	if c.TTL > 0 {
		expires = c.StoredAt.Add(c.TTL)
	}

	resp := middleware.IdempotentResponse{
		Status:    c.Status,
		Body:      []byte(c.WireBody),
		StoredAt:  c.StoredAt,
		ExpiresAt: expires,
	}

	return s.complete(ctx, sharedKey(key, identity), resp)
}

// complete stores resp over whatever the key holds. The old store let Store
// always win, so a refusal because a claim is live must not drop the entry.
// The only claims on a dashboard key are Lookup's momentary ones, so wait for
// the claim to end, or take the key once it is free, and give up when ctx ends
// or s.wait passes (a claim some other writer holds for long).
func (s *InMemoryStore) complete(ctx context.Context, k middleware.IdempotencyKey, resp middleware.IdempotentResponse) error {
	deadline := time.NewTimer(s.wait)
	defer deadline.Stop()

	for {
		err := s.shared.Complete(ctx, k, 0, resp)
		if !errors.Is(err, middleware.ErrIdempotencyNotHolder) {
			return err
		}

		begun, err := s.shared.Begin(ctx, k, "", lookupLease)
		if err != nil {
			return err
		}

		switch begun.State {
		case middleware.IdempotencyAcquired:
			// The claim ended between the two calls. Finish under our own.
			err := s.shared.Complete(ctx, k, begun.Token, resp)
			if err != nil {
				_ = s.shared.Release(ctx, k, begun.Token)
			}

			return err
		case middleware.IdempotencyReplay:
			continue // a response landed; the next Complete overwrites it
		}

		// InFlight: wait for the holder to finish, or poll when the store
		// cannot signal.
		poll := time.NewTimer(pollInterval)

		select {
		case <-begun.Done:
			poll.Stop()
		case <-poll.C:
		case <-ctx.Done():
			poll.Stop()

			return ctx.Err()
		case <-deadline.C:
			poll.Stop()

			return fmt.Errorf("idempotency: key is held by a live claim: %w", middleware.ErrIdempotencyNotHolder)
		}
	}
}

// Compile-time assertion.
var _ Store = (*InMemoryStore)(nil)
