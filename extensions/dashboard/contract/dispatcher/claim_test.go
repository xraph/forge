package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// claimStore is a test IdempotencyClaimer. Claim holds a key until its End
// runs; a Claim that finds the key held sends on waiting (when set) and waits
// for the claim to end or its context to end.
type claimStore struct {
	mu       sync.Mutex
	entries  map[string]IdempotencyCached
	held     map[string]chan struct{}
	claims   int
	stores   int
	releases int
	claimErr error
	endErr   error // when set, End stores nothing, drops the claim and returns it

	waiting chan string // receives the key each time a Claim starts waiting
}

func newClaimStore() *claimStore {
	return &claimStore{
		entries: map[string]IdempotencyCached{},
		held:    map[string]chan struct{}{},
		waiting: make(chan string, 16),
	}
}

func (s *claimStore) Lookup(_ context.Context, key, identity string) (*IdempotencyCached, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.entries[key+"|"+identity]
	if !ok {
		return nil, false
	}

	return &c, true
}

func (s *claimStore) Store(_ context.Context, key, identity string, c IdempotencyCached) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stores++
	s.entries[key+"|"+identity] = c

	return nil
}

func (s *claimStore) Claim(ctx context.Context, key, identity string) (IdempotencyClaim, error) {
	k := key + "|" + identity

	for {
		s.mu.Lock()
		s.claims++

		if s.claimErr != nil {
			s.mu.Unlock()

			return IdempotencyClaim{}, s.claimErr
		}

		if c, ok := s.entries[k]; ok {
			s.mu.Unlock()

			return IdempotencyClaim{Cached: &c}, nil
		}

		done, held := s.held[k]
		if !held {
			done = make(chan struct{})
			s.held[k] = done
			s.mu.Unlock()

			return IdempotencyClaim{End: s.ender(k, done)}, nil
		}
		s.mu.Unlock()

		select {
		case s.waiting <- k:
		default:
		}

		select {
		case <-done:
		case <-ctx.Done():
			return IdempotencyClaim{}, fmt.Errorf("%w: %w", ErrIdempotencyClaimHeld, ctx.Err())
		}
	}
}

func (s *claimStore) ender(k string, done chan struct{}) func(context.Context, *IdempotencyCached) error {
	return func(_ context.Context, c *IdempotencyCached) error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.held[k] != done {
			return errors.New("claimStore: End on a claim that is not held")
		}

		if s.endErr != nil {
			delete(s.held, k)
			close(done)

			return s.endErr
		}

		if c == nil {
			s.releases++
		} else {
			s.stores++
			s.entries[k] = *c
		}

		delete(s.held, k)
		close(done)

		return nil
	}
}

func (s *claimStore) counts() (claims, stores, releases, held int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.claims, s.stores, s.releases, len(s.held)
}

// gatedCommand registers keysmith/keys.create on a dispatcher over store. Its
// handler signals started, then waits for gate (if any) and returns what
// answer returns for that call number.
type gatedCommand struct {
	d       *Dispatcher
	calls   atomic.Int64
	started chan struct{}
	gate    chan struct{}
	answer  func(call int64) (mintOut, error)
}

func newGatedCommand(t *testing.T, store IdempotencyStore, dispOpts []Option, regOpts ...RegisterOption) *gatedCommand {
	t.Helper()

	g := &gatedCommand{
		d:       NewWithOptions(NoopMetricsEmitter{}, append([]Option{WithIdempotencyStore(store)}, dispOpts...)...),
		started: make(chan struct{}, 16),
		gate:    make(chan struct{}),
		answer: func(call int64) (mintOut, error) {
			return mintOut{ID: fmt.Sprintf("key_%d", call), Raw: rawKey}, nil
		},
	}

	err := RegisterCommand(g.d, "keysmith", "keys.create", 1, func(ctx context.Context, _ mintIn, _ contract.Principal) (mintOut, error) {
		call := g.calls.Add(1)
		g.started <- struct{}{}

		select {
		case <-g.gate:
		case <-ctx.Done():
			return mintOut{}, ctx.Err()
		}

		return g.answer(call)
	}, regOpts...)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	return g
}

type dispatchResult struct {
	data json.RawMessage
	err  error
}

