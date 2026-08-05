# Forge Client Generator

An elegant, extensible client code generator that introspects Forge routes and generates type-safe clients for multiple languages.

## Features

- **Multi-Language Support**: Currently supports Go and TypeScript, extensible to Rust and other languages
- **Comprehensive API Coverage**: Generates clients for REST, WebSocket, and SSE endpoints
- **Smart Auth Integration**: Detects and generates proper authentication code (Bearer, API Key, Basic, OAuth2)
- **Advanced Streaming**: Includes reconnection, heartbeat, and connection state management for WebSocket/SSE
- **Type-Safe**: Generates fully typed clients from OpenAPI/AsyncAPI specifications
- **Clean API**: Modern, idiomatic code generation for each target language
- **Extensible**: Plugin-based architecture for adding new language generators

## Architecture

### Core Components

- **`ir.go`**: Intermediate Representation - Language-agnostic API spec data structures
- **`introspector.go`**: Extracts route information from Forge Router (runtime introspection)
- **`spec_parser.go`**: Parses OpenAPI 3.1.0 and AsyncAPI 3.0.0 specifications from files
- **`generator.go`**: Central orchestration and language generator registry
- **`auth.go`**: Authentication scheme detection and code generation utilities
- **`streaming.go`**: Streaming features (reconnection, heartbeat, state management)
- **`config.go`**: Generator configuration with feature flags
- **`output.go`**: File writing and README generation

### Language Generators

#### Go Generator (`generators/golang/`)

- **`generator.go`**: Main generator with client struct and auth config
- **`types.go`**: Type definitions from schemas
- **`rest.go`**: REST endpoint methods
- **`websocket.go`**: WebSocket clients with goroutines and channels
- **`sse.go`**: Server-Sent Events clients

Features:
- Idiomatic Go code with context support
- Graceful error handling
- Connection pooling and reconnection
- Structured with proper package layout

#### TypeScript Generator (`generators/typescript/`)

- **`generator.go`**: Main generator with Axios-based client
- Type-safe interfaces from OpenAPI schemas
- Modern async/await patterns
- NPM package generation with `package.json` and `tsconfig.json`

## Usage

### CLI Commands

#### Generate Client

```bash
# Generate from OpenAPI/AsyncAPI spec file
forge client generate --from-spec ./api/openapi.yaml --language go --output ./sdk

# With full options
forge client generate \
  --from-spec ./api/openapi.yaml \
  --language typescript \
  --output ./clients/typescript \
  --package "@myorg/api-client" \
  --base-url "https://api.example.com" \
  --auth \
  --streaming \
  --reconnection \
  --heartbeat \
  --state-management \
  --field-naming camel \
  --field-overrides "User.user_id=userIdentifier"
```

See "Field Naming" below for `--field-naming`/`--field-overrides` and the
breaking change they relate to.

#### List Endpoints

```bash
# List all endpoints from spec
forge client list --from-spec ./api/openapi.yaml

# Filter by type
forge client list --from-spec ./api/openapi.yaml --type rest
forge client list --from-spec ./api/openapi.yaml --type ws
forge client list --from-spec ./api/openapi.yaml --type sse
```

#### Initialize Config

```bash
# Interactive configuration wizard
forge client init
```

Creates `.forge-client.yaml`:

```yaml
clients:
  - language: go
    output: clients/go
    package: apiclient
    base_url: https://api.example.com
    features:
      reconnection: true
      heartbeat: true
      state_management: true
```

### Programmatic API

```go
import (
    "context"
    "github.com/xraph/forge/internal/client"
    "github.com/xraph/forge/internal/client/generators/golang"
)

// Create generator
gen := client.NewGenerator()

// Register language generators
gen.Register(golang.NewGenerator())

// Configure generation
config := client.GeneratorConfig{
    Language:         "go",
    OutputDir:        "./sdk",
    PackageName:      "apiclient",
    BaseURL:          "https://api.example.com",
    IncludeAuth:      true,
    IncludeStreaming: true,
    Features: client.Features{
        Reconnection:    true,
        Heartbeat:       true,
        StateManagement: true,
        TypedErrors:     true,
    },
}

// Generate from spec file
generatedClient, err := gen.GenerateFromFile(context.Background(), "./openapi.yaml", config)
if err != nil {
    log.Fatal(err)
}

// Write to disk
outputMgr := client.NewOutputManager()
err = outputMgr.WriteClient(generatedClient, config.OutputDir)
```

## Generated Client Structure

### Go

```
sdk/
├── go.mod              # Go module file
├── README.md           # Usage instructions
├── client.go           # Main client with auth config
├── types.go            # Generated type definitions
├── rest.go             # REST endpoint methods
├── websocket.go        # WebSocket clients
├── sse.go              # SSE clients
└── errors.go           # Error types
```

