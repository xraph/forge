package main

import (
	"context"
	"fmt"
	"log"

	"github.com/xraph/forge"
)

// loggingMiddleware creates a middleware that logs requests
func loggingMiddleware(prefix string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			fmt.Printf("✓ %s middleware executed for: %s\n", prefix, ctx.Request().URL.Path)
			return next(ctx)
		}
	}
}

// authMiddleware creates a middleware that simulates authentication
func authMiddleware() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			fmt.Printf("🔒 Auth middleware executed for: %s\n", ctx.Request().URL.Path)
			// In a real app, you'd check auth token here
			return next(ctx)
		}
	}
}

func main() {
	app := forge.New(
		forge.WithAppName("Group Middleware Scoping Example"),
		forge.WithAppVersion("1.0.0"),
		forge.WithHTTPAddress(":8080"),
	)

	router := app.Router()

	// UseGlobal - applies to ALL routes in the entire application
	router.UseGlobal(loggingMiddleware("GLOBAL"))

	// Use (scoped) - applies only to routes registered directly on this router
	router.Use(loggingMiddleware("ROOT-SCOPED"))

	// Create a protected routes group with auth middleware
	protectedRoutes := router.Group("")
	protectedRoutes.Use(authMiddleware())

	// Register protected routes
	// Will have: GLOBAL + ROOT-SCOPED (inherited) + Auth middleware
	err := protectedRoutes.GET("/protected", func(ctx forge.Context) error {
		return ctx.String(200, "This is a protected route")
	})
	if err != nil {
		log.Fatal(err)
	}

	// Register public routes directly on router
	// Will have: GLOBAL + ROOT-SCOPED middleware
	err = router.GET("/public", func(ctx forge.Context) error {
		return ctx.String(200, "This is a public route")
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create another independent group to demonstrate scoping
	apiRoutes := router.Group("/api")
	// Will inherit ROOT-SCOPED but not Auth middleware
	err = apiRoutes.GET("/status", func(ctx forge.Context) error {
		return ctx.String(200, "API is running")
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n🚀 Server starting on http://localhost:8080")
	fmt.Println("\nMiddleware behavior:")
	fmt.Println("  UseGlobal() - Applies to ALL routes everywhere")
	fmt.Println("  Use()       - Scoped to routes on this router/group and its children")
	fmt.Println("\nTest the endpoints:")
	fmt.Println("  curl http://localhost:8080/public     # GLOBAL + ROOT-SCOPED")
	fmt.Println("  curl http://localhost:8080/protected  # GLOBAL + ROOT-SCOPED + Auth")
	fmt.Println("  curl http://localhost:8080/api/status # GLOBAL + ROOT-SCOPED (no Auth)")
	fmt.Println()

	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}

	select {} // Keep running
}

