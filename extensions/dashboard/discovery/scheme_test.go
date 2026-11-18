package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// recordingTransport replaces http.DefaultTransport for a test, so the
// integration's default client can be pointed at non-loopback hostnames
// without touching the network. It answers every request with a manifest.
type recordingTransport struct {
	mu       sync.Mutex
	requests []*http.Request
	body     []byte
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.requests = append(rt.requests, r)
	rt.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(rt.body)),
		Request:    r,
	}, nil
}

func (rt *recordingTransport) seen() []*http.Request {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	return append([]*http.Request(nil), rt.requests...)
}

// useRecordingTransport swaps http.DefaultTransport for the test. Tests that
// call it must not run in parallel.
func useRecordingTransport(t *testing.T) *recordingTransport {
	t.Helper()

	raw, err := json.Marshal(contract.ContractManifest{
		SchemaVersion: 1,
		Contributor: contract.Contributor{
			Name:     "reports",
			Envelope: contract.EnvelopeSupport{Supports: []string{"v1"}, Preferred: "v1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rt := &recordingTransport{body: raw}
	prev := http.DefaultTransport
	http.DefaultTransport = rt

	t.Cleanup(func() { http.DefaultTransport = prev })

	return rt
}

func remoteInstance(metadata map[string]string, tags ...string) *ServiceInstance {
	return &ServiceInstance{
		ID: "reports-1", Name: "reports-svc", Address: "svc.internal", Port: 8443,
		Tags: append([]string{DefaultTag}, tags...), Metadata: metadata, Status: "passing",
	}
}

func harnessWith(allowInsecure bool) *harness {
	h := &harness{reg: contract.NewRegistry(), disc: newFakeDiscovery()}
	h.integ = NewIntegration(h.disc, Config{
		Register:      h.reg.RegisterRemote,
		Unregister:    h.reg.Unregister,
		AllowInsecure: allowInsecure,
	})

	return h
}

// An instance on another host that advertises https is fetched over https,
// with its API key, and registered at its https address.
func TestSync_AdvertisedHTTPSInstanceFetchedOverHTTPSWithKey(t *testing.T) {
	rt := useRecordingTransport(t)
	h := harnessWith(false)
	h.disc.set("reports-svc", remoteInstance(map[string]string{SchemeMetadata: "https", APIKeyMetadata: "s3cret"}))

	h.integ.Reconcile(context.Background())

	reqs := rt.seen()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want one manifest fetch", len(reqs))
	}

	if got := reqs[0].URL.String(); got != "https://svc.internal:8443/_forge/contract/manifest" {
		t.Errorf("manifest fetched from %s, want the https address", got)
	}

	if got := reqs[0].Header.Get("Authorization"); got != "Bearer s3cret" {
		t.Errorf("Authorization = %q, want the instance's key over https", got)
	}

	ep := h.remote(t, "reports")
	if ep.BaseURL != "https://svc.internal:8443" || ep.APIKey != "s3cret" {
		t.Errorf("endpoint = %+v, want the https address and key", ep)
	}
}

// Regression: an instance on another host reached over plain http was sent
// a manifest request carrying its API key. Without AllowInsecure it is now
// refused before any request.
func TestSync_PlainHTTPRemoteRefusedBeforeFetch(t *testing.T) {
	rt := useRecordingTransport(t)
	h := harnessWith(false)
	h.disc.set("reports-svc", remoteInstance(map[string]string{APIKeyMetadata: "s3cret"}))

	h.integ.Reconcile(context.Background())

	if reqs := rt.seen(); len(reqs) != 0 {
		t.Fatalf("plain http instance was contacted: %s", reqs[0].URL)
	}

	if got := h.integ.Tracked(); len(got) != 0 {
		t.Errorf("plain http instance registered: %v", got)
	}
}

// With AllowInsecure the plain http instance is fetched, but without its key.
func TestSync_AllowInsecureFetchesWithoutKey(t *testing.T) {
	rt := useRecordingTransport(t)
	h := harnessWith(true)
	h.disc.set("reports-svc", remoteInstance(map[string]string{APIKeyMetadata: "s3cret"}))

	h.integ.Reconcile(context.Background())

	reqs := rt.seen()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want one manifest fetch", len(reqs))
	}

	if reqs[0].URL.Scheme != "http" {
		t.Errorf("scheme = %s, want http", reqs[0].URL.Scheme)
	}

	if got := reqs[0].Header.Get("Authorization"); got != "" {
		t.Errorf("API key sent over plain http: %q", got)
	}
}

func TestServiceInstance_Scheme(t *testing.T) {
	cases := []struct {
		name string
		inst *ServiceInstance
		want string
	}{
		{"nothing advertised", remoteInstance(nil), "http"},
		{"forge-scheme", remoteInstance(map[string]string{SchemeMetadata: "https"}), "https"},
		{"scheme key, any case", remoteInstance(map[string]string{"scheme": " HTTPS "}), "https"},
		{"protocol key", remoteInstance(map[string]string{"protocol": "https"}), "https"},
		{"https tag", remoteInstance(nil, "https"), "https"},
		{"http advertised", remoteInstance(map[string]string{SchemeMetadata: "http"}), "http"},
	}

	for _, c := range cases {
		if got := c.inst.Scheme(); got != c.want {
			t.Errorf("%s: Scheme() = %s, want %s", c.name, got, c.want)
		}
	}

	if got := remoteInstance(nil, "https").BaseURL(); got != "https://svc.internal:8443" {
		t.Errorf("BaseURL() = %s", got)
	}
}
