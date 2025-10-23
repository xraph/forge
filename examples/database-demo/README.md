# Database Extension Demo

This example demonstrates the Database Extension with SQLite.

## Features Demonstrated

- Database configuration and setup
- Bun ORM integration
- CRUD operations (Create, Read, Update, Delete)
- Transactions
- Health checks
- Connection pool statistics

## Running the Example

```bash
cd examples/database-demo
go run main.go
```

## What It Does

1. **Creates a SQLite database** with connection pooling
2. **Creates a users table** with Bun ORM
3. **Inserts sample users** (Alice, Bob, Charlie)
4. **Queries all users** and displays them
5. **Updates a user** (changes Alice's name)
6. **Performs a transaction** (deletes Bob, inserts David)
7. **Shows health check status** with latency
8. **Displays connection pool stats**

## Output

```
✓ Inserted users

📋 Users:
  - Alice (alice@example.com)
  - Bob (bob@example.com)
  - Charlie (charlie@example.com)

✓ Updated Alice's name
✓ Transaction completed

📊 Total users: 3

💚 Health Check:
  - primary: ✓ healthy (latency: 245µs)

📈 Connection Pool Stats:
  - Open: 1
  - In Use: 0
  - Idle: 1

✅ Database demo completed!
```

## Configuration

The example uses SQLite with these settings:

```go
database.Config{
    Databases: []database.DatabaseConfig{
        {
            Name:            "primary",
            Type:            database.TypeSQLite,
            DSN:             "file:demo.db?cache=shared",
            MaxOpenConns:    10,
            MaxIdleConns:    5,
            ConnMaxLifetime: 5 * time.Minute,
        },
    },
}
```

## Database Types Supported

- **Postgres**: `database.TypePostgres`
- **MySQL**: `database.TypeMySQL`
- **SQLite**: `database.TypeSQLite`
- **MongoDB**: `database.TypeMongoDB`

## Advanced Usage

See the design doc at `v2/design/028-database-extension.md` for:
- PostgreSQL and MySQL setup
- MongoDB usage
- Multiple database connections
- Migrations with Bun Migrate
- Advanced transaction patterns

