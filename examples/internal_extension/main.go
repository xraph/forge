package main

import (
	"context"

	"github.com/xraph/forge"
)

func main() {
	app := forge.New()

	// Register internal extension
	debugExt := NewDebugExtension(app)
	app.RegisterExtension(debugExt)

	// Register public API
	router := app.Router()
	router.GET("/api/users", getUsers,
		forge.WithName("list-users"),
		forge.WithSummary("Get all users"),
	)

	app.Run()
}

// DebugExtension is an internal extension that automatically
// excludes all its routes from schema generation.
type DebugExtension struct {
	app    forge.App
	logger forge.Logger
}

func NewDebugExtension(app forge.App) *DebugExtension {
	return &DebugExtension{
		app:    app,
		logger: app.Logger(),
	}
}

func (e *DebugExtension) Name() string        { return "debug" }
func (e *DebugExtension) Version() string     { return "1.0.0" }
func (e *DebugExtension) Description() string { return "Internal debug extension" }
func (e *DebugExtension) Dependencies() []string { return nil }
func (e *DebugExtension) Logger() forge.Logger   { return e.logger }

// Register registers the extension (required by Extension interface)
func (e *DebugExtension) Register(app forge.App) error {
	e.app = app
	e.logger = app.Logger()
	return nil
}

// ExcludeFromSchemas marks this extension as internal
// All routes will be automatically excluded from OpenAPI/AsyncAPI/oRPC
func (e *DebugExtension) ExcludeFromSchemas() bool {
	return true
}

func (e *DebugExtension) Start(ctx context.Context) error {
	router := e.app.Router()

	// Approach 1: Using WithExtensionExclusion for individual routes
	excludeOpt := forge.WithExtensionExclusion(e)
	router.GET("/internal/debug/status", e.handleStatus, excludeOpt)

	// Approach 2: Using ExtensionRoutes for multiple routes (recommended)
	// This is cleaner when registering many routes
	opts := forge.ExtensionRoutes(e)
	router.GET("/internal/debug/metrics", e.handleMetrics, opts...)
	router.GET("/internal/debug/config", e.handleConfig, opts...)

	// Approach 3: ExtensionRoutes with additional options
	opts = forge.ExtensionRoutes(e,
		forge.WithTags("debug", "internal"),
	)
	router.GET("/internal/debug/routes", e.handleRoutes, opts...)

	e.Logger().Info("debug extension routes registered (excluded from schemas)")

	return nil
}

func (e *DebugExtension) Stop(ctx context.Context) error {
	return nil
}

func (e *DebugExtension) Health(ctx context.Context) error {
	return nil
}

// Handlers
func (e *DebugExtension) handleStatus(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"status":  "healthy",
		"details": "Debug endpoint - not in public docs",
	})
}

func (e *DebugExtension) handleMetrics(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"requests": 1000,
		"errors":   5,
	})
}

func (e *DebugExtension) handleConfig(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"config": "internal configuration",
	})
}

func (e *DebugExtension) handleRoutes(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"routes": "list of all routes",
	})
}

// Public handler
func getUsers(ctx forge.Context) error {
	return ctx.JSON(200, map[string]any{
		"users": []string{"user1", "user2"},
	})
}

