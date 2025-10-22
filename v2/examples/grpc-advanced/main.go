package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// server implements the GreeterServer
type server struct {
	helloworld.UnimplementedGreeterServer
	logger forge.Logger
}

func (s *server) SayHello(ctx context.Context, in *helloworld.HelloRequest) (*helloworld.HelloReply, error) {
	s.logger.Info("received hello request", forge.F("name", in.GetName()))
	return &helloworld.HelloReply{Message: "Hello " + in.GetName() + "!"}, nil
}

// Custom authentication interceptor
func authInterceptor(logger forge.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip auth for health checks
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return handler(ctx, req)
		}

		// Get metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		// Check for API key
		apiKeys := md.Get("x-api-key")
		if len(apiKeys) == 0 {
			logger.Warn("authentication failed: missing API key")
			return nil, status.Error(codes.Unauthenticated, "missing API key")
		}

		// Validate API key (simplified - in production use proper auth)
		if apiKeys[0] != "secret-api-key-12345" {
			logger.Warn("authentication failed: invalid API key")
			return nil, status.Error(codes.Unauthenticated, "invalid API key")
		}

		logger.Debug("authentication successful")
		return handler(ctx, req)
	}
}

// Custom health checker
type customHealthChecker struct {
	logger forge.Logger
}

func (h *customHealthChecker) Check(ctx context.Context) error {
	// Check your dependencies here (database, cache, external services, etc.)
	h.logger.Debug("health check: all systems operational")
	return nil
}

func main() {
	// Create Forge app
	app := forge.NewApp(forge.AppConfig{
		Name:    "grpc-advanced-example",
		Version: "1.0.0",
	})

	// Configure gRPC with advanced settings
	grpcExt := grpc.NewExtension(
		grpc.WithAddress(":50051"),
		grpc.WithReflection(true),
		grpc.WithHealthCheck(true),
		grpc.WithMetrics(true),
		grpc.WithLogging(true),
		grpc.WithMaxRecvMsgSize(8*1024*1024), // 8MB
		grpc.WithMaxSendMsgSize(8*1024*1024), // 8MB
		grpc.WithMaxConcurrentStreams(100),
		// Uncomment to enable TLS:
		// grpc.WithTLS("server.crt", "server.key", ""),
		// Uncomment for mTLS:
		// grpc.WithTLS("server.crt", "server.key", "ca.crt"),
		// grpc.WithClientAuth(true),
	)

	if err := app.RegisterExtension(grpcExt); err != nil {
		log.Fatalf("Failed to register gRPC extension: %v", err)
	}

	// Start app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	// Get dependencies from DI container
	grpcServer, err := forge.Resolve[grpc.GRPC](app.Container(), "grpc")
	if err != nil {
		log.Fatalf("Failed to resolve gRPC: %v", err)
	}

	logger, err := forge.Resolve[forge.Logger](app.Container(), "logger")
	if err != nil {
		log.Fatalf("Failed to resolve logger: %v", err)
	}

	// Add custom interceptor for authentication
	grpcServer.AddUnaryInterceptor(authInterceptor(logger))
	logger.Info("custom auth interceptor registered")

	// Register custom health checker
	grpcServer.RegisterHealthChecker("greeter-service", &customHealthChecker{logger: logger})
	logger.Info("custom health checker registered")

	// Register the greeter service
	helloworld.RegisterGreeterServer(grpcServer.GetServer(), &server{logger: logger})

	// Display server info
	services := grpcServer.GetServices()
	fmt.Println("✓ gRPC server listening on :50051")
	fmt.Println("✓ Health check enabled with custom checker")
	fmt.Println("✓ Reflection enabled")
	fmt.Println("✓ Authentication interceptor enabled")
	fmt.Println("✓ Metrics and logging enabled")
	fmt.Printf("✓ Registered %d services:\n", len(services))
	for _, svc := range services {
		fmt.Printf("  - %s (%d methods)\n", svc.Name, len(svc.Methods))
	}
	fmt.Println("")
	fmt.Println("Test with:")
	fmt.Println("  # Without auth (will fail):")
	fmt.Println("  grpcurl -plaintext localhost:50051 helloworld.Greeter/SayHello -d '{\"name\":\"World\"}'")
	fmt.Println("")
	fmt.Println("  # With auth (will succeed):")
	fmt.Println("  grpcurl -plaintext -H 'x-api-key: secret-api-key-12345' localhost:50051 helloworld.Greeter/SayHello -d '{\"name\":\"World\"}'")
	fmt.Println("")
	fmt.Println("  # Health check (no auth required):")
	fmt.Println("  grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop...")

	// Display stats periodically
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-syscall.Signal(syscall.SIGUSR1):
				stats := grpcServer.GetStats()
				logger.Info("server statistics",
					forge.F("rpcs_started", stats.RPCsStarted),
					forge.F("rpcs_succeeded", stats.RPCsSucceeded),
					forge.F("rpcs_failed", stats.RPCsFailed),
					forge.F("active_streams", stats.ActiveStreams),
				)
			}
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down gracefully...")

	// Display final stats
	stats := grpcServer.GetStats()
	fmt.Printf("\nFinal Statistics:\n")
	fmt.Printf("  RPCs Started:   %d\n", stats.RPCsStarted)
	fmt.Printf("  RPCs Succeeded: %d\n", stats.RPCsSucceeded)
	fmt.Printf("  RPCs Failed:    %d\n", stats.RPCsFailed)
	fmt.Printf("  Active Streams: %d\n", stats.ActiveStreams)

	if err := app.Stop(ctx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
}


