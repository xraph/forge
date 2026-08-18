package plugins

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseGoSource fails the test if src is not valid Go. Every generator in this
// package is gated this way: emitted code that does not parse is the failure
// mode that reaches users, because nothing in the CLI compiles it until they do.
func parseGoSource(t *testing.T, path string) {
	t.Helper()

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = parser.ParseFile(token.NewFileSet(), path, src, parser.AllErrors)
	require.NoError(t, err, "generated file does not parse:\n%s", src)
}

func TestCreateMigrationsGoFileEmitsGroveRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrations.go")

	p := &DatabasePlugin{}
	require.NoError(t, p.createMigrationsGoFile(path))

	parseGoSource(t, path)

	src, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(src)

	assert.Contains(t, content, `"github.com/xraph/grove/migrate"`)
	assert.Contains(t, content, "migrate.NewMigrationRegistry()")
	assert.Contains(t, content, `migrate.NewGroup("app")`)
	assert.Contains(t, content, "Registry.Register(App)")

	// The extension this replaces must not survive anywhere in the scaffold.
	assert.NotContains(t, content, "extensions/database")
}

func TestCreateSQLMigrationWritesAnEmbeddingGoFile(t *testing.T) {
	dir := t.TempDir()

	// writeSQLMigrationFiles is the extracted core of createSQLMigration, so
	// the test does not need a CommandContext.
	up, down, goFile, err := writeSQLMigrationFiles(dir, "create_users", false)
	require.NoError(t, err)

	for _, path := range []string{up, down, goFile} {
		assert.FileExists(t, path)
	}

	parseGoSource(t, goFile)

	src, err := os.ReadFile(goFile)
	require.NoError(t, err)
	content := string(src)

	// The embed directives must name the files this same call wrote. A stale
	// or misspelled name is a compile error in the user's module, discovered
	// long after the CLI reported success.
	assert.Contains(t, content, "//go:embed "+filepath.Base(up))
	assert.Contains(t, content, "//go:embed "+filepath.Base(down))

	assert.Contains(t, content, `_ "embed"`)
	assert.Contains(t, content, "App.MustRegister")
	assert.Contains(t, content, `Name:    "create_users"`)
	assert.Contains(t, content, "exec.Exec(ctx,")
}

func TestCreateSQLMigrationTransactionalVariant(t *testing.T) {
	dir := t.TempDir()

	up, down, goFile, err := writeSQLMigrationFiles(dir, "create_users", true)
	require.NoError(t, err)

	assert.Contains(t, up, ".tx.up.sql")
	assert.Contains(t, down, ".tx.down.sql")

	parseGoSource(t, goFile)

	src, err := os.ReadFile(goFile)
	require.NoError(t, err)
	assert.Contains(t, string(src), "//go:embed "+filepath.Base(up))
}
