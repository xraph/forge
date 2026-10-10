package kubernetes

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"gopkg.in/yaml.v3"
)

// policyGraph combines the approved selection with the frozen running workloads.
// Retained callers keep their bindings and edges until their own approved rollout.
func (k *Kubernetes) policyGraph(d *model.Deployment, snap state.Snapshot) (*model.Deployment, error) {
	graph, err := cloneDeployment(d)
	if err != nil {
		return nil, err
	}

	selected := map[string]bool{}
	resources := map[string]bool{}

	for _, s := range graph.Services {
		selected[s.Name] = true
	}

	for _, r := range graph.Resources {
		resources[r.Name] = true
	}

	names := []string{}

	for name := range snap.Workloads {
		if !selected[name] {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	for _, name := range names {
		prior, err := k.loadPlan(snap.Workloads[name].PlanHash)
		if err != nil {
			return nil, err
		}

		found := false
		needed := map[string]bool{}

		for _, svc := range prior.Deployment.Services {
			if svc.Name == name {
				graph.Services = append(graph.Services, svc)
				found = true

				for _, b := range svc.Bindings {
					needed[b.Resource] = true
				}
			}
		}

		if !found {
			return nil, errors.New("retained workload is missing its frozen service graph")
		}

		for _, r := range prior.Deployment.Resources {
			if needed[r.Name] && !resources[r.Name] {
				graph.Resources = append(graph.Resources, r)
				resources[r.Name] = true
			}
		}

		for _, edge := range prior.Deployment.Connections {
			if edge.From == name {
				graph.Connections = append(graph.Connections, edge)
			}
		}

		for _, route := range prior.Deployment.Routes {
			if route.Service == name {
				graph.Routes = append(graph.Routes, route)
			}
		}
	}

	return graph, nil
}

func (k *Kubernetes) runtimeBundle(ctx context.Context, d *model.Deployment, snap state.Snapshot) (*render.Bundle, error) {
	b, err := k.render(ctx, d, false)
	if err != nil || !d.Target.NetworkPolicy {
		return b, err
	}

	graph, err := k.policyGraph(d, snap)
	if err != nil {
		return nil, err
	}

	if ds := k.Validate(ctx, graph); ds.HasErrors() {
		return nil, errors.New("running service graph is incompatible with the approved network policy")
	}

	for path := range b.Files {
		if strings.HasPrefix(path, "policy/") {
			delete(b.Files, path)
			delete(b.Manifest.Files, path)
		}
	}

	files := []string{}

	for _, obj := range networkObjects(graph) {
		name := obj["metadata"].(map[string]any)["name"].(string)

		raw, err := yaml.Marshal(obj)
		if err != nil {
			return nil, err
		}

		b.Add("policy/"+name+".yaml", raw)
		files = append(files, name+".yaml")
	}

	raw, err := yaml.Marshal(object{"apiVersion": "kustomize.config.k8s.io/v1beta1", "kind": "Kustomization", "resources": files})
	if err != nil {
		return nil, err
	}

	b.Add("policy/kustomization.yaml", raw)

	return b, nil
}

// workloadKey includes every recorded revision, including an interrupted attempt.
func workloadKey(d *model.Deployment, name, key string) bool {
	kind, objectName, _ := strings.Cut(key, "/")
	switch kind {
	case "ConfigMap":
		suffix, ok := strings.CutPrefix(objectName, name+"-config-")
		_, err := hex.DecodeString(suffix)

		return ok && len(suffix) == 12 && err == nil
	case "ServiceAccount", "Role", "RoleBinding":
		return objectName == identity(d)+"-"+name
	case "Job":
		for _, prefix := range []string{name + "-r", name + "-migrate-r"} {
			suffix, ok := strings.CutPrefix(objectName, prefix)
			if _, err := strconv.ParseUint(suffix, 10, 64); ok && err == nil {
				return true
			}
		}

		return false
	case "Ingress", "HTTPRoute":
		for _, route := range d.Routes {
			if route.Service == name && objectName == name+"-"+route.Port {
				return true
			}
		}

		return false
	case "Deployment", "CronJob", "Service", "PodDisruptionBudget", "NetworkPolicy":
		return objectName == name
	}

	return false
}
func workloadObjects(d *model.Deployment, b *render.Bundle, stage, name string) ([]object, error) {
	items, err := stageObjects(b, stage)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(items, func(obj object) bool {
		return !workloadKey(d, name, obj["kind"].(string)+"/"+obj["metadata"].(map[string]any)["name"].(string))
	}), nil
}