Example usage:

```go
import "your-module/sdk"

client := sdk.NewClient(
    sdk.WithBaseURL("https://api.example.com"),
    sdk.WithAuth(sdk.AuthConfig{
        BearerToken: "your-token",
    }),
)

// REST call
result, err := client.GetUser(ctx, "user-123")

// WebSocket
ws := client.NewChatWSClient()
ws.Connect(ctx)
ws.OnMessage(func(msg ChatMessage) {
    fmt.Println("Received:", msg)
})
ws.Send(ChatMessage{Text: "Hello"})

// SSE
sse := client.NewNotificationSSEClient()
sse.Connect(ctx)
sse.OnNotification(func(notif Notification) {
    fmt.Println("Notification:", notif)
})
```

### TypeScript

```
client/
├── package.json        # NPM package config
├── tsconfig.json       # TypeScript config
├── README.md           # Usage instructions
└── src/
    ├── index.ts        # Barrel exports
    ├── types.ts        # Type definitions
    ├── codecs.ts       # Wire <-> client-side field name encode/decode (see "Field Naming" below)
    ├── client.ts       # Main client class
    ├── rest.ts         # REST methods
    ├── websocket.ts    # WebSocket clients
    └── sse.ts          # SSE clients
```

`codecs.ts` is only emitted when it would do real work -- see "Field Naming"
below for exactly when that is (and is not) the case.

Example usage:

```typescript
import { Client } from '@myorg/api-client';

const client = new Client({
  baseURL: 'https://api.example.com',
  auth: {
    bearerToken: 'your-token',
  },
});

// REST call
const user = await client.getUser('user-123');

// WebSocket
const ws = new ChatWSClient();
await ws.connect();
ws.onMessage((msg) => {
  console.log('Received:', msg);
});
ws.send({ text: 'Hello' });
```

## Authentication Support

The client generator automatically detects and generates appropriate auth code for:

- **Bearer Token** (JWT)
- **API Key** (header or query parameter)
- **Basic Auth**
- **OAuth 2.0** (all flows)
- **Custom Headers**

Detection is based on OpenAPI security schemes in the specification.

## Streaming Features

### Reconnection

- Exponential backoff strategy
- Configurable max attempts and delays
- Automatic resume with last event ID (SSE)

### Heartbeat

- Periodic ping messages for WebSocket
- Configurable intervals
- Connection health monitoring

### State Management

- Connection state tracking (disconnected, connecting, connected, reconnecting, closed, error)
- State change callbacks
- Thread-safe state access

## Extension Guide

### Adding a New Language Generator

1. Create a new package: `generators/<language>/`

2. Implement the `LanguageGenerator` interface:

```go
type LanguageGenerator interface {
    Name() string
    SupportedFeatures() []string
    Generate(ctx context.Context, spec APISpec, config GeneratorConfig) (*GeneratedClient, error)
    Validate(spec APISpec) error
}
```

3. Create generator files:
   - `generator.go` - Main generator and client structure
   - `types.go` - Type system mapping
   - `rest.go` - REST endpoint generation
   - `websocket.go` - WebSocket client generation (if supported)
   - `sse.go` - SSE client generation (if supported)

4. Register the generator:

```go
gen.Register(yourlang.NewGenerator())
```

### Type Mapping

Each language generator must map OpenAPI/JSON Schema types to native types:

| JSON Schema | Go | TypeScript | Rust |
|------------|-----|------------|------|
| string | string | string | String |
| integer | int | number | i32/i64 |
| number | float64 | number | f64 |
| boolean | bool | boolean | bool |
| array | []T | T[] | Vec<T> |
| object | struct | interface | struct |

## Configuration Options

### GeneratorConfig

