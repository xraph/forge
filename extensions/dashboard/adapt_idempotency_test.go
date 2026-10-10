package dashboard_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
)

// The exported adapter is what another extension's tests wire a dispatcher
// with, so these tests use it from outside the package.

func TestAdaptIdempotencyStore_ClaimsWhenTheStoreCan(t *testing.T) {
	ctx := context.Background()

	claimer, ok := dashboard.AdaptIdempotencyStore(idempotency.NewInMemoryStore()).(dispatcher.IdempotencyClaimer)
	if !ok {
		t.Fatal("the in-memory store's adapter is not a dispatcher.IdempotencyClaimer")
	}

	held, err := claimer.Claim(ctx, "k1", "alice:keys.create")
	if err != nil || held.End == nil {
		t.Fatalf("first claim = %+v, %v; want to hold the key", held, err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	if _, err := claimer.Claim(waitCtx, "k1", "alice:keys.create"); !errors.Is(err, dispatcher.ErrIdempotencyClaimHeld) {
		t.Fatalf("second claim err = %v, want ErrIdempotencyClaimHeld", err)
	}

	stored := &dispatcher.IdempotencyCached{Status: dispatcher.TombstoneStatus, StoredAt: time.Now(), TTL: time.Hour}
	if err := held.End(ctx, stored); err != nil {
		t.Fatalf("end: %v", err)
	}

	again, err := claimer.Claim(ctx, "k1", "alice:keys.create")
	if err != nil || again.Cached == nil || again.Cached.Status != dispatcher.TombstoneStatus {
		t.Fatalf("claim after end = %+v, %v; want the stored tombstone", again, err)
	}
}

func TestAdaptIdempotencyStore_DoesNotClaimWhenTheStoreCannot(t *testing.T) {
	ctx := context.Background()
	inner := &mapStore{entries: map[string]idempotency.Cached{}}

	adapted := dashboard.AdaptIdempotencyStore(inner)
	if _, ok := adapted.(dispatcher.IdempotencyClaimer); ok {
		t.Fatal("a store that cannot claim was adapted into a claimer")
	}

	in := dispatcher.IdempotencyCached{Status: 200, WireBody: []byte(`{"ok":true}`), StoredAt: time.Now(), TTL: time.Hour}
	if err := adapted.Store(ctx, "k1", "alice:users.disable", in); err != nil {
		t.Fatalf("store: %v", err)
	}

	out, hit := adapted.Lookup(ctx, "k1", "alice:users.disable")
	if !hit || out.Status != in.Status || string(out.WireBody) != string(in.WireBody) || !out.StoredAt.Equal(in.StoredAt) || out.TTL != in.TTL {
		t.Fatalf("lookup = %+v, %v; want %+v", out, hit, in)
	}
}

// mapStore is an idempotency.Store with no Claim.
type mapStore struct{ entries map[string]idempotency.Cached }

func (s *mapStore) Lookup(_ context.Context, key, identity string) (*idempotency.Cached, bool) {
	c, ok := s.entries[identity+"|"+key]
	if !ok {
		return nil, false
	}

	return &c, true
}

func (s *mapStore) Store(_ context.Context, key, identity string, c idempotency.Cached) error {
	s.entries[identity+"|"+key] = c

	return nil
}

func TestAdaptIdempotencyStore_BoundRoundTrips(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"format":"forge.dashboard.idempotency","version":1,"binding":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","response":{"ok":true,"envelope":"v1","kind":"command","data":{"kept":true},"meta":{"intentVersion":1}}}`)

	for _, claiming := range []bool{false, true} {
		store := dashboard.AdaptIdempotencyStore(idempotency.NewInMemoryStore())

		in := dispatcher.IdempotencyCached{Status: dispatcher.TombstoneStatus, WireBody: body, StoredAt: time.Now(), TTL: time.Hour}
		if claiming {
			claim, err := store.(dispatcher.IdempotencyClaimer).Claim(ctx, "k", "u")
			if err != nil {
				t.Fatal(err)
			}

			if err := claim.End(ctx, &in); err != nil {
				t.Fatal(err)
			}
		} else if err := store.Store(ctx, "k", "u", in); err != nil {
			t.Fatal(err)
		}

		out, hit := store.Lookup(ctx, "k", "u")
		if !hit || out.Status != in.Status || string(out.WireBody) != string(body) || !out.StoredAt.Equal(in.StoredAt) || out.TTL != in.TTL {
			t.Fatalf("lookup=%+v", out)
		}

		replay, err := store.(dispatcher.IdempotencyClaimer).Claim(ctx, "k", "u")
		if err != nil || replay.End != nil || replay.Cached == nil || string(replay.Cached.WireBody) != string(body) || replay.Cached.Status != in.Status || !replay.Cached.StoredAt.Equal(in.StoredAt) || replay.Cached.TTL != in.TTL {
			t.Fatalf("claim=%+v err=%v", replay, err)
		}
	}
}

type nilRecordStore struct{ *mapStore }

func (s nilRecordStore) Lookup(context.Context, string, string) (*idempotency.Cached, bool) {
	return nil, true
}
func TestAdaptIdempotencyStore_PreservesNilHit(t *testing.T) {
	got, hit := dashboard.AdaptIdempotencyStore(nilRecordStore{&mapStore{}}).Lookup(context.Background(), "k", "u")
	if got != nil || !hit {
		t.Fatal("nil hit became a miss")
	}
}
