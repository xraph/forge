package main

import (
	"github.com/xraph/forge"
)

func main() {
	app := forge.New()
	router := app.Router()

	// Public API routes - these WILL appear in schemas
	router.GET("/api/users", getUsers,
		forge.WithName("list-users"),
		forge.WithSummary("Get all users"),
	)

	router.GET("/api/products", getProducts,
		forge.WithName("list-products"),
		forge.WithSummary("Get all products"),
	)

	// Internal admin group - these will NOT appear in schemas
	adminGroup := router.Group("/admin", forge.WithGroupSchemaExclude())
	adminGroup.GET("/users", adminListUsers)
	adminGroup.DELETE("/users/:id", adminDeleteUser)
	adminGroup.POST("/cache/flush", adminFlushCache)
	adminGroup.GET("/config", adminGetConfig)

	// Debug group - also excluded from schemas
	debugGroup := router.Group("/debug", forge.WithGroupSchemaExclude())
	debugGroup.GET("/status", debugStatus)
	debugGroup.GET("/routes", debugRoutes)
	debugGroup.GET("/metrics", debugMetrics)

	// Internal monitoring group with additional options
	monitoringGroup := router.Group("/internal/monitoring",
		forge.WithGroupSchemaExclude(),
		forge.WithGroupTags("monitoring", "internal"),
	)
	monitoringGroup.GET("/health", monitoringHealth)
	monitoringGroup.GET("/metrics/detailed", monitoringDetailedMetrics)

	app.Logger().Info("Starting application...")
	app.Logger().Info("Public routes: /api/users, /api/products")
	app.Logger().Info("Internal routes (excluded from schemas): /admin/*, /debug/*, /internal/monitoring/*")
	app.Logger().Info("Check OpenAPI spec at: http://localhost:8080/openapi.json")

	app.Run()
}

// Public API handlers
func getUsers(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"users": []string{"alice", "bob"},
	})
}

func getProducts(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"products": []string{"product1", "product2"},
	})
}

// Admin handlers (excluded from schemas)
func adminListUsers(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"message": "Admin: List all users with sensitive data",
		"users": []map[string]any{
			{"id": 1, "email": "alice@example.com", "role": "admin"},
			{"id": 2, "email": "bob@example.com", "role": "user"},
		},
	})
}

func adminDeleteUser(ctx forge.Context) error {
	userID := ctx.Param("id")
	return ctx.JSON(200, map[string]any{
		"message": "Admin: Deleted user " + userID,
	})
}

func adminFlushCache(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"message": "Admin: Cache flushed",
	})
}

func adminGetConfig(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"message": "Admin: Internal configuration",
		"config": map[string]any{
			"database": "postgres://...",
			"secrets":  "***",
		},
	})
}

// Debug handlers (excluded from schemas)
func debugStatus(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"status": "healthy",
		"debug":  true,
	})
}

func debugRoutes(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"routes": "list of all routes",
	})
}

func debugMetrics(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"requests": 1000,
		"errors":   5,
	})
}

// Monitoring handlers (excluded from schemas)
func monitoringHealth(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"health": "detailed health check",
	})
}

func monitoringDetailedMetrics(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"metrics": "detailed prometheus metrics",
	})
}

