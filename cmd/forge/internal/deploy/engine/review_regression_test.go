package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"gopkg.in/yaml.v3"
)

func TestApplyRejectsChangedApprovedSnapshot(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, f := composeEngine(t, root)

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	st, _ := state.Open(root, "local", "dev")
	defer st.Close()

	if err := st.SaveSnapshot(state.Snapshot{Status: state.StatusCancelled, ActivePlanHash: strings.Repeat("a", 64), Resources: map[string]state.ResourceState{}}); err != nil {
		t.Fatal(err)
	}

	err = e.Apply(context.Background(), p, p.Hash, false, nil)

	var oe *output.Error
	if !errors.As(err, &oe) || oe.Code != output.ExitConflict {
		t.Fatalf("changed snapshot accepted: %v", err)
	}

	for _, call := range f.CallLines() {
		if strings.HasPrefix(call, "docker ") {
			t.Fatal("Docker ran before conflict rejection")
		}
	}
}

func TestApplyImportsVerifiedPlanBeforeRollout(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, f := composeEngine(t, root)

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	transferred, err := plan.Save(t.TempDir(), p)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(e.PlanPath(p)); err != nil {
		t.Fatal(err)
	}

	p, err = plan.Load(transferred)
	if err != nil {
		t.Fatal(err)
	}

	f.Script("docker image inspect", execx.Result{Stdout: "sha256:" + strings.Repeat("a", 64) + "\n"})
	f.Script("docker compose", execx.Result{Stdout: `[{"Service":"api","State":"running","Health":"healthy"},{"Service":"worker","State":"running","Health":"healthy"},{"Service":"gateway","State":"running","Health":"healthy"},{"Service":"cache","State":"running","Health":"healthy"},{"Service":"primary","State":"running","Health":"healthy"},{"Service":"uploads","State":"running","Health":"healthy"}]`})

	if err := e.Apply(context.Background(), p, p.Hash, false, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(e.PlanPath(p)); err != nil {
		t.Fatal("verified imported plan missing")
	}
}

func TestPartialExportPreservesActiveServiceConfiguration(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)

	full, b, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.Export(context.Background(), full, b, "", false); err != nil {
		t.Fatal(err)
	}

	st, _ := state.Open(root, "local", "dev")
	defer st.Close()

	if err := st.RecordRelease(state.Release{ID: "full", PlanHash: full.Hash}); err != nil {
		t.Fatal(err)
	}

	partial, b, err := e.PlanWithOptions(context.Background(), "local", "dev", PlanOptions{Services: []string{"api"}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.Export(context.Background(), partial, b, "", false); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "deployments", "local", "dev", "worker", "forge.overlay.yaml")); err != nil {
		t.Fatal("active worker config removed")
	}
}

func TestCustomExportDirectoryHasWorkingBuildPaths(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)

	p, b, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "review", "compose")
	if _, err := e.Export(context.Background(), p, b, dir, false); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Services map[string]struct {
			Build struct {
				Context    string `yaml:"context"`
				Dockerfile string `yaml:"dockerfile"`
			} `yaml:"build"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	build := doc.Services["api"].Build
	if _, err := os.Stat(filepath.Join(dir, build.Context, ".forge.yml")); err != nil {
		t.Fatal("build context points outside project")
	}

	if _, err := os.Stat(filepath.Join(dir, build.Context, build.Dockerfile)); err != nil {
		t.Fatal("generated Dockerfile not found")
	}
}

func TestConsecutiveRollbackFollowsActiveRelease(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, _ := composeEngine(t, root)

	base, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	st, _ := state.Open(root, "local", "dev")
	defer st.Close()

	var hashes []string

	for i, id := range []string{"a", "b", "c"} {
		p := *base
		p.Diagnostics = append(p.Diagnostics, output.Diagnostic{Message: id})

		p.Hash, _ = p.ComputeHash()
		if _, err := plan.Save(e.plansDir(), &p); err != nil {
			t.Fatal(err)
		}

		images := make([]model.Image, len(p.Deployment.Services))
		for j := range images {
			images[j].Repository = "sha256:" + strings.Repeat(id, 64)
		}

		if err := st.RecordRelease(state.Release{ID: id, PlanHash: p.Hash, Images: images, AppliedAt: time.Now().Add(time.Duration(i-3) * time.Hour), Status: state.StatusHealthy}); err != nil {
			t.Fatal(err)
		}

		hashes = append(hashes, p.Hash)
	}

	snap, _ := st.Snapshot()
	snap.ActivePlanHash = hashes[2]

	snap.Status = state.StatusHealthy
	if err := st.SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}

	if err := e.Rollback(context.Background(), "local", "dev", ""); err != nil {
		t.Fatal(err)
	}

	if err := e.Rollback(context.Background(), "local", "dev", ""); err != nil {
		t.Fatal(err)
	}

	snap, _ = st.Snapshot()
	if snap.ActivePlanHash != hashes[0] {
		t.Fatal("second rollback did not select active predecessor")
	}
}

func TestApplyRejectsReplacedRemoteContainer(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, f := composeEngine(t, root)

	initial, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	st, _ := state.Open(root, "local", "dev")
	defer st.Close()

	snap, _ := st.Snapshot()

	snap.ActivePlanHash, snap.Status = initial.Hash, state.StatusHealthy
	if err := st.SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}

	f.Script("docker compose", execx.Result{Stdout: `[{"Service":"api","ID":"first","State":"running","Health":"healthy"}]`})

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	f.Script("docker image inspect", execx.Result{Stdout: "sha256:" + strings.Repeat("a", 64)})
	f.Script("docker compose", execx.Result{Stdout: `[{"Service":"api","ID":"replaced","State":"running","Health":"healthy"},{"Service":"worker","State":"running","Health":"healthy"},{"Service":"gateway","State":"running","Health":"healthy"},{"Service":"cache","State":"running","Health":"healthy"},{"Service":"primary","State":"running","Health":"healthy"},{"Service":"uploads","State":"running","Health":"healthy"}]`})

	err = e.Apply(context.Background(), p, p.Hash, false, nil)

	var oe *output.Error
	if !errors.As(err, &oe) || oe.Code != output.ExitConflict {
		t.Fatalf("replacement accepted: %v", err)
	}
}

func TestActiveBindingsOutliveReleaseHistory(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	e, f := composeEngine(t, root)

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	f.Script("docker image inspect", execx.Result{Stdout: "sha256:" + strings.Repeat("a", 64)})
	f.Script("docker compose", execx.Result{Stdout: `[{"Service":"api","State":"running","Health":"healthy"},{"Service":"worker","State":"running","Health":"healthy"},{"Service":"gateway","State":"running","Health":"healthy"},{"Service":"cache","State":"running","Health":"healthy"},{"Service":"primary","State":"running","Health":"healthy"},{"Service":"uploads","State":"running","Health":"healthy"}]`})

	if err := e.Apply(context.Background(), p, p.Hash, false, nil); err != nil {
		t.Fatal(err)
	}

	st, _ := state.Open(root, "local", "dev")
	defer st.Close()

	for i := range 21 {
		if err := st.RecordRelease(state.Release{ID: fmt.Sprintf("api-%d", i), PlanHash: strings.Repeat("b", 64)}); err != nil {
			t.Fatal(err)
		}
	}

	raw, err := st.ReadFile("snapshot.json")
	if err != nil {
		t.Fatal(err)
	}

	var snap struct {
		Workloads map[string]struct {
			Resources []string `json:"resources"`
		} `json:"workloads"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}

	if len(snap.Workloads["worker"].Resources) == 0 {
		t.Fatal("active worker bindings lost with release pruning")
	}
}
