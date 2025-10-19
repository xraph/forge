# Core vs Extensions Architecture

## Core Framework Components

### Essential Components (Always Included)
- **DI Container** (`pkg/di/`) - Dependency injection and service management
- **Router** (`pkg/router/`) - HTTP routing and middleware
- **Logger** (`pkg/logger/`) - Structured logging
- **Config** (`pkg/config/`) - Configuration management
- **Common** (`pkg/common/`) - Shared interfaces and utilities

### Core Services
- **Health Checks** - Application health monitoring
- **Metrics** - Basic metrics collection
- **Error Handling** - Centralized error management
- **Lifecycle Management** - Service startup/shutdown

## Optional Extensions

### Web & API Extensions
- **OpenAPI/AsyncAPI** - API documentation generation
- **WebSocket/SSE** - Real-time communication
- **Multiple Routers** - HTTPRouter, BunRouter support

### Enterprise Extensions
- **Consensus & Raft** (`pkg/consensus/`) - Distributed consensus
- **Event System** (`pkg/events/`) - Event-driven architecture
- **Caching** (`pkg/cache/`) - Redis, in-memory caching
- **Cron Scheduling** (`pkg/cron/`) - Distributed job scheduling

### AI Extensions
- **AI Package** (`pkg/ai/`) - LLM support, agent system
- **Training Pipeline** - ML model training
- **Inference Engine** - Model inference

### Security Extensions
- **Authentication** - JWT, OAuth2, SAML, OIDC
- **Authorization** - Role-based access control
- **Security Middleware** - Rate limiting, CORS, security headers

### Database Extensions
- **Database Package** (`pkg/database/`) - Database connections and ORM
- **Migrations** - Database schema management
- **Query Optimization** - Performance monitoring

### Plugin System
- **Plugin Manager** (`pkg/plugins/`) - Extensible plugin architecture
- **Framework Plugins** - Built-in framework plugins
- **Custom Plugins** - User-defined extensions

## Implementation Strategy

### Core Package Structure
```
pkg/
├── di/           # Dependency injection (CORE)
├── router/       # HTTP routing (CORE)
├── logger/       # Logging (CORE)
├── config/       # Configuration (CORE)
├── common/       # Shared utilities (CORE)
└── extensions/   # Optional extensions
    ├── ai/       # AI capabilities
    ├── cache/    # Caching systems
    ├── events/   # Event system
    ├── consensus/# Distributed consensus
    └── plugins/  # Plugin system
```

### Loading Strategy
1. **Core Components**: Always loaded
2. **Extensions**: Loaded based on configuration
3. **Plugins**: Dynamically loaded at runtime

### Configuration Example
```go
// Core application
app := forge.NewSimpleApplication("my-app", "1.0.0")

// Add extensions as needed
app.WithAI()           // Enable AI capabilities
app.WithCache()        // Enable caching
app.WithEvents()       // Enable event system
app.WithConsensus()    // Enable distributed consensus
```

## Benefits

### For Core Framework
- **Minimal Footprint**: Only essential components loaded
- **Fast Startup**: Reduced initialization time
- **Clear Dependencies**: Explicit extension requirements
- **Better Testing**: Easier to test core components

### For Extensions
- **Optional Loading**: Only load what you need
- **Independent Development**: Extensions can be developed separately
- **Version Compatibility**: Extensions can have different version requirements
- **Plugin Ecosystem**: Third-party extensions possible

## Migration Path

### Phase 1: Core Separation ✅
- Simplified DI container
- Basic router functionality
- Essential services only

### Phase 2: Extension Framework
- Move AI package to extensions
- Move consensus to extensions
- Move caching to extensions
- Create extension loading system

### Phase 3: Plugin System
- Dynamic plugin loading
- Plugin dependency management
- Plugin marketplace
- Version compatibility
