package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestDatabaseEditInvalidatesApprovedPlan(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)
	ctx := context.Background()

	raw, err := os.ReadFile(filepath.Join(root, ".forge.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if err := persistence.Configure(ctx, root, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, persistence.Hash(raw)); err != nil {
		t.Fatal(err)
	}

	p, _, err := e.Plan(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	files, err := e.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Save(ctx, files.Hash, []spec.Op{{Path: "deploy.services.api.replicas", Value: 2}}); err != nil {
		t.Fatal(err)
	}

	res, err := e.Inspect(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if res.Doc.Deploy.Services["api"].Replicas != 2 {
		t.Fatal("CLI ignored SQL edit")
	}

	var oe *output.Error
	if err := e.Apply(ctx, p, p.Hash, false, nil); !errors.As(err, &oe) || oe.Code != output.ExitConflict {
		t.Fatal("stale SQL plan accepted", err)
	}

	file, err := os.ReadFile(filepath.Join(root, ".forge.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if string(file) != string(raw) {
		t.Fatal("SQL edit rewrote file-owned config")
	}

	if err := e.Save(ctx, files.Hash, []spec.Op{{Path: "deploy.services.api.replicas", Value: 3}}); !errors.Is(err, persistence.ErrConflict) {
		t.Fatal("stale editor accepted", err)
	}
}
func TestSettingsCASAndAnchoredPaths(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)
	ctx := context.Background()

	view, err := e.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, ".forge.yml")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, append(raw, []byte("# concurrent\n")...), 0600); err != nil {
		t.Fatal(err)
	}

	if err := e.Save(ctx, view.Hash, []spec.Op{{Path: "deploy.services.api.replicas", Value: 2}}); !errors.Is(err, persistence.ErrConflict) {
		t.Fatal("concurrent file edit overwritten", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, raw, 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}

	if _, err := e.Files(ctx); err == nil {
		t.Fatal("outside-root symlink accepted")
	}

	actual, err := os.ReadFile(outside)
	if err != nil || string(actual) != string(raw) {
		t.Fatal("outside config altered", err)
	}
}
func TestDatabaseRuntimeConfigRemainsFileOwned(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)
	ctx := context.Background()
	path := filepath.Join(root, ".forge.yml")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := persistence.Configure(ctx, root, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, persistence.Hash(raw)); err != nil {
		t.Fatal(err)
	}

	changed := strings.Replace(string(raw), "name: atlas", "name: atlas-current", 1)
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}

	view, err := e.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Save(ctx, view.Hash, []spec.Op{{Path: "project.name", Value: "stale"}}); err == nil {
		t.Fatal("runtime config editable through SQL")
	}

	res, err := e.load(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if res.Config.Project.Name != "atlas-current" {
		t.Fatal("file runtime config ignored")
	}

	if err := e.ConfigureStore(ctx, persistence.Options{Backend: "files"}, view.Hash); err != nil {
		t.Fatal(err)
	}

	file, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(file), "atlas-current") {
		t.Fatal("store export overwrote runtime config", err)
	}
}
func TestDatabasePlanIsAuthoritativeWithoutLocalPlanFiles(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)
	ctx := context.Background()

	raw, err := os.ReadFile(filepath.Join(root, ".forge.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if err := persistence.Configure(ctx, root, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, persistence.Hash(raw)); err != nil {
		t.Fatal(err)
	}

	p, _, err := e.Plan(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	db, err := persistence.OpenSelected(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	data, err := db.Read(ctx, "plans", filepath.Base(e.PlanPath(p)))
	if err != nil || !strings.Contains(string(data), p.Hash) {
		t.Fatal("approved plan missing from authoritative store", err)
	}

	if err := os.RemoveAll(filepath.Join(root, ".forge/plans")); err != nil {
		t.Fatal(err)
	}

	loaded, err := e.LoadPlan(ctx, p.Hash)
	if err != nil || loaded.Hash != p.Hash {
		t.Fatal("plan relies on local file snapshot", err)
	}
}
func TestRegistryConnectionMetadataContainsNoCredential(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, f := composeEngine(t, root)
	r := &registryReviewRunner{Fake: f}

	e.runner = r
	if err := e.ConnectRegistry(context.Background(), "ghcr", "ghcr.io", "rex", "private-ui-token"); err != nil {
		t.Fatal(err)
	}

	connections, err := e.Connections(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(connections) != 1 || connections[0].Host != "ghcr.io" {
		t.Fatal("connection not available", connections)
	}

	data, err := json.Marshal(connections)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(data), "private-ui-token") || strings.Contains(string(data), "auth") {
		t.Fatal("credential exposed", string(data))
	}
}
func TestSQLInitPersistsInSelectedAuthority(t *testing.T) {
	root := testdata.Copy(t, "atlas")
	e := newEngine(t, root)
	ctx := context.Background()

	raw, err := os.ReadFile(filepath.Join(root, ".forge.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if err := persistence.Configure(ctx, root, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, persistence.Hash(raw)); err != nil {
		t.Fatal(err)
	}

	if _, _, err := e.Init(ctx, map[string]string{"deploy.services.worker.health": "none", "deploy.resources.cache.features": "none"}, true); err != nil {
		t.Fatal(err)
	}

	res, err := e.load(ctx)
	if err != nil || res.Doc.Deploy == nil {
		t.Fatal("init ignored SQL authority", err)
	}

	file, err := os.ReadFile(filepath.Join(root, ".forge.yml"))
	if err != nil || string(file) != string(raw) {
		t.Fatal("SQL init changed file copy", err)
	}
}
func TestPlanCannotWriteOutsideProjectThroughSymlink(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")

	e, _ := composeEngine(t, root)
	if err := os.Mkdir(filepath.Join(root, ".forge"), 0700); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".forge/plans")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := e.Plan(context.Background(), "local", "dev"); err == nil {
		t.Fatal("outside-root plan directory accepted")
	}

	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("plan escaped project", err)
	}
}
