<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge gRPC, a gRPC server with health checking, server reflection and TLS that runs inside a Go application built on Forge" width="100%">
</p>

# Forge gRPC: a gRPC server for Go apps built on Forge

Forge gRPC is the gRPC extension for [Forge](https://github.com/xraph/forge), the Go backend framework. It runs a [grpc-go](https://github.com/grpc/grpc-go) server next to your HTTP server and ties it to the app's lifecycle, with the standard gRPC health checking protocol, server reflection for tools like grpcurl, TLS and mutual TLS, keepalive and message size limits, request logging, OpenTelemetry spans and metrics, and graceful shutdown. You hand it your generated services with `WithService` or `WithServiceRegistrar`, and it registers them before the server starts.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/grpc.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/grpc) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/grpc) · [Quick start](#quick-start) · [Registering services](#registering-your-own-services) · [Features](#features) · [Health checks](#health-checks) · [TLS](#tls-and-mtls) · [Tracing and metrics](#logging-tracing-and-metrics) · [Clients](#tracing-outbound-calls) · [Configuration](#configuration) · [FAQ](#faq)

## Quick start

You need [Go 1.26 or later](https://go.dev/dl/). Add Forge and the extension to your module:

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/grpc@latest
```

Save this as `main.go`. It starts a gRPC server on port 50051 with health checks and reflection, and adds a health checker for one dependency:

```go
package main

import (
	"context"
	"log"

	"github.com/xraph/forge"
	grpcext "github.com/xraph/forge/extensions/grpc"
)

type dbCheck struct{}

func (dbCheck) Check(ctx context.Context) error {
	return nil // ping your database here
}

func main() {
	app := forge.NewApp(forge.AppConfig{
		Name:        "billing",
		HTTPAddress: ":8080",
		Extensions: []forge.Extension{
			grpcext.NewExtension(
				grpcext.WithAddress(":50051"),
				grpcext.WithReflection(true),
				grpcext.WithHealthCheck(true),
			),
		},
	})

	err := app.RegisterHookFn(forge.PhaseAfterStart, "grpc-health", func(ctx context.Context, a forge.App) error {
		srv, err := forge.Inject[grpcext.GRPC](a.Container())
		if err != nil {
			return err
		}

		srv.RegisterHealthChecker("billing.db", dbCheck{})

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

The package is named `grpc`, so the example imports it as `grpcext` to keep it apart from `google.golang.org/grpc`. Run it with `go mod tidy && go run .`, then talk to it with [grpcurl](https://github.com/fullstorydev/grpcurl):

```bash
grpcurl -plaintext localhost:50051 list
# grpc.health.v1.Health
# grpc.reflection.v1.ServerReflection
# grpc.reflection.v1alpha.ServerReflection

grpcurl -plaintext -d '{"service":"billing.db"}' localhost:50051 grpc.health.v1.Health/Check
# { "status": "SERVING" }
```

When you stop the app, the server drains with `GracefulStop` and falls back to a hard stop if that fails.

## Registering your own services

Pass your services to the extension when you create it. `WithService` takes the generated descriptor and your implementation:

```go
grpcext.NewExtension(
	grpcext.WithAddress(":50051"),
	grpcext.WithService(&pb.Greeter_ServiceDesc, &greeter{}),
)
```

If the implementation needs something from the container, or you want an interceptor or a health checker next to it, use `WithServiceRegistrar`. Your function gets the app and the server, and the extension calls it once, while it builds the server inside `app.Start`:

```go
grpcext.WithServiceRegistrar(func(app forge.App, s grpcext.GRPC) error {
	db, err := forge.Inject[*sql.DB](app.Container())
	if err != nil {
		return err
	}

	s.AddUnaryInterceptor(authInterceptor)
	s.RegisterHealthChecker("greeter", greeterCheck{db: db})

	return s.RegisterService(&pb.Greeter_ServiceDesc, &greeter{db: db})
}),
```

A registrar that returns an error fails `app.Start` with that error wrapped. So does a service the server can't take, which comes back as `ErrInvalidService`: an implementation that doesn't satisfy the descriptor's server interface, or a service name that's already registered. The health and reflection services count when they're on. grpc-go would end the process for either one at `Start`, so the extension checks them when you register.

The registrar runs inside the constructor of the gRPC service, so it can resolve anything you provided on the container before `app.Start` and anything an earlier extension registered. Resolving `grpcext.GRPC` or `*grpcext.GRPCService` from inside it is a circular dependency. `WithConfig` replaces the whole config, registrars included, so put `WithService` and `WithServiceRegistrar` after it.

You can't do this from a lifecycle hook. grpc-go can't add a service to a server that's already serving, and the extension's server starts inside `app.Start`, right after the extension registers. Resolving it from the container starts it too, so even a `PhaseAfterRegister` hook finds it running. Here's what each call does on either side of `Start`:

| What | Before `Start` | After `Start` |
| --- | --- | --- |
| `RegisterService` | Buffered and served | Returns `ErrRegisterAfterStart` |
| `AddUnaryInterceptor`, `AddStreamInterceptor` | Chained onto the server | Stored, never called |
| `RegisterHealthChecker` | Works | Works, checkers are looked up on each call |
| `GetServer` | `nil` | The `grpc.Server` |

Don't call a generated `pb.RegisterXxxServer` on `GetServer()`. It's `nil` until `Start`, and on a serving server grpc-go stops the process with `grpc: Server.RegisterService after Server.Serve`. Use `RegisterService(&pb.Xxx_ServiceDesc, impl)` instead.

If you'd like to own the server's start and stop yourself, `grpcext.NewGRPCServer(cfg, app.Logger(), app.Metrics())` gives you one outside the extension, and you call `Start` and `GracefulStop` on it. Create it after `forge.NewApp`, because it reads the OpenTelemetry globals `NewApp` installs when it starts.

The [`grpc-advanced`](examples/grpc-advanced/) example registers a greeter, an auth interceptor and a health checker through `WithServiceRegistrar`.

## Features

| You need | Forge gRPC gives you |
| --- | --- |
| A gRPC server that starts and stops with the app | A `grpc.Server` built from config when the extension starts, stopped gracefully when the app stops |
| Kubernetes and load balancer probes | The `grpc.health.v1.Health` service, with your own per-service checkers |
| grpcurl, Postman and other dynamic clients | Server reflection, on by default |
| Encrypted traffic | TLS from a certificate and key, and mutual TLS with a client CA |
| Limits | Receive and send size limits (4MB each by default), max concurrent streams, connection timeout and keepalive enforcement |
| Visibility | Debug logs per call, error logs on failure, an OpenTelemetry span per call, and the `rpc.server.call.duration` histogram |
| Calling other gRPC services | `ClientDialOptions()`, dial options that trace outbound calls |
| A quick look at the server | `GetStats` counters and `GetServices` with each service's methods |

## Health checks

With `enable_health_check` on (the default), the server answers the [gRPC health checking protocol](https://github.com/grpc/grpc/blob/master/doc/health-checking.md). A checker is anything with `Check(ctx context.Context) error`, registered under a service name with `RegisterHealthChecker`. You can add one at any time, since checkers are looked up on each call.

| Request | Answer |
| --- | --- |
| `service` empty | `SERVING` |
| A service whose checker returns `nil` | `SERVING` |
| A service whose checker returns an error | `NOT_SERVING` |
| A service with no checker registered | `SERVING` |

`Watch` sends one `SERVING` message and returns, and `List` returns an empty list. Neither reflects your checkers yet.

The extension's own `Health` method only reports whether the extension has started. It doesn't call your checkers.

## TLS and mTLS

```go
grpcext.NewExtension(
	grpcext.WithTLS("server.crt", "server.key", ""),
)

// Mutual TLS: require and verify client certificates against ca.crt.
grpcext.NewExtension(
	grpcext.WithTLS("server.crt", "server.key", "ca.crt"),
	grpcext.WithClientAuth(true),
)
```

Config validation fails at startup if TLS is on without a certificate and key, or client auth is on without a CA file. Check your logs after you turn TLS on, though. If the files exist in config but can't be loaded (a wrong path, a bad PEM), the extension logs `failed to load TLS credentials` and starts the server without TLS, in plaintext.

## Logging, tracing and metrics

`enable_logging`, `enable_metrics` and `enable_tracing` are all on by default. Logging writes a debug line when a call starts and ends, and an error line with the method, duration and error when it fails.

### Spans

With `enable_tracing` on, every served call gets a server span named after the method (`billing.Invoices/Get`), made by [otelgrpc](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc). It carries the OpenTelemetry semconv v1.43 attributes:

| Attribute | Example |
| --- | --- |
| `rpc.system.name` | `grpc` |
| `rpc.method` | `billing.Invoices/Get` |
| `rpc.response.status_code` | `OK`, `NOT_FOUND`, `DEADLINE_EXCEEDED` |

A `traceparent` in the incoming metadata makes the span a child of the caller's trace. Server-side error codes such as `INTERNAL` mark the span as an error. Client-error codes such as `NOT_FOUND` don't. Health and reflection calls get no span, the same way Forge's HTTP tracing skips `/_/`.

The call's context also carries `trace_id` and `span_id`, as it does for HTTP requests, so a logger built from it stamps both on every line. The server uses the OpenTelemetry globals that `forge.New` installs, and reads them when it starts. Your app's tracing config (endpoint, sampling) is what sends the spans anywhere, see [observability](../../docs/content/docs/forge/observability.mdx).

If you already added your own otelgrpc server handler, remove it. You'd get two spans per call.

Leave `OTEL_SEMCONV_STABILITY_OPT_IN` unset. If it holds `rpc/old` or `rpc/dup`, otelgrpc puts the older attributes on the span (`rpc.service`, a short `rpc.method` and a numeric status code), while `rpc_server_call_duration_seconds` keeps the v1.43 labels. Spans and metrics stop lining up.

### Metrics

Every served call lands in one histogram, `rpc.server.call.duration`, recorded through the app's Forge metrics registry. Prometheus exports it as `rpc_server_call_duration_seconds`:

| Label | Value |
| --- | --- |
| `rpc_system_name` | `grpc` |
| `rpc_method` | `billing.Invoices/Get` |
| `rpc_response_status_code` | `OK`, `NOT_FOUND`, and so on |

The buckets run from 5ms to 10s. Health and reflection calls aren't recorded. A stream records the whole call, from open to close. A handler that returns a bare `context.Canceled` or `context.DeadlineExceeded` is recorded as `CANCELLED` or `DEADLINE_EXCEEDED`, which is what the client sees. With `enable_metrics` off nothing is recorded.

Two cases differ from the span. A call to a service or method the server doesn't have gets a span with `UNIMPLEMENTED` but no observation, because grpc-go rejects it before any interceptor runs. And a unary call whose handler succeeds but whose response fails to send is recorded as `OK`, while the wire and the span say otherwise.

The labels are the same keys and values that sit on the span, so a slow bucket and its trace line up. Two queries to start with:

```promql
# Calls per second, by method
sum by (rpc_method) (rate(rpc_server_call_duration_seconds_count[5m]))

# Failures per second, by method
sum by (rpc_method) (rate(rpc_server_call_duration_seconds_count{rpc_response_status_code!="OK"}[5m]))
```

The older `forge_grpc_*` series are gone. Nothing is emitted under the old names, so dashboards and alerts that use them go blank until you move them:

| Old series | Now |
| --- | --- |
| `forge_grpc_unary_started_total`, `_succeeded_total`, `_failed_total` | `rpc_server_call_duration_seconds_count`, split by `rpc_response_status_code` |
| `forge_grpc_unary_duration_seconds` | `rpc_server_call_duration_seconds` |
| `forge_grpc_stream_succeeded_total`, `_failed_total` | `rpc_server_call_duration_seconds_count`, split by `rpc_response_status_code` |
| `forge_grpc_stream_duration_seconds` | `rpc_server_call_duration_seconds` |
| `forge_grpc_stream_active` | No replacement. `GetStats().ActiveStreams` still has it |

Two label changes bite if you relabeled. The status code is the canonical name (`NOT_FOUND`), not the Go spelling (`NotFound`) the old `code` label held, and `rpc_method` has no leading slash, where the old `method` label had one.

`GetStats()` is unchanged. It returns the start time, RPC counts and active streams, and `GetServices()` lists registered services and their methods, with flags for client and server streaming.

### Tracing outbound calls

`ClientDialOptions()` returns dial options that put a client span on every outbound call and send the trace context to the next service:

```go
conn, err := grpc.NewClient("billing:50051",
	append(forgegrpc.ClientDialOptions(), grpc.WithTransportCredentials(creds))...)
```

Import the extension as `forgegrpc "github.com/xraph/forge/extensions/grpc"` here, to keep it apart from grpc-go. If you use [conduit](../conduit/), `Services.GRPC` adds this handler for you, so don't pass `ClientDialOptions()` into it. gRPC appends stats handlers rather than replacing them, and a second one gives every call a second client span. Both read the OpenTelemetry globals when you call them, so call them after `forge.New`.

## Configuration

The extension reads `extensions.grpc` from your config, falling back to `grpc`. Options you pass in code override the file.

```yaml
extensions:
  grpc:
    address: ":50051"
    max_recv_msg_size: 4194304
    max_send_msg_size: 4194304
    max_concurrent_streams: 0     # 0 means no limit
    connection_timeout: 120s
    keepalive:
      time: 2h
      timeout: 20s
      enforcement_policy: true
      min_time: 5m
      permit_without_stream: false
    enable_tls: false
    tls_cert_file: ""
    tls_key_file: ""
    tls_ca_file: ""
    client_auth: false
    enable_health_check: true
    enable_reflection: true
    enable_metrics: true
    enable_tracing: true
    enable_logging: true
```

| Key | Option | Default | What it does |
| --- | --- | --- | --- |
| `address` | `WithAddress` | `:50051` | Listen address |
| `max_recv_msg_size`, `max_send_msg_size` | `WithMaxMessageSize` (sets both) | 4MB | Message size limits |
| `max_concurrent_streams` | `WithMaxConcurrentStreams` | `0` | Streams per connection, `0` for no limit |
| `connection_timeout` | none | `120s` | Time allowed for a new connection's handshake |
| `keepalive.time`, `keepalive.timeout` | none | `2h`, `20s` | Ping interval and ack timeout |
| `keepalive.enforcement_policy`, `min_time`, `permit_without_stream` | none | `true`, `5m`, `false` | What the server accepts from client pings |
| `enable_tls`, `tls_cert_file`, `tls_key_file`, `tls_ca_file` | `WithTLS` | off | Server TLS |
| `client_auth` | `WithClientAuth` | `false` | Require and verify client certificates |
| `enable_health_check` | `WithHealthCheck` | `true` | Registers the health service |
| `enable_reflection` | `WithReflection` | `true` | Registers server reflection |
| `enable_metrics` | `WithMetrics` | `true` | The `rpc.server.call.duration` histogram and the `GetStats` counters |
| `enable_tracing` | `WithTracing` | `true` | A server span per call, and trace IDs on the log context |
| `enable_logging` | none | `true` | Logging interceptors |

`keepalive.time` also sets the server's maximum idle time for a connection, and twice that value is the maximum connection age, with `keepalive.timeout` as the grace period. With the defaults, an idle connection closes after two hours and any connection is recycled after four.

`WithConfig` replaces the whole config and `WithRequireConfig(true)` makes a missing config section an error at startup. `WithService` and `WithServiceRegistrar` have no key: a registrar is code, and the file can't set or remove one. The container holds `*grpcext.GRPCService` (also under the name `grpc`) and the `grpcext.GRPC` interface, backed by the same server.

## Documentation

| Topic | Where |
| --- | --- |
| What the extension does | [Features](https://www.xraph.com/docs/forge/extensions/grpc/features) |
| Every config key | [Configuration](https://www.xraph.com/docs/forge/extensions/grpc/configuration) |
| Exported functions and types | [Functions](https://www.xraph.com/docs/forge/extensions/grpc/functions), [pkg.go.dev](https://pkg.go.dev/github.com/xraph/forge/extensions/grpc) |
| grpc-go | [gRPC Go documentation](https://grpc.io/docs/languages/go/) |
| Extensions in general | [Forge extension system](../../docs/content/docs/forge/extensions-system.mdx) |

## FAQ

### How do I add a gRPC server to a Go app built on Forge?

Add `grpcext.NewExtension()` to your app's `Extensions` and it listens on `:50051` with health checks and reflection on. Add your services with `grpcext.WithService(&pb.Xxx_ServiceDesc, impl)`, as [registering your own services](#registering-your-own-services) shows.

### Does Forge gRPC support the gRPC health checking protocol?

Yes. It registers `grpc.health.v1.Health`, so Kubernetes gRPC probes and `grpc_health_probe` work against it. Register a checker per service name to report `NOT_SERVING` when a dependency is down.

### How do I enable gRPC server reflection for grpcurl?

It's on by default. Turn it off with `WithReflection(false)` or `enable_reflection: false`.

### Can I use mutual TLS?

Yes. Pass the CA file as the third argument to `WithTLS` and add `WithClientAuth(true)`, and the server requires a client certificate signed by that CA.

## Contributing

The extension is its own Go module inside the Forge repository. Test it from its directory:

```bash
cd extensions/grpc
GOWORK=off go test -race ./...
GOWORK=off golangci-lint run ./...
```

[AGENTS.md](../../AGENTS.md) has the full build, lint and release rules for the repository, and [CONTRIBUTING.md](../../CONTRIBUTING.md) has the contribution guidelines.

## License

Forge gRPC is part of Forge and is available under the [Apache License 2.0](../../LICENSE).
