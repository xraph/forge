package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func composeEngine(t *testing.T, root string) (*Engine, *execx.Fake) {
	t.Helper()

	cfg, _ := config.LoadForgeConfigFrom(root)
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Available["docker"] = true
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps", execx.Result{Stdout: "net/http\n"})
	f.Script("docker info", execx.Result{})
	f.Script("docker compose", execx.Result{})
	e, _ := New(Options{Config: cfg, Runner: f, Mode: output.Mode{NonInteractive: true}})

	return e, f
}

func TestPlanWritesFileAndExportWritesBundle(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)

	p, b, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if p.Hash == "" || len(p.Operations) == 0 {
		t.Fatalf("%+v", p)
	}

	matches, _ := filepath.Glob(filepath.Join(root, ".forge", "plans", "dev-local-*.json"))
	if len(matches) != 1 {
		t.Fatalf("plan file: %v", matches)
	}

	res, err := e.Export(context.Background(), p, b, "", false)
	if err != nil || len(res.Written) == 0 {
		t.Fatalf("%+v %v", res, err)
	}

	if _, err := os.Stat(filepath.Join(root, "deployments", "local", "dev", "compose.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRequiresMatchingHash(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)
	p, _, _ := e.Plan(context.Background(), "local", "dev")
	err := e.Apply(context.Background(), p, "wrong", false, nil)

	var oe *output.Error
	if !errors.As(err, &oe) || oe.Code != output.ExitConflict {
		t.Fatalf("%v", err)
	}

	err = e.Apply(context.Background(), p, "", false, nil)
	if !errors.As(err, &oe) || oe.Code != output.ExitInvalidInput {
		t.Fatalf("non-interactive apply without approval must exit 2: %v", err)
	}
}

func TestApplyStaleInputExits6(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)
	p, _, _ := e.Plan(context.Background(), "local", "dev")
	path := filepath.Join(root, ".forge.yml")
	data, _ := os.ReadFile(path)
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
	err := e.Apply(context.Background(), p, p.Hash, false, nil)

	var oe *output.Error
	if !errors.As(err, &oe) || oe.Code != output.ExitConflict || !strings.Contains(oe.Diagnostics[0].File, ".forge.yml") {
		t.Fatalf("%v", err)
	}
}

func TestDoctorOnlineChecksDockerDaemon(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, f := composeEngine(t, root)
	f.Script("docker info", execx.Result{ExitCode: 1, Stderr: "Cannot connect to the Docker daemon"})

	diags, _ := e.Doctor(context.Background(), "local", "dev", true)
	found := false

	for _, d := range diags {
		if d.Code == "DEPLOY_CONTEXT_UNREACHABLE" {
			found = true
		}
	}

	if !found {
		t.Fatalf("%v", diags)
	}
}

func TestPlanningReloadsProjectConfiguration(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)
	path := filepath.Join(root, ".forge.yml")
	raw, _ := os.ReadFile(path)

	raw = []byte(strings.Replace(string(raw), "name: atlas", "name: atlas-renamed", 1))
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if p.Project != "atlas-renamed" {
		t.Fatalf("stale project: %s", p.Project)
	}
}
