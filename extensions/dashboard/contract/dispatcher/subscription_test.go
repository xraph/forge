package dispatcher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestDispatcher_RegisterSubscriptionAndSubscribe(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := d.RegisterSubscription("logs", "audit.tail", 1, func(_ context.Context, _ map[string]any, _ contract.Principal) (<-chan contract.StreamEvent, func(), error) {
		ch := make(chan contract.StreamEvent, 1)
		ch <- contract.StreamEvent{Intent: "audit.tail", Mode: contract.ModeAppend, Payload: json.RawMessage(`{"line":"hi"}`), Seq: 1}
		close(ch)
		return ch, func() {}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	intent := contract.Intent{Name: "audit.tail", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ch, stop, err := d.Subscribe(ctx, contract.Principal{}, "logs", intent, nil)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before event")
		}
		if ev.Intent != "audit.tail" {
			t.Errorf("intent = %q", ev.Intent)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestDispatcher_SubscribeMissingHandler(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	intent := contract.Intent{Name: "missing", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	_, _, err := d.Subscribe(context.Background(), contract.Principal{}, "x", intent, nil)
	if err == nil {
		t.Error("expected not-found")
	}
}

func TestDispatcher_DuplicateRegisterSubscription(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	h := func(_ context.Context, _ map[string]any, _ contract.Principal) (<-chan contract.StreamEvent, func(), error) {
		return nil, nil, nil
	}
	_ = d.RegisterSubscription("c", "i", 1, h)
	if err := d.RegisterSubscription("c", "i", 1, h); err == nil {
		t.Error("duplicate register should fail")
	}
}
