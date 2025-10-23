# MCP Extension for Forge v2

The MCP (Model Context Protocol) extension automatically exposes your Forge application's REST API as MCP-compatible tools, resources, and prompts for use with AI assistants like Claude.

## Features

- ✅ **Automatic Tool Generation**: Converts REST routes to MCP tools
- ✅ **Tool Execution**: Actually executes the underlying HTTP endpoints
- ✅ **Input Schema Generation**: Generates JSON schemas from route metadata
- ✅ **Resources & Prompts**: Support for data access and prompt templates
- ✅ **Authentication**: Token-based authentication for MCP endpoints
- ✅ **Rate Limiting**: Per-client rate limiting (requests per minute)
- ✅ **Custom Handlers**: Register custom resource readers and prompt generators
- ✅ **Observability**: Integrated metrics and logging
- ✅ **Pattern Matching**: Include/exclude routes with glob patterns

## Installation

```bash
go get github.com/xraph/forge/extensions/mcp
```

## Quick Start

### Basic Setup

```go
package main

import (
    "context"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/mcp"
)

func main() {
    // Create Forge app
    app := forge.NewApp(forge.AppConfig{
        Name:    "my-api",
        Version: "1.0.0",
    })
    
    // Add some routes
    app.Router().GET("/users/:id", getUserHandler)
    app.Router().POST("/users", createUserHandler)
    
    // Enable MCP extension (auto-exposes routes as tools)
    app.RegisterExtension(mcp.NewExtension(
        mcp.WithEnabled(true),
        mcp.WithAutoExposeRoutes(true),
    ))
    
    // Start the app
    app.Start(context.Background())
    app.Run(context.Background(), ":8080")
}
```

This automatically creates MCP tools like:
- `get_users_id` → `GET /users/:id`
- `create_users` → `POST /users`

### Access MCP Endpoints

The MCP server exposes these HTTP endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/_/mcp/info` | GET | Server info and capabilities |
| `/_/mcp/tools` | GET | List available tools |
| `/_/mcp/tools/:name` | POST | Execute a tool |
| `/_/mcp/resources` | GET | List resources (if enabled) |
| `/_/mcp/resources/read` | POST | Read a resource (if enabled) |
| `/_/mcp/prompts` | GET | List prompts (if enabled) |
| `/_/mcp/prompts/:name` | POST | Get a prompt (if enabled) |

## Configuration

### Using ConfigManager (Recommended)

In `config.yaml`:

```yaml
extensions:
  mcp:
    enabled: true
    base_path: "/_/mcp"
    auto_expose_routes: true
    tool_prefix: "myapi_"
    exclude_patterns:
      - "/_/*"
      - "/internal/*"
    max_tool_name_length: 64
    enable_resources: true
    enable_prompts: true
    require_auth: false
    rate_limit_per_minute: 100
```

Then in code:

```go
// Config loaded automatically from ConfigManager
app.RegisterExtension(mcp.NewExtension())
```

### Programmatic Configuration

```go
app.RegisterExtension(mcp.NewExtension(
    mcp.WithEnabled(true),
    mcp.WithBasePath("/_/mcp"),
    mcp.WithAutoExposeRoutes(true),
    mcp.WithToolPrefix("api_"),
    mcp.WithExcludePatterns([]string{"/_/*", "/internal/*"}),
    mcp.WithIncludePatterns([]string{"/api/*"}), // Only expose /api/* routes
    mcp.WithResources(true),
    mcp.WithPrompts(true),
    mcp.WithAuth("X-API-Key", []string{"secret-token"}),
    mcp.WithRateLimit(100), // 100 requests per minute
))
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `Enabled` | bool | `true` | Enable/disable MCP server |
| `BasePath` | string | `"/_/mcp"` | Base path for MCP endpoints |
| `AutoExposeRoutes` | bool | `true` | Auto-generate tools from routes |
| `ToolPrefix` | string | `""` | Prefix for tool names |
| `ExcludePatterns` | []string | `[]` | Patterns to exclude from tools |
| `IncludePatterns` | []string | `[]` | Only expose matching patterns |
| `MaxToolNameLength` | int | `64` | Max length for tool names |
| `EnableResources` | bool | `false` | Enable resources endpoint |
| `EnablePrompts` | bool | `false` | Enable prompts endpoint |
| `RequireAuth` | bool | `false` | Require authentication |
| `AuthHeader` | string | `"Authorization"` | Auth header name |
| `AuthTokens` | []string | `[]` | Valid auth tokens |
| `RateLimitPerMinute` | int | `0` | Rate limit (0 = unlimited) |
| `EnableMetrics` | bool | `true` | Enable metrics collection |
| `SchemaCache` | bool | `true` | Cache generated schemas |

