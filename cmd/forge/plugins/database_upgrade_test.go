package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
)

// The fakes below exist because cli.CommandContext is only constructed by the
// unexported newCommandContext, so a test cannot get a real one. Everything is a
// stub except flags and the output calls, which are recorded so a test can assert
// on what the user was told.

type fakeFlagValue struct {
	str     string
	boolean bool
}

func (f fakeFlagValue) String() string          { return f.str }
func (f fakeFlagValue) Int() int                { return 0 }
func (f fakeFlagValue) Bool() bool              { return f.boolean }
func (f fakeFlagValue) StringSlice() []string   { return nil }
func (f fakeFlagValue) Duration() time.Duration { return 0 }
func (f fakeFlagValue) IsSet() bool             { return f.str != "" || f.boolean }
func (f fakeFlagValue) Raw() any                { return f.str }

type fakeContext struct {
	strings map[string]string
	bools   map[string]bool
	output  []string
}

func newFakeContext() *fakeContext {
	return &fakeContext{strings: map[string]string{}, bools: map[string]bool{}}
}

func (c *fakeContext) Args() []string { return nil }
func (c *fakeContext) Arg(int) string { return "" }
func (c *fakeContext) NArgs() int     { return 0 }
func (c *fakeContext) Flag(name string) cli.FlagValue {
	return fakeFlagValue{str: c.strings[name], boolean: c.bools[name]}
}
func (c *fakeContext) String(name string) string                                      { return c.strings[name] }
func (c *fakeContext) Int(string) int                                                 { return 0 }
func (c *fakeContext) Bool(name string) bool                                          { return c.bools[name] }
func (c *fakeContext) StringSlice(string) []string                                    { return nil }
func (c *fakeContext) Duration(string) int64                                          { return 0 }
func (c *fakeContext) Println(a ...any)                                               { c.record(a...) }
func (c *fakeContext) Printf(format string, a ...any)                                 { c.record(format) }
func (c *fakeContext) Error(err error)                                                { c.output = append(c.output, err.Error()) }
func (c *fakeContext) Success(msg string)                                             { c.output = append(c.output, msg) }
func (c *fakeContext) Warning(msg string)                                             { c.output = append(c.output, msg) }
func (c *fakeContext) Info(msg string)                                                { c.output = append(c.output, msg) }
func (c *fakeContext) Prompt(string) (string, error)                                  { return "", nil }
func (c *fakeContext) Confirm(string) (bool, error)                                   { return false, nil }
func (c *fakeContext) Select(string, []string) (string, error)                        { return "", nil }
func (c *fakeContext) MultiSelect(string, []string) ([]string, error)                 { return nil, nil }
func (c *fakeContext) SelectAsync(string, cli.OptionsLoader) (string, error)          { return "", nil }
func (c *fakeContext) MultiSelectAsync(string, cli.OptionsLoader) ([]string, error)   { return nil, nil }
func (c *fakeContext) SelectWithRetry(string, cli.OptionsLoader, int) (string, error) { return "", nil }
func (c *fakeContext) MultiSelectWithRetry(string, cli.OptionsLoader, int) ([]string, error) {
	return nil, nil
}
func (c *fakeContext) ProgressBar(int) cli.ProgressBar { return nil }
func (c *fakeContext) Spinner(string) cli.Spinner      { return nil }
func (c *fakeContext) Table() cli.TableWriter          { return nil }
func (c *fakeContext) Context() context.Context        { return context.Background() }
func (c *fakeContext) App() forge.App                  { return nil }
func (c *fakeContext) Command() cli.Command            { return nil }
func (c *fakeContext) Logger() *cli.CLILogger          { return nil }

func (c *fakeContext) record(a ...any) {
	for _, v := range a {
		if s, ok := v.(string); ok {
			c.output = append(c.output, s)
		}
	}
}

func (c *fakeContext) said(fragment string) bool {
	for _, line := range c.output {
		if strings.Contains(line, fragment) {
			return true
		}
	}

	return false
}

