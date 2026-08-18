package plugins

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
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

// go.mod's single-line form ("require module version") and the parenthesized block form
// "go mod tidy" itself writes are both real, common shapes; getGroveVersion has to
// recognize whichever one the user's project happens to be in.
func TestGetGroveVersionFindsSingleLineRequire(t *testing.T) {
	p, root := runnerPlugin(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.24\n\nrequire github.com/xraph/grove v1.6.0\n"),
		0o644,
	))

	version, err := p.getGroveVersion()
	require.NoError(t, err)
	assert.Equal(t, "v1.6.0", version)
}

func TestGetGroveVersionFindsBlockFormRequire(t *testing.T) {
	p, root := runnerPlugin(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte(`module example.com/app

go 1.24

require (
	github.com/some/other v1.0.0
	github.com/xraph/grove v1.5.4 // indirect
)
`),
		0o644,
	))

	version, err := p.getGroveVersion()
	require.NoError(t, err)
	assert.Equal(t, "v1.5.4", version)
}

// The scaffolded migrations package imports "grove/migrate", a subpackage of the grove
// module itself, so a real project that has ever run "forge db init" then "go mod tidy"
// will have this. A project that has not is exactly the case getGroveVersion must fail
// on cleanly, so buildMigrationRunner's caller can fall back to leaving grove unpinned.
func TestGetGroveVersionErrorsWhenAbsent(t *testing.T) {
	p, _ := runnerPlugin(t)

	_, err := p.getGroveVersion()
	assert.Error(t, err)
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

// The runner carries its own copy of the splitter, because it is emitted source
// and cannot import the CLI's. The two must agree, and the case that matters is
// the bare timestamp bun actually writes: a runner still built around the old
// "<version>_<name>" assumption skips every genuine row and exits zero.
func TestGenerateMigrationRunnerAdoptHandlesBareBunTimestamps(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(src)

	// The all-digits branch, and the synthesized display name it produces.
	assert.Contains(t, content, `return raw, "adopted_" + raw, true`)
	assert.Contains(t, content, "func allDigits(s string) bool")

	// The suffix stripping was dead logic: bun stores no filename to strip a
	// suffix from. It must not creep back in.
	assert.NotContains(t, content, `strings.TrimSuffix(raw, ".sql")`)
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

// Grove's uniqueness constraint is on (version, group) together, and
// ListApplied returns every group unscoped. Keying "already applied" on
// version alone would let one app's row mask another app's migration that
// happens to share a version, so the second app's adopt would skip
// recording its own copy -- and that migration would genuinely re-run on
// the next "forge db migrate".
func TestGenerateMigrationRunnerAdoptKeysKnownByVersionAndGroup(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(src)

	assert.Contains(t, content, "type versionGroup struct")
	assert.Contains(t, content, "known[versionGroup{version: a.Version, group: a.Group}] = true",
		"the known set must be built from both fields returned by ListApplied, not version alone")
	assert.Contains(t, content, "known[versionGroup{version: version, group: group}]",
		"the presence check must look up the pair for the group adopt is about to write, not version alone")
}

// A dry run that creates tables is not a dry run: EnsureMigrationTable and
// EnsureLockTable issue DDL, so they must not run ahead of the dryRun branch
// that decides whether anything gets written.
func TestGenerateMigrationRunnerAdoptDryRunSkipsTableCreation(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(src)

	// The "init" case earlier in the switch also calls EnsureMigrationTable
	// and EnsureLockTable unconditionally (that path has no dry-run concept),
	// so the search has to be scoped to the adopt case specifically or it
	// matches those unrelated calls instead.
	adoptIdx := strings.Index(content, `case "adopt":`)
	require.Greater(t, adoptIdx, -1)
	adoptCase := content[adoptIdx:]

	dryRunIdx := strings.Index(adoptCase, `os.Getenv("FORGE_ADOPT_DRY_RUN")`)
	guardIdx := strings.Index(adoptCase, "if !dryRun {")
	ensureMigrationIdx := strings.Index(adoptCase, "exec.EnsureMigrationTable(ctx)")
	ensureLockIdx := strings.Index(adoptCase, "exec.EnsureLockTable(ctx)")

	require.Greater(t, dryRunIdx, -1, "adopt must read FORGE_ADOPT_DRY_RUN")
	require.Greater(t, guardIdx, -1, "table creation must be guarded by a dryRun check")
	require.Greater(t, ensureMigrationIdx, -1)
	require.Greater(t, ensureLockIdx, -1)

	assert.True(t, dryRunIdx < guardIdx, "dryRun must be read before the guard that checks it")
	assert.True(t, guardIdx < ensureMigrationIdx, "EnsureMigrationTable must sit inside the !dryRun guard")
	assert.True(t, guardIdx < ensureLockIdx, "EnsureLockTable must sit inside the !dryRun guard")
}

// Next() returning false means "no more rows" OR "iteration failed
// partway through" -- Err() is the only way to tell those apart. Without
// checking it, a mid-read failure leaves the legacy slice as a silent
// partial list that adopt then reports as though it were the whole table.
func TestGenerateMigrationRunnerAdoptChecksRowsErr(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(src)

	assert.Contains(t, content, "rows.Err()")

	// The check has to happen after the read loop and before the legacy
	// table is treated as complete, or a partial read still gets adopted.
	loopIdx := strings.Index(content, "for rows.Next() {")
	errCheckIdx := strings.Index(content, "rows.Err()")
	emptyCheckIdx := strings.Index(content, `len(legacy) == 0`)

	require.Greater(t, loopIdx, -1)
	require.Greater(t, errCheckIdx, -1)
	require.Greater(t, emptyCheckIdx, -1)

	assert.True(t, loopIdx < errCheckIdx && errCheckIdx < emptyCheckIdx,
		"rows.Err() must be checked after the read loop and before legacy is trusted as complete")
}

// adopt's primary scenario is a project that ran bun migrations but never
// ran grove's init or migrate: the legacy table exists (so the earlier read
// succeeds) but grove's own tables do not. Outside dry-run,
// EnsureMigrationTable creates them first, so a ListApplied failure there is
// a genuine error. Under --dry-run, table creation is skipped on purpose, so
// ListApplied failing is the expected shape of exactly this scenario and
// must not be treated as fatal, or --dry-run is useless for the population
// adopt exists for.
func TestGenerateMigrationRunnerAdoptDryRunTreatsListAppliedFailureAsNoState(t *testing.T) {
	p, root := runnerPlugin(t)
	out := filepath.Join(root, "main.go")

	require.NoError(t, p.generateMigrationRunner(out, "postgres://localhost/db", ""))

	src, err := os.ReadFile(out)
	require.NoError(t, err)
	content := string(src)

	adoptIdx := strings.Index(content, `case "adopt":`)
	require.Greater(t, adoptIdx, -1)
	adoptCase := content[adoptIdx:]

	listAppliedIdx := strings.Index(adoptCase, "applied, err := exec.ListApplied(ctx)")
	dryRunGuardIdx := strings.Index(adoptCase, "if !dryRun {\n\t\t\t\tfmt.Fprintf(os.Stderr, \"failed to list applied migrations")
	noStateMsgIdx := strings.Index(adoptCase, "grove's migration tables do not exist yet")
	appliedNilIdx := strings.Index(adoptCase, "applied = nil")

	require.Greater(t, listAppliedIdx, -1)
	require.Greater(t, dryRunGuardIdx, -1,
		"the fatal exit on a ListApplied failure must itself be guarded by !dryRun")
	require.Greater(t, noStateMsgIdx, -1,
		"a dry run must say plainly that it found no grove state rather than fail silently")
	require.Greater(t, appliedNilIdx, -1,
		"a dry run must proceed with an empty applied set rather than exit")

	assert.True(t, listAppliedIdx < dryRunGuardIdx && dryRunGuardIdx < noStateMsgIdx && noStateMsgIdx < appliedNilIdx,
		"the !dryRun-guarded exit, the explanatory message, and the empty fallback must appear in that order after ListApplied")
}
