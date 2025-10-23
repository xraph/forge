# gRPC Advanced Example

Advanced gRPC service demonstrating authentication, custom health checkers, interceptors, and statistics.

## Features

- Custom authentication interceptor (API key validation)
- Custom health checker for dependencies
- Server statistics tracking
- Message size limits
- Concurrent stream limits
- Graceful shutdown with stats display
- Ready for TLS/mTLS (commented out)

## Prerequisites

Install `grpcurl` for testing:

```bash
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

## Run

```bash
cd v2/examples/grpc-advanced
go run main.go
```

You should see:

```
✓ gRPC server listening on :50051
✓ Health check enabled with custom checker
✓ Reflection enabled
✓ Authentication interceptor enabled
✓ Metrics and logging enabled
✓ Registered 3 services:
  - helloworld.Greeter (1 methods)
  - grpc.health.v1.Health (3 methods)
  - grpc.reflection.v1alpha.ServerReflection (1 methods)

Test with:
  # Without auth (will fail):
  grpcurl -plaintext localhost:50051 helloworld.Greeter/SayHello -d '{"name":"World"}'

  # With auth (will succeed):
  grpcurl -plaintext -H 'x-api-key: secret-api-key-12345' localhost:50051 helloworld.Greeter/SayHello -d '{"name":"World"}'

  # Health check (no auth required):
  grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check

Press Ctrl+C to stop...
```

## Test Authentication

### Call without API key (should fail)

```bash
grpcurl -plaintext localhost:50051 helloworld.Greeter/SayHello -d '{"name":"World"}'
```

Expected output:

```
ERROR:
  Code: Unauthenticated
  Message: missing API key
```

### Call with valid API key (should succeed)

```bash
grpcurl -plaintext -H 'x-api-key: secret-api-key-12345' localhost:50051 helloworld.Greeter/SayHello -d '{"name":"World"}'
```

Expected output:

```json
{
  "message": "Hello World!"
}
```

### Call with invalid API key (should fail)

```bash
grpcurl -plaintext -H 'x-api-key: wrong-key' localhost:50051 helloworld.Greeter/SayHello -d '{"name":"World"}'
```

Expected output:

```
ERROR:
  Code: Unauthenticated
  Message: invalid API key
```

## Test Health Check

Health checks bypass authentication:

```bash
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

Expected output:

```json
{
  "status": "SERVING"
}
```

## View Statistics

Make some requests and press Ctrl+C to see final statistics:

```
Final Statistics:
  RPCs Started:   10
  RPCs Succeeded: 5
  RPCs Failed:    5
  Active Streams: 0
```

## Enable TLS

Uncomment the TLS lines in `main.go`:

```go
grpc.WithTLS("server.crt", "server.key", ""),
```

Generate certificates:

```bash
# Generate self-signed certificate
openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes
```

Test with TLS:

```bash
grpcurl -insecure localhost:50051 helloworld.Greeter/SayHello -H 'x-api-key: secret-api-key-12345' -d '{"name":"World"}'
```

## Enable mTLS

Uncomment the mTLS lines in `main.go`:

```go
grpc.WithTLS("server.crt", "server.key", "ca.crt"),
grpc.WithClientAuth(true),
```

Generate CA and client certificates:

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 365 -key ca.key -out ca.crt

# Generate server cert
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key -set_serial 01 -out server.crt

# Generate client cert
openssl genrsa -out client.key 4096
openssl req -new -key client.key -out client.csr
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key -set_serial 02 -out client.crt
```

Test with mTLS:

```bash
grpcurl -cert client.crt -key client.key -cacert ca.crt localhost:50051 \
  helloworld.Greeter/SayHello \
  -H 'x-api-key: secret-api-key-12345' \
  -d '{"name":"World"}'
```

## Code Walkthrough

### 1. Custom Interceptor

The `authInterceptor` checks for an API key in metadata:

```go
func authInterceptor(logger forge.Logger) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        md, ok := metadata.FromIncomingContext(ctx)
        // ... validate API key
        return handler(ctx, req)
    }
}
```

### 2. Custom Health Checker

The `customHealthChecker` verifies dependencies:

```go
type customHealthChecker struct {
    logger forge.Logger
}

func (h *customHealthChecker) Check(ctx context.Context) error {
    // Check database, cache, etc.
    return nil
}
```

### 3. Server Statistics

Track RPCs and streams:

```go
stats := grpcServer.GetStats()
fmt.Printf("RPCs Started: %d\n", stats.RPCsStarted)
fmt.Printf("RPCs Succeeded: %d\n", stats.RPCsSucceeded)
fmt.Printf("RPCs Failed: %d\n", stats.RPCsFailed)
```

### 4. Service Discovery

List registered services and methods:

```go
services := grpcServer.GetServices()
for _, svc := range services {
    fmt.Printf("Service: %s\n", svc.Name)
    for _, method := range svc.Methods {
        fmt.Printf("  Method: %s\n", method.Name)
    }
}
```

## Best Practices Demonstrated

1. ✅ Authentication via interceptors
2. ✅ Custom health checkers for dependencies
3. ✅ Message size limits to prevent memory exhaustion
4. ✅ Concurrent stream limits for resource control
5. ✅ Statistics tracking for monitoring
6. ✅ Graceful shutdown with cleanup
7. ✅ Structured logging with context
8. ✅ DI pattern for dependencies
9. ✅ Service discovery via reflection
10. ✅ TLS/mTLS ready configuration

## Next Steps

- Add rate limiting with another interceptor
- Implement streaming RPCs
- Add Prometheus metrics endpoint
- Integrate with distributed tracing
- Add request/response validation
- Implement circuit breakers

## See Also

- [gRPC Extension README](../../extensions/grpc/README.md)
- [gRPC Basic Example](../grpc-basic/README.md)



