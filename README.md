# 🔨 Forge v2

**Enterprise-Grade Web Framework for Go**

> Build scalable, maintainable, and observable Go applications with Forge—the modern framework that brings clean architecture, dependency injection, and powerful extensions to your production services.

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![GitHub Stars](https://img.shields.io/github/stars/xraph/forge)](https://github.com/xraph/forge)
[![CI/CD](https://github.com/xraph/forge/workflows/CI/badge.svg)](https://github.com/xraph/forge/actions)

---

## 🚀 Quick Start

### Installation

```bash
# Install the Forge CLI
go install github.com/xraph/forge/cmd/forge@latest

# Verify installation
forge --version
```

### Create Your First App

```bash
# Initialize a new project
forge init my-app

# Start the development server
forge dev
```

### Minimal Example

```go
package main

import "github.com/xraph/forge"

func main() {
    // Create app with default configuration
    app := forge.NewApp(forge.AppConfig{
        Name:        "my-app",
        Version:     "1.0.0",
        Environment: "development",
        HTTPAddress: ":8080",
    })

    // Register routes
    router := app.Router()
    router.GET("/", func(ctx forge.Context) error {
        return ctx.JSON(200, map[string]string{
            "message": "Hello, Forge v2!",
        })
    })

    // Run the application (blocks until SIGINT/SIGTERM)
    app.Run()
}
```

**Built-in endpoints:**
- `/_/info` - Application information
- `/_/metrics` - Prometheus metrics
- `/_/health` - Health checks

---

## ✨ Key Features

### 🏗️ Core Framework

- **✅ Dependency Injection** - Type-safe container with service lifecycle
- **✅ HTTP Router** - Fast, lightweight routing with middleware support
- **✅ Middleware** - Auth, CORS, logging, rate limiting, and more
- **✅ Configuration** - YAML/JSON/TOML support with environment variable override
- **✅ Observability** - Structured logging, metrics, distributed tracing
- **✅ Health Checks** - Automatic discovery and reporting
- **✅ Lifecycle Management** - Graceful startup and shutdown

### 🔌 Extensions

| Extension | Description | Status |
|-----------|-------------|--------|
| **AI** | LLM integration, agents, inference engine | ✅ |
| **Auth** | Multi-provider authentication (OAuth, JWT, SAML) | ✅ |
| **Cache** | Multi-backend caching (Redis, Memcached, In-Memory) | ✅ |
| **Consensus** | Raft consensus for distributed systems | ✅ |
| **Database** | SQL (Postgres, MySQL, SQLite) + MongoDB support | ✅ |
| **Events** | Event bus and event sourcing | ✅ |
| **GraphQL** | GraphQL server with schema generation | ✅ |
| **gRPC** | gRPC server with reflection | ✅ |
| **HLS** | HTTP Live Streaming | ✅ |
| **Kafka** | Apache Kafka integration | ✅ |
| **MCP** | Model Context Protocol | ✅ |
| **MQTT** | MQTT broker and client | ✅ |
| **orpc** | ORPC transport protocol | ✅ |
| **Queue** | Message queue management | ✅ |
| **Search** | Full-text search (Elasticsearch, Typesense) | ✅ |
| **Storage** | Multi-backend storage (S3, GCS, Local) | ✅ |
| **Streaming** | WebSocket, SSE, WebRTC | ✅ |
| **WebRTC** | Real-time peer-to-peer communication | ✅ |

### 🛠️ CLI Tools

- **✅ Project Scaffolding** - Initialize new projects with templates
- **✅ Code Generation** - Generate handlers, controllers, and services
- **✅ Database Migrations** - Schema management with versioning
- **✅ Interactive Prompts** - Arrow-key navigation, multi-select
- **✅ Server Management** - Development server with hot reload
- **✅ Testing** - Built-in test runner and coverage reports

---

## 📖 Documentation

### Getting Started

- [**Installation Guide**](docs/content/docs/getting-started/installation.mdx)
- [**Quick Start**](docs/content/docs/getting-started/quick-start.mdx)
- [**Architecture**](docs/content/docs/architecture/index.mdx)
- [**Examples**](examples/)

### Core Concepts

- [**Application Lifecycle**](docs/content/docs/core/lifecycle.mdx)
- [**Dependency Injection**](docs/content/docs/core/dependency-injection.mdx)
- [**Routing**](docs/content/docs/core/routing.mdx)
- [**Middleware**](docs/content/docs/core/middleware.mdx)
- [**Configuration**](docs/content/docs/core/configuration.mdx)
- [**Observability**](docs/content/docs/core/observability.mdx)

### Extensions

- [**AI Extension**](extensions/ai/README.md) - LLM integration and AI agents
- [**Auth Extension**](extensions/auth/README.md) - Authentication providers
- [**Database Extension**](extensions/database/) - SQL and NoSQL databases
- [**GraphQL Extension**](extensions/graphql/README.md) - GraphQL server
- [**gRPC Extension**](extensions/grpc/README.md) - gRPC services
- [**Streaming Extension**](extensions/streaming/) - WebSocket and SSE

### CLI Reference

- [**CLI Documentation**](cli/README.md)
- [**Commands Reference**](cmd/forge/COMMANDS.md)

---

## 🌟 Why Forge?

### Production-Ready

Forge is built for production from day one:

- **✅ Graceful Shutdown** - Proper resource cleanup on SIGTERM
- **✅ Health Monitoring** - Automatic discovery and reporting
- **✅ Observability** - Metrics, logging, and distributed tracing
- **✅ Error Handling** - Comprehensive error management
- **✅ Security** - Built-in security best practices

### Developer Experience

- **✅ Type Safety** - Generics and compile-time guarantees
- **✅ Zero Config** - Sensible defaults with full customization
- **✅ Hot Reload** - Instant feedback during development
- **✅ CLI Tools** - Fast project scaffolding and generation
- **✅ Rich Docs** - Comprehensive documentation and examples

### Performance

- **✅ Low Latency** - Optimized HTTP router and middleware
- **✅ Efficient Routing** - Trie-based path matching
- **✅ Concurrent Safe** - Thread-safe components
- **✅ Memory Efficient** - Minimal allocations

### Extensible

- **✅ Extension System** - Modular, composable extensions
- **✅ Plugin Architecture** - Easy to add custom functionality
- **✅ Multi-Backend** - Switch implementations without code changes
- **✅ Middleware Chain** - Powerful middleware composition

---

## 🏛️ Architecture

```go
// Application Structure
app := forge.NewApp(forge.AppConfig{
    Name:        "my-service",
    Version:     "1.0.0",
    Environment: "production",
    
    // Extensions
    Extensions: []forge.Extension{
        database.NewExtension(database.Config{
            Databases: []database.DatabaseConfig{
                {
                    Name: "primary",
                    Type: database.TypePostgres,
                    DSN:  "postgres://localhost/mydb",
                },
            },
        }),
        
        auth.NewExtension(auth.Config{
            Provider: "oauth2",
            // ... auth configuration
        }),
    },
})

// Dependency Injection
forge.RegisterSingleton(app.Container(), "userService", func(c forge.Container) (*UserService, error) {
    db := forge.Must[*bun.DB](c, "db")
    logger := forge.Must[forge.Logger](c, "logger")
    return NewUserService(db, logger), nil
})

// Routing
router := app.Router()
router.GET("/users/:id", getUserHandler)
router.POST("/users", createUserHandler)

// Run
app.Run()
```

---

## 🧩 Extension Example

### Using the AI Extension

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/ai"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Extensions: []forge.Extension{
            ai.NewExtension(ai.Config{
                LLMProviders: map[string]ai.LLMProviderConfig{
                    "openai": {
                        APIKey: os.Getenv("OPENAI_API_KEY"),
                        Model:  "gpt-4",
                    },
                },
            }),
        },
    })

    // Access AI service via DI
    aiService := forge.Must[ai.Service](app.Container(), "ai")

    // Use in your handlers
    router := app.Router()
    router.POST("/chat", func(ctx forge.Context) error {
        result, err := aiService.Chat(ctx, ai.ChatRequest{
            Messages: []ai.Message{
                {Role: "user", Content: "Hello!"},
            },
        })
        if err != nil {
            return err
        }
        return ctx.JSON(200, result)
    })

    app.Run()
}
```

---

## 🛠️ Development

### Prerequisites

- **Go 1.24+** - Latest Go compiler
- **Make** - Build tool (optional but recommended)

### Build

```bash
# Build the CLI
make build

