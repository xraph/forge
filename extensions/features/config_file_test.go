package features

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xraph/forge"
)

// registerWithFile registers the extension against an app that reads yaml
// from a config file, and returns the config Register settled on.
func registerWithFile(t *testing.T, yaml string, opts ...ConfigOption) Config {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	app := forge.New(forge.WithAppName("features-file-test"), forge.WithConfigSearchPaths(dir))

	ext, ok := NewExtension(opts...).(*Extension)
	if !ok {
		t.Fatal("NewExtension did not return *Extension")
	}

	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}

	return ext.config
}

func TestConfigFromFile(t *testing.T) {
	const file = "extensions:\n  features:\n    provider: local\n    refresh_interval: 1m\n" +
		"    local:\n      flags:\n        dark-mode:\n          key: dark-mode\n          enabled: true\n"

	t.Run("file beats default", func(t *testing.T) {
		got := registerWithFile(t, file)
		if got.RefreshInterval != time.Minute {
			t.Fatalf("RefreshInterval = %v, want 1m from the file", got.RefreshInterval)
		}

		if flag, ok := got.Local.Flags["dark-mode"]; !ok || !flag.Enabled {
			t.Fatalf("Local.Flags = %+v, want dark-mode enabled from the file", got.Local.Flags)
		}
	})

	t.Run("option beats file", func(t *testing.T) {
		got := registerWithFile(t, file, WithRefreshInterval(2*time.Minute))
		if got.RefreshInterval != 2*time.Minute {
			t.Fatalf("RefreshInterval = %v, want 2m from WithRefreshInterval", got.RefreshInterval)
		}
	})

	t.Run("file can disable the extension", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  features:\n    enabled: false\n")
		if got.Enabled {
			t.Fatal("Enabled = true, want false from the file")
		}
	})

	t.Run("require config fails without a section", func(t *testing.T) {
		dir := t.TempDir()
		app := forge.New(forge.WithAppName("features-file-test"), forge.WithConfigSearchPaths(dir))

		if err := NewExtension(WithRequireConfig(true)).Register(app); err == nil {
			t.Fatal("Register succeeded with WithRequireConfig(true) and no features section")
		}
	})
}
