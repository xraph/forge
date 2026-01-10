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
  --state-management
```

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
    ├── client.ts       # Main client class
    ├── rest.ts         # REST methods
    ├── websocket.ts    # WebSocket clients
    └── sse.ts          # SSE clients
```

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

### Features

- **Reconnection**: Auto-reconnect for streaming
- **Heartbeat**: Connection health checks
- **StateManagement**: Track connection state
- **TypedErrors**: Generate typed error responses
- **RequestRetry**: Auto-retry failed requests
- **Timeout**: Request timeout configuration
- **Middleware**: Request/response interceptors
- **Logging**: Built-in logging support

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

