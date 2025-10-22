package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// grpcServer implements GRPC interface
type grpcServer struct {
	config                   Config
	logger                   forge.Logger
	metrics                  forge.Metrics
	server                   *grpc.Server
	listener                 net.Listener
	running                  bool
	healthCheckers           map[string]HealthChecker
	customUnaryInterceptors  []grpc.UnaryServerInterceptor
	customStreamInterceptors []grpc.StreamServerInterceptor
	stats                    ServerStats
	mu                       sync.RWMutex
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

	// Build server options from config
	opts := s.buildServerOptions()

	// Create server with options
	s.server = grpc.NewServer(opts...)
	s.stats.StartTime = time.Now().Unix()

	// Register health check
	if s.config.EnableHealthCheck {
		grpc_health_v1.RegisterHealthServer(s.server, &healthServer{s: s})
		s.logger.Info("grpc health check enabled")
	}

	// Register reflection
	if s.config.EnableReflection {
		reflection.Register(s.server)
		s.logger.Info("grpc reflection enabled")
	}

	// Listen on address
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
	s.mu.Lock()
	defer s.mu.Unlock()

	s.customUnaryInterceptors = append(s.customUnaryInterceptors, interceptor)
	s.logger.Debug("added custom unary interceptor")
}

func (s *grpcServer) AddStreamInterceptor(interceptor grpc.StreamServerInterceptor) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.customStreamInterceptors = append(s.customStreamInterceptors, interceptor)
	s.logger.Debug("added custom stream interceptor")
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

// GetStats returns current server statistics
func (s *grpcServer) GetStats() ServerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stats
}

// GetServices returns information about registered services
func (s *grpcServer) GetServices() []ServiceInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server == nil {
		return nil
	}

	services := []ServiceInfo{}
	for name, info := range s.server.GetServiceInfo() {
		methods := make([]MethodInfo, 0, len(info.Methods))
		for _, method := range info.Methods {
			methods = append(methods, MethodInfo{
				Name:           method.Name,
				IsClientStream: method.IsClientStream,
				IsServerStream: method.IsServerStream,
			})
		}

		services = append(services, ServiceInfo{
			Name:    name,
			Methods: methods,
		})
	}

	return services
}

// buildServerOptions creates gRPC server options from config
func (s *grpcServer) buildServerOptions() []grpc.ServerOption {
	var opts []grpc.ServerOption

	// Message size limits
	if s.config.MaxRecvMsgSize > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(s.config.MaxRecvMsgSize))
	}
	if s.config.MaxSendMsgSize > 0 {
		opts = append(opts, grpc.MaxSendMsgSize(s.config.MaxSendMsgSize))
	}

	// Concurrent streams
	if s.config.MaxConcurrentStreams > 0 {
		opts = append(opts, grpc.MaxConcurrentStreams(s.config.MaxConcurrentStreams))
	}

	// Connection timeout
	if s.config.ConnectionTimeout > 0 {
		opts = append(opts, grpc.ConnectionTimeout(s.config.ConnectionTimeout))
	}

	// Keepalive parameters
	opts = append(opts, grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle:     s.config.Keepalive.Time,
		MaxConnectionAge:      s.config.Keepalive.Time * 2,
		MaxConnectionAgeGrace: s.config.Keepalive.Timeout,
		Time:                  s.config.Keepalive.Time,
		Timeout:               s.config.Keepalive.Timeout,
	}))

	// Keepalive enforcement policy
	if s.config.Keepalive.EnforcementPolicy {
		opts = append(opts, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             s.config.Keepalive.MinTime,
			PermitWithoutStream: s.config.Keepalive.PermitWithoutStream,
		}))
	}

	// TLS credentials
	if s.config.EnableTLS {
		creds, err := s.loadTLSCredentials()
		if err != nil {
			s.logger.Error("failed to load TLS credentials", forge.F("error", err))
		} else {
			opts = append(opts, grpc.Creds(creds))
			s.logger.Info("grpc TLS enabled")
		}
	}

	// Add interceptors for observability
	if s.config.EnableMetrics || s.config.EnableLogging || s.config.EnableTracing {
		unaryInterceptors := s.buildUnaryInterceptors()
		streamInterceptors := s.buildStreamInterceptors()

		if len(unaryInterceptors) > 0 {
			opts = append(opts, grpc.ChainUnaryInterceptor(unaryInterceptors...))
		}
		if len(streamInterceptors) > 0 {
			opts = append(opts, grpc.ChainStreamInterceptor(streamInterceptors...))
		}
	}

	return opts
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

func (h *healthServer) List(ctx context.Context, req *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	// Return list of registered health check services
	// Note: The actual field name depends on the protobuf definition
	return &grpc_health_v1.HealthListResponse{}, nil
}
