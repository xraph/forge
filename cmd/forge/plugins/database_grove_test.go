package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/cmd/forge/config"
)

func TestResolveGroveDriver(t *testing.T) {
	tests := []struct {
		name        string
		dsn         string
		wantScheme  string
		wantModule  string
		wantMigrate string
	}{
		{
			name:        "postgres",
			dsn:         "postgres://user:pass@localhost:5432/db",
			wantScheme:  "postgres",
			wantModule:  "github.com/xraph/grove/drivers/pgdriver",
			wantMigrate: "github.com/xraph/grove/drivers/pgdriver/pgmigrate",
		},
		{
			// postgresql:// is a common DSN prefix but grove does not register
			// it, so resolution has to normalise rather than pass it through.
			name:        "postgresql normalises to postgres",
			dsn:         "postgresql://user:pass@localhost:5432/db",
			wantScheme:  "postgres",
			wantModule:  "github.com/xraph/grove/drivers/pgdriver",
			wantMigrate: "github.com/xraph/grove/drivers/pgdriver/pgmigrate",
		},
		{
			name:        "pg alias",
			dsn:         "pg://localhost/db",
			wantScheme:  "pg",
			wantModule:  "github.com/xraph/grove/drivers/pgdriver",
			wantMigrate: "github.com/xraph/grove/drivers/pgdriver/pgmigrate",
		},
		{
			name:        "mysql",
			dsn:         "mysql://root@localhost:3306/db",
			wantScheme:  "mysql",
			wantModule:  "github.com/xraph/grove/drivers/mysqldriver",
			wantMigrate: "github.com/xraph/grove/drivers/mysqldriver/mysqlmigrate",
		},
		{
			name:        "sqlite",
			dsn:         "sqlite://./app.db",
			wantScheme:  "sqlite",
			wantModule:  "github.com/xraph/grove/drivers/sqlitedriver",
			wantMigrate: "github.com/xraph/grove/drivers/sqlitedriver/sqlitemigrate",
		},
		{
			name:        "mongodb",
			dsn:         "mongodb://localhost:27017/db",
			wantScheme:  "mongodb",
			wantModule:  "github.com/xraph/grove/drivers/mongodriver",
			wantMigrate: "github.com/xraph/grove/drivers/mongodriver/mongomigrate",
		},
		{
			name:        "clickhouse",
			dsn:         "clickhouse://localhost:9000/db",
			wantScheme:  "clickhouse",
			wantModule:  "github.com/xraph/grove/drivers/clickhousedriver",
			wantMigrate: "github.com/xraph/grove/drivers/clickhousedriver/clickhousemigrate",
		},
		{
			name:        "turso",
			dsn:         "turso://token@host",
			wantScheme:  "turso",
			wantModule:  "github.com/xraph/grove/drivers/tursodriver",
			wantMigrate: "github.com/xraph/grove/drivers/tursodriver/tursomigrate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveGroveDriver(tt.dsn)
			require.NoError(t, err)
			assert.Equal(t, tt.wantScheme, got.Scheme)
			assert.Equal(t, tt.wantModule, got.Module)
			assert.Equal(t, tt.wantMigrate, got.MigrateImport)
		})
	}
}

// elasticsearch has a grove driver but no migrate subpackage, so it cannot be
// a migration target. Saying that is far more useful than letting grove report
// a generic missing executor at runtime.
func TestResolveGroveDriverRejectsElasticsearch(t *testing.T) {
	_, err := resolveGroveDriver("elasticsearch://localhost:9200")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "elasticsearch")
	assert.Contains(t, err.Error(), "migrat")
}

func TestResolveGroveDriverRejectsUnknownScheme(t *testing.T) {
	_, err := resolveGroveDriver("cassandra://localhost:9042")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cassandra")
	// The error lists what is supported, so the operator can fix the DSN
	// without going to the source.
	assert.Contains(t, err.Error(), "postgres")
}

func TestResolveGroveDriverRejectsSchemelessDSN(t *testing.T) {
	_, err := resolveGroveDriver("/var/lib/app.db")
	require.Error(t, err)
}

// The table is a map, and emitted source has to be byte-identical between
// runs, so the accessor sorts.
func TestGroveDriverSchemesAreSorted(t *testing.T) {
	schemes := groveDriverSchemes()
	require.NotEmpty(t, schemes)

	for i := 1; i < len(schemes); i++ {
		assert.Less(t, schemes[i-1], schemes[i], "groveDriverSchemes must be sorted")
	}
}

