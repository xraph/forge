# Optional Tag Example

This example demonstrates the `optional:"true"` struct tag feature in Forge, which allows you to mark non-pointer fields as optional in OpenAPI and AsyncAPI schema generation.

## Overview

Previously, the only way to mark a field as optional was to use a pointer type (`*string` instead of `string`). However, this approach has drawbacks:

- Requires nil checks in your code
- Makes working with default values more complex
- Doesn't work well with validation tags
- Less ergonomic API design

The `optional:"true"` tag solves these problems by allowing you to keep non-pointer types while marking them as optional in the generated schemas.

## Field Required Logic

Forge determines if a field is required based on the following precedence:

1. **`optional:"true"`** - Explicitly marks field as optional (highest priority)
2. **`required:"true"`** - Explicitly marks field as required
3. **`omitempty` in json/query/header tags** - Marks field as optional
4. **Pointer type** - Automatically optional
5. **Default behavior** - Non-pointer types without the above are required

## Use Cases

### 1. Query Parameters with Defaults

```go
type PaginationParams struct {
    Limit  int    `query:"limit" default:"10" optional:"true"`
    Offset int    `query:"offset" default:"0" optional:"true"`
    Page   int    `query:"page" default:"1" optional:"true"`
}
```

All pagination parameters have sensible defaults and are optional in API calls:
- `GET /users` - Uses all defaults
- `GET /users?page=2` - Only overrides page
- `GET /users?limit=50&page=3` - Overrides limit and page

### 2. Search and Filter Parameters

```go
type BaseRequestParams struct {
    SortBy string `query:"sort_by" default:"created_at" optional:"true"`
    Order  string `query:"order" default:"desc" optional:"true"`
    Search string `query:"search" default:"" optional:"true"`
    Filter string `query:"filter" default:"" optional:"true"`
}
```

These fields have empty string defaults and shouldn't be required:
- `GET /users` - No filtering/sorting applied
- `GET /users?search=john` - Only search applied
- `GET /users?sort_by=name&order=asc` - Custom sorting

### 3. Embedded Structs

```go
type PaginationParams struct {
    BaseRequestParams  // All fields from base are optional
    Limit  int `query:"limit" default:"10" optional:"true"`
    Offset int `query:"offset" default:"0" optional:"true"`
}
```

The `optional` tag works correctly with embedded/anonymous structs, flattening all fields while preserving their optional status.

### 4. Header Parameters

```go
type RequestHeaders struct {
    Authorization string `header:"Authorization"` // Required
    TraceID       string `header:"X-Trace-ID" optional:"true"` // Optional
    RequestID     string `header:"X-Request-ID" optional:"true"` // Optional
}
```

Common pattern: auth headers are required, tracing headers are optional.

### 5. Request Body Fields

```go
type UpdateUserRequest struct {
    Name  string `json:"name"`  // Required
    Email string `json:"email"` // Required
    Bio   string `json:"bio" optional:"true"` // Optional
    Phone string `json:"phone" optional:"true"` // Optional
}
```

Partial updates often have a mix of required and optional fields.

## Running the Example

```bash
# Run the example
go run examples/optional_tag/main.go

# The example will:
# 1. Print the generated OpenAPI spec
# 2. Show which fields are marked as optional
# 3. Start a server on http://localhost:8080
```

## Testing the API

Once the server is running, try these requests:

```bash
# List users with all defaults
curl http://localhost:8080/users

# Paginate to page 2 with custom limit
curl 'http://localhost:8080/users?page=2&limit=20'

# Search and sort
curl 'http://localhost:8080/users?search=john&sort_by=name&order=asc'

# Cursor-based pagination
curl http://localhost:8080/users/cursor

# Cursor with custom limit
curl 'http://localhost:8080/users/cursor?limit=5'
```

## Generated OpenAPI Schema

The `PaginationParams` struct will generate an OpenAPI schema where all fields are optional:

