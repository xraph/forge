package compose

import (
	"context"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"testing"
)

func resolveFixture(t *testing.T, name, target, env string) *model.Deployment {
	t.Helper()

	return resolveFixtureAt(t, testdata.Root(name), target, env)
}
func resolveFixtureAt(t *testing.T, root, target, env string) *model.Deployment {
	t.Helper()

	cfg, err := config.LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	path, _, err := spec.Locate(root)
	if err != nil {
		t.Fatal(err)
	}

	doc, ds, err := spec.Parse(path)
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}

	sec, _ := secrets.New(spec.Secrets{Resolver: "env"}, root, nil, spec.Target{}, cfg.Project.Name)

	d, ds, err := resolve.Resolve(context.Background(), resolve.Input{Config: cfg, Doc: doc, Catalog: catalog.Embedded(), Target: target, Environment: env, Caps: composeCapabilities(), Secrets: sec})
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}

	return d
}
