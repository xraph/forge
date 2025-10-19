# Architecture Overview

Forge is built with a modular, plugin-based architecture designed for scalability, maintainability, and extensibility.

## Core Principles

- **Dependency Injection**: All components are managed through a DI container
- **Plugin Architecture**: Extensible through a robust plugin system
- **Configuration-Driven**: Behavior controlled through configuration
- **Observability First**: Built-in metrics, logging, and health checks
- **Production Ready**: Designed for enterprise-scale deployments

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Forge Application                        │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │   Router     │  │   DI        │  │   Config    │        │
│  │  (HTTP/WS)   │  │ Container  │  │  Manager    │        │
│  └─────────────┘  └─────────────┘  └─────────────┘        │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │   Cache     │  │  Database   │  │   Events    │        │
│  │  (Redis)    │  │ (Postgres)  │  │   (NATS)    │        │
│  └─────────────┘  └─────────────┘  └─────────────┘        │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │  Metrics    │  │   Health   │  │   Cron      │        │
│  │(Prometheus) │  │   Checks   │  │ Scheduler   │        │
│  └─────────────┘  └─────────────┘  └─────────────┘        │
├─────────────────────────────────────────────────────────────┤
│                    Plugin System                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │   AI/LLM    │  │  Security   │  │  Custom     │        │
│  │  Plugins    │  │  Plugins    │  │  Plugins    │        │
│  └─────────────┘  └─────────────┘  └─────────────┘        │
└─────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Application Core (`forge.go`)

The main application orchestrator that:
- Manages application lifecycle (start/stop)
- Coordinates component initialization
- Handles graceful shutdown
- Provides access to all services

```go
type ForgeApplication struct {
    name     string
    config   ApplicationConfig
    container *di.Container
    router   router.Router
    // ... other components
}
```

### 2. Dependency Injection Container (`pkg/di/`)

Central service registry and resolver:
- Service registration and resolution
- Lifecycle management (singleton, transient, scoped)
- Circular dependency detection
- Interface-based abstractions

```go
// Register services
container.Register("user-service", &UserService{})
container.Register("email-service", &EmailService{})

// Resolve services
userService := container.Resolve("user-service").(*UserService)
```

### 3. Router System (`pkg/router/`)

HTTP request handling with multiple backends:
- **HTTPRouter**: High-performance routing
- **BunRouter**: Modern, fast routing
- Middleware pipeline
- WebSocket/SSE support

```go
// Route registration
router.GET("/api/users", getUserHandler)
router.POST("/api/users", createUserHandler)
router.Use(middleware.CORS())
```

### 4. Configuration Management (`pkg/config/`)

Multi-source configuration system:
- YAML, JSON, TOML support
- Environment variable override
- Configuration validation
- Hot reloading

```go
config := &ApplicationConfig{
    Name: "my-app",
    Port: 8080,
    Database: &DatabaseConfig{
        Driver: "postgres",
        Host:   "localhost",
    },
}
```

## Service Layer

### Database (`pkg/database/`)

Database abstraction with multiple drivers:
- PostgreSQL, MySQL, SQLite support
- Connection pooling
- Migration management
- Health checks

### Caching (`pkg/cache/`)

Multi-backend caching system:
- Redis, in-memory, distributed caching
- Cache invalidation strategies
- Metrics and monitoring

### Events (`pkg/events/`)

Event-driven architecture:
- Event bus with multiple backends
- Event stores (PostgreSQL, MongoDB)
- Event sourcing support
- Async processing

### Consensus (`pkg/consensus/`)

Distributed consensus using Raft:
- Leader election
- State machine replication
- Cluster management
- Persistent storage

## Advanced Features

### AI Integration (`pkg/ai/`)

AI and ML capabilities:
- Multiple LLM provider support
- Agent coordination
- Training pipeline
- Inference management

### Cron Scheduling (`pkg/cron/`)

Distributed job scheduling:
- Cron expression support
- Distributed execution
- Job persistence
- Failure handling

### Metrics & Observability (`pkg/metrics/`)

Comprehensive monitoring:
- Prometheus metrics
- Custom collectors
- Health checks
- Distributed tracing

