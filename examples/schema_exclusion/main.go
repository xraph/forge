package main

import (
	"github.com/xraph/forge"
)

func main() {
	// Create app
	app := forge.New()

	router := app.Router()

	// Public API - included in OpenAPI docs
	publicGroup := router.Group("/api/v1")
	publicGroup.GET("/users", getUsers,
		forge.WithName("list-users"),
		forge.WithSummary("Get list of users"),
	)
	publicGroup.GET("/users/:id", getUser,
		forge.WithName("get-user"),
		forge.WithSummary("Get user by ID"),
	)

	// Internal API - excluded from OpenAPI
	internalGroup := router.Group("/internal")
	internalGroup.GET("/health", detailedHealth,
		forge.WithOpenAPIExclude(),
	)
	internalGroup.GET("/metrics", metrics,
		forge.WithOpenAPIExclude(),
	)

	// Admin API - excluded from all schemas
	adminGroup := router.Group("/admin")
	adminGroup.GET("/users", adminGetAllUsers,
		forge.WithSchemaExclude(),
	)
	adminGroup.DELETE("/users/:id", adminDeleteUser,
		forge.WithSchemaExclude(),
	)

	// Debug endpoints - excluded from all schemas
	debugGroup := router.Group("/debug")
	debugGroup.GET("/routes", debugRoutes,
		forge.WithSchemaExclude(),
	)
	debugGroup.GET("/config", debugConfig,
		forge.WithSchemaExclude(),
	)

	// Note: WebSocket routes would be registered like this:
	// router.WebSocket("/ws/notifications", notificationsStreamHandler,
	//     forge.WithName("notifications-stream"),
	//     forge.WithSummary("Real-time notifications"),
	// )
	//
	// router.WebSocket("/ws/internal/logs", internalLogsStreamHandler,
	//     forge.WithAsyncAPIExclude(),
	// )

	// Run app
	app.Run()
}

// Handlers
func getUsers(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"users": []string{"user1", "user2"},
	})
}

func getUser(ctx forge.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(200, map[string]any{
		"id":   id,
		"name": "User " + id,
	})
}

func detailedHealth(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"status":  "healthy",
		"details": "Internal health check with detailed metrics",
	})
}

func metrics(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"requests": 1000,
		"errors":   5,
	})
}

func adminGetAllUsers(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"users": []string{"all", "users", "including", "deleted"},
	})
}

func adminDeleteUser(ctx forge.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(200, map[string]any{
		"message": "User " + id + " permanently deleted",
	})
}

func debugRoutes(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"routes": "list of all registered routes",
	})
}

func debugConfig(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"config": "current application configuration",
	})
}
