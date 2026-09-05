package dashboard

import (
	"sync"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// statusExt is a Forge extension that reports a dashboard status, with a
// setter so a test can flip Configured after registration the way a real
// extension does when it is configured at runtime.
type statusExt struct {
	*forge.BaseExtension

	mu     sync.Mutex
	status DashboardStatus
}

func (s *statusExt) DashboardStatus() DashboardStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status
}

func (s *statusExt) setStatus(st DashboardStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status = st
}

func registerTestManifest(t *testing.T, reg contract.Registry, name string) {
	t.Helper()

	src := `
schemaVersion: 1
contributor: { name: ` + name + `, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: ` + name + `.list, kind: query, version: 1, capability: read }
`

	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}

	if err := reg.Register(&m); err != nil {
		t.Fatal(err)
	}
}

// TestContributorStatus_AttributedToRegisteringExtension covers the half of
// the status path that lives outside the transport handler: an extension's
// DashboardStatusAware implementation must be attached to the contributors
// that extension registered, and to no others. Contributor names are not
// known at the registration call site, so they are recovered by diffing the
// registry — get the diff wrong and one extension's status leaks onto every
// contributor in the process.
func TestContributorStatus_AttributedToRegisteringExtension(t *testing.T) {
	e := newTestDashboardExt(t)
	e.contractRegistry = contract.NewRegistry()

	// A contributor nobody claims — the dashboard's own, or a remote.
	registerTestManifest(t, e.contractRegistry, "core-contract")

	before := e.contractContributorNames()
	registerTestManifest(t, e.contractRegistry, "billing")

	ext := &statusExt{
		BaseExtension: forge.NewBaseExtension("billing", "test", "test"),
		status:        DashboardStatus{Version: "2.1.0", Configured: false, Message: "set SMTP host"},
	}
	e.recordContributorStatus(ext, before)

	got, ok := e.contributorStatusFor("billing")
	if !ok {
		t.Fatalf("billing has no status; the extension that registered it reports one")
	}

	if got.Version != "2.1.0" || got.Configured || got.Message != "set SMTP host" {
		t.Errorf("billing status = %+v", got)
	}

	if _, ok := e.contributorStatusFor("core-contract"); ok {
		t.Errorf("core-contract has a status, but no extension registered it — " +
			"the registering extension's status must not be attributed to " +
			"contributors that were already in the registry")
	}

	// Configured flips at runtime once the extension is set up. The
	// capabilities endpoint must read the live answer, not a boot snapshot.
	ext.setStatus(DashboardStatus{Version: "2.1.0", Configured: true})

	got, _ = e.contributorStatusFor("billing")
	if !got.Configured {
		t.Errorf("billing still reports configured=false after the extension " +
			"flipped it; the status is being snapshotted instead of read live")
	}
}
