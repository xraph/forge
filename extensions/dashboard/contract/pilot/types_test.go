package pilot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func TestExtensionsList_RoundTrip(t *testing.T) {
	in := ExtensionsList{Extensions: []ExtensionInfo{
		{Name: "auth", DisplayName: "Authentication", Version: "1.0", Layout: "extension", PageCount: 2, WidgetCount: 0},
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ExtensionsList
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Extensions[0].DisplayName != "Authentication" {
		t.Errorf("display name lost: %+v", got)
	}
}

func TestServiceDetail_NilSafe(t *testing.T) {
	// A nil ServicesList should round-trip as `{"services":null}` not panic.
	var sl ServicesList
	b, err := json.Marshal(sl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ServicesList
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Services) != 0 {
		t.Errorf("expected zero services, got %d", len(got.Services))
	}
}

func TestPilotManifest_Loads(t *testing.T) {
	m, err := loader.Load(strings.NewReader(string(manifestYAML)), "pilot/manifest.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Contributor.Name != "core-contract" {
		t.Errorf("contributor name = %q", m.Contributor.Name)
	}
	if got := len(m.Intents); got != 9 {
		t.Errorf("intents = %d, want 9", got)
	}
	if got := len(m.Graph); got != 8 {
		t.Errorf("graph routes = %d, want 8", got)
	}
}

func TestPilotManifest_Validates(t *testing.T) {
	m, err := loader.Load(strings.NewReader(string(manifestYAML)), "pilot/manifest.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := loader.Validate(m, contract.NewWardenRegistry()); err != nil {
		t.Errorf("validate: %v", err)
	}
}
