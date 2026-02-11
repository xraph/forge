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
func (s *ORPCService) Start(_ context.Context) error {
	s.logger.Info("starting orpc service",
		forge.F("endpoint", s.config.Endpoint),
	)

	s.logger.Info("orpc service started")
	return nil
}

// Stop stops the oRPC service.
// This is called automatically by Vessel during container.Stop().
func (s *ORPCService) Stop(_ context.Context) error {
	s.logger.Info("stopping orpc service")
	s.logger.Info("orpc service stopped")
	return nil
}

// Health checks if the oRPC service is healthy.
func (s *ORPCService) Health(_ context.Context) error {
	if s.server == nil {
		return fmt.Errorf("orpc server not initialized")
	}
	return nil
}

// Server returns the underlying oRPC server implementation.
func (s *ORPCService) Server() ORPC {
	return s.server
}

// Delegate ORPC interface methods to server

func (s *ORPCService) RegisterMethod(method *Method) error {
	return s.server.RegisterMethod(method)
}

func (s *ORPCService) GetMethod(name string) (*Method, error) {
	return s.server.GetMethod(name)
}

func (s *ORPCService) ListMethods() []Method {
	return s.server.ListMethods()
}

func (s *ORPCService) GenerateMethodFromRoute(route forge.RouteInfo) (*Method, error) {
	return s.server.GenerateMethodFromRoute(route)
}

func (s *ORPCService) HandleRequest(ctx context.Context, req *Request) *Response {
	return s.server.HandleRequest(ctx, req)
}

func (s *ORPCService) HandleBatch(ctx context.Context, requests []*Request) []*Response {
	return s.server.HandleBatch(ctx, requests)
}

func (s *ORPCService) OpenRPCDocument() *OpenRPCDocument {
	return s.server.OpenRPCDocument()
}

func (s *ORPCService) Use(interceptor Interceptor) {
	s.server.Use(interceptor)
}

func (s *ORPCService) GetStats() ServerStats {
	return s.server.GetStats()
}

func (s *ORPCService) SetRouter(router forge.Router) {
	s.server.SetRouter(router)
}
