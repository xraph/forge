package kubernetes

import (
	"context"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func (*Kubernetes) Operations(_ context.Context, d *model.Deployment, _ *render.Bundle, snap state.Snapshot) ([]plan.Operation, error) {
	ops := []plan.Operation{}
	add := func(id string, kind plan.OpKind, service, resource, detail string) {
		op := plan.Operation{ID: id, Kind: kind, Service: service, Resource: resource, Detail: detail}
		if len(ops) > 0 {
			op.DependsOn = []string{ops[len(ops)-1].ID}
		}

		ops = append(ops, op)
	}
	add("deliver:secrets", plan.OpDeliver, "", "", "Resolve and persist selected credentials")

	for _, s := range d.Services {
		if d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci" {
			add("build:"+s.Name, plan.OpBuild, s.Name, "", "Build and verify immutable image")
		}

		if d.Target.Build.Delivery == "registry" {
			add("push:"+s.Name, plan.OpPush, s.Name, "", "Publish and verify registry digest")
		}
	}

	add("namespace:ensure", plan.OpCreate, "", "", "Ensure explicit namespace and check owned object identities")
	add("deliver:cluster", plan.OpDeliver, "", "", "Deliver credential references and validate manifests against the cluster")

	for _, r := range d.Resources {
		if r.Lifecycle != spec.LifecycleContainer {
			continue
		}

		kind := plan.OpCreate
		if _, exists := snap.Resources[r.Name]; exists {
			kind = plan.OpUpdate
		}

		add(string(kind)+":"+r.Name, kind, "", r.Name, "Start backend and wait for readiness")

		if len(r.RuntimeRecipe.Init) > 0 {
			add("init:"+r.Name, plan.OpCreate, "", r.Name, "Complete backend initialization Job")
		}
	}

	for _, s := range d.Services {
		if len(s.Migrate) > 0 {
			add("migrate:"+s.Name, plan.OpMigrate, s.Name, "", "Complete this revision's migration before workload rollout")
		}
	}

	for _, s := range d.Services {
		add("rollout:"+s.Name, plan.OpRollout, s.Name, "", "Roll out selected workload and wait for readiness")
	}

	add("route:policy", plan.OpRoute, "", "", "Apply explicit HTTP routes and declared network policy")
	add("check:stack", plan.OpCheck, "", "", "Observe all recorded selected workloads and backends")

	return ops, nil
}
