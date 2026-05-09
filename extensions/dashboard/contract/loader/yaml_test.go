// yaml_test.go
package loader

import (
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

const okYAML = `
schemaVersion: 1
contributor:
  name: users
  envelope: { supports: [v1], preferred: v1 }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
graph:
  - { route: /users, intent: page.shell }
`

func TestLoad_OK(t *testing.T) {
	m, err := Load(strings.NewReader(okYAML), "users.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Contributor.Name != "users" {
		t.Errorf("name = %q", m.Contributor.Name)
	}
	_ = contract.IntentKindQuery // ensure import retained
}

func TestLoad_BadSchemaVersion(t *testing.T) {
	const yaml = `schemaVersion: 99
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents: []
`
	if _, err := Load(strings.NewReader(yaml), "x.yaml"); err == nil {
		t.Error("expected error for unsupported schemaVersion")
	}
}
