package spec

import (
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
)

func TestMigrateV1Kubernetes(t *testing.T) {
	legacy := &config.DeployConfig{
		Registry: "ghcr.io/x",
		Environments: []config.EnvironmentConfig{
			{Name: "dev", Namespace: "dev", Variables: map[string]string{"LOG_LEVEL": "debug"}},
			{Name: "production", Namespace: "prod", Cluster: "prod-cluster", Region: "us-east-1"},
		},
		Kubernetes: &config.KubernetesConfig{Context: "prod-cluster", Namespace: "default"},
	}

	ops, diags := MigrateV1(legacy)
	if diags.HasErrors() {
		t.Fatal(diags)
	}

	dir := t.TempDir()
	p := write(t, dir, ".forge.yml", "project: {name: a}\ndeploy:\n  registry: ghcr.io/x\n  environments:\n    - {name: dev}\n  kubernetes: {context: c}\n")
	doc, _, _ := Parse(p)

	out, err := doc.Patch(ops)
	if err != nil {
		t.Fatal(err)
	}

	got := string(out[p])
	for _, want := range []string{"version: 2", "registry: ghcr.io/x", "production:\n      target: kubernetes-production", "namespace: prod", "LOG_LEVEL: debug", "kubernetes:\n      provider: kubernetes", "context: prod-cluster"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}

	if strings.Contains(got, "- {name: dev}") || strings.Contains(got, "kubernetes: {context: c}") {
		t.Fatalf("v1 shapes survived:\n%s", got)
	}

	doc2, pd, _ := Parse(p)
	_ = doc2
	_ = pd
}

func TestMigrateV1RenderOnlyDoesNotInventKubernetes(t *testing.T) {
	legacy := &config.DeployConfig{
		Environments: []config.EnvironmentConfig{{Name: "prod", Region: "oregon"}},
		Render:       &config.RenderConfig{Region: "oregon", GitRepo: "x/y"},
	}
	ops, _ := MigrateV1(legacy)

	var (
		targets map[string]any
		envs    map[string]any
	)

	for _, op := range ops {
		if op.Path == "deploy.targets" {
			targets = op.Value.(map[string]any)
		}

		if op.Path == "deploy.environments" && !op.Delete {
			envs = op.Value.(map[string]any)
		}
	}

	if _, ok := targets["kubernetes"]; ok {
		t.Fatal("kubernetes target invented")
	}

	if envs["prod"].(map[string]any)["target"] != "render" {
		t.Fatalf("%v", envs)
	}
}

func TestMigrateV1NoProvidersAddsCompose(t *testing.T) {
	ops, diags := MigrateV1(&config.DeployConfig{Environments: []config.EnvironmentConfig{{Name: "dev"}}})
	if len(diags) == 0 || diags[0].Severity != "info" {
		t.Fatalf("expected an info diagnostic about the default target: %v", diags)
	}

	for _, op := range ops {
		if op.Path == "deploy.environments" && !op.Delete && op.Value.(map[string]any)["dev"].(map[string]any)["target"] != "local" {
			t.Fatalf("%v", op.Value)
		}
	}
}
