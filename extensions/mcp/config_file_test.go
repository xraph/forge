package mcp

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

	app := forge.New(forge.WithAppName("mcp-file-test"), forge.WithConfigSearchPaths(dir))

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
		got := registerWithFile(t, "extensions:\n  mcp:\n    base_path: /tools\n    tool_prefix: shop_\n    max_tool_name_length: 32\n")
		if got.BasePath != "/tools" || got.ToolPrefix != "shop_" || got.MaxToolNameLength != 32 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("Go field names still apply", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  mcp:\n    BasePath: /tools\n    ToolPrefix: shop_\n")
		if got.BasePath != "/tools" || got.ToolPrefix != "shop_" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("option beats file", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  mcp:\n    base_path: /tools\n", WithBasePath("/agents"))
		if got.BasePath != "/agents" {
			t.Fatalf("BasePath = %q, want /agents from WithBasePath", got.BasePath)
		}
	})
}
