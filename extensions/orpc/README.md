<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge oRPC, a JSON-RPC 2.0 and OpenRPC layer that exposes the HTTP routes of a Go application built on Forge" width="100%">
</p>

# Forge oRPC: JSON-RPC 2.0 and OpenRPC for Go HTTP routes

Forge oRPC is the JSON-RPC 2.0 extension for [Forge](https://github.com/xraph/forge), the Go backend framework. You add it to an app and every HTTP route you've registered becomes a JSON-RPC method on one endpoint, with batch requests, a method list and a generated OpenRPC schema. You keep writing ordinary Forge handlers. Clients that prefer RPC call the same code through `POST /rpc`.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/orpc.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/orpc) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/orpc) · [Quick start](#quick-start) · [Features](#features) · [Method names](#method-names) · [Calling methods](#calling-methods) · [OpenRPC schema](#openrpc-schema) · [Configuration](#configuration) · [FAQ](#faq)

## Quick start

You need [Go 1.26 or later](https://go.dev/dl/). Add Forge and the extension to your module:

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/orpc@latest
```

Save this as `main.go`:

```go
package main

import (
	"log"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/orpc"
)

func main() {
	app := forge.NewApp(forge.AppConfig{
		Name:        "users",
		HTTPAddress: ":8080",
		Extensions: []forge.Extension{
			orpc.NewExtension(orpc.WithServerInfo("users", "1.0.0")),
		},
	})

	err := app.Router().GET("/users/:id", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{
			"id":   ctx.Param("id"),
			"name": "Ada",
		})
	},
		forge.WithSummary("Get a user by ID"),
		forge.WithORPCMethod("users.get"),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Run it with `go mod tidy && go run .`, then call the route as a JSON-RPC method:

```bash
curl -X POST http://localhost:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"users.get","params":{"id":"42"},"id":1}'
# {"jsonrpc":"2.0","result":{"id":"42","name":"Ada"},"id":1}

curl http://localhost:8080/rpc/methods
curl http://localhost:8080/rpc/schema
```

The extension reads the router's routes when it starts, so register your routes before `app.Run()` (or `app.Start`). A route you add after that won't show up as a method.

## Features

| You need | Forge oRPC gives you |
| --- | --- |
| An RPC interface without a second set of handlers | Every Forge route, exposed as a JSON-RPC 2.0 method that runs the real handler |
| Control over what gets exposed | Include and exclude path patterns, per-route opt-out, and a method name prefix |
| Predictable method names | `get.users.id` style names from the path, the route name, or one you set with `forge.WithORPCMethod` |
| Fewer round trips | Batch requests, capped by `batch_limit` |
| A machine-readable contract | An OpenRPC 1.3.2 document built from your routes and their OpenAPI request and response schemas |
| Visibility | A method list endpoint, request counters and a method gauge in Forge metrics |

## Endpoints

With the default config the extension adds three routes to your app:

| Route | What it does |
| --- | --- |
| `POST /rpc` | Runs a single JSON-RPC request or a batch (a JSON array) |
| `GET /rpc/methods` | Lists method names, descriptions, tags and deprecation flags |
| `GET /rpc/schema` | Returns the OpenRPC document |

Change the paths with `WithEndpoint` and `WithOpenRPCEndpoint`. The methods route always sits at `<endpoint>/methods`, and `WithDiscovery(false)` or `WithOpenRPC(false)` removes the last two.

## Method names

The default naming strategy, `path`, builds a name from the HTTP verb and the path. `POST` becomes `create`, `GET` becomes `get`, `PUT` becomes `update`, `PATCH` stays `patch` and `DELETE` stays `delete`. Slashes turn into dots, path parameter markers are dropped and hyphens become underscores:

| Route | Method name |
| --- | --- |
| `GET /users/:id` | `get.users.id` |
| `POST /api/v1/posts` | `create.api.v1.posts` |
| `DELETE /order-items/{id}` | `delete.order_items.id` |

Set `naming_strategy: method` to use the route's name (from `forge.WithName`) where it has one, falling back to the path. `WithMethodPrefix("billing.")` puts a prefix on every generated name. A name set with `forge.WithORPCMethod` always wins over the strategy, though the prefix still applies.

These route options shape what the extension does with a route:

| Route option | Effect |
| --- | --- |
| `forge.WithORPCMethod("users.get")` | Sets the method name |
| `forge.WithORPCExclude()` | Leaves the route out |
| `forge.WithORPCParams(&orpc.ParamsSchema{...})` | Replaces the generated params schema |
| `forge.WithORPCResult(&orpc.ResultSchema{...})` | Replaces the generated result schema |
| `forge.WithORPCPrimaryResponse(201)` | Picks which OpenAPI response schema becomes the method's result |
| `forge.WithSummary`, `forge.WithTags` | Become the method's description and tags |

Path patterns come first. `exclude_patterns` defaults to `["/_/*"]`, which keeps Forge's health, info and metrics routes out. If you set `include_patterns`, a route has to match one of them to be exposed. Patterns use `filepath.Match`, and a trailing `/*` matches any path under the prefix. The extension's own routes are always skipped.

## Calling methods

`params` is a JSON object. Path parameters go in as top-level keys, the request body goes under `body`, and query string values go under `query`:

```json
{
  "jsonrpc": "2.0",
  "method": "update.users.id",
  "params": {
    "id": "42",
    "body": {"name": "Ada Lovelace"},
    "query": {"notify": true}
  },
  "id": 7
}
```

The extension turns that into `PUT /users/42?notify=true` with a JSON body, sends it through your router and puts the decoded JSON response in `result`. A handler that answers with a status of 400 or more comes back as a JSON-RPC error with code `-32603` and the message `route returned status <code>`. A non-JSON response comes back as `{"status": ..., "body": ...}`.

Watch for one thing. The router sees a brand new request, and none of the headers on your original `POST /rpc` call (an `Authorization` header, a session cookie, a tracing header) are copied onto it, so if your route reads them to decide who the caller is, it gets nothing back when you call it over JSON-RPC.

A batch is an array of requests, answered with an array of responses in the same order:

```bash
curl -X POST http://localhost:8080/rpc -d '[
  {"jsonrpc":"2.0","method":"users.get","params":{"id":"1"},"id":1},
  {"jsonrpc":"2.0","method":"nope","id":2}
]'
# [{"jsonrpc":"2.0","result":{"id":"1","name":"Ada"},"id":1},
#  {"jsonrpc":"2.0","error":{"code":-32601,"message":"Method 'nope' not found"},"id":2}]
```

JSON-RPC errors are always sent with HTTP 200. The codes are the standard ones, exported as constants:

| Constant | Code | When |
| --- | --- | --- |
| `orpc.ErrParseError` | -32700 | The body isn't valid JSON |
| `orpc.ErrInvalidRequest` | -32600 | Wrong `jsonrpc` version, missing method, or a malformed batch |
| `orpc.ErrMethodNotFound` | -32601 | No method by that name |
| `orpc.ErrInvalidParams` | -32602 | `params` can't be decoded |
| `orpc.ErrInternalError` | -32603 | The route or an interceptor returned an error |
| `orpc.ErrServerError` | -32000 | Request too large, batches disabled, or batch over the limit |

### Register methods and interceptors yourself

The service is in the container as `*orpc.ORPCService`. You can add a method that isn't backed by a route, or wrap every call in an interceptor. A `forge.PhaseAfterStart` hook is a good place for it, since the extension has started by then:

```go
err := app.RegisterHookFn(forge.PhaseAfterStart, "rpc-extras", func(ctx context.Context, a forge.App) error {
	svc, err := forge.Inject[*orpc.ORPCService](a.Container())
	if err != nil {
		return err
	}

	svc.Use(func(ctx context.Context, req *orpc.Request, next orpc.MethodHandler) (any, error) {
		log.Printf("rpc %s", req.Method)
		return next(ctx, req.Params)
	})

	return svc.RegisterMethod(&orpc.Method{
		Name:        "system.ping",
		Description: "Liveness check",
		Handler: func(ctx any, params any) (any, error) {
			return map[string]string{"pong": "ok"}, nil
		},
	})
})
```

An interceptor gets the raw request. Pass `req.Params` (or your own value) to `next`, which hands it to the method's handler.

## OpenRPC schema

`GET /rpc/schema` returns an OpenRPC 1.3.2 document. You set `info.title` and `info.version` with `WithServerInfo`, and `servers` points at your endpoint. For a method's params, the extension uses the first of these you've provided: `forge.WithORPCParams`, the route's OpenAPI request schema, the request type registered for OpenAPI, and finally a basic schema with the path parameters, `query` and (for POST, PUT and PATCH) `body`. Results work the same way, from `forge.WithORPCResult` or the route's OpenAPI response schemas, so if you already describe your responses for OpenAPI you don't have to describe them twice. Methods are sorted by name. You can diff the output between builds.

## Configuration

The extension reads `extensions.orpc` from your config, falling back to `orpc`. Options you pass in code override the file.

```yaml
extensions:
  orpc:
    enabled: true
    endpoint: /rpc
    openrpc_endpoint: /rpc/schema
    server_name: users
    server_version: 1.0.0
    auto_expose_routes: true
    method_prefix: ""
    exclude_patterns: ["/_/*"]
    include_patterns: []
    naming_strategy: path      # path, method or custom
    enable_openrpc: true
    enable_discovery: true
    enable_batch: true
    batch_limit: 10
    max_request_size: 1048576  # bytes, at least 1024
    enable_metrics: true
```

| Key | Option | Default | What it does |
| --- | --- | --- | --- |
| `enabled` | `WithEnabled` | `true` | Turns the extension off without removing it |
| `endpoint` | `WithEndpoint` | `/rpc` | Path of the JSON-RPC endpoint |
| `openrpc_endpoint` | `WithOpenRPCEndpoint` | `/rpc/schema` | Path of the OpenRPC document |
| `server_name`, `server_version` | `WithServerInfo` | `forge-app`, `1.0.0` | `info` block in the OpenRPC document |
| `auto_expose_routes` | `WithAutoExposeRoutes` | `true` | Turns routes into methods at start |
| `method_prefix` | `WithMethodPrefix` | `""` | Prefix for every generated method name |
| `exclude_patterns` | `WithExcludePatterns` | `["/_/*"]` | Paths to leave out |
| `include_patterns` | `WithIncludePatterns` | `[]` | If set, only matching paths are exposed |
| `naming_strategy` | `WithNamingStrategy` | `path` | `path`, `method` or `custom` (which behaves like `path` today) |
| `enable_openrpc` | `WithOpenRPC` | `true` | Serves the OpenRPC document |
| `enable_discovery` | `WithDiscovery` | `true` | Serves `<endpoint>/methods` |
| `enable_batch` | `WithBatch` | `true` | Accepts batch requests |
| `batch_limit` | `WithBatchLimit` | `10` | Largest batch accepted |
| `max_request_size` | `WithMaxRequestSize` | `1048576` | Largest request body, in bytes |
| `enable_metrics` | `WithMetrics` | `true` | Records `orpc_requests_total` and `orpc_methods_total` |

`WithConfig` replaces the whole config and `WithRequireConfig(true)` makes a missing config section an error at startup.

Six more settings exist in `Config` and pass validation, but nothing reads them yet: `require_auth`, `auth_header` and `auth_tokens` (set by `WithAuth`), `rate_limit_per_minute` (`WithRateLimit`), `request_timeout` (`WithRequestTimeout`) and `schema_cache` (`WithSchemaCache`). Don't rely on `WithAuth` to protect the endpoint. Put Forge middleware or your auth extension in front of `/rpc` instead.

## Documentation

| Topic | Where |
| --- | --- |
| What the extension does | [Features](https://www.xraph.com/docs/forge/extensions/orpc/features) |
| Every config key | [Configuration](https://www.xraph.com/docs/forge/extensions/orpc/configuration) |
| Exported functions and types | [Functions](https://www.xraph.com/docs/forge/extensions/orpc/functions), [pkg.go.dev](https://pkg.go.dev/github.com/xraph/forge/extensions/orpc) |
| How the extension follows the spec | [SPEC_COMPLIANCE.md](SPEC_COMPLIANCE.md) |
| Extensions in general | [Forge extension system](../../docs/content/docs/forge/extensions-system.mdx) |

## FAQ

### How do I expose a Go REST API over JSON-RPC 2.0?

Add `orpc.NewExtension()` to your Forge app and register your routes before the app starts. Each route becomes a method you can call at `POST /rpc`, and `GET /rpc/methods` shows you the names it picked.

### Does Forge oRPC generate an OpenRPC document?

Yes. `GET /rpc/schema` serves an OpenRPC 1.3.2 document. It's built from the same route metadata Forge uses for OpenAPI, so typed request and response schemas carry over.

### Is oRPC here the same as the TypeScript oRPC library?

No. This extension speaks JSON-RPC 2.0 and describes itself with OpenRPC. It doesn't implement the wire protocol of the TypeScript `orpc` project.

### Can I keep some routes out of JSON-RPC?

Add `forge.WithORPCExclude()` to the route, or list its path in `exclude_patterns`. Internal routes under `/_/` are excluded by default.

## Contributing

The extension is its own Go module inside the Forge repository. Test it from its directory:

```bash
cd extensions/orpc
GOWORK=off go test -race ./...
GOWORK=off golangci-lint run ./...
```

[AGENTS.md](../../AGENTS.md) has the full build, lint and release rules for the repository, and [CONTRIBUTING.md](../../CONTRIBUTING.md) has the contribution guidelines.

## License

Forge oRPC is part of Forge and is available under the [Apache License 2.0](../../LICENSE).
