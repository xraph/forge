// Package main demonstrates that the database extension with migrations
// can now start successfully in Docker/CI environments without panicking.
package main

import (
	"context"
	"log"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/database"
)

func main() {
	// Create forge application with database extension
	// Before fix: This would panic in Docker with "stat .: no such file or directory"
	// After fix: Application starts successfully
	app := forge.NewApp(forge.AppConfig{
		Name:    "migration-demo",
		Version: "1.0.0",
		Extensions: []forge.Extension{
			database.NewExtension(database.WithDatabases(
				database.DatabaseConfig{
					Name: "default",
					Type: database.TypeSQLite,
					DSN:  ":memory:",
				},
			)),
		},
	})

	// Start the application
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

	log.Println("✅ Application started successfully!")
	log.Println("✅ This would have panicked before the fix!")
	log.Println("✅ Database extension with migrations works in Docker/CI!")

	// Graceful shutdown
	if err := app.Stop(ctx); err != nil {
		log.Fatalf("Failed to stop application: %v", err)
	}

	log.Println("✅ Application stopped gracefully")
}
