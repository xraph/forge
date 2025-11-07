package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/uptrace/bun"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/database"
)

// User model
type User struct {
	ID        int64     `bun:",pk,autoincrement"`
	Name      string    `bun:",notnull"`
	Email     string    `bun:",unique,notnull"`
	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

func main() {
	// Create app with database extension
	app := forge.NewApp(forge.AppConfig{
		Name:    "database-demo",
		Version: "1.0.0",
		Extensions: []forge.Extension{
			database.NewExtension(database.WithDatabases(
				database.DatabaseConfig{
					Name:            "primary",
					Type:            database.TypeSQLite,
					DSN:             "file:demo.db?cache=shared",
					MaxOpenConns:    10,
					MaxIdleConns:    5,
					ConnMaxLifetime: 5 * time.Minute,
				},
			)),
		},
	})

	ctx := context.Background()

	// Start app
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Get database manager using constant
	dbManager := forge.Must[*database.DatabaseManager](app.Container(), database.ManagerKey)

	// Get SQL database
	db, err := dbManager.SQL("primary")
	if err != nil {
		log.Fatalf("Failed to get database: %v", err)
	}

	// Create table
	_, err = db.NewCreateTable().
		Model((*User)(nil)).
		IfNotExists().
		Exec(ctx)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Insert users
	users := []User{
		{Name: "Alice", Email: "alice@example.com"},
		{Name: "Bob", Email: "bob@example.com"},
		{Name: "Charlie", Email: "charlie@example.com"},
	}

	_, err = db.NewInsert().
		Model(&users).
		On("CONFLICT (email) DO NOTHING").
		Exec(ctx)
	if err != nil {
		log.Fatalf("Failed to insert users: %v", err)
	}

	fmt.Println("✓ Inserted users")

	// Query users
	var queriedUsers []User
	err = db.NewSelect().
		Model(&queriedUsers).
		Order("id ASC").
		Scan(ctx)
	if err != nil {
		log.Fatalf("Failed to query users: %v", err)
	}

	fmt.Println("\n📋 Users:")
	for _, user := range queriedUsers {
		fmt.Printf("  - %s (%s)\n", user.Name, user.Email)
	}

	// Update user
	_, err = db.NewUpdate().
		Model((*User)(nil)).
		Set("name = ?", "Alice Smith").
		Where("email = ?", "alice@example.com").
		Exec(ctx)
	if err != nil {
		log.Fatalf("Failed to update user: %v", err)
	}

	fmt.Println("\n✓ Updated Alice's name")

	db1, err := dbManager.Get("primary")
	if err != nil {
		log.Fatalf("Failed to get database: %v", err)
	}
	sqlDB, ok := db1.(*database.SQLDatabase)
	if !ok {
		log.Fatalf("Database is not a SQL database")
	}
	// Transaction example
	err = sqlDB.Transaction(ctx, func(tx bun.Tx) error {
		// Delete a user
		_, err := tx.NewDelete().
			Model((*User)(nil)).
			Where("email = ?", "bob@example.com").
			Exec(ctx)
		if err != nil {
			return err
		}

		// Insert a new user
		newUser := User{Name: "David", Email: "david@example.com"}
		_, err = tx.NewInsert().
			Model(&newUser).
			Exec(ctx)

		return err
	})
	if err != nil {
		log.Fatalf("Transaction failed: %v", err)
	}

	fmt.Println("✓ Transaction completed")

	// Final count
	count, err := db.NewSelect().
		Model((*User)(nil)).
		Count(ctx)
	if err != nil {
		log.Fatalf("Failed to count users: %v", err)
	}

	fmt.Printf("\n📊 Total users: %d\n", count)

	// Health check
	statuses := dbManager.HealthCheckAll(ctx)
	fmt.Println("\n💚 Health Check:")
	for name, status := range statuses {
		healthStr := "✗ unhealthy"
		if status.Healthy {
			healthStr = "✓ healthy"
		}
		fmt.Printf("  - %s: %s (latency: %v)\n", name, healthStr, status.Latency)
	}

	// Stats
	db1, err = dbManager.Get("primary")
	if err != nil {
		log.Fatalf("Failed to get database: %v", err)
	}
	stats := db1.Stats()
	fmt.Println("\n📈 Connection Pool Stats:")
	fmt.Printf("  - Open: %d\n", stats.OpenConnections)
	fmt.Printf("  - In Use: %d\n", stats.InUse)
	fmt.Printf("  - Idle: %d\n", stats.Idle)

	fmt.Println("\n✅ Database demo completed!")
}
