# Forge Framework - Project Structure & Design

## Project Overview

**Forge** is a comprehensive, opinionated backend framework for Go that provides enterprise-grade features for building scalable, distributed applications. Built upon the high-performance `forgerouter`, Forge extends routing capabilities with a complete ecosystem of tools and services for modern backend development.

## Core Architecture Principles

1. **Modularity**: Each feature is a separate package that can be used independently
2. **Extensibility**: Plugin system for extending functionality with AI agents and custom logic
3. **Performance**: Built for high-performance, low-latency applications
4. **Developer Experience**: Type-safe, well-documented, with excellent tooling
5. **Production Ready**: Health checks, metrics, observability, and monitoring built-in
6. **Distributed First**: Native support for distributed systems patterns

## Project Structure

```
forge/
├── cmd/                              # CLI tools and code generation
│   ├── forge/                        # Main CLI tool
│   ├── forge-gen/                    # Code generation tools
│   └── forge-migrate/                # Database migration tool
├── pkg/                              # Core framework packages
│   ├── core/                         # Core framework functionality
│   ├── router/                       # Enhanced router (built on forgerouter)
│   ├── di/                           # Dependency injection system
│   ├── database/                     # Multi-database support
│   ├── events/                       # Distributed event system
│   ├── cron/                         # Distributed cron jobs
│   ├── streaming/                    # Realtime streaming with rooms/presence
│   ├── consensus/                    # Consensus algorithms (Raft, etc.)
│   ├── middleware/                   # Enhanced middleware system
│   ├── health/                       # Health checks and monitoring
│   ├── metrics/                      # Metrics collection and reporting
│   ├── plugins/                      # Plugin system
│   ├── ai/                           # AI/ML integration
│   ├── config/                       # Configuration management
│   ├── observability/                # Logging, tracing, monitoring
│   ├── security/                     # Security utilities
│   ├── cache/                        # Distributed caching
│   └── testing/                      # Testing utilities
├── examples/                         # Example applications
├── docs/                             # Documentation
├── tools/                            # Development tools
├── integrations/                     # Integration with other frameworks
├── exports.go                        # Export pkgs from root
├── forge_opts.go                     # Forge opts
├── forge.go                          # Forge app
└── internal/                         # Internal packages
```

## Detailed Package Documentation

### 1. Core Framework (`pkg/core/`)

**Purpose**: Foundation of the Forge framework providing application lifecycle, service management, and core abstractions.

**Key Components**:
- `Application`: Main application structure and lifecycle management
- `Service`: Base service interface with lifecycle hooks
- `Context`: Enhanced context with framework-specific utilities
- `Registry`: Service registry for dependency management
- `Lifecycle`: Service startup/shutdown orchestration

**Files**:
```
pkg/core/
├── application.go         # Main application structure
├── service.go            # Service interface and base implementations
├── context.go            # Enhanced context utilities
├── registry.go           # Service registry
├── lifecycle.go          # Lifecycle management
├── errors.go             # Framework-specific error types
└── interfaces.go         # Core interfaces
```

### 2. Enhanced Router (`pkg/router/`)

**Purpose**: Extends forgerouter with Forge-specific features like service integration, enhanced middleware, and plugin support.

**Key Features**:
- Integration with DI container
- Enhanced middleware with service lifecycle
- Plugin-based route extensions
- Service-aware error handling
- Automatic health check endpoints

**Files**:
```
pkg/router/
├── forge_router.go       # Enhanced router wrapper
├── service_handler.go    # Service-aware handlers
├── middleware_integration.go # Middleware-DI integration
├── plugin_routes.go      # Plugin-based routing
├── health_routes.go      # Automatic health endpoints
└── extensions.go         # Router extensions
```

### 3. Dependency Injection (`pkg/di/`)

**Purpose**: Comprehensive dependency injection system with lifecycle management, configuration binding, and service discovery.

**Key Features**:
- Constructor-based injection
- Interface-based service resolution
- Lifecycle hooks (OnStart, OnStop, OnHealthCheck)
- Configuration injection
- Service discovery
- Circular dependency detection

