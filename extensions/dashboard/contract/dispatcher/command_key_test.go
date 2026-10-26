package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestCommandIdempotencyKeyUsesCurrentEnvelope(t *testing.T) {
	d := New(nil)

	if key := CommandIdempotencyKey(t.Context()); key != "" {
		t.Fatalf("outside dispatch: %q", key)
	}

	type input struct {
		IdempotencyKey string `json:"idempotency_key"`
	}

	if err := RegisterCommand(d, "test", "write", 1, func(ctx context.Context, in input, _ contract.Principal) (string, error) {
		if in.IdempotencyKey != "payload-key" {
			t.Fatalf("payload unexpectedly changed: %+v", in)
		}

		return CommandIdempotencyKey(ctx), nil
	}, BeforeDispatch(func(ctx context.Context, req contract.Request, _ contract.Principal) error {
		if CommandIdempotencyKey(ctx) != req.IdempotencyKey {
			t.Fatal("admission lost envelope key")
		}

		return nil
	}), BypassIdempotency()); err != nil {
		t.Fatal(err)
	}

	data, _, err := d.Dispatch(t.Context(), contract.Request{Contributor: "test", Intent: "write", IntentVersion: 1, Kind: contract.KindCommand, IdempotencyKey: "envelope-key", Params: map[string]any{"idempotency_key": "param-key"}, Payload: json.RawMessage(`{"idempotency_key":"payload-key"}`)}, contract.Principal{})
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != `"envelope-key"` {
		t.Fatalf("handler key: %s", data)
	}
}

func TestCommandIdempotencyKeyDoesNotLeakIntoNestedRequests(t *testing.T) {
	d := New(nil)
	if err := RegisterQuery(d, "test", "read", 1, func(ctx context.Context, _ struct{}, _ contract.Principal) (string, error) {
		return CommandIdempotencyKey(ctx), nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := RegisterCommand(d, "test", "inner", 1, func(ctx context.Context, _ struct{}, _ contract.Principal) (string, error) {
		return CommandIdempotencyKey(ctx), nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := RegisterCommand(d, "test", "outer", 1, func(ctx context.Context, _ struct{}, p contract.Principal) (string, error) {
		for _, tc := range []struct {
			intent    string
			kind      contract.Kind
			key, want string
		}{
			{"read", contract.KindQuery, "forged-query-key", `""`},
			{"inner", contract.KindCommand, "inner-key", `"inner-key"`},
			{"inner", contract.KindCommand, "", `""`},
		} {
			data, _, err := d.Dispatch(ctx, contract.Request{Contributor: "test", Intent: tc.intent, IntentVersion: 1, Kind: tc.kind, IdempotencyKey: tc.key}, p)
			if err != nil {
				t.Fatal(err)
			}

			if string(data) != tc.want {
				t.Fatalf("nested %s: %s", tc.intent, data)
			}

			if CommandIdempotencyKey(ctx) != "outer-key" {
				t.Fatal("nested dispatch mutated parent context")
			}
		}

		return CommandIdempotencyKey(ctx), nil
	}); err != nil {
		t.Fatal(err)
	}

	data, _, err := d.Dispatch(t.Context(), contract.Request{Contributor: "test", Intent: "outer", IntentVersion: 1, Kind: contract.KindCommand, IdempotencyKey: "outer-key"}, contract.Principal{})
	if err != nil || string(data) != `"outer-key"` {
		t.Fatalf("outer: %s %v", data, err)
	}
}

func TestCommandIdempotencyKeyConcurrentDispatch(t *testing.T) {
	d := New(nil)
	if err := RegisterCommand(d, "test", "write", 1, func(ctx context.Context, _ struct{}, _ contract.Principal) (string, error) {
		return CommandIdempotencyKey(ctx), nil
	}); err != nil {
		t.Fatal(err)
	}

	var group sync.WaitGroup
	for i := range 32 {
		group.Go(func() {
			key := fmt.Sprintf("request-%d", i)

			data, _, err := d.Dispatch(t.Context(), contract.Request{Contributor: "test", Intent: "write", IntentVersion: 1, Kind: contract.KindCommand, IdempotencyKey: key}, contract.Principal{})
			if err != nil {
				t.Error(err)

				return
			}

			var got string
			if err = json.Unmarshal(data, &got); err != nil {
				t.Error(err)

				return
			}

			if got != key {
				t.Errorf("key crossed requests: %q != %q", got, key)
			}
		})
	}

	group.Wait()
}
