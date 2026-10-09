package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

func TestEmbeddedHasEveryExtension(t *testing.T) {
	c := Embedded()
	for _, name := range []string{"grove", "grove_kv", "trove", "cache", "queue", "events",
		"search", "kafka", "mqtt", "discovery", "bastion", "authsome", "relay", "dispatch",
		"chronicle", "nexus", "vault", "keysmith", "ledger", "herald"} {
		d, ok := c.DescriptorFor(name)
		if !ok {
			t.Errorf("missing descriptor %s", name)

			continue
		}

		if d.Schema != 1 || d.ConfigKey == "" || d.Source != "embedded" {
			t.Errorf("%s: bad descriptor %+v", name, d)
		}
	}
}

func TestGroveKinds(t *testing.T) {
	d, _ := Embedded().DescriptorFor("grove")
	if d.Kinds["pg"] != model.Postgres || d.Kinds["mongo"] != model.MongoDB {
		t.Fatalf("grove kinds wrong: %v", d.Kinds)
	}

	if d.Instances == nil || d.Instances.Path != "databases" || d.Instances.Name != "name" {
		t.Fatalf("grove instances wrong: %+v", d.Instances)
	}
}

func TestRecipeForRedisFeatures(t *testing.T) {
	c := Embedded()

	plain, ok := c.RecipeFor(model.Redis, "", nil)
	if !ok || plain.ID != "redis-7" {
		t.Fatalf("plain redis: %v %v", plain.ID, ok)
	}

	stack, ok := c.RecipeFor(model.Redis, "", []string{"json", "search"})
	if !ok || stack.ID != "redis-stack-7" {
		t.Fatalf("redis with features: %v %v", stack.ID, ok)
	}

	if _, ok := c.RecipeFor(model.Redis, "", []string{"graph"}); ok {
		t.Fatal("no recipe provides graph")
	}
}

func TestProjectDescriptorOverridesEmbedded(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "deploy", "descriptors")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "cache.yaml"), []byte("schema: 1\nextension: cache\nconfig_key: extensions.cache\nkinds: {valkey: redis}\n"), 0o644)

	c, diags, err := Load(context.Background(), root, nil)
	if err != nil || diags.HasErrors() {
		t.Fatalf("load: %v %v", err, diags)
	}

	d, _ := c.DescriptorFor("cache")
	if d.Source != filepath.Join(dir, "cache.yaml") || d.Kinds["valkey"] != model.Redis {
		t.Fatalf("override not applied: %+v", d)
	}
}

func TestModuleDescriptorNewerSchemaIsRejected(t *testing.T) {
	mod := t.TempDir()
	_ = os.WriteFile(filepath.Join(mod, "forge-deploy.yaml"), []byte("schema: 2\nextension: thing\nconfig_key: extensions.thing\n"), 0o644)

	_, diags, err := Load(context.Background(), t.TempDir(), []Module{{Path: "example.com/thing", Dir: mod}})
	if err != nil {
		t.Fatal(err)
	}

	if !diags.HasErrors() || diags[0].Code != "DEPLOY_DESCRIPTOR_SCHEMA" {
		t.Fatalf("expected DEPLOY_DESCRIPTOR_SCHEMA, got %v", diags)
	}
}

func TestLoadRejectsMalformedDescriptor(t *testing.T) {
	for _, body := range []string{"schema: 0\nextension: cache\nconfig_key: extensions.cache\n", "schema: 1\nconfig_key: extensions.cache\n", "schema: 1\nextension: cache\nconfig_key: extensions.cache\nunknown: ignored\n"} {
		root := t.TempDir()

		dir := filepath.Join(root, "deploy", "descriptors")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, "cache.yaml"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}

		_, diags, err := Load(context.Background(), root, nil)
		if err != nil || !diags.HasErrors() {
			t.Fatalf("bad descriptor accepted: %v %v", diags, err)
		}
	}
}
func TestLoadHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := Load(ctx, t.TempDir(), nil); err == nil {
		t.Fatal("cancelled load succeeded")
	}
}

func TestDefaultObjectRecipeUsesAvailablePinnedImage(t *testing.T) {
	recipe, ok := Embedded().RecipeFor(model.ObjectStorage, "", nil)
	if !ok || recipe.ID != "rustfs-s3" || !strings.Contains(recipe.Image, "@sha256:") {
		t.Fatalf("default object recipe: %+v %v", recipe, ok)
	}
}
