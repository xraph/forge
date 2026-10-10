// Package persistence owns deployment settings, state blobs and SQL leases.
package persistence

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

var (
	ErrRetired     = errors.New("deployment store was retired by an explicit authority switch; select the current store")
	ErrConflict    = errors.New("deployment settings changed; reload before saving")
	ErrLocked      = errors.New("another deployment holds the lock")
	ErrLeaseLost   = errors.New("deployment lease lost; stop and inspect state")
	ErrUnavailable = errors.New("deployment store unavailable; check its connection reference and permissions")
)

type Options struct {
	Backend   string `json:"backend"`
	Reference string `json:"reference,omitempty"`
	Project   string `json:"project,omitempty"`
}
type DB struct {
	sql     *sql.DB
	options Options
	root    string
}

func Open(ctx context.Context, root string, options Options) (*DB, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if options.Project == "" {
		return nil, errors.New("deployment store needs a stable project identity")
	}

	var driver, dsn string

	switch options.Backend {
	case "sqlite":
		if !regexp.MustCompile(`^\.forge/[A-Za-z0-9][A-Za-z0-9_.-]{0,80}\.db$`).MatchString(options.Reference) {
			return nil, errors.New("SQLite reference must be a .forge/<name>.db file")
		}

		project, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer project.Close()

		if err := project.MkdirAll(".forge", 0700); err != nil {
			return nil, err
		}

		for _, path := range []string{".forge", options.Reference} {
			info, err := project.Lstat(path)
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}

			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("SQLite store path cannot be a symlink")
			}
		}

		file, err := project.OpenFile(options.Reference, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}

		if err := file.Close(); err != nil {
			return nil, err
		}

		if err := project.Chmod(options.Reference, 0600); err != nil {
			return nil, err
		}

		driver = "sqlite"

		absolute, err := filepath.Abs(filepath.Join(root, options.Reference))
		if err != nil {
			return nil, err
		}

		uri := url.URL{Scheme: "file", Path: absolute, RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"}
		dsn = uri.String()
	case "postgres":
		driver = "pgx"

		value, err := resolveReference(root, options.Reference)
		if err != nil {
			return nil, err
		}

		dsn = value
	default:
		return nil, errors.New("SQL deployment store must be sqlite or postgres")
	}

	database, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, ErrUnavailable
	}

	database.SetMaxOpenConns(4)

	if driver == "sqlite" {
		database.SetMaxOpenConns(1)
	}

	d := &DB{sql: database, options: options, root: root}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()

		return nil, ErrUnavailable
	}

	if err := d.migrate(ctx); err != nil {
		_ = database.Close()

		return nil, err
	}

	return d, nil
}
func resolveReference(root, reference string) (string, error) {
	kind, name, ok := strings.Cut(reference, ":")
	if !ok || name == "" {
		return "", errors.New("store DSN must use an env: or file: reference")
	}

	switch kind {
	case "env":
		for _, c := range name {
			if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return "", errors.New("invalid store environment reference")
			}
		}

		value := os.Getenv(name)
		if value == "" {
			return "", ErrUnavailable
		}

		return value, nil
	case "file":
		if !filepath.IsLocal(name) {
			return "", errors.New("store reference must stay inside the project")
		}

		r, err := os.OpenRoot(root)
		if err != nil {
			return "", err
		}
		defer r.Close()

		raw, err := r.ReadFile(name)
		if err != nil {
			return "", ErrUnavailable
		}

		return strings.TrimSpace(string(raw)), nil
	default:
		return "", errors.New("store DSN must use an env: or file: reference")
	}
}
func (d *DB) migrate(ctx context.Context) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = tx.Rollback() }()

	for _, statement := range []string{
		"CREATE TABLE IF NOT EXISTS forge_deploy_schema (id INTEGER PRIMARY KEY, version INTEGER NOT NULL)",
		"INSERT INTO forge_deploy_schema(id,version) VALUES(1,1) ON CONFLICT(id) DO NOTHING",
		"CREATE TABLE IF NOT EXISTS forge_deploy_settings (project TEXT PRIMARY KEY, revision BIGINT NOT NULL, content BYTEA NOT NULL)",
		"CREATE TABLE IF NOT EXISTS forge_deploy_blobs (project TEXT NOT NULL, scope TEXT NOT NULL, name TEXT NOT NULL, content BYTEA NOT NULL, PRIMARY KEY(project,scope,name))",
		"CREATE TABLE IF NOT EXISTS forge_deploy_retired (project TEXT PRIMARY KEY)",
		"CREATE TABLE IF NOT EXISTS forge_deploy_leases (project TEXT NOT NULL, scope TEXT NOT NULL, fence BIGINT NOT NULL, owner TEXT NOT NULL, expires BIGINT NOT NULL, PRIMARY KEY(project,scope))",
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return ErrUnavailable
		}
	}

	var version int
	if err := tx.QueryRowContext(ctx, "SELECT version FROM forge_deploy_schema WHERE id=1").Scan(&version); err != nil {
		return ErrUnavailable
	}

	if version != 1 {
		return errors.New("unsupported deployment store schema version")
	}

	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}

	return nil
}
func (d *DB) leaseQuery(operation string) string {
	if d.options.Backend == "postgres" {
		switch operation {
		case "acquire":
			return "UPDATE forge_deploy_leases SET fence=fence+1,owner=$1,expires=CAST(EXTRACT(EPOCH FROM clock_timestamp())*1000 AS BIGINT)+$2 WHERE project=$3 AND scope=$4 AND expires<=CAST(EXTRACT(EPOCH FROM clock_timestamp())*1000 AS BIGINT) AND NOT EXISTS(SELECT 1 FROM forge_deploy_retired WHERE project=forge_deploy_leases.project) RETURNING fence"
		case "renew":
			return "UPDATE forge_deploy_leases SET expires=CAST(EXTRACT(EPOCH FROM clock_timestamp())*1000 AS BIGINT)+$1 WHERE project=$2 AND scope=$3 AND owner=$4 AND fence=$5 AND expires>CAST(EXTRACT(EPOCH FROM clock_timestamp())*1000 AS BIGINT) AND NOT EXISTS(SELECT 1 FROM forge_deploy_retired WHERE project=forge_deploy_leases.project)"
		case "guard":
			return "UPDATE forge_deploy_leases SET expires=expires WHERE project=$1 AND scope=$2 AND owner=$3 AND fence=$4 AND expires>CAST(EXTRACT(EPOCH FROM clock_timestamp())*1000 AS BIGINT) AND NOT EXISTS(SELECT 1 FROM forge_deploy_retired WHERE project=forge_deploy_leases.project)"
		}
	}

	switch operation {
	case "acquire":
		return "UPDATE forge_deploy_leases SET fence=fence+1,owner=$1,expires=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER)+$2 WHERE project=$3 AND scope=$4 AND expires<=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER) AND NOT EXISTS(SELECT 1 FROM forge_deploy_retired WHERE project=forge_deploy_leases.project) RETURNING fence"
	case "renew":
		return "UPDATE forge_deploy_leases SET expires=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER)+$1 WHERE project=$2 AND scope=$3 AND owner=$4 AND fence=$5 AND expires>CAST((julianday('now')-2440587.5)*86400000 AS INTEGER) AND NOT EXISTS(SELECT 1 FROM forge_deploy_retired WHERE project=forge_deploy_leases.project)"
	case "guard":
		return "UPDATE forge_deploy_leases SET expires=expires WHERE project=$1 AND scope=$2 AND owner=$3 AND fence=$4 AND expires>CAST((julianday('now')-2440587.5)*86400000 AS INTEGER) AND NOT EXISTS(SELECT 1 FROM forge_deploy_retired WHERE project=forge_deploy_leases.project)"
	}

	panic("unknown internal lease operation")
}
func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) Settings(ctx context.Context) (uint64, []byte, error) {
	var (
		revision uint64
		raw      []byte
	)

	err := d.sql.QueryRowContext(ctx, "SELECT revision,content FROM forge_deploy_settings WHERE project=$1", d.options.Project).Scan(&revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}

	if err != nil {
		return 0, nil, ErrUnavailable
	}

	return revision, raw, nil
}
func (d *DB) SaveSettings(ctx context.Context, expected uint64, raw []byte) error {
	if expected == 0 {
		result, err := d.sql.ExecContext(ctx, "INSERT INTO forge_deploy_settings(project,revision,content) VALUES($1,1,$2) ON CONFLICT(project) DO NOTHING", d.options.Project, raw)

		return changed(result, err, ErrConflict)
	}

	result, err := d.sql.ExecContext(ctx, "UPDATE forge_deploy_settings SET revision=revision+1,content=$1 WHERE project=$2 AND revision=$3", raw, d.options.Project, expected)

	return changed(result, err, ErrConflict)
}
func changed(result sql.Result, err, failure error) error {
	if err != nil {
		return ErrUnavailable
	}

	count, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}

	if count != 1 {
		return failure
	}

	return nil
}
func (d *DB) Read(ctx context.Context, scope, name string) ([]byte, error) {
	var raw []byte

	err := d.sql.QueryRowContext(ctx, "SELECT content FROM forge_deploy_blobs WHERE project=$1 AND scope=$2 AND name=$3", d.options.Project, scope, name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, os.ErrNotExist
	}

	if err != nil {
		return nil, ErrUnavailable
	}

	return raw, nil
}
func bounded() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (d *DB) Active(ctx context.Context) error {
	var retired int
	if err := d.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM forge_deploy_retired WHERE project=$1", d.options.Project).Scan(&retired); err != nil {
		return ErrUnavailable
	}

	if retired != 0 {
		return ErrRetired
	}

	return nil
}
func (l *Lease) retire(ctx context.Context) error {
	tx, err := l.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = tx.Rollback() }()

	if err := l.guard(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO forge_deploy_retired(project) VALUES($1) ON CONFLICT(project) DO NOTHING", l.db.options.Project); err != nil {
		return ErrUnavailable
	}

	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}

	return nil
}
