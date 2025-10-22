# gRPC Extension for Forge v2

Production-ready gRPC server with TLS, observability, and streaming support.

## Features

- ✅ **Full gRPC Support**: Unary, client streaming, server streaming, bidirectional streaming
- ✅ **TLS/mTLS**: Secure communication with client authentication
- ✅ **Health Checks**: Built-in gRPC health checking protocol
- ✅ **Reflection**: Service discovery support
- ✅ **Observability**: Automatic metrics, logging, and tracing
- ✅ **Interceptors**: Custom middleware support
- ✅ **Configuration**: Keepalive, message sizes, concurrency limits
- ✅ **Server Statistics**: Track RPCs, streams, and performance

## Quick Start

```go
package main

import (
    "context"
    "github.com/xraph/forge/v2"
    "github.com/xraph/forge/v2/extensions/grpc"
    pb "your/proto/package"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Name: "grpc-service",
    })
    
    // Register gRPC extension
    app.RegisterExtension(grpc.NewExtension(
        grpc.WithAddress(":50051"),
        grpc.WithReflection(true),
        grpc.WithHealthCheck(true),
    ))
    
    // Start app
    app.Start(context.Background())
    
    // Get gRPC server and register service
    grpcServer, _ := forge.Resolve[grpc.GRPC](app.Container(), "grpc")
    pb.RegisterYourServiceServer(grpcServer.GetServer(), &yourServiceImpl{})
    
    app.Run(context.Background(), ":8080")
}
```

## Configuration

### YAML Configuration

```yaml
extensions:
  grpc:
    address: ":50051"
    max_recv_msg_size: 4194304  # 4MB
    max_send_msg_size: 4194304  # 4MB
    max_concurrent_streams: 100
    connection_timeout: 120s
    
    # TLS/mTLS
    enable_tls: true
    tls_cert_file: "server.crt"
    tls_key_file: "server.key"
    tls_ca_file: "ca.crt"
    client_auth: true  # Require client certificates
    
    # Features
    enable_health_check: true
    enable_reflection: true
    enable_metrics: true
    enable_tracing: true
    enable_logging: true
    
    # Keepalive
    keepalive:
      time: 2h
      timeout: 20s
      enforcement_policy: true
      min_time: 5m
      permit_without_stream: false
```

### Programmatic Configuration

```go
grpc.NewExtension(
    grpc.WithAddress(":50051"),
    grpc.WithMaxMessageSize(8 * 1024 * 1024), // 8MB
    grpc.WithMaxConcurrentStreams(100),
    grpc.WithTLS("server.crt", "server.key", "ca.crt"),
    grpc.WithClientAuth(true),
    grpc.WithHealthCheck(true),
    grpc.WithReflection(true),
    grpc.WithMetrics(true),
)
```

## TLS/mTLS Setup

### Generate Certificates

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 365 -key ca.key -out ca.crt

# Generate server certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key -set_serial 01 -out server.crt

# Generate client certificate (for mTLS)
openssl genrsa -out client.key 4096
openssl req -new -key client.key -out client.csr
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key -set_serial 02 -out client.crt
```

### Enable TLS

```go
grpc.NewExtension(
    grpc.WithTLS("server.crt", "server.key", ""),
)
```

### Enable mTLS (Mutual TLS)

```go
grpc.NewExtension(
    grpc.WithTLS("server.crt", "server.key", "ca.crt"),
    grpc.WithClientAuth(true), // Verify client certificates
)
```

## Custom Interceptors

### Add Custom Middleware

```go
// Create custom interceptor
func authInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    // Check authentication
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok {
        return nil, status.Error(codes.Unauthenticated, "missing metadata")
    }
    
    token := md.Get("authorization")
    if len(token) == 0 {
        return nil, status.Error(codes.Unauthenticated, "missing token")
    }
    
    // Validate token...
    
    return handler(ctx, req)
}

// Register interceptor before starting the server
grpcServer, _ := forge.Resolve[grpc.GRPC](app.Container(), "grpc")
grpcServer.AddUnaryInterceptor(authInterceptor)
```

## Health Checks

The extension automatically registers the gRPC health checking protocol.

### Check Health

```bash
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

### Register Custom Health Checkers

```go
type myHealthChecker struct{}

func (h *myHealthChecker) Check(ctx context.Context) error {
    // Check dependencies (database, cache, etc.)
    return nil
}

grpcServer.RegisterHealthChecker("my-service", &myHealthChecker{})
```

## Metrics

Automatic metrics collection when `enable_metrics: true`:

