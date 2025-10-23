package graphql

import (
	"context"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/graphql/generated"
)

// graphqlImpl wraps gqlgen server to implement GraphQL interface
type graphqlImpl struct {
	server   *handler.Server
	schema   graphql.ExecutableSchema
	config   Config
	logger   forge.Logger
	metrics  forge.Metrics
	resolver *Resolver
}

// NewGraphQLServer creates a new GraphQL server with gqlgen
func NewGraphQLServer(config Config, logger forge.Logger, metrics forge.Metrics, container forge.Container) (GraphQL, error) {
	// Create resolver with DI
	resolver := NewResolver(container, logger, metrics, config)

	// Create executable schema
	cfg := generated.Config{Resolvers: resolver}
	schema := generated.NewExecutableSchema(cfg)

	// Create handler
	srv := handler.NewDefaultServer(schema)

	// Configure transports
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.MultipartForm{
		MaxMemory:     config.MaxUploadSize,
		MaxUploadSize: config.MaxUploadSize,
	})

	// WebSocket for subscriptions (future use)
	srv.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
	})

	// Add extensions
	if config.EnableIntrospection {
		srv.Use(extension.Introspection{})
	}

	if config.MaxComplexity > 0 {
		srv.Use(extension.FixedComplexityLimit(config.MaxComplexity))
	}

	if config.EnableQueryCache {
		srv.Use(extension.AutomaticPersistedQuery{
			Cache: lru.New(config.MaxCacheSize),
		})
	}

	// Add custom middleware for observability
	if config.EnableMetrics || config.EnableLogging {
		srv.AroundOperations(observabilityMiddleware(logger, metrics, config))
		srv.AroundResponses(responseMiddleware(config, logger))
	}

	return &graphqlImpl{
		server:   srv,
		schema:   schema,
		config:   config,
		logger:   logger,
		metrics:  metrics,
		resolver: resolver,
	}, nil
}

// Implement GraphQL interface methods

func (g *graphqlImpl) RegisterType(name string, obj interface{}) error {
	// Types are handled by schema generation in gqlgen
	g.logger.Debug("type registration handled by gqlgen", forge.F("name", name))
	return nil
}

func (g *graphqlImpl) RegisterQuery(name string, resolver FieldResolverFunc) error {
	// Queries are defined in schema, not registered dynamically
	g.logger.Debug("query registration handled by schema", forge.F("name", name))
	return nil
}

func (g *graphqlImpl) RegisterMutation(name string, resolver FieldResolverFunc) error {
	// Mutations are defined in schema, not registered dynamically
	g.logger.Debug("mutation registration handled by schema", forge.F("name", name))
	return nil
}

func (g *graphqlImpl) RegisterSubscription(name string, resolver SubscriptionResolverFunc) error {
	// Subscriptions are defined in schema, not registered dynamically
	g.logger.Debug("subscription registration handled by schema", forge.F("name", name))
	return nil
}

func (g *graphqlImpl) GenerateSchema() (string, error) {
	// Return SDL from schema using formatter
	astSchema := g.schema.Schema()

	// Simple SDL generation for now
	// In production, you might want to use a proper schema printer
	sdl := "# GraphQL Schema\n\n"

	if astSchema.Query != nil {
		sdl += "type Query {\n"
		for _, field := range astSchema.Query.Fields {
			sdl += "  " + field.Name + ": " + field.Type.String() + "\n"
		}
		sdl += "}\n\n"
	}

	if astSchema.Mutation != nil {
		sdl += "type Mutation {\n"
		for _, field := range astSchema.Mutation.Fields {
			sdl += "  " + field.Name + ": " + field.Type.String() + "\n"
		}
		sdl += "}\n\n"
	}

	return sdl, nil
}

func (g *graphqlImpl) GetSchema() *GraphQLSchema {
	// Convert gqlgen schema to wrapper format for introspection
	schema := &GraphQLSchema{
		Types:         make(map[string]*TypeDefinition),
		Queries:       make(map[string]*FieldDefinition),
		Mutations:     make(map[string]*FieldDefinition),
		Subscriptions: make(map[string]*FieldDefinition),
		Directives:    make([]*GraphQLDirective, 0),
	}

	// Extract from gqlgen schema
	astSchema := g.schema.Schema()
	if astSchema.Query != nil {
		for _, field := range astSchema.Query.Fields {
			schema.Queries[field.Name] = &FieldDefinition{
				Name:        field.Name,
				Description: field.Description,
				Type:        field.Type.String(),
			}
		}
	}

	if astSchema.Mutation != nil {
		for _, field := range astSchema.Mutation.Fields {
			schema.Mutations[field.Name] = &FieldDefinition{
				Name:        field.Name,
				Description: field.Description,
				Type:        field.Type.String(),
			}
		}
	}

	if astSchema.Subscription != nil {
		for _, field := range astSchema.Subscription.Fields {
			schema.Subscriptions[field.Name] = &FieldDefinition{
				Name:        field.Name,
				Description: field.Description,
				Type:        field.Type.String(),
			}
		}
	}

	return schema
}

func (g *graphqlImpl) ExecuteQuery(ctx context.Context, query string, variables map[string]interface{}) (*Response, error) {
	// For direct programmatic execution, use the HTTP handler with in-memory request
	// This is simpler than dealing with gqlgen's internal APIs

	// TODO: Implement direct execution using httptest or similar
	// For now, return a stub response
	g.logger.Debug("ExecuteQuery called (direct execution not yet implemented)",
		forge.F("query", query),
	)

	return &Response{
		Data: nil,
		Errors: []Error{{
			Message: "Direct query execution not yet implemented. Use HTTPHandler() for query execution.",
		}},
	}, nil
}

func (g *graphqlImpl) HTTPHandler() http.Handler {
	return g.server
}

func (g *graphqlImpl) PlaygroundHandler() http.Handler {
	return playground.Handler("GraphQL Playground", g.config.Endpoint)
}

func (g *graphqlImpl) Use(middleware Middleware) {
	// Middleware is applied via AroundOperations in gqlgen
	// This method is for backward compatibility
	g.logger.Debug("custom middleware should be added via AroundOperations")
}

func (g *graphqlImpl) EnableIntrospection(enable bool) {
	g.config.EnableIntrospection = enable
	// Note: This won't take effect until server is recreated
	g.logger.Warn("introspection setting changed, requires server restart")
}

func (g *graphqlImpl) Ping(ctx context.Context) error {
	// GraphQL server is stateless, always healthy if initialized
	return nil
}
