package grpc

import (
	"context"

	"google.golang.org/grpc"
)

// GRPC represents a unified gRPC server interface
type GRPC interface {
	// Service registration
	RegisterService(desc *grpc.ServiceDesc, impl interface{}) error

	// Server management
	Start(ctx context.Context, addr string) error
	Stop(ctx context.Context) error
	GracefulStop(ctx context.Context) error

	// Interceptors
	AddUnaryInterceptor(interceptor grpc.UnaryServerInterceptor)
	AddStreamInterceptor(interceptor grpc.StreamServerInterceptor)

	// Health checking
	RegisterHealthChecker(service string, checker HealthChecker)

	// Server info
	GetServer() *grpc.Server
	IsRunning() bool

	// Health
	Ping(ctx context.Context) error
}

// HealthChecker checks service health
type HealthChecker interface {
	Check(ctx context.Context) error
}

// ServiceInfo contains service metadata
type ServiceInfo struct {
	Name        string
	Methods     []MethodInfo
	Description string
}

// MethodInfo contains method metadata
type MethodInfo struct {
	Name           string
	IsClientStream bool
	IsServerStream bool
	InputType      string
	OutputType     string
}

// ServerStats contains server statistics
type ServerStats struct {
	StartTime        int64
	TotalConnections int64
	ActiveStreams    int64
	RPCsStarted      int64
	RPCsSucceeded    int64
	RPCsFailed       int64
}
