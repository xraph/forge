package idempotency

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/internal/router"
)

func quotaKey(principal, v string) Key {
	return Key{Principal: principal, Scope: "POST /pay", Value: v}
}

// One principal used to be able to push every other principal's stored
// responses out of the shared LRU by sending fresh keys. A victim's retry then
// found nothing stored and ran the handler a second time.
func TestOnePrincipalCannotEvictAnother(t *testing.T) {
	s, _ := newTestStore(WithMaxEntries(100))
	ctx := context.Background()

	if err := s.Complete(ctx, quotaKey("victim", "k1"), 0, Response{Status: 201}); err != nil {
		t.Fatal(err)
	}

	refused := 0

	for i := range 1000 {
		got, err := s.Begin(ctx, quotaKey("attacker", fmt.Sprintf("a%d", i)), "", time.Minute)
		if _, ok := errors.AsType[*QuotaError](err); ok {
			refused++

			continue
		}

		if err != nil || got.State != Acquired {
			t.Fatalf("attacker Begin %d = %+v, %v", i, got, err)
		}

		if err := s.Complete(ctx, quotaKey("attacker", fmt.Sprintf("a%d", i)), got.Token, Response{Status: 201}); err != nil {
			t.Fatal(err)
		}
	}

	if refused != 990 {
		t.Fatalf("refused %d attacker keys, want 990 (a principal may hold a tenth of 100)", refused)
	}

	if got, _ := s.Begin(ctx, quotaKey("victim", "k1"), "", time.Minute); got.State != Replay {
		t.Fatalf("victim's key = %v, want Replay", got.State)
	}
}

func TestQuotaCountsClaimsAndFreesOnRelease(t *testing.T) {
	s, _ := newTestStore(WithMaxEntriesPerPrincipal(2))
	ctx := context.Background()

	a, _ := s.Begin(ctx, quotaKey("u", "a"), "", time.Minute)
	_, _ = s.Begin(ctx, quotaKey("u", "b"), "", time.Minute)

	if _, err := s.Begin(ctx, quotaKey("u", "c"), "", time.Minute); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("third claim = %v, want ErrQuotaExceeded", err)
	}

	// Asking about a key the principal already holds is not a new key.
	if got, err := s.Begin(ctx, quotaKey("u", "a"), "", time.Minute); err != nil || got.State != InFlight {
		t.Fatalf("existing key = %+v, %v; want InFlight", got, err)
	}

	// Another principal is unaffected.
	if got, err := s.Begin(ctx, quotaKey("v", "c"), "", time.Minute); err != nil || got.State != Acquired {
		t.Fatalf("other principal = %+v, %v; want Acquired", got, err)
	}

	_ = s.Release(ctx, quotaKey("u", "a"), a.Token)

	if got, err := s.Begin(ctx, quotaKey("u", "c"), "", time.Minute); err != nil || got.State != Acquired {
		t.Fatalf("after a release = %+v, %v; want Acquired", got, err)
	}
}