## Usage Examples

### Tool Execution

Call a tool via HTTP:

```bash
curl -X POST http://localhost:8080/_/mcp/tools/get_users_id \
  -H "Content-Type: application/json" \
  -d '{
    "name": "get_users_id",
    "arguments": {
      "id": "123",
      "query": {
        "include": "profile"
      }
    }
  }'
```

Response:

```json
{
  "content": [
    {
      "type": "text",
      "text": "Tool executed: GET /users/123?include=profile"
    }
  ],
  "isError": false
}
```

### Custom Resource Readers

```go
// Register a custom resource reader
ext := app.Extensions().Get("mcp").(*mcp.Extension)
server := ext.Server()

// Register resource
server.RegisterResource(&mcp.Resource{
    URI:         "file://config/settings.json",
    Name:        "App Settings",
    Description: "Application configuration",
    MimeType:    "application/json",
})

// Register custom reader
server.RegisterResourceReader("file://config/settings.json", 
    func(ctx context.Context, resource *mcp.Resource) (mcp.Content, error) {
        data, err := os.ReadFile("config/settings.json")
        if err != nil {
            return mcp.Content{}, err
        }
        
        return mcp.Content{
            Type: "text",
            Text: string(data),
            MimeType: "application/json",
        }, nil
    })
```

### Custom Prompt Generators

```go
// Register a prompt
server.RegisterPrompt(&mcp.Prompt{
    Name:        "generate-user-query",
    Description: "Generates a SQL query to find users",
    Arguments: []mcp.PromptArgument{
        {Name: "filter", Description: "Filter criteria", Required: true},
        {Name: "limit", Description: "Max results", Required: false},
    },
})

// Register custom generator
server.RegisterPromptGenerator("generate-user-query",
    func(ctx context.Context, prompt *mcp.Prompt, args map[string]interface{}) ([]mcp.PromptMessage, error) {
        filter := args["filter"].(string)
        limit := 10
        if l, ok := args["limit"].(int); ok {
            limit = l
        }
        
        query := fmt.Sprintf("SELECT * FROM users WHERE %s LIMIT %d", filter, limit)
        
        return []mcp.PromptMessage{
            {
                Role: "assistant",
                Content: []mcp.Content{
                    {Type: "text", Text: query},
                },
            },
        }, nil
    })
```

## Security

### Authentication

Enable token-based authentication:

```go
mcp.NewExtension(
    mcp.WithAuth("X-API-Key", []string{
        "prod-token-abc123",
        "dev-token-xyz789",
    }),
)
```

Clients must include the header:

```bash
curl -H "X-API-Key: prod-token-abc123" \
  http://localhost:8080/_/mcp/tools
```

Or Bearer token format:

```bash
curl -H "Authorization: Bearer prod-token-abc123" \
  http://localhost:8080/_/mcp/tools
```

### Rate Limiting

Limit requests per client per minute:

```go
mcp.NewExtension(
    mcp.WithRateLimit(100), // 100 requests/minute
)
```

Rate limit headers in response:

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1234567890
```

When exceeded, returns `429 Too Many Requests` with `Retry-After` header.

## Pattern Matching

### Exclude Patterns

Exclude internal/system routes:

```go
mcp.NewExtension(
    mcp.WithExcludePatterns([]string{
        "/_/*",        // Exclude /_/health, /_/metrics, etc.
        "/internal/*", // Exclude /internal/admin, etc.
        "/debug/*",    // Exclude debug routes
    }),
)
```

### Include Patterns

Only expose specific API routes:

```go
mcp.NewExtension(
    mcp.WithIncludePatterns([]string{
        "/api/*",      // Only expose /api/* routes
        "/public/*",   // And /public/* routes
    }),
)
```

**Note:** If `IncludePatterns` is set, only matching routes are exposed.

## Tool Naming

Tools are named based on HTTP method and path:

| Route | Method | Tool Name |
|-------|--------|-----------|
| `/users` | GET | `get_users` |
| `/users` | POST | `create_users` |
| `/users/:id` | GET | `get_users_id` |
| `/users/:id` | PUT | `update_users_id` |
| `/users/:id` | DELETE | `delete_users_id` |
| `/users/:id` | PATCH | `patch_users_id` |
| `/api/v1/posts` | GET | `get_api_v1_posts` |

With `ToolPrefix`:

```go
mcp.WithToolPrefix("myapi_")
// Results in: myapi_get_users, myapi_create_users, etc.
```

## Input Schemas

The extension automatically generates JSON schemas for tool inputs:

```json
{
  "type": "object",
  "properties": {
    "id": {
      "type": "string",
      "description": "Path parameter: id"
    },
    "body": {
      "type": "object",
      "description": "Request body"
    },
    "query": {
      "type": "object",
      "description": "Query parameters (optional)"
    }
  },
  "required": ["id"]
}
```

## Observability

### Metrics

The extension automatically records:

- `mcp_tools_total` - Total number of registered tools (gauge)
- `mcp_resources_total` - Total resources (gauge)
- `mcp_prompts_total` - Total prompts (gauge)
- `mcp_tool_calls_total{status}` - Tool calls by status (counter)
- `mcp_rate_limit_exceeded_total` - Rate limit violations (counter)

### Logging

All MCP operations are logged with structured fields:

```go
logger.Debug("mcp: tool registered", forge.F("tool", toolName))
logger.Warn("mcp: rate limit exceeded", forge.F("client", clientID))
logger.Error("mcp: tool execution failed", forge.F("tool", toolName), forge.F("error", err))
```

## Advanced Usage

### Manual Tool Registration

Register tools manually without auto-exposure:

```go
server := ext.Server()

