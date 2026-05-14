package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
	"github.com/xraph/forge/extensions/dashboard/contract/remote"
)

// upstreamEnv builds a complete remote-service stack: a registry holding
// a manifest, a dispatcher with a query handler bound, and a Server
// exposing /_forge/contract/manifest and /_forge/contract/dispatch via
// httptest. Mirrors the smallest possible "service exposing contract
// intents" wire-up.
func upstreamEnv(t *testing.T) (*httptest.Server, contract.Registry) {
	t.Helper()
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()
	yaml := `
schemaVersion: 1
contributor: { name: things, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: things.list, kind: query, version: 1, capability: read }
`
	m, err := loader.Load(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := reg.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}

	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	type out struct {
		Items []string `json:"items"`
	}
	if err := dispatcher.RegisterQuery(d, "things", "things.list", 1,
		func(_ context.Context, _ struct{}, _ contract.Principal) (out, error) {
			return out{Items: []string{"a", "b"}}, nil
		},
	); err != nil {
		t.Fatalf("register handler: %v", err)
	}

	s := New(reg, wreg, d, contract.NoopAuditEmitter{})
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, reg
}

func TestServer_ManifestEndpoint(t *testing.T) {
	srv, _ := upstreamEnv(t)
	res, err := http.Get(srv.URL + "/_forge/contract/manifest?contributor=things")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var m contract.ContractManifest
	_ = json.NewDecoder(res.Body).Decode(&m)
	if m.Contributor.Name != "things" {
		t.Errorf("contributor = %q", m.Contributor.Name)
	}
}

func TestServer_SingleContributorReturnedDirectly(t *testing.T) {
	// When exactly one contributor is registered, /manifest with no
	// ?contributor query returns the single manifest verbatim. This is the
	// shape the legacy templ flow's /_forge/dashboard/manifest serves and
	// what remote.FetchManifest expects by default.
	srv, _ := upstreamEnv(t)
	res, err := http.Get(srv.URL + "/_forge/contract/manifest")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	var m contract.ContractManifest
	_ = json.NewDecoder(res.Body).Decode(&m)
	if m.Contributor.Name != "things" {
		t.Errorf("single-manifest shape = %+v", m)
	}
}

func TestServer_DispatchEndpoint(t *testing.T) {
	srv, _ := upstreamEnv(t)
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "things", Intent: "things.list", IntentVersion: 1,
	})
	res, err := http.Post(srv.URL+"/_forge/contract/dispatch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var resp contract.Response
	_ = json.NewDecoder(res.Body).Decode(&resp)
	if !resp.OK {
		t.Errorf("ok = false")
	}
}

func TestServer_UnknownPath_404(t *testing.T) {
	srv, _ := upstreamEnv(t)
	res, err := http.Get(srv.URL + "/_forge/contract/nope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}

// TestRoundTrip_HostToUpstream wires a host dispatcher with the forwarding
// dispatcher pointing at the upstream Server. The host's own registry
// records the upstream manifest as a remote so the forwarder can find it.
// A dispatch through the host should round-trip to the upstream and back.
func TestRoundTrip_HostToUpstream(t *testing.T) {
	upstream, upstreamReg := upstreamEnv(t)
	_ = upstreamReg // referenced for the upstream side; host side below

	// Host: fetch manifest, register remote, install forwarding dispatcher.
	hostReg := contract.NewRegistry()
	hostWreg := contract.NewWardenRegistry()
	m, err := remote.FetchManifest(context.Background(), upstream.URL, "", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := loader.Validate(m, hostWreg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := hostReg.RegisterRemote(m, contract.RemoteEndpoint{BaseURL: upstream.URL}); err != nil {
		t.Fatalf("register remote: %v", err)
	}

	hostDisp := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	hostDisp.SetRemoteDispatcher(remote.NewForwardingDispatcher(hostReg))

	// No local handler for things.list — it should forward to the upstream.
	data, _, err := hostDisp.Dispatch(context.Background(), contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "things", Intent: "things.list", IntentVersion: 1,
	}, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var got struct {
		Items []string `json:"items"`
	}
	_ = json.Unmarshal(data, &got)
	if len(got.Items) != 2 || got.Items[0] != "a" {
		t.Errorf("round trip data = %+v", got)
	}
}