// legacyMigrationsGo is the migrations.go the CLI wrote before the grove
// migration, reduced to the two lines that identify it. A project created
// against any released Forge before this branch has this file on disk.
const legacyMigrationsGo = `package migrations

import (
	"sync"

	"github.com/xraph/forge/extensions/database"
)

// Migrations is the application's migration collection
var Migrations = database.Migrations

var (
	discoveryOnce sync.Once
	discoveryErr  error
)
`

// upgradePlugin builds a plugin rooted at a temp project whose migrations
// directory already holds the given migrations.go content.
func upgradePlugin(t *testing.T, migrationsGo string) (*DatabasePlugin, string) {
	t.Helper()

	root := t.TempDir()
	migrationsDir := filepath.Join(root, "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0o755))

	if migrationsGo != "" {
		require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "migrations.go"), []byte(migrationsGo), 0o644))
	}

	p := &DatabasePlugin{config: &config.ForgeConfig{
		RootDir:  root,
		Database: config.DatabaseConfig{MigrationsPath: "migrations"},
	}}

	return p, migrationsDir
}

func TestIsLegacyMigrationsScaffold(t *testing.T) {
	assert.True(t, isLegacyMigrationsScaffold(legacyMigrationsGo))

	// The current scaffold must never be mistaken for the old one, or every
	// "forge db init" would silently overwrite a file the user may have edited.
	p := &DatabasePlugin{}
	dir := t.TempDir()
	path := filepath.Join(dir, "migrations.go")
	require.NoError(t, p.createMigrationsGoFile(path, ""))

	current, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.False(t, isLegacyMigrationsScaffold(string(current)))

	// One signal alone is not enough. Someone may import the extension for a
	// reason unrelated to migrations, and rewriting their file would lose work.
	assert.False(t, isLegacyMigrationsScaffold(`import "github.com/xraph/forge/extensions/database"`))
	assert.False(t, isLegacyMigrationsScaffold(`var x = database.Migrations`))
}

// Before this fix, a project that predated the branch hit a compile error from
// the generated runner on every db command, with nothing pointing at the cause.
func TestCheckLegacyMigrationsScaffoldExplainsTheUpgrade(t *testing.T) {
	p, _ := upgradePlugin(t, legacyMigrationsGo)

	err := p.checkLegacyMigrationsScaffold("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forge db init")
	assert.Contains(t, err.Error(), "forge db adopt")
	assert.Contains(t, err.Error(), "migrations.go")
}

func TestCheckLegacyMigrationsScaffoldPassesForTheGroveScaffold(t *testing.T) {
	p, dir := upgradePlugin(t, "")

	require.NoError(t, p.createMigrationsGoFile(filepath.Join(dir, "migrations.go"), ""))
	assert.NoError(t, p.checkLegacyMigrationsScaffold(""))
}

// A project with no migrations.go at all is a normal pre-init state, not a
// legacy one, and must not be blocked.
func TestCheckLegacyMigrationsScaffoldPassesWhenAbsent(t *testing.T) {
	p, _ := upgradePlugin(t, "")

	assert.NoError(t, p.checkLegacyMigrationsScaffold(""))
}

// This is the path an upgrader actually walks. "already exists" used to be the
// end of the road: init changed nothing, and every other command died in the
// build. The rewrite has to happen before init reaches for a DSN, so it still
// happens on a machine with no database running.
func TestInitRewritesTheLegacyScaffold(t *testing.T) {
	p, dir := upgradePlugin(t, legacyMigrationsGo)

	ctx := newFakeContext()

	// No DSN anywhere, so init fails at its table-creation step. The scaffold
	// rewrite is deliberately ordered before that.
	t.Setenv("DATABASE_URL", "")

	err := p.initMigrations(ctx)
	require.Error(t, err)

	rewritten, readErr := os.ReadFile(filepath.Join(dir, "migrations.go"))
	require.NoError(t, readErr)

	assert.Contains(t, string(rewritten), "migrate.NewMigrationRegistry()")
	assert.NotContains(t, string(rewritten), "extensions/database")

	assert.True(t, ctx.said("Rewrote"), "init must say it rewrote the file, not stay silent: %v", ctx.output)
	assert.True(t, ctx.said("forge db adopt"), "init must point at adopt, since the database may already be migrated: %v", ctx.output)
}