func (g *gatedCommand) dispatch(ctx context.Context) <-chan dispatchResult {
	out := make(chan dispatchResult, 1)

	go func() {
		data, _, err := g.d.Dispatch(ctx, mintRequest(), alice())
		out <- dispatchResult{data, err}
	}()

	return out
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
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

func requireStillRunning(t *testing.T, err error) {
	t.Helper()

	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *contract.Error", err)
	}

	if ce.Code != contract.CodeConflict {
		t.Fatalf("code = %s, want %s", ce.Code, contract.CodeConflict)
	}

	if !strings.Contains(ce.Message, "still running") {
		t.Errorf("message = %q, want it to say the same command is still running", ce.Message)
	}

	if !ce.Retryable {
		t.Error("still-running CONFLICT should be retryable")
	}
}

func TestClaim_ConcurrentSecretDuplicatesRunTheHandlerOnce(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, nil, SecretResponse())

	first := g.dispatch(context.Background())
	recv(t, g.started, "the first handler to start")

	second := g.dispatch(context.Background())

	recv(t, store.waiting, "the second dispatch to wait on the claim")

	close(g.gate)

	r1 := recv(t, first, "the first dispatch")
	if r1.err != nil {
		t.Fatalf("first dispatch: %v", r1.err)
	}

	if !strings.Contains(string(r1.data), rawKey) {
		t.Errorf("first answer = %s, want the raw key", r1.data)
	}

	r2 := recv(t, second, "the second dispatch")
	requireSecretConflict(t, r2.err)

	if r2.data != nil {
		t.Errorf("second answer data = %s, want none", r2.data)
	}

	if n := g.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}

	_, stores, releases, held := store.counts()
	if stores != 1 || releases != 0 || held != 0 {
		t.Errorf("stores=%d releases=%d held=%d, want one tombstone and no claim left", stores, releases, held)
	}
}

func TestClaim_ConcurrentNonSecretDuplicatesBothGetTheResponse(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, nil)

	first := g.dispatch(context.Background())
	recv(t, g.started, "the first handler to start")

	second := g.dispatch(context.Background())

	recv(t, store.waiting, "the second dispatch to wait on the claim")

	close(g.gate)

	r1 := recv(t, first, "the first dispatch")
	r2 := recv(t, second, "the second dispatch")

	if r1.err != nil || r2.err != nil {
		t.Fatalf("errs = %v, %v; want both to succeed", r1.err, r2.err)
	}

	if string(r1.data) != string(r2.data) {
		t.Errorf("answers differ: %s vs %s", r1.data, r2.data)
	}

	if !strings.Contains(string(r2.data), `"key_1"`) {
		t.Errorf("second answer = %s, want the first run's response", r2.data)
	}

	if n := g.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

func TestClaim_FailedHandlerReleasesAndAWaiterRuns(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, nil, SecretResponse())
	g.answer = func(call int64) (mintOut, error) {
		if call == 1 {
			return mintOut{}, &contract.Error{Code: contract.CodeUnavailable, Message: "backend down"}
		}

		return mintOut{ID: "key_2", Raw: rawKey}, nil
	}

	first := g.dispatch(context.Background())
	recv(t, g.started, "the first handler to start")

	second := g.dispatch(context.Background())

	recv(t, store.waiting, "the second dispatch to wait on the claim")

	close(g.gate)

	r1 := recv(t, first, "the first dispatch")

	var ce *contract.Error
	if !errors.As(r1.err, &ce) || ce.Code != contract.CodeUnavailable {
		t.Fatalf("first err = %v, want the handler's UNAVAILABLE", r1.err)
	}

	r2 := recv(t, second, "the second dispatch")
	if r2.err != nil {
		t.Fatalf("second dispatch: %v, want it to run once the failed claim was released", r2.err)
	}

	if !strings.Contains(string(r2.data), rawKey) {
		t.Errorf("second answer = %s, want the raw key", r2.data)
	}

	if n := g.calls.Load(); n != 2 {
		t.Fatalf("handler ran %d times, want 2 (the failure stored nothing)", n)
	}

	_, stores, releases, held := store.counts()
	if stores != 1 || releases != 1 || held != 0 {
		t.Errorf("stores=%d releases=%d held=%d, want one release, one tombstone, no claim left", stores, releases, held)
	}
}

func TestClaim_FailedHandlerReleasesAndARetryRuns(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, nil)
	close(g.gate)

	g.answer = func(call int64) (mintOut, error) {
		if call == 1 {
			return mintOut{}, errors.New("boom")
		}

		return mintOut{ID: "key_2"}, nil
	}

	if r := recv(t, g.dispatch(context.Background()), "the first dispatch"); r.err == nil {
		t.Fatal("first dispatch succeeded, want the handler's failure")
	}

	r := recv(t, g.dispatch(context.Background()), "the retry")
	if r.err != nil {
		t.Fatalf("retry: %v", r.err)
	}

	if n := g.calls.Load(); n != 2 {
		t.Fatalf("handler ran %d times, want 2", n)
	}
}

