package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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
	h := func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) { return &Result{}, nil }
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
