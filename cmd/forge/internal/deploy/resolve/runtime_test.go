package resolve

import (
	"context"
	"fmt"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/cmd/forge/internal/deploy/testdata"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeConfigurationAndRecipeArePreserved(t *testing.T) {
	in := input(t, "atlas-v2", "local", "dev")
	root := testdata.Copy(t, "atlas-v2")
	in.Config, _ = config.LoadForgeConfigFrom(root)
	path := filepath.Join(root, "config", "api.yaml")
	raw, _ := os.ReadFile(path)
	_ = os.WriteFile(path, []byte(strings.Replace(string(raw), "driver: postgres", "driver: postgres, max_open_conns: 12", 1)), 0600)
	rec := in.Catalog.Recipes["postgres-16"]
	rec.Image = "example.com/pg@sha256:custom"
	in.Catalog.Recipes[rec.ID] = rec

	d, ds, err := Resolve(context.Background(), in)
	if err != nil || ds.HasErrors() {
		t.Fatal(err, ds)
	}

	overlay, err := Overlay(d, &d.Services[0])
	if err != nil || !strings.Contains(string(overlay), "max_open_conns: 12") {
		t.Fatal(err, string(overlay))
	}

	if d.Resources[1].RuntimeRecipe.Image != rec.Image {
		t.Fatal("recipe override lost")
	}
}
func TestRuntimeLiteralSecretNeverEntersPlan(t *testing.T) {
	in := input(t, "atlas-v2", "local", "dev")
	root := testdata.Copy(t, "atlas-v2")
	in.Config, _ = config.LoadForgeConfigFrom(root)
	_ = os.WriteFile(filepath.Join(root, "config", "api.yaml"), []byte("extensions:\n  custom:\n    api_key: literal-private-key\n"), 0600)

	d, ds, err := Resolve(context.Background(), in)
	if err != nil || !ds.HasErrors() || d != nil {
		t.Fatal("literal secret accepted", err, ds)
	}

	if strings.Contains(fmt.Sprint(ds), "literal-private-key") {
		t.Fatal("secret exposed")
	}
}
