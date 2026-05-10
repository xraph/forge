package pilot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

func newRegistryWith(t *testing.T, manifests ...*contributor.Manifest) *contributor.ContributorRegistry {
	t.Helper()
	r := contributor.NewContributorRegistry("/dashboard")
	for _, m := range manifests {
		stub := &stubLocal{manifest: m}
		if err := r.RegisterRemote(stub); err != nil {
			t.Fatalf("register %q: %v", m.Name, err)
		}
	}
	return r
}

type stubLocal struct{ manifest *contributor.Manifest }

func (s *stubLocal) Manifest() *contributor.Manifest { return s.manifest }

func TestExtensionsListHandler_ReturnsRegisteredContributors(t *testing.T) {
	r := newRegistryWith(t,
		&contributor.Manifest{Name: "auth", DisplayName: "Authentication", Version: "1.0", Layout: "extension", Nav: []contributor.NavItem{{}, {}}, Widgets: nil},
		&contributor.Manifest{Name: "cron", DisplayName: "", Version: "0.9", Widgets: []contributor.WidgetDescriptor{{}}},
	)

	h := extensionsListHandler(r)
	res, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(res.Extensions) != 2 {
		t.Fatalf("got %d, want 2", len(res.Extensions))
	}
	// Check that empty DisplayName is filled with Name (matches today's API behavior).
	for _, e := range res.Extensions {
		if e.Name == "cron" && e.DisplayName != "cron" {
			t.Errorf("cron display name fallback = %q", e.DisplayName)
		}
	}

	// Verify the result encodes cleanly to JSON.
	if _, err := json.Marshal(res); err != nil {
		t.Errorf("marshal: %v", err)
	}
}