// A grove scaffold is left alone unless --force says otherwise.
func TestInitLeavesACurrentScaffoldAlone(t *testing.T) {
	p, dir := upgradePlugin(t, "")

	path := filepath.Join(dir, "migrations.go")
	require.NoError(t, p.createMigrationsGoFile(path, ""))
	require.NoError(t, os.WriteFile(path, []byte("// hand edit\n"+mustRead(t, path)), 0o644))

	before := mustRead(t, path)

	t.Setenv("DATABASE_URL", "")

	ctx := newFakeContext()
	require.Error(t, p.initMigrations(ctx))

	assert.Equal(t, before, mustRead(t, path))
	assert.True(t, ctx.said("already exists"), "%v", ctx.output)
}

func TestInitForceRewritesEvenACurrentScaffold(t *testing.T) {
	p, dir := upgradePlugin(t, "")

	path := filepath.Join(dir, "migrations.go")
	require.NoError(t, p.createMigrationsGoFile(path, ""))
	require.NoError(t, os.WriteFile(path, []byte("// hand edit\n"+mustRead(t, path)), 0o644))

	t.Setenv("DATABASE_URL", "")

	ctx := newFakeContext()
	ctx.bools["force"] = true
	require.Error(t, p.initMigrations(ctx))

	assert.NotContains(t, mustRead(t, path), "// hand edit")
}

func mustRead(t *testing.T, path string) string {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(src)
}

// --type has had no reader since the driver started coming from the DSN scheme.
// Accepting it silently is the same defect the --dsn threading fix removed.
func TestTypeFlagIsRejectedRatherThanIgnored(t *testing.T) {
	ctx := newFakeContext()
	assert.NoError(t, rejectRemovedTypeFlag(ctx), "an unset --type must not block anything")

	ctx.strings["type"] = "sqlite"

	err := rejectRemovedTypeFlag(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--type")
	// The message has to say what to do instead, not just what stopped working.
	assert.Contains(t, err.Error(), "DSN")
}

// Every subcommand that still declares --type must also reject it. A handler
// that declares the flag and forgets the guard is exactly the silent-ignore bug
// this replaces.
func TestEveryTypeFlagIsGuarded(t *testing.T) {
	p := &DatabasePlugin{}

	var declaring []string

	for _, cmd := range p.Commands() {
		for _, sub := range cmd.Subcommands() {
			for _, flag := range sub.Flags() {
				if flag.Name() == "type" {
					declaring = append(declaring, sub.Name())
					assert.Contains(t, flag.Description(), "Removed",
						"the help text for --type on %q must say it was removed", sub.Name())
				}
			}
		}
	}

	require.NotEmpty(t, declaring)

	// Handlers are driven directly with a context that sets --type and nothing
	// else. Each must fail on the flag before it does anything else, which for
	// a plugin with no config would otherwise be "not a forge project".
	handlers := map[string]func(cli.CommandContext) error{
		"init":         p.initMigrations,
		"migrate":      p.runMigrations,
		"rollback":     p.rollbackMigrations,
		"status":       p.migrationStatus,
		"reset":        p.resetDatabase,
		"lock":         p.lockMigrations,
		"unlock":       p.unlockMigrations,
		"mark-applied": p.markApplied,
	}

	for _, name := range declaring {
		handler, ok := handlers[name]
		require.True(t, ok, "subcommand %q declares --type but this test does not drive it", name)

		ctx := newFakeContext()
		ctx.strings["type"] = "postgres"

		err := handler(ctx)
		require.Error(t, err, "%s must reject --type", name)
		assert.Contains(t, err.Error(), "--type", "%s must reject --type by name", name)
	}
}
