package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactDSN(t *testing.T) {
	tests := []struct {
		name   string
		dsn    string
		want   string
		secret string // must not appear in the output when set
	}{
		{name: "empty", dsn: "", want: ""},
		{name: "url without credentials", dsn: "postgres://localhost:5432/app", want: "postgres://localhost:5432/app"},
		{name: "url user only", dsn: "postgres://app@localhost/app", want: "postgres://app@localhost/app"},
		{
			name:   "url password",
			dsn:    "postgres://app:s3cret@localhost:5432/app?sslmode=disable",
			want:   "postgres://app:xxxxx@localhost:5432/app?sslmode=disable",
			secret: "s3cret",
		},
		{
			name:   "url escaped password",
			dsn:    "mysql://root:p%40ss@db:3306/app",
			want:   "mysql://root:xxxxx@db:3306/app",
			secret: "p%40ss",
		},
		{
			name:   "url unescaped at in password",
			dsn:    "postgres://app:p@ss@localhost/app",
			want:   "postgres://app:xxxxx@localhost/app",
			secret: "p@ss",
		},
		{
			name:   "url unescaped hash in password",
			dsn:    "postgres://app:pa#ss@localhost/app",
			want:   "postgres://app:xxxxx@localhost/app",
			secret: "pa#ss",
		},
		{
			name:   "url unescaped slash in numeric password",
			dsn:    "postgres://app:123/45@localhost/app",
			want:   "postgres://app:xxxxx@localhost/app",
			secret: "123/45",
		},
		{
			name:   "url token only userinfo",
			dsn:    "redis://:tok3n@cache:6379/0",
			want:   "redis://:xxxxx@cache:6379/0",
			secret: "tok3n",
		},
		{
			name:   "url query token",
			dsn:    "libsql://app.turso.io?authToken=tok3n&tls=1",
			want:   "libsql://app.turso.io?authToken=xxxxx&tls=1",
			secret: "tok3n",
		},
		{
			name:   "url query password",
			dsn:    "sqlserver://db:1433?database=app&password=s3cret",
			want:   "sqlserver://db:1433?database=app&password=xxxxx",
			secret: "s3cret",
		},
		{
			name:   "unparseable url keeps only the scheme",
			dsn:    "postgres://ho st:bad%zz/app",
			want:   "postgres://[redacted]",
			secret: "ho st",
		},
		{name: "sqlite url", dsn: "sqlite://./data/app.db", want: "sqlite://./data/app.db"},
		{name: "sqlite path", dsn: "/var/lib/app.db", want: "/var/lib/app.db"},
		{
			name:   "sqlite file uri with auth",
			dsn:    "file:app.db?_auth&_auth_user=admin&_auth_pass=s3cret",
			want:   "file:app.db?_auth&_auth_user=admin&_auth_pass=xxxxx",
			secret: "s3cret",
		},
		{
			name:   "mysql driver form",
			dsn:    "root:s3cret@tcp(localhost:3306)/app?parseTime=true",
			want:   "root:xxxxx@tcp(localhost:3306)/app?parseTime=true",
			secret: "s3cret",
		},
		{
			name:   "mysql driver form with at in password",
			dsn:    "root:p@ss@tcp(localhost:3306)/app",
			want:   "root:xxxxx@tcp(localhost:3306)/app",
			secret: "p@ss",
		},
		{name: "mysql driver form without password", dsn: "root@tcp(localhost)/app", want: "root@tcp(localhost)/app"},
		{
			name:   "libpq key value",
			dsn:    "host=localhost password=s3cret user=app dbname=app",
			want:   "host=localhost password=xxxxx user=app dbname=app",
			secret: "s3cret",
		},
		{
			name:   "libpq quoted password",
			dsn:    "host=localhost password='s3cret with spaces' user=app",
			want:   "host=localhost password=xxxxx user=app",
			secret: "with spaces",
		},
		{
			name:   "libpq quoted password with escaped quote",
			dsn:    `host=localhost password='it\'s s3cret' user=app`,
			want:   "host=localhost password=xxxxx user=app",
			secret: "s3cret",
		},
		{
			name:   "libpq unterminated quote hides the tail",
			dsn:    "host=localhost password='s3cret user=app",
			want:   "host=localhost password=xxxxx",
			secret: "s3cret",
		},
		{
			name:   "libpq spaces around equals and email user",
			dsn:    "user=me@example.com host=::1 PASSWORD = s3cret",
			want:   "user=me@example.com host=::1 PASSWORD = xxxxx",
			secret: "s3cret",
		},
		{
			name:   "semicolon connection string",
			dsn:    "Server=db;User Id=app;Password=s3cret;Database=app",
			want:   "Server=db;User Id=app;Password=xxxxx;Database=app",
			secret: "s3cret",
		},
		{name: "unrelated key containing pass", dsn: "host=db passage=1", want: "host=db passage=1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactDSN(tt.dsn)
			assert.Equal(t, tt.want, got)

			if tt.secret != "" {
				assert.NotContains(t, got, tt.secret)
			}
		})
	}
}

// TestInitMigrationsFailureHidesDSNPassword drives "forge db init" with DSNs
// that fail before anything is built and checks that neither the printed
// warning nor the returned error repeats the password.
func TestInitMigrationsFailureHidesDSNPassword(t *testing.T) {
	// Subtests are named by label, not by DSN, because t.TempDir derives its
	// path from the test name and the path is printed in the output.
	for name, dsn := range map[string]string{
		// Scheme-less, so driver resolution fails and quotes the DSN back.
		"mysql driver form": "admin:hunter2@tcp(db:3306)/app",
		// Unsupported scheme, so the init warning is the only place the DSN shows.
		"unsupported scheme": "oracle://admin:hunter2@db:1521/app",
	} {
		t.Run(name, func(t *testing.T) {
			p, migrationsDir := upgradePlugin(t, "")
			root := filepath.Dir(migrationsDir)
			require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.24\n"), 0o644))

			ctx := newFakeContext()
			ctx.strings["dsn"] = dsn

			err := p.initMigrations(ctx)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "hunter2")

			printed := strings.Join(ctx.output, "\n")
			assert.Contains(t, printed, "migration tables were not created", "the failure must still be reported")
			assert.NotContains(t, printed, "hunter2")
			assert.Contains(t, printed, "admin:xxxxx@", "the redacted DSN should still identify the user and host")
		})
	}
}
