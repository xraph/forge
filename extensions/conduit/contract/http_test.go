package contract

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/security"
)

func TestHTTPAuthenticationScopeAndCSRF(t *testing.T) {
	deps := testDeps(t)

	registry, wardens, disp := dash.NewRegistry(), dash.NewWardenRegistry(), dispatcher.New(nil)
	if err := Register(disp, registry, wardens, deps); err != nil {
		t.Fatal(err)
	}

	csrf := security.NewCSRFManager()
	handler := transport.NewHandlerWithCSRF(registry, wardens, disp, nil, csrf)
	query := func(claims map[string]any) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/dashboard/api/dashboard/v1", bytes.NewBufferString(`{"envelope":"v1","kind":"query","contributor":"conduit","intent":"overview"}`))
		request = request.WithContext(dashauth.WithUser(request.Context(), &dashauth.UserInfo{Claims: claims}))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		return response
	}

	response := query(map[string]any{"namespace": "prod", "service_id": "billing"})
	if response.Code != http.StatusOK {
		t.Fatalf("query failed: %d %s", response.Code, response.Body)
	}

	var result struct {
		Data struct {
			Streams       []any `json:"streams"`
			Subscriptions []any `json:"subscriptions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	if result.Data.Streams == nil || result.Data.Subscriptions == nil {
		t.Fatal("empty arrays became null")
	}

	if response := query(map[string]any{"namespace": "another"}); response.Code != http.StatusForbidden {
		t.Fatalf("wrong namespace returned %d", response.Code)
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/dashboard/api/dashboard/v1", bytes.NewBufferString(`{"envelope":"v1","kind":"command","contributor":"conduit","intent":"deadletters.replay","payload":{"provider":"events","subscription":"work","id":"x"},"csrf":"invalid","idempotencyKey":"test"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF accepted: %d", response.Code)
	}
}