# Build with debug symbols
make build-debug

# Build for all platforms
make release
```

### Run Tests

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run specific package
go test ./extensions/ai/...
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Fix linting issues
make lint-fix

# Run security scan
make security-scan

# Check vulnerabilities
make vuln-check
```

### Development Server

```bash
# Start development server
forge dev

# Start with hot reload
forge dev --watch

# Start on custom port
forge dev --port 3000
```

---

## 📊 Project Status

### Core Framework

- **✅ Dependency Injection** - Production ready
- **✅ HTTP Router** - Fast, lightweight
- **✅ Middleware System** - Comprehensive
- **✅ Configuration** - Multi-format support
- **✅ Observability** - Metrics, logging, tracing
- **✅ Health Checks** - Automatic discovery
- **✅ CLI Tools** - Full-featured CLI

### Extensions (17 total)

**Production Ready (14):**
- ✅ AI - LLM integration and agents
- ✅ Auth - Multi-provider authentication
- ✅ Cache - Multi-backend caching
- ✅ Consensus - Raft consensus
- ✅ Database - SQL and NoSQL
- ✅ Events - Event bus and sourcing
- ✅ GraphQL - GraphQL server
- ✅ gRPC - gRPC services
- ✅ HLS - HTTP Live Streaming
- ✅ Kafka - Apache Kafka
- ✅ MCP - Model Context Protocol
- ✅ MQTT - MQTT broker
- ✅ Storage - Multi-backend storage
- ✅ Streaming - WebSocket, SSE, WebRTC

