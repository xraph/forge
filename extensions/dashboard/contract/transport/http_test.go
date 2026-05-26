// http_test.go
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/security"
)

type stubDispatcher struct {
	called   string
	response json.RawMessage
}

func (s *stubDispatcher) Dispatch(_ context.Context, in contract.Request, _ contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	s.called = string(in.Kind) + ":" + in.Intent
	return s.response, contract.ResponseMeta{IntentVersion: in.IntentVersion}, nil
}

func setupRegistry(t *testing.T) (contract.Registry, contract.WardenRegistry) {
	t.Helper()
	r := contract.NewRegistry()
	src := `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
  - { name: user.disable, kind: command, version: 1, capability: write }
`
	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&m); err != nil {
		t.Fatal(err)
	}
	return r, contract.NewWardenRegistry()
}

func TestHandler_DispatchesQuery(t *testing.T) {
	reg, wreg := setupRegistry(t)
	disp := &stubDispatcher{response: json.RawMessage(`{"users":[]}`)}
	h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{})

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	if disp.called != "query:users.list" {
		t.Errorf("dispatcher not called: %s", disp.called)
	}
}

// TestHandler_NormalizesOmittedIntentVersion guards the contract that a
// request with IntentVersion=0 (the JSON default when the client omits the
// field) is resolved to the highest registered version BEFORE the dispatcher
// is invoked. Without this normalization the registry lookup succeeds via
// the "0 means highest" rule but the dispatcher's (contributor, intent,
// version) handler-map lookup misses against version 0 — surfacing as
// "handler {contributor}/{intent}@0 not registered". The same normalization
// matters when the dispatcher forwards to a remote upstream, because the
// upstream's transport reuses this exact handler.
func TestHandler_NormalizesOmittedIntentVersion(t *testing.T) {
	reg, wreg := setupRegistry(t)
	disp := &stubDispatcher{response: json.RawMessage(`{"users":[]}`)}
	h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{})

	// IntentVersion omitted → marshals as 0.
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var resp contract.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// stubDispatcher echoes the request's IntentVersion into the meta.
	// If the transport forgot to normalize, this would be 0 (and the
	// dispatcher would have hit the handler-not-registered path in prod).
	if resp.Meta.IntentVersion != 1 {
		t.Errorf("resp.Meta.IntentVersion = %d; want 1 (resolved from registry)", resp.Meta.IntentVersion)
	}
}

func TestHandler_RejectsKindCapabilityMismatch(t *testing.T) {
	reg, wreg := setupRegistry(t)
	h := NewHandler(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{})

	// Send Kind=command for an intent whose Capability=read => mismatch
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "users.list", IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "BAD_REQUEST") {
		t.Errorf("expected BAD_REQUEST in body: %s", w.Body)
	}
}

func TestHandler_UnsupportedVersion(t *testing.T) {
	reg, wreg := setupRegistry(t)
	h := NewHandler(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{})

	body, _ := json.Marshal(contract.Request{
		Envelope: "v999", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "UNSUPPORTED_VERSION") {
		t.Errorf("expected UNSUPPORTED_VERSION: %s", w.Body)
	}
}

func TestHandler_CommandRequiresIdempotencyKey(t *testing.T) {
	reg, wreg := setupRegistry(t)
	h := NewHandler(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{})

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "user.disable", IntentVersion: 1,
		// CSRF and IdempotencyKey omitted
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandler_CommandRejectsInvalidCSRF(t *testing.T) {
	reg, wreg := setupRegistry(t)
	mgr := security.NewCSRFManager()
	h := NewHandlerWithCSRF(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{}, mgr)

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "user.disable", IntentVersion: 1,
		CSRF: "not-a-real-token", IdempotencyKey: "ik_1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "UNAUTHENTICATED") {
		t.Errorf("expected UNAUTHENTICATED in body: %s", w.Body)
	}
}

func TestHandler_CommandAcceptsValidCSRF(t *testing.T) {
	reg, wreg := setupRegistry(t)
	mgr := security.NewCSRFManager()
	tok := mgr.GenerateToken()
	disp := &stubDispatcher{response: json.RawMessage(`{"ok":true}`)}
	h := NewHandlerWithCSRF(reg, wreg, disp, contract.NoopAuditEmitter{}, mgr)

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "user.disable", IntentVersion: 1,
		CSRF: tok, IdempotencyKey: "ik_1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
}
