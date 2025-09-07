# Forge Framework Implementation Prompt

## Project Context

You are implementing **Forge**, a comprehensive, opinionated backend framework for Go that provides enterprise-grade features for building scalable, distributed applications. Forge is built upon the high-performance `forgerouter` (provided in context) and extends it with a complete ecosystem of tools and services.

## Core Requirements

### Primary Goals
1. **Multi-database support**: Unified interface for PostgreSQL, MySQL, MongoDB, Redis, etc.
2. **Distributed events**: Event-driven architecture with multiple message brokers
3. **Distributed cron jobs**: Job scheduling with leader election and distribution
4. **Scalable realtime streaming**: WebSocket/SSE with rooms and presence
5. **Consensus support**: Raft algorithm for distributed coordination
6. **Dependency injection**: Comprehensive DI system with lifecycle management
7. **Health checks & metrics**: Automatic monitoring for all services
8. **Robust middleware**: Service-aware middleware with lifecycle integration
9. **Plugin system**: Extensible architecture for AI agents and custom logic
10. **Framework integration**: Ability to integrate with existing Go frameworks

### Technical Specifications

#### Architecture Principles
- **Modularity**: Each feature is an independent package
- **Performance**: Built for high-performance, low-latency applications
- **Type Safety**: Extensive use of Go's type system
- **Service-Oriented**: Everything is a service with lifecycle management
- **Configuration-Driven**: Highly configurable through multiple sources
- **Observability First**: Built-in monitoring, logging, and tracing

#### Core Interfaces

```go
// Service interface - all services must implement this
type Service interface {
    Name() string
    Dependencies() []string
    OnStart(ctx context.Context) error
    OnStop(ctx context.Context) error
    OnHealthCheck(ctx context.Context) error
}

// Plugin interface - for extensibility
type Plugin interface {
    Name() string
    Version() string
    Initialize(container *di.Container) error
    Middleware() []middleware.Middleware
    Routes() []router.Route
}

// Middleware interface with lifecycle awareness
type Middleware interface {
    Name() string
    Priority() int
    Handle(ctx *MiddlewareContext) error
    OnServiceStart(service Service) error
    OnServiceStop(service Service) error
}
```

## Implementation Guidelines

### 1. Project Structure
Follow the exact structure defined in the project design document:
```
forge/
├── pkg/core/           # Application lifecycle, service management
├── pkg/router/         # Enhanced forgerouter integration
├── pkg/di/             # Dependency injection system
├── pkg/database/       # Multi-database support
├── pkg/events/         # Distributed event system
├── pkg/cron/           # Distributed cron jobs
├── pkg/streaming/      # Realtime streaming with rooms/presence
├── pkg/consensus/      # Consensus algorithms
├── pkg/middleware/     # Enhanced middleware system
├── pkg/health/         # Health checks and monitoring
├── pkg/metrics/        # Metrics collection
├── pkg/plugins/        # Plugin system
├── pkg/ai/             # AI/ML integration
├── pkg/config/         # Configuration management
├── pkg/observability/  # Logging, tracing, monitoring
├── pkg/security/       # Security utilities
├── pkg/cache/          # Distributed caching
└── pkg/testing/        # Testing utilities
```

### 2. Core Framework Implementation (`pkg/core/`)

**Key Components to Implement:**

```go
// Application - Main application structure
type Application struct {
    name       string
    container  *di.Container
    router     *router.ForgeRouter
    services   map[string]Service
    plugins    []Plugin
    config     *config.Manager
    lifecycle  *LifecycleManager
    logger     *observability.Logger
}

// Service lifecycle management
type LifecycleManager struct {
    services    []Service
    graph       *dependency.Graph
    healthCheck *health.Checker
    metrics     *metrics.Collector
}

// Enhanced context with framework utilities
type ForgeContext struct {
    context.Context
    container *di.Container
    logger    *observability.Logger
    metrics   *metrics.Collector
    tracer    *observability.Tracer
}
```

### 3. Dependency Injection (`pkg/di/`)

**Implementation Requirements:**

