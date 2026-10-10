package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestPublishUsesAuthorityHashAndExactApproval(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	f := execx.NewFake(t)
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps", execx.Result{Stdout: "net/http\n"})

	e, err := engine.New(engine.Options{Config: cfg, Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	out, code := runDeploy(t, root, "publish", "--plan", p.Hash, "--approve-plan", "wrong", "--output", "json", "--non-interactive")
	if code != 6 {
		t.Fatalf("wrong publication approval must exit 6: %d %s", code, out)
	}

	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if json.Unmarshal([]byte(out), &envelope) != nil || envelope.Command != "publish" || envelope.OK {
		t.Fatal("missing publication envelope", out)
	}

	out, code = runDeploy(t, root, "publish", "--plan", p.Hash, "--output", "json", "--non-interactive")
	if code != 2 || !strings.Contains(out, "approve-plan") {
		t.Fatal("missing approval accepted", code, out)
	}
}
