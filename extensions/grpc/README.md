<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge gRPC, a gRPC server with health checking, server reflection and TLS that runs inside a Go application built on Forge" width="100%">
</p>

# Forge gRPC: a gRPC server for Go apps built on Forge

Forge gRPC is the gRPC extension for [Forge](https://github.com/xraph/forge), the Go backend framework. It runs a [grpc-go](https://github.com/grpc/grpc-go) server next to your HTTP server and ties it to the app's lifecycle, with the standard gRPC health checking protocol, server reflection for tools like grpcurl, TLS and mutual TLS, keepalive and message size limits, request logging and metrics, and graceful shutdown. Read the [known issue](#known-issue-registering-your-own-services) before you plan around it: registering your own service on the managed server doesn't work reliably yet.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/grpc.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/grpc) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/grpc) · [Quick start](#quick-start) · [Known issue](#known-issue-registering-your-own-services) · [Features](#features) · [Health checks](#health-checks) · [TLS](#tls-and-mtls) · [Metrics](#logging-and-metrics) · [Configuration](#configuration) · [FAQ](#faq)

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

## Known issue: registering your own services

This is the main gap today. There's no dependable point at which you can register a generated service (`pb.RegisterUserServiceServer`) on the server this extension manages:

| When you register | What happens |
| --- | --- |
| Before the extension starts | You can't reach the server yet, because it's only in the container once the extension registers, and resolving it starts it. If you call `RegisterService` on a server built with `grpcext.NewGRPCServer` before `Start`, `Start` builds a new `grpc.Server` and your registration is dropped |
| After the extension starts | The server is already serving, and grpc-go stops the process with `grpc: Server.RegisterService after Server.Serve` |

`AddUnaryInterceptor` and `AddStreamInterceptor` have the same timing problem. Interceptors are chained when the server is built, so one you add once it's running is stored and never called.

Until that's fixed, keep your application services on a `grpc.Server` you create and start yourself (in a Forge `RunnableExtension` or a lifecycle hook), and use this extension for the parts that work today. The [`grpc-advanced`](examples/grpc-advanced/) example registers its service after `app.Start` and hits the fatal error above.

## Features

| You need | Forge gRPC gives you |
| --- | --- |
| A gRPC server that starts and stops with the app | A `grpc.Server` built from config when the extension starts, stopped gracefully when the app stops |
| Kubernetes and load balancer probes | The `grpc.health.v1.Health` service, with your own per-service checkers |
| grpcurl, Postman and other dynamic clients | Server reflection, on by default |
| Encrypted traffic | TLS from a certificate and key, and mutual TLS with a client CA |
| Limits | Receive and send size limits (4MB each by default), max concurrent streams, connection timeout and keepalive enforcement |
| Visibility | Debug logs per call, error logs on failure, and Forge metrics for unary calls and streams |
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

## Logging and metrics

`enable_logging`, `enable_metrics` and `enable_tracing` each add an interceptor to every unary and streaming call. Logging writes a debug line when a call starts and ends, and an error line with the method, duration and error when it fails. Metrics go to the app's Forge metrics:

| Metric | Type | Labels |
| --- | --- | --- |
| `grpc_unary_started_total` | counter | `method` |
| `grpc_unary_succeeded_total` | counter | `method` |
| `grpc_unary_failed_total` | counter | `method`, `code` |
| `grpc_unary_duration_seconds` | histogram | `method` |
| `grpc_stream_active` | gauge | `method` |
| `grpc_stream_succeeded_total`, `grpc_stream_failed_total` | counter | `method` |
| `grpc_stream_duration_seconds` | histogram | `method` |

The tracing interceptor is a placeholder that writes a debug log line. It doesn't create spans yet.

`GetStats()` returns the start time, RPC counts and active streams that the metrics interceptor keeps. `GetServices()` lists registered services and their methods, with flags for client and server streaming.

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
| `enable_metrics` | `WithMetrics` | `true` | Metrics interceptors |
| `enable_tracing` | `WithTracing` | `true` | Placeholder tracing interceptor |
| `enable_logging` | none | `true` | Logging interceptors |

`keepalive.time` also sets the server's maximum idle time for a connection, and twice that value is the maximum connection age, with `keepalive.timeout` as the grace period. With the defaults, an idle connection closes after two hours and any connection is recycled after four.

`WithConfig` replaces the whole config and `WithRequireConfig(true)` makes a missing config section an error at startup. The container holds `*grpcext.GRPCService` (also under the name `grpc`) and the `grpcext.GRPC` interface, backed by the same server.

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

Add `grpcext.NewExtension()` to your app's `Extensions` and it listens on `:50051` with health checks and reflection on. For your own services, see the [known issue](#known-issue-registering-your-own-services) first.

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