```go
// Container - Main DI container
type Container struct {
    services     map[reflect.Type]ServiceDefinition
    instances    map[reflect.Type]interface{}
    lifecycle    *LifecycleManager
    configurator *Configurator
    resolver     *Resolver
}

// ServiceDefinition - Service registration
type ServiceDefinition struct {
    Type         reflect.Type
    Constructor  interface{}
    Singleton    bool
    Lifecycle    LifecycleHooks
    Config       interface{}
    Dependencies []reflect.Type
}

// LifecycleHooks - Service lifecycle hooks
type LifecycleHooks struct {
    OnStart      func(ctx context.Context, service interface{}) error
    OnStop       func(ctx context.Context, service interface{}) error
    OnHealthCheck func(ctx context.Context, service interface{}) error
}
```

### 4. Enhanced Router (`pkg/router/`)

**Build upon forgerouter with these enhancements:**

```go
// ForgeRouter - Enhanced router with service integration
type ForgeRouter struct {
    *forgerouter.FastRouter
    container    *di.Container
    middleware   *middleware.Manager
    plugins      []Plugin
    healthCheck  *health.Checker
    metrics      *metrics.Collector
}

// ServiceHandler - Service-aware handler
type ServiceHandler[T Service, Req any, Res any] func(
    ctx *ForgeContext, 
    service T, 
    req Req,
) (*Res, error)

// Plugin-based routing
type RoutePlugin interface {
    Routes() []RouteDefinition
    Middleware() []middleware.Middleware
}
```

### 5. Database Support (`pkg/database/`)

**Multi-database implementation:**

```go
// DatabaseManager - Unified database interface
type DatabaseManager struct {
    connections map[string]Connection
    health      *health.Checker
    metrics     *metrics.Collector
    migrations  *MigrationManager
}

// Connection - Database connection interface
type Connection interface {
    Name() string
    Type() string
    Connect(ctx context.Context) error
    Close(ctx context.Context) error
    HealthCheck(ctx context.Context) error
    Migrate(ctx context.Context) error
}

// Adapters for different databases
type PostgresAdapter struct {
    config *PostgresConfig
    db     *gorm.DB
}

type MongoAdapter struct {
    config *MongoConfig
    client *mongo.Client
}
```

### 6. Event System (`pkg/events/`)

**Distributed event implementation:**

```go
// EventBus - Main event bus
type EventBus struct {
    brokers    map[string]Broker
    store      EventStore
    projectors map[string]Projector
    schemas    *SchemaRegistry
}

// Event - Event structure
type Event struct {
    ID          string
    AggregateID string
    Type        string
    Data        interface{}
    Metadata    map[string]interface{}
    Timestamp   time.Time
    Version     int
}

// Broker interface for different message brokers
type Broker interface {
    Publish(ctx context.Context, event Event) error
    Subscribe(ctx context.Context, handler EventHandler) error
    Close(ctx context.Context) error
}
```

### 7. Streaming (`pkg/streaming/`)

**Realtime streaming with rooms and presence:**

```go
// StreamingManager - WebSocket/SSE manager
type StreamingManager struct {
    rooms       map[string]*Room
    presence    *PresenceTracker
    scaling     *HorizontalScaling
    persistence *MessagePersistence
}

// Room - Message room
type Room struct {
    ID          string
    connections map[string]*Connection
    messages    chan Message
    filters     []EventFilter
}

// PresenceTracker - User presence tracking
type PresenceTracker struct {
    users    map[string]*UserPresence
    rooms    map[string]map[string]*UserPresence
    redis    *redis.Client
}
```

### 8. Consensus (`pkg/consensus/`)

**Raft consensus implementation:**

```go
// RaftNode - Raft consensus node
type RaftNode struct {
    id       string
    state    NodeState
    log      *Log
    leader   string
    peers    []string
    election *LeaderElection
}

// LeaderElection - Distributed leader election
type LeaderElection struct {
    node     *RaftNode
    timeout  time.Duration
    majority int
}
```

## Code Standards and Patterns

### 1. Error Handling
- Use structured errors with context
- Implement custom error types for different scenarios
- Always include error wrapping for better debugging

```go
type ForgeError struct {
    Code    string
    Message string
    Cause   error
    Context map[string]interface{}
}

func (e *ForgeError) Error() string {
    return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
}
```

### 2. Configuration
- Use struct tags for configuration binding
- Support multiple configuration sources
- Implement configuration validation

