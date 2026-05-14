package pilot

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func loadPilotIntoRegistry(t *testing.T) contract.Registry {
	t.Helper()
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()
	m, err := loader.Load(strings.NewReader(string(manifestYAML)), "pilot/manifest.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := reg.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg
}

func TestNavigationHandler_GroupsAndSorts(t *testing.T) {
	reg := loadPilotIntoRegistry(t)
	got, err := navigationHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("navigation: %v", err)
	}
	if len(got.Groups) == 0 {
		t.Fatal("expected at least one group")
	}
	// Overview should come before Operations (priority 0 vs 6).
	overviewIdx, opsIdx := -1, -1
	for i, g := range got.Groups {
		if g.Group == "Overview" {
			overviewIdx = i
		}
		if g.Group == "Operations" {
			opsIdx = i
		}
	}
	if overviewIdx < 0 || opsIdx < 0 {
		t.Fatalf("expected Overview + Operations groups, got %+v", got.Groups)
	}
	if overviewIdx >= opsIdx {
		t.Errorf("Overview (%d) should come before Operations (%d)", overviewIdx, opsIdx)
	}
	// Within Overview, items should be priority-sorted.
	for _, g := range got.Groups {
		if g.Group != "Overview" {
			continue
		}
		for i := 1; i < len(g.Items); i++ {
			if g.Items[i].Priority < g.Items[i-1].Priority {
				t.Errorf("Overview items not priority-sorted: %+v", g.Items)
			}
		}
	}
}

func TestNavigationHandler_SkipsRoutesWithoutNav(t *testing.T) {
	reg := loadPilotIntoRegistry(t)
	got, _ := navigationHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
	// /traces/:id has no Nav (slice j detail route) — make sure it doesn't appear.
	for _, g := range got.Groups {
		for _, item := range g.Items {
			if item.Href == "/traces/:id" {
				t.Errorf("nav included :id detail route: %+v", item)
			}
		}
	}
}

func TestNavigationHandler_NilRegistryUnavailable(t *testing.T) {
	_, err := navigationHandler(nil)(context.Background(), struct{}{}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}
