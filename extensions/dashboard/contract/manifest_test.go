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
}

// dataBindingHolder wraps a DataBinding so the test can exercise
// DataBinding's custom UnmarshalYAML (both the queryRef-shorthand and the
// inline {intent, params} shape) directly, without routing through the
// deleted UI-graph node type the way this used to.
type dataBindingHolder struct {
	Data *DataBinding `yaml:"data"`
}

func TestDataBinding_BothShapes(t *testing.T) {
	var shorthand dataBindingHolder
	if err := yaml.Unmarshal([]byte(`data: queries.userList`), &shorthand); err != nil {
		t.Fatalf("unmarshal shorthand: %v", err)
	}
	if shorthand.Data == nil || shorthand.Data.QueryRef != "queries.userList" {
		t.Errorf("shorthand not parsed: %+v", shorthand.Data)
	}

	var inline dataBindingHolder
	if err := yaml.Unmarshal([]byte(`
data:
  intent: count.events
  params: { since: { value: "1h" } }
`), &inline); err != nil {
		t.Fatalf("unmarshal inline: %v", err)
	}
	if inline.Data == nil || inline.Data.Intent != "count.events" {
		t.Errorf("inline form not parsed: %+v", inline.Data)
	}
}

const paramShorthandYAML = `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents: []
queries:
  q1:
    intent: foo
    params:
      shorthand: route.tenant
      explicit:  { from: parent.id }
      literal:   { value: 5 }
`

func TestParamSource_Shorthand(t *testing.T) {
	var m ContractManifest
	if err := yaml.Unmarshal([]byte(paramShorthandYAML), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	q := m.Queries["q1"]
	if q.Params["shorthand"].From != "route.tenant" {
		t.Errorf("shorthand not parsed: %+v", q.Params["shorthand"])
	}
	if q.Params["explicit"].From != "parent.id" {
		t.Errorf("explicit form lost data: %+v", q.Params["explicit"])
	}
	if v, ok := q.Params["literal"].Value.(int); !ok || v != 5 {
		t.Errorf("literal value wrong: %+v", q.Params["literal"].Value)
	}
}
