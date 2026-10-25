package plugins

import (
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
)

func TestCheckMigrationName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSuffix string // empty means the name is accepted
		wantKind   string
	}{
		{name: "plain name", input: "backfill_emails"},
		{name: "single word", input: "init"},
		{name: "test suffix", input: "backfill_test", wantSuffix: "_test", wantKind: "a test file"},
		{name: "test alone", input: "test", wantSuffix: "_test", wantKind: "a test file"},
		{name: "GOOS suffix", input: "seed_linux", wantSuffix: "_linux", wantKind: "a GOOS build constraint"},
		{name: "GOOS suffix, windows", input: "seed_windows", wantSuffix: "_windows", wantKind: "a GOOS build constraint"},
		{name: "GOOS suffix, wasip1", input: "seed_wasip1", wantSuffix: "_wasip1", wantKind: "a GOOS build constraint"},
		{name: "GOOS suffix, js", input: "seed_js", wantSuffix: "_js", wantKind: "a GOOS build constraint"},
		{name: "retired GOOS suffix", input: "seed_zos", wantSuffix: "_zos", wantKind: "a GOOS build constraint"},
		{name: "GOARCH suffix", input: "seed_arm64", wantSuffix: "_arm64", wantKind: "a GOARCH build constraint"},
		{name: "GOARCH suffix, wasm", input: "seed_wasm", wantSuffix: "_wasm", wantKind: "a GOARCH build constraint"},
		{name: "retired GOARCH suffix", input: "seed_amd64p32", wantSuffix: "_amd64p32", wantKind: "a GOARCH build constraint"},
		{name: "GOOS and GOARCH suffix", input: "seed_linux_amd64", wantSuffix: "_linux_amd64", wantKind: "a GOOS and GOARCH build constraint"},
		{name: "GOOS and GOARCH suffix, darwin", input: "seed_darwin_arm64", wantSuffix: "_darwin_arm64", wantKind: "a GOOS and GOARCH build constraint"},
		{name: "GOARCH then GOOS reads as the GOOS alone", input: "seed_amd64_linux", wantSuffix: "_linux", wantKind: "a GOOS build constraint"},
		{name: "GOOS with test", input: "seed_linux_test", wantSuffix: "_test", wantKind: "a test file"},
		{name: "name that is only a GOOS", input: "linux", wantSuffix: "_linux", wantKind: "a GOOS build constraint"},
		{name: "name that is only a GOARCH", input: "amd64", wantSuffix: "_amd64", wantKind: "a GOARCH build constraint"},
		{name: "dot before the extension hides the suffix from go build", input: "report_linux.v2", wantSuffix: "_linux", wantKind: "a GOOS build constraint"},
		{name: "token in the middle", input: "linux_hosts"},
		{name: "token in the middle, arch", input: "amd64_report_totals"},
		{name: "GOOS then a word", input: "windows_users_table"},
		{name: "test in the middle", input: "test_fixtures"},
		{name: "test inside a word", input: "create_latest"},
		{name: "token inside a word", input: "create_linuxbox"},
		{name: "dotted name with a clean stem", input: "report.linux"},
		{name: "go build matches tokens case sensitively", input: "seed_Linux"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkMigrationName(tt.input)

			if tt.wantSuffix == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), `"`+tt.wantSuffix+`"`, "the error should name the suffix")
			assert.Contains(t, err.Error(), tt.wantKind)
			assert.Contains(t, err.Error(), "choose another name")
		})
	}
}

