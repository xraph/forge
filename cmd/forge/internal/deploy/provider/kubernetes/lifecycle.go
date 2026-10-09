package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func (k *Kubernetes) Rollback(ctx context.Context, _ provider.EnvRef, st *state.Store, releaseID string) error {
	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	var release state.Release

	found := false

	for _, r := range snap.Releases {
		if r.ID == releaseID {
			release = r
			found = true
		}
	}

	if !found {
		return errors.New("release is unavailable")
	}

	for _, r := range snap.Releases {
		if r.AppliedAt.After(release.AppliedAt) {
			for _, reversible := range r.Migrations {
				if !reversible {
					return errors.New("rollback blocked by an intervening irreversible migration")
				}
			}
		}
	}

	events, err := st.Journal().Events()
	if err != nil {
		return err
	}

	for _, event := range events {
		if event.Time.After(release.AppliedAt) && strings.HasPrefix(event.Op, "migrate:") && (event.Status == state.StatusApplying || event.Status == state.StatusAccepted) {
			return errors.New("rollback blocked by an intervening or interrupted migration")
		}
	}

	raw, err := st.ReadFile("release-" + release.PlanHash + ".json")
	if err != nil {
		return errors.New("complete workload snapshot for this release is unavailable")
	}

	var restored state.Snapshot
	if err := json.Unmarshal(raw, &restored); err != nil {
		return err
	}

	p, err := k.loadPlan(release.PlanHash)
	if err != nil {
		return err
	}

	live, err := k.clusterObjects(ctx, p.Deployment, false)
	if err != nil {
		return err
	}

	for key, ids := range snap.Identities {
		if obj, exists := live[key]; exists && (!owned(p.Deployment, obj) || firstID(ids) != obj["metadata"].(map[string]any)["uid"]) {
			return errors.New("recorded object identity changed before rollback")
		}
	}

	keys := []string{}
	for name := range restored.Workloads {
		keys = append(keys, name)
	}

	slices.Sort(keys)

	prepared := map[string]*model.Deployment{}
	bundles := map[string]*render.Bundle{}

	for _, name := range keys {
		workload := restored.Workloads[name]

		prior, err := k.loadPlan(workload.PlanHash)
		if err != nil {
			return err
		}

		d, err := cloneDeployment(prior.Deployment)
		if err != nil {
			return err
		}

		d.Services = slices.DeleteFunc(d.Services, func(svc model.Service) bool { return svc.Name != name })
		if len(d.Services) != 1 {
			return errors.New("saved workload is unavailable")
		}

		d.Services[0].Image = workload.Image
		if err := images.Verify(ctx, k.runner, k.root, d, workload.Image); err != nil {
			return errors.New("rollback image is unavailable; restore its immutable artifact")
		}

		bundle, err := k.Render(ctx, d)
		if err != nil {
			return err
		}

		for _, stage := range []string{"config", "applications"} {
			objects, err := stageObjects(bundle, stage)
			if err != nil {
				return err
			}

			for _, obj := range objects {
				key := obj["kind"].(string) + "/" + obj["metadata"].(map[string]any)["name"].(string)
				if existing, exists := live[key]; exists && !owned(d, existing) {
					return errors.New("rollback would overwrite an unowned object")
				}
			}

			if err := k.applyObjects(ctx, d, objects, true); err != nil {
				return err
			}
		}

		prepared[name], bundles[name] = d, bundle
	}

	fail := func(name string, cause error) error {
		snap.Status = state.StatusPartial
		snap.FailedOperation = "rollback:" + name

		return errors.Join(cause, st.SaveSnapshot(snap), st.Journal().Record(state.Event{Time: time.Now().UTC(), Op: snap.FailedOperation, Status: state.StatusFailed, Message: "Rollback interrupted; completed work retained"}))
	}
	for _, name := range keys {
		d, bundle := prepared[name], bundles[name]

		workload := restored.Workloads[name]
		if workload.Image.Digest == "" {
			env := []string{}
			if d.Target.DockerContext != "" {
				env = append(env, "DOCKER_CONTEXT="+d.Target.DockerContext)
			}

			if _, err := k.runner.Run(ctx, execx.Command{Name: "kind", Args: []string{"load", "docker-image", images.Ref(workload.Image), "--name", d.Target.LocalCluster}, Env: env, Dir: k.root}); err != nil {
				return fail(name, errors.New("cannot deliver immutable rollback image to kind"))
			}
		}

		for _, stage := range []string{"config", "applications"} {
			objects, err := stageObjects(bundle, stage)
			if err != nil {
				return err
			}

			if err := k.applyObjects(ctx, d, objects, false); err != nil {
				return fail(name, err)
			}
		}

		svc := d.Services[0]
		if svc.Kind != spec.KindCron {
			kind, objectName := "Deployment", name
			if svc.Kind == spec.KindJob {
				kind, objectName = "Job", jobName(d, name)
			}

			if err := k.wait(ctx, d, kind, objectName); err != nil {
				return fail(name, err)
			}
		}

		snap.Workloads[name] = workload

		snap.Identities, err = k.SnapshotIDs(ctx, d)
		if err != nil {
			return fail(name, err)
		}

		if err := st.SaveSnapshot(snap); err != nil {
			return fail(name, err)
		}

		if err := st.Journal().Record(state.Event{Time: time.Now().UTC(), Op: "rollback:" + name, Status: state.StatusAccepted, Message: "Recorded immutable workload restored"}); err != nil {
			return err
		}
	}

	snap.ActivePlanHash = release.PlanHash
	snap.Revision++
	snap.FailedOperation = ""
	snap.Status = state.StatusApplying

	snap.Identities, err = k.SnapshotIDs(ctx, p.Deployment)
	if err != nil {
		return err
	}

	if err := st.SaveSnapshot(snap); err != nil {
		return err
	}

	observed, err := k.Observe(ctx, provider.EnvRef{}, st)
	if err != nil {
		return err
	}

	snap.Status = observed.Overall

	return st.SaveSnapshot(snap)
}
func (k *Kubernetes) deleteObject(ctx context.Context, d *model.Deployment, obj object) error {
	plural := map[string]string{"Deployment": "deployments", "StatefulSet": "statefulsets", "Service": "services", "ConfigMap": "configmaps", "Secret": "secrets", "ServiceAccount": "serviceaccounts", "Job": "jobs", "CronJob": "cronjobs", "Role": "roles", "RoleBinding": "rolebindings", "PodDisruptionBudget": "poddisruptionbudgets", "Ingress": "ingresses", "NetworkPolicy": "networkpolicies", "HTTPRoute": "httproutes", "PersistentVolumeClaim": "persistentvolumeclaims"}[obj["kind"].(string)]
	if plural == "" || !owned(d, obj) {
		return errors.New("refusing to delete an unowned or unsupported Kubernetes object")
	}

	meta := obj["metadata"].(map[string]any)

	uid, _ := meta["uid"].(string)
	if uid == "" {
		return errors.New("recorded object has no UID")
	}

	api := obj["apiVersion"].(string)

	prefix := "/apis/" + api
	if api == "v1" {
		prefix = "/api/v1"
	}

	path := prefix + "/namespaces/" + namespace(d) + "/" + plural + "/" + meta["name"].(string)

	raw, err := json.Marshal(object{"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Foreground", "preconditions": object{"uid": uid}})
	if err != nil {
		return err
	}

	if _, err := k.run(ctx, d, strings.NewReader(string(raw)), "delete", "--raw", path, "-f", "-"); err != nil {
		return errors.New("Kubernetes deletion rejected; ownership or identity may have changed")
	}

	if _, err := k.run(ctx, d, nil, "wait", "--for=delete", strings.ToLower(obj["kind"].(string))+"/"+meta["name"].(string), "--timeout="+timeout(ctx)); err != nil {
		return errors.New("Kubernetes object has not finished deleting")
	}

	return nil
}
func (k *Kubernetes) Destroy(ctx context.Context, p *plan.Plan, st *state.Store, opts provider.DestroyOptions) error {
	snap, err := st.Snapshot()
	if err != nil {
		return err
	}

	live, err := k.clusterObjects(ctx, p.Deployment, false)
	if err != nil {
		return err
	}

	selected := map[string]bool{}
	for _, s := range p.Deployment.Services {
		selected[s.Name] = true
	}

	needed := map[string]bool{}

	for name, w := range snap.Workloads {
		if !selected[name] {
			for _, r := range w.Resources {
				needed[r] = true
			}
		}
	}

	candidates := map[string]bool{}

	for name := range selected {
		w, exists := snap.Workloads[name]
		if !exists {
			continue
		}

		prior, err := k.loadPlan(w.PlanHash)
		if err != nil {
			return err
		}

		d, err := cloneDeployment(prior.Deployment)
		if err != nil {
			return err
		}

		d.Services = slices.DeleteFunc(d.Services, func(svc model.Service) bool { return svc.Name != name })
		d.Resources = nil
		d.Connections = nil
		d.Routes = slices.DeleteFunc(d.Routes, func(r model.Route) bool { return r.Service != name })

		bundle, err := k.Render(ctx, d)
		if err != nil {
			return err
		}

		objects, err := bundleObjects(bundle)
		if err != nil {
			return err
		}

		for key := range objects {
			if !strings.HasPrefix(key, "Namespace/") {
				candidates[key] = true
			}
		}
	}

	resources := map[string]bool{}

	for _, r := range p.Deployment.Resources {
		if !needed[r.Name] {
			resources[r.Name] = true
		}
	}

	for key, obj := range live {
		meta := obj["metadata"].(map[string]any)
		labels, _ := meta["labels"].(map[string]any)
		component, _ := labels["forge.xraph.io/component"].(string)

		kind := obj["kind"].(string)
		for name := range resources {
			if key == "StatefulSet/"+name || key == "Service/"+name || key == "NetworkPolicy/"+name || kind == "Job" && strings.HasPrefix(key, "Job/"+name+"-init-r") {
				candidates[key] = true
			}
		}

		if resources[component] && (kind == "StatefulSet" || kind == "Service" || kind == "Job" || kind == "NetworkPolicy" || kind == "PersistentVolumeClaim" && opts.DeleteData) {
			candidates[key] = true
		}
	}

	keys := []string{}

	for key := range candidates {
		if _, exists := live[key]; exists {
			keys = append(keys, key)
		}
	}

	slices.SortFunc(keys, func(a, b string) int {
		if strings.HasPrefix(a, "PersistentVolumeClaim/") != strings.HasPrefix(b, "PersistentVolumeClaim/") {
			if strings.HasPrefix(a, "PersistentVolumeClaim/") {
				return 1
			}

			return -1
		}

		return strings.Compare(a, b)
	})

	for _, key := range keys {
		obj := live[key]

		uid, _ := obj["metadata"].(map[string]any)["uid"].(string)
		if !owned(p.Deployment, obj) || firstID(snap.Identities[key]) != uid {
			return fmt.Errorf("recorded identity changed for %s", key)
		}
	}

	for _, key := range keys {
		if err := k.deleteObject(ctx, p.Deployment, live[key]); err != nil {
			snap.Status = state.StatusPartial
			snap.FailedOperation = "destroy:" + key

			return errors.Join(err, st.SaveSnapshot(snap))
		}

		delete(snap.Identities, key)

		if err := st.Journal().Record(state.Event{Time: time.Now().UTC(), Op: "destroy:" + key, Status: state.StatusAccepted, Message: "Recorded owned object removed; namespace retained"}); err != nil {
			return err
		}

		if err := st.SaveSnapshot(snap); err != nil {
			return err
		}
	}

	for name := range selected {
		delete(snap.Workloads, name)
	}

	for name := range resources {
		delete(snap.Resources, name)
	}

	snap.Status = state.StatusAccepted
	snap.Revision++
	snap.FailedOperation = ""

	return st.SaveSnapshot(snap)
}
