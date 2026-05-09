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
