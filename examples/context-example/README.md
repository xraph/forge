# Context Enhancements Example

This example demonstrates the enhanced Forge context API with superior path parameter handling, cookie management, and session integration.

## Features Demonstrated

### 1. Path Parameter Type Conversion
- `ParamInt64` for product IDs
- `ParamFloat64` for prices
- `ParamBool` for boolean flags
- `ParamIntDefault` for pagination with defaults

### 2. Cookie Management
- Reading cookies with `Cookie()`
- Setting secure cookies with `SetCookie()`
- Checking cookie existence with `HasCookie()`
- Deleting cookies with `DeleteCookie()`

### 3. Session Management
- Creating sessions on login
- Loading sessions via middleware
- Storing/retrieving session values
- Updating sessions
- Destroying sessions on logout

## Running the Example

```bash
cd examples/context-example
go run main.go
```

The server will start on `http://localhost:8080`.

## API Endpoints

### Public Endpoints

#### GET /
Get welcome message with theme and authentication status.

**Example:**
```bash
curl http://localhost:8080/
```

**Response:**
```json
{
  "message": "Welcome to Context Example API",
  "theme": "light",
  "username": "",
  "authenticated": false
}
```

#### POST /login
Login and create a session.

**Example:**
```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username": "john", "password": "secret"}' \
  -c cookies.txt
```

**Response:**
```json
{
  "message": "logged in successfully",
  "user": {
    "username": "john",
    "email": "john@example.com"
  }
}
```

#### POST /logout
Logout and destroy session.

**Example:**
```bash
curl -X POST http://localhost:8080/logout \
  -b cookies.txt
```

**Response:**
```json
{
  "message": "logged out successfully"
}
```

#### GET /products
List products with optional filtering and pagination.

**Query Parameters:**
- `page` (int, default: 1)
- `limit` (int, default: 10, max: 100)
- `min_price` (float, default: 0.0)
- `max_price` (float, default: 999999.99)
- `in_stock_only` (bool, default: false)

**Example:**
```bash
# Get all products
curl http://localhost:8080/products

# Get products with pagination
curl "http://localhost:8080/products?page=1&limit=5"

# Filter by price
curl "http://localhost:8080/products?min_price=50&max_price=100"

# Show only in-stock products
curl "http://localhost:8080/products?in_stock_only=true"
```

**Response:**
```json
{
  "products": [
    {
      "id": 1,
      "name": "Laptop",
      "price": 999.99,
      "description": "High-performance laptop",
      "in_stock": true
    },
    {
      "id": 2,
      "name": "Mouse",
      "price": 29.99,
      "description": "Wireless mouse",
      "in_stock": true
    }
  ],
  "page": 1,
  "limit": 10,
  "total": 2
}
```

#### GET /products/:id
Get a single product by ID (demonstrates `ParamInt64`).

**Example:**
```bash
curl http://localhost:8080/products/1
```

**Response:**
```json
{
  "id": 1,
  "name": "Laptop",
  "price": 999.99,
  "description": "High-performance laptop",
  "in_stock": true
}
```

**Error Cases:**
```bash
# Invalid ID format
curl http://localhost:8080/products/abc
# Response: {"error": "invalid product ID", "details": "..."}

# Negative ID
curl http://localhost:8080/products/-1
# Response: {"error": "product ID must be positive"}

# Product not found
curl http://localhost:8080/products/999
# Response: {"error": "product not found"}
```

### Protected Endpoints (Require Authentication)

#### GET /profile
Get current user profile from session.

**Example:**
```bash
curl http://localhost:8080/profile \
  -b cookies.txt
```

**Response:**
```json
{
  "username": "john",
  "email": "john@example.com",
  "roles": ["user"],
  "login_time": "2024-01-15T10:30:00Z",
  "session_id": "abc123...",
  "created_at": "2024-01-15T10:30:00Z",
  "expires_at": "2024-01-16T10:30:00Z"
}
```

#### POST /preferences
Update user preferences in session.

**Example:**
```bash
curl -X POST http://localhost:8080/preferences \
  -H "Content-Type: application/json" \
  -d '{"theme": "dark", "language": "en"}' \
  -b cookies.txt \
  -c cookies.txt
```

**Response:**
```json
{
  "message": "preferences updated"
}
```

#### POST /products/:id/price
Update product price (demonstrates `ParamInt64` and `ParamFloat64`).

