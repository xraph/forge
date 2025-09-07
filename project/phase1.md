# Forge Framework - Phase 1 Implementation

**Status: ✅ Complete**

Phase 1 of the Forge framework provides the core foundation with dependency injection, enhanced routing, and plugin architecture.

## 🏗️ What's Implemented

### Core Framework (`pkg/core/`)
- **Interfaces**: Complete set of interfaces for all framework components
- **Errors**: Structured error handling with context and stack traces
- **Context**: Enhanced `ForgeContext` with DI integration and utilities
- **Application**: Application lifecycle management with service orchestration

### Dependency Injection (`pkg/di/`)
- **Container**: Full DI container with constructor injection and lifecycle management
- **Lifecycle Manager**: Service dependency resolution and startup/shutdown ordering
- **Resolver**: Dependency resolution with singleton caching
- **Configurator**: Service configuration binding
- **Validator**: Dependency validation and circular dependency detection

### Enhanced Router (`pkg/router/`)
- **ForgeRouter**: Extension of forgerouter with service integration
- **Middleware Manager**: Priority-based middleware system with instrumentation
- **Plugin Manager**: Plugin lifecycle management with dependency resolution
- **Service Handlers**: Type-safe service-aware request handlers

## 🚀 Key Features

### 1. Service-Oriented Architecture
All components are services with well-defined lifecycles:
```go
type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, user *User) error
}

// Register service using interface directly
container.Register(core.ServiceDefinition{
    Name:        "user-service",
    Type:        (*UserService)(nil),
    Constructor: NewUserService,
    Singleton:   true,
})
```

### 2. Type-Safe Service Handlers
Handlers automatically inject services and bind parameters:
```go
// Handler signature: func(*core.ForgeContext, ServiceType, RequestType) (*ResponseType, error)
router.GET("/users/:id", func(
    ctx *core.ForgeContext, 
    userService UserService, 
    req GetUserRequest,
) (*User, error) {
    return userService.GetUser(ctx, req.ID)
})
```

### 3. Dependency Injection with Lifecycle
- Constructor-based injection with reflection
- Circular dependency detection
- Ordered startup/shutdown based on dependencies
- Health checks for all services

### 4. Plugin Architecture
Extensible plugin system with middleware and routes:
```go
type Plugin interface {
    Name() string
    Version() string
    Initialize(container Container) error
    Middleware() []MiddlewareDefinition
    Routes() []RouteDefinition
    OnStart(ctx context.Context) error
    OnStop(ctx context.Context) error
}
```

### 5. Comprehensive Error Handling
Structured errors with context and stack traces:
```go
type ForgeError struct {
    Code      string
    Message   string
    Cause     error
    Context   map[string]interface{}
    Timestamp time.Time
    Stack     string
}
```

### 6. Instrumentation & Observability
- Built-in metrics collection
- Request/response instrumentation
- Health checks for all components
- Performance monitoring

## 📋 Usage Examples

### Basic Application Setup
```go
// Create core components
logger := &simpleLogger{}
metrics := &simpleMetrics{}
config := newSimpleConfig()

// Create DI container
container := di.NewContainer(di.ContainerConfig{
    Logger:  logger,
    Metrics: metrics,
    Config:  config,
})

// Register services using interface directly
container.Register(core.ServiceDefinition{
    Name:        "user-service",
    Type:        (*UserService)(nil),
    Constructor: NewUserService,
    Singleton:   true,
})

// Create router
router := router.NewForgeRouter(router.ForgeRouterConfig{
    Container: container,
    Logger:    logger,
    Metrics:   metrics,
    Config:    config,
})

// Register handlers
router.GET("/users/:id", getUserHandler)
router.POST("/users", createUserHandler)
```

### Service Definition
```go
type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, user *User) error
}

type userService struct {
    logger core.Logger
    db     Database // Injected dependency
}

func NewUserService(logger core.Logger, db Database) UserService {
    return &userService{
        logger: logger,
        db:     db,
    }
}
```

### Plugin Development
```go
type MyPlugin struct {
    logger core.Logger
}

func (p *MyPlugin) Name() string { return "my-plugin" }
func (p *MyPlugin) Version() string { return "1.0.0" }

func (p *MyPlugin) Initialize(container core.Container) error {
    // Plugin initialization
    return nil
}

func (p *MyPlugin) Middleware() []router.MiddlewareDefinition {
    return []router.MiddlewareDefinition{
        {
            Name:     "my-middleware",
            Priority: 50,
            Handler:  myMiddlewareHandler,
        },
    }
}

func (p *MyPlugin) Routes() []core.RouteDefinition {
    return []core.RouteDefinition{
        {
            Method:  "GET",
            Pattern: "/plugin/status",
            Handler: p.statusHandler,
        },
    }
}
```

