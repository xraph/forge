package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// roundTripper answers every request without touching the network and
// records the requests it saw, so tests can use non-loopback hostnames.
type roundTripper struct {
	mu   sync.Mutex
	seen []*http.Request
	body []byte
}

func (rt *roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.seen = append(rt.seen, r)
	rt.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(rt.body)),
		Request:    r,
	}, nil
}

func manifestJSON(t *testing.T) []byte {
	t.Helper()

	raw, err := json.Marshal(contract.ContractManifest{SchemaVersion: 1, Contributor: contract.Contributor{Name: "things"}})
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

// Regression: FetchManifest always sent the API key, so a discovered
// instance reached over plain http got it in clear text.
func TestFetchManifest_APIKeyOnlyOverSecureTransport(t *testing.T) {
	cases := []struct {
		url      string
		sendsKey bool
	}{
		{"http://svc.internal:8080", false},
		{"http://10.0.0.5:8080", false},
		{"https://svc.internal:8443", true},
		{"http://127.0.0.1:8080", true},
		{"http://localhost:8080", true},
	}

	for _, c := range cases {
		rt := &roundTripper{body: manifestJSON(t)}

		if _, err := FetchManifest(context.Background(), c.url, "s3cret", &http.Client{Transport: rt}); err != nil {
			t.Fatalf("%s: %v", c.url, err)
		}

		got := rt.seen[0].Header.Get("Authorization")
		if c.sendsKey && got != "Bearer s3cret" {
			t.Errorf("%s: Authorization = %q, want the key", c.url, got)
		}

		if !c.sendsKey && got != "" {
			t.Errorf("%s: API key sent over plain http: %q", c.url, got)
		}
	}
}

// Regression: a manifest over 64 KiB was silently truncated.
func TestFetchManifest_OversizedIsAnError(t *testing.T) {
	big := `{"schemaVersion":1,"contributor":{"name":"things"},"pad":"` + strings.Repeat("a", int(MaxManifestBytes)) + `"}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	t.Cleanup(upstream.Close)

	_, err := FetchManifest(context.Background(), upstream.URL, "", nil)
	if !errors.Is(err, ErrManifestTooLarge) {
		t.Fatalf("err = %v, want ErrManifestTooLarge", err)
	}

	// Between the old 64 KiB cut and the cap, a manifest now decodes.
	mid := `{"schemaVersion":1,"contributor":{"name":"things"},"pad":"` + strings.Repeat("a", 100<<10) + `"}`

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(mid))
	}))
	t.Cleanup(upstream2.Close)

	if _, err := FetchManifest(context.Background(), upstream2.URL, "", nil); err != nil {
		t.Fatalf("100 KiB manifest: %v", err)
	}
}

// An endpoint allowed onto plain http still does not get the API key.
func TestForwardingDispatcher_NoAPIKeyOverInsecureHTTP(t *testing.T) {
	ok, err := json.Marshal(contract.Response{OK: true, Envelope: "v1", Kind: contract.KindQuery, Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}

	rt := &roundTripper{body: ok}
	reg := &remoteRegistry{endpoints: map[string]contract.RemoteEndpoint{
		"things": {BaseURL: "http://svc.internal:8080", APIKey: "s3cret", AllowInsecureHTTP: true, Client: &http.Client{Transport: rt}},
	}}

	if _, _, err := NewForwardingDispatcher(reg).Dispatch(context.Background(), contract.Request{Contributor: "things", Intent: "x"}, contract.Principal{}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if got := rt.seen[0].Header.Get("Authorization"); got != "" {
		t.Errorf("API key sent over plain http: %q", got)
	}
}

func TestIsSecureTransport(t *testing.T) {
	for url, want := range map[string]bool{
		"https://svc":         true,
		"http://127.0.0.1:80": true,
		"http://[::1]:80":     true,
		"http://localhost":    true,
		"http://svc.internal": false,
		"ftp://svc":           false,
		"://bad":              false,
	} {
		if got := IsSecureTransport(url); got != want {
			t.Errorf("IsSecureTransport(%q) = %v, want %v", url, got, want)
		}
	}
}