// The rule is only worth having if it agrees with the toolchain, so ask
// go/build. Under a GOOS and GOARCH that no file name can carry, every file
// whose name holds a constraint token is excluded and every other file is not.
func TestBuildConstraintSuffixAgreesWithGoBuild(t *testing.T) {
	dir := t.TempDir()

	// Only the file-name rule is in play here: a bogus platform keeps real
	// GOOS and GOARCH tokens from matching.
	ctxt := build.Default
	ctxt.GOOS = "forgeos"
	ctxt.GOARCH = "forgearch"

	// A file with no build tags and no body beyond the package clause, so
	// nothing but its name can exclude it.
	const body = "package migrations\n"

	included := func(name string) bool {
		t.Helper()

		file := "20260101000000_" + name + ".go"
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644))

		ok, err := ctxt.MatchFile(dir, file)
		require.NoError(t, err)

		return ok
	}

	names := []string{"plain", "linux_hosts", "seed_Linux", "report.linux", "report_linux.v2", "linux", "amd64"}
	for _, tok := range knownGOOS {
		names = append(names, "seed_"+tok)
	}

	for _, tok := range knownGOARCH {
		names = append(names, "seed_"+tok)
	}

	for _, goos := range []string{"linux", "plan9", "zos"} {
		for _, goarch := range []string{"amd64", "wasm", "s390"} {
			names = append(names, "seed_"+goos+"_"+goarch, "seed_"+goarch+"_"+goos)
		}
	}

	for _, name := range names {
		suffix, _ := buildConstraintSuffix(name)
		// _test files are a go list concern, not a MatchFile one, so only the
		// GOOS and GOARCH suffixes are compared here.
		assert.Equal(t, suffix != "", !included(name), "name %q: suffix %q", name, suffix)
	}
}

// go tool dist list names the ports Go builds today. Every one of its GOOS and
// GOARCH values must be in the tables, so a new port in a future toolchain
// fails here instead of letting a migration vanish.
func TestKnownGOOSAndGOARCHCoverTheToolchainPorts(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}

	out, err := exec.CommandContext(t.Context(), goBin, "tool", "dist", "list").Output()
	if err != nil {
		t.Skipf("go tool dist list unavailable: %v", err)
	}

	for port := range strings.FieldsSeq(string(out)) {
		goos, goarch, ok := strings.Cut(port, "/")
		require.True(t, ok, "unexpected port %q", port)

		assert.Contains(t, knownGOOS, goos, "GOOS from go tool dist list")
		assert.Contains(t, knownGOARCH, goarch, "GOARCH from go tool dist list")
	}
}

func TestWriteGoMigrationFileRejectsConstraintNames(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"seed_test", "seed_linux", "seed_arm64", "seed_linux_amd64"} {
		path, err := writeGoMigrationFile(dir, name)
		require.Error(t, err, name)
		assert.Empty(t, path)
	}

	// Nothing may be written for a rejected name: a leftover file would be
	// the very dropped migration the check exists to prevent.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)

	path, err := writeGoMigrationFile(dir, "linux_hosts")
	require.NoError(t, err)
	assert.FileExists(t, path)
}

