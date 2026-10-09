package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
)

func projectWithV1(t *testing.T) (*config.ForgeConfig, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".forge.yml")
	_ = os.WriteFile(p, []byte("project:\n  name: a\n  module: m\ndeploy:\n  registry: r   # keep\n  environments:\n    - {name: dev, namespace: dev}\n  kubernetes: {context: c}\n"), 0o644)

	cfg, err := config.LoadForgeConfigFrom(dir)
	if err != nil {
		t.Fatal(err)
	}

	return cfg, p
}

func TestMigrateDryRunWritesNothing(t *testing.T) {
	cfg, p := projectWithV1(t)
	before, _ := os.ReadFile(p)

	out, err := runCLI(t, NewDeployPlugin(cfg), "deploy", "migrate", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("dry run wrote the file")
	}

	if !strings.Contains(out, "+  version: 2") {
		t.Fatalf("expected a diff:\n%s", out)
	}
}

func TestMigrateYesWritesWithBackup(t *testing.T) {
	cfg, p := projectWithV1(t)

	_, err := runCLI(t, NewDeployPlugin(cfg), "deploy", "migrate", "--yes")
	if err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(p)
	if !strings.Contains(string(after), "version: 2") || !strings.Contains(string(after), "# keep") {
		t.Fatalf("%s", after)
	}

	if _, err := os.Stat(p + ".v1.bak"); err != nil {
		t.Fatal("backup missing")
	}

	_, err = runCLI(t, NewDeployPlugin(cfg), "deploy", "migrate", "--yes")
	if cli.GetExitCode(err) != 2 {
		t.Fatalf("second migrate must exit 2, got %v", err)
	}
}

func TestSchemaPrintsJSON(t *testing.T) {
	out, err := runCLI(t, NewDeployPlugin(nil), "deploy", "schema")
	if err != nil || !strings.Contains(out, `"$defs"`) {
		t.Fatalf("%v %s", err, out)
	}
}

func TestMigrateJSONIsOneDocument(t *testing.T) {
	cfg, _ := projectWithV1(t)

	out, err := runCLI(t, NewDeployPlugin(cfg), "deploy", "migrate", "--dry-run", "--output", "json", "--non-interactive")
	if err != nil {
		t.Fatal(err)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("invalid JSON output: %v %s", err, out)
	}

	if envelope["ok"] != true {
		t.Fatalf("migration preview failed: %s", out)
	}
}
func TestMigrateNonInteractiveRequiresYes(t *testing.T) {
	cfg, _ := projectWithV1(t)

	_, err := runCLI(t, NewDeployPlugin(cfg), "deploy", "migrate", "--non-interactive")
	if cli.GetExitCode(err) != 2 {
		t.Fatalf("expected consent error, got %v", err)
	}
}

func TestMigrateJSONErrorIsOneDocument(t *testing.T) {
	cfg, _ := projectWithV1(t)

	out, err := runCLI(t, NewDeployPlugin(cfg), "deploy", "migrate", "--non-interactive", "--output", "json")
	if cli.GetExitCode(err) != 2 {
		t.Fatalf("exit: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("error output: %v %q", err, out)
	}

	if envelope["ok"] != false {
		t.Fatalf("error envelope: %s", out)
	}
}
