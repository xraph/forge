package compose

import (
	"context"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func TestSubsetObserveAndLogsRetainExcludedWorkloads(t *testing.T) {
	c, p, st, f := applyFixture(t)
	ctx := context.Background()
	d := *p.Deployment
	d.Services = append([]model.Service{}, d.Services...)

	all := resolveFixtureAt(t, c.root, "local", "dev")
	for _, service := range all.Services {
		if service.Name == "worker" {
			d.Services = append(d.Services, service)
		}
	}

	bundle, err := c.Render(ctx, &d)
	if err != nil {
		t.Fatal(err)
	}

	prior, err := plan.Build(&d, bundle, state.Snapshot{}, nil, p.Operations)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := plan.Save(c.root+"/.forge/plans", prior); err != nil {
		t.Fatal(err)
	}

	snap, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	snap.ActivePlanHash = p.Hash

	snap.Workloads = map[string]state.WorkloadState{"api": {PlanHash: p.Hash}, "worker": {PlanHash: prior.Hash}}
	if err := st.SaveSnapshot(snap); err != nil {
		t.Fatal(err)
	}

	f.Script("docker compose", execx.Result{Stdout: `[{"Service":"api","State":"running","Health":"healthy"},{"Service":"worker","State":"running","Health":"healthy"}]`})

	status, err := c.Observe(ctx, provider.EnvRef{Target: "local", Env: "dev"}, st)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := status.Services["worker"]; !ok {
		t.Error("excluded live workload disappeared from status")
	}

	logs, err := c.Logs(ctx, provider.ServiceRef{Target: "local", Env: "dev", Service: "worker"}, provider.LogOptions{Tail: 1})
	if err != nil {
		t.Fatal("excluded live workload logs unavailable", err)
	}

	_ = logs.Close()
}