```go
type DatabaseConfig struct {
    Host     string `yaml:"host" env:"DB_HOST" validate:"required"`
    Port     int    `yaml:"port" env:"DB_PORT" default:"5432"`
    Database string `yaml:"database" env:"DB_NAME" validate:"required"`
    Username string `yaml:"username" env:"DB_USER" validate:"required"`
    Password string `yaml:"password" env:"DB_PASSWORD" validate:"required"`
}
```

### 3. Logging
- Use structured logging with context
- Include request tracing information
- Support multiple log levels and formats

```go
type Logger interface {
    Debug(ctx context.Context, msg string, fields ...Field)
    Info(ctx context.Context, msg string, fields ...Field)
    Warn(ctx context.Context, msg string, fields ...Field)
    Error(ctx context.Context, msg string, fields ...Field)
}
```

### 4. Testing
- Write comprehensive unit tests
- Implement integration tests for external dependencies
- Use test containers for database testing
- Implement benchmarks for performance-critical code

```go
func TestDatabaseService(t *testing.T) {
    container := testcontainers.NewPostgresContainer()
    defer container.Terminate()
    
    service := database.NewPostgresService(container.Config())
    assert.NoError(t, service.OnStart(context.Background()))
}
```

## Integration Requirements

### 1. Forgerouter Integration
- Extend forgerouter without breaking existing functionality
- Add service injection to handlers
- Integrate with middleware system
- Maintain OpenAPI documentation generation

### 2. Existing Framework Integration
- Create adapters for Gin, Echo, Fiber, Chi
- Maintain framework-specific features
- Provide migration utilities
- Support gradual adoption

### 3. External Service Integration
- Support for cloud providers (AWS, GCP, Azure)
- Integration with service meshes (Istio, Linkerd)
- Support for container orchestration (Kubernetes, Docker Swarm)

## Performance Requirements

### 1. Benchmarks
- Sub-millisecond request handling
- Minimal memory allocations
- Efficient connection pooling
- Optimized serialization/deserialization

### 2. Scalability
- Horizontal scaling support
- Load balancing integration
- Circuit breaker patterns
- Graceful degradation

### 3. Resource Management
- Connection pooling for all external services
- Memory pool reuse
- Goroutine pool management
- Efficient cleanup on shutdown

## Security Requirements

### 1. Authentication/Authorization
- JWT token validation
- OAuth2 integration
- Role-based access control
- API key authentication

### 2. Security Headers
- CORS configuration
- Security headers middleware
- Rate limiting
- Input validation

### 3. Data Protection
- Encryption at rest and in transit
- Secure configuration management
- Audit logging
- Secrets management

## Documentation Requirements

### 1. Code Documentation
- Comprehensive GoDoc comments
- Usage examples for all public APIs
- Architecture decision records (ADRs)
- API documentation generation

### 2. User Documentation
- Getting started guide
- Configuration reference
- Best practices guide
- Migration guides

### 3. Examples
- Complete example applications
- Integration examples
- Performance benchmarks
- Testing examples

## Implementation Priority

1. **Phase 1**: Core framework, DI system, enhanced router
2. **Phase 2**: Database support, configuration, basic middleware
3. **Phase 3**: Event system, health checks, metrics
4. **Phase 4**: Streaming, caching, security
5. **Phase 5**: Cron jobs, consensus, observability
6. **Phase 6**: AI integration, plugin system
7. **Phase 7**: Framework integrations, CLI tools

## Development Workflow

1. **Setup**: Initialize Go module and project structure
2. **Core Implementation**: Start with core framework and DI
3. **Testing**: Write tests for each component
4. **Integration**: Integrate with forgerouter
5. **Documentation**: Add comprehensive documentation
6. **Examples**: Create example applications
7. **Optimization**: Performance tuning and optimization

## Quality Assurance

### 1. Testing Strategy
- Unit tests with >90% coverage
- Integration tests for external dependencies
- End-to-end tests for complete workflows
- Performance benchmarks

### 2. Code Quality
- Go vet and golint compliance
- Consistent code formatting with gofmt
- Security scanning with gosec
- Dependency vulnerability scanning

### 3. CI/CD
- Automated testing pipeline
- Code coverage reporting
- Performance regression testing
- Security scanning

This prompt provides comprehensive guidance for implementing the Forge framework. Focus on building a robust, performant, and well-documented framework that can serve as the foundation for enterprise-grade applications.