package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/security"
)

func TestCommandIdempotencyKeyThroughHTTPEnvelope(t *testing.T) {
	registry := contract.NewRegistry()

	var manifest contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(`schemaVersion: 1
contributor: {name: test, envelope: {supports: [v1], preferred: v1}}
intents:
  - {name: write, kind: command, version: 1, capability: write}
`), &manifest); err != nil {
		t.Fatal(err)
	}

	if err := registry.Register(&manifest); err != nil {
		t.Fatal(err)
	}

	d := New(nil)
	calls := 0

	if err := RegisterCommand(d, "test", "write", 1, func(ctx context.Context, _ struct{}, _ contract.Principal) (string, error) {
		calls++

		return CommandIdempotencyKey(ctx), nil
	}, BypassIdempotency()); err != nil {
		t.Fatal(err)
	}

	csrf := security.NewCSRFManager()

	handler := transport.NewHandlerWithCSRF(registry, contract.NewWardenRegistry(), d, contract.NoopAuditEmitter{}, csrf)
	for _, tc := range []struct {
		key, token string
		status     int
	}{
		{"", csrf.GenerateToken(), http.StatusBadRequest},
		{"envelope-key", "invalid", http.StatusForbidden},
		{"envelope-key", csrf.GenerateToken(), http.StatusOK},
	} {
		body, err := json.Marshal(contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "test", Intent: "write", IntentVersion: 1, IdempotencyKey: tc.key, CSRF: tc.token, Payload: json.RawMessage(`{"idempotency_key":"payload-key"}`)})
		if err != nil {
			t.Fatal(err)
		}

		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
		request.Header.Set("Idempotency-Key", "header-key")
		handler.ServeHTTP(response, request)

		if response.Code != tc.status {
			t.Fatalf("status: %d %s", response.Code, response.Body)
		}

		if tc.status == http.StatusOK {
			var envelope struct {
				Data string `json:"data"`
			}
			if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}

			if envelope.Data != "envelope-key" {
				t.Fatalf("transport replaced envelope key: %s", response.Body)
			}
		}
	}

	if calls != 1 {
		t.Fatalf("rejected commands reached handler: %d", calls)
	}
}
