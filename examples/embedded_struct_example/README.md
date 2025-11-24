# Embedded Struct Example

This example demonstrates how embedded/anonymous structs are automatically flattened in Forge's OpenAPI schema generation.

## Features

### Embedded Structs
The example shows how to use embedded structs for code reuse:

```go
type PaginationParams struct {
    Page  int `query:"page" json:"page,omitempty"`
    Limit int `query:"limit" json:"limit,omitempty"`
}

type BaseFilter struct {
    SortBy    string `query:"sort_by" json:"sort_by,omitempty"`
    SortOrder string `query:"sort_order" json:"sort_order,omitempty"`
}

type ListWorkspacesRequest struct {
    PaginationParams  // Embedded - fields are flattened
    BaseFilter        // Embedded - fields are flattened
    Plan string `query:"plan" json:"plan,omitempty"`
}
```

### Automatic Flattening
All query parameters from embedded structs are automatically extracted:
- `page` and `limit` from `PaginationParams`
- `sort_by` and `sort_order` from `BaseFilter`
- `plan` from `ListWorkspacesRequest`

The generated OpenAPI spec will show all 5 parameters at the same level.

## Running the Example

```bash
# Navigate to the example directory
cd examples/embedded_struct_example

# Run the server
go run main.go
```

The server will start on `http://localhost:8080`

## Endpoints

### GET /workspaces
List workspaces with pagination, filtering, and sorting.

**Query Parameters:**
- `page` (integer) - Page number (default: 1)
- `limit` (integer) - Items per page (default: 10)
- `sort_by` (string) - Field to sort by
- `sort_order` (string) - Sort order: asc or desc
- `plan` (string) - Filter by plan: free, pro, or enterprise

**Example Requests:**

```bash
# Basic request
curl 'http://localhost:8080/workspaces'

# With pagination
curl 'http://localhost:8080/workspaces?page=1&limit=10'

# With filtering and sorting
curl 'http://localhost:8080/workspaces?page=1&limit=10&plan=pro&sort_by=name&sort_order=asc'
```

## OpenAPI Documentation

- **OpenAPI Spec**: http://localhost:8080/openapi.json
- **Swagger UI**: http://localhost:8080/swagger

Open the Swagger UI in your browser to see all the query parameters properly documented, including those from embedded structs.

## Key Takeaways

1. **Code Reuse**: Create reusable base types for common patterns (pagination, filtering, sorting)
2. **Automatic Flattening**: Embedded struct fields are automatically flattened in the generated schema
3. **Clean Code**: No need to repeat common fields in every request struct
4. **Type Safety**: All parameters are still strongly typed and validated

## Comparison

### Before (Without Embedding)
```go
type ListWorkspacesRequest struct {
    Page      int    `query:"page" json:"page,omitempty"`
    Limit     int    `query:"limit" json:"limit,omitempty"`
    SortBy    string `query:"sort_by" json:"sort_by,omitempty"`
    SortOrder string `query:"sort_order" json:"sort_order,omitempty"`
    Plan      string `query:"plan" json:"plan,omitempty"`
}
```

### After (With Embedding)
```go
type ListWorkspacesRequest struct {
    PaginationParams
    BaseFilter
    Plan string `query:"plan" json:"plan,omitempty"`
}
```

Both produce the same OpenAPI spec, but the second version is more maintainable and reusable!