## Plugin System (`pkg/plugins/`)

Extensible architecture through plugins:

### Plugin Lifecycle
1. **Discovery**: Auto-discovery of plugins
2. **Loading**: Dynamic loading of plugins
3. **Initialization**: Plugin setup and configuration
4. **Execution**: Plugin runtime
5. **Cleanup**: Graceful shutdown

### Plugin Types
- **Core Plugins**: Essential functionality
- **Security Plugins**: Authentication, authorization
- **AI Plugins**: Machine learning capabilities
- **Custom Plugins**: User-defined functionality

```go
type Plugin interface {
    ID() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Cleanup(ctx context.Context) error
}
```

## Data Flow

### Request Processing
```
HTTP Request → Router → Middleware → Handler → Service → Database
     ↓              ↓         ↓         ↓        ↓
   Metrics      Logging   Auth/ACL  Business   Persistence
```

### Event Processing
```
Event → Event Bus → Handlers → Services → Database
  ↓         ↓          ↓         ↓         ↓
Metrics  Logging   Processing  Business  Storage
```

## Security Architecture

### Authentication & Authorization
- JWT token validation
- OAuth2/OIDC integration
- Role-based access control (RBAC)
- API key management

### Security Middleware
- Rate limiting
- CORS handling
- Security headers
- Input validation

## Deployment Architecture

### Single Instance
```
┌─────────────────┐
│   Forge App     │
│   (All Features)│
└─────────────────┘
```

### Microservices
```
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│   API       │  │   Worker    │  │   Scheduler │
│   Service   │  │   Service   │  │   Service   │
└─────────────┘  └─────────────┘  └─────────────┘
       │               │               │
       └───────────────┼───────────────┘
                       │
              ┌─────────────┐
              │   Shared    │
              │  Services   │
              │ (DB/Cache)  │
              └─────────────┘
```

### Kubernetes Deployment
```
┌─────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                  │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│  │   API       │  │   Worker    │  │   Scheduler │    │
│  │   Pods      │  │    Pods     │  │     Pods    │    │
│  └─────────────┘  └─────────────┘  └─────────────┘    │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│  │ PostgreSQL  │  │    Redis    │  │    NATS     │    │
│  │   Service   │  │   Service   │  │   Service   │    │
│  └─────────────┘  └─────────────┘  └─────────────┘    │
└─────────────────────────────────────────────────────────┘
```

## Performance Considerations

### Optimization Strategies
- **Connection Pooling**: Database and cache connections
- **Caching**: Multi-level caching strategies
- **Async Processing**: Non-blocking operations
- **Resource Management**: Memory and CPU optimization

### Scalability Patterns
- **Horizontal Scaling**: Multiple instances
- **Load Balancing**: Request distribution
- **Database Sharding**: Data partitioning
- **Event-Driven**: Decoupled processing

## Monitoring & Observability

### Metrics Collection
- **Application Metrics**: Request rates, latencies
- **System Metrics**: CPU, memory, disk usage
- **Business Metrics**: Custom application metrics
- **Infrastructure Metrics**: Database, cache performance

### Logging Strategy
- **Structured Logging**: JSON format with context
- **Log Levels**: Debug, Info, Warn, Error, Fatal
- **Correlation IDs**: Request tracing
- **Centralized Logging**: Aggregated log collection

### Health Checks
- **Liveness Probes**: Application is running
- **Readiness Probes**: Application is ready to serve
- **Dependency Checks**: External service health
- **Custom Health Checks**: Application-specific health

## Best Practices

### Development
- **Interface-Based Design**: Loose coupling
- **Dependency Injection**: Testable code
- **Configuration Management**: Environment-specific configs
- **Error Handling**: Explicit error management

### Production
- **Graceful Shutdown**: Clean resource cleanup
- **Circuit Breakers**: Fault tolerance
- **Retry Logic**: Transient failure handling
- **Monitoring**: Comprehensive observability

### Security
- **Input Validation**: Sanitize all inputs
- **Authentication**: Secure user management
- **Authorization**: Principle of least privilege
- **Audit Logging**: Security event tracking

---

This architecture provides a solid foundation for building scalable, maintainable, and production-ready applications with Forge.
