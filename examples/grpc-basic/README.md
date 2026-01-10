# gRPC Basic Example

Simple gRPC service with Forge demonstrating the HelloWorld service.

## Features

- Basic gRPC server with HelloWorld service
- Health check enabled
- Reflection enabled (for grpcurl)
- Metrics and logging enabled
- Graceful shutdown

## Prerequisites

Install `grpcurl` for testing:

```bash
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

## Run

```bash
cd v2/examples/grpc-basic
go run main.go
```

You should see:

```
✓ gRPC server listening on :50051
✓ Health check enabled
✓ Reflection enabled

Test with:
  grpcurl -plaintext localhost:50051 helloworld.Greeter/SayHello -d '{"name":"World"}'
  grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
  grpcurl -plaintext localhost:50051 list

Press Ctrl+C to stop...
```

## Test

### Call the SayHello method

```bash
grpcurl -plaintext localhost:50051 helloworld.Greeter/SayHello -d '{"name":"World"}'
```

Expected output:

```json
{
  "message": "Hello World"
}
```

### Check service health

```bash
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

Expected output:

```json
{
  "status": "SERVING"
}
```

### List available services

```bash
grpcurl -plaintext localhost:50051 list
```

Expected output:

```
grpc.health.v1.Health
grpc.reflection.v1alpha.ServerReflection
helloworld.Greeter
```

### Describe a service

```bash
grpcurl -plaintext localhost:50051 describe helloworld.Greeter
```

### Get service methods

```bash
grpcurl -plaintext localhost:50051 list helloworld.Greeter
```

## Code Walkthrough

1. **Create Forge app** with name and version
2. **Register gRPC extension** with options (reflection, health, metrics)
3. **Start the app** to initialize all extensions
4. **Resolve gRPC server** from DI container
5. **Register your service** using the gRPC server
6. **Wait for shutdown signal** and cleanup gracefully

## Next Steps

- Add authentication with custom interceptors
- Enable TLS (see grpc-advanced example)
- Add custom health checkers
- Implement streaming RPCs
- Add rate limiting

## See Also

- [gRPC Extension README](../../extensions/grpc/README.md)
- [gRPC Advanced Example](../grpc-advanced/README.md)



