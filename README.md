# Forge

A Go framework for backend services, with dependency injection, an extension system, and observability built in.

Forge™ is a backend framework, and Forge Cloud™ is its AI cloud offering, maintained by XRAPH™.

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![Go Report Card](https://goreportcard.com/badge/github.com/xraph/forge)](https://goreportcard.com/report/github.com/xraph/forge)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![GitHub Stars](https://img.shields.io/github/stars/xraph/forge)](https://github.com/xraph/forge)
[![CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml)

## Quick start

```bash
go install github.com/xraph/forge/cmd/forge@latest
forge --version
```

```bash
forge init my-app
forge dev
```

A minimal service:

```go
package main

import "github.com/xraph/forge"

func main() {
    app := forge.NewApp(forge.AppConfig{
        Name:        "my-app",
        Version:     "1.0.0",
        Environment: "development",
        HTTPAddress: ":8080",
    })

    router := app.Router()
    router.GET("/", func(ctx forge.Context) error {
        return ctx.JSON(200, map[string]string{
            "message": "Hello, Forge!",
        })
    })

    // Blocks until SIGINT or SIGTERM.
    app.Run()
}
```

Every app serves three endpoints without configuration: `/_/info` for application
metadata, `/_/metrics` for Prometheus, and `/_/health` for health checks.

## What you get

The core framework handles the parts most services need before they can do
anything interesting:

- A type-safe dependency injection container with service lifecycles
- An HTTP router with trie-based path matching and middleware support
- Middleware for auth, CORS, logging and rate limiting
- Configuration from YAML, JSON or TOML, overridable by environment variables
- Structured logging, Prometheus metrics and distributed tracing
- Health checks that discover and report themselves
- Graceful startup and shutdown, so SIGTERM cleans up rather than drops work

The CLI scaffolds projects, generates handlers and services, runs migrations,
and serves your app with hot reload. See [cli/README.md](cli/README.md) and the
[commands reference](cmd/forge/COMMANDS.md).

## Extensions

Extensions are modules you compose into an app. Most are production ready; three
are still being built.

| Extension | What it does |
|---|---|
| [auth](extensions/auth/README.md) | Multi-provider authentication (OAuth, JWT, SAML) |
| cache | Multi-backend caching (Redis, Memcached, in-memory) |
| [consensus](extensions/consensus/README.md) | Raft consensus for distributed systems |
| [cron](extensions/cron/README.md) | Distributed cron scheduling with execution history |
| [dashboard](extensions/dashboard/README.md) | Micro-frontend shell for admin dashboards |
| database | SQL (Postgres, MySQL, SQLite) and MongoDB |
| [discovery](extensions/discovery/README.md) | Service discovery and registry |
| events | Event bus and event sourcing |
| [features](extensions/features/README.md) | Feature flags and A/B testing |
| [graphql](extensions/graphql/README.md) | GraphQL server with schema generation |
| [grpc](extensions/grpc/README.md) | gRPC server with reflection |
| [hls](extensions/hls/README.md) | HTTP Live Streaming |
| [kafka](extensions/kafka/README.md) | Apache Kafka integration |
| [mcp](extensions/mcp/README.md) | Model Context Protocol |
| [mqtt](extensions/mqtt/README.md) | MQTT broker and client |
| [security](extensions/security/README.md) | Security hardening for production apps |
| [storage](extensions/storage/README.md) | Object storage (S3, GCS, local) |
| [streaming](extensions/streaming/README.md) | WebSocket and SSE |
| [webrtc](extensions/webrtc/README.md) | Peer-to-peer real-time communication |
| [orpc](extensions/orpc/README.md) | ORPC transport protocol (in progress) |
| [queue](extensions/queue/README.md) | Message queue management (in progress) |
| search | Full-text search, Elasticsearch and Typesense (in progress) |

The [complete catalog](docs/content/docs/extensions/complete-catalog.mdx) covers
configuration for each one.

## Composing an application

Extensions are declared in the app config. Services register against the
container, and handlers resolve them from it:

```go
app := forge.NewApp(forge.AppConfig{
    Name:        "my-service",
    Version:     "1.0.0",
    Environment: "production",

    Extensions: []forge.Extension{
        database.NewExtension(database.Config{
            Databases: []database.DatabaseConfig{
                {
                    Name: "primary",
                    Type: database.TypePostgres,
                    DSN:  "postgres://localhost/mydb",
                },
            },
        }),

        auth.NewExtension(auth.Config{
            Provider: "oauth2",
        }),
    },
})

forge.RegisterSingleton(app.Container(), "userService", func(c forge.Container) (*UserService, error) {
    db, err := database.GetSQL(c)
    if err != nil {
        return nil, err
    }
    logger := forge.Must[forge.Logger](c, "logger")
    return NewUserService(db, logger), nil
})

router := app.Router()
router.GET("/users/:id", getUserHandler)
router.POST("/users", createUserHandler)

app.Run()
```

Switching a backend is a config change rather than a code change: the same
`database.GetSQL(c)` call works whether it resolves to Postgres or SQLite.

## Documentation

- [Installation](docs/content/docs/forge/installation.mdx)
- [Quick start](docs/content/docs/forge/quick-start.mdx)
- [Architecture](docs/content/docs/forge/architecture.mdx)
- [Application lifecycle](docs/content/docs/forge/lifecycle.mdx)
- [Dependency injection](docs/content/docs/forge/dependency-injection.mdx)
- [Routing](docs/content/docs/forge/%28router%29/router.mdx) and [middleware](docs/content/docs/forge/%28router%29/middleware.mdx)
- [Configuration](docs/content/docs/forge/configuration.mdx)
- [Observability](docs/content/docs/forge/observability.mdx)

Full docs are at [forge.dev](https://forge.dev). Questions and ideas go in
[Discussions](https://github.com/xraph/forge/discussions); bugs go in
[Issues](https://github.com/xraph/forge/issues).

## Examples

The [examples](examples/) directory has runnable services. Some worth starting
with:

- [di-patterns](examples/di-patterns/) for container registration
- [lifecycle-hooks](examples/lifecycle-hooks/) for startup and shutdown ordering
- [observability](examples/observability/) for metrics, logging and tracing
- [simple-extension](examples/simple-extension/) and [runnable-extension](examples/runnable-extension/) for writing your own
- [openapi-demo](examples/openapi-demo/) for generated API specs
- [sse-streaming](examples/sse-streaming/) and [webtransport](examples/webtransport/) for streaming transports
- [auth](extensions/auth/examples/auth_example/) and [graphql](extensions/graphql/examples/graphql-basic/) for those extensions

## Development

You need Go 1.24 or later. Make is optional but the targets below assume it.

```bash
make build          # build the CLI
make build-debug    # build with debug symbols
make release        # build for all platforms
```

```bash
make test           # all tests
make test-coverage  # with coverage
go test ./extensions/graphql/...
```

```bash
make fmt            # format
make lint           # lint
make lint-fix       # lint and fix
make security-scan  # security scan
make vuln-check     # check dependencies for known vulnerabilities
make ci             # everything CI runs
```

The dev server takes `--watch` for hot reload and `--port` to override the
address:

```bash
forge dev --watch --port 3000
```

## Contributing

Fork, branch, and open a pull request. Run `make install-tools` once, then
`make ci` before you push.

Commits follow [Conventional Commits](https://www.conventionalcommits.org/),
which the release tooling reads to decide the version bump. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the rest.

## Releases

Releases run through [Release Please](https://github.com/googleapis/release-please)
and a GitHub Actions workflow.

Push to `main` with conventional commits and Release Please opens a PR carrying
the version bumps and changelog. Merging that PR creates a tag, and the tag
triggers the release pipeline. For a release you need to cut by hand, go to
[Actions > Release](../../actions/workflows/release.yml) and run the workflow
against a chosen module and version.

The pipeline builds cross-platform binaries and Docker images for the main
module and CLI and publishes them to Homebrew, Scoop and NFPM through
GoReleaser. Extension modules get a GitHub release and a notification to the Go
module proxy. Dry-run mode validates the whole pipeline without publishing, and
tests can be skipped for a hotfix that CI has already verified.

## License

Apache License 2.0. See [LICENSE](LICENSE).

## Acknowledgments

Built by [Rex Raphael](https://github.com/juicycleff), with thanks to
[Bun](https://github.com/uptrace/bun) for the SQL ORM,
[Uptrace](https://github.com/uptrace/uptrace) for observability, and
[Chi](https://github.com/go-chi/chi), whose router shaped the design of this one.
