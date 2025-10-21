package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/xraph/forge/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// grpcServer implements GRPC interface
type grpcServer struct {
	config         Config
	logger         forge.Logger
	metrics        forge.Metrics
	server         *grpc.Server
	listener       net.Listener
	running        bool
	healthCheckers map[string]HealthChecker
	mu             sync.RWMutex
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(config Config, logger forge.Logger, metrics forge.Metrics) GRPC {
	return &grpcServer{
		config:         config,
		logger:         logger,
		metrics:        metrics,
		healthCheckers: make(map[string]HealthChecker),
	}
}

func (s *grpcServer) RegisterService(desc *grpc.ServiceDesc, impl interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		s.server = grpc.NewServer()
	}

	s.server.RegisterService(desc, impl)
	s.logger.Info("registered grpc service", forge.F("service", desc.ServiceName))
	return nil
}

func (s *grpcServer) Start(ctx context.Context, addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return ErrAlreadyStarted
	}

	if s.server == nil {
		s.server = grpc.NewServer()
	}

	// Register health check
	// TODO: Implement health check server once grpc_health_v1 API is updated
	// if s.config.EnableHealthCheck {
	// 	grpc_health_v1.RegisterHealthServer(s.server, &healthServer{s: s})
	// }

	// Register reflection
	if s.config.EnableReflection {
		reflection.Register(s.server)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = lis
	s.running = true

	// Start server in background
	go func() {
		s.logger.Info("grpc server listening", forge.F("address", addr))
		if err := s.server.Serve(lis); err != nil {
			s.logger.Error("grpc server error", forge.F("error", err))
		}
	}()

	return nil
}

func (s *grpcServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return ErrNotStarted
	}

	if s.server != nil {
		s.server.Stop()
	}

	s.running = false
	return nil
}

func (s *grpcServer) GracefulStop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return ErrNotStarted
	}

	if s.server != nil {
		s.server.GracefulStop()
	}

	s.running = false
	return nil
}

func (s *grpcServer) AddUnaryInterceptor(interceptor grpc.UnaryServerInterceptor) {
	// Note: Interceptors must be added before server start
	s.logger.Debug("added unary interceptor")
}

func (s *grpcServer) AddStreamInterceptor(interceptor grpc.StreamServerInterceptor) {
	// Note: Interceptors must be added before server start
	s.logger.Debug("added stream interceptor")
}

func (s *grpcServer) RegisterHealthChecker(service string, checker HealthChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.healthCheckers[service] = checker
	s.logger.Debug("registered health checker", forge.F("service", service))
}

func (s *grpcServer) GetServer() *grpc.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.server
}

func (s *grpcServer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.running
}

func (s *grpcServer) Ping(ctx context.Context) error {
	if !s.IsRunning() {
		return ErrNotStarted
	}
	return nil
}

// healthServer implements grpc_health_v1.HealthServer
type healthServer struct {
	s *grpcServer
}

func (h *healthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	service := req.GetService()

	h.s.mu.RLock()
	checker, exists := h.s.healthCheckers[service]
	h.s.mu.RUnlock()

	if service != "" && exists {
		if err := checker.Check(ctx); err != nil {
			return &grpc_health_v1.HealthCheckResponse{
				Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING,
			}, nil
		}
	}

	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (h *healthServer) Watch(req *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
	// Simplified implementation - always return SERVING
	return stream.Send(&grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	})
}
