package graphql

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for GraphQL functionality.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension
	config Config
	// No longer storing server - Vessel manages it
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

// Register registers the GraphQL extension with the app.
// This method loads configuration and registers service constructors.
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

	// Register GraphQLService constructor with Vessel using vessel.WithAliases for backward compatibility
	if err := e.RegisterConstructor(func(container forge.Container, logger forge.Logger, metrics forge.Metrics) (*GraphQLService, error) {
		return NewGraphQLService(finalConfig, container, logger, metrics)
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register graphql service: %w", err)
	}

	e.Logger().Info("graphql extension registered",
		forge.F("endpoint", finalConfig.Endpoint),
		forge.F("playground", finalConfig.EnablePlayground),
	)

	return nil
}

// Start starts the GraphQL extension and registers routes.
// Routes need the service, so we resolve it here.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting graphql extension")

	// Resolve GraphQL service from DI
	graphqlService, err := forge.InjectType[*GraphQLService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve graphql service: %w", err)
	}

	// Register routes with Forge router
	router := e.App().Router()

	// GraphQL endpoint (POST for queries/mutations, GET for introspection)
	if err := router.POST(e.config.Endpoint, func(c forge.Context) error {
		graphqlService.HTTPHandler().ServeHTTP(c.Response(), c.Request())
		return nil
	},
		forge.WithName("graphql"),
		forge.WithTags("api", "graphql"),
		forge.WithSummary("GraphQL API endpoint"),
	); err != nil {
		return fmt.Errorf("failed to register graphql POST route: %w", err)
	}

	if err := router.GET(e.config.Endpoint, func(c forge.Context) error {
		graphqlService.HTTPHandler().ServeHTTP(c.Response(), c.Request())
		return nil
	},
		forge.WithName("graphql-get"),
		forge.WithTags("api", "graphql"),
	); err != nil {
		return fmt.Errorf("failed to register graphql GET route: %w", err)
	}

	// Playground
	if e.config.EnablePlayground {
		if err := router.GET(e.config.PlaygroundEndpoint, func(c forge.Context) error {
			graphqlService.PlaygroundHandler().ServeHTTP(c.Response(), c.Request())
			return nil
		},
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

// Stop marks the extension as stopped.
// The actual server is stopped by Vessel calling GraphQLService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through GraphQLService.Health().
func (e *Extension) Health(ctx context.Context) error {
	return nil
}
