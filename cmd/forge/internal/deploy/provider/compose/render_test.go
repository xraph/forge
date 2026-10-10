package compose

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"

	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
)

var update = flag.Bool("update", false, "rewrite golden files")

func atlasDev(t *testing.T) *model.Deployment {
	t.Helper()
	// Uses resolve's test helper pattern: load atlas-v2, local, dev.
	d := resolveFixture(t, "atlas-v2", "local", "dev")

	return d
}

func TestRenderGolden(t *testing.T) {
	d := atlasDev(t)
	c := New(execx.NewFake(t), testdata.Root("atlas-v2"))

	b, err := c.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join("testdata", "golden", "atlas-dev")
	if *update {
		_ = os.RemoveAll(dir)
		if _, err := render.Write(dir, b, render.WriteOptions{Force: true}); err != nil {
			t.Fatal(err)
		}
	}

	for _, f := range b.Sorted() {
		want, err := os.ReadFile(filepath.Join(dir, f.Path))
		if err != nil {
			t.Fatalf("missing golden %s; run with -update", f.Path)
		}

		if string(want) != string(f.Content) {
			t.Fatalf("%s differs:\n%s", f.Path, f.Content)
		}
	}

	b2, _ := c.Render(context.Background(), d)
	for p, h := range b.Hashes() {
		if b2.Hashes()[p] != h {
			t.Fatalf("render is not deterministic: %s", p)
		}
	}
}

func TestRenderHasNoSecretValues(t *testing.T) {
	d := atlasDev(t)
	c := New(execx.NewFake(t), testdata.Root("atlas-v2"))

	b, _ := c.Render(context.Background(), d)
	for _, f := range b.Sorted() {
		if strings.Contains(string(f.Content), "password=") || strings.Contains(string(f.Content), "POSTGRES_PASSWORD: ") && !strings.Contains(string(f.Content), "${") {
			t.Fatalf("%s leaks a value:\n%s", f.Path, f.Content)
		}
	}
}

func TestComposeConfigAcceptsOutput(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}

	d := atlasDev(t)
	c := New(execx.System(), testdata.Root("atlas-v2"))
	b, _ := c.Render(context.Background(), d)
	dir := t.TempDir()
	_, _ = render.Write(dir, b, render.WriteOptions{Force: true})
	_ = os.WriteFile(filepath.Join(dir, "generated.env"), []byte("ATLAS_PRIMARY_USER=u\nATLAS_PRIMARY_PASSWORD=p\nATLAS_PRIMARY_DSN=x\nATLAS_CACHE_URL=y\nATLAS_UPLOADS_USER=u\nATLAS_UPLOADS_PASSWORD=p\nATLAS_UPLOADS_DSN=z\n"), 0o600)
	cmd := exec.CommandContext(context.Background(), "docker", "compose", "-f", filepath.Join(dir, "compose.yaml"), "--env-file", filepath.Join(dir, "generated.env"), "config", "-q")

	cmd.Dir = testdata.Root("atlas-v2")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker compose config: %v\n%s", err, out)
	}
}

func TestValidateDuplicateHostPort(t *testing.T) {
	d := atlasDev(t)
	for i := range d.Services {
		for j := range d.Services[i].Ports {
			d.Services[i].Ports[j].Exposure = spec.ExposurePublic
			d.Services[i].Ports[j].Port = 8080
		}
	}

	c := New(execx.NewFake(t), testdata.Root("atlas-v2"))

	diags := c.Validate(context.Background(), d)
	if len(diags) == 0 || !strings.Contains(diags[0].Message, "gateway") || !strings.Contains(diags[0].Message, "api") {
		t.Fatalf("%v", diags)
	}
}

func TestHostBuilderUsesPrivateBinaryContext(t *testing.T) {
	d := atlasDev(t)
	d.Target.Build.Builder = "host"
	c := New(execx.NewFake(t), testdata.Root("atlas-v2"))

	b, err := c.Render(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}

	file := string(b.Files["api/Dockerfile"].Content)
	if strings.Contains(file, "FROM golang:") || !strings.Contains(file, "COPY app /app/app") {
		t.Fatal(file)
	}

	if !strings.Contains(string(b.Files["compose.yaml"].Content), ".forge/state/local/dev/build/api") {
		t.Fatal("host binary context missing")
	}
}

func TestExistingImagesRequireImmutableDigests(t *testing.T) {
	d := resolveFixture(t, "atlas-v2", "local", "dev")

	d.Target.Build.Source = "existing"
	if !New(nil, ".").Validate(context.Background(), d).HasErrors() {
		t.Fatal("existing source accepted generated mutable tags")
	}

	for i := range d.Services {
		d.Services[i].Image.Digest = "sha256:" + strings.Repeat("a", 64)
	}

	if New(nil, ".").Validate(context.Background(), d).HasErrors() {
		t.Fatal("valid immutable images rejected")
	}
}

func TestComposeLogicalIdentity(t *testing.T) {
	d := atlasDev(t)
	c := New(execx.NewFake(t), testdata.Root("atlas-v2"))

	b, e := c.Render(t.Context(), d)
	if e != nil {
		t.Fatal(e)
	}

	if !strings.Contains(string(b.Files["compose.yaml"].Content), "FORGE_SERVICE_ID: api") {
		t.Fatal("missing logical service identity")
	}

	d.Services[0].Env = map[string]string{"FORGE_INSTANCE_ID": "shared"}
	if !c.Validate(t.Context(), d).HasErrors() {
		t.Fatal("fixed replica identity accepted")
	}
}