```json
{
  "PaginationParams": {
    "type": "object",
    "properties": {
      "sortBy": {
        "type": "string",
        "default": "created_at",
        "example": "created_at"
      },
      "order": {
        "type": "string",
        "default": "desc",
        "example": "desc"
      },
      "search": {
        "type": "string",
        "default": "",
        "example": "john"
      },
      "filter": {
        "type": "string",
        "default": "",
        "example": "status:active"
      },
      "fields": {
        "type": "string",
        "default": "",
        "example": "id,name,email"
      },
      "limit": {
        "type": "integer",
        "default": 10,
        "example": 10
      },
      "offset": {
        "type": "integer",
        "default": 0,
        "example": 0
      },
      "page": {
        "type": "integer",
        "default": 1,
        "example": 1
      }
    },
    "required": []  // No required fields!
  }
}
```

## Query Parameters

The query parameters for `/users` endpoint will all have `required: false`:

```json
{
  "parameters": [
    {
      "name": "sort_by",
      "in": "query",
      "required": false,
      "schema": {
        "type": "string",
        "default": "created_at"
      }
    },
    {
      "name": "order",
      "in": "query",
      "required": false,
      "schema": {
        "type": "string",
        "default": "desc"
      }
    },
    // ... all other parameters with required: false
  ]
}
```

## Comparison: Before vs After

### Before (using pointers)

```go
type PaginationParams struct {
    Limit  *int    `query:"limit"`
    Offset *int    `query:"offset"`
    Page   *int    `query:"page"`
}

func handler(ctx forge.Context) error {
    params := PaginationParams{}
    ctx.BindQuery(&params)
    
    // Painful nil checks and default handling
    limit := 10
    if params.Limit != nil {
        limit = *params.Limit
    }
    
    offset := 0
    if params.Offset != nil {
        offset = *params.Offset
    }
    
    page := 1
    if params.Page != nil {
        page = *params.Page
    }
    
    // Use values...
}
```

### After (using optional tag)

```go
type PaginationParams struct {
    Limit  int `query:"limit" default:"10" optional:"true"`
    Offset int `query:"offset" default:"0" optional:"true"`
    Page   int `query:"page" default:"1" optional:"true"`
}

func handler(ctx forge.Context) error {
    params := PaginationParams{}
    ctx.BindQuery(&params)
    
    // Defaults are already set by binding!
    // Just use the values directly
    limit := params.Limit
    offset := params.Offset
    page := params.Page
    
    // Use values...
}
```

## Best Practices

1. **Use `optional:"true"` for fields with sensible defaults**
   - Pagination parameters (limit, offset, page)
   - Sort parameters (sortBy, order)
   - Search/filter parameters

2. **Combine with `default` tag for documentation**
   ```go
   Limit int `query:"limit" default:"10" optional:"true"`
   ```

3. **Use pointers only when you need to distinguish between "not provided" and "zero value"**
   ```go
   // Bad: Using pointer when optional tag would work
   Name *string `query:"name"`
   
   // Good: Using optional tag for truly optional field
   Name string `query:"name" optional:"true"`
   
   // Good: Using pointer when you need to know if it was explicitly set to zero
   Weight *float64 `json:"weight"` // Need to distinguish 0.0 from not provided
   ```

4. **`optional` tag takes precedence over `required`**
   ```go
   // This field is optional (optional wins)
   Field string `json:"field" optional:"true" required:"true"`
   ```

5. **Works with validation tags**
   ```go
   Limit int `query:"limit" default:"10" validate:"min=1,max=100" optional:"true"`
   ```

## Related Tags

- `required:"true"` - Explicitly mark field as required
- `omitempty` (in json/query/header) - Mark field as optional (alternative)
- `default:"value"` - Set default value in schema
- `validate:` - Add validation constraints (works with optional fields)

## See Also

- [Schema Generation Documentation](../../docs/openapi.md)
- [Query Parameter Binding](../../docs/binding.md)
- [Validation](../../docs/validation.md)

