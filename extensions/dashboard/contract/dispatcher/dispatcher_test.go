package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestDispatcher_RegisterAndDispatch(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := d.Register("users", "users.list", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`{"users":[]}`)}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1}
	data, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(data) != `{"users":[]}` {
		t.Errorf("data = %s", data)
	}
}

func TestDispatcher_DuplicateRegister(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	h := func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{}, nil
	}
	_ = d.Register("c", "i", 1, h)
	if err := d.Register("c", "i", 1, h); err == nil {
		t.Error("duplicate register should fail")
	}
}

func TestDispatcher_NotFound(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "x", Intent: "y", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestDispatcher_ContractErrorPassesThrough(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, &contract.Error{Code: contract.CodeConflict, Message: "duplicate"}
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeConflict {
		t.Errorf("expected CodeConflict pass-through, got %v", err)
	}
}

func TestDispatcher_PlainErrorWrappedAsInternal(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, errors.New("kaboom")
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeInternal {
		t.Errorf("expected CodeInternal wrap, got %v", err)
	}
}

func TestDispatcher_ContextCanceledMappedToUnavailable(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, context.Canceled
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable for canceled, got %v", err)
	}
	if !ce.Retryable {
		t.Error("canceled errors should be retryable")
	}
}

// stubStore is a test-only IdempotencyStore that records hit/miss/store
// counters and keys cached entries by `key|identity`.
type stubStore struct {
	hits map[string]IdempotencyCached
	puts int64
	gets int64
}

func newStubStore() *stubStore { return &stubStore{hits: map[string]IdempotencyCached{}} }

func (s *stubStore) Lookup(_ context.Context, key, identity string) (*IdempotencyCached, bool) {
	atomic.AddInt64(&s.gets, 1)
	c, ok := s.hits[key+"|"+identity]
	if !ok {
		return nil, false
	}
	cc := c
	return &cc, true
}

func (s *stubStore) Store(_ context.Context, key, identity string, c IdempotencyCached) error {
	atomic.AddInt64(&s.puts, 1)
	s.hits[key+"|"+identity] = c
	return nil
}

func TestDispatcher_IdempotencyHitReturnsCached(t *testing.T) {
	store := newStubStore()
	store.hits["k|alice:i"] = IdempotencyCached{
		Status:   200,
		WireBody: json.RawMessage(`{"ok":true,"envelope":"v1","kind":"command","data":{"cached":true},"meta":{}}`),
		StoredAt: time.Now(),
		TTL:      time.Hour,
	}
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	called := int64(0)
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		atomic.AddInt64(&called, 1)
		return &Result{Data: json.RawMessage(`{"fresh":true}`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		IdempotencyKey: "k",
	}
	p := contract.PrincipalFor(&dashauth.UserInfo{Subject: "alice"})
	data, _, err := d.Dispatch(context.Background(), req, p)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(data) != `{"cached":true}` {
		t.Errorf("expected cached body, got %s", data)
	}
	if atomic.LoadInt64(&called) != 0 {
		t.Errorf("handler should not have been called on cache hit")
	}
}

func TestDispatcher_IdempotencyMissCallsHandlerAndStores(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`{"fresh":true}`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		IdempotencyKey: "k",
	}
	p := contract.PrincipalFor(&dashauth.UserInfo{Subject: "alice"})
	_, _, _ = d.Dispatch(context.Background(), req, p)
	if atomic.LoadInt64(&store.puts) != 1 {
		t.Errorf("expected 1 store write, got %d", store.puts)
	}
}

func TestDispatcher_IdempotencyOnlyAppliesToCommands(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		IdempotencyKey: "k",
	}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	if atomic.LoadInt64(&store.gets) != 0 {
		t.Errorf("query should not consult store, gets=%d", store.gets)
	}
}

func TestDispatcher_IdempotencyMissingKeyBypassesStore(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		// IdempotencyKey intentionally empty — slice (a)'s presence check is
		// the gate; when missing, the dispatcher bypasses dedup entirely.
	}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	if atomic.LoadInt64(&store.gets) != 0 {
		t.Errorf("missing key should bypass store, gets=%d", store.gets)
	}
}