func TestGroveUpgradeFor(t *testing.T) {
	const header = "module example.com/app\n\ngo 1.26\n\n"

	tests := []struct {
		name        string
		goMod       string
		wantWarn    bool
		wantCurrent string
		wantDrivers []string
	}{
		{
			name:        "old release warns",
			goMod:       header + "require github.com/xraph/grove v1.9.1\n",
			wantWarn:    true,
			wantCurrent: "v1.9.1",
		},
		{
			name:        "old release in a require block warns",
			goMod:       header + "require (\n\tgithub.com/stretchr/testify v1.11.1\n\tgithub.com/xraph/grove v1.5.8\n)\n",
			wantWarn:    true,
			wantCurrent: "v1.5.8",
		},
		{
			name:        "indirect require warns",
			goMod:       header + "require github.com/xraph/grove v1.9.1 // indirect\n",
			wantWarn:    true,
			wantCurrent: "v1.9.1",
		},
		{
			name:        "numeric not lexical, v1.9.9 is below v1.10.0",
			goMod:       header + "require github.com/xraph/grove v1.9.9\n",
			wantWarn:    true,
			wantCurrent: "v1.9.9",
		},
		{
			name:        "pseudo-version after v1.9.1 warns",
			goMod:       header + "require github.com/xraph/grove v1.9.2-0.20261013042516-c28fa000e1b3\n",
			wantWarn:    true,
			wantCurrent: "v1.9.2-0.20261013042516-c28fa000e1b3",
		},
		{
			name:        "pseudo-version that sorts before the v1.10.0 release warns",
			goMod:       header + "require github.com/xraph/grove v1.10.0-0.20261020101010-abcdef123456\n",
			wantWarn:    true,
			wantCurrent: "v1.10.0-0.20261020101010-abcdef123456",
		},
		{
			name:     "v1.10.0 does not warn",
			goMod:    header + "require github.com/xraph/grove v1.10.0\n",
			wantWarn: false,
		},
		{
			name:     "later patch does not warn",
			goMod:    header + "require github.com/xraph/grove v1.10.3\n",
			wantWarn: false,
		},
		{
			name:     "later minor does not warn",
			goMod:    header + "require github.com/xraph/grove v1.11.0\n",
			wantWarn: false,
		},
		{
			name:     "pseudo-version after v1.10.0 does not warn",
			goMod:    header + "require github.com/xraph/grove v1.10.1-0.20261030120000-abcdef123456\n",
			wantWarn: false,
		},
		{
			name:     "no grove require does not warn",
			goMod:    header + "require github.com/stretchr/testify v1.11.1\n",
			wantWarn: false,
		},
		{
			name:     "a module that only shares the prefix does not warn",
			goMod:    header + "require github.com/xraph/grove-tools v0.1.0\n",
			wantWarn: false,
		},
		{
			name:     "a grove submodule that is not a driver does not warn on its own",
			goMod:    header + "require github.com/xraph/grove/kv v1.8.0\n",
			wantWarn: false,
		},
		{
			name:     "old driver alone does not warn",
			goMod:    header + "require (\n\tgithub.com/xraph/grove v1.10.0\n\tgithub.com/xraph/grove/drivers/pgdriver v1.9.1\n)\n",
			wantWarn: false,
		},
		{
			name:     "replaced grove does not warn, the require line is not what builds",
			goMod:    header + "require github.com/xraph/grove v1.9.1\n\nreplace github.com/xraph/grove => ../grove\n",
			wantWarn: false,
		},
		{
			name:     "malformed version does not warn",
			goMod:    header + "require github.com/xraph/grove not-a-version\n",
			wantWarn: false,
		},
		{
			name:     "empty file does not warn",
			goMod:    "",
			wantWarn: false,
		},
		{
			name:     "garbage does not warn",
			goMod:    "this is not a go.mod {{{",
			wantWarn: false,
		},
		{
			name: "old driver modules are listed, current ones and other modules are not",
			goMod: header + `require (
	github.com/xraph/grove v1.9.1
	github.com/xraph/grove/drivers/sqlitedriver v1.9.1
	github.com/xraph/grove/drivers/pgdriver v1.9.1
	github.com/xraph/grove/drivers/mysqldriver v1.10.0
	github.com/xraph/grove/kv v1.8.0
)
`,
			wantWarn:    true,
			wantCurrent: "v1.9.1",
			wantDrivers: []string{
				"github.com/xraph/grove/drivers/pgdriver",
				"github.com/xraph/grove/drivers/sqlitedriver",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groveUpgradeFor([]byte(tt.goMod))

			if !tt.wantWarn {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, tt.wantCurrent, got.Current)
			assert.Equal(t, tt.wantDrivers, got.Drivers)
		})
	}
}

// quietSpinner stands in for the terminal spinner, which fakeContext leaves
// nil: the migration commands call Stop on it.
type quietSpinner struct{}

func (quietSpinner) Update(string) {}
func (quietSpinner) Stop(string)   {}

// migrationContext lets the two migration commands run end to end against the
// recording fakeContext.
type migrationContext struct {
	*fakeContext

	args []string
}

func (c *migrationContext) Args() []string             { return c.args }
func (c *migrationContext) Spinner(string) cli.Spinner { return quietSpinner{} }

// migrationProject builds a project root holding the given go.mod, or none
// when goMod is empty.
func migrationProject(t *testing.T, goMod string) *config.ForgeConfig {
	t.Helper()

	root := t.TempDir()
	if goMod != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644))
	}

	return &config.ForgeConfig{
		RootDir:  root,
		Database: config.DatabaseConfig{MigrationsPath: "migrations"},
	}
}

const (
	oldGroveGoMod = "module example.com/app\n\ngo 1.26\n\nrequire (\n\tgithub.com/xraph/grove v1.9.1\n\tgithub.com/xraph/grove/drivers/pgdriver v1.9.1\n)\n"
	newGroveGoMod = "module example.com/app\n\ngo 1.26\n\nrequire github.com/xraph/grove v1.10.0\n"
)

