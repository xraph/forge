package engine

import (
	"context"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"

	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineInspectionNeverReadsKubernetesSecrets(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	path := filepath.Join(root, ".forge.yml")
	doc, _, _ := spec.Parse(path)
	files, _ := doc.Patch([]spec.Op{{Path: "deploy.secrets", Value: spec.Secrets{Resolver: "kubernetes"}}, {Path: "deploy.environments.dev.resources.primary", Value: spec.ResourceOverride{Lifecycle: spec.LifecycleExternal, Secret: "primary-dsn"}}})
	_ = spec.Write(path, doc.Hash, files[path])
	cfg, _ := config.LoadForgeConfigFrom(root)
	f := execx.NewFake(t)
	f.T = nil
	f.Available["go"] = true
	f.Available["kubectl"] = true
	f.Script("go", execx.Result{Stdout: "net/http\n"})
	e, _ := New(Options{Config: cfg, Runner: f})
	_, _ = e.Inspect(context.Background(), "local", "dev")
	_, _ = e.Doctor(context.Background(), "local", "dev", false)

	for _, call := range f.Calls {
		if call.Name == "kubectl" {
			t.Fatal("offline contact", call)
		}
	}
}
func TestSkipImportedRedisDoesNotRecreateResource(t *testing.T) {
	root := testdata.Copy(t, "bare")
	cfg, _ := config.LoadForgeConfigFrom(root)
	f := execx.NewFake(t)
	f.Available["go"] = true
	f.Script("go list -m", execx.Result{})
	f.Script("go list -deps", execx.Result{Stdout: "github.com/redis/go-redis/v9\n"})
	e, _ := New(Options{Config: cfg, Runner: f})

	_, files, err := e.Init(context.Background(), map[string]string{"deploy.resources.redis": "skip", "deploy.resources.redis.features": "none", "deploy.services.svc.health": "none"}, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, raw := range files {
		if strings.Contains(string(raw), "redis:") {
			t.Fatal("skipped resource recreated")
		}
	}
}
