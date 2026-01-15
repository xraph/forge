package main

import (
	"context"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/graphql"
)

func main() {
	// Create Forge application
	app := forge.NewApp(forge.DefaultAppConfig())

	// Register GraphQL extension with configuration
	gqlExt := graphql.NewExtension(
		graphql.WithEndpoint("/graphql"),
		graphql.WithPlayground(true),
		graphql.WithIntrospection(true),
		graphql.WithMaxComplexity(1000),
		graphql.WithQueryCache(true, 5*time.Minute),
		graphql.WithMetrics(true),
		graphql.WithTracing(true),
	)

	if err := app.RegisterExtension(gqlExt); err != nil {
		log.Fatalf("Failed to register GraphQL extension: %v", err)
	}

	// Start the application
	if err := app.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}

	log.Println("🚀 GraphQL server is running!")
	log.Println("📊 GraphQL endpoint: http://localhost:8080/graphql")
	log.Println("🎮 Playground: http://localhost:8080/playground")
	log.Println("")
	log.Println("Example queries:")
	log.Println("  { hello(name: \"World\") }")
	log.Println("  { version }")
	log.Println("")
	log.Println("Example mutations:")
	log.Println("  mutation { echo(message: \"Hello\") }")

	// Run server
	if err := app.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
