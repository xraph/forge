# MCP Advanced Example

Advanced example demonstrating authentication, rate limiting, custom resources, and custom prompts.

## Features

- 🔐 **Token-based authentication**
- ⏱️ **Rate limiting** (60 requests/minute)
- 📦 **Custom resources** with readers
- 💬 **Custom prompts** with generators
- 🎯 **Pattern-based filtering** (only `/api/*` routes)

## Running

```bash
cd v2/examples/mcp-advanced
go run main.go
```

## Authentication

All MCP endpoints require the `X-API-Key` header:

```bash
curl -H "X-API-Key: dev-secret-key-123" http://localhost:8080/_/mcp/tools
```

Valid keys:
- `dev-secret-key-123` (development)
- `prod-secret-key-456` (production)

Without auth:

```bash
curl http://localhost:8080/_/mcp/tools
# Returns: 401 Unauthorized
```

## Rate Limiting

Limited to 60 requests per minute per client.

Test the limit:

```bash
# This will hit the rate limit
for i in {1..70}; do
  curl -H "X-API-Key: dev-secret-key-123" http://localhost:8080/_/mcp/tools
done
```

After 60 requests, you'll get:

```
429 Too Many Requests
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
Retry-After: 60
```

## Custom Resources

### List resources

```bash
curl -H "X-API-Key: dev-secret-key-123" \
  http://localhost:8080/_/mcp/resources | jq
```

### Read a resource

```bash
curl -X POST http://localhost:8080/_/mcp/resources/read \
  -H "X-API-Key: dev-secret-key-123" \
  -H "Content-Type: application/json" \
  -d '{
    "uri": "config://app-settings"
  }' | jq
```

This uses the custom resource reader to fetch application settings.

## Custom Prompts

### List prompts

```bash
curl -H "X-API-Key: dev-secret-key-123" \
  http://localhost:8080/_/mcp/prompts | jq
```

### Get a prompt

```bash
curl -X POST http://localhost:8080/_/mcp/prompts/api-documentation \
  -H "X-API-Key: dev-secret-key-123" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "api-documentation",
    "arguments": {
      "format": "markdown"
    }
  }' | jq
```

This uses the custom prompt generator to create API documentation.

## Available Tools

Since we use `WithIncludePatterns([]string{"/api/*"})`, only `/api/*` routes are exposed:

- `api_get_api_status` → `GET /api/status`
- `api_get_api_metrics` → `GET /api/metrics`

### Call a tool

```bash
curl -X POST http://localhost:8080/_/mcp/tools/api_get_api_status \
  -H "X-API-Key: dev-secret-key-123" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "api_get_api_status",
    "arguments": {}
  }' | jq
```

## Server Info

Get MCP server capabilities:

```bash
curl -H "X-API-Key: dev-secret-key-123" \
  http://localhost:8080/_/mcp/info | jq
```

Response shows enabled capabilities:
- Tools: ✓
- Resources: ✓ (custom reader registered)
- Prompts: ✓ (custom generator registered)

## Key Concepts

### Authentication Flow

1. Client sends request with `X-API-Key` header
2. `AuthMiddleware` validates token against `config.AuthTokens`
3. If valid, request proceeds; if not, returns 401

### Rate Limiting Flow

1. `RateLimiter` tracks requests per client (by token or IP)
2. Maintains a sliding window (1 minute)
3. When limit exceeded, returns 429 with `Retry-After` header
4. Response includes rate limit headers on all requests

### Custom Resources

1. Register resource with URI, name, description
2. Register custom reader function for that URI
3. Reader is called when resource is read via MCP
4. Returns content (text, JSON, etc.)

### Custom Prompts

1. Register prompt with name, description, arguments
2. Register custom generator function for that prompt
3. Generator is called with prompt arguments
4. Returns messages (user/assistant role)

## Production Considerations

1. **Secrets Management**: Don't hardcode tokens, use environment variables
2. **Rate Limits**: Adjust based on your API capacity
3. **Monitoring**: Track `mcp_tool_calls_total` and `mcp_rate_limit_exceeded_total` metrics
4. **Logging**: Monitor failed auth attempts
5. **HTTPS**: Always use TLS in production

## Code Structure

```go
// Security features
mcp.WithAuth("X-API-Key", []string{"token1", "token2"}),
mcp.WithRateLimit(60),

// Feature flags
mcp.WithResources(true),
mcp.WithPrompts(true),

// Pattern matching
mcp.WithIncludePatterns([]string{"/api/*"}),

// Custom handlers
server.RegisterResourceReader("config://app-settings", readerFunc)
server.RegisterPromptGenerator("api-documentation", generatorFunc)
```

