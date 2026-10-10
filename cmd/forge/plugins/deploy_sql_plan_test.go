package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/persistence"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestExportAcceptsAuthoritativeSQLPlanHash(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	fake := execx.NewFake(t)
	fake.Available["go"] = true
	fake.Script("go list -m -json all", execx.Result{})
	fake.Script("go list -deps", execx.Result{Stdout: "net/http\n"})

	e, err := engine.New(engine.Options{Config: cfg, Runner: fake})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	view, err := e.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.ConfigureStore(ctx, persistence.Options{Backend: "sqlite", Reference: ".forge/deploy.db"}, view.Hash); err != nil {
		t.Fatal(err)
	}

	p, _, err := e.Plan(ctx, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(e.PlanPath(p)); !os.IsNotExist(err) {
		t.Fatal("test must use SQL-only plan storage", err)
	}

	out, code := runDeploy(t, root, "export", "--plan", p.Hash, "--output-dir", filepath.Join(root, "review"), "--output", "json", "--non-interactive")
	if code != 0 {
		t.Fatalf("SQL plan handoff failed: exit %d: %s", code, out)
	}

	if _, err := os.Stat(filepath.Join(root, "review", "compose.yaml")); err != nil {
		t.Fatal("export missing", err)
	}

	out, code = runDeploy(t, root, "plan", "--target", "local", "--env", "dev", "--output", "json", "--non-interactive")
	if code != 0 {
		t.Fatalf("SQL plan failed: exit %d: %s", code, out)
	}

	var envelope struct {
		Data struct {
			File     string `json:"file"`
			Selector string `json:"selector"`
			Hash     string `json:"hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}

	if envelope.Data.File != "" || envelope.Data.Selector != envelope.Data.Hash {
		t.Fatal("SQL plan advertised a nonexistent file or omitted its hash selector", envelope.Data)
	}
}
