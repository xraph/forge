# CORS Preflight Example

This example demonstrates how CORS preflight requests are automatically handled by the Forge CORS middleware.

## What is CORS Preflight?

When a browser makes a cross-origin request with certain characteristics (custom headers, methods other than GET/POST, etc.), it first sends a "preflight" OPTIONS request to check if the actual request is allowed.

### Preflight Flow

```
1. Browser: OPTIONS /api/users
   Headers:
   - Origin: http://localhost:3000
   - Access-Control-Request-Method: POST
   - Access-Control-Request-Headers: Content-Type, Authorization

2. Server: 204 No Content
   Headers:
   - Access-Control-Allow-Origin: http://localhost:3000
   - Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
   - Access-Control-Allow-Headers: Content-Type, Authorization
   - Access-Control-Max-Age: 3600

3. Browser: POST /api/users (actual request)
   Headers:
   - Origin: http://localhost:3000
   - Content-Type: application/json

4. Server: 201 Created (actual response)
   Headers:
   - Access-Control-Allow-Origin: http://localhost:3000
```

## Running the Example

```bash
go run main.go
```

The server will start on port 8080.

## Testing CORS

### 1. Test Preflight Request

```bash
curl -X OPTIONS http://localhost:8080/api/users \
  -H 'Origin: http://localhost:3000' \
  -H 'Access-Control-Request-Method: GET' \
  -H 'Access-Control-Request-Headers: Content-Type' \
  -v
```

Expected response:
- Status: 204 No Content
- Headers include `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods`, etc.

### 2. Test Actual Request

```bash
curl http://localhost:8080/api/users \
  -H 'Origin: http://localhost:3000' \
  -v
```

Expected response:
- Status: 200 OK
- Headers include `Access-Control-Allow-Origin`
- Body contains user data

### 3. Test with Disallowed Origin

```bash
curl -X OPTIONS http://localhost:8080/api/users \
  -H 'Origin: http://evil.com' \
  -H 'Access-Control-Request-Method: GET' \
  -v
```

Expected response:
- Status: 204 No Content (or 405 depending on implementation)
- No CORS headers (browser will block the request)

### 4. Test POST with Preflight

```bash
# Preflight
curl -X OPTIONS http://localhost:8080/api/users \
  -H 'Origin: http://localhost:3000' \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: Content-Type' \
  -v

# Actual request
curl -X POST http://localhost:8080/api/users \
  -H 'Origin: http://localhost:3000' \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice","email":"alice@example.com"}' \
  -v
```

## Testing with a Frontend

Create a simple HTML file to test from a browser:

```html
<!DOCTYPE html>
<html>
<head>
    <title>CORS Test</title>
</head>
<body>
    <h1>CORS Preflight Test</h1>
    <button onclick="fetchUsers()">Fetch Users</button>
    <button onclick="createUser()">Create User</button>
    <pre id="output"></pre>

    <script>
        const API_URL = 'http://localhost:8080';
        const output = document.getElementById('output');

        async function fetchUsers() {
            try {
                const response = await fetch(`${API_URL}/api/users`, {
                    method: 'GET',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    credentials: 'include', // Include cookies
                });
                const data = await response.json();
                output.textContent = JSON.stringify(data, null, 2);
            } catch (error) {
                output.textContent = `Error: ${error.message}`;
            }
        }

        async function createUser() {
            try {
                const response = await fetch(`${API_URL}/api/users`, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': 'Bearer fake-token', // Triggers preflight
                    },
                    credentials: 'include',
                    body: JSON.stringify({
                        name: 'Alice',
                        email: 'alice@example.com'
                    }),
                });
                const data = await response.json();
                output.textContent = JSON.stringify(data, null, 2);
            } catch (error) {
                output.textContent = `Error: ${error.message}`;
            }
        }
    </script>
</body>
</html>
```

Serve this HTML file on `http://localhost:3000` (or any allowed origin) and test the buttons.

## Key Points

1. **No Explicit OPTIONS Handlers Needed**: The CORS middleware automatically handles OPTIONS preflight requests.

2. **Global Middleware**: CORS middleware must be applied at the router level using `router.Use()` to work correctly.

3. **Origin Validation**: The middleware validates the `Origin` header against the configured `AllowOrigins`.

4. **Method Validation**: The middleware validates the `Access-Control-Request-Method` header against `AllowMethods`.

5. **Header Validation**: The middleware validates the `Access-Control-Request-Headers` header against `AllowHeaders`.

6. **Credentials**: When `AllowCredentials` is true, `AllowOrigins` cannot be `["*"]` - you must specify exact origins.

## Configuration Options

```go
type CORSConfig struct {
    // AllowOrigins: List of allowed origins
    // Use ["*"] for all origins (not recommended for production)
    // Or specific origins: ["https://app.example.com", "https://admin.example.com"]
    AllowOrigins []string

    // AllowMethods: List of allowed HTTP methods
    AllowMethods []string

    // AllowHeaders: List of allowed request headers
    AllowHeaders []string

    // ExposeHeaders: List of headers exposed to the client
    ExposeHeaders []string

    // AllowCredentials: Whether to allow credentials (cookies, auth headers)
    // Cannot be true when AllowOrigins is ["*"]
    AllowCredentials bool

    // MaxAge: How long (in seconds) preflight results can be cached
    MaxAge int
}
```

## Common Issues

### Issue: CORS error even with middleware applied

**Solution**: Make sure CORS middleware is applied BEFORE routes are registered:

```go
// ✅ Correct
router.Use(cors)
router.GET("/api/users", handler)

// ❌ Wrong
router.GET("/api/users", handler)
router.Use(cors) // Too late!
```

### Issue: Credentials not working

**Solution**: Ensure both server and client are configured correctly:

Server:
```go
CORSConfig{
    AllowOrigins: []string{"https://app.example.com"}, // Specific origin
    AllowCredentials: true,
}
```

Client:
```javascript
fetch(url, {
    credentials: 'include', // Important!
})
```

### Issue: Custom headers not allowed

**Solution**: Add custom headers to `AllowHeaders`:

```go
CORSConfig{
    AllowHeaders: []string{
        "Content-Type",
        "Authorization",
        "X-Custom-Header", // Add your custom headers
    },
}
```

## References

- [MDN: CORS](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS)
- [MDN: Preflight Request](https://developer.mozilla.org/en-US/docs/Glossary/Preflight_request)
- [Forge CORS Documentation](../../docs/middleware/cors.md)

