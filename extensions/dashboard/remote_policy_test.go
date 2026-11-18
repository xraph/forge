package dashboard

import (
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func remoteManifest(t *testing.T, name string) *contract.ContractManifest {
	t.Helper()

	var m contract.ContractManifest

	src := `
schemaVersion: 1
contributor: { name: ` + name + `, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: ` + name + `.list, kind: query, version: 1, capability: read }
`
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}

	return &m
}

// Regression: a remote on another host over plain http was accepted, and the
// manifest fetch sent the API key in clear text before anything was checked.
func TestRemotePolicy_PlainHTTPToRemoteHostRefused(t *testing.T) {
	_, ext := startGatedDashboard(t)

	// 198.51.100.0/24 is reserved for documentation; a fetch would hang
	// until the client timeout, so a quick error proves no fetch happened.
	err := ext.RegisterRemoteContractContributor(t.Context(), "http://198.51.100.1:65535", "secret")
	if err == nil || !strings.Contains(err.Error(), "refusing plain http") {
		t.Fatalf("got %v, want a plain http refusal", err)
	}

	// Discovery hands over an already-fetched manifest; the same rule holds.
	err = ext.registerRemoteManifest(remoteManifest(t, "far"), contract.RemoteEndpoint{BaseURL: "http://svc.internal:8080"})
	if err == nil || !strings.Contains(err.Error(), "refusing plain http") {
		t.Fatalf("discovered plain http remote: got %v, want a refusal", err)
	}
}

func TestRemotePolicy_ConfigAppliesToRegisteredEndpoints(t *testing.T) {
	_, ext := startGatedDashboard(t,
		WithAllowInsecureRemotes(true),
		WithRemoteForwardHeaders("Cookie"),
		WithRemotePropagateSetCookie(true),
	)

	if err := ext.registerRemoteManifest(remoteManifest(t, "far"), contract.RemoteEndpoint{BaseURL: "http://svc.internal:8080"}); err != nil {
		t.Fatalf("register with AllowInsecureRemotes: %v", err)
	}

	ep, ok := ext.contractRegistry.Remote("far")
	if !ok {
		t.Fatal("remote not registered")
	}

	if !ep.AllowInsecureHTTP || !ep.PropagateSetCookie || len(ep.ForwardHeaders) != 1 || ep.ForwardHeaders[0] != "Cookie" {
		t.Errorf("config policy not applied: %+v", ep)
	}
}
