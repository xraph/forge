package orpc

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// ORPCService wraps an ORPC server implementation and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type ORPCService struct {
	config  Config
	server  ORPC
	logger  forge.Logger
	metrics forge.Metrics
}

// NewORPCService creates a new oRPC service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewORPCService(config Config, logger forge.Logger, metrics forge.Metrics) (*ORPCService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid orpc config: %w", err)
	}

	// Create oRPC server
	server := NewORPCServer(config, logger, metrics)

	return &ORPCService{
		config:  config,
		server:  server,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *ORPCService) Name() string {
	return "orpc-service"
}

// Start starts the oRPC service.
// This is called automatically by Vessel during container.Start().
func (s *ORPCService) Start(ctx context.Context) error {
	s.logger.Info("starting orpc service",
		forge.F("endpoint", s.config.Endpoint),
	)

	// oRPC server doesn't have explicit Start, it's always ready after construction
	s.logger.Info("orpc service started")
	return nil
}

// Stop stops the oRPC service.
// This is called automatically by Vessel during container.Stop().
func (s *ORPCService) Stop(ctx context.Context) error {
	s.logger.Info("stopping orpc service")
	// oRPC server cleanup happens automatically
	s.logger.Info("orpc service stopped")
	return nil
}

// Health checks if the oRPC service is healthy.
func (s *ORPCService) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("orpc server not initialized")
	}
	// oRPC doesn't have explicit health check, check if server exists
	return nil
}

// Server returns the underlying oRPC server implementation.
func (s *ORPCService) Server() ORPC {
	return s.server
}

// Delegate ORPC interface methods to server

func (s *ORPCService) RegisterMethod(name string, handler MethodHandler, schema *MethodSchema) error {
	return s.server.RegisterMethod(name, handler, schema)
}

func (s *ORPCService) RegisterBatchMethod(name string, handler BatchMethodHandler, schema *MethodSchema) error {
	return s.server.RegisterBatchMethod(name, handler, schema)
}

func (s *ORPCService) GetMethods() []MethodInfo {
	return s.server.GetMethods()
}

func (s *ORPCService) GetMethod(name string) (*MethodInfo, error) {
	return s.server.GetMethod(name)
}

func (s *ORPCService) GetOpenRPCDocument() (*OpenRPCDocument, error) {
	return s.server.GetOpenRPCDocument()
}

func (s *ORPCService) ExecuteMethod(ctx context.Context, name string, params interface{}) (interface{}, error) {
	return s.server.ExecuteMethod(ctx, name, params)
}

func (s *ORPCService) ExecuteBatch(ctx context.Context, requests []BatchRequest) []BatchResponse {
	return s.server.ExecuteBatch(ctx, requests)
}

func (s *ORPCService) Use(middleware ORPCMiddleware) {
	s.server.Use(middleware)
}
