package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

const errorSecret = "secret-token-postgres://private-dsn"

type countingErrorDispatcher struct {
	err      error
	calls    int
	response json.RawMessage
}

func (d *countingErrorDispatcher) Dispatch(_ context.Context, _ contract.Request, _ contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	d.calls++

	body := d.response
	if body == nil {
		body = json.RawMessage(`{"users":[]}`)
	}

	return body, contract.ResponseMeta{}, d.err
}

type errorWarden struct {
	err      error
	decision contract.Decision
}

func (w errorWarden) Authorize(_ context.Context, _ contract.Principal, _ contract.Action) (contract.Decision, error) {
	return w.decision, w.err
}

func errorRegistry(t *testing.T) (contract.Registry, contract.WardenRegistry) {
	t.Helper()
	reg, wreg := setupRegistry(t)

	m, ok := reg.Contributor("users")
	if !ok {
		t.Fatal("users contributor missing")
	}

	reg.Unregister("users")

	m.Intents[0].Requires.Warden = "policy"
	if err := reg.Register(m); err != nil {
		t.Fatal(err)
	}

	return reg, wreg
}

func errorRequest(t *testing.T, h http.Handler, envelope string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(contract.Request{
		Envelope: envelope, Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	return w
}

func assertErrorResponse(t *testing.T, w *httptest.ResponseRecorder, status int, want *contract.Error) {
	t.Helper()

	if w.Code != status {
		t.Errorf("status = %d; want %d; body=%s", w.Code, status, w.Body)
	}

	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	var got contract.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got.OK || got.Envelope != "v1" || !reflect.DeepEqual(got.Error, want) {
		t.Errorf("envelope = %#v, error = %#v; want v1/false and %#v", got, got.Error, want)
	}

	if strings.Contains(w.Body.String(), errorSecret) {
		t.Error("response contains private error or decision text")
	}

	if !strings.HasSuffix(w.Body.String(), "\n") {
		t.Error("response missing trailing newline")
	}
}

func TestHandler_ResponseEncodingFailure(t *testing.T) {
	cases := []struct {
		name     string
		response json.RawMessage
		err      error
	}{
		{"malformed-data", json.RawMessage(`{"secret":"` + errorSecret + `"`), nil},
		{"unsafe-details", nil, &contract.Error{Code: contract.CodeBadRequest, Message: errorSecret, Details: map[string]any{"unsupported": make(chan int)}}},
		{"marshaler-contract-error", nil, &contract.Error{Code: contract.CodeBadRequest, Details: map[string]any{"custom": failingMarshaler{}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			reg, wreg := setupRegistry(t)
			disp := &countingErrorDispatcher{err: test.err, response: test.response}
			w := errorRequest(t, NewHandler(reg, wreg, disp, nil), "v1")
			assertErrorResponse(t, w, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "internal error"})

			if disp.calls != 1 {
				t.Errorf("dispatch calls = %d; want 1", disp.calls)
			}
		})
	}
}

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, &contract.Error{Code: contract.CodeNotFound, Message: errorSecret, Details: map[string]any{"custom": failingMarshaler{}}}
}

type failingResponseWriter struct {
	header      http.Header
	statusCalls int
	writeCalls  int
}

func (w *failingResponseWriter) Header() http.Header { return w.header }

func (w *failingResponseWriter) WriteHeader(_ int) { w.statusCalls++ }

func (w *failingResponseWriter) Write(_ []byte) (int, error) {
	w.writeCalls++

	return 0, errors.New(errorSecret)
}

func TestResponseWriteFailureDoesNotRetry(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprintf("success=%t", success), func(t *testing.T) {
			w := &failingResponseWriter{header: make(http.Header)}
			wantStatusCalls := 1

			if success {
				writeOK(w, contract.Response{OK: true, Envelope: "v1", Data: json.RawMessage(`{"users":[]}`)})

				wantStatusCalls = 0
			} else {
				writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest})
			}

			if w.writeCalls != 1 || w.statusCalls != wantStatusCalls {
				t.Errorf("write calls = %d, status calls = %d; want 1/%d", w.writeCalls, w.statusCalls, wantStatusCalls)
			}
		})
	}
}

