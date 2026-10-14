package forge

import (
	"testing"
	"time"
)

// precedenceConfig mirrors the shape extensions use: a DefaultConfig with
// non-zero values, options over it, and a nested struct.
type precedenceConfig struct {
	Limit   int               `yaml:"limit"`
	Name    string            `yaml:"name"`
	Enabled bool              `yaml:"enabled"`
	Timeout time.Duration     `yaml:"timeout"`
	Tags    []string          `yaml:"tags"`
	Nested  precedenceNested  `yaml:"nested"`
	Labels  map[string]string `yaml:"labels"`
}

type precedenceNested struct {
	Retries int    `yaml:"retries"`
	Mode    string `yaml:"mode"`
}

func precedenceDefaults() precedenceConfig {
	return precedenceConfig{
		Limit:   20,
		Name:    "default",
		Enabled: true,
		Timeout: 5 * time.Second,
		Tags:    []string{"a"},
		Nested:  precedenceNested{Retries: 3, Mode: "fast"},
	}
}

type precedenceOption func(*precedenceConfig)

func withPrecedenceLimit(n int) precedenceOption {
	return func(c *precedenceConfig) { c.Limit = n }
}

func withPrecedenceEnabled(on bool) precedenceOption {
	return func(c *precedenceConfig) { c.Enabled = on }
}

func withPrecedenceRetries(n int) precedenceOption {
	return func(c *precedenceConfig) { c.Nested.Retries = n }
}

func withPrecedenceConfig(cfg precedenceConfig) precedenceOption {
	return func(c *precedenceConfig) { *c = cfg }
}

// precedenceExtension is built the way extensions/cache is: options applied
// over DefaultConfig in the constructor, then LoadConfig in Register.
type precedenceExtension struct {
	*BaseExtension

	config precedenceConfig
}

func newPrecedenceExtension(opts ...precedenceOption) *precedenceExtension {
	cfg := precedenceDefaults()
	for _, opt := range opts {
		opt(&cfg)
	}

	return &precedenceExtension{
		BaseExtension: NewBaseExtension("precedence", "0.0.0", "test"),
		config:        cfg,
	}
}

func (e *precedenceExtension) Register(app App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	final := precedenceDefaults()
	if err := e.LoadConfig("x", &final, e.config, precedenceDefaults(), false); err != nil {
		return err
	}

	e.config = final

	return nil
}

func registerPrecedence(t *testing.T, yaml string, opts ...precedenceOption) precedenceConfig {
	t.Helper()

	dir := t.TempDir()
	if yaml != "" {
		writeTemp(t, dir, "config.yaml", yaml)
	}

	app := New(WithAppName("precapp"), WithConfigSearchPaths(dir))

	ext := newPrecedenceExtension(opts...)
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}

	return ext.config
}