**Files**:
```
pkg/di/
├── container.go          # Main DI container
├── service_definition.go # Service definitions
├── lifecycle_manager.go  # Service lifecycle management
├── resolver.go           # Dependency resolution
├── configurator.go       # Configuration injection
├── discovery.go          # Service discovery
├── validator.go          # Dependency validation
└── annotations.go        # Dependency annotations
```

### 4. Multi-Database Support (`pkg/database/`)

**Purpose**: Unified interface for multiple database systems with connection pooling, migrations, and monitoring.

**Supported Databases**:
- PostgreSQL, MySQL, SQLite (via GORM)
- MongoDB
- Redis
- CockroachDB
- ClickHouse
- Cassandra

**Files**:
```
pkg/database/
├── manager.go            # Database manager
├── connection.go         # Connection management
├── migration.go          # Migration system
├── adapters/             # Database adapters
│   ├── postgres/
│   ├── mysql/
│   ├── mongodb/
│   ├── redis/
│   ├── cockroachdb/
│   ├── clickhouse/
│   └── cassandra/
├── health/               # Database health checks
├── metrics/              # Database metrics
└── testing/              # Database testing utilities
```

### 5. Distributed Events (`pkg/events/`)

**Purpose**: Event-driven architecture with support for multiple message brokers and event sourcing.

**Key Features**:
- Event sourcing
- CQRS pattern support
- Multiple brokers (NATS, Kafka, RabbitMQ, Redis)
- Event replay and projection
- Dead letter queues
- Event schema validation

**Files**:
```
pkg/events/
├── event_bus.go          # Main event bus
├── event_store.go        # Event store implementation
├── projector.go          # Event projections
├── brokers/              # Message broker adapters
│   ├── nats/
│   ├── kafka/
│   ├── rabbitmq/
│   └── redis/
├── sourcing/             # Event sourcing utilities
├── schema/               # Event schema validation
└── replay/               # Event replay system
```

### 6. Distributed Cron Jobs (`pkg/cron/`)

**Purpose**: Distributed job scheduling with leader election, job distribution, and monitoring.

**Key Features**:
- Leader election for job coordination
- Job distribution across nodes
- Retry mechanisms with exponential backoff
- Job monitoring and metrics
- Cron expression parsing
- Job persistence

**Files**:
```
pkg/cron/
├── scheduler.go          # Job scheduler
├── job.go               # Job definition
├── leader_election.go    # Leader election
├── distributor.go        # Job distribution
├── executor.go           # Job execution
├── persistence.go        # Job persistence
├── monitoring.go         # Job monitoring
└── expressions.go        # Cron expression parsing
```

### 7. Realtime Streaming (`pkg/streaming/`)

**Purpose**: Scalable WebSocket and SSE with rooms, presence, and horizontal scaling.

**Key Features**:
- Room-based messaging
- User presence tracking
- Horizontal scaling with Redis
- Message persistence
- Connection management
- Event filtering

**Files**:
```
pkg/streaming/
├── manager.go            # Connection manager
├── room.go              # Room management
├── presence.go           # Presence tracking
├── scaling.go            # Horizontal scaling
├── persistence.go        # Message persistence
├── filters.go            # Event filtering
├── handlers.go           # Message handlers
└── auth.go              # Authentication/authorization
```

### 8. Consensus (`pkg/consensus/`)

**Purpose**: Distributed consensus algorithms for leader election and distributed coordination.

**Key Features**:
- Raft consensus algorithm
- Leader election
- Distributed locks
- Configuration consensus
- Node discovery

**Files**:
```
pkg/consensus/
├── raft/                # Raft implementation
│   ├── node.go
│   ├── leader.go
│   ├── follower.go
│   ├── candidate.go
│   └── log.go
├── leader_election.go    # Leader election utilities
├── distributed_lock.go   # Distributed locking
└── coordinator.go        # Distributed coordination
```

### 9. Enhanced Middleware (`pkg/middleware/`)

