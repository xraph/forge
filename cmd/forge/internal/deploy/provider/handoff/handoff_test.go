package handoff

import (
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"strings"
	"testing"
)

func fixture(name string) *model.Deployment {
	return &model.Deployment{Project: "atlas", TargetName: name, Environment: "production", Target: spec.Target{Provider: name, Build: spec.Build{Source: "existing", Delivery: "registry"}}, Services: []model.Service{{Name: "api", MainPath: "cmd/api", Kind: spec.KindWeb, Replicas: 1, Image: model.Image{Repository: "ghcr.io/example/api", Digest: "sha256:" + strings.Repeat("a", 64)}}}}
}
func TestPortableContract(t *testing.T) {
	for _, name := range []string{"vm", "fly", "railway"} {
		p := New(name, execx.NewFake(t), t.TempDir())
		d := fixture(name)

		b, e := p.Render(t.Context(), d)
		if e != nil {
			t.Fatal(e)
		}

		again, e := p.Render(t.Context(), d)
		if e != nil || string(b.Files["compose.yaml"].Content) != string(again.Files["compose.yaml"].Content) {
			t.Fatal("nondeterministic", e)
		}

		if !strings.Contains(string(b.Files["HANDOFF.md"].Content), "export") {
			t.Fatal("missing handoff")
		}

		if !errors.Is(p.Apply(t.Context(), nil, nil, nil, nil), provider.ErrUnsupported) {
			t.Fatal(name)
		}

		if _, e = p.Observe(t.Context(), provider.EnvRef{}, nil); !errors.Is(e, provider.ErrUnsupported) {
			t.Fatal(name, e)
		}

		if _, e = p.Logs(t.Context(), provider.ServiceRef{}, provider.LogOptions{}); !errors.Is(e, provider.ErrUnsupported) {
			t.Fatal(name, e)
		}

		if e = p.Rollback(t.Context(), provider.EnvRef{}, nil, ""); !errors.Is(e, provider.ErrUnsupported) {
			t.Fatal(name, e)
		}

		if e = p.Destroy(t.Context(), nil, nil, provider.DestroyOptions{}); !errors.Is(e, provider.ErrUnsupported) {
			t.Fatal(name, e)
		}

		d.Services[0].Image.Digest = ""
		if !p.Validate(t.Context(), d).HasErrors() {
			t.Fatal("mutable image accepted")
		}
	}
}
