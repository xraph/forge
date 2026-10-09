package resolve

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

var update = flag.Bool("update", false, "rewrite golden files")

func composeCaps() model.Capabilities {
	return model.Capabilities{Level: model.LevelApply, FileMounts: true,
		Resources: map[model.ResourceType][]model.Lifecycle{
			model.Postgres:      {spec.LifecycleContainer, spec.LifecycleExternal},
			model.Redis:         {spec.LifecycleContainer, spec.LifecycleExternal},
			model.ObjectStorage: {spec.LifecycleContainer, spec.LifecycleExternal},
		}}
}

func input(t *testing.T, fixture, target, env string) Input {
	t.Helper()

	root := testdata.Root(fixture)

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	path, _, _ := spec.Locate(root)

	doc, diags, err := spec.Parse(path)
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	res, _ := secrets.New(spec.Secrets{Resolver: "env"}, root, nil, spec.Target{}, "atlas")

	return Input{Config: cfg, Doc: doc, Catalog: catalog.Embedded(), Target: target, Environment: env, Caps: composeCaps(), Secrets: res}
}

func TestResolveAtlasV2Dev(t *testing.T) {
	d, diags, err := Resolve(context.Background(), input(t, "atlas-v2", "local", "dev"))
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	if len(d.Services) != 3 || len(d.Resources) != 3 {
		t.Fatalf("%d services %d resources", len(d.Services), len(d.Resources))
	}

	api := d.Services[0]
	if api.Name != "api" || api.Health.Readiness != "/_/health/ready" || api.Health.Liveness != "/_/health/live" {
		t.Fatalf("api health: %+v", api)
	}

	if len(api.Bindings) != 3 || api.Bindings[0].Keys["extensions.grove.databases[primary].dsn"] != "${ATLAS_PRIMARY_DSN}" {
		t.Fatalf("api bindings: %+v", api.Bindings)
	}

	if d.Resources[0].Name != "cache" || d.Resources[0].Lifecycle != spec.LifecycleContainer || d.Resources[0].Recipe != "redis-stack-7" {
		t.Fatalf("resources sorted by name, cache first with the stack recipe: %+v", d.Resources[0])
	}

	if len(d.Connections) != 1 || d.Connections[0].ConfigKey != "services.api.url" || d.Connections[0].EnvVar != "API_URL" {
		t.Fatalf("connections: %+v", d.Connections)
	}

	if len(d.Migrations) != 1 || d.Migrations[0].Service != "api" || len(d.Migrations[0].Resources) != 2 {
		t.Fatalf("migrations: %+v", d.Migrations)
	}

	if d.Overlay != model.OverlayFallback {
		t.Fatalf("overlay mode: %v", d.Overlay)
	}
}

func TestResolveProductionNeedsSecrets(t *testing.T) {
	in := input(t, "atlas-v2", "k8s-prod", "production")
	_, diags, _ := Resolve(context.Background(), in)

	var codes []string
	for _, d := range diags.Errors() {
		codes = append(codes, d.Code)
	}

	want := map[string]bool{"DEPLOY_SECRET_UNRESOLVED": true}
	for _, c := range codes {
		delete(want, c)
	}

	if len(want) != 0 {
		t.Fatalf("missing %v in %v", want, codes)
	}
}

func TestLifecycleUnsupportedByTarget(t *testing.T) {
	in := input(t, "atlas-v2", "k8s-prod", "production")
	in.Caps = composeCaps() // compose cannot do managed

	t.Setenv("ATLAS_PRIMARY_DSN", "x")
	t.Setenv("ATLAS_UPLOADS_CREDENTIALS", "x")

	_, diags, _ := Resolve(context.Background(), in)
	found := false

	for _, d := range diags {
		if d.Code == "DEPLOY_LIFECYCLE_UNSUPPORTED" && d.Field == "deploy.environments.production.resources.cache.lifecycle" {
			found = true
		}
	}

	if !found {
		t.Fatalf("expected DEPLOY_LIFECYCLE_UNSUPPORTED for managed cache, got %v", diags)
	}
}

func TestSharedResourceAcrossApps(t *testing.T) {
	d, diags, _ := Resolve(context.Background(), input(t, "atlas-v2", "local", "dev"))
	if diags.HasErrors() {
		t.Fatal(diags)
	}

	for _, r := range d.Resources {
		if r.Name == "primary" && (len(r.UsedBy) != 2 || r.UsedBy[0] != "api" || r.UsedBy[1] != "worker") {
			t.Fatalf("primary used by: %v", r.UsedBy)
		}
	}
}

func TestOverlayGolden(t *testing.T) {
	d, _, _ := Resolve(context.Background(), input(t, "atlas-v2", "local", "dev"))

	for _, name := range []string{"api", "gateway"} {
		var svc *model.Service

		for i := range d.Services {
			if d.Services[i].Name == name {
				svc = &d.Services[i]
			}
		}

		got, err := Overlay(d, svc)
		if err != nil {
			t.Fatal(err)
		}

		golden := filepath.Join("testdata", "golden", "overlay-"+name+".yaml")
		if *update {
			_ = os.MkdirAll(filepath.Dir(golden), 0o755)
			_ = os.WriteFile(golden, got, 0o644)
		}

		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("missing golden; run with -update: %v", err)
		}

		if string(want) != string(got) {
			t.Fatalf("overlay %s differs:\n%s", name, got)
		}
	}
}

func TestSelectedWorkerKeepsOnlyItsResources(t *testing.T) {
	in := input(t, "atlas-v2", "local", "dev")
	in.Services = []string{"worker"}

	d, diags, err := Resolve(context.Background(), in)
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	if len(d.Services) != 1 || len(d.Resources) != 2 || len(d.Routes) != 0 {
		t.Fatalf("scope: %+v", d)
	}
}
func TestExcludedCallRequiresExternalEndpoint(t *testing.T) {
	in := input(t, "atlas-v2", "local", "dev")
	in.Services = []string{"gateway"}

	_, diags, _ := Resolve(context.Background(), in)
	if !diags.HasErrors() {
		t.Fatal("excluded API accepted")
	}

	e := in.Doc.Deploy.Environments["dev"]
	e.ExternalServices = map[string]spec.ExternalService{"api": {URL: "https://api.example.test"}}
	in.Doc.Deploy.Environments["dev"] = e

	d, diags, err := Resolve(context.Background(), in)
	if err != nil || diags.HasErrors() {
		t.Fatalf("%v %v", err, diags)
	}

	if len(d.Services) != 1 || d.Connections[0].Address != "https://api.example.test" {
		t.Fatal(d)
	}
}
