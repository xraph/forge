package plugins

import (
	"encoding/json"

	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func TestPlanPrintsHashJSON(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")

	out, code := runDeploy(t, root, "plan", "--env", "dev", "--output", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	var env struct {
		Data struct {
			Hash string `json:"hash"`
			File string `json:"file"`
		} `json:"data"`
	}

	_ = json.Unmarshal([]byte(out), &env)
	if len(env.Data.Hash) != 64 || !strings.HasSuffix(env.Data.File, ".json") {
		t.Fatalf("%s", out)
	}
}

func TestApplyNonInteractiveNeedsApproval(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	runDeploy(t, root, "plan", "--env", "dev")

	out, code := runDeploy(t, root, "apply", "--env", "dev", "--non-interactive", "--output", "json")
	if code != 2 || !strings.Contains(out, "approve-plan") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestUpWithoutDockerExits2BeforePlanning(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	t.Setenv("PATH", t.TempDir()) // no docker, no go

	out, code := runDeploy(t, root, "up", "--env", "dev", "--non-interactive", "--output", "json")
	if code != 2 || !strings.Contains(out, "DEPLOY_TOOL_MISSING") {
		t.Fatalf("exit %d: %s", code, out)
	}

	if matches, _ := filepath.Glob(filepath.Join(root, ".forge", "plans", "*.json")); len(matches) != 0 {
		t.Fatal("no plan must be written when doctor fails")
	}
}

func TestProvidersListsCompose(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")

	out, code := runDeploy(t, root, "providers", "--output", "json")
	if code != 0 || !strings.Contains(out, `"compose"`) || !strings.Contains(out, `"apply"`) {
		t.Fatalf("exit %d: %s", code, out)
	}
}
