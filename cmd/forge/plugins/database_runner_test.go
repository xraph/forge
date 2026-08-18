package plugins

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/cmd/forge/config"
)

// runnerPlugin builds a DatabasePlugin rooted at a temp project with a go.mod,
// which generateMigrationRunner reads for the module name.
func runnerPlugin(t *testing.T) (*DatabasePlugin, string) {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.24\n"),
		0o644,
	))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "migrations"), 0o755))

	return &DatabasePlugin{config: &config.ForgeConfig{RootDir: root}}, root
}

func TestGenerateMigrationRunnerParsesForEveryScheme(t *testing.T) {
	dsns := map[string]string{
		"postgres":   "postgres://localhost/db",
		"mysql":      "mysql://localhost/db",
		"sqlite":     "sqlite://./app.db",
		"mongodb":    "mongodb://localhost/db",
		"clickhouse": "clickhouse://localhost/db",
		"turso":      "turso://token@host",
	}

	for scheme, dsn := range dsns {
		t.Run(scheme, func(t *testing.T) {
			p, root := runnerPlugin(t)
			out := filepath.Join(root, "main.go")

			require.NoError(t, p.generateMigrationRunner(out, dsn, ""))

			src, err := os.ReadFile(out)
			require.NoError(t, err)

			_, err = parser.ParseFile(token.NewFileSet(), out, src, parser.AllErrors)
			require.NoError(t, err, "generated runner does not parse:\n%s", src)

			drv, err := resolveGroveDriver(dsn)
			require.NoError(t, err)

			content := string(src)
			assert.Contains(t, content, drv.Module, "runner must import the driver module")
			assert.Contains(t, content, drv.MigrateImport, "runner must import the migrate subpackage or grove has no executor")
			assert.Contains(t, content, "migrate.NewOrchestrator")
			assert.Contains(t, content, "grove.OpenDriver")
		})
	}
}

// The whole point of the change: no bun anywhere in the emitted runner.
func TestGenerateMigrationRunnerHasNoBun(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)

	assert.NotContains(t, string(src), "uptrace/bun")
	assert.NotContains(t, string(src), "extensions/database")
}

func TestGenerateMigrationRunnerRejectsBadDSN(t *testing.T) {
	p, root := runnerPlugin(t)

	err := p.generateMigrationRunner(filepath.Join(root, "main.go"), "cassandra://localhost", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cassandra")
}

// The runner has to understand every subcommand the CLI will send it, or the
// CLI reports success for a command the binary silently ignored.
func TestGenerateMigrationRunnerHandlesEverySubcommand(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(src)

	for _, command := range []string{"migrate", "rollback", "status", "init", "lock", "unlock", "mark-applied"} {
		assert.Contains(t, content, `case "`+command+`"`, "runner does not handle %q", command)
	}
}

func TestGenerateMigrationRunnerEmitsAdopt(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(src)

	assert.Contains(t, content, `case "adopt"`)

	// adopt reads the old bun table directly through the executor, because
	// grove knows nothing about it.
	assert.Contains(t, content, "bun_migrations")
	assert.Contains(t, content, "exec.ListApplied")
	assert.Contains(t, content, "exec.RecordApplied")

	// Refusing on a missing old table is the whole safety property: quietly
	// succeeding on a fresh database tells an operator they are covered when
	// nothing was adopted.
	assert.Contains(t, content, "nothing to adopt")
}

func TestAdoptIsRegisteredAsASubcommand(t *testing.T) {
	p := &DatabasePlugin{}

	var found bool
	for _, cmd := range p.Commands() {
		for _, sub := range cmd.Subcommands() {
			if sub.Name() == "adopt" {
				found = true
			}
		}
	}

	assert.True(t, found, "forge db adopt must be registered")
}
