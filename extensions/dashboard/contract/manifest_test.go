// manifest_test.go
package contract

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const sampleManifestYAML = `
schemaVersion: 1
contributor:
  name: users
  envelope:
    supports: [v1]
    preferred: v1
  capabilities: [users.read, users.write]

intents:
  - name: users.list
    kind: query
    version: 1
    capability: read
    requires:
      all: ["scope:users.read"]
    audit: false

  - name: user.disable
    kind: command
    version: 2
    capability: write
    requires:
      all: ["role:admin", "scope:users.write"]
      warden: tenantOwner
    invalidates: [users.list, user.detail]

graph:
  - route: /users
    intent: page.shell
    title: Users
    nav:
      group: Identity
      icon: users
      priority: 10
`

func TestManifest_YAML_RoundTrip(t *testing.T) {
	var m ContractManifest
	if err := yaml.Unmarshal([]byte(sampleManifestYAML), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d", m.SchemaVersion)
	}
	if m.Contributor.Name != "users" {
		t.Errorf("contributor name = %q", m.Contributor.Name)
	}
	if got := len(m.Intents); got != 2 {
		t.Fatalf("intents count = %d", got)
	}
	if m.Intents[0].Kind != IntentKindQuery || m.Intents[1].Kind != IntentKindCommand {
		t.Errorf("intent kinds = %v, %v", m.Intents[0].Kind, m.Intents[1].Kind)
	}
	if m.Intents[1].Requires.Warden != "tenantOwner" {
		t.Errorf("warden ref = %q", m.Intents[1].Requires.Warden)
	}
	if got := len(m.Graph); got != 1 {
		t.Fatalf("graph count = %d", got)
	}
	if m.Graph[0].Route != "/users" {
		t.Errorf("route = %q", m.Graph[0].Route)
	}
}