func TestExtensionConfigPrecedence(t *testing.T) {
	const fileLimit = "extensions:\n  x:\n    limit: 50\n"

	t.Run("file beats default", func(t *testing.T) {
		got := registerPrecedence(t, fileLimit)
		if got.Limit != 50 {
			t.Fatalf("Limit = %d, want 50 from the file", got.Limit)
		}
	})

	t.Run("option beats file", func(t *testing.T) {
		got := registerPrecedence(t, fileLimit, withPrecedenceLimit(70))
		if got.Limit != 70 {
			t.Fatalf("Limit = %d, want 70 from WithLimit", got.Limit)
		}
	})

	t.Run("default when neither", func(t *testing.T) {
		got := registerPrecedence(t, "")
		if got.Limit != 20 {
			t.Fatalf("Limit = %d, want the default 20", got.Limit)
		}
	})

	t.Run("fields the file leaves out keep their defaults", func(t *testing.T) {
		got := registerPrecedence(t, fileLimit)

		want := precedenceDefaults()
		want.Limit = 50

		if got.Name != want.Name || !got.Enabled || got.Timeout != want.Timeout ||
			got.Nested != want.Nested || len(got.Tags) != 1 || got.Tags[0] != "a" {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("file can turn a true default off", func(t *testing.T) {
		got := registerPrecedence(t, "extensions:\n  x:\n    enabled: false\n")
		if got.Enabled {
			t.Fatal("Enabled = true, want false from the file")
		}
	})

	t.Run("nested file value beats nested default", func(t *testing.T) {
		got := registerPrecedence(t, "extensions:\n  x:\n    nested:\n      mode: slow\n")
		if got.Nested.Mode != "slow" || got.Nested.Retries != 3 {
			t.Fatalf("Nested = %+v, want {Retries:3 Mode:slow}", got.Nested)
		}
	})

	t.Run("nested option beats nested file value", func(t *testing.T) {
		got := registerPrecedence(t,
			"extensions:\n  x:\n    nested:\n      retries: 9\n      mode: slow\n",
			withPrecedenceRetries(7))
		if got.Nested.Retries != 7 || got.Nested.Mode != "slow" {
			t.Fatalf("Nested = %+v, want {Retries:7 Mode:slow}", got.Nested)
		}
	})

	t.Run("legacy top-level key", func(t *testing.T) {
		got := registerPrecedence(t, "x:\n  limit: 50\n")
		if got.Limit != 50 {
			t.Fatalf("Limit = %d, want 50 from the legacy key", got.Limit)
		}
	})

	t.Run("WithConfig of a zero struct still defers to the file", func(t *testing.T) {
		got := registerPrecedence(t, fileLimit, withPrecedenceConfig(precedenceConfig{}))
		if got.Limit != 50 {
			t.Fatalf("Limit = %d, want 50 from the file", got.Limit)
		}
	})

	t.Run("an option equal to its default defers to the file", func(t *testing.T) {
		got := registerPrecedence(t, fileLimit, withPrecedenceLimit(20))
		if got.Limit != 50 {
			t.Fatalf("Limit = %d, want 50 from the file", got.Limit)
		}
	})
}

// An option that sets a field to its zero value over a non-zero default has to
// beat the file, the same way any other option does.
func TestExtensionConfigPrecedenceZeroOptions(t *testing.T) {
	t.Run("false option beats a file that says true", func(t *testing.T) {
		got := registerPrecedence(t, "extensions:\n  x:\n    enabled: true\n", withPrecedenceEnabled(false))
		if got.Enabled {
			t.Fatal("Enabled = true, want false from the option")
		}
	})

	t.Run("false option beats a true default when the file omits the key", func(t *testing.T) {
		got := registerPrecedence(t, "extensions:\n  x:\n    limit: 50\n", withPrecedenceEnabled(false))
		if got.Enabled {
			t.Fatal("Enabled = true, want false from the option")
		}

		if got.Limit != 50 {
			t.Fatalf("Limit = %d, want 50 from the file", got.Limit)
		}
	})

	t.Run("false option with no file", func(t *testing.T) {
		got := registerPrecedence(t, "", withPrecedenceEnabled(false))
		if got.Enabled {
			t.Fatal("Enabled = true, want false from the option")
		}
	})

	t.Run("zero int option beats file and default", func(t *testing.T) {
		got := registerPrecedence(t, "extensions:\n  x:\n    limit: 50\n", withPrecedenceLimit(0))
		if got.Limit != 0 {
			t.Fatalf("Limit = %d, want 0 from the option", got.Limit)
		}
	})

	t.Run("nested zero option beats nested file value", func(t *testing.T) {
		got := registerPrecedence(t,
			"extensions:\n  x:\n    nested:\n      retries: 9\n      mode: slow\n",
			withPrecedenceRetries(0))
		if got.Nested.Retries != 0 || got.Nested.Mode != "slow" {
			t.Fatalf("Nested = %+v, want {Retries:0 Mode:slow}", got.Nested)
		}
	})

	t.Run("partial WithConfig is taken as written", func(t *testing.T) {
		got := registerPrecedence(t,
			"extensions:\n  x:\n    name: file\n    labels:\n      k: v\n",
			withPrecedenceConfig(precedenceConfig{Limit: 5}))

		// Labels has no default, so its zero value defers to the file. Every
		// other field differs from its default and comes from the option.
		if got.Limit != 5 || got.Name != "" || got.Enabled || got.Timeout != 0 ||
			got.Tags != nil || got.Nested != (precedenceNested{}) || got.Labels["k"] != "v" {
			t.Fatalf("got %+v, want the WithConfig struct plus the file's labels", got)
		}
	})
}