## 🧪 Testing

### Running the Example
```bash
cd examples/basic_usage
go run main.go
```

### Available Endpoints
- `GET /health` - Health check
- `GET /metrics` - Application metrics
- `POST /users` - Create user
- `GET /users` - List users
- `GET /users/:id` - Get user by ID
- `GET /plugin/info` - Plugin information

### Example Request
```bash
# Create a user
curl -X POST http://localhost:8080/users \
  -H "Content-Type: application/json" \
  -d '{"name": "John Doe", "email": "john@example.com"}'

# Get user
curl http://localhost:8080/users/user-1234567890
```

## 🔧 Configuration

### Router Configuration
```go
router := router.NewForgeRouter(router.ForgeRouterConfig{
    Container:     container,
    Logger:        logger,
    Metrics:       metrics,
    Config:        config,
    HealthChecker: healthChecker,
    RouterOptions: forgerouter.RouterOptions{
        OpenAPITitle:       "My API",
        OpenAPIVersion:     "1.0.0",
        OpenAPIDescription: "API built with Forge",
    },
})
```

### Middleware Configuration
```go
// Add logging middleware
router.AddMiddleware(router.LoggingMiddleware(logger))

// Add recovery middleware
router.AddMiddleware(router.RecoveryMiddleware(logger))

// Add CORS middleware
router.AddMiddleware(router.CORSMiddleware(router.CORSConfig{
    AllowedOrigins: []string{"*"},
    AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
    AllowedHeaders: []string{"Content-Type", "Authorization"},
}))
```

## 📊 Metrics & Monitoring

### Built-in Metrics
- `forge.di.services_registered` - Number of services registered
- `forge.di.services_started` - Number of services started
- `forge.router.handlers_registered` - Number of handlers registered
- `forge.router.requests_success` - Successful requests
- `forge.router.requests_error` - Failed requests
- `forge.router.request_duration` - Request duration histogram

### Health Checks
- Application health: `/health`
- Container health: DI container status
- Service health: Individual service health checks
- Router health: Router component status

## 🔍 Debugging & Observability

### Logging
All components provide structured logging:
```go
logger.Info(ctx, "user created", 
    core.String("user_id", user.ID),
    core.String("email", user.Email),
    core.Duration("processing_time", time.Since(start)),
)
```

### Error Context
Errors include rich context:
```go
err := core.ErrServiceNotFound("user-service").
    WithContext("user_id", userID).
    WithContext("operation", "get_user").
    WithStack()
```

### Statistics
Get detailed statistics:
```go
// Container stats
stats := container.GetStats()

// Router stats
routerStats := router.GetStats()

// Plugin stats
pluginStats := router.GetPluginStats("my-plugin")
```

## 🧩 Architecture Highlights

### Dependency Management
- **Constructor Injection**: Services are created using constructor functions
- **Dependency Resolution**: Automatic resolution of service dependencies
- **Lifecycle Management**: Ordered startup/shutdown based on dependencies
- **Circular Detection**: Prevents circular dependency issues

### Service Handlers
- **Type Safety**: Compile-time type checking for handlers
- **Automatic Injection**: Services automatically injected into handlers
- **Parameter Binding**: Request parameters automatically bound to structs
- **Error Handling**: Structured error responses

### Plugin System
- **Isolation**: Plugins run in isolated contexts
- **Dependencies**: Plugins can depend on other plugins
- **Extensibility**: Plugins can add middleware and routes
- **Lifecycle**: Full lifecycle management for plugins

## 🚦 Status & Next Steps

### ✅ Completed
- Core framework foundation
- Dependency injection system
- Enhanced router with service integration
- Plugin architecture
- Comprehensive error handling
- Basic observability

### 🔄 Phase 2 Planning
- Database support (PostgreSQL, MongoDB, Redis)
- Event system (NATS, Kafka)
- Configuration management
- Enhanced middleware system
- Basic caching

### 🎯 Usage Recommendations
1. Start with the basic example to understand the architecture
2. Define your services as interfaces
3. Use constructor injection for dependencies
4. Leverage plugins for extensibility
5. Add comprehensive error handling
6. Monitor with built-in metrics

## 🤝 Contributing

Phase 1 provides a solid foundation for the Forge framework. The architecture is designed for:
- **Extensibility**: Easy to add new features
- **Testability**: All components are easily testable
- **Performance**: Minimal overhead with efficient dependency injection
- **Maintainability**: Clear separation of concerns

Ready to build enterprise-grade applications with Forge! 🚀