package compose

import (
	"context"
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func applyFixture(t *testing.T) (*Compose, *plan.Plan, *state.Store, *execx.Fake) {
	t.Helper()
	root := testdata.Copy(t, "atlas-v2")
	d := resolveFixtureAt(t, root, "local", "dev")
	d.Services = d.Services[:1]
	d.Connections = nil
	d.Routes = nil
	f := execx.NewFake(t)
	f.Script("docker compose", execx.Result{})
	f.Script("docker buildx", execx.Result{})
	f.Script("docker image inspect", execx.Result{Stdout: "sha256:" + strings.Repeat("a", 64) + "\n"})
	c := New(f, root)

	b, err := c.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	ops, err := c.Operations(context.Background(), d, b, state.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	p, err := plan.Build(d, b, state.Snapshot{}, nil, ops)
	if err != nil {
		t.Fatal(err)
	}

	st, err := state.Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	_, err = render.Write(c.bundleDir(d), b, render.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = plan.Save(root+"/.forge/plans", p); err != nil {
		t.Fatal(err)
	}

	return c, p, st, f
}
func TestApplyStopsBeforeRolloutWhenMigrationFails(t *testing.T) {
	c, p, st, f := applyFixture(t)
	if err := st.WriteFile("image-rollout.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}

	prefix := strings.Join(c.composeArgs(p.Deployment, "run", "--rm", "--no-deps", "api-migrate"), " ")
	f.Script("docker "+prefix, execx.Result{ExitCode: 1, Stderr: "migration failed"})

	err := c.Apply(context.Background(), p, st, nil, nil)
	if err == nil {
		t.Fatal("migration failure ignored")
	}

	for _, call := range f.CallLines() {
		if strings.Contains(call, " up -d --no-deps --no-build api") {
			t.Fatal("application rolled out after failure")
		}
	}

	snap, _ := st.Snapshot()
	if snap.Status != state.StatusPartial {
		t.Fatal(snap.Status)
	}
}
func TestApplyPersistsCredentialsAndNeverRemovesDeselectedServices(t *testing.T) {
	c, p, st, f := applyFixture(t)
	rows := `[{"Service":"api","State":"running","Health":"healthy"},{"Service":"cache","State":"running","Health":"healthy"},{"Service":"primary","State":"running","Health":"healthy"},{"Service":"uploads","State":"running","Health":"healthy"}]`
	scriptPS(t, c, st, f, p.Deployment, execx.Result{Stdout: rows})

	if err := c.Apply(context.Background(), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	before, err := st.ReadFile("generated.env")
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Apply(context.Background(), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	after, _ := st.ReadFile("generated.env")
	if string(before) != string(after) {
		t.Fatal("credentials changed on retry")
	}

	for _, call := range f.CallLines() {
		if strings.Contains(call, "--remove-orphans") {
			t.Fatal("deselected services removed")
		}
	}

	snap, _ := st.Snapshot()
	if snap.Status != state.StatusHealthy || len(snap.Releases) != 1 {
		t.Fatal(snap)
	}

	info, _ := os.Stat(st.SecretsPath())
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestCorruptJournalBlocksBeforeDocker(t *testing.T) {
	c, p, st, f := applyFixture(t)

	_ = st.WriteFile("journal.jsonl", []byte("not json\n"))
	if err := c.Apply(context.Background(), p, st, nil, nil); err == nil {
		t.Fatal("corrupt journal ignored")
	}

	if len(f.Calls) != 0 {
		t.Fatal("Docker invoked")
	}
}
func TestRollbackRefusesIrreversibleMigration(t *testing.T) {
	c, p, st, f := applyFixture(t)
	_ = st.RecordRelease(state.Release{ID: "old", PlanHash: p.Hash})
	_ = st.RecordRelease(state.Release{ID: "new", Migrations: map[string]bool{"migrate:api": false}})

	err := c.Rollback(context.Background(), provider.EnvRef{Target: "local", Env: "dev"}, st, "old")
	if err == nil || !strings.Contains(err.Error(), "migrate:api") {
		t.Fatal(err)
	}

	if len(f.Calls) != 0 {
		t.Fatal("Docker invoked")
	}
}
func TestObserveCountsAllReplicasAndRejectsInvalidOutput(t *testing.T) {
	c, p, st, f := applyFixture(t)
	p.Deployment.Services[0].Replicas = 2
	_ = st.SaveSnapshot(state.Snapshot{ActivePlanHash: p.Hash})
	p.Hash, _ = p.ComputeHash()
	_ = st.SaveSnapshot(state.Snapshot{ActivePlanHash: p.Hash})
	_, _ = plan.Save(c.root+"/.forge/plans", p)
	prefix := observationPrefix(t, c, st, p.Deployment)
	f.Script(prefix, execx.Result{Stdout: `{"Service":"api","State":"running","Health":"healthy"}` + "\n" + `{"Service":"api","State":"running","Health":"starting"}` + "\n"})

	s, err := c.Observe(context.Background(), provider.EnvRef{}, st)
	if err != nil || s.Services["api"].Ready != 1 || s.Overall == state.StatusHealthy {
		t.Fatal(err, s)
	}

	f.Script(prefix, execx.Result{Stdout: "broken-json"})

	s, err = c.Observe(context.Background(), provider.EnvRef{}, st)
	if err == nil || s.Overall != state.StatusUnknown {
		t.Fatal(err, s)
	}

	_ = errors.Is(err, provider.ErrUnsupported)
}

func TestDestroyProtectsBackendsUsedByDeselectedRunningServices(t *testing.T) {
	c, p, st, f := applyFixture(t)
	f.Script("docker volume rm", execx.Result{})

	prior := *p
	prior.Deployment = &model.Deployment{}
	*prior.Deployment = *p.Deployment
	worker := p.Deployment.Services[0]
	worker.Name = "worker"
	prior.Deployment.Services = append(append([]model.Service(nil), p.Deployment.Services...), worker)

	prior.Hash, _ = prior.ComputeHash()
	if _, err := plan.Save(c.root+"/.forge/plans", &prior); err != nil {
		t.Fatal(err)
	}

	if err := st.RecordRelease(state.Release{ID: "full", PlanHash: prior.Hash}); err != nil {
		t.Fatal(err)
	}

	scriptPS(t, c, st, f, p.Deployment, execx.Result{Stdout: `[{"Service":"worker","State":"running","Health":"healthy"}]`})

	if err := c.Destroy(context.Background(), p, st, provider.DestroyOptions{DeleteData: true}); err != nil {
		t.Fatal(err)
	}

	for _, call := range f.CallLines() {
		if strings.Contains(call, " rm -s -f ") && (strings.Contains(call, " primary") || strings.Contains(call, " cache") || strings.Contains(call, " uploads")) {
			t.Fatal("shared backend stopped: " + call)
		}

		if strings.Contains(call, " volume rm ") {
			t.Fatal("shared persistent data removed")
		}
	}
}

func TestRollbackRejectsEditedGeneratedArtifact(t *testing.T) {
	c, p, st, f := applyFixture(t)

	images := make([]model.Image, len(p.Deployment.Services))
	for i := range images {
		images[i].Repository = "sha256:" + strings.Repeat("a", 64)
	}

	if err := st.RecordRelease(state.Release{ID: "old", PlanHash: p.Hash, Images: images}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(c.bundleDir(p.Deployment), "compose.yaml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := c.Rollback(context.Background(), provider.EnvRef{}, st, "old"); err == nil {
		t.Fatal("edited rollback files accepted")
	}

	if len(f.Calls) != 0 {
		t.Fatal("rollback ran against edited files")
	}
}
