package security

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

	app := forge.New(forge.WithAppName("security-file-test"), forge.WithConfigSearchPaths(dir))

	ext := NewExtension(opts...)
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}

	return ext.config
}

func TestConfigFromFile(t *testing.T) {
	const key = "file-test-signing-key-0123456789"

	t.Run("snake_case keys inside every section", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  security:\n"+
			"    jwt:\n      signing_key: "+key+"\n      issuer: orders\n"+
			"    cors:\n      allow_origins: [\"https://app.example.com\"]\n      max_age: 600\n"+
			"    rate_limit:\n      requests_per_window: 5\n"+
			"    session:\n      cookie_name: sid\n")

		if got.JWT.SigningKey != key || got.JWT.Issuer != "orders" {
			t.Fatalf("JWT = %+v", got.JWT)
		}

		if len(got.CORS.AllowOrigins) != 1 || got.CORS.AllowOrigins[0] != "https://app.example.com" || got.CORS.MaxAge != 600 {
			t.Fatalf("CORS = %+v", got.CORS)
		}

		if got.RateLimit.RequestsPerWindow != 5 {
			t.Fatalf("RateLimit.RequestsPerWindow = %d, want 5", got.RateLimit.RequestsPerWindow)
		}

		if got.Session.CookieName != "sid" || got.Session.TTL != DefaultConfig().Session.TTL {
			t.Fatalf("Session = %+v", got.Session)
		}
	})

	t.Run("Go field names still apply", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  security:\n    jwt:\n      SigningKey: "+key+"\n      Issuer: orders\n")
		if got.JWT.SigningKey != key || got.JWT.Issuer != "orders" {
			t.Fatalf("JWT = %+v", got.JWT)
		}
	})

	t.Run("option beats file", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  security:\n    jwt:\n      signing_key: "+key+"\n"+
			"    cors:\n      allow_origins: [\"https://file.example.com\"]\n",
			WithCORSOrigins([]string{"https://code.example.com"}))
		if len(got.CORS.AllowOrigins) != 1 || got.CORS.AllowOrigins[0] != "https://code.example.com" {
			t.Fatalf("CORS.AllowOrigins = %v, want the origin from WithCORSOrigins", got.CORS.AllowOrigins)
		}
	})

	t.Run("file can disable the extension", func(t *testing.T) {
		got := registerWithFile(t, "extensions:\n  security:\n    enabled: false\n")
		if got.Enabled {
			t.Fatal("Enabled = true, want false from the file")
		}
	})
}