- `grpc_unary_started_total` - Total unary RPCs started
- `grpc_unary_succeeded_total` - Successful unary RPCs
- `grpc_unary_failed_total` - Failed unary RPCs (with code label)
- `grpc_unary_duration_seconds` - Unary RPC duration histogram
- `grpc_stream_active` - Current active streams
- `grpc_stream_duration_seconds` - Stream duration histogram
- `grpc_stream_succeeded_total` - Successful streams
- `grpc_stream_failed_total` - Failed streams (with code label)

## Server Statistics

Get runtime statistics:

```go
stats := grpcServer.GetStats()
fmt.Printf("Start Time: %d\n", stats.StartTime)
fmt.Printf("RPCs Started: %d\n", stats.RPCsStarted)
fmt.Printf("RPCs Succeeded: %d\n", stats.RPCsSucceeded)
fmt.Printf("RPCs Failed: %d\n", stats.RPCsFailed)
fmt.Printf("Active Streams: %d\n", stats.ActiveStreams)
```

## Service Discovery

List registered services:

```go
services := grpcServer.GetServices()
for _, service := range services {
    fmt.Printf("Service: %s\n", service.Name)
    for _, method := range service.Methods {
        fmt.Printf("  Method: %s (client=%v, server=%v)\n", 
            method.Name, method.IsClientStream, method.IsServerStream)
    }
}
```

## Testing

```go
func TestGRPCService(t *testing.T) {
    app := forge.NewApp(forge.AppConfig{Name: "test"})
    app.RegisterExtension(grpc.NewExtension(
        grpc.WithAddress("127.0.0.1:0"), // Random port
    ))
    
    app.Start(context.Background())
    defer app.Stop(context.Background())
    
    // Get server and register service
    grpcServer, _ := forge.Resolve[grpc.GRPC](app.Container(), "grpc")
    pb.RegisterTestServiceServer(grpcServer.GetServer(), &testImpl{})
    
    // Create client and test...
}
```

## Best Practices

1. **Always enable TLS in production** - Use `WithTLS()` to secure your gRPC traffic
2. **Use mTLS for service-to-service communication** - Enable client authentication with `WithClientAuth(true)`
3. **Set appropriate message size limits** - Use `WithMaxRecvMsgSize()` and `WithMaxSendMsgSize()` to prevent memory exhaustion
4. **Configure keepalive to detect dead connections** - Adjust keepalive settings based on your network environment
5. **Enable health checks for load balancers** - Use `WithHealthCheck(true)` and register custom health checkers
6. **Use reflection in development only** - Disable in production for security
7. **Monitor metrics and set up alerts** - Use the automatic metrics to track RPC performance
8. **Implement custom health checkers for dependencies** - Check database, cache, and other service health
9. **Add custom interceptors for cross-cutting concerns** - Authentication, authorization, rate limiting, etc.
10. **Use context for cancellation and deadlines** - Always pass context through your RPC calls

## Architecture

The gRPC extension follows Forge's extension pattern:

1. **Configuration**: Loads config from `ConfigManager` with dual-key support
2. **DI Registration**: Registers the gRPC server as `"grpc"` in the container
3. **Lifecycle**: Implements `Register()`, `Start()`, `Stop()`, `Health()` methods
4. **Observability**: Integrates with Forge's logging and metrics systems
5. **Interceptors**: Chains observability and custom interceptors
6. **TLS**: Loads certificates and configures mTLS if enabled
7. **Health Checking**: Implements standard gRPC health protocol

## Performance

The gRPC extension is designed for high-performance production use:

- **Zero-copy streaming** where possible
- **Connection pooling** with configurable limits
- **Concurrent stream handling** with `MaxConcurrentStreams`
- **Message size limits** to prevent memory exhaustion
- **Keepalive configuration** to detect and close dead connections
- **Efficient observability** with minimal overhead

## Troubleshooting

### Connection Refused

Ensure the server is started before registering services:

```go
app.Start(ctx)  // Start first
grpcServer, _ := forge.Resolve[grpc.GRPC](app.Container(), "grpc")
pb.RegisterYourServiceServer(grpcServer.GetServer(), &impl{})  // Register after
```

### TLS Certificate Errors

Verify certificate paths and permissions:

```bash
ls -la server.crt server.key ca.crt
openssl verify -CAfile ca.crt server.crt
```

### Health Check Failures

Check if health check is enabled and custom health checkers are registered:

```go
grpc.WithHealthCheck(true)
grpcServer.RegisterHealthChecker("my-service", &checker{})
```

### High Memory Usage

Reduce message size limits and concurrent streams:

```go
grpc.WithMaxRecvMsgSize(1 * 1024 * 1024)  // 1MB
grpc.WithMaxConcurrentStreams(50)
```

## Examples

See the `v2/examples/` directory for complete examples:

- `grpc-basic/` - Simple gRPC service
- `grpc-advanced/` - TLS, interceptors, health checks

## License

MIT



