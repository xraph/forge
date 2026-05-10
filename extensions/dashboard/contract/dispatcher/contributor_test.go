package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type fakeContributor struct {
	name string
	q    map[IntentRef]Handler
	s    map[IntentRef]SubscriptionHandler
}

func (f *fakeContributor) Name() string                                      { return f.name }
func (f *fakeContributor) Handlers() map[IntentRef]Handler                   { return f.q }
func (f *fakeContributor) Subscriptions() map[IntentRef]SubscriptionHandler { return f.s }

func TestRegisterContributor_RegistersAllTables(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	c := &fakeContributor{
		name: "users",
		q: map[IntentRef]Handler{
			{Intent: "users.list", Version: 1}: func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
				return &Result{Data: json.RawMessage(`{}`)}, nil
			},
		},
		s: map[IntentRef]SubscriptionHandler{
			{Intent: "users.events", Version: 1}: func(_ context.Context, _ map[string]any, _ contract.Principal) (<-chan contract.StreamEvent, func(), error) {
				return nil, nil, nil
			},
		},
	}
	if err := d.RegisterContributor(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1}
	if _, _, err := d.Dispatch(context.Background(), req, contract.Principal{}); err != nil {
		t.Errorf("dispatch query: %v", err)
	}
	intent := contract.Intent{Name: "users.events", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	if _, _, err := d.Subscribe(context.Background(), contract.Principal{}, "users", intent, nil); err != nil {
		t.Errorf("subscribe: %v", err)
	}
}

func TestRegisterContributor_NameRequired(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	c := &fakeContributor{name: "", q: map[IntentRef]Handler{}, s: map[IntentRef]SubscriptionHandler{}}
	if err := d.RegisterContributor(c); err == nil {
		t.Error("expected name-required error")
	}
}

func TestRegisterContributor_PartialFailureIsAtomic(t *testing.T) {
	// First register a conflicting handler; then attempt RegisterContributor and verify
	// it surfaces the conflict and does not partially apply.
	d := New(NoopMetricsEmitter{})
	_ = d.Register("users", "users.list", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{}, nil
	})

	c := &fakeContributor{
		name: "users",
		q: map[IntentRef]Handler{
			{Intent: "users.detail", Version: 1}: func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
				return &Result{}, nil
			},
			{Intent: "users.list", Version: 1}: func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
				return &Result{}, nil
			},
		},
		s: nil,
	}
	err := d.RegisterContributor(c)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	// users.detail must NOT be registered (atomicity).
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.detail", IntentVersion: 1}
	if _, _, dispErr := d.Dispatch(context.Background(), req, contract.Principal{}); dispErr == nil {
		t.Error("partial registration leaked: users.detail should not be registered")
	} else {
		var ce *contract.Error
		if !errors.As(dispErr, &ce) || ce.Code != contract.CodeNotFound {
			t.Errorf("expected NotFound, got %v", dispErr)
		}
	}
}
