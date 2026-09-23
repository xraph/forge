package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type invalidationDispatcher struct {
	invalidates []string
	err         error
}

func (disp invalidationDispatcher) Dispatch(context.Context, contract.Request, contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	return json.RawMessage(`{"ok":true}`), contract.ResponseMeta{Invalidates: disp.invalidates}, disp.err
}

func TestHandler_CommandInvalidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		extra []string
		err   error
		want  []string
	}{
		{name: "manifest", want: []string{"auth.featureToggles"}},
		{name: "forwarded and extra", extra: []string{"auth.featureToggles", "auth.config"}, want: []string{"auth.featureToggles", "auth.config"}},
		{name: "failure", err: errors.New("write failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			reg := contract.NewRegistry()
			manifest := &contract.ContractManifest{}
			if err := contract.UnmarshalManifestForTest([]byte(`
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: auth.toggleFeature, kind: command, version: 1, capability: write, invalidates: [auth.featureToggles] }
`), manifest); err != nil {
				t.Fatal(err)
			}
			if err := reg.Register(manifest); err != nil {
				t.Fatal(err)
			}
			handler := NewHandler(reg, contract.NewWardenRegistry(), invalidationDispatcher{test.extra, test.err}, nil)
			request := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", strings.NewReader(`{"envelope":"v1","kind":"command","contributor":"auth","intent":"auth.toggleFeature","csrf":"test","idempotencyKey":"test"}`))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			var envelope contract.Response
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.OK != (test.err == nil) {
				t.Fatalf("unexpected response: %s", response.Body)
			}
			if !reflect.DeepEqual(envelope.Meta.Invalidates, test.want) {
				t.Fatalf("invalidates = %v, want %v", envelope.Meta.Invalidates, test.want)
			}
		})
	}
}
