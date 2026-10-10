package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestSQLSettingsExcludeRuntimeCredentials(t *testing.T) {
	for _, operation := range []string{"configure", "save", "init"} {
		t.Run(operation, func(t *testing.T) {
			fixture := "atlas-v2"
			if operation == "init" {
				fixture = "atlas"
			}

			root := testdata.Copy(t, fixture)
			path := filepath.Join(root, ".forge.yml")

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			raw = append(raw, []byte("\ndev:\n  docker:\n    env:\n      PRIVATE_TOKEN: runtime-credential-sentinel\ndatabase:\n  connections:\n    primary:\n      url: postgres://local:runtime-password@localhost/test\n")...)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()

			e := newEngine(t, root)
			if err := persistence.Configure(ctx, root, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, persistence.Hash(raw)); err != nil {
				t.Fatal(err)
			}

			if operation == "save" {
				view, err := e.Files(ctx)
				if err != nil {
					t.Fatal(err)
				}

				if err := e.Save(ctx, view.Hash, []spec.Op{{Path: "deploy.services.api.replicas", Value: 2}}); err != nil {
					t.Fatal(err)
				}
			}

			if operation == "init" {
				if _, _, err := e.Init(ctx, map[string]string{"deploy.services.worker.health": "none", "deploy.resources.cache.features": "none"}, true); err != nil {
					t.Fatal(err)
				}
			}

			db, err := persistence.OpenSelected(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			_, stored, err := db.Settings(ctx)
			if err != nil {
				t.Fatal(err)
			}

			for _, sentinel := range []string{"runtime-credential-sentinel", "runtime-password", "\nproject:", "\nbuild:", "\ndev:", "\ndatabase:"} {
				if strings.Contains("\n"+string(stored), sentinel) {
					t.Errorf("SQL settings contain file-owned %q", sentinel)
				}
			}

			file, err := os.ReadFile(path)
			if err != nil || string(file) != string(raw) {
				t.Fatal("runtime checkout modified", err)
			}
		})
	}
}

func TestSettingsRejectSameContentDirectorySymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"config/current", "config/other"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}

	raw := []byte("services:\n  api: {app: api, kind: web, ports: {http: {port: 8080}}, health: {none: true}}\n")
	for _, dir := range []string{"config/current", "config/other"} {
		if err := os.WriteFile(filepath.Join(root, dir, "stack.yml"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}

	cfg := "project: {name: atlas, module: example.com/atlas}\nbuild:\n  apps: [{name: api, cmd: ./cmd/api}]\ndeploy:\n  version: 2\n  spec: config/current/stack.yml\n  defaults: {target: local, environment: dev}\n  targets: {local: {provider: compose}}\n  environments: {dev: {target: local}}\n"
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}

	e := newEngine(t, root)
	ctx := context.Background()

	view, err := e.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(filepath.Join(root, "config/current"), filepath.Join(root, "config/reviewed")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("other", filepath.Join(root, "config/current")); err != nil {
		t.Fatal(err)
	}

	if err := e.Save(ctx, view.Hash, []spec.Op{{Path: "deploy.services.api.replicas", Value: 2}}); !errors.Is(err, persistence.ErrConflict) {
		t.Fatal("retarget did not conflict", err)
	}

	for _, dir := range []string{"config/reviewed", "config/other"} {
		got, err := os.ReadFile(filepath.Join(root, dir, "stack.yml"))
		if err != nil || string(got) != string(raw) {
			t.Fatal("retarget modified config", dir, err)
		}
	}
}
