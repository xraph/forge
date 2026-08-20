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

// splitBunMigrationName turns one row of a legacy migration table into grove's
// separate version and name fields.
//
// The common case is a bare timestamp. bun's own migration table stores nothing
// else: migrate/migrations.go's fnameRE splits "20240115120000_create_users.up.sql"
// into a digits group and a descriptive group, keeps the digits as Migration.Name,
// and puts the rest in Comment, which is declared `bun:"-"` and so is never
// persisted. A row therefore reads back as "20240115120000" with the descriptive
// half already gone. Rejecting that shape is what made adopt skip every real row
// and still exit zero.
//
// The "<digits>_<rest>" form is kept too. bun never writes it, but a hand-written
// or third-party tracking table might, and honoring it costs nothing.
//
// Grove keys an applied migration on its group plus its version (see grove's
// migrate/migrator.go), so the name is display text only. When there is no name to
// recover, "adopted_<version>" is synthesized: it reads unambiguously in
// "forge db status" as a row that came from adopt rather than from a migration
// registered in code, which matters because such a row has no Up or Down to run.
//
// It still reports false rather than guessing when no version can be read at all.
// A fabricated version sorts wrongly against real ones and silently reorders every
// migration that follows, which is worse than refusing to adopt one row.
func splitBunMigrationName(raw string) (version, name string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	if prefix, rest, found := strings.Cut(raw, "_"); found {
		if prefix == "" || rest == "" || !isAllDigits(prefix) {
			return "", "", false
		}

		return prefix, rest, true
	}

	if !isAllDigits(raw) {
		return "", "", false
	}

	return raw, "adopted_" + raw, true
}

// isAllDigits reports whether s consists only of ASCII digits. s must be
// non-empty; the empty string reports false.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
