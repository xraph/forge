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

func newEngine(t *testing.T, root string) *Engine {
	t.Helper()

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Available["docker"] = true
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps", execx.Result{Stdout: "net/http\n"})

	e, err := New(Options{Config: cfg, Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	return e
}

func TestInspectWithoutDeployBlockReportsSuggestions(t *testing.T) {
	e := newEngine(t, testdata.Root("atlas"))

	res, err := e.Inspect(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}

	if res.Deployment != nil || len(res.Discovery.Suggestions) == 0 {
		t.Fatalf("expected suggestions and no model: %+v", res)
	}

	if res.Diagnostics[0].Code != "DEPLOY_VERSION_MISSING" {
		t.Fatalf("%v", res.Diagnostics)
	}
}

func TestInspectResolvesV2(t *testing.T) {
	e := newEngine(t, testdata.Root("atlas-v2"))

	res, err := e.Inspect(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if res.Deployment == nil || len(res.Deployment.Services) != 3 {
		t.Fatalf("%+v", res.Diagnostics)
	}
}

func TestInitWritesBlockAndRefusesTwice(t *testing.T) {
	root := testdata.Copy(t, "atlas")
	e := newEngine(t, root)
	answers := map[string]string{"deploy.services.worker.health": "heartbeat", "deploy.resources.cache.features": "json,search"}

	_, files, err := e.Init(context.Background(), answers, true)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := files[filepath.Join(root, ".forge.yml")]; !ok {
		t.Fatalf("expected .forge.yml written, got %v", files)
	}

	data, _ := os.ReadFile(filepath.Join(root, ".forge.yml"))
	if !strings.Contains(string(data), "version: 2") || !strings.Contains(string(data), "heartbeat: true") || !strings.Contains(string(data), "features:") {
		t.Fatalf("block missing pieces:\n%s", data)
	}

	_, _, err = e.Init(context.Background(), answers, true)

	var oe *output.Error
	if !errors.As(err, &oe) || oe.Code != output.ExitInvalidInput {
		t.Fatalf("second init must fail with exit 2, got %v", err)
	}
}

func TestInitNonInteractiveWithOpenDecisionExits3(t *testing.T) {
	root := testdata.Copy(t, "atlas")
	e := newEngine(t, root)
	e.mode.NonInteractive = true
	_, files, err := e.Init(context.Background(), nil, true)

	var oe *output.Error
	if !errors.As(err, &oe) || oe.Code != output.ExitUnresolved || len(files) != 0 {
		t.Fatalf("expected exit 3 and no files, got %v %v", err, files)
	}

	if len(oe.Diagnostics) != 2 || !strings.Contains(oe.Diagnostics[0].Fix, "--answer deploy.services.worker.health=") {
		t.Fatalf("diagnostics: %+v", oe.Diagnostics)
	}

	if _, err := os.Stat(filepath.Join(root, ".forge.yml.bak")); err == nil {
		t.Fatal("nothing should have been written")
	}
}

func TestDoctorOfflineReportsMissingTool(t *testing.T) {
	root := testdata.Root("atlas-v2")
	cfg, _ := config.LoadForgeConfigFrom(root)
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps", execx.Result{Stdout: "net/http\n"})
	e, _ := New(Options{Config: cfg, Runner: f})

	diags, err := e.Doctor(context.Background(), "local", "dev", false)
	if err != nil {
		t.Fatal(err)
	}

	found := false

	for _, d := range diags {
		if d.Code == "DEPLOY_TOOL_MISSING" && strings.Contains(d.Message, "docker") {
			found = true
		}
	}

	if !found {
		t.Fatalf("expected docker missing, got %v", diags)
	}
}
