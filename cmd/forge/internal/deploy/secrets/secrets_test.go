package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

func TestEnvVarName(t *testing.T) {
	if got := EnvVarName("my-app", "primary-dsn"); got != "MY_APP_PRIMARY_DSN" {
		t.Fatal(got)
	}
}

func TestEnvResolver(t *testing.T) {
	t.Setenv("ATLAS_PRIMARY_DSN", "postgres://x")
	r, _ := New(spec.Secrets{Resolver: "env"}, t.TempDir(), nil, spec.Target{}, "atlas")

	st, err := r.Check(context.Background(), "primary-dsn")
	if err != nil || !st.Resolved || st.Where != "environment ATLAS_PRIMARY_DSN" {
		t.Fatalf("%+v %v", st, err)
	}

	st, _ = r.Check(context.Background(), "missing")
	if st.Resolved {
		t.Fatal("missing must not resolve")
	}
}

func TestFileResolverReportsLine(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".forge"), 0o700)
	_ = os.WriteFile(filepath.Join(root, ".forge", "secrets.env"), []byte("# comment\nATLAS_CACHE_URL=redis://c\nATLAS_PRIMARY_DSN=\"postgres://p\"\n"), 0o600)
	r, _ := New(spec.Secrets{Resolver: "file"}, root, nil, spec.Target{}, "atlas")

	st, _ := r.Check(context.Background(), "primary-dsn")
	if !st.Resolved || st.Where != ".forge/secrets.env:3" {
		t.Fatalf("%+v", st)
	}

	vals, err := r.(*FileResolver).ValuesForApply(context.Background())
	if err != nil || vals["ATLAS_PRIMARY_DSN"] != "postgres://p" {
		t.Fatalf("%v %v", vals, err)
	}
}

func TestKubernetesResolverUsesRunner(t *testing.T) {
	f := execx.NewFake(t)
	f.Available["kubectl"] = true
	f.Script("kubectl --context prod get secret forge-atlas-production -n atlas-production -o jsonpath={.data}", execx.Result{Stdout: `{"ATLAS_PRIMARY_DSN":"cG9zdGdyZXM6Ly9w"}`})
	r, _ := New(spec.Secrets{Resolver: "kubernetes"}, t.TempDir(), f, spec.Target{Provider: "kubernetes", Context: "prod", Namespace: "atlas-production"}, "atlas")
	kr := r.(*KubernetesResolver)
	kr.Environment = "production"

	st, err := r.Check(context.Background(), "primary-dsn")
	if err != nil || !st.Resolved || st.Where != "secret forge-atlas-production key ATLAS_PRIMARY_DSN" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestEmptyEnvironmentSecretDoesNotResolve(t *testing.T) {
	t.Setenv("ATLAS_PRIMARY_DSN", "")

	resolver, err := New(spec.Secrets{}, t.TempDir(), nil, spec.Target{}, "atlas")
	if err != nil {
		t.Fatal(err)
	}

	status, err := resolver.Check(context.Background(), "primary-dsn")
	if err != nil || status.Resolved {
		t.Fatalf("empty secret resolved: %v %v", status, err)
	}
}
func TestFileResolverRejectsInvalidDotenv(t *testing.T) {
	root := t.TempDir()

	path := filepath.Join(root, "secrets.env")
	if err := os.WriteFile(path, []byte("INVALID LINE\nATLAS_PRIMARY_DSN='unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}

	resolver, err := New(spec.Secrets{Resolver: "file", File: "secrets.env"}, root, nil, spec.Target{}, "atlas")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := resolver.(*FileResolver).ValuesForApply(context.Background()); err == nil {
		t.Fatal("invalid secret file accepted")
	}
}
func TestSecretFileErrorRedactsContent(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "s.env"), []byte("ATLAS_PASSWORD='top-secret-value\n"), 0600)
	r, _ := New(spec.Secrets{Resolver: "file", File: "s.env"}, root, nil, spec.Target{}, "atlas")

	_, err := r.(*FileResolver).ValuesForApply(context.Background())
	if err == nil || strings.Contains(err.Error(), "top-secret-value") {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestRecordedBackendCannotRegenerateLostCredentials(t *testing.T) {
	root := t.TempDir()

	st, err := state.Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.SaveSnapshot(state.Snapshot{Resources: map[string]state.ResourceState{"primary": {Name: "primary", Lifecycle: spec.LifecycleContainer}}}); err != nil {
		t.Fatal(err)
	}

	d := &model.Deployment{Project: "atlas", Resources: []model.Resource{{Name: "primary", Lifecycle: spec.LifecycleContainer, Secret: model.SecretRef{EnvVar: "ATLAS_PRIMARY_DSN"}, RuntimeRecipe: &catalog.Recipe{Port: 5432, DSN: "postgres://USER:PASSWORD@primary:5432/atlas"}}}}
	if _, err := Generate(d, st, nil); err == nil {
		t.Fatal("running database credential silently replaced")
	}
}
