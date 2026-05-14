package contributor

import (
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// TestManifest_HasContractField verifies that a Manifest carrying a contract
// manifest round-trips through JSON without losing the embedded payload — i.e.
// that the new optional Contract field is wired correctly with the right JSON
// tag and is reachable by callers that consume manifests over the wire.
func TestManifest_HasContractField(t *testing.T) {
	t.Parallel()

	cm := &contract.ContractManifest{
		SchemaVersion: 1,
		Contributor: contract.Contributor{
			Name: "billing",
			Envelope: contract.EnvelopeSupport{
				Supports:  []string{"v1"},
				Preferred: "v1",
			},
		},
		Intents: []contract.Intent{
			{
				Name:       "billing.invoice.list",
				Kind:       contract.IntentKindQuery,
				Version:    1,
				Capability: contract.CapRead,
			},
		},
	}

	m := &Manifest{
		Name:        "billing",
		DisplayName: "Billing",
		Version:     "1.0.0",
		Contract:    cm,
	}

	// Round-trip the manifest through JSON to confirm the Contract field
	// serialises under the expected tag and decodes back to an equivalent value.
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	// The raw payload must contain the "contract" key — guards against a
	// silent typo on the json struct tag.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["contract"]; !ok {
		t.Fatalf("expected json key %q in marshalled manifest, got keys: %v", "contract", keysOf(raw))
	}

	var got Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if got.Contract == nil {
		t.Fatal("decoded manifest has nil Contract; expected populated value")
	}
	if got.Contract.Contributor.Name != cm.Contributor.Name {
		t.Errorf("contributor name = %q, want %q", got.Contract.Contributor.Name, cm.Contributor.Name)
	}
	if len(got.Contract.Intents) != 1 || got.Contract.Intents[0].Name != "billing.invoice.list" {
		t.Errorf("intents not preserved: %#v", got.Contract.Intents)
	}
}

// TestManifest_ContractOmittedWhenNil verifies the omitempty tag — manifests
// that don't opt into the contract path keep their JSON payload free of the
// contract key.
func TestManifest_ContractOmittedWhenNil(t *testing.T) {
	t.Parallel()

	m := &Manifest{Name: "legacy", DisplayName: "Legacy", Version: "1.0.0"}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["contract"]; ok {
		t.Errorf("expected contract key to be omitted when nil; got keys: %v", keysOf(raw))
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
