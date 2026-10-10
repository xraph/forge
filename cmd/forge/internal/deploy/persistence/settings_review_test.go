package persistence

import (
	"context"
	"strings"
	"testing"
)

func TestLegacySQLRuntimeCredentialsAreScrubbed(t *testing.T) {
	db := sqliteFixture(t)
	ctx := context.Background()

	raw := []byte("project: {name: atlas}\ndev: {docker: {env: {TOKEN: legacy-runtime-sentinel}}}\ndeploy: {version: 2, registry: ghcr.io/example}\n")
	if _, err := db.sql.ExecContext(ctx, "INSERT INTO forge_deploy_settings(project,revision,content) VALUES($1,1,$2)", db.options.Project, raw); err != nil {
		t.Fatal(err)
	}

	if _, err := db.sql.ExecContext(ctx, "UPDATE forge_deploy_schema SET version=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}

	if err := db.migrate(ctx); err != nil {
		t.Fatal(err)
	}

	revision, got, err := db.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if revision != 2 || strings.Contains(string(got), "legacy-runtime-sentinel") || !strings.Contains(string(got), "ghcr.io/example") {
		t.Fatal("legacy SQL settings were not scrubbed", revision)
	}
}
