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
