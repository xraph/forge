package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func (c *Compose) Operations(_ context.Context, d *model.Deployment, _ *render.Bundle, snap state.Snapshot) ([]plan.Operation, error) {
	var ops []plan.Operation

	add := func(id string, kind plan.OpKind, service, resource, detail string) {
		ops = append(ops, plan.Operation{ID: id, Kind: kind, Service: service, Resource: resource, Detail: detail})
	}
	add("deliver:secrets", plan.OpDeliver, "", "", "Resolve and persist required credentials")

	for _, s := range d.Services {
		if d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci" {
			add("build:"+s.Name, plan.OpBuild, s.Name, "", "Build the selected service image")
		}

		if d.Target.Build.Delivery == "registry" && d.Target.Build.Source != "remote" && d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci" {
			add("push:"+s.Name, plan.OpPush, s.Name, "", "Publish the selected service image")
		}
	}

	for _, r := range d.Resources {
		if r.Lifecycle == spec.LifecycleContainer {
			kind := plan.OpCreate
			if _, ok := snap.Resources[r.Name]; ok {
				kind = plan.OpUpdate
			}

			add(string(kind)+":"+r.Name, kind, "", r.Name, "Start the backend and wait for health")

			if r.RuntimeRecipe != nil && len(r.RuntimeRecipe.Init) > 0 {
				add("init:"+r.Name, plan.OpCreate, "", r.Name, "Initialize the backend")
			}
		}
	}

	for _, m := range d.Migrations {
		add("migrate:"+m.Service, plan.OpMigrate, m.Service, "", "Run migrations before application rollout")
	}

	for _, s := range d.Services {
		add("rollout:"+s.Name, plan.OpRollout, s.Name, "", "Start the selected service without removing other workloads")
	}

	add("check:stack", plan.OpCheck, "", "", "Observe every selected service and backend")

	for i := 1; i < len(ops); i++ {
		ops[i].DependsOn = []string{ops[i-1].ID}
	}

	return ops, nil
}
func (c *Compose) dockerArgs(d *model.Deployment, rest ...string) []string {
	var args []string
	if d.Target.DockerContext != "" {
		args = append(args, "--context", d.Target.DockerContext)
	}

	return append(args, rest...)
}
func (c *Compose) composeArgs(d *model.Deployment, rest ...string) []string {
	args := []string{"compose", "-p", c.projectName(d), "-f", filepath.Join(c.bundleDir(d), "compose.yaml"), "--env-file", filepath.Join(c.root, ".forge", "state", d.TargetName, d.Environment, "generated.env")}

	if len(rest) > 0 && (rest[0] == "run" || rest[0] == "up") {
		file := filepath.Join(c.root, ".forge", "state", d.TargetName, d.Environment, "image-rollout.json")
		if _, err := os.Stat(file); err == nil {
			args = append(args, "-f", file)
		}
	}

	return c.dockerArgs(d, append(args, rest...)...)
}
func (c *Compose) run(ctx context.Context, d *model.Deployment, rest ...string) (execx.Result, error) {
	return c.runDocker(ctx, d, c.composeArgs(d, rest...))
}
func record(ctx context.Context, events chan<- provider.Event, j state.Journal, p *plan.Plan, op plan.Operation, status state.Status, msg, id string) error {
	if err := j.Record(state.Event{Time: time.Now().UTC(), Op: op.ID, Status: status, Message: msg, ProviderID: id, IdempotencyKey: p.Hash + ":" + op.ID}); err != nil {
		return err
	}

	if events != nil {
		select {
		case events <- provider.Event{Op: op.ID, Status: status, Message: msg}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
func (c *Compose) Apply(ctx context.Context, p *plan.Plan, st *state.Store, values map[string]string, events chan<- provider.Event) error {
	d := p.Deployment

	j := st.Journal()
	if _, err := j.Events(); err != nil {
		return err
	}

	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	if snap.Resources == nil {
		snap.Resources = map[string]state.ResourceState{}
	}

	if snap.Workloads == nil {
		snap.Workloads = map[string]state.WorkloadState{}
		for _, v := range slices.Backward(snap.Releases) {
			prior, loadErr := c.loadPlan(st, v.PlanHash)
			if loadErr != nil {
				return loadErr
			}

			for index, svc := range prior.Deployment.Services {
				if _, known := snap.Workloads[svc.Name]; known {
					continue
				}

				workload := state.WorkloadState{PlanHash: prior.Hash}
				for _, binding := range svc.Bindings {
					workload.Resources = append(workload.Resources, binding.Resource)
				}

				if index < len(v.Images) {
					workload.Image = v.Images[index]
				}

				snap.Workloads[svc.Name] = workload
			}
		}
	}

	if snap.ActivePlanHash != p.Hash || snap.Status == state.StatusHealthy || snap.Status == state.StatusAccepted {
		snap.Revision++
		snap.Identities = p.ObservedIDs

		if err := record(ctx, events, j, p, plan.Operation{ID: "apply:begin"}, state.StatusAccepted, "Deployment attempt started", ""); err != nil {
			return err
		}
	}

	snap.FailedOperation = ""

	snap.Status, snap.ActivePlanHash = state.StatusApplying, p.Hash
	if err := st.SaveSnapshot(snap); err != nil {
		return err
	}

	fail := func(op plan.Operation, err error) error {
		snap.Status = state.StatusPartial

		snap.FailedOperation = op.ID
		if ctx.Err() != nil {
			snap.Status = state.StatusCancelled
		}

		if serr := st.SaveSnapshot(snap); serr != nil {
			return errors.Join(err, serr)
		}

		if jerr := record(context.WithoutCancel(ctx), nil, j, p, op, state.StatusFailed, "Operation failed; data and completed operations are retained", ""); jerr != nil {
			return errors.Join(err, jerr)
		}

		return fmt.Errorf("%s failed: %w", op.ID, redact(err, values))
	}

	credentials, err := generatedSecrets(d, st, values)
	if err != nil {
		return fail(plan.Operation{ID: "deliver:secrets"}, err)
	}

	values = credentials
	if err := saveSecrets(st, credentials); err != nil {
		return fail(plan.Operation{ID: "deliver:secrets"}, err)
	}

	actualImages, err := images.Build(ctx, c.runner, c.root, p, st, values)
	if err != nil {
		return fail(plan.Operation{ID: "build:images"}, err)
	}

	override := map[string]any{"services": map[string]any{}}

	pinned := override["services"].(map[string]any)
	for _, svc := range d.Services {
		pinned[svc.Name] = map[string]any{"image": images.Ref(actualImages[svc.Name]), "pull_policy": "never"}
		if len(svc.Migrate) > 0 {
			pinned[svc.Name+"-migrate"] = map[string]any{"image": images.Ref(actualImages[svc.Name]), "pull_policy": "never"}
		}
	}

	raw, err := json.Marshal(override)
	if err != nil {
		return fail(plan.Operation{ID: "build:images"}, err)
	}

	if err := st.WriteFile("image-rollout.json", raw); err != nil {
		return fail(plan.Operation{ID: "build:images"}, err)
	}

	overall := state.StatusAccepted

	for _, op := range p.Operations {
		if err := ctx.Err(); err != nil {
			return fail(op, err)
		}

		key := p.Hash + ":" + op.ID
		if _, ok := j.Completed(key); ok && op.Kind == plan.OpMigrate {
			if err := record(ctx, events, j, p, op, state.StatusAccepted, "Migration already completed for this plan", ""); err != nil {
				return fail(op, err)
			}

			continue
		}

		if err := record(ctx, events, j, p, op, state.StatusApplying, op.Detail, ""); err != nil {
			return fail(op, err)
		}

		id := ""

		switch op.Kind {
		case plan.OpDeliver:
		case plan.OpBuild, plan.OpPush:
			// The shared pipeline already verified every selected image before this stage.

		case plan.OpCreate, plan.OpUpdate:
			if strings.HasPrefix(op.ID, "init:") {
				_, err = c.run(ctx, d, "run", "--rm", "--no-deps", op.Resource+"-init")
			} else {
				_, err = c.run(ctx, d, "up", "-d", "--wait", "--no-deps", op.Resource)
				if err == nil {
					snap.Resources[op.Resource] = state.ResourceState{Name: op.Resource, Lifecycle: spec.LifecycleContainer, CreatedAt: time.Now().UTC()}
					for _, r := range d.Resources {
						if r.Name == op.Resource {
							rs := snap.Resources[op.Resource]
							rs.Type = r.Type
							snap.Resources[op.Resource] = rs
						}
					}

					err = st.SaveSnapshot(snap)
				}
			}
		case plan.OpMigrate:
			_, err = c.run(ctx, d, "run", "--rm", "--no-deps", op.Service+"-migrate")
		case plan.OpRollout:
			_, err = c.run(ctx, d, "up", "-d", "--no-deps", "--no-build", op.Service)
			if err == nil {
				for _, s := range d.Services {
					if s.Name == op.Service {
						image := actualImages[s.Name]

						workload := state.WorkloadState{PlanHash: p.Hash, Image: image}
						for _, binding := range s.Bindings {
							workload.Resources = append(workload.Resources, binding.Resource)
						}

						snap.Workloads[s.Name] = workload
						err = st.SaveSnapshot(snap)
					}
				}
			}
		case plan.OpCheck:
			var observed provider.Status
			for {
				observed, err = c.observe(ctx, st, false)
				if err != nil {
					break
				}

				if observed.Overall == state.StatusHealthy || observed.Overall == state.StatusAccepted {
					overall = observed.Overall

					break
				}

				if observed.Overall == state.StatusFailed {
					err = errors.New("a selected workload failed its readiness check")

					break
				}

				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()

					err = ctx.Err()
				case <-timer.C:
				}

				if err != nil {
					break
				}
			}
		default:
			err = fmt.Errorf("unsupported Compose operation %q", op.Kind)
		}

		if err != nil {
			return fail(op, err)
		}

		if op.Kind == plan.OpRollout || op.Kind == plan.OpCreate || op.Kind == plan.OpUpdate {
			identities, identityErr := c.SnapshotIDs(ctx, d)
			if identityErr != nil {
				return fail(op, identityErr)
			}

			snap.Identities = identities
			if err := st.SaveSnapshot(snap); err != nil {
				return fail(op, err)
			}
		}

		if err := record(ctx, events, j, p, op, state.StatusAccepted, "Completed", id); err != nil {
			return fail(op, err)
		}
	}
	// Freeze local image IDs before recording a release so rollback never uses a mutable tag.
	images := make([]model.Image, 0, len(d.Services))
	for _, s := range d.Services {
		if img, ok := actualImages[s.Name]; ok {
			images = append(images, img)
		} else {
			images = append(images, s.Image)
		}
	}

	release := state.Release{ID: "rel-" + p.Hash[:12], PlanHash: p.Hash, Images: images, AppliedAt: time.Now().UTC(), Status: overall, Migrations: map[string]bool{}}
	for _, m := range d.Migrations {
		release.Migrations["migrate:"+m.Service] = false
	}

	if err := st.RecordRelease(release); err != nil {
		return err
	}

	snap, err = st.Snapshot()
	if err != nil {
		return err
	}

	snap.Status = overall

	return st.SaveSnapshot(snap)
}
func redact(err error, values map[string]string) error {
	msg := err.Error()

	for k, v := range values {
		if strings.HasSuffix(k, "_USER") {
			continue
		}

		if len(v) > 3 {
			msg = strings.ReplaceAll(msg, v, "[redacted]")
		}
	}

	return errors.New(msg)
}
func (c *Compose) loadPlan(st *state.Store, hash string) (*plan.Plan, error) {
	return plan.Recorded(c.root, hash, filepath.Base(filepath.Dir(st.Dir())), filepath.Base(st.Dir()))
}
func (c *Compose) Rollback(ctx context.Context, _ provider.EnvRef, st *state.Store, releaseID string) error {
	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	if len(snap.Releases) == 0 {
		return errors.New("no release to roll back")
	}

	var release *state.Release

	for i := range snap.Releases {
		if snap.Releases[i].ID == releaseID {
			release = &snap.Releases[i]
		}
	}

	if release == nil {
		return errors.New("release not found")
	}

	if err := state.CheckRollback(snap, st.Journal(), *release); err != nil {
		return err
	}

	p, err := c.loadPlan(st, release.PlanHash)
	if err != nil {
		return err
	}

	if len(release.Images) != len(p.Deployment.Services) {
		return errors.New("release has no complete immutable image snapshot")
	}

	override := map[string]any{"services": map[string]any{}}

	services := override["services"].(map[string]any)
	for i, s := range p.Deployment.Services {
		services[s.Name] = map[string]string{"image": release.Images[i].Repository}
	}

	raw, err := json.Marshal(override)
	if err != nil {
		return err
	}

	if err := st.WriteFile("rollback.json", raw); err != nil {
		return err
	}
	// Use the prior config and overlays, while the override pins the exact previous image IDs.
	b, err := c.Render(ctx, p.Deployment)
	if err != nil {
		return err
	}

	if result, err := render.Write(c.bundleDir(p.Deployment), b, render.WriteOptions{PreserveMissing: true}); err != nil {
		return err
	} else if len(result.Skipped) > 0 {
		return errors.New("rollback artifacts have local edits; review before rollback")
	}

	args := c.composeArgs(p.Deployment)

	args = append(args, "-f", filepath.Join(st.Dir(), "rollback.json"), "up", "-d", "--wait", "--no-deps", "--no-build")
	for _, s := range p.Deployment.Services {
		args = append(args, s.Name)
	}

	if _, err := c.runDocker(ctx, p.Deployment, args); err != nil {
		return err
	}

	snap.ActivePlanHash = p.Hash
	snap.Status = release.Status
	snap.Revision++

	snap.FailedOperation = ""
	if snap.Workloads == nil {
		snap.Workloads = map[string]state.WorkloadState{}
	}

	for i, svc := range p.Deployment.Services {
		workload := state.WorkloadState{PlanHash: p.Hash, Image: release.Images[i]}
		for _, binding := range svc.Bindings {
			workload.Resources = append(workload.Resources, binding.Resource)
		}

		snap.Workloads[svc.Name] = workload
	}

	snap.Identities, err = c.SnapshotIDs(ctx, p.Deployment)
	if err != nil {
		return err
	}

	return st.SaveSnapshot(snap)
}
func (c *Compose) Destroy(ctx context.Context, p *plan.Plan, st *state.Store, opts provider.DestroyOptions) error {
	d := p.Deployment

	protected, err := c.protectedResources(ctx, p, st)
	if err != nil {
		return err
	}

	var names []string
	for _, s := range d.Services {
		names = append(names, s.Name)
	}

	for _, r := range d.Resources {
		if r.Lifecycle == spec.LifecycleContainer && !protected[r.Name] {
			names = append(names, r.Name)
		}
	}

	if len(names) > 0 {
		args := append([]string{"rm", "-s", "-f"}, names...)
		if _, err := c.run(ctx, d, args...); err != nil {
			return err
		}
	}

	if opts.DeleteData {
		for _, r := range d.Resources {
			if protected[r.Name] || r.Lifecycle != spec.LifecycleContainer || r.RuntimeRecipe == nil || r.RuntimeRecipe.Volume == "" {
				continue
			}

			volume := c.projectName(d) + "_" + r.Name + "-data"

			owner, err := c.runDocker(ctx, d, c.dockerArgs(d, "volume", "inspect", "--format", `{{index .Labels "com.docker.compose.project"}}`, volume))
			if err != nil {
				return err
			}

			if strings.TrimSpace(owner.Stdout) != c.projectName(d) {
				return errors.New("volume ownership does not match this deployment")
			}

			if _, err := c.runDocker(ctx, d, c.dockerArgs(d, "volume", "rm", c.projectName(d)+"_"+r.Name+"-data")); err != nil {
				return err
			}
		}
	}

	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	if opts.DeleteData {
		for _, r := range d.Resources {
			if !protected[r.Name] {
				delete(snap.Resources, r.Name)
			}
		}
	}

	snap.Status = state.StatusCancelled
	snap.Revision++

	snap.FailedOperation = ""
	for _, svc := range d.Services {
		delete(snap.Workloads, svc.Name)
		delete(snap.Identities, svc.Name)
	}

	return st.SaveSnapshot(snap)
}

// Protect backends still bound by a running service outside this selected plan.
func (c *Compose) protectedResources(ctx context.Context, p *plan.Plan, st *state.Store) (map[string]bool, error) {
	protected := map[string]bool{}

	selected := map[string]bool{}
	for _, svc := range p.Deployment.Services {
		selected[svc.Name] = true
	}

	result, err := c.run(ctx, p.Deployment, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, errors.New("cannot verify running workloads before removal")
	}

	rows, err := parseRows(result.Stdout)
	if err != nil {
		return nil, err
	}

	other := map[string]bool{}

	for _, row := range rows {
		if row.State == "running" && !selected[row.Service] {
			other[row.Service] = true
		}
	}

	if len(other) == 0 {
		return protected, nil
	}

	snap, err := st.Snapshot()
	if err != nil {
		return nil, err
	}

	for _, resource := range p.Deployment.Resources {
		delete(other, resource.Name)
		delete(other, resource.Name+"-init")
	}

	for name := range snap.Resources {
		delete(other, name)
		delete(other, name+"-init")
	}

	for name, workload := range snap.Workloads {
		if other[name] {
			for _, resource := range workload.Resources {
				protected[resource] = true
			}

			delete(other, name)
		}
	}
	// Existing state may predate the inventory. Import bindings only while their plan is still available.
	for _, release := range snap.Releases {
		if len(other) == 0 {
			break
		}

		prior, err := c.loadPlan(st, release.PlanHash)
		if err != nil {
			continue
		}

		for _, svc := range prior.Deployment.Services {
			if other[svc.Name] {
				for _, binding := range svc.Bindings {
					protected[binding.Resource] = true
				}

				delete(other, svc.Name)
			}
		}
	}

	if len(other) > 0 {
		return nil, errors.New("running workloads have unknown bindings; resource removal refused")
	}

	return protected, nil
}

func (c *Compose) runDocker(ctx context.Context, d *model.Deployment, args []string) (execx.Result, error) {
	config, err := images.ConfigDir(c.root, d)
	if err != nil {
		return execx.Result{}, err
	}

	if err := images.PrepareDocker(ctx, c.runner, c.root, d, config); err != nil {
		return execx.Result{}, err
	}

	if config != "" {
		args = append([]string{"--config", config}, args...)
	}

	return c.runner.Run(ctx, execx.Command{Name: "docker", Args: args, Dir: c.root, Env: images.DockerEnv()})
}
