package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestHandlerUsesCanonicalErrorStatus(t *testing.T) {
	for _, test := range []struct {
		code   contract.ErrorCode
		status int
	}{{contract.CodeBadRequest, 400}, {contract.CodePermissionDenied, 403}, {contract.CodeNotFound, 404}, {contract.CodeConflict, 409}, {contract.CodeRateLimited, 429}, {contract.CodeUnavailable, 503}, {contract.CodeInternal, 500}} {
		t.Run(string(test.code), func(t *testing.T) {
			registry, wardens := setupRegistry(t)
			disp := &errorDispatcher{code: test.code}
			handler := NewHandler(registry, wardens, disp, nil)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/dashboard/v1", strings.NewReader(`{"envelope":"v1","kind":"query","contributor":"users","intent":"users.list"}`))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status %d, want %d: %s", response.Code, test.status, response.Body)
			}
		})
	}

	wrapped := fmt.Errorf("context: %w", contract.ErrPermissionDenied)
	if code := asContractError(wrapped).Code; code != contract.CodePermissionDenied {
		t.Fatalf("wrapped error lost its code: %s", code)
	}
}

type errorDispatcher struct{ code contract.ErrorCode }

func (d *errorDispatcher) Dispatch(_ context.Context, _ contract.Request, _ contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	return nil, contract.ResponseMeta{}, &contract.Error{Code: d.code, Message: "intent refused"}
}
