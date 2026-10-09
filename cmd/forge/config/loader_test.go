package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadForgeConfigStopsOnInvalidFile(t *testing.T) {
	root := t.TempDir()

	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), []byte("project: {name: parent}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(child, ".forge.yml")
	if err := os.WriteFile(file, []byte("project: [\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(child)

	_, _, err := LoadForgeConfig()
	if err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("invalid local file must stop search: %v", err)
	}
}
func TestLoadForgeConfigAmbiguous(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".forge.yml", ".forge.yaml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("project: {name: a}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(root)

	if _, _, err := LoadForgeConfig(); err == nil {
		t.Fatal("ambiguous files accepted")
	}
}
func TestLoadForgeConfigV2KeepsProjectAndBuild(t *testing.T) {
	root := t.TempDir()

	body := "project: {name: atlas}\nbuild: {apps: [{name: api, cmd: ./cmd/api}]}\ndeploy: {version: 2, environments: {dev: {target: local}}}\n"
	if err := os.WriteFile(filepath.Join(root, ".forge.yml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)

	cfg, _, err := LoadForgeConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Project.Name != "atlas" || len(cfg.Build.Apps) != 1 {
		t.Fatalf("project data missing: %+v", cfg)
	}
}

func TestSaveForgeConfigPreservesVersionTwoDeploy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".forge.yml")

	body := "project: {name: atlas}\ncustom: retained\ndeploy:\n  version: 2\n  targets: {local: {provider: compose}} # keep target\n  environments: {dev: {target: local}}\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForgeConfigFrom(root)
	if err != nil {
		t.Fatal(err)
	}

	cfg.Project.Name = "renamed"
	if err := SaveForgeConfig(cfg, path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"version: 2", "provider: compose", "keep target", "custom: retained", "renamed"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("lost %q: %s", want, data)
		}
	}
}
