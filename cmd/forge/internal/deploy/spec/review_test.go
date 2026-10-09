package spec

import (
	"github.com/xraph/forge/cmd/forge/config"
	"testing"
)

func TestMigrationKeepsClusterOnlyOverrideAndRegion(t *testing.T) {
	root := t.TempDir()
	p := write(t, root, ".forge.yml", "deploy:\n  kubernetes: {context: staging, namespace: apps}\n  environments: [{name: prod, cluster: production, region: us-west}]\n")

	doc, _, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}

	ops, _ := MigrateV1(&config.DeployConfig{Kubernetes: &config.KubernetesConfig{Context: "staging", Namespace: "apps"}, Environments: []config.EnvironmentConfig{{Name: "prod", Cluster: "production", Region: "us-west"}}})

	files, err := doc.Patch(ops)
	if err != nil {
		t.Fatal(err)
	}

	write(t, root, ".forge.yml", string(files[p]))

	migrated, diags, err := Parse(p)
	if err != nil || diags.HasErrors() {
		t.Fatalf("parse: %v %v", diags, err)
	}

	target := migrated.Deploy.Targets[migrated.Deploy.Environments["prod"].Target]
	if target.Context != "production" || target.Namespace != "apps" || target.Region != "us-west" {
		t.Fatalf("wrong target: %+v", target)
	}
}
func TestPatchRejectsDanglingAnchor(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "deploy:\n  kubernetes: &kube {context: prod}\nx-kube: *kube\n")

	doc, _, _ := Parse(p)
	if _, err := doc.Patch([]Op{{Path: "deploy.kubernetes", Delete: true}}); err == nil {
		t.Fatal("dangling alias accepted")
	}
}
func TestParseRejectsDuplicateDeploy(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "deploy: {version: 2, registry: first}\ndeploy: {version: 2, registry: second}\n")

	_, diags, err := Parse(p)
	if err != nil || !diags.HasErrors() {
		t.Fatalf("duplicate accepted: %v %v", diags, err)
	}
}
func TestParseChecksAliasAndMergeKeys(t *testing.T) {
	for _, syntax := range []string{"*svc", "{<<: *svc}"} {
		p := write(t, t.TempDir(), ".forge.yml", "template: &svc {app: api, kind: worker, typo: ignored}\ndeploy: {version: 2, services: {api: "+syntax+"}}\n")

		_, diags, err := Parse(p)
		if err != nil || !diags.HasErrors() {
			t.Fatalf("alias unknown key accepted: %v %v", diags, err)
		}

		p = write(t, t.TempDir(), ".forge.yml", "template: &svc {app: api, kind: worker}\ndeploy: {version: 2, services: {api: "+syntax+"}}\n")

		_, diags, err = Parse(p)
		if err != nil || diags.HasErrors() {
			t.Fatalf("valid alias/merge rejected: %v %v", diags, err)
		}
	}
}
func TestValidationRejectsAppWhenNoAppsDiscovered(t *testing.T) {
	p := write(t, t.TempDir(), ".forge.yml", "deploy: {version: 2, services: {api: {app: missing, kind: worker}}}\n")

	doc, _, _ := Parse(p)
	if !Validate(doc, []string{}).HasErrors() {
		t.Fatal("missing app accepted")
	}
}
