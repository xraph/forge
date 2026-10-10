package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/middleware"
)

const claimRaw = "sk_live_minted_once"

// mintRig is keysmith's keys.create on a dispatcher over a production store.
// Its handler signals started and then waits for gate.
type mintRig struct {
	d       *dispatcher.Dispatcher
	calls   atomic.Int64
	started chan struct{}
	gate    chan struct{}
	fail    func(call int64) error
}

func newMintRig(t *testing.T, store idempotency.Store, dispOpts []dispatcher.Option, regOpts ...dispatcher.RegisterOption) *mintRig {
	t.Helper()

	opts := append([]dispatcher.Option{dispatcher.WithIdempotencyStore(AdaptIdempotencyStore(store))}, dispOpts...)
	r := &mintRig{
		d:       dispatcher.NewWithOptions(dispatcher.NoopMetricsEmitter{}, opts...),
		started: make(chan struct{}, 64),
		gate:    make(chan struct{}),
		fail:    func(int64) error { return nil },
	}

	type out struct {
		Raw  string `json:"raw"`
		Call int64  `json:"call"`
	}

	err := dispatcher.RegisterCommand(r.d, "keysmith", "keys.create", 1, func(ctx context.Context, _ struct{}, _ contract.Principal) (out, error) {
		call := r.calls.Add(1)
		r.started <- struct{}{}

		select {
		case <-r.gate:
		case <-ctx.Done():
			return out{}, ctx.Err()
		}

		if err := r.fail(call); err != nil {
			return out{}, err
		}

		return out{Raw: claimRaw, Call: call}, nil
	}, regOpts...)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	return r
}

type mintAnswer struct {
	data json.RawMessage
	err  error
}

func (r *mintRig) dispatch() <-chan mintAnswer {
	out := make(chan mintAnswer, 1)

	go func() {
		req := contract.Request{
			Envelope: "v1", Kind: contract.KindCommand,
			Contributor: "keysmith", Intent: "keys.create", IntentVersion: 1,
			IdempotencyKey: "k1",
			Payload:        json.RawMessage(`{}`),
		}

		data, _, err := r.d.Dispatch(context.Background(), req, contract.PrincipalFor(&dashauth.UserInfo{Subject: "alice"}))
		out <- mintAnswer{data, err}
	}()

	return out
}

func await[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)

		var zero T

		return zero
	}
}

func conflictMessage(t *testing.T, err error) string {
	t.Helper()

	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeConflict {
		t.Fatalf("err = %v, want CONFLICT", err)
	}

	return ce.Message
}

// lookupOnlyStore is an idempotency.Store with no Claim.
type lookupOnlyStore struct{}

func (lookupOnlyStore) Lookup(context.Context, string, string) (*idempotency.Cached, bool) {
	return nil, false
}

func (lookupOnlyStore) Store(context.Context, string, string, idempotency.Cached) error { return nil }

