package compose

import (
	"context"
	"encoding/json"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImmutableImageOverrideIsReadyBeforeMigrations(t *testing.T) {
	c, p, st, f := applyFixture(t)
	digest := "sha256:" + strings.Repeat("a", 64)

	f.Script("docker buildx", execx.Result{})
	f.Script("docker image inspect", execx.Result{Stdout: digest})
	f.Script("docker "+strings.Join(c.composeArgs(p.Deployment, "ps", "-a", "--format", "json"), " "), execx.Result{Stdout: `[{"Service":"api","State":"running","Health":"healthy"},{"Service":"cache","State":"running","Health":"healthy"},{"Service":"primary","State":"running","Health":"healthy"},{"Service":"uploads","State":"running","Health":"healthy"}]`})

	if err := c.Apply(context.Background(), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(st.Dir(), "image-rollout.json"))
	if err != nil {
		t.Fatal("no pinned rollout override", err)
	}

	var override struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &override); err != nil {
		t.Fatal(err)
	}

	if override.Services["api"].Image != digest || override.Services["api-migrate"].Image != digest {
		t.Fatal("migration/application tags not frozen", string(raw))
	}

	inspected := false

	for _, call := range f.CallLines() {
		if strings.Contains(call, "image inspect") {
			inspected = true
		}

		if strings.Contains(call, "run --rm") || strings.Contains(call, "up -d --no-deps --no-build") {
			if !inspected || !strings.Contains(call, "image-rollout.json") {
				t.Fatal("migration/workload used mutable image", call)
			}
		}
	}
}

func TestExportDockerignoreExcludesRuntimeSources(t *testing.T) {
	d := atlasDev(t)
	c := New(nil, t.TempDir())
	// Render only needs the module source for generated Dockerfiles.
	c.root = filepath.Dir(filepath.Dir(d.Services[0].Dir))

	b, err := c.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	found := false

	for _, file := range b.Sorted() {
		if file.Path == "api/Dockerfile.dockerignore" {
			found = true

			if !strings.Contains(string(file.Content), "config/api.yaml\n") {
				t.Fatal("export build leaks runtime config")
			}
		}
	}

	if !found {
		t.Fatal("no build ignore")
	}
}

func TestFailedImageBuildRetainsApprovedRemoteIdentities(t *testing.T) {
	c, p, st, f := applyFixture(t)
	p.ObservedIDs = map[string][]string{"api": {"current-container"}}

	if err := st.SaveSnapshot(state.Snapshot{Identities: map[string][]string{"api": {"old-container"}}}); err != nil {
		t.Fatal(err)
	}

	f.Script("docker buildx", execx.Result{ExitCode: 1})

	if err := c.Apply(context.Background(), p, st, nil, nil); err == nil {
		t.Fatal("build failure ignored")
	}

	snap, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(snap.Identities, p.ObservedIDs) {
		t.Fatal("resume compares against stale remote identities", snap.Identities)
	}
}
