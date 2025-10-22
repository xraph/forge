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
	"google.golang.org/grpc/examples/helloworld/helloworld"
)

// server implements the GreeterServer
type server struct {
	helloworld.UnimplementedGreeterServer
	logger forge.Logger
}

func (s *server) SayHello(ctx context.Context, in *helloworld.HelloRequest) (*helloworld.HelloReply, error) {
	s.logger.Info("received hello request", forge.F("name", in.GetName()))
	return &helloworld.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func main() {
	// Create Forge app
	app := forge.NewApp(forge.AppConfig{
		Name:    "grpc-basic-example",
		Version: "1.0.0",
	})

	// Register gRPC extension
	grpcExt := grpc.NewExtension(
		grpc.WithAddress(":50051"),
		grpc.WithReflection(true),
		grpc.WithHealthCheck(true),
		grpc.WithMetrics(true),
		grpc.WithLogging(true),
	)

	if err := app.RegisterExtension(grpcExt); err != nil {
		log.Fatalf("Failed to register gRPC extension: %v", err)
	}

	// Start app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	// Get gRPC server and register service
	grpcServer, err := forge.Resolve[grpc.GRPC](app.Container(), "grpc")
	if err != nil {
		log.Fatalf("Failed to resolve gRPC: %v", err)
	}

	// Get logger for service
	logger, err := forge.Resolve[forge.Logger](app.Container(), "logger")
	if err != nil {
		log.Fatalf("Failed to resolve logger: %v", err)
	}

	// Register the greeter service
	helloworld.RegisterGreeterServer(grpcServer.GetServer(), &server{logger: logger})

	fmt.Println("✓ gRPC server listening on :50051")
	fmt.Println("✓ Health check enabled")
	fmt.Println("✓ Reflection enabled")
	fmt.Println("")
	fmt.Println("Test with:")
	fmt.Println("  grpcurl -plaintext localhost:50051 helloworld.Greeter/SayHello -d '{\"name\":\"World\"}'")
	fmt.Println("  grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check")
	fmt.Println("  grpcurl -plaintext localhost:50051 list")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop...")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down gracefully...")
	if err := app.Stop(ctx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
}


