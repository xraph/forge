package dispatcher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type listIn struct {
	Tenant string `json:"tenant"`
}
type listOut struct {
	Users []string `json:"users"`
}

func TestRegisterQuery_DecodesAndEncodes(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := RegisterQuery(d, "users", "users.list", 1, func(_ context.Context, in listIn, _ contract.Principal) (listOut, error) {
		if in.Tenant != "acme" {
			t.Errorf("decoded tenant = %q", in.Tenant)
		}
		return listOut{Users: []string{"alice", "bob"}}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1, Payload: json.RawMessage(`{"tenant":"acme"}`)}
	data, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var got listOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Users) != 2 {
		t.Errorf("users = %v", got.Users)
	}
}

// TestRegisterQuery_DecodesFromParams guards the shell-side contract that
// resource.detail and similar lookup components bind route placeholders
// into `params` (not `payload`). Without merging params into the typed
// input, every detail handler that takes an ID-shaped struct receives
// the zero value and fails with "id is required".
type detailIn struct {
	ID string `json:"id"`
}
type detailOut struct {
	Echoed string `json:"echoed"`
}

func TestRegisterQuery_DecodesFromParams(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := RegisterQuery(d, "users", "users.detail", 1, func(_ context.Context, in detailIn, _ contract.Principal) (detailOut, error) {
		if in.ID == "" {
			t.Errorf("expected id from params, got empty")
		}
		return detailOut{Echoed: in.ID}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "users", Intent: "users.detail", IntentVersion: 1,
		Params: map[string]any{"id": "ausr_abc"},
		// No Payload — this is the resource.detail path: route binding
		// fills Params, Payload stays empty.
	}
	data, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var got detailOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Echoed != "ausr_abc" {
		t.Errorf("echoed = %q, want ausr_abc", got.Echoed)
	}
}

// TestRegisterQuery_PayloadOverridesParams locks in the merge order:
// payload wins on conflicting fields so a caller explicitly passing
// `{ id: "X" }` in the body isn't silently overwritten by a route-bound
// param. This matters for commands authored against the same input
// type, where the form submission carries the authoritative value.
func TestRegisterQuery_PayloadOverridesParams(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := RegisterQuery(d, "u", "u.d", 1, func(_ context.Context, in detailIn, _ contract.Principal) (detailOut, error) {
		return detailOut{Echoed: in.ID}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "u", Intent: "u.d", IntentVersion: 1,
		Params:  map[string]any{"id": "from-params"},
		Payload: json.RawMessage(`{"id":"from-payload"}`),
	}
	data, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var got detailOut
	_ = json.Unmarshal(data, &got)
	if got.Echoed != "from-payload" {
		t.Errorf("echoed = %q, want from-payload (payload should win over params)", got.Echoed)
	}
}

func TestRegisterQuery_DecodeErrorBecomesBadRequest(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = RegisterQuery(d, "u", "u.l", 1, func(_ context.Context, _ listIn, _ contract.Principal) (listOut, error) { return listOut{}, nil })
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "u", Intent: "u.l", IntentVersion: 1, Payload: json.RawMessage(`not json`)}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err == nil {
		t.Fatal("expected decode error")
	}
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", err)
	}
}

type tickIn struct{}
type tickEvent struct {
	N int `json:"n"`
}

func TestRegisterSubscriptionGeneric_PumpsTypedEvents(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := RegisterSubscription(d, "feed", "tick", 1, func(ctx context.Context, _ tickIn, _ contract.Principal) (<-chan tickEvent, func(), error) {
		ch := make(chan tickEvent, 2)
		ch <- tickEvent{N: 1}
		ch <- tickEvent{N: 2}
		close(ch)
		return ch, func() {}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	intent := contract.Intent{Name: "tick", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	out, stop, err := d.Subscribe(ctx, contract.Principal{}, "feed", intent, nil)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	count := 0
	for ev := range out {
		count++
		var got tickEvent
		if err := json.Unmarshal(ev.Payload, &got); err != nil {
			t.Errorf("unmarshal event: %v", err)
		}
		if got.N != count {
			t.Errorf("event %d N = %d", count, got.N)
		}
	}
	if count != 2 {
		t.Errorf("expected 2 events, got %d", count)
	}
}
