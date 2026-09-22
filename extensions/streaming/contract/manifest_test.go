package contract

import (
	"bytes"
	"testing"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func TestEmbeddedManifest_LoadsAndValidates(t *testing.T) {
	m, err := loader.Load(bytes.NewReader(manifestYAML), "streaming/contract/manifest.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Contributor.Name != "streaming-contract" {
		t.Errorf("contributor name = %q", m.Contributor.Name)
	}
	if got := len(m.Intents); got < 14 {
		t.Errorf("expected ≥14 intents (9 reads + 5 mutations), got %d", got)
	}
	if err := loader.Validate(m, dashcontract.NewWardenRegistry()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// Register is covered end to end by
// TestDashboardDiscovery_PublishesStreamingContract in the parent package,
// which boots a real Forge app with the real dashboard extension and asserts
// streaming-contract reaches the capabilities endpoint. It lives there rather
// than here because only a package outside contract/ can import the dashboard
// extension without a cycle.
//
// The distinction matters: the test above stops at loader.Validate, and
// contract.Registry.Register enforces rules the loader does not. A manifest
// that loads and validates here can still be rejected at registration, and the
// dashboard logs that rejection and carries on, so the only visible symptom
// is streaming-contract missing from /capabilities.
