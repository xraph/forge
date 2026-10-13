package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge"
)

// statusExt is a migratable extension that reports one group, the way a grove
// extension does after contributing its groups to a central registry.
type statusExt struct {
	migratableExt

	groups []*forge.MigrationGroupInfo
}

func (e *statusExt) MigrationStatus(_ context.Context) ([]*forge.MigrationGroupInfo, error) {
	return e.groups, nil
}

// newGroveLikeApp has central migrations on but no CentralMigrator in the
// container, which is what an app using grove's MigrationRegistry looks like.
func newGroveLikeApp() *centralMockApp {
	app := &centralMockApp{mockApp: mockApp{name: "grove-like", logger: forge.NewNoopLogger()}}
	app.extensions = []forge.Extension{&statusExt{
		migratableExt: migratableExt{name: "grove"},
		groups: []*forge.MigrationGroupInfo{{
			Name:    "loomplane",
			Applied: []*forge.MigrationInfo{{Version: "001", Name: "init", AppliedAt: "2026-10-01"}},
			Pending: []*forge.MigrationInfo{{Version: "002", Name: "runs"}},
		}},
	}}

	return app
}

func TestCentralMigrateStatus_FallsBackToExtensions(t *testing.T) {
	var out bytes.Buffer

	if err := newCentralCLI(newGroveLikeApp(), &out).Run([]string{"testapp", "migrate", "status"}); err != nil {
		t.Fatalf("status: %v", err)
	}

	got := out.String()
	for _, want := range []string{"loomplane", "init", "runs"} {
		if !strings.Contains(got, want) {
			t.Errorf("status output is missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "no CentralMigrator") {
		t.Errorf("status still reports nothing to do:\n%s", got)
	}
}

func TestCentralMigrateUp_ReportsFromExtensions(t *testing.T) {
	var out bytes.Buffer

	if err := newCentralCLI(newGroveLikeApp(), &out).Run([]string{"testapp", "migrate", "up"}); err != nil {
		t.Fatalf("up: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "1 migration(s) applied, 1 still pending") {
		t.Errorf("up did not report the extension's migrations:\n%s", got)
	}
}

func TestCentralMigrateDown_FailsWithoutCentralMigrator(t *testing.T) {
	var out bytes.Buffer

	err := newCentralCLI(newGroveLikeApp(), &out).Run([]string{"testapp", "migrate", "down", "--force"})
	if err == nil {
		t.Fatalf("down without a CentralMigrator succeeded:\n%s", out.String())
	}

	if !strings.Contains(err.Error(), "forge.CentralMigrator") {
		t.Errorf("error does not say what is missing: %v", err)
	}
}
