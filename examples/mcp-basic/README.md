# MCP Basic Example

A simple example demonstrating the MCP extension with automatic route-to-tool conversion.

## Features

- Auto-exposes REST routes as MCP tools
- Simple CRUD operations for users
- Tool naming with prefix
- Pattern-based route filtering

## Running

```bash
cd v2/examples/mcp-basic
go run main.go
```

## Testing

### List all available tools

```bash
curl http://localhost:8080/_/mcp/tools | jq
```

Expected output shows tools like:
- `example_get_users`
- `example_get_users_id`
- `example_create_users`
- `example_update_users_id`
- `example_delete_users_id`

### Get user by ID

```bash
curl -X POST http://localhost:8080/_/mcp/tools/example_get_users_id \
  -H "Content-Type: application/json" \
  -d '{
    "name": "example_get_users_id",
    "arguments": {
      "id": "1"
    }
  }' | jq
```

### Create a new user

```bash
curl -X POST http://localhost:8080/_/mcp/tools/example_create_users \
  -H "Content-Type: application/json" \
  -d '{
    "name": "example_create_users",
    "arguments": {
      "body": {
        "name": "Charlie",
        "email": "charlie@example.com"
      }
    }
  }' | jq
```

### List all users

```bash
curl -X POST http://localhost:8080/_/mcp/tools/example_get_users \
  -H "Content-Type: application/json" \
  -d '{
    "name": "example_get_users",
    "arguments": {}
  }' | jq
```

## What's Happening

1. **Route Registration**: The app registers standard REST routes (`GET /users`, `POST /users`, etc.)
2. **Auto-Exposure**: MCP extension automatically converts these routes to tools
3. **Tool Naming**: Routes become tools with names like `example_get_users_id`
4. **Schema Generation**: Input schemas are auto-generated from route metadata
5. **Execution**: Calling a tool executes the underlying HTTP route

## Key Concepts

- **Tool Prefix**: All tools have `example_` prefix for namespace
- **Pattern Exclusion**: `/_/*` routes (like MCP endpoints) are excluded
- **Path Parameters**: `:id` in `/users/:id` becomes required `id` argument
- **Request Body**: POST/PUT methods accept `body` argument
- **Query Parameters**: Optional `query` argument for query strings