- **Language**: Target language (go, typescript, rust)
- **OutputDir**: Where to write generated files
- **PackageName**: Package/module name
- **APIName**: Main client class/struct name
- **BaseURL**: Default API base URL
- **IncludeAuth**: Generate authentication configuration
- **IncludeStreaming**: Generate WebSocket/SSE clients
- **Module**: Go module path (Go only)
- **Version**: Generated client version
- **FieldNaming**: Client-side identifier style for schema properties --
  `camel`, `pascal`, `snake`, or `preserve` (TypeScript only; see "Field
  Naming" below). Empty resolves to `camel` when `Language` is
  `"typescript"`, `preserve` otherwise.
- **FieldOverrides**: Per-field client-side name overrides that bypass
  `FieldNaming` entirely (see "Field Naming" below).

### Features

- **Reconnection**: Auto-reconnect for streaming
- **Heartbeat**: Connection health checks
- **StateManagement**: Track connection state
- **TypedErrors**: Generate typed error responses
- **RequestRetry**: Auto-retry failed requests
- **Timeout**: Request timeout configuration
- **Middleware**: Request/response interceptors
- **Logging**: Built-in logging support

## Field Naming (TypeScript)

> **Breaking change.** Generating a TypeScript client with the default
> configuration now renames every schema property to camelCase --
> `user.user_id` in an OpenAPI spec becomes `user.userId` in the generated
> client, not `user.user_id`. If you are upgrading an existing generated
> client and want the old, wire-cased behaviour back, set `FieldNaming:
> "preserve"` (Go API) or pass `--field-naming preserve` (CLI) -- see
> "Escape hatch" below.

By default, the TypeScript generator renders every schema property under a
client-side name derived from its wire (JSON) name, and generates a codec
(`src/codecs.ts`) that renames payloads at the HTTP boundary so the actual
runtime values match the declared TypeScript types: a request encodes
client-side names back to wire names before the request is sent, and a
response decodes wire names to client-side names after it arrives.

### `FieldNaming` strategies

| Value | Example (wire `user_id`) | Notes |
|---|---|---|
| `camel` (default for TypeScript) | `userId` | Standard TypeScript/JavaScript convention |
| `pascal` | `UserId` | |
| `snake` | `user_id` | No-op if the wire name is already snake_case |
| `preserve` | `user_id` | Wire name rendered verbatim; the pre-Phase-3 behaviour |

An unset `FieldNaming` (Go zero value, or omitting `--field-naming` on the
CLI) resolves to `camel` when `Language` is `"typescript"`, and to
`preserve` for every other language -- so a Go generator caller is
completely unaffected. An unrecognised strategy value passed to the Go API
directly (e.g. a typo'd `client.GeneratorConfig{FieldNaming:
"kebab"}`) silently falls back to `preserve` rather than erroring, since
`GeneratorConfig` currently has no validation path for this field; the CLI
layer (`--field-naming`) does NOT share this leniency -- an unrecognised
CLI value is rejected outright.

### `FieldOverrides`

`FieldOverrides` renames one specific field differently from whatever
`FieldNaming` strategy is configured -- including under `preserve` (see
"Escape hatch" below). Each key is either:

- **Schema-scoped**: `"SchemaName.wire_name"` -- applies only within that
  schema (and any inline nested object using that same namespace id, e.g.
  `"Order.shipping.street_name"`).
- **Global**: `"wire_name"` -- applies everywhere that wire name occurs with
  no schema-scoped entry taking precedence.

A schema-scoped key always wins over a global one for the same wire name.
An override's value is used verbatim -- it is never case-converted, even if
it happens to look like a wire name.

```go
config := client.GeneratorConfig{
    Language:    "typescript",
    FieldNaming: client.NamingCamel,
    FieldOverrides: map[string]string{
        "User.user_id": "userIdentifier", // schema-scoped
        "api_key":       "apiKey",        // global
    },
}
```

On the CLI, `--field-overrides` takes a single comma-separated
`key=clientName` list (schema-scoped and global keys use the same format as
above):

```bash
forge client generate --language typescript \
  --field-overrides "User.user_id=userIdentifier,api_key=apiKey"
```

**Known ambiguity**: a schema name or wire name containing a literal `.`
makes the concatenated key ambiguous -- schema `"User.Detail"` + wire `"id"`
and schema `"User"` + wire `"Detail.id"` both produce the same key,
`"User.Detail.id"`. OpenAPI schema and property names are conventionally
dot-free, so this is treated as an accepted, unresolved edge case rather
than requiring an escaping scheme.

**Known limitation**: when a nested object is reachable through more than
one composition path (e.g. both `Addr.payload.x` and `Base.payload.x`
resolve to logically "the same" property), an override must be repeated
once per namespace it is reachable through -- there is no single override
that applies to every path at once.

### Collision detection

Generation fails outright (producing no output files at all) if two
distinct wire names in the same object namespace would resolve to the same
client-side name under the configured `FieldNaming`/`FieldOverrides` --
this includes top-level schemas, inline nested objects, array items,
`additionalProperties` values, and `oneOf`/`anyOf`/`allOf` members. The
error names both wire names, the schema, and the exact `FieldOverrides` key
that would resolve the collision.

This check also runs under `preserve` whenever `FieldOverrides` is
non-empty -- an override renames a field even under `preserve`, so two
overrides that map different wire names to the same client name are still
a real collision, not just an ordinary no-op-renaming pair of wire names.

Two narrower gaps are known and not yet closed: a collision cannot be
detected through an `allOf` member that is itself a union (its alternatives
are invisible to the guard), and an `allOf` member that is a bare array or
an `additionalProperties`-only schema is silently unwarned about (though
harmless, since such a member degrades to passthrough rather than
contributing fields).

### Escape hatch

Set `FieldNaming: "preserve"` (Go API) or `--field-naming preserve` (CLI)
to keep every generated field name exactly as the wire declares it -- byte-
identical to the pre-Phase-3 output. When `preserve` is set AND
`FieldOverrides` is empty, the entire codec table (`src/codecs.ts`, its
imports, and every `bodyCodec`/`responseCodec` reference) is omitted
entirely as dead weight, since nothing would ever need renaming. Setting
even one `FieldOverrides` entry keeps the codec table alive, since that one
field still needs to be renamed at the HTTP boundary.

### The operation manifest under renaming

`src/ops.ts` is what the `@forge-go/client-core` runtime reads, and it
drives the generated `HTTPClient#request` -- never the typed per-endpoint
methods, whose parameters are positional and per-endpoint. Two things
follow, and they are one change because either alone is a regression:

- Each `OperationMeta` carries `bodyCodec`/`responseCodec`, the same ids
  `rest.go` writes into the typed methods' `RequestConfig`, resolved by the
  same functions (`requestBodyCodecRef`/`responseCodecRef`). Without them
  the transport ships wire-cased bodies and returns un-decoded responses,
  so hooks and the direct REST client disagree about the shape of the same
  generated type.
- The `entities` table's `idField`, and the KEYS of its `fields`, are
  emitted in the CLIENT-side naming (via `tsFieldName`, the same function
  the type renderer and the codec table use), because the runtime
  normalizes a response that `decode` has already renamed. The VALUES of
  `fields` are typenames, not field names, and are never renamed. A table
  still naming wire fields against a decoded payload does not fail loudly:
  a type whose id field is absent is simply not an entity, so the cache
  quietly stops caching.

- The DERIVED item cache tag, `Type:{IDField}`, is renamed for the same
  reason: the runtime resolves a `provides` template against the decoded
  response (`QueryRegistry#settle`), so `Order:{order_number}` would resolve
  to nothing, the query would be registered under no item tag, and a later
  write to that order would invalidate nothing. Only the exact tag
  `DeriveTags` builds is rewritten -- a hand-declared template
  (`Shipment:{res.shipment.id}`) names properties of types the manifest
  cannot resolve a namespace for, and is left alone rather than guessed at.

All of the above are identity under `preserve` with no `FieldOverrides`, so
that configuration emits a byte-identical `ops.ts`.

### Other known limitations

- A discriminated union whose members are THEMSELVES discriminated unions
  encodes entirely in camelCase (the rename does not apply) -- generation
  warns, but the warning's stated reason (implemented as of this writing)
  is inaccurate; the underlying gap is tracked, not yet fixed.
- A schema property literally named `additionalProperties` can alias an
  internal codec id, risking silent data corruption if such a property
  occurs in practice.
- `additionalProperties` declared on an `allOf` composition is dropped by
  both the type renderer and the codec table (so it is silently untyped and
  unrenamed), though the collision guard still walks it.
- WebSocket and SSE payload types do not go through the codec at all --
  `types.User` renders camelCase, but a streamed payload is parsed/
  stringified raw, so a streaming consumer reading a renamed field is
  reading a value that was never actually renamed.
- A media type of `application/json; charset=utf-8` (or any other
  parameterized JSON content type) is not recognized by the generator's
  spec-side content-type lookups, which match `"application/json"`
  exactly. A request/response body declared with a parameterized
  content type is typed as `Blob` instead of the schema type, gets no codec
  reference, and generation does not warn -- while the runtime HTTP client
  still JSON-parses the response body regardless.

## Testing

The client generator includes comprehensive tests:

- **Unit tests**: Test IR conversion, type mapping, code generation
- **Integration tests**: Generate clients and verify compilation
- **Fixture tests**: Test against sample OpenAPI/AsyncAPI specs

Run tests:

```bash
cd v2/internal/client
go test ./...
```

## Performance Considerations

- **Lazy initialization**: Clients are created on-demand
- **Connection pooling**: Reuse HTTP connections
- **Streaming efficiency**: Goroutines/async for concurrent processing
- **Memory management**: Proper cleanup and resource disposal

## Roadmap

- [ ] Rust generator
- [ ] Python generator  
- [ ] gRPC support
- [ ] GraphQL support
- [ ] Code formatting integration (gofmt, prettier, rustfmt)
- [ ] Validation middleware generation
- [ ] Rate limiting client-side
- [ ] Circuit breaker patterns
- [ ] Metrics and telemetry hooks
- [ ] Mock client generation for testing

## Contributing

When adding features or fixing bugs:

1. Update IR types if needed (`ir.go`)
2. Update language generators
3. Add tests
4. Update documentation
5. Run linters: `golangci-lint run`

## License

Part of the Forge framework. See main project license.

