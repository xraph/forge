# Forge Framework

[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)]()

**Forge** is a comprehensive, production-ready backend framework for Go that provides enterprise-grade features out of the box. Built with modern Go practices and designed for scalability, reliability, and developer productivity.

## 🚀 Quick Start

Get up and running with Forge in under 5 minutes:

```bash
# Install Forge CLI
go install github.com/xraph/forge/cmd/forge@latest

# Create a new project
forge new my-app

# Start development server
cd my-app
forge dev
```

## ✨ Features

### 🏗️ **Core Architecture**
- **Dependency Injection**: Powerful DI container with lifecycle management
- **Modular Design**: Plugin-based architecture for extensibility
- **Configuration Management**: Multi-source config with validation
- **Health Checks**: Built-in health monitoring and reporting

### 🌐 **Web & API**
- **Multiple Routers**: HTTPRouter, BunRouter support
- **OpenAPI/AsyncAPI**: Automatic API documentation generation
- **Middleware Pipeline**: Composable middleware system
- **WebSocket/SSE**: Real-time communication support

### 🔧 **Enterprise Features**
- **Consensus & Raft**: Distributed consensus for high availability
- **Event System**: Event-driven architecture with multiple backends
- **Caching**: Redis, in-memory, and distributed caching
- **Cron Scheduling**: Distributed job scheduling
- **Metrics & Observability**: Prometheus metrics, structured logging

### 🤖 **AI Integration**
- **LLM Support**: Multiple AI model providers
- **Agent System**: AI agent coordination and management
- **Training Pipeline**: ML model training and inference

### 🔒 **Security & Auth**
- **Authentication**: JWT, OAuth2, SAML, OIDC support
- **Authorization**: Role-based access control
- **Security Middleware**: Rate limiting, CORS, security headers

## 📖 Documentation

- **[Quick Start Guide](docs/quickstart.md)** - Get started in minutes
- **[Architecture Overview](docs/architecture.md)** - Understand the framework design
- **[API Reference](https://pkg.go.dev/github.com/xraph/forge)** - Complete API documentation
- **[Examples](examples/)** - Real-world usage examples

## 🛠️ Installation

### Prerequisites
- Go 1.24 or higher
- Git

### Install Forge CLI
```bash
go install github.com/xraph/forge/cmd/forge@latest
```

### Create New Project
```bash
forge new my-awesome-app
cd my-awesome-app
```

## 🎯 Hello World Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/pkg/common"
)

func main() {
    // Create application with minimal config
    app, err := forge.NewApplication(forge.ApplicationConfig{
        Name: "hello-forge",
        Port: 8080,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Add a simple route
    app.Router().GET("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello, Forge! 🔥"))
    })
    
    // Start the application
    if err := app.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

## 🏢 Production Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/pkg/common"
)

func main() {
    // Production-ready configuration
    config := forge.ApplicationConfig{
        Name: "production-api",
        Port: 8080,
        
        // Database
        Database: &common.DatabaseConfig{
            Driver: "postgres",
            Host:   "localhost",
            Port:   5432,
            Name:   "myapp",
        },
        
        // Redis Cache
        Cache: &common.CacheConfig{
            Enabled: true,
            Backend: "redis",
            Host:    "localhost:6379",
        },
        
        // Metrics
        Metrics: &common.MetricsConfig{
            Enabled: true,
            Port:    9090,
        },
        
        // Health Checks
        Health: &common.HealthConfig{
            Enabled: true,
            Port:    8081,
        },
    }
    
    app, err := forge.NewApplication(config)
    if err != nil {
        log.Fatal(err)
    }
    
    // Register services
    app.Container().Register("user-service", &UserService{})
    app.Container().Register("email-service", &EmailService{})
    
    // Add routes with middleware
    app.Router().Use(middleware.CORS())
    app.Router().Use(middleware.RateLimit(100))
    
    app.Router().GET("/api/users", getUserHandler)
    app.Router().POST("/api/users", createUserHandler)
    
    // Start with graceful shutdown
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    if err := app.Start(ctx); err != nil {
        log.Fatal(err)
    }
}
```

## 🧩 Plugin System

Forge's plugin system allows you to extend functionality:

```go
// Custom plugin
type MyPlugin struct {
    logger common.Logger
}

func (p *MyPlugin) ID() string {
    return "my-plugin"
}

func (p *MyPlugin) Start(ctx context.Context) error {
    p.logger.Info("My plugin started!")
    return nil
}

func (p *MyPlugin) Stop(ctx context.Context) error {
    p.logger.Info("My plugin stopped!")
    return nil
}

// Register plugin
app.PluginManager().Register(&MyPlugin{})
```

## 🔧 Configuration

Forge supports multiple configuration sources:

```yaml
# config.yaml
app:
  name: "my-app"
  port: 8080
  env: "production"

database:
  driver: "postgres"
  host: "localhost"
  port: 5432
  name: "myapp"
  ssl_mode: "require"

cache:
  enabled: true
  backend: "redis"
  host: "localhost:6379"

metrics:
  enabled: true
  port: 9090
```

## 🚀 Deployment

### Docker
```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o forge ./cmd/forge

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/forge .
CMD ["./forge"]
```

### Kubernetes
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: forge-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: forge-app
  template:
    metadata:
      labels:
        app: forge-app
    spec:
      containers:
      - name: forge-app
        image: my-registry/forge-app:latest
        ports:
        - containerPort: 8080
        env:
        - name: DATABASE_URL
          value: "postgres://user:pass@db:5432/myapp"
```

## 📊 Monitoring

Forge includes comprehensive monitoring:

- **Health Checks**: `/health` endpoint with detailed status
- **Metrics**: Prometheus metrics at `/metrics`
- **Logging**: Structured JSON logging
- **Tracing**: OpenTelemetry distributed tracing

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup
```bash
git clone https://github.com/xraph/forge.git
cd forge
go mod download
make test
make build
```

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🆘 Support

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/xraph/forge/issues)
- **Discussions**: [GitHub Discussions](https://github.com/xraph/forge/discussions)
- **Discord**: [Join our community](https://discord.gg/forge)

## 🗺️ Roadmap

- [ ] **v1.1**: Enhanced AI integration
- [ ] **v1.2**: GraphQL support
- [ ] **v1.3**: Advanced caching strategies
- [ ] **v2.0**: Microservices orchestration

---

**Built with ❤️ by the Forge team**

[![GitHub stars](https://img.shields.io/github/stars/xraph/forge?style=social)](https://github.com/xraph/forge)
[![Twitter Follow](https://img.shields.io/twitter/follow/xraph?style=social)](https://twitter.com/xraph)
