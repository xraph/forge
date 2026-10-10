package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"gopkg.in/yaml.v3"
)

func TestRollbackChecksInterveningAndInterruptedMigrations(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "intervening", true: "interrupted"}[interrupted], func(t *testing.T) {
			c, p, st, f := applyFixture(t)

			now := time.Now().UTC()
			if err := st.RecordRelease(state.Release{ID: "a", PlanHash: p.Hash, AppliedAt: now.Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}

			if interrupted {
				if err := st.Journal().Record(state.Event{Time: now, Op: "migrate:api", Status: state.StatusAccepted, IdempotencyKey: strings.Repeat("b", 64) + ":migrate:api"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := st.RecordRelease(state.Release{ID: "b", PlanHash: strings.Repeat("b", 64), AppliedAt: now.Add(-time.Minute), Migrations: map[string]bool{"migrate:api": false}}); err != nil {
					t.Fatal(err)
				}
			}

			if err := st.RecordRelease(state.Release{ID: "c", PlanHash: strings.Repeat("c", 64), AppliedAt: now}); err != nil {
				t.Fatal(err)
			}

			if err := c.Rollback(context.Background(), provider.EnvRef{}, st, "a"); err == nil || !strings.Contains(err.Error(), "migrat") {
				t.Fatalf("unsafe rollback: %v", err)
			}

			if len(f.Calls) > 0 {
				t.Fatal("Docker ran before migration compatibility check")
			}
		})
	}
}

func TestDestroyFailsClosedWhenRunningBindingsWerePruned(t *testing.T) {
	c, p, st, f := applyFixture(t)
	f.Script("docker volume", execx.Result{Stdout: "atlas-local-dev\n"})

	for i := range 21 {
		if err := st.RecordRelease(state.Release{ID: strings.Repeat("a", i+1), PlanHash: p.Hash}); err != nil {
			t.Fatal(err)
		}
	}

	scriptPS(t, c, st, f, p.Deployment, execx.Result{Stdout: `[{"Service":"worker","State":"running","Health":"healthy"}]`})

	if err := c.Destroy(context.Background(), p, st, provider.DestroyOptions{DeleteData: true}); err == nil {
		t.Fatal("unknown running dependencies allowed deletion")
	}

	for _, call := range f.CallLines() {
		if strings.Contains(call, " rm ") {
			t.Fatal("resources changed before ownership established")
		}
	}
}

func TestFailedMigrationRemainsVisibleWhenOldContainersAreHealthy(t *testing.T) {
	c, p, st, f := applyFixture(t)
	if err := st.SaveSnapshot(state.Snapshot{ActivePlanHash: p.Hash, Status: state.StatusPartial}); err != nil {
		t.Fatal(err)
	}

	if err := st.Journal().Record(state.Event{Time: time.Now(), Op: "migrate:api", Status: state.StatusFailed, IdempotencyKey: p.Hash + ":migrate:api"}); err != nil {
		t.Fatal(err)
	}

	scriptPS(t, c, st, f, p.Deployment, execx.Result{Stdout: `[{"Service":"api","State":"running","Health":"healthy","Image":"old:image"},{"Service":"cache","State":"running","Health":"healthy"},{"Service":"primary","State":"running","Health":"healthy"},{"Service":"uploads","State":"running","Health":"healthy"}]`})

	observed, err := c.Observe(context.Background(), provider.EnvRef{}, st)
	if err != nil {
		t.Fatal(err)
	}

	if observed.Overall == state.StatusHealthy {
		t.Fatal("failed deployment reported healthy")
	}

	if observed.Services["api"].Image.Repository == p.Deployment.Services[0].Image.Repository {
		t.Fatal("intended image reported as observed")
	}
}

func TestRemoteRegistryBuildUsesCustomDockerfileAndOnlyPublishesOnce(t *testing.T) {
	c, p, st, f := applyFixture(t)
	p.Deployment.Target.Build.Source = "remote"
	p.Deployment.Target.Build.Builder = "remote-builder"
	p.Deployment.Target.Build.Delivery = "registry"
	p.Deployment.Services[0].Image.Dockerfile = "custom.Dockerfile"

	if err := os.WriteFile(filepath.Join(c.root, "custom.Dockerfile"), []byte("FROM alpine:3.22\n"), 0644); err != nil {
		t.Fatal(err)
	}

	f.Script("docker pull", execx.Result{})
	f.Script("docker buildx", execx.Result{Stdout: "sha256:" + strings.Repeat("a", 64)})
	scriptPS(t, c, st, f, p.Deployment, execx.Result{Stdout: `[{"Service":"api","State":"running","Health":"healthy"},{"Service":"cache","State":"running","Health":"healthy"},{"Service":"primary","State":"running","Health":"healthy"},{"Service":"uploads","State":"running","Health":"healthy"}]`})

	var err error

	p.Operations, err = c.Operations(context.Background(), p.Deployment, nil, state.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}

	p.Hash, _ = p.ComputeHash()
	if _, err = plan.Save(c.root+"/.forge/plans", p); err != nil {
		t.Fatal(err)
	}

	if err = c.Apply(context.Background(), p, st, nil, nil); err != nil {
		t.Fatal(err)
	}

	built := false

	for _, call := range f.Calls {
		if strings.Contains(call.String(), " compose ") && strings.Contains(call.String(), " push ") {
			t.Fatal("already published remote image pushed from local store")
		}

		if strings.Contains(call.String(), "buildx build") {
			built = true
			expected := filepath.Join(st.Dir(), "build", "source-"+p.Hash, "custom.Dockerfile")
			found := false

			for i, arg := range call.Args {
				if arg == "-f" && i+1 < len(call.Args) && call.Args[i+1] == expected {
					found = true
				}
			}

			if !found {
				t.Fatalf("wrong Dockerfile: %v", call.Args)
			}
		}
	}

	if !built {
		t.Fatal("no remote build")
	}
}

func TestDefaultDockerBuilderHonorsPlatform(t *testing.T) {
	d := resolveFixture(t, "atlas-v2", "local", "dev")
	d.Target.Build.Platforms = []string{"linux/amd64"}

	b, err := New(nil, fixtureRootPath()).Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Services map[string]struct {
			Platform string `yaml:"platform"`
			Build    struct {
				Platforms []string `yaml:"platforms"`
			} `yaml:"build"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(b.Files["compose.yaml"].Content, &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Services["api"].Platform != "linux/amd64" || len(doc.Services["api"].Build.Platforms) != 1 {
		t.Fatal("requested architecture ignored")
	}
}

func TestDockerIgnorePreservesGoConfigPackages(t *testing.T) {
	d := resolveFixture(t, "atlas-v2", "local", "dev")

	b, err := New(nil, fixtureRootPath()).Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	for rule := range strings.SplitSeq(string(b.Files["api/Dockerfile.dockerignore"].Content), "\n") {
		if rule == "config" || rule == "**/config" {
			t.Fatal("Go config packages excluded from build")
		}
	}
}

func fixtureRootPath() string { return testdata.Root("atlas-v2") }
