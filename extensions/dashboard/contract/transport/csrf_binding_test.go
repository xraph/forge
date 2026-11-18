package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/security"
)

type recordingDispatcher struct{ called bool }

func (d *recordingDispatcher) Dispatch(context.Context, contract.Request, contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	d.called = true

	return json.RawMessage(`{"deleted":true}`), contract.ResponseMeta{}, nil
}

func adminCommandRegistry(t *testing.T) contract.Registry {
	t.Helper()

	reg := contract.NewRegistry()

	var m contract.ContractManifest

	src := `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.delete, kind: command, version: 1, capability: write, requires: { all: ["role:admin"] } }
`
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}

	if err := reg.Register(&m); err != nil {
		t.Fatal(err)
	}

	return reg
}

func mintToken(t *testing.T, mgr *security.CSRFManager, user *dashauth.UserInfo) CSRFTokenResponse {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/csrf", nil)
	if user != nil {
		req = req.WithContext(dashauth.WithUser(req.Context(), user))
	}

	rec := httptest.NewRecorder()
	NewCSRFTokenHandler(mgr, 0).ServeHTTP(rec, req)

	var tok CSRFTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
		t.Fatalf("decode token: %v", err)
	}

	return tok
}

type commandCase struct {
	token, contentType string
	headers            map[string]string
}

func sendDelete(t *testing.T, h http.Handler, user *dashauth.UserInfo, c commandCase) *httptest.ResponseRecorder {
	t.Helper()

	body := `{"envelope":"v1","kind":"command","contributor":"users","intent":"users.delete","idempotencyKey":"x1","csrf":"` + c.token + `","params":{"id":"42"}}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://dash.example/dashboard/api/dashboard/v1", strings.NewReader(body))

	if c.contentType != "" {
		req.Header.Set("Content-Type", c.contentType)
	}

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	req = req.WithContext(dashauth.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

var victim = &dashauth.UserInfo{Subject: "victim", Roles: []string{"admin"}}

// Regression for the reviewed attack: an attacker mints a token anonymously
// and replays it inside a cross-site text/plain form that the victim's
// browser submits with the victim's session. Each of the three defences must
// stop it on its own, so each is tested with the other two satisfied.
func TestCommand_AttackerMintedTokenRefused(t *testing.T) {
	mgr := security.NewCSRFManager()
	attackerTok := mintToken(t, mgr, nil).Token
	victimTok := mintToken(t, mgr, victim).Token

	cases := []struct {
		name string
		c    commandCase
		want int
	}{
		{"original PoC: cross-site text/plain with attacker token",
			commandCase{attackerTok, "text/plain", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}}, http.StatusForbidden},
		{"attacker token, otherwise well-formed same-origin request",
			commandCase{attackerTok, "application/json", map[string]string{"Sec-Fetch-Site": "same-origin"}}, http.StatusForbidden},
		{"victim token but text/plain",
			commandCase{victimTok, "text/plain", nil}, http.StatusUnsupportedMediaType},
		{"victim token, JSON, but marked cross-site",
			commandCase{victimTok, "application/json", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}}, http.StatusForbidden},
		{"victim token, JSON, foreign Origin from a browser without Fetch Metadata",
			commandCase{victimTok, "application/json", map[string]string{"Origin": "https://evil.example"}}, http.StatusForbidden},
		{"victim token, JSON, null Origin",
			commandCase{victimTok, "application/json", map[string]string{"Origin": "null"}}, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			disp := &recordingDispatcher{}
			h := NewHandlerWithCSRF(adminCommandRegistry(t), contract.NewWardenRegistry(), disp, nil, mgr)

			rec := sendDelete(t, h, victim, tc.c)
			if rec.Code != tc.want || disp.called {
				t.Fatalf("got %d dispatched=%v, want %d and no dispatch; body=%s", rec.Code, disp.called, tc.want, rec.Body)
			}
		})
	}
}

func TestCommand_LegitimateRequestsStillWork(t *testing.T) {
	mgr := security.NewCSRFManager()
	victimTok := mintToken(t, mgr, victim).Token

	cases := []struct {
		name string
		c    commandCase
		opts []HandlerOption
	}{
		{"same-origin browser", commandCase{victimTok, "application/json; charset=utf-8", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://dash.example"}}, nil},
		{"older browser, matching Origin", commandCase{victimTok, "application/json", map[string]string{"Origin": "http://dash.example"}}, nil},
		{"proxy rewrote Host", commandCase{victimTok, "application/json", map[string]string{"Origin": "https://public.example", "X-Forwarded-Host": "public.example"}}, nil},
		{"non-browser client", commandCase{victimTok, "application/json", nil}, nil},
		{"trusted cross-site origin", commandCase{victimTok, "application/json", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://app.example"}},
			[]HandlerOption{WithTrustedOrigins("https://app.example/")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			disp := &recordingDispatcher{}
			h := NewHandlerWithCSRF(adminCommandRegistry(t), contract.NewWardenRegistry(), disp, nil, mgr, tc.opts...)

			rec := sendDelete(t, h, victim, tc.c)
			if rec.Code != http.StatusOK || !disp.called {
				t.Fatalf("got %d dispatched=%v, want 200; body=%s", rec.Code, disp.called, rec.Body)
			}
		})
	}
}

// The token endpoint used to advertise 12h while validation honoured 1h.
func TestCSRFTokenHandler_ExpiryMatchesValidation(t *testing.T) {
	mgr := security.NewCSRFManager()

	for _, ttl := range []time.Duration{0, 12 * time.Hour, mgr.MaxAge()} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/csrf", nil)
		rec := httptest.NewRecorder()
		before := time.Now()

		NewCSRFTokenHandler(mgr, ttl).ServeHTTP(rec, req)

		var tok CSRFTokenResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
			t.Fatal(err)
		}

		if tok.ExpiresAt.After(before.Add(mgr.MaxAge()).Add(time.Second)) {
			t.Errorf("ttl %v: advertised expiry %v is past the validation window", ttl, tok.ExpiresAt)
		}
	}

	// A shorter ttl is honoured as given.
	rec := httptest.NewRecorder()
	before := time.Now()

	NewCSRFTokenHandler(mgr, 10*time.Minute).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/csrf", nil))

	var tok CSRFTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}

	if d := tok.ExpiresAt.Sub(before); d > 11*time.Minute || d < 9*time.Minute {
		t.Errorf("short ttl advertised as %v", d)
	}
}
