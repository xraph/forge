package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkbenchContractsParseAndValidate(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".forge.yml")

	data := `deploy:
  version: 2
  defaults: {target: render-prod, environment: production}
  services:
    api: {app: api, kind: web, ports: {http: {port: 8080}}}
  targets:
    render-prod:
      provider: render
      build:
        source: git
        repo: https://github.com/example/atlas
        branch: main
        commit: abcdef0123456789abcdef0123456789abcdef01
        trigger: off
        services: {api: {root_dir: ., dockerfile: Dockerfile}}
      release: {mode: direct}
  environments:
    production: {target: render-prod, services: [api], environment_action: existing}
  workbench:
    persistence: {backend: files}
  secrets:
    references: {registry: 'env:GHCR_TOKEN'}
`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	d, diags, err := Parse(p)
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	if diags = Validate(d, []string{"api"}); diags.HasErrors() {
		t.Fatal(diags)
	}

	d.Deploy.Environments["production"] = Environment{Target: "render-prod", Services: []string{}}
	if !Validate(d, []string{"api"}).HasErrors() {
		t.Fatal("explicit empty selection accepted")
	}

	target := d.Deploy.Targets["render-prod"]
	target.Build.Source = "imaginary"

	d.Deploy.Targets["render-prod"] = target
	if !Validate(d, []string{"api"}).HasErrors() {
		t.Fatal("unknown build source accepted")
	}
}
