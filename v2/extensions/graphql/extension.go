package graphql

import (
	"context"
	"fmt"

	"github.com/xraph/forge/v2"
)

// Extension implements forge.Extension for GraphQL functionality
type Extension struct {
	*forge.BaseExtension
	config Config
	server GraphQL
}

// NewExtension creates a new GraphQL extension
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("graphql", "2.0.0", "GraphQL API with automatic schema generation")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new GraphQL extension with a complete config
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the GraphQL extension with the app
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config
	finalConfig := DefaultConfig()
	if err := e.LoadConfig("graphql", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("graphql: failed to load required config: %w", err)
		}
		e.Logger().Warn("graphql: using default/programmatic config", forge.F("error", err.Error()))
	}
	e.config = finalConfig

	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("graphql config validation failed: %w", err)
	}

	server, err := NewGraphQLServer(e.config, e.Logger(), e.Metrics(), app.Container())
	if err != nil {
		return fmt.Errorf("failed to create graphql server: %w", err)
	}
	e.server = server

	if err := forge.RegisterSingleton(app.Container(), "graphql", func(c forge.Container) (GraphQL, error) {
		return e.server, nil
	}); err != nil {
		return fmt.Errorf("failed to register graphql service: %w", err)
	}

	e.Logger().Info("graphql extension registered",
		forge.F("endpoint", e.config.Endpoint),
		forge.F("playground", e.config.EnablePlayground),
	)

	return nil
}

// Start starts the GraphQL extension and registers routes
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting graphql extension")

	// Register routes with Forge router
	router := e.App().Router()

	// GraphQL endpoint (POST for queries/mutations, GET for introspection)
	if err := router.POST(e.config.Endpoint, e.handleGraphQL,
		forge.WithName("graphql"),
		forge.WithTags("api", "graphql"),
		forge.WithSummary("GraphQL API endpoint"),
	); err != nil {
		return fmt.Errorf("failed to register graphql POST route: %w", err)
	}

	if err := router.GET(e.config.Endpoint, e.handleGraphQL,
		forge.WithName("graphql-get"),
		forge.WithTags("api", "graphql"),
	); err != nil {
		return fmt.Errorf("failed to register graphql GET route: %w", err)
	}

	// Playground
	if e.config.EnablePlayground {
		if err := router.GET(e.config.PlaygroundEndpoint, e.handlePlayground,
			forge.WithName("graphql-playground"),
			forge.WithTags("dev", "graphql"),
			forge.WithSummary("GraphQL Playground UI"),
		); err != nil {
			return fmt.Errorf("failed to register playground route: %w", err)
		}
	}

	e.MarkStarted()
	e.Logger().Info("graphql extension started",
		forge.F("endpoint", e.config.Endpoint),
		forge.F("playground", e.config.EnablePlayground),
	)
	return nil
}

// handleGraphQL handles GraphQL requests
func (e *Extension) handleGraphQL(c forge.Context) error {
	e.server.HTTPHandler().ServeHTTP(c.Response(), c.Request())
	return nil
}

// handlePlayground handles playground requests
func (e *Extension) handlePlayground(c forge.Context) error {
	e.server.PlaygroundHandler().ServeHTTP(c.Response(), c.Request())
	return nil
}

// Stop stops the GraphQL extension
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping graphql extension")
	e.MarkStopped()
	e.Logger().Info("graphql extension stopped")
	return nil
}

// Health checks if the GraphQL server is healthy
func (e *Extension) Health(ctx context.Context) error {
	if e.server == nil {
		return fmt.Errorf("graphql server not initialized")
	}

	if err := e.server.Ping(ctx); err != nil {
		return fmt.Errorf("graphql health check failed: %w", err)
	}

	return nil
}

// GraphQL returns the GraphQL server instance
func (e *Extension) GraphQL() GraphQL {
	return e.server
}