// Overlapping duplicates of a secret command through the production store run
// the handler once. Whichever way they interleave with the first, every other
// dispatch either waits on its claim or finds its tombstone, so each one gets
// CONFLICT and only the first sees the key.
func TestSecretCommand_ProductionStoreRunsOverlappingDuplicatesOnce(t *testing.T) {
	r := newMintRig(t, idempotency.NewInMemoryStore(), nil, dispatcher.SecretResponse())

	first := r.dispatch()
	await(t, r.started, "the first handler to start")

	const dupes = 8

	rest := make([]<-chan mintAnswer, dupes)
	for i := range rest {
		rest[i] = r.dispatch()
	}

	time.Sleep(50 * time.Millisecond) // let the duplicates reach the claim
	close(r.gate)

	if a := await(t, first, "the first dispatch"); a.err != nil || !strings.Contains(string(a.data), claimRaw) {
		t.Fatalf("first answer = %s, %v; want the raw key", a.data, a.err)
	}

	for i, ch := range rest {
		a := await(t, ch, "a duplicate dispatch")
		if msg := conflictMessage(t, a.err); !strings.Contains(msg, "already ran") {
			t.Errorf("duplicate %d message = %q, want the tombstone's CONFLICT", i, msg)
		}

		if strings.Contains(string(a.data), claimRaw) {
			t.Errorf("duplicate %d got the raw key", i)
		}
	}

	if n := r.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

func TestCommand_ProductionStoreGivesOverlappingDuplicatesOneResponse(t *testing.T) {
	r := newMintRig(t, idempotency.NewInMemoryStore(), nil)

	first := r.dispatch()
	await(t, r.started, "the first handler to start")

	second := r.dispatch()

	time.Sleep(50 * time.Millisecond)
	close(r.gate)

	a1 := await(t, first, "the first dispatch")
	a2 := await(t, second, "the second dispatch")

	if a1.err != nil || a2.err != nil {
		t.Fatalf("errs = %v, %v; want both to succeed", a1.err, a2.err)
	}

	if string(a1.data) != string(a2.data) || !strings.Contains(string(a2.data), `"call":1`) {
		t.Fatalf("answers = %s and %s, want the first run's response twice", a1.data, a2.data)
	}

	if n := r.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

func TestSecretCommand_ProductionStoreWaitBoundAnswersStillRunning(t *testing.T) {
	r := newMintRig(t, idempotency.NewInMemoryStore(),
		[]dispatcher.Option{dispatcher.WithIdempotencyWait(30 * time.Millisecond)},
		dispatcher.SecretResponse())

	first := r.dispatch()
	await(t, r.started, "the first handler to start")

	a := await(t, r.dispatch(), "the second dispatch")
	if msg := conflictMessage(t, a.err); !strings.Contains(msg, "still running") {
		t.Fatalf("message = %q, want it to say the command is still running", msg)
	}

	close(r.gate)

	if a := await(t, first, "the first dispatch"); a.err != nil {
		t.Fatalf("first dispatch: %v", a.err)
	}

	if n := r.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

func TestSecretCommand_ProductionStoreReleasesAFailedRun(t *testing.T) {
	r := newMintRig(t, idempotency.NewInMemoryStore(), nil, dispatcher.SecretResponse())
	close(r.gate)

	r.fail = func(call int64) error {
		if call == 1 {
			return &contract.Error{Code: contract.CodeUnavailable, Message: "backend down"}
		}

		return nil
	}

	if a := await(t, r.dispatch(), "the first dispatch"); a.err == nil {
		t.Fatal("first dispatch succeeded, want the handler's failure")
	}

	if a := await(t, r.dispatch(), "the retry"); a.err != nil || !strings.Contains(string(a.data), claimRaw) {
		t.Fatalf("retry = %s, %v; want it to run and return the raw key", a.data, a.err)
	}

	if n := r.calls.Load(); n != 2 {
		t.Fatalf("handler ran %d times, want 2", n)
	}
}

// signalInFlight is a shared store that reports each Begin that found the key
// held, so a test knows a duplicate is waiting on the claim.
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

// The same overlap, made certain: the duplicate is known to be waiting on the
// first dispatch's claim in the shared store before the handler finishes.
func TestSecretCommand_SharedStoreDuplicateWaitsOnTheClaim(t *testing.T) {
	shared := signalInFlight{middleware.NewMemoryIdempotencyStore(), make(chan struct{}, 1)}
	r := newMintRig(t, idempotency.NewSharedStore(shared), nil, dispatcher.SecretResponse())

	first := r.dispatch()
	await(t, r.started, "the first handler to start")

	second := r.dispatch()

	await(t, shared.inFlight, "the duplicate to find the key claimed")

	close(r.gate)

	if a := await(t, first, "the first dispatch"); a.err != nil || !strings.Contains(string(a.data), claimRaw) {
		t.Fatalf("first answer = %s, %v; want the raw key", a.data, a.err)
	}

	a := await(t, second, "the duplicate")
	if msg := conflictMessage(t, a.err); !strings.Contains(msg, "already ran") {
		t.Fatalf("duplicate message = %q, want the tombstone's CONFLICT", msg)
	}

	if n := r.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

// A lost lease can leave no receipt. The stale handler must not publish without
// ownership, even for a secret response. A later execution remains possible.
func TestSecretCommand_ProductionStoreLostLeaseHasNoReceipt(t *testing.T) {
	var (
		mu  sync.Mutex
		now = time.Now()
	)

	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()

		return now
	}

	shared := middleware.NewMemoryIdempotencyStore(middleware.MemoryIdempotencyClock(clock))
	r := newMintRig(t, idempotency.NewSharedStore(shared), nil, dispatcher.SecretResponse())

	first := r.dispatch()
	await(t, r.started, "the handler to start")

	// The handler runs for hours of store time, and an unrelated Begin
	// sweeps its lapsed claim.
	mu.Lock()
	now = now.Add(2 * time.Hour)
	mu.Unlock()

	other := middleware.IdempotencyKey{Principal: "bob", Scope: idempotency.SharedScope, Value: "other"}

	swept, err := shared.Begin(context.Background(), other, "", time.Minute)
	if err != nil || swept.State != middleware.IdempotencyAcquired {
		t.Fatalf("sweeping Begin = %+v, %v", swept, err)
	}

	_ = shared.Release(context.Background(), other, swept.Token)

	close(r.gate)

	if a := await(t, first, "the first dispatch"); a.err != nil || !strings.Contains(string(a.data), claimRaw) {
		t.Fatalf("first answer = %s, %v; want the raw key", a.data, a.err)
	}

	a := await(t, r.dispatch(), "the later execution")
	if a.err != nil || r.calls.Load() != 2 {
		t.Fatalf("missing-receipt limit: calls=%d err=%v", r.calls.Load(), a.err)
	}
}
