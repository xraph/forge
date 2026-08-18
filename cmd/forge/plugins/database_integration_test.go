//go:build integration

// These tests build and execute the generated migration runner against a real
// sqlite database. They need the network, because the runner has its own
// go.mod and `go mod tidy` has to resolve grove.
//
// Run them with: go test -tags integration ./cmd/forge/plugins/ -run TestRunnerEndToEnd

package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/cmd/forge/config"
)

// newTestConfig builds the config type DatabasePlugin actually holds, rooted at a temp
// project directory, the same way runnerPlugin does in database_runner_test.go. It is
// not config.Config: DatabasePlugin.config is *config.ForgeConfig.
//
// MigrationsPath is set explicitly to "migrations" rather than left at its zero value.
// DatabaseConfig.GetMigrationsPath defaults to "./database/migrations" when unset, which
// is where generateMigrationRunner would compute the import path from; buildRunner below
// writes the scaffolded package into "<root>/migrations" instead, matching what a real
// "forge db init" lays out under RootDir when this field is left to the caller's config.
// Without setting it here, the generated runner would import a package one directory
// away from where the files actually live, and "go build" would fail on that mismatch
// alone -- a gap the parser-only tests in database_runner_test.go never exercised, since
// they only check that the generated source parses, not that it points at real files.
func newTestConfig(root string) *config.ForgeConfig {
	return &config.ForgeConfig{
		RootDir: root,
		Database: config.DatabaseConfig{
			MigrationsPath: "migrations",
		},
	}
}

// seedLegacyTable opens the sqlite file directly (bypassing grove entirely) and writes a
// bun_migrations table containing one row per name. This is what the old bun-based
// tooling would have left behind, and it is the only way to get such a table into the
// database: grove's own runner never creates one.
//
// This shells out to the sqlite3 CLI rather than importing a Go sqlite driver into this
// module. modernc.org/sqlite is already a transitive dependency of grove's sqlitedriver,
// but it is only reachable from this file, which is excluded from every build that does
// not pass -tags integration; a plain "go mod tidy" (the form most contributors run)
// does not see this file at all and would delete the requirement it added, breaking the
// next integration run until someone notices and re-adds it by hand. The CLI has no such
// footprint on go.mod/go.sum.
func seedLegacyTable(t *testing.T, dbPath string, names []string) {
	t.Helper()

	var script strings.Builder
	script.WriteString("CREATE TABLE bun_migrations (name TEXT);\n")

	for _, name := range names {
		// The only place the test-controlled names could break the script is a
		// literal single quote; double it, which is how sqlite escapes one
		// inside a quoted string literal.
		script.WriteString("INSERT INTO bun_migrations (name) VALUES ('" + strings.ReplaceAll(name, "'", "''") + "');\n")
	}

	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(script.String())

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// buildRunner generates, builds and returns the path to a runner binary for a
// scratch project containing one create-sql migration.
func buildRunner(t *testing.T) (binary, dsn string) {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.24\n"),
		0o644,
	))

	migrationsDir := filepath.Join(root, "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0o755))

	p := &DatabasePlugin{config: newTestConfig(root)}

	require.NoError(t, p.createMigrationsGoFile(filepath.Join(migrationsDir, "migrations.go"), ""))

	up, _, _, err := writeSQLMigrationFiles(migrationsDir, "create_widgets", false)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(up, []byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644))

	dsn = "sqlite://" + filepath.Join(root, "app.db")

	binary, err = p.buildMigrationRunner(dsn, "")
	require.NoError(t, err, "the runner must build")

	return binary, dsn
}

func runCommand(t *testing.T, binary, dsn, command string, env ...string) (string, error) {
	t.Helper()

	cmd := exec.Command(binary, command)
	cmd.Env = append(os.Environ(), "DATABASE_URL="+dsn)
	cmd.Env = append(cmd.Env, env...)

	out, err := cmd.CombinedOutput()

	return string(out), err
}

func TestRunnerEndToEndMigrateStatusRollback(t *testing.T) {
	binary, dsn := buildRunner(t)

	out, err := runCommand(t, binary, dsn, "migrate")
	require.NoError(t, err, out)

	out, err = runCommand(t, binary, dsn, "status")
	require.NoError(t, err, out)
	assert.Contains(t, out, "create_widgets")

	out, err = runCommand(t, binary, dsn, "rollback")
	require.NoError(t, err, out)
}

func TestRunnerEndToEndAdoptRequiresTheLegacyTable(t *testing.T) {
	binary, dsn := buildRunner(t)

	// Nothing has ever written a bun table here. Adopt must refuse rather than
	// report success, because a quiet success is what convinces an operator
	// they are covered when nothing was adopted.
	out, err := runCommand(t, binary, dsn, "adopt")
	require.Error(t, err, out)
	assert.Contains(t, out, "nothing to adopt")
}

func TestRunnerEndToEndAdoptIsIdempotent(t *testing.T) {
	binary, dsn := buildRunner(t)

	// Seed a legacy table the way the old tooling would have left it.
	seed, err := runCommand(t, binary, dsn, "init")
	require.NoError(t, err, seed)

	dbPath := dsn[len("sqlite://"):]
	seedLegacyTable(t, dbPath, []string{"20240115120000_create_widgets"})

	first, err := runCommand(t, binary, dsn, "adopt")
	require.NoError(t, err, first)
	assert.Contains(t, first, "adopted 1")

	second, err := runCommand(t, binary, dsn, "adopt")
	require.NoError(t, err, second)
	assert.Contains(t, second, "adopted 0")
	assert.Contains(t, second, "already present 1")
}
