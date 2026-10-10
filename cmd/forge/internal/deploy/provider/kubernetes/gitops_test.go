package kubernetes

import (
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

func gitopsFixture(t *testing.T) (*Kubernetes, *model.Deployment) {
	t.Helper()
	k, d := fixture(t)
	d.Target.Build = spec.Build{Source: "existing", Delivery: "registry", Registry: spec.Registry{Visibility: "public"}}
	d.Target.Release = spec.Release{Mode: "gitops", Repo: "https://github.com/acme/atlas-manifests", Branch: "main", Path: "environments/production", Controller: "argo-cd"}

	d.Migrations = nil
	for i := range d.Services {
		d.Services[i].Migrate = nil
		d.Services[i].Image = model.Image{Repository: "ghcr.io/acme/" + d.Services[i].Name, Digest: "sha256:" + strings.Repeat("a", 64)}
	}

	for i := range d.Resources {
		d.Resources[i].Lifecycle = spec.LifecycleExternal
		d.Resources[i].RuntimeRecipe = nil
		d.Resources[i].Secret.Resolver = "env"
	}

	return k, d
}
func TestGitOpsHandoff(t *testing.T) {
	k, d := gitopsFixture(t)

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	var handoff struct {
		Schema     string `json:"schema"`
		Repository string `json:"repository"`
		Path       string `json:"path"`
		Controller string `json:"controller"`
		Prune      bool   `json:"prune"`
		Secrets    []struct {
			Name string   `json:"name"`
			Keys []string `json:"keys"`
		} `json:"secrets"`
	}

	raw := b.Files["gitops-handoff.json"].Content
	if err := json.Unmarshal(raw, &handoff); err != nil {
		t.Fatal(err)
	}

	if handoff.Schema != "forge.deploy.gitops/v1" || handoff.Repository != d.Target.Release.Repo || handoff.Path != d.Target.Release.Path || handoff.Controller != "argo-cd" || handoff.Prune || len(handoff.Secrets) == 0 {
		t.Fatal("incomplete handoff", string(raw))
	}

	if !strings.Contains(string(b.Files["GITOPS.md"].Content), "existing Secrets") {
		t.Fatal("missing bootstrap instructions")
	}

	if _, ok := b.Files["migrations/kustomization.yaml"]; ok {
		t.Fatal("unordered migration exported")
	}

	again, err := k.Render(context.Background(), d)
	if err != nil || !reflect.DeepEqual(b.Hashes(), again.Hashes()) {
		t.Fatal("unstable handoff", err)
	}

	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl unavailable for Kustomize parse")
	}

	dir := t.TempDir()
	if _, err := render.Write(dir, b, render.WriteOptions{}); err != nil {
		t.Fatal(err)
	}

	if output, err := exec.CommandContext(t.Context(), "kubectl", "kustomize", dir).CombinedOutput(); err != nil {
		t.Fatalf("invalid Kustomize handoff: %v %s", err, output)
	}
}
func TestGitOpsRejectsUnqualifiedDelivery(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*model.Deployment)
	}{
		{"repository-credentials", func(d *model.Deployment) { d.Target.Release.Repo = "https://token:private@github.com/acme/manifests" }},
		{"path-escape", func(d *model.Deployment) { d.Target.Release.Path = "../outside" }},
		{"windows-path-escape", func(d *model.Deployment) { d.Target.Release.Path = `env\..\..\outside` }},
		{"controller", func(d *model.Deployment) { d.Target.Release.Controller = "fictional" }},
		{"mutable-image", func(d *model.Deployment) { d.Services[0].Image.Digest = "" }},
		{"container-resource", func(d *model.Deployment) { d.Resources[0].Lifecycle = spec.LifecycleContainer }},
		{"migration", func(d *model.Deployment) { d.Services[0].Migrate = []string{"migrate", "up"} }},
		{"oneoff", func(d *model.Deployment) { d.Services[0].Kind = spec.KindJob }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, d := gitopsFixture(t)
			tc.edit(d)

			if !k.Validate(context.Background(), d).HasErrors() {
				t.Fatal("unqualified GitOps accepted")
			}

			if _, err := k.Render(context.Background(), d); err == nil {
				t.Fatal("unqualified handoff rendered")
			}
		})
	}
}
