package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"gopkg.in/yaml.v3"
)

var clusterKinds = []string{"deployments", "statefulsets", "services", "configmaps", "secrets", "serviceaccounts", "jobs", "cronjobs", "roles", "rolebindings", "poddisruptionbudgets", "ingresses", "networkpolicies", "persistentvolumeclaims"}

func clusterArgs(d *model.Deployment, rest ...string) []string {
	return append([]string{"--context", d.Target.Context, "--namespace", namespace(d)}, rest...)
}
func (k *Kubernetes) run(ctx context.Context, d *model.Deployment, stdin io.Reader, rest ...string) (execx.Result, error) {
	return k.runner.Run(ctx, execx.Command{Name: "kubectl", Args: clusterArgs(d, rest...), Dir: k.root, Stdin: stdin})
}
func selector(d *model.Deployment) string {
	labels := owner(d)

	keys := []string{}
	for key, value := range labels {
		keys = append(keys, key+"="+value.(string))
	}

	sort.Strings(keys)

	return strings.Join(keys, ",")
}
func (k *Kubernetes) clusterObjects(ctx context.Context, d *model.Deployment, ownedOnly bool) (map[string]object, error) {
	kinds := append([]string{}, clusterKinds...)
	if d.Target.GatewayAPI {
		kinds = append(kinds, "httproutes")
	}

	args := []string{"get", strings.Join(kinds, ","), "-o", "json"}
	if ownedOnly {
		args = append(args, "--selector", selector(d))
	}

	response, err := k.run(ctx, d, nil, args...)
	if err != nil {
		return nil, errors.New("cannot read namespaced Kubernetes objects; check context and read permissions")
	}

	var list struct {
		Items []object `json:"items"`
	}
	if err := json.Unmarshal([]byte(response.Stdout), &list); err != nil {
		return nil, errors.New("cluster returned invalid object status")
	}

	out := map[string]object{}

	for _, obj := range list.Items {
		kind, _ := obj["kind"].(string)
		meta, _ := obj["metadata"].(map[string]any)

		name, _ := meta["name"].(string)
		if kind == "" || name == "" {
			return nil, errors.New("cluster returned an object without identity")
		}

		if ownedOnly && !owned(d, obj) {
			continue
		}

		out[kind+"/"+name] = obj
	}

	return out, nil
}
func owned(d *model.Deployment, obj object) bool {
	meta, _ := obj["metadata"].(map[string]any)

	labels, _ := meta["labels"].(map[string]any)
	for key, value := range owner(d) {
		if labels[key] != value {
			return false
		}
	}

	return meta["namespace"] == namespace(d)
}
func (k *Kubernetes) SnapshotIDs(ctx context.Context, d *model.Deployment) (map[string][]string, error) {
	objects, err := k.clusterObjects(ctx, d, true)
	if err != nil {
		return nil, err
	}

	return objectIDs(objects), nil
}
func objectIDs(objects map[string]object) map[string][]string {
	ids := map[string][]string{}

	for key, obj := range objects {
		meta, _ := obj["metadata"].(map[string]any)

		uid, _ := meta["uid"].(string)
		if uid != "" {
			ids[key] = []string{uid}
		}
	}

	return ids
}
func bundleObjects(b *render.Bundle) (map[string]object, error) {
	objects := map[string]object{}

	for _, file := range b.Sorted() {
		if !strings.HasSuffix(file.Path, ".yaml") || strings.HasSuffix(file.Path, "kustomization.yaml") {
			continue
		}

		var obj object
		if err := yaml.Unmarshal(file.Content, &obj); err != nil {
			return nil, err
		}

		kind, _ := obj["kind"].(string)
		meta, _ := obj["metadata"].(map[string]any)

		name, _ := meta["name"].(string)
		if kind != "" && name != "" {
			key := kind + "/" + name
			if _, exists := objects[key]; exists {
				return nil, fmt.Errorf("duplicate rendered Kubernetes object %s", key)
			}

			objects[key] = obj
		}
	}

	return objects, nil
}
func stageObjects(b *render.Bundle, stage string) ([]object, error) {
	objects := []object{}

	for _, file := range b.Sorted() {
		if !strings.HasPrefix(file.Path, stage+"/") || !strings.HasSuffix(file.Path, ".yaml") || strings.HasSuffix(file.Path, "kustomization.yaml") {
			continue
		}

		var obj object
		if err := yaml.Unmarshal(file.Content, &obj); err != nil {
			return nil, err
		}

		objects = append(objects, obj)
	}

	return objects, nil
}
func (k *Kubernetes) applyObjects(ctx context.Context, d *model.Deployment, objects []object, dry bool) error {
	if len(objects) == 0 {
		return nil
	}

	raw, err := json.Marshal(object{"apiVersion": "v1", "kind": "List", "items": objects})
	if err != nil {
		return err
	}

	args := []string{"apply", "--server-side", "--field-manager=forge-deploy"}
	if dry {
		args = append(args, "--dry-run=server")
	}

	args = append(args, "-f", "-")
	if _, err := k.run(ctx, d, strings.NewReader(string(raw)), args...); err != nil {
		return errors.New("Kubernetes manifest rejected; inspect cluster permissions, schema and field ownership")
	}

	return nil
}
func timeout(ctx context.Context) string {
	if deadline, ok := ctx.Deadline(); ok {
		return strconvDuration(time.Until(deadline))
	}

	return "600s"
}
func strconvDuration(remaining time.Duration) string {
	if remaining < time.Second {
		return "1s"
	}

	return fmt.Sprintf("%ds", int64(remaining.Seconds()))
}
func (k *Kubernetes) wait(ctx context.Context, d *model.Deployment, kind, name string) error {
	args := []string{"rollout", "status", strings.ToLower(kind) + "/" + name, "--timeout=" + timeout(ctx)}
	if kind == "Job" {
		args = []string{"wait", "--for=condition=complete", "job/" + name, "--timeout=" + timeout(ctx)}
	}

	_, err := k.run(ctx, d, nil, args...)
	if err != nil {
		return errors.New("Kubernetes workload did not become ready before the deadline")
	}

	return nil
}
func cloneDeployment(d *model.Deployment) (*model.Deployment, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}

	var clone model.Deployment
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}

	return &clone, nil
}
