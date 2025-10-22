# OpenAPI Demo

A comprehensive example demonstrating OpenAPI 3.1.0 specification generation with Swagger UI in Forge v2.

## Features

This example showcases:

- **OpenAPI 3.1.0 Specification Generation**: Automatic generation of OpenAPI spec from route definitions
- **Swagger UI Integration**: Interactive API documentation interface
- **Route Metadata**: Tags, summaries, descriptions, and operation IDs
- **Security Schemes**: JWT Bearer and API Key authentication
- **Multiple Server Configurations**: Development and production server URLs
- **Route Grouping**: Logical organization of API endpoints
- **RESTful API Design**: Complete CRUD operations for user management
- **External Documentation Links**: References to additional documentation
- **Contact & License Information**: API metadata and legal information

## What You'll Learn

1. How to configure OpenAPI generation in Forge v2
2. How to add metadata to routes (tags, summaries, descriptions)
3. How to define security schemes (JWT, API Key)
4. How to organize routes with groups
5. How to access the generated OpenAPI spec and Swagger UI
6. Best practices for API documentation

## Running the Example

```bash
cd v2/examples/openapi-demo
go run main.go
```

The server will start on `http://localhost:8080`

## Accessing the Documentation

### Swagger UI (Interactive)
Open your browser and navigate to:
```
http://localhost:8080/api/v1/swagger
```

This provides an interactive interface where you can:
- Browse all available endpoints
- View request/response schemas
- Try out API calls directly from the browser
- See authentication requirements

### OpenAPI Spec (JSON)
Get the raw OpenAPI specification:
```
http://localhost:8080/api/v1/openapi.json
```

This returns the complete OpenAPI 3.1.0 specification in JSON format, which can be:
- Imported into API testing tools (Postman, Insomnia)
- Used for client code generation
- Validated against OpenAPI standards
- Shared with API consumers

## API Endpoints

### Users
- `GET /api/v1/users` - List all users
- `GET /api/v1/users/:id` - Get user by ID
- `POST /api/v1/users` - Create new user
- `PUT /api/v1/users/:id` - Update user
- `DELETE /api/v1/users/:id` - Delete user
- `GET /api/v1/users/search` - Search users

### Admin
- `GET /api/v1/admin/stats` - Get system statistics
- `POST /api/v1/admin/maintenance` - Toggle maintenance mode

### Health
- `GET /api/v1/health` - Health check

## Testing the API

### List Users
```bash
curl http://localhost:8080/api/v1/users
```

### Get User by ID
```bash
curl http://localhost:8080/api/v1/users/1
```

### Create User
```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "email": "alice@example.com",
    "password": "secret123",
    "role": "user",
    "tags": ["new", "premium"]
  }'
```

### Update User
```bash
curl -X PUT http://localhost:8080/api/v1/users/1 \
  -H "Content-Type: application/json" \
  -d '{
    "username": "johndoe_updated",
    "role": "admin"
  }'
```

### Delete User
```bash
curl -X DELETE http://localhost:8080/api/v1/users/2
```

### Search Users
```bash
# Search by query
curl "http://localhost:8080/api/v1/users/search?q=johndoe"

# Search by role
curl "http://localhost:8080/api/v1/users/search?role=admin"
```

### Health Check
```bash
curl http://localhost:8080/api/v1/health
```

### Admin Endpoints
```bash
# Get statistics
curl http://localhost:8080/api/v1/admin/stats

# Toggle maintenance mode
curl -X POST http://localhost:8080/api/v1/admin/maintenance \
  -H "Content-Type: application/json" \
  -d '{"enabled": true, "message": "System maintenance"}'
```

## Configuration Details

### OpenAPI Configuration

The example uses `forge.WithOpenAPI()` to configure OpenAPI generation:

```go
forge.WithOpenAPI(forge.OpenAPIConfig{
    Title:       "User Management API",
    Description: "A comprehensive REST API for user management",
    Version:     "1.0.0",
    
    // Server URLs
    Servers: []forge.OpenAPIServer{
        {
            URL:         "http://localhost:8080",
            Description: "Development server",
        },
    },
    
    // Security schemes
    Security: map[string]forge.SecurityScheme{
        "bearerAuth": {
            Type:         "http",
            Scheme:       "bearer",
            BearerFormat: "JWT",
        },
    },
    
    // UI settings
    UIPath:      "/swagger",
    SpecPath:    "/openapi.json",
    UIEnabled:   true,
    SpecEnabled: true,
    PrettyJSON:  true,
})
```

### Route Options

Each route can be annotated with metadata:

```go
router.GET("/users/:id",
    getUserHandler,
    forge.WithSummary("Get user by ID"),
    forge.WithDescription("Retrieve detailed information about a specific user"),
    forge.WithTags("users"),
    forge.WithOperationID("getUser"),
)
```

### Route Groups

Organize related routes with groups:

```go
adminGroup := router.Group("/admin",
    forge.WithGroupTags("admin"),
    forge.WithGroupMetadata("requiresAuth", true),
)

adminGroup.GET("/stats", statsHandler,
    forge.WithSummary("Get system statistics"),
)
```

## OpenAPI Features Demonstrated

### 1. Basic Information
- API title, description, and version
- Contact information
- License details
- External documentation links

### 2. Server Configuration
- Multiple server URLs (dev, production)
- Server descriptions

### 3. Security Schemes
- JWT Bearer authentication
- API Key authentication
- Scheme descriptions

### 4. Operation Metadata
- Summaries and descriptions
- Tags for grouping
- Operation IDs for unique identification
- Deprecated flag support

### 5. Tags
- Logical grouping of operations
- Tag descriptions
- External documentation per tag

### 6. Paths and Operations
- All HTTP methods (GET, POST, PUT, DELETE)
- Path parameters
- Query parameters
- Request/response schemas

## Best Practices

1. **Comprehensive Descriptions**: Always provide clear summaries and descriptions
2. **Consistent Tagging**: Use tags to logically group related operations
3. **Operation IDs**: Provide unique, descriptive operation IDs
4. **Security Documentation**: Clearly document authentication requirements
5. **Examples**: Include request/response examples when possible
6. **Versioning**: Version your API and document changes
7. **Server URLs**: Provide accurate server URLs for each environment

## Next Steps

- Add request/response schema validation
- Implement security middleware to enforce authentication
- Add more detailed parameter descriptions
- Include request/response examples
- Add error response documentation
- Implement rate limiting documentation
- Add webhook documentation

## Architecture Notes

The example demonstrates a production-ready approach to API documentation:

1. **Separation of Concerns**: Handlers are separate from routing configuration
2. **In-Memory Storage**: Simple user store for demo purposes (use database in production)
3. **Error Handling**: Consistent error response format
4. **RESTful Design**: Follows REST conventions for resource management
5. **Documentation First**: API design is documented as it's built

## Common Issues

### Swagger UI Not Loading
- Ensure `UIEnabled: true` in config
- Check that the server is running on the correct port
- Verify the UI path matches your configuration

### OpenAPI Spec Empty
- Ensure routes are registered before app starts
- Check that `SpecEnabled: true` in config
- Verify route metadata is properly set

### CORS Issues
- Add CORS middleware if accessing from different origin
- Configure allowed origins and headers

## Additional Resources

- [OpenAPI Specification](https://spec.openapis.org/oas/latest.html)
- [Swagger UI Documentation](https://swagger.io/tools/swagger-ui/)
- [Forge v2 Documentation](../../docs/)
- [REST API Best Practices](https://restfulapi.net/)

## License

This example is part of the Forge framework and is available under the same license as the main project.

