package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"gopkg.in/yaml.v3"
)

type object = map[string]any

var variable = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`)

func namespace(d *model.Deployment) string {
	if d.Target.Namespace != "" {
		return d.Target.Namespace
	}

	return d.Project + "-" + d.Environment
}
func owner(d *model.Deployment) object {
	return object{"app.kubernetes.io/managed-by": "forge", "forge.xraph.io/project": d.Project, "forge.xraph.io/target": d.TargetName, "forge.xraph.io/environment": d.Environment}
}
func labels(d *model.Deployment, name, role string) object {
	v := owner(d)
	v["forge.xraph.io/component"] = name
	v["forge.xraph.io/role"] = role

	return v
}
func metadata(d *model.Deployment, name string) object {
	return object{"name": name, "namespace": namespace(d), "labels": owner(d)}
}
func makeObject(d *model.Deployment, api, kind, name string, body object) object {
	v := object{"apiVersion": api, "kind": kind, "metadata": metadata(d, name)}
	maps.Copy(v, body)

	return v
}
func identity(d *model.Deployment) string {
	raw := d.Project + "/" + d.TargetName + "/" + d.Environment
	h := sha256.Sum256([]byte(raw))

	return "forge-" + hex.EncodeToString(h[:])[:16]
}
func revision(d *model.Deployment) uint64 {
	if d.Revision == 0 {
		return 1
	}

	return d.Revision
}
func jobName(d *model.Deployment, name string) string {
	return name + "-r" + strconv.FormatUint(revision(d), 10)
}
func (k *Kubernetes) Render(ctx context.Context, d *model.Deployment) (*render.Bundle, error) {
	return k.render(ctx, d, true)
}

func (k *Kubernetes) render(ctx context.Context, d *model.Deployment, buildFiles bool) (*render.Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if ds := k.Validate(ctx, d); ds.HasErrors() {
		return nil, fmt.Errorf("invalid Kubernetes deployment: %v", ds)
	}

	b := render.New(d.TargetName, d.Environment)
	stages := map[string][]string{}
	add := func(stage, name string, obj object) error {
		raw, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}

		path := stage + "/" + name + ".yaml"
		b.Add(path, raw)

		stages[stage] = append(stages[stage], name+".yaml")

		return nil
	}
	ns := makeObject(d, "v1", "Namespace", namespace(d), nil)
	delete(ns["metadata"].(map[string]any), "namespace")

	if err := add("namespace", "namespace", ns); err != nil {
		return nil, err
	}

	services := map[string]model.Service{}
	for _, s := range d.Services {
		services[s.Name] = s
	}

	for i := range d.Connections {
		edge := &d.Connections[i]
		if s, ok := services[edge.To]; ok {
			p, err := namedPort(s, edge.Port)
			if err != nil {
				return nil, err
			}

			scheme := p.Protocol
			if scheme == "" || scheme == "grpc" {
				scheme = "http"
			}

			edge.Address = fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", scheme, s.Name, namespace(d), p.Port)
		}
	}

	for _, r := range d.Resources {
		if r.Lifecycle != spec.LifecycleContainer {
			continue
		}

		objects := resourceObjects(d, r)
		for _, obj := range objects {
			kind := obj["kind"].(string)
			name := obj["metadata"].(map[string]any)["name"].(string)

			stage := "resources"
			if kind == "Job" {
				stage = "init"
			}

			if err := add(stage, strings.ToLower(kind)+"-"+name, obj); err != nil {
				return nil, err
			}
		}
	}

	for _, s := range d.Services {
		if buildFiles && d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci" && s.Image.Dockerfile == "" {
			raw, err := images.Dockerfile(d, s, k.root)
			if err != nil {
				return nil, err
			}

			b.Add("images/"+s.Name+"/Dockerfile", raw)
			b.Add("images/"+s.Name+"/Dockerfile.dockerignore", images.Ignore(d))
		}

		overlay, err := serviceOverlay(d, s)
		if err != nil {
			return nil, err
		}

		hash := sha256.Sum256(overlay)
		configName := s.Name + "-config-" + hex.EncodeToString(hash[:])[:12]

		cm := makeObject(d, "v1", "ConfigMap", configName, object{"immutable": true, "data": object{"overlay.yaml": string(overlay)}})
		if err := add("config", "config-"+s.Name, cm); err != nil {
			return nil, err
		}

		sa := makeObject(d, "v1", "ServiceAccount", identity(d)+"-"+s.Name, object{"automountServiceAccountToken": s.Discovery})
		if err := add("config", "account-"+s.Name, sa); err != nil {
			return nil, err
		}

		if s.Discovery {
			for _, obj := range discoveryObjects(d, s) {
				if err := add("config", strings.ToLower(obj["kind"].(string))+"-"+s.Name, obj); err != nil {
					return nil, err
				}
			}
		}

		pod := applicationPod(d, s, configName, overlay)
		kind, api, name := "Deployment", "apps/v1", s.Name

		body := object{"spec": object{"replicas": s.Replicas, "revisionHistoryLimit": 3, "selector": object{"matchLabels": labels(d, s.Name, "app")}, "template": object{"metadata": object{"labels": labels(d, s.Name, "app")}, "spec": pod}}}
		if s.Kind == spec.KindJob {
			kind, api, name = "Job", "batch/v1", jobName(d, s.Name)
			body = jobBody(d, s.Name, pod, "app")
		}

		if s.Kind == spec.KindCron {
			kind, api = "CronJob", "batch/v1"
			body = object{"spec": object{"schedule": s.Schedule, "concurrencyPolicy": "Forbid", "successfulJobsHistoryLimit": 2, "failedJobsHistoryLimit": 3, "jobTemplate": jobBody(d, s.Name, pod, "app")}}
		}

		if err := add("applications", strings.ToLower(kind)+"-"+name, makeObject(d, api, kind, name, body)); err != nil {
			return nil, err
		}

		if len(s.Ports) > 0 {
			if err := add("applications", "service-"+s.Name, serviceObject(d, s.Name, "app", s.Ports)); err != nil {
				return nil, err
			}
		}

		if s.Replicas > 1 && (s.Kind == spec.KindWeb || s.Kind == spec.KindWorker || s.Kind == spec.KindGateway) {
			obj := makeObject(d, "policy/v1", "PodDisruptionBudget", s.Name, object{"spec": object{"maxUnavailable": 1, "selector": object{"matchLabels": labels(d, s.Name, "app")}}})
			if err := add("applications", "pdb-"+s.Name, obj); err != nil {
				return nil, err
			}
		}

		if len(s.Migrate) > 0 {
			migration := applicationPod(d, s, configName, overlay)
			container := migration["containers"].([]any)[0].(map[string]any)
			container["args"] = s.Migrate
			delete(container, "readinessProbe")
			delete(container, "livenessProbe")
			delete(container, "startupProbe")

			if err := add("migrations", s.Name, makeObject(d, "batch/v1", "Job", jobName(d, s.Name+"-migrate"), jobBody(d, s.Name, migration, "migration"))); err != nil {
				return nil, err
			}
		}
	}

	for _, route := range d.Routes {
		if route.Host == "" {
			continue
		}

		obj, err := routeObject(d, services[route.Service], route)
		if err != nil {
			return nil, err
		}

		if err := add("routes", route.Service+"-"+route.Port, obj); err != nil {
			return nil, err
		}
	}

	if d.Target.NetworkPolicy {
		for _, obj := range networkObjects(d) {
			name := obj["metadata"].(map[string]any)["name"].(string)
			if err := add("policy", name, obj); err != nil {
				return nil, err
			}
		}
	}

	stageNames := []string{}

	for stage, files := range stages {
		sort.Strings(files)

		raw, err := yaml.Marshal(object{"apiVersion": "kustomize.config.k8s.io/v1beta1", "kind": "Kustomization", "resources": files})
		if err != nil {
			return nil, err
		}

		b.Add(stage+"/kustomization.yaml", raw)
		stageNames = append(stageNames, stage)
	}

	sort.Strings(stageNames)

	raw, err := yaml.Marshal(object{"apiVersion": "kustomize.config.k8s.io/v1beta1", "kind": "Kustomization", "resources": stageNames})
	if err != nil {
		return nil, err
	}

	b.Add("kustomization.yaml", raw)

	if err := gitOpsHandoff(d, b); err != nil {
		return nil, err
	}

	return b, nil
}
func serviceOverlay(d *model.Deployment, s model.Service) ([]byte, error) {
	if s.Discovery {
		raw, err := yaml.Marshal(s.RuntimeConfig)
		if err != nil {
			return nil, err
		}

		runtime := map[string]any{}
		if err := yaml.Unmarshal(raw, &runtime); err != nil {
			return nil, err
		}

		legacy, _ := runtime["discovery"].(map[string]any)

		extensions, _ := runtime["extensions"].(map[string]any)
		if extensions == nil {
			extensions = object{}
		}

		canonical, _ := extensions["discovery"].(map[string]any)
		discovery := object{}
		mergeConfiguration(discovery, legacy)
		mergeConfiguration(discovery, canonical)
		maps.Copy(discovery, object{"enabled": true, "backend": "kubernetes", "kubernetes": object{"namespace": namespace(d), "in_cluster": true, "label_selector": fmt.Sprintf("forge.xraph.io/project=%s,forge.xraph.io/target=%s,forge.xraph.io/environment=%s", d.Project, d.TargetName, d.Environment)}})
		extensions["discovery"] = discovery
		runtime["extensions"] = extensions
		delete(runtime, "discovery")
		s.RuntimeConfig = runtime
	}

	return resolve.Overlay(d, &s)
}

func mergeConfiguration(destination, source map[string]any) {
	for key, value := range source {
		if child, ok := value.(map[string]any); ok {
			prior, _ := destination[key].(map[string]any)
			if prior == nil {
				prior = object{}
			}

			mergeConfiguration(prior, child)
			destination[key] = prior
		} else {
			destination[key] = value
		}
	}
}
func namedPort(s model.Service, name string) (model.Port, error) {
	if name == "" {
		name = "http"
	}

	for _, p := range s.Ports {
		if p.Name == name {
			return p, nil
		}
	}

	return model.Port{}, fmt.Errorf("service %s has no port %s", s.Name, name)
}
func healthPort(s model.Service) int {
	for _, p := range s.Ports {
		if p.Protocol == "http" || p.Name == "http" {
			return p.Port
		}
	}

	return 8080
}
func transport(protocol string) string {
	if protocol == "udp" {
		return "UDP"
	}

	return "TCP"
}
func serviceObject(d *model.Deployment, name, role string, ports []model.Port) object {
	rows := []any{}
	for _, p := range ports {
		rows = append(rows, object{"name": p.Name, "port": p.Port, "targetPort": p.Name, "protocol": transport(p.Protocol)})
	}

	v := makeObject(d, "v1", "Service", name, object{"spec": object{"type": "ClusterIP", "selector": labels(d, name, role), "ports": rows}})
	v["metadata"].(map[string]any)["labels"] = labels(d, name, role)

	return v
}
func environment(d *model.Deployment, values map[string]string) []any {
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	rows := []any{}
	emitted := map[string]bool{}
	active := map[string]bool{}

	var emit func(string)

	emit = func(key string) {
		if emitted[key] || active[key] {
			return
		}

		active[key] = true

		value, known := values[key]
		if !known || value == "${"+key+"}" {
			rows = append(rows, object{"name": key, "valueFrom": object{"secretKeyRef": object{"name": identity(d), "key": key}}})
		} else {
			for _, ref := range variable.FindAllStringSubmatch(value, -1) {
				emit(ref[1])
			}

			rows = append(rows, object{"name": key, "value": variable.ReplaceAllString(value, "$($1)")})
		}

		emitted[key] = true
		delete(active, key)
	}
	for _, key := range keys {
		emit(key)
	}

	return rows
}
func applicationPod(d *model.Deployment, s model.Service, configName string, overlay []byte) object {
	port := healthPort(s)
	env := map[string]string{"PORT": strconv.Itoa(port), "FORGE_HTTP_PORT": strconv.Itoa(port)}

	mount := "/etc/forge/overlay.yaml"
	if d.Overlay == model.OverlayFallback {
		mount = "/app/config.local.yaml"
	} else {
		env["FORGE_CONFIG_OVERLAY"] = mount
	}

	for _, ref := range variable.FindAllStringSubmatch(string(overlay), -1) {
		env[ref[1]] = "${" + ref[1] + "}"
	}

	for _, edge := range d.Connections {
		if edge.From == s.Name {
			env[edge.EnvVar] = edge.Address
		}
	}

	maps.Copy(env, s.Env)
	container := object{"name": s.Name, "image": images.Ref(s.Image), "imagePullPolicy": "IfNotPresent", "env": environment(d, env), "resources": resourceRequests(s), "securityContext": object{"runAsNonRoot": true, "runAsUser": 65532, "allowPrivilegeEscalation": false, "capabilities": object{"drop": []string{"ALL"}}}, "volumeMounts": []any{object{"name": "config", "mountPath": mount, "subPath": "overlay.yaml", "readOnly": true}}}

	rows := []any{}
	for _, p := range s.Ports {
		rows = append(rows, object{"name": p.Name, "containerPort": p.Port, "protocol": transport(p.Protocol)})
	}

	if len(rows) > 0 {
		container["ports"] = rows
	}

	if !s.Health.None {
		for _, probe := range []struct{ name, path string }{{"readinessProbe", s.Health.Readiness}, {"livenessProbe", s.Health.Liveness}, {"startupProbe", s.Health.Startup}} {
			if probe.path != "" {
				limit := 3
				if probe.name == "startupProbe" {
					limit = 30
				}

				container[probe.name] = object{"httpGet": object{"path": probe.path, "port": port}, "periodSeconds": 5, "timeoutSeconds": 2, "failureThreshold": limit}
			}
		}
	}

	pod := object{"serviceAccountName": identity(d) + "-" + s.Name, "automountServiceAccountToken": s.Discovery, "securityContext": object{"seccompProfile": object{"type": "RuntimeDefault"}}, "terminationGracePeriodSeconds": 30, "containers": []any{container}, "volumes": []any{object{"name": "config", "configMap": object{"name": configName}}}}
	container["env"] = append(container["env"].([]any), object{"name": "POD_IP", "valueFrom": object{"fieldRef": object{"fieldPath": "status.podIP"}}})

	if secret := d.Target.Build.Registry.PullSecret; secret != "" {
		pod["imagePullSecrets"] = []any{object{"name": secret}}
	}

	return pod
}
func resourceRequests(s model.Service) object {
	cpu, memory := s.Resources.CPU, s.Resources.Memory
	if cpu == "" {
		cpu = "100m"
	}

	if memory == "" {
		memory = "128Mi"
	}

	limitCPU, limitMemory := s.Resources.CPULimit, s.Resources.MemoryLimit
	if limitCPU == "" {
		limitCPU = "1"
	}

	if limitMemory == "" {
		limitMemory = "512Mi"
	}

	return object{"requests": object{"cpu": cpu, "memory": memory}, "limits": object{"cpu": limitCPU, "memory": limitMemory}}
}
func jobBody(d *model.Deployment, name string, pod object, role string) object {
	pod["restartPolicy"] = "Never"

	return object{"spec": object{"activeDeadlineSeconds": 600, "backoffLimit": 0, "template": object{"metadata": object{"labels": labels(d, name, role)}, "spec": pod}}}
}
func discoveryObjects(d *model.Deployment, s model.Service) []object {
	name := identity(d) + "-" + s.Name

	return []object{makeObject(d, "rbac.authorization.k8s.io/v1", "Role", name, object{"rules": []any{object{"apiGroups": []string{""}, "resources": []string{"services"}, "verbs": []string{"get", "list", "watch"}}}}), makeObject(d, "rbac.authorization.k8s.io/v1", "RoleBinding", name, object{"roleRef": object{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": name}, "subjects": []any{object{"kind": "ServiceAccount", "name": name, "namespace": namespace(d)}}})}
}
func (*Kubernetes) bundleDir(root string, d *model.Deployment) string {
	return filepath.Join(root, "deployments", d.TargetName, d.Environment)
}
