package auth

import (
	"os"
	"path/filepath"
	"testing"

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

	app := forge.New(forge.WithAppName("auth-file-test"), forge.WithConfigSearchPaths(dir))

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
	const file = "extensions:\n  auth:\n    default_provider: jwt\n" +
		"    providers:\n      - name: jwt\n        type: bearer\n        enabled: true\n"

	t.Run("file beats default", func(t *testing.T) {
		got := registerWithFile(t, file)
		if got.DefaultProvider != "jwt" {
			t.Fatalf("DefaultProvider = %q, want jwt from the file", got.DefaultProvider)
		}

		if len(got.Providers) != 1 || got.Providers[0].Name != "jwt" || !got.Providers[0].Enabled {
			t.Fatalf("Providers = %+v, want one enabled jwt provider from the file", got.Providers)
		}
	})

	t.Run("option beats file", func(t *testing.T) {
		got := registerWithFile(t, file, WithDefaultProvider("apikey"))
		if got.DefaultProvider != "apikey" {
			t.Fatalf("DefaultProvider = %q, want apikey from WithDefaultProvider", got.DefaultProvider)
		}
	})

	t.Run("file can disable the extension", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  auth:\n    enabled: false\n")
		if got.Enabled {
			t.Fatal("Enabled = true, want false from the file")
		}
	})
}
