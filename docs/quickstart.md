# Quick Start Guide

Get up and running with Forge in under 5 minutes!

## Prerequisites

- Go 1.24 or higher
- Git
- (Optional) Docker for containerized examples

## Installation

### Install Forge CLI

```bash
go install github.com/xraph/forge/cmd/forge@latest
```

### Verify Installation

```bash
forge --version
```

## Create Your First App

### 1. Generate New Project

```bash
forge new hello-forge
cd hello-forge
```

This creates a new project with:
- Basic project structure
- Example configuration
- Sample routes and handlers
- Docker setup

### 2. Explore the Generated Code

```bash
tree hello-forge/
hello-forge/
├── main.go              # Application entry point
├── config.yaml         # Configuration file
├── Dockerfile          # Container setup
├── go.mod             # Go dependencies
└── handlers/          # Request handlers
    └── hello.go
```

### 3. Run the Application

```bash
# Development mode with hot reload
forge dev

# Or run directly
go run main.go
```

Visit `http://localhost:8080` to see your app!

## Basic Example

Here's a minimal Forge application:

```go
package main

import (
    "context"
    "log"
    "net/http"
    
    "github.com/xraph/forge"
)

func main() {
    // Create application
    app, err := forge.NewApplication(forge.ApplicationConfig{
        Name: "hello-forge",
        Port: 8080,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Add routes
    app.Router().GET("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello, Forge! 🔥"))
    })
    
    app.Router().GET("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })
    
    // Start application
    ctx := context.Background()
    if err := app.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

## Adding Features

### Database Integration

```go
config := forge.ApplicationConfig{
    Name: "my-app",
    Port: 8080,
    Database: &common.DatabaseConfig{
        Driver: "postgres",
        Host:   "localhost",
        Port:   5432,
        Name:   "myapp",
    },
}
```

### Caching

```go
config := forge.ApplicationConfig{
    Name: "my-app",
    Port: 8080,
    Cache: &common.CacheConfig{
        Enabled: true,
        Backend: "redis",
        Host:    "localhost:6379",
    },
}
```

### Metrics & Health Checks

```go
config := forge.ApplicationConfig{
    Name: "my-app",
    Port: 8080,
    Metrics: &common.MetricsConfig{
        Enabled: true,
        Port:    9090,
    },
    Health: &common.HealthConfig{
        Enabled: true,
        Port:    8081,
    },
}
```

## Configuration

### YAML Configuration

Create `config.yaml`:

```yaml
app:
  name: "my-app"
  port: 8080
  env: "development"

database:
  driver: "postgres"
  host: "localhost"
  port: 5432
  name: "myapp"

cache:
  enabled: true
  backend: "redis"
  host: "localhost:6379"

metrics:
  enabled: true
  port: 9090
```

### Environment Variables

```bash
export FORGE_DATABASE_HOST=localhost
export FORGE_DATABASE_PORT=5432
export FORGE_CACHE_ENABLED=true
```

## Common Patterns

### Dependency Injection

```go
// Register services
app.Container().Register("user-service", &UserService{})
app.Container().Register("email-service", &EmailService{})

// Resolve services
userService := app.Container().Resolve("user-service").(*UserService)
```

### Middleware

```go
// Global middleware
app.Router().Use(middleware.CORS())
app.Router().Use(middleware.RateLimit(100))

// Route-specific middleware
app.Router().GET("/api/users", 
    middleware.Auth(),
    middleware.Logging(),
    getUserHandler,
)
```

### Error Handling

```go
func getUserHandler(w http.ResponseWriter, r *http.Request) {
    user, err := userService.GetUser(r.URL.Query().Get("id"))
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }
    
    json.NewEncoder(w).Encode(user)
}
```

## Next Steps

1. **Explore Examples**: Check out the [examples/](examples/) directory
2. **Read Documentation**: See [Architecture Overview](architecture.md)
3. **API Reference**: Browse [GoDoc](https://pkg.go.dev/github.com/xraph/forge)
4. **Join Community**: [Discord](https://discord.gg/forge) or [GitHub Discussions](https://github.com/xraph/forge/discussions)

## Troubleshooting

### Common Issues

**Port already in use:**
```bash
# Check what's using the port
lsof -i :8080

# Use a different port
forge dev --port 8081
```

**Database connection failed:**
```bash
# Check database is running
docker ps | grep postgres

# Start database
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=password postgres
```

**Module not found:**
```bash
# Download dependencies
go mod download

# Tidy modules
go mod tidy
```

### Getting Help

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/xraph/forge/issues)
- **Discord**: [Join our community](https://discord.gg/forge)

---

**Ready to build something amazing? Let's go! 🚀**
