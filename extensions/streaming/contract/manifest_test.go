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
	if got := len(m.Graph); got != 6 {
		t.Errorf("expected 6 routes, got %d", got)
	}
	if err := loader.Validate(m, dashcontract.NewWardenRegistry()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestRegister_BindsAllIntentsToTheDispatcher(t *testing.T) {
	t.Skip("dispatcher.Dispatcher exercised via unit tests; full Register e2e is covered by the dashboard discovery loop integration test")
}