func TestSplitBunMigrationName(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantVersion string
		wantName    string
		wantOK      bool
	}{
		{
			name:        "standard bun name",
			raw:         "20240115120000_create_users",
			wantVersion: "20240115120000",
			wantName:    "create_users",
			wantOK:      true,
		},
		{
			name:        "name containing underscores",
			raw:         "20240115120000_add_index_to_users_email",
			wantVersion: "20240115120000",
			wantName:    "add_index_to_users_email",
			wantOK:      true,
		},
		{
			name:        "trailing .up.sql is stripped",
			raw:         "20240115120000_create_users.up.sql",
			wantVersion: "20240115120000",
			wantName:    "create_users",
			wantOK:      true,
		},
		{
			// No version to key on. Guessing one would silently reorder later
			// migrations, so adopt reports and skips instead.
			name:   "no leading digits",
			raw:    "create_users",
			wantOK: false,
		},
		{
			name:   "no separator",
			raw:    "20240115120000",
			wantOK: false,
		},
		{
			name:   "empty",
			raw:    "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, name, ok := splitBunMigrationName(tt.raw)
			assert.Equal(t, tt.wantOK, ok)

			if tt.wantOK {
				assert.Equal(t, tt.wantVersion, version)
				assert.Equal(t, tt.wantName, name)
			}
		})
	}
}

func TestResolveDSNPrefersTheFlag(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://from-env/db")

	p := &DatabasePlugin{config: nil}

	dsn, err := p.resolveDSNFrom("postgres://from-flag/db", "", "")
	require.NoError(t, err)
	assert.Equal(t, "postgres://from-flag/db", dsn)
}

// resolveGroveDriver already matches scheme names case-insensitively, but the DSN
// string flows on verbatim into DATABASE_URL and from there into the generated
// runner, where the sqlite scheme gets stripped with a literal, case-sensitive
// prefix check. "--dsn Sqlite://..." used to resolve to the right driver and then
// fail that strip, producing a mangled path that pointed at nothing.
func TestResolveDSNNormalizesSchemeCase(t *testing.T) {
	p := &DatabasePlugin{config: nil}

	dsn, err := p.resolveDSNFrom("Sqlite://./app.db", "", "")
	require.NoError(t, err)
	assert.Equal(t, "sqlite://./app.db", dsn)
}

func TestNormalizeDSNSchemeLeavesEverythingAfterTheSchemeAlone(t *testing.T) {
	// Only the scheme keyword is case-normalized. A password, host, or path with
	// meaningful casing must survive unchanged.
	assert.Equal(t, "postgres://User:PaSSw0rd@Host/DB", normalizeDSNScheme("POSTGRES://User:PaSSw0rd@Host/DB"))
	// No "://" at all means resolveGroveDriver will reject it anyway; normalizing
	// must not panic or invent a scheme separator that was not there.
	assert.Equal(t, "not-a-dsn", normalizeDSNScheme("not-a-dsn"))
}

func TestResolveDSNFallsBackToEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://from-env/db")

	p := &DatabasePlugin{config: nil}

	dsn, err := p.resolveDSNFrom("", "", "")
	require.NoError(t, err)
	assert.Equal(t, "postgres://from-env/db", dsn)
}

func TestResolveDSNReportsWhereItLooked(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	p := &DatabasePlugin{config: nil}

	_, err := p.resolveDSNFrom("", "default", "")
	require.Error(t, err)
	// The operator needs to know which sources were consulted, which is what
	// the old buildConfigNotFoundError did and is worth keeping.
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

// loadDatabaseConfig used to search the config.yaml family (config.yaml,
// config.yml, config.local.yaml, config.local.yml, in the project root and
// its config/ subdirectory) in addition to .forge.yaml. resolveDSNFrom folds
// that search in as a third source, between .forge.yaml and DATABASE_URL, so
// it must still be found there.
func TestResolveDSNFindsConfigYamlWhenForgeYamlHasNoMatch(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	tmpDir := t.TempDir()

	configYAML := `database:
  databases:
    - name: default
      dsn: postgres://from-config-yaml/db
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0644))

	p := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			// No database.connections entries, so .forge.yaml has nothing to
			// offer and resolution must fall through to config.yaml.
		},
	}

	dsn, err := p.resolveDSNFrom("", "default", "")
	require.NoError(t, err)
	assert.Equal(t, "postgres://from-config-yaml/db", dsn)
}

// Naming the sources it tried is the entire value of the not-found error:
// it is what tells an operator where to put their DSN. The config.yaml
// family must show up in that list even when it exists but does not define
// the requested database.
func TestResolveDSNReportsConfigYamlSourcesTried(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	tmpDir := t.TempDir()

	configYAML := `database:
  databases:
    - name: other
      dsn: postgres://not-the-one/db
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0644))

	p := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
		},
	}

	_, err := p.resolveDSNFrom("", "default", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), configPath)
	assert.Contains(t, err.Error(), "DATABASE_URL")
}
