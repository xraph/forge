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

// TestValidate_QueryIntentBinding pins the query -> intent check, which is the
// only thing that reads ContractManifest.Queries. The two failing cases and the
// passing one straddle looksCrossContributor: a bare name and a name whose first
// dotted segment is this contributor are both "mine", so an undeclared intent is
// an authoring mistake; a name owned by someone else is allowed through because
// this contributor's manifest cannot see the other's intents.
func TestValidate_QueryIntentBinding(t *testing.T) {
	const head = `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
queries:
`

	cases := []struct {
		name    string
		queries string
		wantErr bool
		errHas  string
	}{
		{
			name:    "same contributor, intent not declared",
			queries: "  userList: { intent: users.listAll }\n",
			wantErr: true,
			errHas:  "users.listAll",
		},
		{
			name:    "undotted name, intent not declared",
			queries: "  userList: { intent: listAll }\n",
			wantErr: true,
			errHas:  "listAll",
		},
		{
			name:    "other contributor's intent is allowed through",
			queries: "  billingPlan: { intent: billing.plan }\n",
			wantErr: false,
		},
		{
			name:    "declared intent resolves",
			queries: "  userList: { intent: users.list }\n",
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mustLoad(t, head+tc.queries)
			err := Validate(m, contract.NewWardenRegistry())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected validation to fail for queries %q", tc.queries)
				}
				if !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("error %q does not name the bad intent %q", err, tc.errHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
		})
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
