package managed

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"gopkg.in/yaml.v3"
)

func deployment(name string) *model.Deployment {
	digest := "sha256:" + strings.Repeat("a", 64)

	return &model.Deployment{Project: "atlas", Environment: "production", TargetName: "cloud", Overlay: model.OverlayInline,
		Target: spec.Target{Provider: name, Build: spec.Build{Source: "existing", Delivery: "registry"}, ManagedDatabases: map[string]string{"primary": "atlas-postgres", "cache": "atlas-cache"}},
		Services: []model.Service{
			{Name: "api", MainPath: "cmd/api", Kind: spec.KindWeb, Replicas: 2, Image: model.Image{Repository: "ghcr.io/acme/api", Digest: digest}, Ports: []model.Port{{Name: "http", Port: 8080, Protocol: "http", Exposure: spec.ExposurePrivate}}, Health: model.Health{Readiness: "/health"}, Bindings: []model.Binding{{Resource: "primary", Keys: map[string]string{"extensions.grove.databases[primary].dsn": "${ATLAS_PRIMARY_DSN}"}}}},
			{Name: "gateway", MainPath: "cmd/gateway", Kind: spec.KindGateway, Replicas: 1, Image: model.Image{Repository: "ghcr.io/acme/gateway", Digest: digest}, Ports: []model.Port{{Name: "http", Port: 8081, Protocol: "http", Exposure: spec.ExposurePublic}}},
			{Name: "worker", MainPath: "cmd/worker", Kind: spec.KindWorker, Replicas: 1, Image: model.Image{Repository: "ghcr.io/acme/worker", Digest: digest}, Bindings: []model.Binding{{Resource: "cache", Keys: map[string]string{"extensions.cache.url": "${ATLAS_CACHE_URL}"}}}},
		},
		Resources:   []model.Resource{{Name: "primary", Type: model.Postgres, Version: "16", Lifecycle: spec.LifecycleManaged, Secret: model.SecretRef{Name: "primary-managed", EnvVar: "ATLAS_PRIMARY_DSN"}}, {Name: "cache", Type: model.Redis, Lifecycle: spec.LifecycleManaged, Secret: model.SecretRef{Name: "cache-managed", EnvVar: "ATLAS_CACHE_URL"}}},
		Connections: []model.Connection{{From: "gateway", To: "api", Port: "http", ConfigKey: "services.api.url", EnvVar: "API_URL"}},
	}
}
func TestManagedExports(t *testing.T) {
	for _, name := range []string{"render", "digitalocean"} {
		t.Run(name, func(t *testing.T) {
			p := New(name, testdata.Root("atlas-v2"))
			d := deployment(name)

			d.Target.Region = "ams"
			if name == "render" {
				d.Target.Region = "frankfurt"
			}

			d.Services[0].Migrate = []string{"migrate", "up"}

			b, err := p.Render(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}

			path := "render.yaml"
			if name == "digitalocean" {
				path = "app.yaml"
			}

			raw := b.Files[path].Content
			if len(raw) == 0 {
				t.Fatal("missing provider spec")
			}

			var doc map[string]any
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}

			if err := ValidateSchema(name, doc); err != nil {
				t.Fatal(err)
			}

			again, err := p.Render(context.Background(), d)
			if err != nil || !reflect.DeepEqual(b.Hashes(), again.Hashes()) {
				t.Fatal("nondeterministic render", err)
			}

			want := "fromService:"
			if name == "digitalocean" {
				want = "${api.PRIVATE_URL}"
			}

			if !strings.Contains(string(raw), want) || !strings.Contains(string(raw), "FORGE_CONFIG_OVERLAY_YAML") || (!strings.Contains(string(raw), d.Services[0].Image.Repository) && name == "render") || !strings.Contains(string(raw), d.Services[0].Image.Digest) {
				t.Fatalf("missing wiring: %s", raw)
			}

			migration := doc["services"].([]any)[0].(map[string]any)["preDeployCommand"]
			if name == "digitalocean" {
				migration = doc["jobs"].([]any)[0].(map[string]any)["run_command"]
			}

			if migration != "'/app/app' 'migrate' 'up'" {
				t.Fatal("migration must invoke the service binary")
			}

			caps, _ := p.Capabilities(context.Background(), d.Target)
			if caps.Level != model.LevelValidated || caps.Observe {
				t.Fatal(caps)
			}

			if err := p.Apply(context.Background(), nil, nil, nil, nil); !errors.Is(err, provider.ErrUnsupported) {
				t.Fatal(err)
			}

			if d.Connections[0].Address != "" {
				t.Fatal("render mutated model")
			}
		})
	}
}
func digestRef(i model.Image) string { return i.Repository + "@" + i.Digest }
func TestManagedRejectsUnsupportedTraits(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		edit     func(*model.Deployment)
	}{
		{"container", "render", func(d *model.Deployment) { d.Resources[0].Lifecycle = spec.LifecycleContainer }},
		{"redis-stack", "render", func(d *model.Deployment) { d.Resources[1].Features = []string{"json", "search"} }},
		{"missing-do-cluster", "digitalocean", func(d *model.Deployment) { d.Target.ManagedDatabases = nil }},
		{"private-do-image", "digitalocean", func(d *model.Deployment) { d.Target.Build.Registry.Visibility = "private" }},
		{"private-render-pull", "render", func(d *model.Deployment) { d.Target.Build.Registry.Visibility = "private" }},
		{"git-commit", "render", func(d *model.Deployment) {
			d.Target.Build.Source = "git"
			d.Target.Build.Repo = "https://github.com/acme/atlas"
			d.Target.Build.Commit = strings.Repeat("a", 40)
		}},
		{"do-checks-trigger", "digitalocean", func(d *model.Deployment) {
			d.Target.Build.Source = "git"
			d.Target.Build.Repo = "https://github.com/acme/atlas"
			d.Target.Build.Trigger = "checksPass"
		}},
		{"excluded-dependency", "render", func(d *model.Deployment) { d.Services = d.Services[1:] }},
		{"fallback-overlay", "render", func(d *model.Deployment) { d.Overlay = model.OverlayFallback }},
		{"multiple-port", "digitalocean", func(d *model.Deployment) { d.Services[0].Ports = append(d.Services[0].Ports, model.Port{Port: 9090}) }},
		{"direct-gitops", "render", func(d *model.Deployment) { d.Target.Release.Mode = "gitops" }},
		{"render-oneoff", "render", func(d *model.Deployment) { d.Services[2].Kind = spec.KindJob }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := deployment(tc.provider)
			tc.edit(d)

			p := New(tc.provider, testdata.Root("atlas-v2"))
			if !p.Validate(context.Background(), d).HasErrors() {
				t.Fatal("invalid traits accepted")
			}

			if _, err := p.Render(context.Background(), d); err == nil {
				t.Fatal("render accepted invalid traits")
			}
		})
	}
}
func TestManagedGitBuildAndExternalSecrets(t *testing.T) {
	for _, name := range []string{"render", "digitalocean"} {
		t.Run(name, func(t *testing.T) {
			d := deployment(name)
			d.Target.Build = spec.Build{Source: "git", Delivery: "git", Repo: "https://github.com/acme/atlas", Branch: "main", Trigger: "commit"}
			d.Resources[1].Lifecycle = spec.LifecycleExternal
			d.Resources[1].Secret = model.SecretRef{Name: "cache-url", EnvVar: "ATLAS_CACHE_URL", Resolver: "env"}

			b, err := New(name, testdata.Root("atlas-v2")).Render(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(b.Files["api/Dockerfile"].Content), "/app/app") {
				t.Fatal("missing Git build Dockerfile")
			}

			if !strings.Contains(string(b.Files["HANDOFF.md"].Content), "ATLAS_CACHE_URL") {
				t.Fatal("missing provider secret handoff")
			}

			if name == "render" && !strings.Contains(string(b.Files["render.yaml"].Content), "sync: false") {
				t.Fatal("missing prompted secret")
			}

			if name == "digitalocean" && !strings.Contains(string(b.Files["app.yaml"].Content), "deploy_on_push: true") {
				t.Fatal("commit trigger missing")
			}

			if name == "digitalocean" && strings.Contains(string(b.Files["app.yaml"].Content), "ATLAS_CACHE_URL\n        value:") {
				t.Fatal("unresolved external secret literal")
			}
		})
	}
}
func TestSchemasRejectBrokenOutput(t *testing.T) {
	if err := ValidateSchema("render", map[string]any{"services": []any{map[string]any{"type": "fictional", "name": "api"}}}); err == nil {
		t.Fatal("invalid Render output passed")
	}

	if err := ValidateSchema("digitalocean", map[string]any{"name": "CAPITAL SPACE", "services": []any{map[string]any{"name": "api", "http_port": "not-a-port"}}}); err == nil {
		t.Fatal("invalid DO output passed")
	}
}