**Purpose**: Robust middleware system with service lifecycle integration and performance optimization.

**Key Features**:
- Service-aware middleware
- Lifecycle integration
- Performance monitoring
- Circuit breakers
- Rate limiting
- Authentication/authorization

**Files**:
```
pkg/middleware/
├── service_middleware.go # Service-aware middleware
├── lifecycle.go          # Lifecycle integration
├── circuit_breaker.go    # Circuit breaker pattern
├── rate_limiter.go       # Rate limiting
├── auth.go              # Authentication middleware
├── monitoring.go         # Performance monitoring
├── recovery.go           # Enhanced recovery
└── validator.go          # Request validation
```

### 10. Health Checks (`pkg/health/`)

**Purpose**: Comprehensive health monitoring for all services and dependencies.

**Key Features**:
- Service health checks
- Dependency health monitoring
- Health check aggregation
- Custom health indicators
- Health metrics

**Files**:
```
pkg/health/
├── checker.go           # Health checker
├── registry.go          # Health check registry
├── indicators.go        # Health indicators
├── aggregator.go        # Health aggregation
├── endpoints.go         # Health endpoints
└── metrics.go           # Health metrics
```

### 11. Metrics (`pkg/metrics/`)

**Purpose**: Comprehensive metrics collection and reporting with multiple backend support.

**Key Features**:
- Prometheus metrics
- Custom metrics
- Service metrics
- Performance metrics
- Alerting integration

**Files**:
```
pkg/metrics/
├── collector.go         # Metrics collector
├── registry.go          # Metrics registry
├── prometheus.go        # Prometheus integration
├── custom.go            # Custom metrics
├── service.go           # Service metrics
└── alerting.go          # Alerting integration
```

### 12. Plugin System (`pkg/plugins/`)

**Purpose**: Extensible plugin architecture for AI agents and custom functionality.

**Key Features**:
- Plugin discovery and loading
- AI agent integration
- Custom middleware plugins
- Route plugins
- Service plugins

**Files**:
```
pkg/plugins/
├── manager.go           # Plugin manager
├── loader.go            # Plugin loader
├── registry.go          # Plugin registry
├── ai_agent.go          # AI agent integration
├── middleware_plugin.go # Middleware plugins
├── route_plugin.go      # Route plugins
└── service_plugin.go    # Service plugins
```

### 13. AI Integration (`pkg/ai/`)

**Purpose**: Built-in AI/ML capabilities and agent system integration.

**Key Features**:
- AI agent framework
- ML model serving
- LLM integration
- AI-powered middleware
- Intelligent routing

**Files**:
```
pkg/ai/
├── agent.go             # AI agent framework
├── model_server.go      # ML model serving
├── llm/                 # LLM integrations
│   ├── openai.go
│   ├── anthropic.go
│   └── local.go
├── middleware.go        # AI-powered middleware
├── routing.go           # Intelligent routing
└── inference.go         # Inference utilities
```

### 14. Configuration (`pkg/config/`)

**Purpose**: Flexible configuration management with multiple sources and validation.

**Key Features**:
- Multiple configuration sources
- Environment-based configuration
- Configuration validation
- Hot reload
- Secrets management

**Files**:
```
pkg/config/
├── manager.go           # Configuration manager
├── loader.go            # Configuration loader
├── validator.go         # Configuration validation
├── sources/             # Configuration sources
│   ├── file.go
│   ├── env.go
│   ├── consul.go
│   └── kubernetes.go
├── secrets.go           # Secrets management
└── hot_reload.go        # Hot reload functionality
```

### 15. Observability (`pkg/observability/`)

**Purpose**: Comprehensive observability with logging, tracing, and monitoring.

**Key Features**:
- Structured logging
- Distributed tracing
- Metrics collection
- Error tracking
- Performance monitoring

**Files**:
```
pkg/observability/
├── logger.go            # Structured logging
├── tracer.go            # Distributed tracing
├── metrics.go           # Metrics collection
├── error_tracker.go     # Error tracking
├── performance.go       # Performance monitoring
└── exporters/           # Telemetry exporters
```

