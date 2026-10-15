package forge

import (
	"reflect"
	"testing"
	"time"
)

// legacyConfig has snake_case tags on fields whose Go names used to be the
// YAML keys, the way mcp and cache did before they were tagged.
type legacyConfig struct {
	ToolPrefix string        `yaml:"tool_prefix"`
	MaxSize    int           `yaml:"max_size"`
	DefaultTTL time.Duration `yaml:"default_ttl"`
	Enabled    bool          `yaml:"enabled"`
	JWT        legacyJWT     `yaml:"jwt"`
	Untagged   string
}

type legacyJWT struct {
	SigningKey string `yaml:"signing_key"`
	Issuer     string `yaml:"issuer"`
}

func loadLegacy(t *testing.T, yaml string) legacyConfig {
	t.Helper()

	dir := t.TempDir()
	writeTemp(t, dir, "config.yaml", yaml)

	app := New(WithAppName("legacyapp"), WithConfigSearchPaths(dir))

	cfg := legacyConfig{MaxSize: 10, Enabled: true}
	if err := NewExtensionConfigLoader(app, nil).LoadConfig("x", &cfg, nil, nil, false); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	return cfg
}

func TestExtensionConfigLegacyKeys(t *testing.T) {
	t.Run("Go field names still apply", func(t *testing.T) {
		got := loadLegacy(t, "extensions:\n  x:\n    ToolPrefix: shop_\n    MaxSize: 50\n    DefaultTTL: 10m\n    Enabled: false\n")

		want := legacyConfig{ToolPrefix: "shop_", MaxSize: 50, DefaultTTL: 10 * time.Minute}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("tag names apply", func(t *testing.T) {
		got := loadLegacy(t, "extensions:\n  x:\n    tool_prefix: shop_\n    max_size: 50\n")
		if got.ToolPrefix != "shop_" || got.MaxSize != 50 || !got.Enabled {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("tag name wins when both are set", func(t *testing.T) {
		got := loadLegacy(t, "extensions:\n  x:\n    MaxSize: 50\n    max_size: 70\n")
		if got.MaxSize != 70 {
			t.Fatalf("MaxSize = %d, want 70 from max_size", got.MaxSize)
		}
	})

	t.Run("nested Go field names apply", func(t *testing.T) {
		got := loadLegacy(t, "extensions:\n  x:\n    jwt:\n      SigningKey: k\n      issuer: forge\n")
		if got.JWT.SigningKey != "k" || got.JWT.Issuer != "forge" {
			t.Fatalf("JWT = %+v", got.JWT)
		}
	})

	t.Run("legacy top-level key", func(t *testing.T) {
		got := loadLegacy(t, "x:\n  ToolPrefix: shop_\n")
		if got.ToolPrefix != "shop_" {
			t.Fatalf("ToolPrefix = %q", got.ToolPrefix)
		}
	})

	t.Run("untagged field still binds by Go name", func(t *testing.T) {
		got := loadLegacy(t, "extensions:\n  x:\n    Untagged: plain\n")
		if got.Untagged != "plain" {
			t.Fatalf("Untagged = %q", got.Untagged)
		}
	})
}

func TestRenameLegacyKeys(t *testing.T) {
	section := map[string]any{
		"ToolPrefix": "shop_",
		"MaxSize":    50,
		"max_size":   70,
		"enabled":    true,
		"Untagged":   "yes",
		"jwt":        map[string]any{"SigningKey": "k"},
	}

	renamed, legacy := renameLegacyKeys(section, reflect.TypeFor[legacyConfig](), "extensions.x.")

	want := []legacyKey{
		{old: "extensions.x.ToolPrefix", new: "extensions.x.tool_prefix"},
		{old: "extensions.x.MaxSize", new: "extensions.x.max_size"},
		{old: "extensions.x.jwt.SigningKey", new: "extensions.x.jwt.signing_key"},
	}
	if !reflect.DeepEqual(legacy, want) {
		t.Fatalf("legacy = %+v, want %+v", legacy, want)
	}

	if renamed["tool_prefix"] != "shop_" || renamed["max_size"] != 70 || renamed["Untagged"] != "yes" {
		t.Fatalf("renamed = %+v", renamed)
	}

	if _, ok := renamed["ToolPrefix"]; ok {
		t.Fatal("renamed still holds ToolPrefix")
	}

	if jwt, _ := renamed["jwt"].(map[string]any); jwt["signing_key"] != "k" {
		t.Fatalf("renamed jwt = %+v", renamed["jwt"])
	}

	if section["ToolPrefix"] != "shop_" {
		t.Fatal("renameLegacyKeys modified the section it was given")
	}

	if _, legacy := renameLegacyKeys(map[string]any{"tool_prefix": "x"}, reflect.TypeFor[legacyConfig](), ""); legacy != nil {
		t.Fatalf("legacy = %+v for a section with no old keys", legacy)
	}
}