func TestHandler_PublicErrorResponses(t *testing.T) {
	codes := []struct {
		code   contract.ErrorCode
		status int
	}{
		{contract.CodeUnauthenticated, http.StatusUnauthorized},
		{contract.CodePermissionDenied, http.StatusForbidden},
		{contract.CodeBadRequest, http.StatusBadRequest},
		{contract.CodeNotFound, http.StatusNotFound},
		{contract.CodeConflict, http.StatusConflict},
		{contract.CodeRateLimited, http.StatusTooManyRequests},
		{contract.CodeUnavailable, http.StatusServiceUnavailable},
		{contract.CodeInternal, http.StatusInternalServerError},
		{contract.CodeUnsupportedVersion, http.StatusBadRequest},
	}
	for _, source := range []string{"warden", "dispatcher"} {
		for _, code := range codes {
			for _, wrapped := range []bool{false, true} {
				for _, allow := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/wrapped=%t/allow=%t", source, code.code, wrapped, allow), func(t *testing.T) {
						want := &contract.Error{
							Code: code.code, Message: "public explanation", Details: map[string]any{"dependency": "policy"},
							Retryable: true, CorrelationID: "public-correlation", Redactions: []string{"private.field"},
						}

						var returned error = want
						if wrapped {
							returned = fmt.Errorf("%s: %w", errorSecret, want)
						}

						reg, wreg := errorRegistry(t)
						disp := &countingErrorDispatcher{}
						warden := errorWarden{decision: contract.Decision{Allow: allow, Reason: errorSecret}}
						wantCalls := 0

						if source == "warden" {
							warden.err = returned
						} else {
							warden.decision.Allow = true
							disp.err = returned
							wantCalls = 1
						}

						if err := wreg.Register("policy", warden); err != nil {
							t.Fatal(err)
						}

						w := errorRequest(t, NewHandler(reg, wreg, disp, nil), "v1")
						assertErrorResponse(t, w, code.status, want)

						if disp.calls != wantCalls {
							t.Errorf("dispatch calls = %d; want %d", disp.calls, wantCalls)
						}
					})
				}
			}
		}
	}
}

func TestHandler_PrivateErrorResponses(t *testing.T) {
	var nilContract *contract.Error

	cases := []struct {
		name string
		err  error
	}{
		{"unknown", errors.New(errorSecret)},
		{"wrapped-unknown", fmt.Errorf("storage: %w", errors.New(errorSecret))},
		{"typed-nil", nilContract},
		{"wrapped-typed-nil", fmt.Errorf("%s: %w", errorSecret, nilContract)},
	}
	for _, source := range []string{"warden", "dispatcher"} {
		for _, test := range cases {
			for _, allow := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/allow=%t", source, test.name, allow), func(t *testing.T) {
					reg, wreg := errorRegistry(t)
					disp := &countingErrorDispatcher{}
					warden := errorWarden{decision: contract.Decision{Allow: allow, Reason: errorSecret}}
					wantCalls := 0

					if source == "warden" {
						warden.err = test.err
					} else {
						warden.decision.Allow = true
						disp.err = test.err
						wantCalls = 1
					}

					if err := wreg.Register("policy", warden); err != nil {
						t.Fatal(err)
					}

					w := errorRequest(t, NewHandler(reg, wreg, disp, nil), "v1")
					assertErrorResponse(t, w, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "internal error"})

					if disp.calls != wantCalls {
						t.Errorf("dispatch calls = %d; want %d", disp.calls, wantCalls)
					}
				})
			}
		}
	}
}

func TestHandler_WardenDecisionResponses(t *testing.T) {
	cases := []struct {
		name     string
		envelope string
		warden   *errorWarden
		status   int
		want     *contract.Error
	}{
		{"plain-deny", "v1", &errorWarden{decision: contract.Decision{Reason: "policy denied"}}, http.StatusForbidden, &contract.Error{Code: contract.CodePermissionDenied, Message: "policy denied"}},
		{"missing-warden", "v1", nil, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "warden not registered"}},
		{"unsupported-envelope", "v999", nil, http.StatusBadRequest, &contract.Error{Code: contract.CodeUnsupportedVersion, Message: "envelope v999 unsupported"}},
		{"permitted", "v1", &errorWarden{decision: contract.Decision{Allow: true}}, http.StatusOK, nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			reg, wreg := errorRegistry(t)
			disp := &countingErrorDispatcher{}

			if test.warden != nil {
				if err := wreg.Register("policy", *test.warden); err != nil {
					t.Fatal(err)
				}
			}

			w := errorRequest(t, NewHandler(reg, wreg, disp, nil), test.envelope)
			if test.want != nil {
				assertErrorResponse(t, w, test.status, test.want)

				if disp.calls != 0 {
					t.Errorf("dispatcher called on refusal: %d", disp.calls)
				}

				return
			}

			var got contract.Response
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}

			if w.Code != test.status || !got.OK || got.Envelope != "v1" || got.Kind != contract.KindQuery || string(got.Data) != `{"users":[]}` || disp.calls != 1 {
				t.Errorf("permitted response: status=%d body=%s dispatch calls=%d", w.Code, w.Body, disp.calls)
			}

			if !strings.HasSuffix(w.Body.String(), "\n") {
				t.Error("response missing trailing newline")
			}
		})
	}
}