**Example:**
```bash
curl -X POST http://localhost:8080/products/1/price \
  -H "Content-Type: application/json" \
  -d '{"price": 899.99}' \
  -b cookies.txt
```

**Response:**
```json
{
  "message": "price updated",
  "old_price": 999.99,
  "new_price": 899.99,
  "product": {
    "id": 1,
    "name": "Laptop",
    "price": 899.99,
    "description": "High-performance laptop",
    "in_stock": true
  }
}
```

#### POST /products/:id/stock
Toggle product stock status (demonstrates `ParamBool`).

**Example:**
```bash
curl -X POST http://localhost:8080/products/3/stock \
  -b cookies.txt
```

**Response:**
```json
{
  "message": "stock status updated",
  "in_stock": true,
  "product": {
    "id": 3,
    "name": "Keyboard",
    "price": 79.99,
    "description": "Mechanical keyboard",
    "in_stock": true
  }
}
```

## Complete Workflow Example

```bash
# 1. Check initial state
curl http://localhost:8080/

# 2. Login
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "admin123"}' \
  -c cookies.txt

# 3. Get profile
curl http://localhost:8080/profile \
  -b cookies.txt

# 4. Update preferences
curl -X POST http://localhost:8080/preferences \
  -H "Content-Type: application/json" \
  -d '{"theme": "dark", "language": "es"}' \
  -b cookies.txt \
  -c cookies.txt

# 5. List products
curl "http://localhost:8080/products?page=1&limit=10"

# 6. Get specific product
curl http://localhost:8080/products/1 \
  -b cookies.txt

# 7. Update product price
curl -X POST http://localhost:8080/products/2/price \
  -H "Content-Type: application/json" \
  -d '{"price": 24.99}' \
  -b cookies.txt

# 8. Toggle product stock
curl -X POST http://localhost:8080/products/3/stock \
  -b cookies.txt

# 9. Logout
curl -X POST http://localhost:8080/logout \
  -b cookies.txt

# 10. Try to access protected endpoint (should fail)
curl http://localhost:8080/profile \
  -b cookies.txt
```

## Key Code Patterns

### Type-Safe Path Parameters

```go
// Before: Manual parsing
idStr := ctx.Param("id")
id, err := strconv.ParseInt(idStr, 10, 64)
if err != nil {
    return ctx.Status(400).JSON(forge.Map{"error": "invalid id"})
}

// After: Type-safe extraction
id, err := ctx.ParamInt64("id")
if err != nil {
    return ctx.Status(400).JSON(forge.Map{"error": "invalid id"})
}
```

### Default Values

```go
// Pagination with sensible defaults
page := ctx.ParamIntDefault("page", 1)
limit := ctx.ParamIntDefault("limit", 10)

// Feature flags
inStockOnly := ctx.ParamBoolDefault("in_stock_only", false)
```

### Cookie Operations

```go
// Read cookie
theme, err := ctx.Cookie("theme")

// Set cookie with secure defaults
ctx.SetCookie("session_id", sessionID, 3600)

// Delete cookie
ctx.DeleteCookie("session_id")
```

### Session Management

```go
// Get session
session, err := ctx.Session()

// Store values
ctx.SetSessionValue("theme", "dark")
ctx.SaveSession()

// Retrieve values
theme, ok := ctx.GetSessionValue("theme")

// Destroy session
ctx.DestroySession()
```

## Testing

```bash
# Run all tests
go test -v

# Test specific functionality
go test -v -run TestParamInt
go test -v -run TestCookie
go test -v -run TestSession
```

## Security Features

1. **Secure Cookie Defaults**
   - HttpOnly: true (prevents XSS)
   - Secure: true (HTTPS only)
   - SameSite: Lax (CSRF protection)

2. **Session Security**
   - Cryptographically secure session IDs
   - Automatic expiration
   - Session validation on each request

3. **Input Validation**
   - Type-safe path parameters
   - Range validation
   - Existence checks

## Performance Considerations

- Path parameter conversion uses efficient standard library functions
- Session loaded once per request via middleware
- Cookie operations are lightweight header manipulations
- Session changes are saved explicitly (not on every access)

## Learn More

- [Context Enhancements Documentation](../../CONTEXT_ENHANCEMENTS.md)
- [Quick Reference Guide](../../CONTEXT_QUICK_REFERENCE.md)
- [Forge Documentation](https://github.com/xraph/forge)

