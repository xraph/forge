package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func (k *Kubernetes) Preflight(ctx context.Context, d *model.Deployment) error {
	if d.Target.Context == "" {
		return errors.New("select an explicit Kubernetes context")
	}

	if _, err := k.run(ctx, d, nil, "get", "--raw", "/readyz"); err != nil {
		return errors.New("selected Kubernetes context is unavailable")
	}

	if d.Target.Build.Delivery != "registry" {
		if d.Target.LocalCluster == "" || d.Target.Context != "kind-"+d.Target.LocalCluster {
			return errors.New("local image delivery requires a named kind cluster; use registry delivery for other clusters")
		}

		env := []string{}
		if d.Target.DockerContext != "" {
			env = append(env, "DOCKER_CONTEXT="+d.Target.DockerContext)
		}

		result, err := k.runner.Run(ctx, execx.Command{Name: "kind", Args: []string{"get", "nodes", "--name", d.Target.LocalCluster}, Env: env, Dir: k.root})
		if err != nil || strings.TrimSpace(result.Stdout) == "" {
			return errors.New("the selected kind cluster is unavailable on this Docker host")
		}
	}

	registry := d.Target.Build.Registry
	if registry.Visibility == "private" && registry.PullSecret == "" {
		return errors.New("private image delivery requires cluster pull_secret as well as builder push access")
	}

	if registry.PullSecret != "" {
		result, err := k.run(ctx, d, nil, "get", "secret", registry.PullSecret, "-o", "json")
		if err != nil {
			return errors.New("cluster pull_secret is unavailable in the selected namespace")
		}

		var secret struct {
			Type string            `json:"type"`
			Data map[string]string `json:"data"`
		}
		if json.Unmarshal([]byte(result.Stdout), &secret) != nil || secret.Type != "kubernetes.io/dockerconfigjson" || secret.Data[".dockerconfigjson"] == "" {
			return errors.New("cluster pull_secret must be a Docker registry Secret")
		}
	}

	return k.routePreflight(ctx, d)
}
func journalEvent(ctx context.Context, events chan<- provider.Event, st *state.Store, p *plan.Plan, op plan.Operation, status state.Status) error {
	message := op.Detail
	if status == state.StatusFailed {
		message = "Operation failed; completed work and persistent data are retained"
	}

	if err := st.Journal().Record(state.Event{Time: time.Now().UTC(), Op: op.ID, Status: status, Message: message, IdempotencyKey: p.Hash + ":" + op.ID}); err != nil {
		return err
	}

	if events != nil {
		select {
		case events <- provider.Event{Op: op.ID, Status: status, Message: message}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
func (k *Kubernetes) Apply(ctx context.Context, p *plan.Plan, st *state.Store, external map[string]string, events chan<- provider.Event) error {
	if ds := k.Validate(ctx, p.Deployment); ds.HasErrors() {
		return fmt.Errorf("invalid Kubernetes deployment: %v", ds)
	}

	if p.Target.Release.Mode == "gitops" {
		return provider.ErrUnsupported
	}

	if _, err := st.Journal().Events(); err != nil {
		return err
	}

	if err := k.Preflight(ctx, p.Deployment); err != nil {
		return err
	}

	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	released := slices.ContainsFunc(snap.Releases, func(r state.Release) bool { return r.PlanHash == p.Hash })
	if released && snap.ActivePlanHash == p.Hash && (snap.Status == state.StatusHealthy || snap.Status == state.StatusAccepted) {
		observed, err := k.Observe(ctx, provider.EnvRef{}, st)
		if err != nil {
			return err
		}

		if observed.Overall == state.StatusHealthy || observed.Overall == state.StatusAccepted {
			return nil
		}

		return errors.New("recorded release is no longer ready; create a new plan")
	}

	if snap.Resources == nil {
		snap.Resources = map[string]state.ResourceState{}
	}

	if snap.Workloads == nil {
		snap.Workloads = map[string]state.WorkloadState{}
	}

	if snap.ActivePlanHash != p.Hash {
		snap.Revision++

		snap.Identities = p.ObservedIDs
		if err := journalEvent(ctx, events, st, p, plan.Operation{ID: "apply:begin", Detail: "Deployment attempt started"}, state.StatusAccepted); err != nil {
			return err
		}
	}

	snap.ActivePlanHash = p.Hash
	snap.Status = state.StatusApplying

	snap.FailedOperation = ""
	if err := st.SaveSnapshot(snap); err != nil {
		return err
	}

	failed := func(op plan.Operation, cause error) error {
		snap.Status = state.StatusPartial

		snap.FailedOperation = op.ID
		if ctx.Err() != nil {
			snap.Status = state.StatusCancelled
		}

		return errors.Join(fmt.Errorf("%s failed: %w", op.ID, cause), st.SaveSnapshot(snap), journalEvent(context.WithoutCancel(ctx), nil, st, p, op, state.StatusFailed))
	}

	credentials, err := secrets.Generate(p.Deployment, st, external)
	if err != nil {
		return failed(plan.Operation{ID: "deliver:secrets"}, err)
	}

	raw, err := godotenv.Marshal(credentials)
	if err != nil {
		return failed(plan.Operation{ID: "deliver:secrets"}, errors.New("cannot encode private credentials"))
	}

	if err := st.WriteFile("generated.env", []byte(raw+"\n")); err != nil {
		return failed(plan.Operation{ID: "deliver:secrets"}, err)
	}

	actual, err := images.Build(ctx, k.runner, k.root, p, st, external)
	if err != nil {
		return failed(plan.Operation{ID: "build:images"}, err)
	}

	d, err := cloneDeployment(p.Deployment)
	if err != nil {
		return failed(plan.Operation{ID: "build:images"}, err)
	}

	for i := range d.Services {
		image := actual[d.Services[i].Name]

		image, err = k.deliverLocal(ctx, d, image, d.Services[i].Name)
		if err != nil {
			return failed(plan.Operation{ID: "deliver:images"}, err)
		}

		d.Services[i].Image = image
	}

	b, err := k.runtimeBundle(ctx, d, snap)
	if err != nil {
		return failed(plan.Operation{ID: "render:runtime"}, err)
	}

	desired, err := bundleObjects(b)
	if err != nil {
		return failed(plan.Operation{ID: "render:runtime"}, err)
	}

	credentialObject := makeObject(d, "v1", "Secret", identity(d), object{"type": "Opaque", "stringData": credentials})
	apply := func(items []object) error {
		if err := k.applyObjects(ctx, d, items, false); err != nil {
			return err
		}

		ids, err := k.SnapshotIDs(ctx, d)
		if err != nil {
			return err
		}

		snap.Identities = ids

		return st.SaveSnapshot(snap)
	}
	applyStage := func(stage string, match func(object) bool) error {
		items, err := stageObjects(b, stage)
		if err != nil {
			return err
		}

		if match != nil {
			items = slices.DeleteFunc(items, func(obj object) bool { return !match(obj) })
		}

		return apply(items)
	}

	for _, op := range p.Operations {
		if err := ctx.Err(); err != nil {
			return failed(op, err)
		}

		if _, done := st.Journal().Completed(p.Hash + ":" + op.ID); done {
			continue
		}

		if err := journalEvent(ctx, events, st, p, op, state.StatusApplying); err != nil {
			return failed(op, err)
		}

		switch {
		case op.ID == "deliver:secrets", op.Kind == plan.OpBuild, op.Kind == plan.OpPush:
		case op.ID == "namespace:ensure":
			result, getErr := k.run(ctx, d, nil, "get", "namespace", namespace(d), "--ignore-not-found", "-o", "json")
			if getErr != nil {
				err = errors.New("cannot check namespace access")
			} else if strings.TrimSpace(result.Stdout) == "" {
				err = applyStage("namespace", nil)
			}
		case op.ID == "deliver:cluster":
			var live map[string]object

			live, err = k.clusterObjects(ctx, d, false)
			if err == nil {
				for key, obj := range live {
					if _, exists := desired[key]; exists && !owned(d, obj) {
						err = fmt.Errorf("refusing to overwrite unowned Kubernetes object %s", key)

						break
					}
				}

				if obj, exists := live["Secret/"+identity(d)]; exists && !owned(d, obj) {
					err = errors.New("deployment credential Secret is owned by another deployment")
				}
			}

			if err == nil {
				items := []object{credentialObject}

				for key, obj := range desired {
					if !strings.HasPrefix(key, "Namespace/") {
						items = append(items, obj)
					}
				}

				sortObjects(items)
				err = k.applyObjects(ctx, d, items, true)
			}

			if err == nil {
				err = apply([]object{credentialObject})
			}

			if err == nil {
				err = applyStage("config", nil)
			}

			if err == nil {
				err = applyStage("policy", nil)
			}
		case op.Resource != "":
			if strings.HasPrefix(op.ID, "init:") {
				err = applyStage("init", func(obj object) bool {
					return obj["metadata"].(map[string]any)["name"] == jobName(d, op.Resource+"-init")
				})
				if err == nil {
					err = k.wait(ctx, d, "Job", jobName(d, op.Resource+"-init"))
				}
			} else {
				err = applyStage("resources", func(obj object) bool { return obj["metadata"].(map[string]any)["name"] == op.Resource })
				if err == nil {
					err = k.wait(ctx, d, "StatefulSet", op.Resource)
				}

				if err == nil {
					snap.Resources[op.Resource] = state.ResourceState{Name: op.Resource, Type: resourceType(d, op.Resource), Lifecycle: spec.LifecycleContainer, ProviderID: firstID(snap.Identities["StatefulSet/"+op.Resource]), CreatedAt: time.Now().UTC()}
				}
			}
		case op.Kind == plan.OpMigrate:
			err = applyStage("migrations", func(obj object) bool {
				return obj["metadata"].(map[string]any)["name"] == jobName(d, op.Service+"-migrate")
			})
			if err == nil {
				err = k.wait(ctx, d, "Job", jobName(d, op.Service+"-migrate"))
			}
		case op.Kind == plan.OpRollout:
			err = applyStage("applications", func(obj object) bool {
				meta := obj["metadata"].(map[string]any)
				name := meta["name"].(string)

				return name == op.Service || name == jobName(d, op.Service)
			})
			if err == nil {
				for _, s := range d.Services {
					if s.Name != op.Service {
						continue
					}

					kind, name := "Deployment", s.Name
					if s.Kind == spec.KindJob {
						kind, name = "Job", jobName(d, s.Name)
					}

					if s.Kind != spec.KindCron {
						err = k.wait(ctx, d, kind, name)
					}

					if err == nil {
						w := state.WorkloadState{PlanHash: p.Hash, Image: s.Image}
						for _, binding := range s.Bindings {
							w.Resources = append(w.Resources, binding.Resource)
						}

						snap.Workloads[s.Name] = w
					}
				}
			}
		case op.ID == "route:policy":
			err = applyStage("routes", nil)
			if err == nil {
				err = k.waitRoutes(ctx, d)
			}
		case op.ID == "check:stack":
			if err = st.SaveSnapshot(snap); err == nil {
				var observed provider.Status

				observed, err = k.Observe(ctx, provider.EnvRef{}, st)
				if err == nil && (observed.Overall != state.StatusHealthy && observed.Overall != state.StatusAccepted) {
					err = errors.New("selected stack has unready or unknown workloads")
				}
			}
		default:
			err = provider.ErrUnsupported
		}

		if err != nil {
			return failed(op, err)
		}

		if err := st.SaveSnapshot(snap); err != nil {
			return failed(op, err)
		}

		if err := journalEvent(ctx, events, st, p, op, state.StatusAccepted); err != nil {
			return failed(op, err)
		}
	}

	observed, err := k.Observe(ctx, provider.EnvRef{}, st)
	if err != nil {
		return err
	}

	snap.Status = observed.Overall

	for _, s := range d.Services {
		if s.Health.None || s.Kind == spec.KindCron {
			snap.Status = state.StatusAccepted
		}
	}

	for _, r := range d.Resources {
		if r.Lifecycle == spec.LifecycleExternal || r.RuntimeRecipe != nil && len(r.RuntimeRecipe.Healthcheck) == 0 {
			snap.Status = state.StatusAccepted
		}
	}

	if err := st.SaveSnapshot(snap); err != nil {
		return err
	}

	snapshotRaw, err := json.Marshal(snap)
	if err != nil {
		return err
	}

	if err := st.WriteFile("release-"+p.Hash+".json", snapshotRaw); err != nil {
		return err
	}

	release := state.Release{ID: p.Hash[:12], PlanHash: p.Hash, AppliedAt: time.Now().UTC(), Status: snap.Status, Migrations: map[string]bool{}}
	for _, s := range d.Services {
		release.Images = append(release.Images, s.Image)
		if len(s.Migrate) > 0 {
			release.Migrations["migrate:"+s.Name] = false
		}
	}

	return st.RecordRelease(release)
}
func firstID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}

	return ids[0]
}
func resourceType(d *model.Deployment, name string) model.ResourceType {
	for _, r := range d.Resources {
		if r.Name == name {
			return r.Type
		}
	}

	return ""
}
func sortObjects(items []object) {
	slices.SortFunc(items, func(a, b object) int {
		return strings.Compare(a["kind"].(string)+"/"+a["metadata"].(map[string]any)["name"].(string), b["kind"].(string)+"/"+b["metadata"].(map[string]any)["name"].(string))
	})
}