func TestQuotaFreesWhenResponsesExpire(t *testing.T) {
	s, clock := newTestStore(WithMaxEntriesPerPrincipal(2))
	ctx := context.Background()
	expires := clock.Now().Add(time.Hour)

	for _, v := range []string{"a", "b"} {
		if err := s.Complete(ctx, quotaKey("u", v), 0, Response{Status: 200, ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}

	_, err := s.Begin(ctx, quotaKey("u", "c"), "", time.Minute)

	var qe *QuotaError
	if !errors.As(err, &qe) {
		t.Fatalf("Begin at quota = %v, want *QuotaError", err)
	}

	if qe.RetryAfter != time.Hour || qe.Principal != "u" {
		t.Fatalf("QuotaError = %+v, want principal u and RetryAfter 1h", qe)
	}

	clock.Advance(30 * time.Minute)

	if _, err := s.Begin(ctx, quotaKey("u", "c"), "", time.Minute); !errors.As(err, &qe) || qe.RetryAfter != 30*time.Minute {
		t.Fatalf("Begin half way = %v, want *QuotaError with RetryAfter 30m", err)
	}

	clock.Advance(30 * time.Minute)

	if got, err := s.Begin(ctx, quotaKey("u", "c"), "", time.Minute); err != nil || got.State != Acquired {
		t.Fatalf("Begin after expiry = %+v, %v; want Acquired", got, err)
	}

	s.mu.Lock()
	entries := len(s.entries)
	s.mu.Unlock()

	if entries != 1 {
		t.Fatalf("entries = %d, want only the new claim", entries)
	}
}

func TestQuotaCountsBytes(t *testing.T) {
	s, _ := newTestStore(WithMaxBytesPerPrincipal(4<<10), WithMaxEntriesPerPrincipal(-1))
	ctx := context.Background()
	body := []byte(strings.Repeat("x", 3<<10))

	if err := s.Complete(ctx, quotaKey("u", "a"), 0, Response{Status: 200, Body: body}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Begin(ctx, quotaKey("u", "b"), "", time.Minute); err != nil {
		t.Fatalf("under the byte quota = %v", err)
	}

	if err := s.Complete(ctx, quotaKey("u", "b"), 0, Response{Status: 200, Body: body}); err == nil {
		t.Fatal("Complete without the token should be refused while b is claimed")
	}

	if err := s.Complete(ctx, quotaKey("u", "c"), 0, Response{Status: 200, Body: body}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Begin(ctx, quotaKey("u", "d"), "", time.Minute); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("over the byte quota = %v, want ErrQuotaExceeded", err)
	}
}

func TestGlobalByteBudgetEvictsLeastRecentlyUsed(t *testing.T) {
	s, _ := newTestStore(WithMaxBytes(10<<10), WithMaxBytesPerPrincipal(-1), WithMaxEntriesPerPrincipal(-1))
	ctx := context.Background()
	body := []byte(strings.Repeat("x", 4<<10))

	for _, v := range []string{"a", "b", "c"} {
		if err := s.Complete(ctx, quotaKey("u", v), 0, Response{Status: 200, Body: body}); err != nil {
			t.Fatal(err)
		}
	}

	if got, _ := s.Begin(ctx, quotaKey("u", "a"), "", time.Minute); got.State != Acquired {
		t.Fatalf("a = %v, want Acquired (evicted to stay under the byte budget)", got.State)
	}

	s.mu.Lock()
	used := s.bytes
	s.mu.Unlock()

	if used > 10<<10 {
		t.Fatalf("store holds %d bytes, over the 10 KiB budget", used)
	}
}

func TestAccountingReturnsToZero(t *testing.T) {
	s, clock := newTestStore()
	ctx := context.Background()

	for i := range 50 {
		k := quotaKey(fmt.Sprintf("p%d", i%5), fmt.Sprintf("k%d", i))
		got, _ := s.Begin(ctx, k, "", time.Minute)

		switch i % 3 {
		case 0:
			_ = s.Complete(ctx, k, got.Token, Response{Status: 200, Body: []byte("abc"), ExpiresAt: clock.Now().Add(time.Second)})
		case 1:
			_ = s.Release(ctx, k, got.Token)
		}
	}

	clock.Advance(time.Hour)

	// Looking every key up drops expired responses; the sweep drops the
	// lapsed claims.
	for i := range 50 {
		k := quotaKey(fmt.Sprintf("p%d", i%5), fmt.Sprintf("k%d", i))
		if got, _ := s.Begin(ctx, k, "", time.Minute); got.State == Acquired {
			_ = s.Release(ctx, k, got.Token)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bytes != 0 || len(s.usage) != 0 || len(s.entries) != 0 {
		t.Fatalf("bytes=%d usage=%d entries=%d, want all zero", s.bytes, len(s.usage), len(s.entries))
	}
}

// Through the middleware, the PoC from the review: a victim stores one
// response, an attacker sends many fresh keys, and the victim retries.
func TestMiddleware_OverQuotaPrincipalGets429AndVictimReplays(t *testing.T) {
	store := NewMemoryStore(WithMaxEntries(100))

	var victimCharges atomic.Int32

	r := router.NewRouter()
	r.Use(func(next router.Handler) router.Handler {
		return func(ctx router.Context) error {
			ctx.Set("auth.subject", ctx.Request().Header.Get("X-Sub"))

			return next(ctx)
		}
	})

	if err := r.POST("/pay", func(ctx router.Context) error {
		if ctx.Request().Header.Get("X-Sub") == "victim" {
			victimCharges.Add(1)
		}

		return ctx.String(http.StatusCreated, strings.Repeat("x", 1000))
	}, router.WithMiddleware(Middleware(store))); err != nil {
		t.Fatal(err)
	}

	do := func(sub, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/pay", strings.NewReader(`{}`))
		req.Header.Set("X-Sub", sub)
		req.Header.Set(HeaderName, key)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		return rec
	}

	do("victim", "k1")

	var last *httptest.ResponseRecorder
	for i := range 100 {
		last = do("attacker", fmt.Sprintf("a%d", i))
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("attacker over quota got %d, want 429", last.Code)
	}

	if ra := last.Header().Get("Retry-After"); ra != "86400" {
		t.Fatalf("Retry-After = %q, want the 24h TTL in seconds", ra)
	}

	if !strings.Contains(last.Body.String(), "too many idempotent requests") {
		t.Fatalf("429 body = %s", last.Body.String())
	}

	if rec := do("victim", "k1"); rec.Header().Get(ReplayedHeader) != "true" {
		t.Fatalf("victim retry was not replayed: %d %v", rec.Code, rec.Header())
	}

	if n := victimCharges.Load(); n != 1 {
		t.Fatalf("victim charged %d times for one key, want 1", n)
	}
}
