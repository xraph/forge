package cache

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

	app := forge.New(forge.WithAppName("cache-file-test"), forge.WithConfigSearchPaths(dir))

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
	t.Run("snake_case keys", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  cache:\n    max_size: 50000\n    default_ttl: 10m\n    prefix: \"orders:\"\n")
		if got.MaxSize != 50000 || got.DefaultTTL.String() != "10m0s" || got.Prefix != "orders:" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("Go field names still apply", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  cache:\n    MaxSize: 50000\n    DefaultTTL: 10m\n")
		if got.MaxSize != 50000 || got.DefaultTTL.String() != "10m0s" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("option beats file", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  cache:\n    max_size: 50000\n", WithMaxSize(7))
		if got.MaxSize != 7 {
			t.Fatalf("MaxSize = %d, want 7 from WithMaxSize", got.MaxSize)
		}
	})
}
