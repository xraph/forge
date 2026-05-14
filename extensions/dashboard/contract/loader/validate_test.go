// validate_test.go
package loader

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func mustLoad(t *testing.T, src string) *contract.ContractManifest {
	t.Helper()
	m, err := Load(strings.NewReader(src), "test.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m
}

func TestValidate_GoodManifest(t *testing.T) {
	m := mustLoad(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list,  kind: query,   version: 1, capability: read }
  - { name: user.disable, kind: command, version: 1, capability: write,
      requires: { warden: tenantOwner } }
queries:
  userList: { intent: users.list }
graph:
  - { route: /users, intent: page.shell, data: queries.userList }
`)
	wreg := contract.NewWardenRegistry()
	_ = wreg.Register("tenantOwner", &noopWarden{})
	if err := Validate(m, wreg); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_UnknownWarden(t *testing.T) {
	m := mustLoad(t, `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: a, kind: query, version: 1, capability: read,
      requires: { warden: missing } }
`)
	if err := Validate(m, contract.NewWardenRegistry()); err == nil {
		t.Error("expected unknown-warden error")
	}
}

func TestValidate_UnknownQueryRef(t *testing.T) {
	m := mustLoad(t, `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - { intent: page.shell, data: queries.nope }
`)
	if err := Validate(m, contract.NewWardenRegistry()); err == nil {
		t.Error("expected unknown-query error")
	}
}

func TestValidate_KindCapabilityMismatch(t *testing.T) {
	cases := []string{
		"kind: command, capability: read", // command must be write
		"kind: query, capability: write",  // query must be read
		"kind: subscription, capability: write",
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			m := mustLoad(t, `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: a, version: 1, `+body+` }
`)
			if err := Validate(m, contract.NewWardenRegistry()); err == nil {
				t.Errorf("expected kind/capability mismatch error for %q", body)
			}
		})
	}
}

type noopWarden struct{}

func (noopWarden) Authorize(_ context.Context, _ contract.Principal, _ contract.Action) (contract.Decision, error) {
	return contract.Decision{Allow: true}, nil
}
