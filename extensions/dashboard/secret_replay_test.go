package dashboard

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
)

// The tombstone has to survive the production store, which keeps entries in
// the shared middleware idempotency store, not just the dispatcher's stub.
func TestSecretCommand_ProductionStoreKeepsOnlyATombstone(t *testing.T) {
	inner := idempotency.NewInMemoryStore()
	d := dispatcher.NewWithOptions(dispatcher.NoopMetricsEmitter{},
		dispatcher.WithIdempotencyStore(adaptIdempotencyStore(inner)))

	const raw = "sk_live_do_not_keep"

	type out struct {
		Raw string `json:"raw"`
	}

	calls := int64(0)

	err := dispatcher.RegisterCommand(d, "keysmith", "keys.create", 1, func(context.Context, struct{}, contract.Principal) (out, error) {
		atomic.AddInt64(&calls, 1)

		return out{Raw: raw}, nil
	}, dispatcher.SecretResponse())
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	req := contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "keysmith", Intent: "keys.create", IntentVersion: 1,
		IdempotencyKey: "k1",
	}
	p := contract.PrincipalFor(&dashauth.UserInfo{Subject: "alice"})

	data, _, err := d.Dispatch(context.Background(), req, p)
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}

	if !strings.Contains(string(data), raw) {
		t.Fatalf("first answer = %s, want the raw key", data)
	}

	cached, ok := inner.Lookup(context.Background(), "k1", "alice:keys.create")
	if !ok {
		t.Fatal("no tombstone in the store")
	}

	if cached.Status != dispatcher.TombstoneStatus {
		t.Errorf("stored status = %d, want %d", cached.Status, dispatcher.TombstoneStatus)
	}

	if len(cached.WireBody) != 0 {
		t.Errorf("stored body = %s, want none", cached.WireBody)
	}

	_, _, err = d.Dispatch(context.Background(), req, p)

	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeConflict {
		t.Fatalf("replay err = %v, want CONFLICT", err)
	}

	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("handler ran %d times, want 1", calls)
	}
}
