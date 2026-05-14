package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// remoteRegistry is a hand-rolled minimal Registry just for the dispatcher
// tests so we don't have to spin up the full registry.go merging logic
// when all we care about is the Remote() lookup.
type remoteRegistry struct {
	contract.Registry
	endpoints map[string]contract.RemoteEndpoint
}

func (r *remoteRegistry) Remote(name string) (contract.RemoteEndpoint, bool) {
	ep, ok := r.endpoints[name]
	return ep, ok
}

func TestForwardingDispatcher_NotFoundWhenNoRemote(t *testing.T) {
	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{}}
	f := NewForwardingDispatcher(reg)
	_, _, err := f.Dispatch(context.Background(), contract.Request{Contributor: "missing"}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestForwardingDispatcher_RoundTripsSuccessEnvelope(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req contract.Request
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req.Intent != "things.list" {
			t.Errorf("upstream got intent=%q, want things.list", req.Intent)
		}
		_ = json.NewEncoder(w).Encode(contract.Response{
			OK:       true,
			Envelope: "v1",
			Kind:     contract.KindQuery,
			Data:     json.RawMessage(`{"items":["a","b"]}`),
			Meta:     contract.ResponseMeta{IntentVersion: 1},
		})
	}))
	defer upstream.Close()

	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{
		"things": {BaseURL: upstream.URL},
	}}
	f := NewForwardingDispatcher(reg)

	data, meta, err := f.Dispatch(context.Background(), contract.Request{
		Envelope:      "v1",
		Kind:          contract.KindQuery,
		Contributor:   "things",
		Intent:        "things.list",
		IntentVersion: 1,
	}, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(data) != `{"items":["a","b"]}` {
		t.Errorf("data = %s", data)
	}
	if meta.IntentVersion != 1 {
		t.Errorf("meta.IntentVersion = %d", meta.IntentVersion)
	}
}

func TestForwardingDispatcher_SurfacesUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(contract.ErrorResponse{
			OK:       false,
			Envelope: "v1",
			Error:    &contract.Error{Code: contract.CodeBadRequest, Message: "upstream-said-no"},
		})
	}))
	defer upstream.Close()

	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{
		"things": {BaseURL: upstream.URL},
	}}
	f := NewForwardingDispatcher(reg)
	_, _, err := f.Dispatch(context.Background(), contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeBadRequest || !strings.Contains(ce.Message, "upstream-said-no") {
		t.Errorf("expected upstream error to surface verbatim, got %v", err)
	}
}

func TestForwardingDispatcher_ForwardsAuthHeaders(t *testing.T) {
	var sawAuthz, sawCookie, sawAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuthz = r.Header.Get("X-Forwarded-Authorization")
		sawCookie = r.Header.Get("X-Forwarded-Cookie")
		sawAPIKey = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(contract.Response{OK: true, Envelope: "v1", Kind: contract.KindQuery, Data: json.RawMessage(`{}`)})
	}))
	defer upstream.Close()

	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{
		"things": {BaseURL: upstream.URL, APIKey: "dashboard-secret"},
	}}
	f := NewForwardingDispatcher(reg)

	// Build an inbound request carrying user auth + cookie.
	inbound := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", nil)
	inbound.Header.Set("Authorization", "Bearer user-token")
	inbound.Header.Set("Cookie", "session=abc")
	ctx := dashauth.WithHTTP(context.Background(), httptest.NewRecorder(), inbound)

	_, _, err := f.Dispatch(ctx, contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sawAuthz != "Bearer user-token" {
		t.Errorf("upstream did not see forwarded user auth, got %q", sawAuthz)
	}
	if sawCookie != "session=abc" {
		t.Errorf("upstream did not see forwarded cookie, got %q", sawCookie)
	}
	if sawAPIKey != "Bearer dashboard-secret" {
		t.Errorf("upstream did not see dashboard API key, got %q", sawAPIKey)
	}
}

func TestForwardingDispatcher_NetworkErrorMapsToCodeUnavailable(t *testing.T) {
	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{
		// Reserved-for-documentation network: every dial will fail fast.
		"things": {BaseURL: "http://198.51.100.1:65535"},
	}}
	f := NewForwardingDispatcher(reg)
	ctx, cancel := context.WithTimeout(context.Background(), 50*1000*1000) // 50ms
	defer cancel()
	_, _, err := f.Dispatch(ctx, contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

func TestFetchManifest_DecodesJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(contract.ContractManifest{
			SchemaVersion: 1,
			Contributor:   contract.Contributor{Name: "things"},
		})
	}))
	defer upstream.Close()

	m, err := FetchManifest(context.Background(), upstream.URL, "", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if m.Contributor.Name != "things" {
		t.Errorf("manifest name = %q", m.Contributor.Name)
	}
}

func TestFetchManifest_Non2xx(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no manifest here"))
	}))
	defer upstream.Close()
	_, err := FetchManifest(context.Background(), upstream.URL, "", nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 surfaced in error, got %v", err)
	}
}

func TestFetchManifest_MissingContributorName(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(contract.ContractManifest{SchemaVersion: 1})
	}))
	defer upstream.Close()
	_, err := FetchManifest(context.Background(), upstream.URL, "", nil)
	if err == nil || !strings.Contains(err.Error(), "missing contributor.name") {
		t.Errorf("expected missing-name error, got %v", err)
	}
}
