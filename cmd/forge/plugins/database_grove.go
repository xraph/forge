package plugins

import (
	"fmt"
	"sort"
	"strings"
)

// groveDriver is one grove driver module the generated migration runner can
// import. Grove keeps each driver in its own Go module and each migration
// executor in a subpackage of it, so the runner needs both paths: the driver
// registers the DSN scheme, the subpackage registers the executor.
type groveDriver struct {
	Scheme        string
	Module        string
	MigrateImport string
}

// groveDrivers maps a DSN scheme to the driver that serves it. The names are
// exactly what each driver's register.go passes to grove.RegisterDriver, so
// adding an entry here that grove does not register produces a runner that
// compiles and then fails at run time.
//
// elasticsearch is deliberately absent. esdriver exists, but ships no migrate
// subpackage, so it cannot be a migration target. resolveGroveDriver reports
// that case separately rather than as an unknown scheme.
var groveDrivers = map[string]groveDriver{
	"postgres":   {"postgres", "github.com/xraph/grove/drivers/pgdriver", "github.com/xraph/grove/drivers/pgdriver/pgmigrate"},
	"pg":         {"pg", "github.com/xraph/grove/drivers/pgdriver", "github.com/xraph/grove/drivers/pgdriver/pgmigrate"},
	"mysql":      {"mysql", "github.com/xraph/grove/drivers/mysqldriver", "github.com/xraph/grove/drivers/mysqldriver/mysqlmigrate"},
	"sqlite":     {"sqlite", "github.com/xraph/grove/drivers/sqlitedriver", "github.com/xraph/grove/drivers/sqlitedriver/sqlitemigrate"},
	"mongodb":    {"mongodb", "github.com/xraph/grove/drivers/mongodriver", "github.com/xraph/grove/drivers/mongodriver/mongomigrate"},
	"mongo":      {"mongo", "github.com/xraph/grove/drivers/mongodriver", "github.com/xraph/grove/drivers/mongodriver/mongomigrate"},
	"clickhouse": {"clickhouse", "github.com/xraph/grove/drivers/clickhousedriver", "github.com/xraph/grove/drivers/clickhousedriver/clickhousemigrate"},
	"turso":      {"turso", "github.com/xraph/grove/drivers/tursodriver", "github.com/xraph/grove/drivers/tursodriver/tursomigrate"},
}

// schemeAliases maps DSN prefixes people actually write onto the names grove
// registers. postgresql:// is the whole reason this exists.
var schemeAliases = map[string]string{
	"postgresql": "postgres",
}

// groveDriverSchemes returns every supported scheme, sorted, so error messages
// and emitted source are stable between runs.
func groveDriverSchemes() []string {
	schemes := make([]string, 0, len(groveDrivers))
	for scheme := range groveDrivers {
		schemes = append(schemes, scheme)
	}

	sort.Strings(schemes)

	return schemes
}

// resolveGroveDriver picks the driver for a DSN by its scheme.
func resolveGroveDriver(dsn string) (groveDriver, error) {
	scheme, _, found := strings.Cut(dsn, "://")
	if !found || scheme == "" {
		return groveDriver{}, fmt.Errorf("cannot determine database type from %q: a DSN must start with a scheme, for example postgres://", dsn)
	}

	scheme = strings.ToLower(scheme)
	if alias, ok := schemeAliases[scheme]; ok {
		scheme = alias
	}

	if scheme == "elasticsearch" {
		return groveDriver{}, fmt.Errorf("elasticsearch has no migration support: grove ships an elasticsearch driver but no migrate executor for it")
	}

	drv, ok := groveDrivers[scheme]
	if !ok {
		return groveDriver{}, fmt.Errorf("unsupported database scheme %q: supported schemes are %s", scheme, strings.Join(groveDriverSchemes(), ", "))
	}

	return drv, nil
}

// splitBunMigrationName turns a bun migration name into grove's separate name
// and version. bun records "20240115120000_create_users"; grove wants the
// version and the name as distinct fields.
//
// It reports false rather than guessing when the shape does not match. A
// fabricated version sorts wrongly against real ones and silently reorders
// every migration that follows, which is worse than refusing to adopt one row.
func splitBunMigrationName(raw string) (version, name string, ok bool) {
	raw = strings.TrimSuffix(raw, ".sql")
	raw = strings.TrimSuffix(raw, ".up")
	raw = strings.TrimSuffix(raw, ".tx")

	version, name, found := strings.Cut(raw, "_")
	if !found || version == "" || name == "" {
		return "", "", false
	}

	for _, r := range version {
		if r < '0' || r > '9' {
			return "", "", false
		}
	}

	return version, name, true
}
