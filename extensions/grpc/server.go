package grpc

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/xraph/forge"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// rpcDurationBuckets are the bucket bounds, in seconds, the semconv spec
// recommends for rpc.server.call.duration.
var rpcDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// Names of the services reflection.Register adds.
const (
	reflectionV1      = "grpc.reflection.v1.ServerReflection"
	reflectionV1Alpha = "grpc.reflection.v1alpha.ServerReflection"
)

// serviceRegistration is a service held back until Start builds the server.
type serviceRegistration struct {
	desc *grpc.ServiceDesc
	impl any
}

// grpcServer implements GRPC interface.
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
	pending                  []serviceRegistration
	stats                    ServerStats
	rpcDuration              *forge.Vec[forge.Histogram]
	mu                       sync.RWMutex
}

// NewGRPCServer creates a new gRPC server.
func NewGRPCServer(config Config, logger forge.Logger, metrics forge.Metrics) GRPC {
	s := &grpcServer{
		config:         config,
		logger:         logger,
		metrics:        metrics,
		healthCheckers: make(map[string]HealthChecker),
	}

	if metrics != nil && config.EnableMetrics {
		s.rpcDuration = forge.HistogramVec(metrics, "rpc.server.call.duration", forge.HistogramOpts{
			Buckets: rpcDurationBuckets,
			Help:    "Duration of gRPC calls served.",
			Unit:    "s",
		}, "rpc.system.name", "rpc.method", "rpc.response.status_code")
	}

	return s
}

// RegisterService buffers a service until Start builds the server. grpc-go
// can't add services once Serve has been called, so a running server returns
// ErrRegisterAfterStart. grpc-go also ends the process when Start applies a
// registration it rejects, so those come back here as ErrInvalidService.
func (s *grpcServer) RegisterService(desc *grpc.ServiceDesc, impl any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return ErrRegisterAfterStart
	}

	if err := s.checkRegistration(desc, impl); err != nil {
		return err
	}

	s.pending = append(s.pending, serviceRegistration{desc: desc, impl: impl})
	s.logger.Info("registered grpc service", forge.F("service", desc.ServiceName))

	return nil
}

// checkRegistration rejects what grpc-go's RegisterService would log.Fatalf
// on: an implementation that doesn't satisfy the descriptor's handler type,
// and a service name that is already taken, by a pending registration or by
// the health or reflection service the config turns on. s.mu must be held.
func (s *grpcServer) checkRegistration(desc *grpc.ServiceDesc, impl any) error {
	if desc == nil {
		return fmt.Errorf("%w: nil service descriptor", ErrInvalidService)
	}

	if impl != nil {
		if desc.HandlerType == nil {
			return fmt.Errorf("%w: %s has no handler type", ErrInvalidService, desc.ServiceName)
		}

		want := reflect.TypeOf(desc.HandlerType).Elem()
		if got := reflect.TypeOf(impl); !got.Implements(want) {
			return fmt.Errorf("%w: %v does not implement %v", ErrInvalidService, got, want)
		}
	}

	taken := s.config.EnableHealthCheck && desc.ServiceName == grpc_health_v1.Health_ServiceDesc.ServiceName ||
		s.config.EnableReflection && (desc.ServiceName == reflectionV1 || desc.ServiceName == reflectionV1Alpha)

	for _, reg := range s.pending {
		taken = taken || reg.desc.ServiceName == desc.ServiceName
	}

	if taken {
		return fmt.Errorf("%w: %s is already registered", ErrInvalidService, desc.ServiceName)
	}

	return nil
}

// Start is idempotent — calling it on an already-running server is a no-op.
func (s *grpcServer) Start(ctx context.Context, addr string) error {
	// Check if already running
	s.mu.RLock()

	if s.running {
		s.mu.RUnlock()

		return nil
	}

	s.mu.RUnlock()

	// Build server options from config (without holding lock to avoid deadlock)
	opts := s.buildServerOptions()

	// Now acquire write lock for modifications
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check running status after acquiring write lock
	if s.running {
		return nil
	}

	// Create server with options
	s.server = grpc.NewServer(opts...)
	s.stats.StartTime = time.Now().Unix()

	// Apply services registered before Start.
	for _, reg := range s.pending {
		s.server.RegisterService(reg.desc, reg.impl)
	}

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
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		// Drop the server built above, so GetServer and GetServices don't
		// report one that never started. The pending registrations stay, and
		// a later Start applies them to a fresh server.
		s.server = nil
		s.stats.StartTime = 0

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

// GetServer returns the underlying grpc.Server. It is nil until Start has
// built it, and a server that is already serving can't take new services, so
// registering a generated service on it crashes the process. Register
// services with RegisterService before Start instead.
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

// GetStats returns current server statistics.
func (s *grpcServer) GetStats() ServerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.stats
}

// GetServices returns information about registered services.
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

// buildServerOptions creates gRPC server options from config.
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

	// Server spans. The stats handler runs before the interceptors.
	if s.config.EnableTracing {
		opts = append(opts, serverStatsHandler())
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

// healthServer implements grpc_health_v1.HealthServer.
type healthServer struct {
	s *grpcServer
}

func (h *healthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	service := req.GetService()

	h.s.mu.RLock()
	checker, exists := h.s.healthCheckers[service]
	h.s.mu.RUnlock()

	// A failing checker is a health result, not an RPC failure: the client
	// learns about it through the NOT_SERVING status.
	status := grpc_health_v1.HealthCheckResponse_SERVING
	if service != "" && exists && checker.Check(ctx) != nil {
		status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}

	return &grpc_health_v1.HealthCheckResponse{Status: status}, nil
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
