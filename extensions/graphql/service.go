package graphql

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xraph/forge"
)

// GraphQLService wraps a GraphQL server implementation and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type GraphQLService struct {
	config  Config
	server  GraphQL
	logger  forge.Logger
	metrics forge.Metrics
}

// NewGraphQLService creates a new GraphQL service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewGraphQLService(config Config, container forge.Container, logger forge.Logger, metrics forge.Metrics) (*GraphQLService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid graphql config: %w", err)
	}

	// Create GraphQL server
	server, err := NewGraphQLServer(config, logger, metrics, container)
	if err != nil {
		return nil, fmt.Errorf("failed to create graphql server: %w", err)
	}

	return &GraphQLService{
		config:  config,
		server:  server,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *GraphQLService) Name() string {
	return "graphql-service"
}

// Start starts the GraphQL service.
// This is called automatically by Vessel during container.Start().
func (s *GraphQLService) Start(ctx context.Context) error {
	s.logger.Info("starting graphql service",
		forge.F("endpoint", s.config.Endpoint),
		forge.F("playground", s.config.EnablePlayground),
	)

	// GraphQL server doesn't have explicit Start, it's always ready after construction
	s.logger.Info("graphql service started")
	return nil
}

// Stop stops the GraphQL service.
// This is called automatically by Vessel during container.Stop().
func (s *GraphQLService) Stop(ctx context.Context) error {
	s.logger.Info("stopping graphql service")
	// GraphQL server cleanup happens automatically
	s.logger.Info("graphql service stopped")
	return nil
}

// Health checks if the GraphQL service is healthy.
func (s *GraphQLService) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("graphql server not initialized")
	}

	if err := s.server.Ping(ctx); err != nil {
		return fmt.Errorf("graphql health check failed: %w", err)
	}

	return nil
}

// Server returns the underlying GraphQL server implementation.
func (s *GraphQLService) Server() GraphQL {
	return s.server
}

// Ensure GraphQLService implements GraphQL interface for convenience

func (s *GraphQLService) RegisterType(name string, obj interface{}) error {
	return s.server.RegisterType(name, obj)
}

func (s *GraphQLService) RegisterQuery(name string, resolver FieldResolverFunc) error {
	return s.server.RegisterQuery(name, resolver)
}

func (s *GraphQLService) RegisterMutation(name string, resolver FieldResolverFunc) error {
	return s.server.RegisterMutation(name, resolver)
}

func (s *GraphQLService) RegisterSubscription(name string, resolver SubscriptionResolverFunc) error {
	return s.server.RegisterSubscription(name, resolver)
}

func (s *GraphQLService) GenerateSchema() (string, error) {
	return s.server.GenerateSchema()
}

func (s *GraphQLService) GetSchema() *GraphQLSchema {
	return s.server.GetSchema()
}

func (s *GraphQLService) ExecuteQuery(ctx context.Context, query string, variables map[string]interface{}) (*Response, error) {
	return s.server.ExecuteQuery(ctx, query, variables)
}

func (s *GraphQLService) HTTPHandler() http.Handler {
	return s.server.HTTPHandler()
}

func (s *GraphQLService) PlaygroundHandler() http.Handler {
	return s.server.PlaygroundHandler()
}

func (s *GraphQLService) Use(middleware Middleware) {
	s.server.Use(middleware)
}

func (s *GraphQLService) Ping(ctx context.Context) error {
	return s.server.Ping(ctx)
}