func TestClaim_WaitBoundAnswersStillRunning(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, []Option{WithIdempotencyWait(30 * time.Millisecond)}, SecretResponse())

	first := g.dispatch(context.Background())
	recv(t, g.started, "the first handler to start")

	start := time.Now()
	r2 := recv(t, g.dispatch(context.Background()), "the second dispatch")

	requireStillRunning(t, r2.err)

	if waited := time.Since(start); waited < 30*time.Millisecond {
		t.Errorf("second dispatch answered after %v, want it to wait the bound first", waited)
	}

	if n := g.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times while the claim was held, want 1", n)
	}

	close(g.gate)

	if r1 := recv(t, first, "the first dispatch"); r1.err != nil {
		t.Fatalf("first dispatch: %v", r1.err)
	}
}

func TestClaim_WaiterContextEndAnswersStillRunning(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, nil)

	first := g.dispatch(context.Background())
	recv(t, g.started, "the first handler to start")

	ctx, cancel := context.WithCancel(context.Background())
	second := g.dispatch(ctx)

	recv(t, store.waiting, "the second dispatch to wait on the claim")
	cancel()

	requireStillRunning(t, recv(t, second, "the second dispatch").err)

	close(g.gate)
	recv(t, first, "the first dispatch")

	if n := g.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

func TestClaim_DefaultWaitIsTheDocumentedOne(t *testing.T) {
	d := NewWithOptions(NoopMetricsEmitter{})
	if d.claimWait != DefaultIdempotencyWait {
		t.Errorf("claimWait = %v, want %v", d.claimWait, DefaultIdempotencyWait)
	}

	d = NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyWait(0))
	if d.claimWait != DefaultIdempotencyWait {
		t.Errorf("WithIdempotencyWait(0) set claimWait = %v, want the default kept", d.claimWait)
	}
}

func TestClaim_PanickingHandlerReleasesTheClaim(t *testing.T) {
	store := newClaimStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))

	_ = d.Register("keysmith", "keys.create", 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*Result, error) {
		panic("handler blew up")
	})

	func() {
		defer func() { _ = recover() }()

		_, _, _ = d.Dispatch(context.Background(), mintRequest(), alice())
	}()

	if _, _, releases, held := store.counts(); releases != 1 || held != 0 {
		t.Fatalf("releases=%d held=%d after a panic, want the claim given back", releases, held)
	}
}

func TestClaim_ClaimFailureAnswersUnavailableWithoutRunning(t *testing.T) {
	store := newClaimStore()
	store.claimErr = errors.New("backend unreachable")
	g := newGatedCommand(t, store, nil)
	close(g.gate)

	r := recv(t, g.dispatch(context.Background()), "the dispatch")

	var ce *contract.Error
	if !errors.As(r.err, &ce) || ce.Code != contract.CodeUnavailable || !ce.Retryable {
		t.Fatalf("err = %v, want a retryable UNAVAILABLE", r.err)
	}

	if n := g.calls.Load(); n != 0 {
		t.Fatalf("handler ran %d times without a claim, want 0", n)
	}
}

// emptyClaimer returns neither an entry nor a claim, which breaks the
// IdempotencyClaimer contract.
type emptyClaimer struct{ *claimStore }

func (emptyClaimer) Claim(context.Context, string, string) (IdempotencyClaim, error) {
	return IdempotencyClaim{}, nil
}

func TestClaim_EmptyClaimAnswersUnavailableWithoutRunning(t *testing.T) {
	g := newGatedCommand(t, emptyClaimer{newClaimStore()}, nil)
	close(g.gate)

	r := recv(t, g.dispatch(context.Background()), "the dispatch")

	var ce *contract.Error
	if !errors.As(r.err, &ce) || ce.Code != contract.CodeUnavailable {
		t.Fatalf("err = %v, want UNAVAILABLE", r.err)
	}

	if n := g.calls.Load(); n != 0 {
		t.Fatalf("handler ran %d times, want 0", n)
	}
}

