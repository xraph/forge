# Database (removed)

This extension is gone. Use [grove](https://github.com/xraph/grove) for the
database layer and [`github.com/xraph/forge/models`](../../models) for the base
model types.

Grove is a database toolkit with its own driver modules, migration engine,
hooks, schema tooling and observability. It is what `forge db` has built its
migration runner on since the CLI moved off this extension.

## Moving over

```bash
go get github.com/xraph/grove
go get github.com/xraph/grove/drivers/pgdriver
```

Drivers ship as separate modules. There are six: `pgdriver`, `mysqldriver`,
`sqlitedriver`, `mongodriver`, `clickhousedriver` and `tursodriver`. The last
two are new, this extension never had them.

Open a connection directly rather than registering an extension:

```go
import (
    "github.com/xraph/grove"
    _ "github.com/xraph/grove/drivers/pgdriver"
)

db, err := grove.Open("postgres://localhost/mydb",
    grove.WithPoolSize(25),
    grove.WithQueryTimeout(30*time.Second),
)
```

Models register on the connection, not through a package-level list:

```go
db.RegisterModel((*User)(nil), (*Post)(nil))
```

## Your models barely change

The nine base models moved to `github.com/xraph/forge/models` intact.
`models.BaseModel` has the same fields and the same `BeforeInsert` and
`BeforeUpdate` hooks as the one that used to live here.

What you have to touch is struct tags. Grove reads `grove:` where bun read
`bun:`:

```go
// before
ID int64 `bun:"id,pk,autoincrement" json:"id"`

// after
ID int64 `grove:"id,pk,autoincrement" json:"id"`
```

Tag contents are unchanged, so this is a rename rather than a rewrite.

## If you have migrations

This is the part that breaks on you, because the CLI changed underneath it.
The old scaffold re-exported this package's global collection:

```go
var Migrations = database.Migrations
```

The scaffold `forge db init` writes now declares a grove `Registry` and a
`Group`, so any file calling `Migrations.MustRegister` fails to compile. Grove
keys an applied migration on its group plus its version and has no global
collection to register into.

Three steps, in this order:

```bash
forge db init
```

Rewrites `migrations.go` against grove. Your migration files are left alone.

```bash
forge db adopt
```

Only if this database already has migrations applied. Grove has never heard of
them, so without adopt your next migrate re-runs every one of them against a
live schema. That is the failure mode to worry about. Adopt reads the old
`bun_migrations` table and records those versions as applied, and you can pass
`--dry-run` to see what it would claim before it claims anything.

```bash
forge db migrate
```

Both commands take `--app <name>` when you run a monorepo with per-app
migration groups.

## What grove does not carry over

Parts of this extension sat higher than anything grove offers, and there is no
drop-in:

- the Redis client and helpers, which have no grove equivalent at all
- the repository pattern
- bulk insert, update, upsert and delete
- pagination
- seeding
- the multi-database manager, which let one app hold several named connections

If you leaned on any of these, budget for porting them onto `grove.DB` by hand.
They are why you should plan this migration rather than try it in an afternoon.
