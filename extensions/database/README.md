# Database (removed)

This extension is gone. Use [grove](https://github.com/xraph/grove) instead.

Grove is a rethink of the ORM rather than a port of this one, so the query
builder reads differently. That part you'll notice immediately. What you get back is drivers this never had:
Postgres, MySQL, SQLite, MongoDB, ClickHouse, Turso and Elasticsearch, each in
its own module so you compile only what you use.

## Moving over

```bash
go get github.com/xraph/grove
```

Where you registered this extension, register grove's:

```go
import (
    groveext "github.com/xraph/grove/extension"
    _ "github.com/xraph/grove/drivers/pgdriver"
)

app.RegisterExtension(groveext.New(
    groveext.WithDatabaseDSN("primary", "postgres://localhost/mydb"),
))
```

Import the driver you need for its side effects. It registers the DSN scheme,
and without it the scheme will not resolve.

## Migrations

Read this before you run anything.

Grove records applied migrations in its own tables, `grove_migrations` and
`grove_migration_locks`. It knows nothing about the ones you applied through
this extension, so left alone it'll happily run your entire history again
against a live schema. On a production database. Once.

`forge db adopt` exists for that. It reads your old table and records those
migrations into grove, so an upgrade stays an upgrade:

```bash
forge db adopt --dry-run
forge db adopt
```

Run the dry run first. It writes nothing, and it tells you exactly what it
would adopt. The command refuses to do anything when it cannot find the old table,
rather than reporting success on a database it never touched.

Migrations themselves are Go values now. Grove has no filesystem discovery, so
a loose `.sql` file is never found. `forge db create-sql` still writes the SQL
pair and adds a small Go file that embeds and registers it, and that Go file
belongs in your commit alongside the SQL.

## Base models

The nine embeddable base models moved to
[github.com/xraph/forge/models](../../models). `BaseModel`, `UUIDModel`,
`XIDModel`, the soft delete and audit variants: same fields, same column names,
so your tables still match. The struct tags read `grove:` now instead of `bun:`.

## What has no equivalent

Redis. This extension held Redis connections alongside SQL ones, and grove is
an ORM, so it has no Redis driver. If you were borrowing a Redis client from
the database manager, give that component its own connection. The queue
extension did exactly this and it cost one config field.
