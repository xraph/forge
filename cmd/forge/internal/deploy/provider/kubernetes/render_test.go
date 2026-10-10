package kubernetes

import (
	"context"
	"encoding/json"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"gopkg.in/yaml.v3"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Kubernetes, *model.Deployment) {
	t.Helper()
	root := testdata.Copy(t, "atlas-v2")

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	doc, ds, err := spec.Parse(filepath.Join(root, ".forge.yml"))
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}

	caps, _ := New(nil, root).Capabilities(context.Background(), spec.Target{})

	d, ds, err := resolve.Resolve(context.Background(), resolve.Input{Config: cfg, Doc: doc, Catalog: catalog.Embedded(), Target: "local", Environment: "dev", Caps: caps})
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}

	d.Target.Provider = "kubernetes"
	d.Target.Context = "test"
	d.Target.Namespace = "atlas-dev"

	return New(nil, root), d
}
func objects(t *testing.T, b *render.Bundle) map[string]map[string]any {
	t.Helper()

	out := map[string]map[string]any{}

	for _, f := range b.Sorted() {
		if !strings.HasSuffix(f.Path, ".yaml") || strings.HasSuffix(f.Path, "kustomization.yaml") {
			continue
		}

		var obj map[string]any
		if err := yaml.Unmarshal(f.Content, &obj); err != nil {
			t.Fatal(err)
		}

		kind, _ := obj["kind"].(string)
		if kind == "" {
			continue
		}

		meta := obj["metadata"].(map[string]any)
		out[kind+"/"+meta["name"].(string)] = obj
	}

	return out
}
func textObject(t *testing.T, o map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}
func TestKubernetesDataAndApplicationObjects(t *testing.T) {
	k, d := fixture(t)
	d.Services[0].Replicas = 2
	d.Services[0].Discovery = true

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	objs := objects(t, b)

	for _, kind := range []string{"Namespace", "ServiceAccount", "StatefulSet", "Service", "ConfigMap", "Deployment", "Job", "PodDisruptionBudget", "Role", "RoleBinding"} {
		found := false

		for key := range objs {
			if strings.HasPrefix(key, kind+"/") {
				found = true
			}
		}

		if !found {
			t.Errorf("missing %s", kind)
		}
	}

	for _, o := range objs {
		txt := textObject(t, o)
		if o["kind"] == "StatefulSet" && !strings.Contains(txt, `"whenDeleted":"Retain"`) {
			t.Fatal("data retention absent", txt)
		}

		if o["kind"] == "Deployment" && !strings.Contains(txt, `"runAsNonRoot":true`) {
			t.Fatal("runtime runs as root", txt)
		}

		if strings.Contains(txt, "literal-password") {
			t.Fatal("credential leaked")
		}
	}

	api := textObject(t, objs["Deployment/api"])
	if !strings.Contains(api, "secretKeyRef") || !strings.Contains(api, "readinessProbe") {
		t.Fatal("missing credentials/probe", api)
	}

	worker := textObject(t, objs["Deployment/worker"])
	if !strings.Contains(worker, `"port":8080`) {
		t.Fatal("worker health has no port", worker)
	}
}
func TestKubernetesNamedConnectionsAndPublicRoutes(t *testing.T) {
	k, d := fixture(t)
	for i := range d.Services {
		if d.Services[i].Name == "api" {
			d.Services[i].Ports = append(d.Services[i].Ports, model.Port{Name: "events", Port: 9090, Protocol: "tcp", Exposure: spec.ExposurePrivate})
		}
	}

	d.Connections = []model.Connection{{From: "gateway", To: "api", Port: "events", ConfigKey: "services.api.url", EnvVar: "API_URL"}}
	d.Routes = []model.Route{{Service: "gateway", Port: "http", Host: "api.example.test", Path: "/", TLS: "tls-secret"}}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	if d.Connections[0].Address != "tcp://api.atlas-dev.svc.cluster.local:9090" {
		t.Fatal(d.Connections[0].Address)
	}

	objs := objects(t, b)
	if _, ok := objs["Ingress/gateway-http"]; !ok {
		t.Fatal("no explicit HTTP route")
	}

	d.Routes[0].Service = "api"

	d.Routes[0].Port = "events"
	if _, err := k.Render(context.Background(), d); err == nil {
		t.Fatal("private TCP promoted to Ingress")
	}
}
func TestKubernetesCollisionsFailBeforeRendering(t *testing.T) {
	k, d := fixture(t)

	d.Services[0].Name = d.Resources[0].Name
	if _, err := k.Render(context.Background(), d); err == nil {
		t.Fatal("shared Service name collision accepted")
	}
}
func TestKubernetesJobsAndCronPreserveCommandsAndSchedule(t *testing.T) {
	k, d := fixture(t)
	s := d.Services[0]
	s.Name = "cleanup"
	s.Kind = spec.KindCron
	s.Ports = nil
	s.Schedule = "*/5 * * * *"
	s.Migrate = nil
	d.Services = append(d.Services, s)
	s.Name = "import"
	s.Kind = spec.KindJob
	d.Services = append(d.Services, s)

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	objs := objects(t, b)
	if !strings.Contains(textObject(t, objs["CronJob/cleanup"]), `"schedule":"*/5 * * * *"`) {
		t.Fatal("cron schedule lost")
	}

	found := false

	for key, o := range objs {
		if strings.HasPrefix(key, "Job/import-") {
			found = true

			if !strings.Contains(textObject(t, o), "activeDeadlineSeconds") {
				t.Fatal("job unbounded")
			}
		}
	}

	if !found {
		t.Fatal("job absent")
	}
}
func TestKubernetesStableKustomizeBundle(t *testing.T) {
	k, d := fixture(t)

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	again, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	if !maps.Equal(b.Hashes(), again.Hashes()) {
		t.Fatal("unstable output")
	}

	if _, ok := b.Files["kustomization.yaml"]; !ok {
		t.Fatal("no Kustomize entry point")
	}

	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl unavailable")
	}

	dir := t.TempDir()
	if _, err := render.Write(dir, b, render.WriteOptions{}); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(context.Background(), "kubectl", "kustomize", dir)

	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(raw))
	}

	golden := filepath.Join("testdata", "atlas-dev.yaml")
	if os.Getenv("FORGE_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(golden, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}

	if string(raw) != string(want) {
		t.Fatal("Kustomize golden differs")
	}
}

