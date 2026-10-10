package hosted

import (
	"encoding/json"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"strings"
	"testing"
)

func fixture() *model.Deployment {
	return &model.Deployment{Project: "atlas", TargetName: "hosted", Environment: "production", Target: spec.Target{Provider: "hosted", SecretKeys: map[string]string{"primary-dsn": "vault-primary"}, Build: spec.Build{Source: "existing", Delivery: "registry"}}, Services: []model.Service{{Name: "api", Kind: spec.KindWeb, Replicas: 2, Image: model.Image{Repository: "ghcr.io/example/api", Digest: "sha256:" + strings.Repeat("a", 64)}, Ports: []model.Port{{Name: "http", Port: 8080, Protocol: "http"}}, Bindings: []model.Binding{{Resource: "primary", Keys: map[string]string{"extensions.grove.databases[primary].dsn": "${ATLAS_PRIMARY_DSN}"}}}}}, Resources: []model.Resource{{Name: "primary", Type: model.Postgres, Lifecycle: spec.LifecycleExternal, UsedBy: []string{"api"}, Secret: model.SecretRef{Name: "primary-dsn", EnvVar: "ATLAS_PRIMARY_DSN"}}}}
}
func TestHostedContract(t *testing.T) {
	p := New(t.TempDir())
	d := fixture()

	b, e := p.Render(t.Context(), d)
	if e != nil {
		t.Fatal(e)
	}

	var out Export
	if e = json.Unmarshal(b.Files["forge-hosted.json"].Content, &out); e != nil {
		t.Fatal(e)
	}

	if len(out.Workloads) != 1 || out.Workloads[0].Services[0].Resources.Replicas != 2 || out.Workloads[0].SecretBindings[0].EnvKey != "ATLAS_PRIMARY_DSN" || out.Workloads[0].SecretBindings[0].Ref.Key != "vault-primary" {
		t.Fatal(out)
	}

	if strings.Contains(string(b.Files["forge-hosted.json"].Content), "tenant_id") {
		t.Fatal("laptop assigned authority")
	}
}
func TestHostedRejectsUnmappedTraits(t *testing.T) {
	for _, change := range []func(*model.Deployment){func(d *model.Deployment) { d.Target.SecretKeys = nil }, func(d *model.Deployment) { d.Services[0].Image.Digest = "" }, func(d *model.Deployment) { d.Resources[0].Lifecycle = spec.LifecycleContainer }, func(d *model.Deployment) { d.Services[0].Discovery = true }, func(d *model.Deployment) { d.Services[0].Kind = spec.KindJob }, func(d *model.Deployment) {
		d.Services[0].ConfigFiles = []model.ConfigFile{{Name: "arbitrary", Path: "/tmp/config"}}
	}, func(d *model.Deployment) { d.Migrations = []model.Migration{{Service: "api"}} }} {
		d := fixture()
		change(d)

		if !New(t.TempDir()).Validate(t.Context(), d).HasErrors() {
			t.Fatal("unsupported trait accepted", d)
		}
	}
}

func TestHostedNamedHTTPPort(t *testing.T) {
	d := fixture()
	d.Services[0].Ports = []model.Port{{Name: "api", Port: 9090, Protocol: "http"}}
	d.Services[0].Health.Readiness = "/_/health/ready"

	b, e := New(t.TempDir()).Render(t.Context(), d)
	if e != nil {
		t.Fatal(e)
	}

	var out Export
	if e = json.Unmarshal(b.Files["forge-hosted.json"].Content, &out); e != nil {
		t.Fatal(e)
	}

	if out.Workloads[0].Services[0].Env["FORGE_HTTP_PORT"] != "9090" || out.Workloads[0].Services[0].HealthCheck.Port != 9090 {
		t.Fatal("HTTP port guessed")
	}
}

func TestHostedComputeUsesCtrlplaneBinaryMemory(t *testing.T) {
	s := fixture().Services[0]
	s.Resources = spec.ResourceSpec{CPU: "0.5", Memory: "128Mi"}

	r, e := compute(s)
	if e != nil || r.CPUMillis != 500 || r.MemoryMB != 128 {
		t.Fatal(r, e)
	}
}

func TestHostedRejectsUnmappedImagePlatforms(t *testing.T) {
	d := fixture()

	d.Target.Build.Platforms = []string{"linux/arm64"}
	if _, err := New(t.TempDir()).Render(t.Context(), d); err == nil {
		t.Fatal("architecture request dropped")
	}
}

func TestHostedRejectsUnmappedPublicRouting(t *testing.T) {
	for _, change := range []func(*model.Deployment){
		func(d *model.Deployment) { d.Services[0].Ports[0].Exposure = spec.ExposurePublic },
		func(d *model.Deployment) {
			d.Routes = []model.Route{{Service: "api", Port: "http", Host: "api.example.com"}}
		},
		func(d *model.Deployment) {
			d.Routes = []model.Route{{Service: "api", Port: "http", TLS: "issuer-prod"}}
		},
		func(d *model.Deployment) { d.Routes = []model.Route{{Service: "api", Port: "http", Path: "/billing"}} },
	} {
		d := fixture()
		change(d)

		if _, err := New(t.TempDir()).Render(t.Context(), d); err == nil {
			t.Fatal("public routing request dropped")
		}
	}
}
