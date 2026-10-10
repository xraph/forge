package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/images"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/secrets"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

type registryReviewRunner struct {
	*execx.Fake

	token string
}

func (r *registryReviewRunner) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	if c.Name == "docker" && slices.Contains(c.Args, "login") {
		raw, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}

		r.token = strings.TrimSpace(string(raw))
		index := slices.Index(c.Args, "--config")

		data, err := json.Marshal(map[string]any{"auths": map[string]any{"ghcr.io": map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte("rex:" + r.token))}}})
		if err != nil {
			return execx.Result{}, err
		}

		if err := os.WriteFile(filepath.Join(c.Args[index+1], "config.json"), data, 0600); err != nil {
			return execx.Result{}, err
		}

		return execx.Result{}, nil
	}

	return r.Fake.Run(ctx, c)
}

type registryReviewProvider struct {
	provider.Provider

	root   string
	runner execx.Runner
}

func (p *registryReviewProvider) Apply(ctx context.Context, approved *plan.Plan, st *state.Store, values map[string]string, _ chan<- provider.Event) error {
	if _, err := images.Build(ctx, p.runner, p.root, approved, st, values); err != nil {
		return err
	}

	runtime, err := secrets.Generate(approved.Deployment, st, values)
	if err != nil {
		return err
	}

	for _, v := range runtime {
		if v == "private-registry-token" {
			return io.ErrUnexpectedEOF
		}
	}

	return nil
}
func TestEngineResolvesRegistrySecretWithoutNamedConnection(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	path := filepath.Join(root, ".forge.yml")
	raw, _ := os.ReadFile(path)
	raw = []byte(strings.Replace(string(raw), "{ provider: compose }", "{ provider: compose, build: {source: existing, delivery: registry, images: {api: ghcr.io/example/api@sha256:AAAAAAAA, gateway: ghcr.io/example/gateway@sha256:AAAAAAAA, worker: ghcr.io/example/worker@sha256:AAAAAAAA}, registry: {host: ghcr.io, username: rex, secret_ref: env:GHCR_DEPLOY_TOKEN}} }", 1))
	// Resolve every selected existing image without rebuilding.
	raw = []byte(strings.ReplaceAll(string(raw), "AAAAAAAA", strings.Repeat("a", 64)))
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GHCR_DEPLOY_TOKEN", "private-registry-token")
	e, f := composeEngine(t, root)
	f.Script("docker", execx.Result{})
	f.Script("docker context show", execx.Result{Stdout: "default"})
	f.Script("docker --config", execx.Result{Stdout: "sha256:" + strings.Repeat("a", 64)})
	r := &registryReviewRunner{Fake: f}
	e.runner = r
	base, _ := e.registry.Get("compose")
	e.registry = provider.NewRegistry(r, root, func(execx.Runner, string) provider.Provider {
		return &registryReviewProvider{Provider: base, root: root, runner: r}
	})

	p, _, err := e.Plan(context.Background(), "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Apply(context.Background(), p, p.Hash, false, nil); err != nil {
		t.Fatal(err)
	}

	if r.token != "private-registry-token" {
		t.Fatal("registry credential discarded")
	}

	rawPlan, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(rawPlan), r.token) {
		t.Fatal("plan contains registry credential")
	}
}
func TestRegistryDoctorRejectsUnresolvedCredential(t *testing.T) {
	root := testdata.Copy(t, "atlas-v2")
	path := filepath.Join(root, ".forge.yml")
	raw, _ := os.ReadFile(path)

	raw = []byte(strings.Replace(string(raw), "{ provider: compose }", "{ provider: compose, build: {registry: {host: ghcr.io, username: rex, secret_ref: env:GHCR_DEPLOY_TOKEN}} }", 1))
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GHCR_DEPLOY_TOKEN", "")
	e, _ := composeEngine(t, root)

	ds, err := e.Doctor(context.Background(), "local", "dev", false)
	if err != nil {
		t.Fatal(err)
	}

	if !ds.HasErrors() {
		t.Fatal("unresolved registry credential hidden")
	}
}
