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

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
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

// setupGatedRegistry mirrors setupRegistry but declares a predicate on
// users.list, so requests reach the authorization branch at all. Every
// intent in the repo's manifests today declares nothing, which is why this
// fixture has to exist rather than reusing the one above.
func setupGatedRegistry(t *testing.T) (contract.Registry, contract.WardenRegistry) {
	t.Helper()
	r := contract.NewRegistry()
	src := `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list, kind: query, version: 1, capability: read, requires: { any: ["role:admin"] } }
  - { name: users.open, kind: query, version: 1, capability: read }
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

func gatedRequest(t *testing.T, intent string, user *dashauth.UserInfo) *httptest.ResponseRecorder {
	t.Helper()
	reg, wreg := setupGatedRegistry(t)
	disp := &stubDispatcher{response: json.RawMessage(`{"users":[]}`)}
	h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{})

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: intent, IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	if user != nil {
		req = req.WithContext(dashauth.WithUser(req.Context(), user))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestHandler_UnauthenticatedIsNot403 pins the difference a client cannot
// work without. A caller with no identity has to be told to sign in; a
// caller with an identity that falls short of the predicate has to be told
// it fell short. Both answered 403 PERMISSION_DENIED before, so the shell
// had no way to tell "sign in" from "you are not allowed" and could only
// guess which screen to render.
func TestHandler_UnauthenticatedIsNot403(t *testing.T) {
	w := gatedRequest(t, "users.list", nil)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), string(contract.CodeUnauthenticated)) {
		t.Errorf("body = %s, want code %s", w.Body, contract.CodeUnauthenticated)
	}
}

func TestHandler_AuthenticatedButUnauthorizedIs403(t *testing.T) {
	w := gatedRequest(t, "users.list", &dashauth.UserInfo{Subject: "u1", Roles: []string{"viewer"}})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), string(contract.CodePermissionDenied)) {
		t.Errorf("body = %s, want code %s", w.Body, contract.CodePermissionDenied)
	}
}

func TestHandler_AuthenticatedAndAuthorizedPasses(t *testing.T) {
	w := gatedRequest(t, "users.list", &dashauth.UserInfo{Subject: "u1", Roles: []string{"admin"}})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body)
	}
}

// TestHandler_NoPredicateStillAllowsAnonymous is the regression guard that
// matters most here. Every intent in every manifest in the repo declares no
// predicate today, so a change that answered 401 whenever the user is nil
// would lock every existing dashboard out of itself, including the login
// intent that has to work while signed out.
func TestHandler_NoPredicateStillAllowsAnonymous(t *testing.T) {
	w := gatedRequest(t, "users.open", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body)
	}
}
