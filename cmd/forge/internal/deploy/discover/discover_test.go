package discover

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

func load(t *testing.T, fixture string) *config.ForgeConfig {
	t.Helper()

	root := testdata.Root(fixture)

	cfg, err := config.LoadForgeConfigFrom(root) // added in plan 01 task 7
	if err != nil {
		t.Fatal(err)
	}

	return cfg
}

func fakeGoList(t *testing.T) *execx.Fake {
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go list -m -json all", execx.Result{Stdout: `{"Path":"github.com/xraph/grove","Dir":"/mod/grove"}` + "\n" + `{"Path":"github.com/xraph/trove","Dir":"/mod/trove"}` + "\n"})
	f.Script("go list -deps -f {{.ImportPath}} ./cmd/api", execx.Result{Stdout: "net/http\ngithub.com/xraph/grove/drivers/pgdriver\ngithub.com/xraph/grove/kv/drivers/redisdriver\ngithub.com/xraph/trove/drivers/s3driver\n"})
	f.Script("go list -deps -f {{.ImportPath}} ./cmd/worker", execx.Result{Stdout: "github.com/xraph/grove/drivers/pgdriver\ngithub.com/xraph/grove/kv/drivers/redisdriver\n"})
	f.Script("go list -deps -f {{.ImportPath}} ./cmd/gateway", execx.Result{Stdout: "net/http\n"})

	return f
}

func find(res *Result, kind SuggestionKind, path string) (Suggestion, bool) {
	for _, s := range res.Suggestions {
		if s.Kind == kind && s.Path == path {
			return s, true
		}
	}

	return Suggestion{}, false
}

func TestAtlasApps(t *testing.T) {
	res, err := Run(context.Background(), load(t, "atlas"), catalog.Embedded(), Options{Runner: fakeGoList(t)})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Apps) != 3 {
		t.Fatalf("apps: %+v", res.Apps)
	}

	var worker App

	for _, a := range res.Apps {
		if a.Name == "worker" {
			worker = a
		}
	}

	if worker.Type != "worker" || worker.Port != 0 {
		t.Fatalf("worker app: %+v", worker)
	}
}

func TestAtlasResourceSuggestionsComeFromConfig(t *testing.T) {
	res, _ := Run(context.Background(), load(t, "atlas"), catalog.Embedded(), Options{Runner: fakeGoList(t)})

	s, ok := find(res, SuggestResource, "deploy.resources.primary")
	if !ok {
		t.Fatalf("no primary resource; got %+v", res.Suggestions)
	}

	if s.Confidence != High || s.Source != "config/api.yaml:4" {
		t.Fatalf("primary provenance: %+v", s)
	}

	if v := s.Value.(map[string]any); v["type"] != string(model.Postgres) {
		t.Fatalf("primary type: %v", v)
	}

	if _, ok := find(res, SuggestResource, "deploy.resources.uploads"); !ok {
		t.Fatal("no uploads object-storage resource")
	}

	if _, ok := find(res, SuggestResource, "deploy.resources.cache"); !ok {
		t.Fatal("no cache redis resource")
	}
}

func TestAtlasBindingsAndDecisions(t *testing.T) {
	res, _ := Run(context.Background(), load(t, "atlas"), catalog.Embedded(), Options{Runner: fakeGoList(t)})

	b, ok := find(res, SuggestBinding, "deploy.services.api.bindings")
	if !ok {
		t.Fatal("no api bindings")
	}

	list := b.Value.([]map[string]any)
	if len(list) != 3 {
		t.Fatalf("api bindings: %v", list)
	}

	if list[1]["extension"] != "trove" || list[1]["metadata_database"] != "primary" {
		t.Fatalf("trove binding: %v", list[1])
	}

	if d, ok := find(res, SuggestDecision, "deploy.services.worker.health"); !ok || len(d.Options) != 2 {
		t.Fatalf("worker health decision: %+v", d)
	}

	if d, ok := find(res, SuggestDecision, "deploy.resources.cache.features"); !ok || d.Options[0] != "json,search" {
		t.Fatalf("redis features decision: %+v", d)
	}
}

func TestGroveDefaultInstanceWithoutList(t *testing.T) {
	root := testdata.Copy(t, "bare")
	writeFile(t, root, "config/svc.yaml", "extensions:\n  grove:\n    driver: pg\n    dsn: ${DATABASE_URL}\n")
	cfg, _ := config.LoadForgeConfigFrom(root)
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps -f {{.ImportPath}} ./cmd/svc", execx.Result{Stdout: "net/http\n"})
	res, _ := Run(context.Background(), cfg, catalog.Embedded(), Options{Runner: f})

	s, ok := find(res, SuggestResource, "deploy.resources.default")
	if !ok || s.Value.(map[string]any)["type"] != "postgres" {
		t.Fatalf("default instance: %+v", s)
	}

	b, _ := find(res, SuggestBinding, "deploy.services.svc.bindings")
	if b.Value.([]map[string]any)[0]["database"] != "default" {
		t.Fatalf("default binding: %+v", b)
	}
}

func TestBareAppIsWebAtMediumConfidence(t *testing.T) {
	cfg := load(t, "bare")
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps -f {{.ImportPath}} ./cmd/svc", execx.Result{Stdout: "net/http\n"})
	res, _ := Run(context.Background(), cfg, catalog.Embedded(), Options{Runner: f})

	s, ok := find(res, SuggestService, "deploy.services.svc")
	if !ok {
		t.Fatal("no service suggestion")
	}

	v := s.Value.(map[string]any)
	if v["kind"] != "web" || s.Confidence == High {
		t.Fatalf("bare service: %+v", s)
	}
}

func TestImportsOnlyProduceLowConfidence(t *testing.T) {
	cfg := load(t, "bare")
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go list -m -json all", execx.Result{})
	f.Script("go list -deps -f {{.ImportPath}} ./cmd/svc", execx.Result{Stdout: "github.com/nats-io/nats.go\n"})
	res, _ := Run(context.Background(), cfg, catalog.Embedded(), Options{Runner: f})

	s, ok := find(res, SuggestResource, "deploy.resources.nats")
	if !ok || s.Confidence != Low || s.Source != "go list: github.com/nats-io/nats.go" {
		t.Fatalf("nats from imports: %+v", s)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestOfflineDiscoveryNeverRunsMissingGo(t *testing.T) {
	f := execx.NewFake(t)

	res, err := Run(context.Background(), load(t, "bare"), catalog.Embedded(), Options{Runner: f})
	if err != nil || len(f.Calls) != 0 || len(res.Diagnostics) == 0 {
		t.Fatalf("%v %+v", err, res)
	}
}
func TestMalformedInstanceListReturnsDiagnostic(t *testing.T) {
	root := testdata.Copy(t, "bare")
	writeFile(t, root, "config/svc.yaml", "extensions:\n  grove:\n    databases: invalid\n")
	cfg, _ := config.LoadForgeConfigFrom(root)
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go", execx.Result{})

	res, err := Run(context.Background(), cfg, catalog.Embedded(), Options{Runner: f})
	if err != nil {
		t.Fatal(err)
	}

	if !res.Diagnostics.HasErrors() {
		t.Fatal("invalid config did not block")
	}
}
