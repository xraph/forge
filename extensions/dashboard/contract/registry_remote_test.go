package contract_test

import (
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func tinyManifest(t *testing.T, name string) *contract.ContractManifest {
	t.Helper()
	yaml := `
schemaVersion: 1
contributor: { name: ` + name + `, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: ` + name + `.list, kind: query, version: 1, capability: read }
`
	m, err := loader.Load(strings.NewReader(yaml), "test.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m
}

func TestRegisterRemote_RecordsEndpoint(t *testing.T) {
	r := contract.NewRegistry()
	m := tinyManifest(t, "things")
	ep := contract.RemoteEndpoint{BaseURL: "http://svc:8080", APIKey: "secret"}
	if err := r.RegisterRemote(m, ep); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !r.IsRemote("things") {
		t.Errorf("IsRemote(things) = false")
	}
	got, ok := r.Remote("things")
	if !ok || got.BaseURL != "http://svc:8080" || got.APIKey != "secret" {
		t.Errorf("endpoint round-trip wrong: %+v ok=%v", got, ok)
	}
	// Verify the manifest is registered like a local one — intents stay
	// queryable so the dispatcher's lookup logic isn't bypassed.
	if _, ok := r.Intent("things", "things.list", 1); !ok {
		t.Errorf("intent lookup failed for remote")
	}
}

func TestRegisterRemote_LocalsAreNotRemote(t *testing.T) {
	r := contract.NewRegistry()
	m := tinyManifest(t, "things")
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	if r.IsRemote("things") {
		t.Errorf("local contributor reported as remote")
	}
	if _, ok := r.Remote("things"); ok {
		t.Errorf("Remote(local) returned ok=true")
	}
}

func TestRegisterRemote_RequiresBaseURL(t *testing.T) {
	r := contract.NewRegistry()
	m := tinyManifest(t, "things")
	if err := r.RegisterRemote(m, contract.RemoteEndpoint{}); err == nil {
		t.Errorf("expected error when BaseURL empty")
	}
}

func TestUnregister_ClearsAllState(t *testing.T) {
	r := contract.NewRegistry()
	m := tinyManifest(t, "things")
	if err := r.RegisterRemote(m, contract.RemoteEndpoint{BaseURL: "http://x"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	r.Unregister("things")
	if r.IsRemote("things") {
		t.Errorf("IsRemote still true after Unregister")
	}
	if _, ok := r.Contributor("things"); ok {
		t.Errorf("Contributor still returns ok after Unregister")
	}
	if _, ok := r.Intent("things", "things.list", 1); ok {
		t.Errorf("Intent still resolvable after Unregister")
	}
	if _, ok := r.HighestVersion("things", "things.list"); ok {
		t.Errorf("HighestVersion still present after Unregister")
	}
}

func TestUnregister_UnknownNameIsNoop(t *testing.T) {
	r := contract.NewRegistry()
	r.Unregister("nobody") // must not panic
}
