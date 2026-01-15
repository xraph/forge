package grpc

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"google.golang.org/grpc"
)

// GRPCService wraps a GRPC server implementation and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type GRPCService struct {
	config  Config
	server  GRPC
	logger  forge.Logger
	metrics forge.Metrics
}

// NewGRPCService creates a new gRPC service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewGRPCService(config Config, logger forge.Logger, metrics forge.Metrics) (*GRPCService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid grpc config: %w", err)
	}

	// Create gRPC server
	server := NewGRPCServer(config, logger, metrics)

	return &GRPCService{
		config:  config,
		server:  server,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *GRPCService) Name() string {
	return "grpc-service"
}

// Start starts the gRPC service.
// This is called automatically by Vessel during container.Start().
func (s *GRPCService) Start(ctx context.Context) error {
	s.logger.Info("starting grpc service",
		forge.F("address", s.config.Address),
		forge.F("tls", s.config.EnableTLS),
	)

	if err := s.server.Start(ctx, s.config.Address); err != nil {
		return fmt.Errorf("failed to start grpc server: %w", err)
	}

	s.logger.Info("grpc service started",
		forge.F("address", s.config.Address),
	)

	return nil
}

// Stop stops the gRPC service gracefully.
// This is called automatically by Vessel during container.Stop().
func (s *GRPCService) Stop(ctx context.Context) error {
	s.logger.Info("stopping grpc service")

	if s.server != nil {
		if err := s.server.GracefulStop(ctx); err != nil {
			s.logger.Error("failed to stop grpc server gracefully", forge.F("error", err))
			// Force stop
			if err := s.server.Stop(ctx); err != nil {
				s.logger.Error("failed to force stop grpc server", forge.F("error", err))
			}
		}
	}

	s.logger.Info("grpc service stopped")
	return nil
}

// Health checks if the gRPC service is healthy.
func (s *GRPCService) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("grpc server not initialized")
	}
	// GRPC interface doesn't have a Ping method, so we check if server exists
	return nil
}

// Server returns the underlying gRPC server implementation.
func (s *GRPCService) Server() GRPC {
	return s.server
}

// Ensure GRPCService implements GRPC interface for convenience

func (s *GRPCService) RegisterService(desc *grpc.ServiceDesc, impl interface{}) error {
	return s.server.RegisterService(desc, impl)
}

func (s *GRPCService) AddUnaryInterceptor(interceptor grpc.UnaryServerInterceptor) {
	s.server.AddUnaryInterceptor(interceptor)
}

func (s *GRPCService) AddStreamInterceptor(interceptor grpc.StreamServerInterceptor) {
	s.server.AddStreamInterceptor(interceptor)
}

func (s *GRPCService) RegisterHealthChecker(service string, checker HealthChecker) {
	s.server.RegisterHealthChecker(service, checker)
}

func (s *GRPCService) GetServer() *grpc.Server {
	return s.server.GetServer()
}

func (s *GRPCService) IsRunning() bool {
	return s.server.IsRunning()
}

func (s *GRPCService) GetStats() ServerStats {
	return s.server.GetStats()
}

func (s *GRPCService) GetServices() []ServiceInfo {
	return s.server.GetServices()
}

func (s *GRPCService) Ping(ctx context.Context) error {
	return s.server.Ping(ctx)
}