### 16. Security (`pkg/security/`)

**Purpose**: Security utilities and middleware for secure applications.

**Key Features**:
- Authentication providers
- Authorization policies
- Security headers
- Input validation
- Rate limiting

**Files**:
```
pkg/security/
├── auth/                # Authentication
├── authz/               # Authorization
├── headers.go           # Security headers
├── validation.go        # Input validation
├── rate_limiting.go     # Rate limiting
└── encryption.go        # Encryption utilities
```

### 17. Caching (`pkg/cache/`)

**Purpose**: Distributed caching with multiple backend support.

**Key Features**:
- Redis caching
- In-memory caching
- Cache invalidation
- Cache patterns
- Performance optimization

**Files**:
```
pkg/cache/
├── manager.go           # Cache manager
├── redis.go             # Redis cache
├── memory.go            # In-memory cache
├── invalidation.go      # Cache invalidation
├── patterns.go          # Cache patterns
└── optimization.go      # Performance optimization
```

### 18. Testing (`pkg/testing/`)

**Purpose**: Testing utilities for Forge applications.

**Key Features**:
- Test server setup
- Mock services
- Integration testing
- Load testing
- Test containers

**Files**:
```
pkg/testing/
├── test_server.go       # Test server utilities
├── mocks.go             # Mock services
├── integration.go       # Integration testing
├── load.go              # Load testing
├── containers.go        # Test containers
└── fixtures.go          # Test fixtures
```

## Framework Integration Points

### 1. Existing Framework Integration (`integrations/`)

**Purpose**: Allow Forge to integrate with existing Go frameworks.

**Supported Frameworks**:
- Gin integration
- Echo integration
- Fiber integration
- Chi integration
- Gorilla Mux integration

**Files**:
```
integrations/
├── gin/
├── echo/
├── fiber/
├── chi/
└── gorilla/
```

### 2. CLI Tools (`cmd/`)

**Purpose**: Command-line tools for development and deployment.

**Tools**:
- `forge`: Main CLI tool for project scaffolding
- `forge-gen`: Code generation for services and models
- `forge-migrate`: Database migration tool

## Key Design Decisions

1. **Built on forgerouter**: Leverages the high-performance routing capabilities
2. **Service-Oriented**: Everything is a service with lifecycle management
3. **Plugin Architecture**: Extensible through plugins for AI agents and custom logic
4. **Type Safety**: Extensive use of Go's type system for safety and performance
5. **Configuration-Driven**: Highly configurable through multiple sources
6. **Observability First**: Built-in monitoring, logging, and tracing
7. **Distributed by Default**: Native support for distributed systems patterns
8. **Developer Experience**: Rich tooling and documentation

## Usage Example

```go
package main

import (
    "github.com/forge-framework/forge/pkg/core"
    "github.com/forge-framework/forge/pkg/router"
    "github.com/forge-framework/forge/pkg/database"
    "github.com/forge-framework/forge/pkg/events"
)

func main() {
    app := core.NewApplication("my-service")
    
    // Configure services
    app.AddService(database.NewPostgresService())
    app.AddService(events.NewNATSService())
    app.AddService(router.NewService())
    
    // Add custom services
    app.AddService(NewUserService())
    
    // Configure plugins
    app.AddPlugin(ai.NewLLMPlugin())
    
    // Start application
    app.Run()
}
```

## Implementation Priority

1. **Phase 1**: Core framework, DI system, enhanced router
2. **Phase 2**: Database support, events, middleware
3. **Phase 3**: Streaming, health checks, metrics
4. **Phase 4**: Cron jobs, consensus, caching
5. **Phase 5**: AI integration, plugin system
6. **Phase 6**: Observability, security, testing utilities
7. **Phase 7**: Framework integrations, CLI tools

This structure provides a solid foundation for building a comprehensive backend framework that can grow and evolve with enterprise needs while maintaining performance and developer experience.