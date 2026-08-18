package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