func TestWarnIfGroveTooOld(t *testing.T) {
	t.Run("old grove names the fix with the driver module", func(t *testing.T) {
		cfg := migrationProject(t, oldGroveGoMod)
		ctx := newFakeContext()

		warnIfGroveTooOld(ctx, cfg.RootDir)

		assert.True(t, ctx.said("requires github.com/xraph/grove v1.9.1"))
		assert.True(t, ctx.said("go get github.com/xraph/grove@v1.10.0 github.com/xraph/grove/drivers/pgdriver@v1.10.0"))
		assert.False(t, ctx.said("your grove driver modules"))
	})

	t.Run("old grove without a driver require points at the driver modules", func(t *testing.T) {
		cfg := migrationProject(t, "module example.com/app\n\ngo 1.26\n\nrequire github.com/xraph/grove v1.9.1\n")
		ctx := newFakeContext()

		warnIfGroveTooOld(ctx, cfg.RootDir)

		assert.True(t, ctx.said("go get github.com/xraph/grove@v1.10.0"))
		assert.True(t, ctx.said("and your grove driver modules"))
	})

	t.Run("v1.10.0 says nothing", func(t *testing.T) {
		cfg := migrationProject(t, newGroveGoMod)
		ctx := newFakeContext()

		warnIfGroveTooOld(ctx, cfg.RootDir)

		assert.Empty(t, ctx.output)
	})

	t.Run("missing go.mod says nothing", func(t *testing.T) {
		cfg := migrationProject(t, "")
		ctx := newFakeContext()

		warnIfGroveTooOld(ctx, cfg.RootDir)

		assert.Empty(t, ctx.output)
	})

	t.Run("go.mod that is a directory says nothing", func(t *testing.T) {
		cfg := migrationProject(t, "")
		require.NoError(t, os.Mkdir(filepath.Join(cfg.RootDir, "go.mod"), 0o755))

		ctx := newFakeContext()

		warnIfGroveTooOld(ctx, cfg.RootDir)

		assert.Empty(t, ctx.output)
	})
}

// Both commands write the same file, so both have to raise the warning, and a
// warning must not turn into a failure.
func TestMigrationCommandsWarnWhenGroveIsTooOld(t *testing.T) {
	t.Run("forge db create-go", func(t *testing.T) {
		cfg := migrationProject(t, oldGroveGoMod)
		p := &DatabasePlugin{config: cfg}
		ctx := &migrationContext{fakeContext: newFakeContext(), args: []string{"backfill", "emails"}}

		require.NoError(t, p.createGoMigration(ctx))

		matches, err := filepath.Glob(filepath.Join(cfg.RootDir, "migrations", "*_backfill_emails.go"))
		require.NoError(t, err)
		assert.Len(t, matches, 1)

		assert.True(t, ctx.said("go get github.com/xraph/grove@v1.10.0"))
	})

	t.Run("forge generate migration", func(t *testing.T) {
		cfg := migrationProject(t, oldGroveGoMod)
		p := &GeneratePlugin{config: cfg}
		ctx := &migrationContext{fakeContext: newFakeContext()}
		ctx.strings["name"] = "create users table"

		require.NoError(t, p.generateMigration(ctx))

		matches, err := filepath.Glob(filepath.Join(cfg.RootDir, "migrations", "*_create_users_table.go"))
		require.NoError(t, err)
		assert.Len(t, matches, 1)

		assert.True(t, ctx.said("go get github.com/xraph/grove@v1.10.0"))
		assert.True(t, ctx.said("passed to App.Add"))
		assert.True(t, ctx.said("version is in the file name"))
		assert.False(t, ctx.said("Up and Down function bodies"))
	})

	t.Run("a current grove stays quiet", func(t *testing.T) {
		cfg := migrationProject(t, newGroveGoMod)
		p := &DatabasePlugin{config: cfg}
		ctx := &migrationContext{fakeContext: newFakeContext(), args: []string{"backfill"}}

		require.NoError(t, p.createGoMigration(ctx))
		assert.False(t, ctx.said("go get"))
	})

	t.Run("a rejected name fails the command and writes nothing", func(t *testing.T) {
		cfg := migrationProject(t, oldGroveGoMod)
		p := &DatabasePlugin{config: cfg}
		ctx := &migrationContext{fakeContext: newFakeContext(), args: []string{"seed", "linux"}}

		err := p.createGoMigration(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"_linux"`)

		matches, globErr := filepath.Glob(filepath.Join(cfg.RootDir, "migrations", "*.go"))
		require.NoError(t, globErr)
		assert.Empty(t, matches)
		assert.False(t, ctx.said("go get"), "no warning for a file that was never written")
	})
}
