package compose

import (
	"context"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/resolve"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"strings"
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

func observationPrefix(t *testing.T, c *Compose, st *state.Store, d *model.Deployment) string {
	t.Helper()

	args, err := c.observationArgs(d, st, "ps", "-a", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}

	return "docker " + strings.Join(args, " ")
}
func scriptPS(t *testing.T, c *Compose, st *state.Store, f *execx.Fake, d *model.Deployment, result execx.Result) {
	t.Helper()
	f.Script("docker "+strings.Join(c.composeArgs(d, "ps", "-a", "--format", "json"), " "), result)
	f.Script(observationPrefix(t, c, st, d), result)
}
