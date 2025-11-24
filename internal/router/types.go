package router

import (
	"context"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

type Context = shared.Context
type Logger = logger.Logger

// Extension represents an official Forge extension that can be registered with an App.
// Extensions have full access to the framework and are first-party, trusted components.
//
// Extensions follow a standard lifecycle:
//  1. Register(app) - Register services with DI container
//  2. Start(ctx) - Start the extension
//  3. Health(ctx) - Check extension health (called periodically)
//  4. Stop(ctx) - Stop the extension (called during graceful shutdown)
type Extension interface {
	// Name returns the unique name of the extension
	Name() string

	// Version returns the semantic version of the extension
	Version() string

	// Description returns a human-readable description
	Description() string

	// Start starts the extension.
	// This is called after all extensions have been registered and the DI container has started.
	Start(ctx context.Context) error

	// Stop stops the extension gracefully.
	// Extensions are stopped in reverse dependency order.
	Stop(ctx context.Context) error

	// Health checks if the extension is healthy.
	// This is called periodically by the health check system.
	// Return nil if healthy, error otherwise.
	Health(ctx context.Context) error

	// Dependencies returns the names of extensions this extension depends on.
	// The app will ensure dependencies are started before this extension.
	Dependencies() []string
}

// InternalExtension is an optional interface that extensions can implement
// to indicate that all their routes should be excluded from schema generation.
//
// This is useful for:
//   - Internal monitoring/metrics extensions
//   - Debug/development extensions
//   - Admin/management extensions
//   - Infrastructure extensions
//
// Example:
//
//	type DebugExtension struct {
//	    forge.ExtensionBase
//	}
//
//	func (e *DebugExtension) ExcludeFromSchemas() bool {
//	    return true
//	}
//
// Routes registered by this extension will automatically be excluded from
// OpenAPI, AsyncAPI, and oRPC schema generation.
type InternalExtension interface {
	// ExcludeFromSchemas returns true if all routes from this extension
	// should be excluded from schema generation (OpenAPI, AsyncAPI, oRPC).
	ExcludeFromSchemas() bool
}