func TestClaim_UndecodableEntryRunsAfreshAndOverwrites(t *testing.T) {
	store := newClaimStore()
	store.entries["k1|alice:keys.create"] = IdempotencyCached{Status: 200, WireBody: json.RawMessage(`not json`)}

	g := newGatedCommand(t, store, nil)
	close(g.gate)

	r := recv(t, g.dispatch(context.Background()), "the dispatch")
	if r.err != nil {
		t.Fatalf("dispatch: %v", r.err)
	}

	if n := g.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}

	got, ok := store.Lookup(context.Background(), "k1", "alice:keys.create")
	if !ok || !strings.Contains(string(got.WireBody), `"key_1"`) {
		t.Fatalf("entry = %+v, want the fresh response stored over the undecodable one", got)
	}
}

// A command with no local handler goes to the remote dispatcher, which stores
// nothing here, so it must not claim the key.
func TestClaim_ForwardedCommandDoesNotClaim(t *testing.T) {
	store := newClaimStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	d.SetRemoteDispatcher(remoteFunc(func(context.Context, contract.Request, contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
		return json.RawMessage(`{"remote":true}`), contract.ResponseMeta{}, nil
	}))

	if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if claims, _, _, held := store.counts(); claims != 0 || held != 0 {
		t.Fatalf("claims=%d held=%d for a forwarded command, want none", claims, held)
	}
}

type remoteFunc func(context.Context, contract.Request, contract.Principal) (json.RawMessage, contract.ResponseMeta, error)

func (f remoteFunc) Dispatch(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	return f(ctx, req, p)
}

func TestClaim_QueriesAndKeylessCommandsDoNotClaim(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, nil)
	close(g.gate)

	keyless := mintRequest()
	keyless.IdempotencyKey = ""

	query := mintRequest()
	query.Kind = contract.KindQuery

	for _, req := range []contract.Request{keyless, query} {
		if _, _, err := g.d.Dispatch(context.Background(), req, alice()); err != nil {
			t.Fatalf("dispatch %+v: %v", req, err)
		}
	}

	if claims, _, _, _ := store.counts(); claims != 0 {
		t.Fatalf("claims = %d, want none", claims)
	}
}

// A secret command whose claim lapsed while its handler ran must still leave
// its tombstone, or a replay with the same key would mint a second secret.
func TestClaim_LapsedClaimStillLeavesTheTombstone(t *testing.T) {
	store := newClaimStore()
	store.endErr = fmt.Errorf("%w: lease lapsed", ErrIdempotencyClaimLost)

	g := newGatedCommand(t, store, nil, SecretResponse())
	close(g.gate)

	first := recv(t, g.dispatch(context.Background()), "the first dispatch")
	if first.err != nil || !strings.Contains(string(first.data), rawKey) {
		t.Fatalf("first answer = %s, %v; want the raw key", first.data, first.err)
	}

	entry, ok := store.Lookup(context.Background(), "k1", "alice:keys.create")
	if !ok || entry.Status != TombstoneStatus || len(entry.WireBody) != 0 {
		t.Fatalf("entry = %+v, %v; want a tombstone written through Store", entry, ok)
	}

	requireSecretConflict(t, recv(t, g.dispatch(context.Background()), "the replay").err)

	if n := g.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

func TestClaim_LapsedClaimStillLeavesTheResponse(t *testing.T) {
	store := newClaimStore()
	store.endErr = fmt.Errorf("%w: lease lapsed", ErrIdempotencyClaimLost)

	g := newGatedCommand(t, store, nil)
	close(g.gate)

	first := recv(t, g.dispatch(context.Background()), "the first dispatch")
	if first.err != nil {
		t.Fatalf("first dispatch: %v", first.err)
	}

	second := recv(t, g.dispatch(context.Background()), "the replay")
	if second.err != nil || string(second.data) != string(first.data) {
		t.Fatalf("replay = %s, %v; want the first response", second.data, second.err)
	}

	if n := g.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}

// Any other End failure is logged, not papered over with a Store.
func TestClaim_OtherEndFailureDoesNotFallBackToStore(t *testing.T) {
	store := newClaimStore()
	store.endErr = errors.New("backend write failed")

	g := newGatedCommand(t, store, nil, SecretResponse())
	close(g.gate)

	if r := recv(t, g.dispatch(context.Background()), "the dispatch"); r.err != nil {
		t.Fatalf("dispatch: %v", r.err)
	}

	if _, stores, _, held := store.counts(); stores != 0 || held != 0 {
		t.Fatalf("stores=%d held=%d, want no Store fallback and no claim left", stores, held)
	}
}
