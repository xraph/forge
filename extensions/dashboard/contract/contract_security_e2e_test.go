// Package contract_test verifies the slice (b) security stack — CSRF
// validation and idempotency dedup — at the seam where transport.Handler
// meets dispatcher.Dispatcher. Both tests use the production
// transport.NewHandlerWithCSRF + dispatcher.NewWithOptions constructors,
// so a regression in either layer surfaces here.
//
// External package (contract_test) deliberately avoids the dashboard
// extension's import cycle: this test depends on contract, dispatcher,
// idempotency, transport, and security — all of which the extension also
// imports — but never the other way round.
package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/security"
)

// TestSecurityE2E_CSRFRequired confirms that a command envelope with a CSRF
// token the manager refuses returns 403 + UNAUTHENTICATED. This is the
// rollout-critical path: a stale or forged token must NEVER reach the
// dispatcher.
func TestSecurityE2E_CSRFRequired(t *testing.T) {
	reg, wreg, disp := setupSecurityEnv(t, idempotency.NewInMemoryStore())
	mgr := security.NewCSRFManager()
	h := transport.NewHandlerWithCSRF(reg, wreg, disp, contract.NoopAuditEmitter{}, mgr)

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "test", Intent: "do.thing", IntentVersion: 1,
		CSRF: "wrong", IdempotencyKey: "k1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "UNAUTHENTICATED") {
		t.Errorf("expected UNAUTHENTICATED in response, got %s", w.Body.String())
	}
}

// TestSecurityE2E_IdempotencyDedup confirms that two identical command
// envelopes — same idempotency key, same CSRF token, same payload — produce
// byte-equal response bodies. The first call dispatches; the second is a
// cache hit served verbatim from the idempotency store.
func TestSecurityE2E_IdempotencyDedup(t *testing.T) {
	store := idempotency.NewInMemoryStore()
	reg, wreg, disp := setupSecurityEnv(t, store)
	mgr := security.NewCSRFManager()
	tok := mgr.GenerateToken()
	h := transport.NewHandlerWithCSRF(reg, wreg, disp, contract.NoopAuditEmitter{}, mgr)

	build := func() *http.Request {
		body, _ := json.Marshal(contract.Request{
			Envelope: "v1", Kind: contract.KindCommand,
			Contributor: "test", Intent: "do.thing", IntentVersion: 1,
			CSRF: tok, IdempotencyKey: "ik_e2e",
		})
		return httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	}
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, build())
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, build())

	if w1.Body.String() != w2.Body.String() {
		t.Errorf("idempotent calls produced different bodies:\nfirst:  %s\nsecond: %s", w1.Body, w2.Body)
	}
}

// setupSecurityEnv wires a registry containing one write-capability command
// intent (`test/do.thing@1`), an empty warden registry, and a dispatcher
// configured with the supplied idempotency store. The bound handler is the
// minimum viable command handler — returns OK:true with no work.
func setupSecurityEnv(t *testing.T, store idempotency.Store) (contract.Registry, contract.WardenRegistry, *dispatcher.Dispatcher) {
	t.Helper()
	reg := contract.NewRegistry()
	src := `
schemaVersion: 1
contributor: { name: test, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: do.thing, kind: command, version: 1, capability: write }
`
	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&m); err != nil {
		t.Fatal(err)
	}
	wreg := contract.NewWardenRegistry()
	disp := dispatcher.NewWithOptions(dispatcher.NoopMetricsEmitter{},
		dispatcher.WithIdempotencyStore(adaptStore(store)))
	if err := dispatcher.RegisterCommand(disp, "test", "do.thing", 1,
		func(_ context.Context, _ struct{}, _ contract.Principal) (struct{ OK bool }, error) {
			return struct{ OK bool }{OK: true}, nil
		}); err != nil {
		t.Fatalf("RegisterCommand: %v", err)
	}
	return reg, wreg, disp
}

// adapter mirrors the production idempotencyAdapter in extensions/dashboard.
// The duplication is intentional: this is an external test package that
// can't reach into the dashboard package without re-introducing the cycle
// the contract sub-packages were carved out to avoid. The conversion is
// trivial enough that keeping it inline here is cheaper than exposing a
// public adapter constructor.
type adapter struct{ inner idempotency.Store }

func adaptStore(s idempotency.Store) dispatcher.IdempotencyStore { return &adapter{inner: s} }

func (a *adapter) Lookup(ctx context.Context, k, id string) (*dispatcher.IdempotencyCached, bool) {
	c, ok := a.inner.Lookup(ctx, k, id)
	if !ok {
		return nil, false
	}
	return &dispatcher.IdempotencyCached{
		Status: c.Status, WireBody: c.WireBody,
		StoredAt: c.StoredAt, TTL: c.TTL,
	}, true
}

func (a *adapter) Store(ctx context.Context, k, id string, c dispatcher.IdempotencyCached) error {
	return a.inner.Store(ctx, k, id, idempotency.Cached{
		Status: c.Status, WireBody: c.WireBody,
		StoredAt: c.StoredAt, TTL: c.TTL,
	})
}