server.RegisterTool(&mcp.Tool{
    Name:        "complex-operation",
    Description: "Performs a complex multi-step operation",
    InputSchema: &mcp.JSONSchema{
        Type: "object",
        Properties: map[string]*mcp.JSONSchema{
            "step1": {Type: "string", Description: "First step"},
            "step2": {Type: "integer", Description: "Second step"},
        },
        Required: []string{"step1", "step2"},
    },
})
```

### Accessing from AI

Use with Claude Desktop or other MCP-compatible clients:

1. **Configure Claude Desktop** (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "my-api": {
      "url": "http://localhost:8080/_/mcp",
      "headers": {
        "X-API-Key": "your-token-here"
      }
    }
  }
}
```

2. **Claude can now call your API** via natural language:
   - "Get user with ID 123"
   - "Create a new user with name John"
   - "List all posts"

## Testing

```go
func TestMCPExtension(t *testing.T) {
    app := forge.NewApp(forge.AppConfig{
        Name: "test-app",
        Extensions: []forge.Extension{
            mcp.NewExtension(mcp.WithEnabled(true)),
        },
    })
    
    // Add test routes
    app.Router().GET("/test", func(ctx forge.Context) error {
        return ctx.JSON(200, map[string]string{"status": "ok"})
    })
    
    // Start app
    app.Start(context.Background())
    defer app.Stop(context.Background())
    
    // Test MCP endpoints
    req := httptest.NewRequest("GET", "/_/mcp/tools", nil)
    rec := httptest.NewRecorder()
    app.Router().ServeHTTP(rec, req)
    
    assert.Equal(t, 200, rec.Code)
}
```

## Architecture

```
┌─────────────────────────────────────────────┐
│           Forge Application                 │
│  ┌──────────────────────────────────────┐  │
│  │     Router (REST API)                │  │
│  │  GET /users/:id                      │  │
│  │  POST /users                         │  │
│  │  PUT /users/:id                      │  │
│  └──────────────────────────────────────┘  │
│                    │                        │
│                    ▼                        │
│  ┌──────────────────────────────────────┐  │
│  │     MCP Extension                    │  │
│  │  • Auto-generate tools               │  │
│  │  • Execute via HTTP                  │  │
│  │  • Auth & Rate Limiting              │  │
│  └──────────────────────────────────────┘  │
│                    │                        │
│                    ▼                        │
│  ┌──────────────────────────────────────┐  │
│  │     MCP Server Endpoints             │  │
│  │  /_/mcp/info                         │  │
│  │  /_/mcp/tools                        │  │
│  │  /_/mcp/tools/:name (execute)        │  │
│  └──────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
                    │
                    ▼
        ┌───────────────────────┐
        │   AI Assistant        │
        │   (Claude, etc.)      │
        └───────────────────────┘
```

## Best Practices

1. **Security First**: Always enable authentication in production
2. **Rate Limiting**: Set appropriate limits to prevent abuse
3. **Pattern Matching**: Exclude internal/debug routes
4. **Tool Naming**: Use consistent prefixes for your API
5. **Custom Handlers**: Register custom readers/generators for complex operations
6. **Monitoring**: Track metrics to understand usage patterns
7. **Documentation**: Add summaries to routes for better tool descriptions

## License

MIT

## Contributing

Contributions welcome! Please submit PRs to the main Forge repository.

## Support

- [Documentation](https://forge.xraph.dev)
- [GitHub Issues](https://github.com/xraph/forge/issues)
- [Discord Community](https://discord.gg/forge)

