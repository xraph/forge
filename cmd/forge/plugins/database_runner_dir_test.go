package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGoTool replaces runGoTool for one test. A "go build -o <path>" writes a
// shell script to <path> that exits with exitCode, so runWithGoMigrations can
// run its usual exec without a real compile; tidyErr, when set, fails "go mod
// tidy" instead.
func stubGoTool(t *testing.T, exitCode string, tidyErr error) {
	t.Helper()

	old := runGoTool

	t.Cleanup(func() { runGoTool = old })

	runGoTool = func(_ string, _ []string, args ...string) ([]byte, error) {
		if slices.Equal(args[:2], []string{"mod", "tidy"}) {
			if tidyErr != nil {
				return []byte("tidy output"), tidyErr
			}

			return nil, nil
		}

		if i := slices.Index(args, "-o"); i >= 0 {
			script := "#!/bin/sh\nexit " + exitCode + "\n"

			return nil, os.WriteFile(args[i+1], []byte(script), 0o755)
		}

		return nil, errors.New("unexpected go invocation")
	}
}

// runnerDirTestPlugin builds a scratch project that runWithGoMigrations can get
// through to the build step, and points the temp directory at a fresh one so
// the test can see exactly which forge-migrate-* directories were left behind.
func runnerDirTestPlugin(t *testing.T) (*DatabasePlugin, string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the stub runner is a shell script")
	}

	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	p, migrationsDir := upgradePlugin(t, "")
	root := filepath.Dir(migrationsDir)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.24\n"), 0o644))
	require.NoError(t, p.createMigrationsGoFile(filepath.Join(migrationsDir, "migrations.go"), ""))

	return p, tmp
}

func runnerDirs(t *testing.T, tmp string) []string {
	t.Helper()

	dirs, err := filepath.Glob(filepath.Join(tmp, "forge-migrate-*"))
	require.NoError(t, err)

	return dirs
}

func TestRunWithGoMigrationsRemovesRunnerDir(t *testing.T) {
	for name, exitCode := range map[string]string{"migration succeeds": "0", "migration fails": "3"} {
		t.Run(name, func(t *testing.T) {
			p, tmp := runnerDirTestPlugin(t)
			stubGoTool(t, exitCode, nil)

			err := p.runWithGoMigrations(newFakeContext(), "migrate", "sqlite://"+filepath.Join(tmp, "app.db"))
			if exitCode == "0" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			assert.Empty(t, runnerDirs(t, tmp), "the runner's temp directory must be removed once it has run")
		})
	}
}

func TestBuildMigrationRunnerKeepsDirOnBuildFailure(t *testing.T) {
	p, tmp := runnerDirTestPlugin(t)
	stubGoTool(t, "0", errors.New("tidy failed"))

	_, cleanup, err := p.buildMigrationRunner("sqlite://"+filepath.Join(tmp, "app.db"), "")
	require.Error(t, err)
	assert.Nil(t, cleanup)

	dirs := runnerDirs(t, tmp)
	require.Len(t, dirs, 1, "a failed build keeps its generated source for inspection")
	assert.Contains(t, err.Error(), dirs[0], "and says where it is")
}
