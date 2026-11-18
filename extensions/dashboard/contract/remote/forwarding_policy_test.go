package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

func okUpstream(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h != nil {
			h(w, r)
		}

		_ = json.NewEncoder(w).Encode(contract.Response{OK: true, Envelope: "v1", Kind: contract.KindQuery, Data: json.RawMessage(`{}`)})
	}))
	t.Cleanup(srv.Close)

	return srv
}

// Regression: the user's Cookie and Authorization used to be forwarded to
// every remote contributor. By default only the authenticated subject goes.
func TestForwardingDispatcher_DefaultSendsIdentityNotCredentials(t *testing.T) {
	var got http.Header

	upstream := okUpstream(t, func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Clone() })

	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{"things": {BaseURL: upstream.URL, APIKey: "dashboard-secret"}}}
	f := NewForwardingDispatcher(reg)

	inbound := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/dashboard/v1", nil)
	inbound.Header.Set("Authorization", "Bearer user-token")
	inbound.Header.Set("Cookie", "session=abc")

	ctx := dashauth.WithHTTP(context.Background(), httptest.NewRecorder(), inbound)
	ctx = dashauth.WithUser(ctx, &dashauth.UserInfo{Subject: "alice"})

	if _, _, err := f.Dispatch(ctx, contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	for _, h := range []string{"X-Forwarded-Authorization", "X-Forwarded-Cookie", "Cookie"} {
		if v := got.Get(h); v != "" {
			t.Errorf("%s forwarded by default: %q", h, v)
		}
	}

	if got.Get("Authorization") != "Bearer dashboard-secret" {
		t.Errorf("Authorization = %q, want the dashboard's own key", got.Get("Authorization"))
	}

	if got.Get(ForwardedUserHeader) != "alice" {
		t.Errorf("%s = %q, want alice", ForwardedUserHeader, got.Get(ForwardedUserHeader))
	}
}

// Regression: an upstream's Set-Cookie used to be re-emitted on the host
// origin for every remote.
func TestForwardingDispatcher_DropsSetCookieByDefault(t *testing.T) {
	upstream := okUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "planted", Path: "/"})
	})

	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{"things": {BaseURL: upstream.URL}}}
	rec := httptest.NewRecorder()
	ctx := dashauth.WithHTTP(context.Background(), rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil))

	if _, _, err := NewForwardingDispatcher(reg).Dispatch(ctx, contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if c := rec.Result().Cookies(); len(c) != 0 {
		t.Errorf("upstream cookies re-emitted on the host: %v", c)
	}
}

// Regression: the upstream body was read with an unbounded io.ReadAll.
func TestForwardingDispatcher_CapsResponseBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"envelope":"v1","kind":"query","data":"` + strings.Repeat("a", 4096) + `"}`))
	}))
	t.Cleanup(upstream.Close)

	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{"things": {BaseURL: upstream.URL}}}

	_, _, err := NewForwardingDispatcher(reg, WithMaxResponseBytes(1024)).Dispatch(context.Background(), contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{})

	ce := &contract.Error{}
	if !errors.As(err, &ce) || !strings.Contains(ce.Message, "exceeds 1024 bytes") {
		t.Fatalf("oversized body not refused: %v", err)
	}

	if _, _, err := NewForwardingDispatcher(reg).Dispatch(context.Background(), contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{}); err != nil {
		t.Fatalf("body under the default cap refused: %v", err)
	}
}

// Regression: credentials went to non-loopback contributors over plain http.
func TestForwardingDispatcher_RefusesPlainHTTPToRemoteHosts(t *testing.T) {
	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{"things": {BaseURL: "http://198.51.100.1:65535", APIKey: "k"}}}

	_, _, err := NewForwardingDispatcher(reg).Dispatch(context.Background(), contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{})

	ce := &contract.Error{}
	if !errors.As(err, &ce) || !strings.Contains(ce.Message, "refusing plain http") {
		t.Fatalf("plain http to a remote host not refused: %v", err)
	}
}

func TestCheckTransport(t *testing.T) {
	cases := []struct {
		url   string
		allow bool
		ok    bool
	}{
		{"https://svc.internal:8443", false, true},
		{"http://127.0.0.1:8080", false, true},
		{"http://localhost:8080", false, true},
		{"http://[::1]:8080", false, true},
		{"http://svc.internal:8080", false, false},
		{"HTTP://10.0.0.5", false, false},
		{"http://svc.internal:8080", true, true},
	}

	for _, c := range cases {
		if err := CheckTransport(c.url, c.allow); (err == nil) != c.ok {
			t.Errorf("CheckTransport(%q, %v) = %v, want ok=%v", c.url, c.allow, err, c.ok)
		}
	}
}
