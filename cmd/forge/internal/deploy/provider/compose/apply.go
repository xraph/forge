package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"os"
	"path/filepath"
	"runtime"
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

		if d.Target.Build.Delivery == "registry" {
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

	return c.dockerArgs(d, append(args, rest...)...)
}
func (c *Compose) run(ctx context.Context, d *model.Deployment, rest ...string) (execx.Result, error) {
	return c.runner.Run(ctx, execx.Command{Name: "docker", Args: c.composeArgs(d, rest...), Dir: c.root})
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

	snap.Status, snap.ActivePlanHash = state.StatusApplying, p.Hash
	if err := st.SaveSnapshot(snap); err != nil {
		return err
	}

	fail := func(op plan.Operation, err error) error {
		snap.Status = state.StatusPartial
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

	actualImages := map[string]model.Image{}
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
		case plan.OpBuild:
			switch {
			case d.Target.Build.Builder == "host":
				var svc model.Service

				for _, s := range d.Services {
					if s.Name == op.Service {
						svc = s
					}
				}

				if err := st.MkdirAll(filepath.Join("build", svc.Name)); err != nil {
					return fail(op, err)
				}

				arch := runtime.GOARCH

				if len(d.Target.Build.Platforms) > 0 {
					parts := strings.Split(d.Target.Build.Platforms[0], "/")
					if len(parts) != 2 || parts[0] != "linux" {
						return fail(op, errors.New("host builds require a linux platform"))
					}

					arch = parts[1]
				}

				_, err = c.runner.Run(ctx, execx.Command{Name: "go", Args: []string{"build", "-mod=readonly", "-trimpath", "-ldflags", "-s -w", "-o", filepath.Join(st.Dir(), "build", svc.Name, "app"), "./" + filepath.ToSlash(svc.MainPath)}, Dir: c.root, Env: append(hostWorkEnv(c.root), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")})
				if err != nil {
					return fail(op, err)
				}

				raw, readErr := os.ReadFile(filepath.Join(c.bundleDir(d), svc.Name, "Dockerfile"))
				if readErr != nil {
					return fail(op, readErr)
				}

				if err := st.WriteFile(filepath.Join("build", svc.Name, "Dockerfile"), raw); err != nil {
					return fail(op, err)
				}

				_, err = c.run(ctx, d, "build", op.Service)
			case d.Target.Build.Source == "remote":
				var svc model.Service

				for _, s := range d.Services {
					if s.Name == op.Service {
						svc = s
					}
				}

				args := []string{"buildx", "build", "--builder", d.Target.Build.Builder, "-t", imageRef(svc.Image), "-f", filepath.Join(c.bundleDir(d), svc.Name, "Dockerfile")}
				if len(d.Target.Build.Platforms) > 0 {
					args = append(args, "--platform", strings.Join(d.Target.Build.Platforms, ","))
				}

				if d.Target.Build.Delivery == "registry" {
					args = append(args, "--push")
				} else {
					args = append(args, "--load")
				}

				args = append(args, c.root)
				_, err = c.runner.Run(ctx, execx.Command{Name: "docker", Args: c.dockerArgs(d, args...), Dir: c.root})
			default:
				_, err = c.run(ctx, d, "build", op.Service)
			}
		case plan.OpPush:
			_, err = c.run(ctx, d, "push", op.Service)
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
						res, inspectErr := c.runner.Run(ctx, execx.Command{Name: "docker", Args: c.dockerArgs(d, "image", "inspect", "--format", "{{.Id}}", imageRef(s.Image)), Dir: c.root})
						if inspectErr != nil {
							err = inspectErr

							break
						}

						image := s.Image
						image.Repository = strings.TrimSpace(res.Stdout)
						image.Tag = ""
						image.Digest = ""
						actualImages[s.Name] = image
					}
				}
			}
		case plan.OpCheck:
			var observed provider.Status
			for {
				observed, err = c.Observe(ctx, provider.EnvRef{Target: d.TargetName, Env: d.Environment}, st)
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
	if len(hash) != 64 {
		return nil, errors.New("recorded deployment hash is invalid")
	}

	matches, err := filepath.Glob(filepath.Join(c.root, ".forge", "plans", "*-"+hash[:min(12, len(hash))]+".json"))
	if err != nil {
		return nil, err
	}

	for _, path := range matches {
		p, err := plan.Load(path)
		if err == nil && p.Hash == hash && p.Environment == filepath.Base(st.Dir()) && p.TargetName == filepath.Base(filepath.Dir(st.Dir())) {
			return p, nil
		}
	}

	return nil, errors.New("recorded deployment plan is unavailable")
}
func (c *Compose) Rollback(ctx context.Context, _ provider.EnvRef, st *state.Store, releaseID string) error {
	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	if len(snap.Releases) == 0 {
		return errors.New("no release to roll back")
	}

	current := snap.Releases[len(snap.Releases)-1]
	for id, reversible := range current.Migrations {
		if !reversible {
			return fmt.Errorf("%s is not marked reversible; roll forward", id)
		}
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

	if result, err := render.Write(c.bundleDir(p.Deployment), b, render.WriteOptions{}); err != nil {
		return err
	} else if len(result.Skipped) > 0 {
		return errors.New("rollback artifacts have local edits; review before rollback")
	}

	args := c.composeArgs(p.Deployment)

	args = append(args, "-f", filepath.Join(st.Dir(), "rollback.json"), "up", "-d", "--wait", "--no-deps", "--no-build")
	for _, s := range p.Deployment.Services {
		args = append(args, s.Name)
	}

	if _, err := c.runner.Run(ctx, execx.Command{Name: "docker", Args: args, Dir: c.root}); err != nil {
		return err
	}

	snap.ActivePlanHash = p.Hash
	snap.Status = release.Status

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

			owner, err := c.runner.Run(ctx, execx.Command{Name: "docker", Args: c.dockerArgs(d, "volume", "inspect", "--format", `{{index .Labels "com.docker.compose.project"}}`, volume), Dir: c.root})
			if err != nil {
				return err
			}

			if strings.TrimSpace(owner.Stdout) != c.projectName(d) {
				return errors.New("volume ownership does not match this deployment")
			}

			if _, err := c.runner.Run(ctx, execx.Command{Name: "docker", Args: c.dockerArgs(d, "volume", "rm", c.projectName(d)+"_"+r.Name+"-data"), Dir: c.root}); err != nil {
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

	return st.SaveSnapshot(snap)
}

func hostWorkEnv(root string) []string {
	work := filepath.Join(root, "go.work")
	if _, err := os.Stat(work); err == nil {
		return []string{"GOWORK=" + work}
	}

	return []string{"GOWORK=off"}
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

	for _, release := range snap.Releases {
		prior, err := c.loadPlan(st, release.PlanHash)
		if err != nil {
			return nil, err
		}

		for _, svc := range prior.Deployment.Services {
			if other[svc.Name] {
				for _, binding := range svc.Bindings {
					protected[binding.Resource] = true
				}
			}
		}
	}

	return protected, nil
}
