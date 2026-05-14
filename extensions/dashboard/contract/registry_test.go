// registry_test.go
package contract

import (
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// unmarshalForTest is a private test helper that wraps yaml.Unmarshal.
// First introduced for the registry tests; reused by other test files in this package.
func unmarshalForTest(b []byte, v any) error { return yaml.Unmarshal(b, v) }

func mustManifest(t *testing.T, src string) *ContractManifest {
	t.Helper()
	var m ContractManifest
	if err := unmarshalForTest([]byte(src), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return &m
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	intent, ok := r.Intent("users", "users.list", 1)
	if !ok || intent.Name != "users.list" {
		t.Errorf("lookup failed: ok=%v intent=%+v", ok, intent)
	}
}

func TestRegistry_DuplicateContributor(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
`)
	_ = r.Register(m)
	if err := r.Register(m); err == nil {
		t.Error("expected duplicate-contributor error")
	}
}

func TestRegistry_HighestActiveIntentVersion(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: user.disable, kind: command, version: 1, capability: write,
      deprecated: { intentVersion: 1, removeAfter: "2026-09-01" } }
  - { name: user.disable, kind: command, version: 2, capability: write }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.HighestVersion("users", "user.disable")
	if !ok || got != 2 {
		t.Errorf("HighestVersion = %d, ok=%v", got, ok)
	}
}

func TestRegistry_RejectsBadSlotFill(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - intent: page.shell
    slots:
      main:
        - { intent: action.button }   # not allowed in page.shell.main
`)
	if err := r.Register(m); err == nil {
		t.Error("expected slot-accept rejection")
	}
}