func TestKubernetesEnvironmentReferencesPrecedeUseAndRemainUnique(t *testing.T) {
	k, d := fixture(t)
	d.Services[0].Env = map[string]string{"A_URL": "https://${Z_HOST}/${Z_HOST}", "Z_HOST": "api.example.test"}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	obj := objects(t, b)["Deployment/api"]
	env := obj["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)["env"].([]any)
	seen := map[string]bool{}

	for _, row := range env {
		v := row.(map[string]any)

		name := v["name"].(string)
		if seen[name] {
			t.Fatal("duplicate environment variable", name)
		}

		if name == "A_URL" && !seen["Z_HOST"] {
			t.Fatal("environment expansion precedes dependency")
		}

		seen[name] = true
	}
}
func TestKubernetesDiscoveryRetainsUserConfiguration(t *testing.T) {
	k, d := fixture(t)
	d.Services[0].Discovery = true
	d.Services[0].RuntimeConfig = map[string]any{"discovery": map[string]any{"farp": map[string]any{"enabled": true, "auto_register": false}}}

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	found := false

	for key, o := range objects(t, b) {
		if strings.HasPrefix(key, "ConfigMap/api-config-") {
			text := textObject(t, o)
			if !strings.Contains(text, "auto_register: false") {
				t.Fatal("discovery preferences lost", text)
			}

			found = true
		}
	}

	if !found {
		t.Fatal("overlay absent")
	}
}
func TestKubernetesNetworkPoliciesFollowDeclaredEdges(t *testing.T) {
	k, d := fixture(t)
	d.Target.NetworkPolicy = true

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	objs := objects(t, b)

	api := textObject(t, objs["NetworkPolicy/api"])
	if !strings.Contains(api, "gateway") || !strings.Contains(api, "kube-dns") {
		t.Fatal("call/DNS missing", api)
	}

	cache := textObject(t, objs["NetworkPolicy/cache"])
	if !strings.Contains(cache, "api") || !strings.Contains(cache, "worker") || strings.Contains(cache, "gateway") {
		t.Fatal("resource policy ignores bindings", cache)
	}

	d.Resources[0].Lifecycle = spec.LifecycleExternal
	if _, err := k.Render(context.Background(), d); err == nil {
		t.Fatal("external access guessed without CIDRs")
	}
}
func TestKubernetesRevisionNamesJobsAndKeepsPriorConfig(t *testing.T) {
	k, d := fixture(t)
	d.Revision = 12

	b, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := objects(t, b)["Job/api-migrate-r12"]; !ok {
		t.Fatal("migration name lacks approved revision")
	}

	d.Revision = 13

	next, err := k.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := objects(t, next)["Job/api-migrate-r13"]; !ok {
		t.Fatal("prior Job reused")
	}
}

func TestKubernetesReplicaIdentity(t *testing.T) {
	k, d := fixture(t)

	b, e := k.Render(t.Context(), d)
	if e != nil {
		t.Fatal(e)
	}

	found := false

	for _, f := range b.Files {
		if strings.Contains(string(f.Content), "name: FORGE_INSTANCE_ID") && strings.Contains(string(f.Content), "fieldPath: metadata.uid") {
			found = true
		}
	}

	if !found {
		t.Fatal("missing replica identity")
	}

	d.Services[0].Env = map[string]string{"FORGE_SERVICE_ID": "wrong"}
	if !k.Validate(t.Context(), d).HasErrors() {
		t.Fatal("logical identity override accepted")
	}
}
