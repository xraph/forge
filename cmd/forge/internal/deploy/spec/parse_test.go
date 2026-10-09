package spec

import (
	"github.com/xraph/forge/cmd/forge/config"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()

	p := filepath.Join(dir, name)

	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestLocateBothSpellingsIsAmbiguous(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".forge.yml", "project: {name: a}\n")
	write(t, dir, ".forge.yaml", "project: {name: a}\n")

	_, diags, err := Locate(dir)
	if err == nil || len(diags) != 1 || diags[0].Code != "DEPLOY_CONFIG_AMBIGUOUS" {
		t.Fatalf("%v %v", err, diags)
	}
}

func TestLocateMissing(t *testing.T) {
	_, _, err := Locate(t.TempDir())
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestParseInvalidYAMLHasLine(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project:\n  name: a\ndeploy:\n  version: [\n")

	_, diags, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}

	if len(diags) != 1 || diags[0].Code != "DEPLOY_CONFIG_INVALID" || diags[0].Line == 0 {
		t.Fatalf("%v", diags)
	}
}

func TestParseV1Detected(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project: {name: a}\ndeploy:\n  registry: r\n")

	doc, diags, err := Parse(p)
	if err != nil || diags.HasErrors() {
		t.Fatal(err, diags)
	}

	if !doc.IsV1 || doc.Deploy != nil {
		t.Fatalf("%+v", doc)
	}
}

func TestParseUnknownKeyHasLineAndField(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project: {name: a}\ndeploy:\n  version: 2\n  services:\n    api:\n      app: api\n      kind: web\n      portz: {}\n")

	_, diags, _ := Parse(p)
	if len(diags) != 1 || diags[0].Code != "DEPLOY_UNKNOWN_KEY" || diags[0].Line != 8 || diags[0].Field != "deploy.services.api.portz" {
		t.Fatalf("%+v", diags)
	}
}

func TestParseV2WithSplitFile(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project: {name: a}\ndeploy:\n  version: 2\n  spec: ./deploy/stack.yml\n")
	write(t, dir, "deploy/stack.yml", "services:\n  api: {app: api, kind: web, ports: {http: {port: 8080}}}\nresources: {}\n")

	doc, diags, err := Parse(p)
	if err != nil || diags.HasErrors() {
		t.Fatal(err, diags)
	}

	if doc.Deploy.Services["api"].Ports["http"].Port != 8080 || len(doc.Splits) != 1 {
		t.Fatalf("%+v", doc.Deploy)
	}
}

func TestParseInlineAndSplitConflict(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project: {name: a}\ndeploy:\n  version: 2\n  spec: ./deploy/stack.yml\n  services: {}\n")
	write(t, dir, "deploy/stack.yml", "services: {}\n")

	_, diags, _ := Parse(p)
	if len(diags) == 0 || diags[0].Code != "DEPLOY_SPEC_KEY_CONFLICT" {
		t.Fatalf("%+v", diags)
	}
}

func TestLineIndex(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project:\n  name: a\ndeploy:\n  version: 2\n  resources:\n    primary: {type: postgres}\n  services:\n    api:\n      bindings:\n        - {resource: primary, extension: grove}\n")

	doc, _, _ := Parse(p)
	if doc.Line("deploy.resources.primary") != 6 || doc.Line("deploy.services.api.bindings.0") != 10 {
		t.Fatalf("%d %d", doc.Line("deploy.resources.primary"), doc.Line("deploy.services.api.bindings.0"))
	}
}

func TestLoadForgeConfigFrom(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".forge.yml", "project: {name: a, module: m}\n")
	write(t, dir, "sub/x.txt", "")

	_, err := config.LoadForgeConfigFrom(filepath.Join(dir, "sub"))
	if err == nil {
		t.Fatal("LoadForgeConfigFrom must not walk up")
	}

	cfg, err := config.LoadForgeConfigFrom(dir)
	if err != nil || cfg.Project.Name != "a" || cfg.RootDir != dir {
		t.Fatalf("%+v %v", cfg, err)
	}
}