**In Progress (3):**
- 🔄 Queue - Message queue management
- 🔄 Search - Full-text search
- 🔄 orpc - ORPC transport protocol

---

## 🧪 Examples

The `examples/` directory contains production-ready examples:

- **[Minimal App](examples/minimal-app/)** - Hello World
- **[Configuration](examples/config-example/)** - Config management
- **[Database](examples/database-demo/)** - Database integration
- **[Auth](examples/auth_example/)** - Authentication
- **[GraphQL](examples/graphql-basic/)** - GraphQL server
- **[gRPC](examples/grpc-basic/)** - gRPC services
- **[WebRTC](examples/webrtc/)** - Real-time communication
- **[MCP](examples/mcp-basic/)** - Model Context Protocol
- **[AI Agents](examples/ai-agents-demo/)** - AI agent system

---

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Development Workflow

```bash
# Fork and clone
git clone https://github.com/your-username/forge.git
cd forge

# Install tools
make install-tools

# Make changes
# ...

# Run tests
make test

# Check code quality
make ci

# Commit with conventional commits
git commit -m "feat: add new feature"
```

### Conventional Commits

```bash
feat: add new feature
fix: fix bug in router
docs: update documentation
style: format code
refactor: refactor DI container
perf: optimize routing performance
test: add tests for middleware
chore: update dependencies
```

---

## 📄 License
This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

---

## 🙏 Acknowledgments

Built with ❤️ by [Rex Raphael](https://github.com/juicycleff)

**Special thanks to:**
- [Bun](https://github.com/uptrace/bun) - SQL ORM
- [Uptrace](https://github.com/uptrace/uptrace) - Observability platform
- [Chi](https://github.com/go-chi/chi) - Router inspiration
- All contributors and maintainers

---

## 🔗 Links

- **[Documentation](https://forge.dev)** - Comprehensive docs
- **[GitHub](https://github.com/xraph/forge)** - Source code
- **[Issues](https://github.com/xraph/forge/issues)** - Bug reports
- **[Discussions](https://github.com/xraph/forge/discussions)** - Questions and ideas
- **[Examples](examples/)** - Code examples
- **[CLI Reference](cli/README.md)** - CLI documentation

---

## 📈 Roadmap

### v2.1 (Q1 2025)
- [ ] Complete remaining extensions (Queue, Search, orpc)
- [ ] Enhanced AI agent orchestration
- [ ] Real-time collaboration features
- [ ] Advanced monitoring dashboard

### v2.2 (Q2 2025)
- [ ] Kubernetes operator
- [ ] Helm charts and deployment automation
- [ ] Advanced caching strategies
- [ ] Performance optimization pass

### v3.0 (Q3 2025)
- [ ] TypeScript/Node.js runtime
- [ ] Multi-language code generation
- [ ] Enhanced observability platform
- [ ] Enterprise features (SLA, auditing, compliance)

---

**Ready to build?** [Get Started →](docs/content/docs/getting-started/quick-start.mdx)

