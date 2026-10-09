package plugins

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func runDeploy(t *testing.T, root string, args ...string) (string, int) {
	t.Helper()

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	app := cli.New(cli.Config{Name: "forge", Version: "test"})

	var out bytes.Buffer
	app.SetOutput(&out)

	if err := app.RegisterPlugin(NewDeployPlugin(cfg)); err != nil {
		t.Fatal(err)
	}

	err = app.Run(append([]string{"forge", "deploy"}, args...))

	return out.String(), cli.GetExitCode(err)
}

func TestInspectJSONEnvelope(t *testing.T) {
	out, code := runDeploy(t, testdata.Root("atlas-v2"), "inspect", "--output", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	var env struct {
		Schema string `json:"schema"`
		OK     bool   `json:"ok"`
		Data   struct {
			Services []struct {
				Name string `json:"name"`
			} `json:"services"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}

	if env.Schema != "forge.deploy/v1" || !env.OK || len(env.Data.Services) != 3 {
		t.Fatalf("%+v", env)
	}
}

func TestInspectWithoutBlockExits3(t *testing.T) {
	out, code := runDeploy(t, testdata.Root("atlas"), "inspect", "--output", "json")
	if code != 3 {
		t.Fatalf("exit %d: %s", code, out)
	}

	if !strings.Contains(out, "DEPLOY_VERSION_MISSING") || !strings.Contains(out, `"suggestions"`) {
		t.Fatalf("%s", out)
	}
}

func TestInitNonInteractiveListsAnswers(t *testing.T) {
	root := testdata.Copy(t, "atlas")

	out, code := runDeploy(t, root, "init", "--non-interactive", "--output", "json")
	if code != 3 || !strings.Contains(out, "--answer deploy.services.worker.health=heartbeat|none") {
		t.Fatalf("exit %d: %s", code, out)
	}

	out, code = runDeploy(t, root, "init", "--non-interactive", "--yes",
		"--answer", "deploy.services.worker.health=heartbeat",
		"--answer", "deploy.resources.cache.features=json,search")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	data, _ := os.ReadFile(root + "/.forge.yml")
	if !strings.Contains(string(data), "version: 2") {
		t.Fatalf("block not written:\n%s", data)
	}
}

func TestDoctorTextOutputHasTable(t *testing.T) {
	out, code := runDeploy(t, testdata.Root("atlas-v2"), "doctor", "--offline")
	if code != 0 && code != 2 {
		t.Fatalf("exit %d: %s", code, out)
	}

	if !strings.Contains(out, "Code") || !strings.Contains(out, "DEPLOY_") {
		t.Fatalf("expected a diagnostics table: %s", out)
	}
}
