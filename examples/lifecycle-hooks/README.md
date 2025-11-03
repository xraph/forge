# Lifecycle Hooks Example

This example demonstrates how to use Forge's lifecycle hooks to manage external applications and background workers.

## Overview

This example shows:
- Starting external processes when Forge runs
- Managing background workers
- Graceful shutdown of external resources
- Hook execution order and priority
- Error handling in hooks

## Running the Example

```bash
# From the forge root directory
cd examples/lifecycle-hooks
go run main.go
```

The application will:
1. Validate the environment (Phase: BeforeStart)
2. Configure services (Phase: AfterRegister)
3. Run post-initialization tasks (Phase: AfterStart)
4. Start two external apps (Phase: BeforeRun)
5. Start the HTTP server on `:8080`
6. Start a background worker (Phase: AfterRun)
7. Log startup complete

## Testing

Once running, test the endpoints:

```bash
# Check app status
curl http://localhost:8080/

# Check health
curl http://localhost:8080/_/health

# Check app info
curl http://localhost:8080/_/info
```

## Shutdown

Press `Ctrl+C` to trigger graceful shutdown. You'll see:
1. Shutdown signal received
2. HTTP server stops accepting new requests
3. Background worker stops (Phase: BeforeStop)
4. External apps stop gracefully (Phase: BeforeStop)
5. Final cleanup (Phase: AfterStop)

## Lifecycle Phases Demonstrated

| Phase | What Happens |
|-------|--------------|
| `PhaseBeforeStart` | Environment validation |
| `PhaseAfterRegister` | Service configuration |
| `PhaseAfterStart` | Post-initialization logging |
| `PhaseBeforeRun` | External apps start (priority ordered) |
| `PhaseAfterRun` | Background worker starts |
| `PhaseBeforeStop` | Resources stop gracefully |
| `PhaseAfterStop` | Final logging |

## Key Concepts

### Hook Priority

External apps are started with different priorities to control order:

```go
opts1.Priority = 100  // Runs first
opts2.Priority = 90   // Runs second
```

### Error Handling

Some hooks continue on error, while others stop execution:

```go
notifyOpts.ContinueOnError = true  // Don't fail if notification fails
```

### Resource Management

External apps are started in `PhaseBeforeRun` and stopped in `PhaseBeforeStop`:

```go
// Start
app.RegisterHook(forge.PhaseBeforeRun, func(ctx context.Context, app forge.App) error {
    return externalApp.Start(ctx)
}, opts)

// Stop
app.RegisterHookFn(forge.PhaseBeforeStop, "stop-app", func(ctx context.Context, app forge.App) error {
    return externalApp.Stop(ctx)
})
```

## Real-World Use Cases

This pattern is useful for:
- Starting sidecar containers
- Managing background workers
- Initializing external services
- Running database migrations
- Cache warming
- Service discovery registration
- Health check initialization
- Monitoring agent startup

## Customization

You can modify this example to:
- Start your own external services
- Add more background workers
- Implement custom health checks
- Add service discovery integration
- Implement crash recovery and restarts
- Add more sophisticated process monitoring

## See Also

- [Lifecycle Hooks Documentation](../../docs/lifecycle-hooks.md)
- [Extensions Guide](../../docs/extensions.md)
- [Dependency Injection](../../docs/dependency-injection.md)

