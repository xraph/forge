package engine

import (
	"errors"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustedProviderFactoriesCannotReplaceBuiltins(t *testing.T) {
	cfg, _ := config.LoadForgeConfigFrom(testdata.Root("atlas-v2"))

	factory := func(execx.Runner, string) provider.Provider { return provider.ExportOnly{ProviderName: "compose"} }
	if _, e := New(Options{Config: cfg, ProviderFactories: []provider.Factory{factory}}); e == nil {
		t.Fatal("builtin replaced")
	}

	factory = func(execx.Runner, string) provider.Provider { return provider.ExportOnly{ProviderName: "custom"} }

	e, err := New(Options{Config: cfg, ProviderFactories: []provider.Factory{factory}})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := e.registry.Get("custom"); !ok {
		t.Fatal("factory omitted")
	}
}
func TestHostedPlanExportsWithoutLaptopCredentials(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	path := filepath.Join(root, ".forge.yml")

	doc, ds, err := spec.Parse(path)
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}

	api := doc.Deploy.Services["api"]
	api.Migrate = ""
	doc.Deploy.Services["api"] = api
	images := map[string]string{"api": "ghcr.io/example/api@sha256:" + strings.Repeat("a", 64)}
	doc.Deploy.Targets["hosted"] = spec.Target{Provider: "hosted", Build: spec.Build{Source: "existing", Delivery: "registry", Images: images}, SecretKeys: map[string]string{"primary-dsn": "vault-primary", "uploads-credentials": "vault-uploads", "cache-dsn": "vault-cache"}}
	doc.Deploy.Environments["hosted"] = spec.Environment{Target: "hosted", Services: []string{"api"}, Resources: map[string]spec.ResourceOverride{"primary": {Lifecycle: spec.LifecycleExternal, Secret: "primary-dsn"}, "uploads": {Lifecycle: spec.LifecycleExternal, Secret: "uploads-credentials"}, "cache": {Lifecycle: spec.LifecycleExternal, Secret: "cache-dsn"}}}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var whole map[string]any
	if err = yaml.Unmarshal(raw, &whole); err != nil {
		t.Fatal(err)
	}

	whole["deploy"] = doc.Deploy

	raw, err = yaml.Marshal(whole)
	if err != nil {
		t.Fatal(err)
	}

	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	e, f := composeEngine(t, root)

	p, b, err := e.Plan(t.Context(), "hosted", "hosted")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := b.Files["forge-hosted.json"]; !ok {
		t.Fatal("missing contract")
	}

	if _, err = e.Export(t.Context(), p, b, "", false); err != nil {
		t.Fatal(err)
	}

	f.Calls = nil
	err = e.Apply(t.Context(), p, p.Hash, false, nil)

	var typed *output.Error
	if !errors.As(err, &typed) || typed.Code != output.ExitUnsupported || len(f.Calls) > 0 {
		t.Fatal("export adapter attempted apply", err, f.Calls)
	}
}
