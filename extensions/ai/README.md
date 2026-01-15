# AI Extension v3.0.0

Pure ai-sdk wrapper for building intelligent, LLM-powered agents with tool capabilities and persistent conversation state.

## Features

- **Pure ai-sdk Integration**: No custom wrappers, direct ai-sdk usage
- **Specialized Agent Templates**: 8 pre-configured agent types with tools
- **State Persistence**: Native StateStore support for conversation memory
- **REST API**: Full HTTP API for agent management
- **Tool Registry**: Extensible tool system for agent capabilities

## Quick Start

### Installation

```bash
go get github.com/xraph/forge/extensions/ai
go get github.com/xraph/ai-sdk
```

### Basic Usage

```go
package main

import (
    "context"
    aisdk "github.com/xraph/ai-sdk"
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/ai"
    "github.com/xraph/forge/extensions/ai/stores"
)

func main() {
    // 1. Create LLM manager
    llmManager, _ := aisdk.NewLLMManager(aisdk.LLMConfig{
        DefaultProvider: "openai",
        Providers: map[string]aisdk.ProviderConfig{
            "openai": {
                Type:   "openai",
                APIKey: "your-api-key",
            },
        },
    })
    
    // 2. Create state store
    stateStore := stores.NewMemoryStateStore()
    
    // 3. Create app
    app := forge.NewApp()
    
    // 4. Register dependencies
    app.Container().Register("llmManager", func(c forge.Container) (any, error) {
        return llmManager, nil
    })
    app.Container().Register("stateStore", func(c forge.Container) (any, error) {
        return stateStore, nil
    })
    
    // 5. Register AI extension
    app.RegisterExtension(ai.NewExtension())
    app.Start(context.Background())
    
    // 6. Create specialized agent
    agentMgr, _ := ai.GetAgentManager(app.Container())
    agent, _ := agentMgr.CreateAgent(ctx, &ai.AgentDefinition{
        ID:          "optimizer",
        Name:        "Cache Optimizer",
        Type:        "cache_optimizer",
        Model:       "gpt-4",
        Temperature: 0.7,
    })
    
    // 7. Execute agent
    result, _ := agent.Execute(ctx, "Analyze cache with 65% hit rate")
    fmt.Println(result)
}
```

## Agent Types

### cache_optimizer
Cache optimization and eviction strategies.

**Tools:**
- `analyze_cache_metrics` - Analyze hit/miss rates
- `optimize_eviction` - Recommend eviction policies
- `predict_warmup` - Cache warming strategies

**Use Cases:**
- Optimizing cache hit rates
- Selecting eviction policies
- Planning cache warmup

### scheduler
Job scheduling and resource allocation optimization.

**Tools:**
- `analyze_job_schedule` - Analyze scheduling efficiency
- `optimize_resource_allocation` - Optimize resource distribution
- `detect_scheduling_conflicts` - Find conflicts

**Use Cases:**
- Optimizing job queues
- Resource allocation
- Reducing wait times

### anomaly_detector
Statistical anomaly detection and pattern analysis.

**Tools:**
- `detect_anomalies` - Detect statistical anomalies
- `analyze_patterns` - Identify patterns
- `calculate_baseline` - Establish baselines

**Use Cases:**
- Monitoring system metrics
- Detecting unusual behavior
- Alerting on outliers

### load_balancer
Traffic distribution and load optimization.

**Tools:**
- `analyze_traffic` - Analyze traffic patterns
- `optimize_routing` - Optimize routing strategies
- `predict_capacity` - Forecast capacity needs

**Use Cases:**
- Optimizing load distribution
- Capacity planning
- Traffic routing

### security_monitor
Security threat detection and monitoring.

**Tools:**
- `detect_threats` - Detect security threats
- `analyze_access_patterns` - Analyze access for anomalies
- `recommend_security_actions` - Security recommendations

**Use Cases:**
- Threat detection
- Access pattern analysis
- Security improvements

### resource_manager
Resource utilization optimization.

**Tools:**
- `analyze_resource_usage` - Analyze resource patterns
- `optimize_allocation` - Optimize allocation
- `predict_resource_needs` - Predict future needs

**Use Cases:**
- CPU/memory optimization
- Resource provisioning
- Cost optimization

### predictor
Predictive analytics and forecasting.

**Tools:**
- `forecast_metrics` - Forecast future values
- `analyze_trends` - Analyze historical trends
- `predict_behavior` - Predict system behavior

**Use Cases:**
- Capacity forecasting
- Trend analysis
- Proactive planning

### optimizer
General system optimization.

**Tools:**
- `analyze_performance` - Analyze overall performance
- `recommend_optimizations` - System optimizations
- `measure_impact` - Measure improvements

**Use Cases:**
- Performance tuning
- System optimization
- Configuration recommendations

## REST API

### Create Agent
```http
POST /agents
Content-Type: application/json

{
    "name": "My Optimizer",
    "type": "cache_optimizer",
    "model": "gpt-4",
    "temperature": 0.7,
    "config": {}
}
```

### Execute Agent
```http
POST /agents/:id/execute
Content-Type: application/json

{
    "message": "Analyze cache with 65% hit rate and 35% miss rate"
}
```

### List Templates
```http
GET /agents/templates
```

Response:
```json
{
    "templates": [
        "cache_optimizer",
        "scheduler",
        "anomaly_detector",
        "load_balancer",
        "security_monitor",
        "resource_manager",
        "predictor",
        "optimizer"
    ],
    "total": 8
}
```

## Package Architecture

The AI extension consists of two complementary components:

### 1. LLM Operations & Agents (Main Extension)

**Purpose**: LLM-powered agents with tool calling and conversation management

**Use for:**
- Chat applications and conversational AI
- AI agents with tool capabilities
- RAG (Retrieval Augmented Generation)
- Text generation and streaming
- Agent orchestration

**Key Features:**
- Pure ai-sdk integration
- 8 specialized agent templates
- Tool registry for extensibility
- State persistence via StateStore
- REST API for agent management

### 2. ML Model Inference ([`inference/`](inference/))

**Purpose**: Production ML model serving infrastructure

**Use for:**
- Serving custom TensorFlow/PyTorch models
- High-throughput batch inference (>1000 req/s)
- Real-time inference with SLAs (<100ms)
- Complex pre/post processing pipelines
- Auto-scaling inference workloads

**Key Features:**
- Dynamic batching strategies
- Auto-scaling worker pool
- LRU/LFU caching with TTL
- Pre/post processing pipelines
- Production observability

### When to Use What?

```mermaid
flowchart TD
    Start[AI Workload] --> Type{What type?}
    Type -->|Custom ML Model| Inference[Use inference/]
    Type -->|LLM API| Extension[Use AI Extension]
    Type -->|Self-Hosted LLM| Both[Use Both]
    
    Inference --> Features{Need advanced features?}
    Features -->|Yes batching/scaling| InferenceEngine[InferenceEngine]
    Features -->|No| SimplePredict[Simple model.Predict]
    
    Extension --> Operations{What operations?}
    Operations -->|Agents/Chat| UseAgents[AgentManager + Templates]
    Operations -->|Generation| UseSDK[Direct ai-sdk]
    
    Both --> Bridge[Consider LLM Bridge Layer]
```

**Decision Guide:**

| Scenario | Solution | Reference |
|----------|----------|-----------|
| Chat with GPT-4 | **AI Extension** | This README |
| Serve TensorFlow model | **Inference Package** | [`inference/README.md`](inference/README.md) |
| AI agents with tools | **AI Extension** | This README |
| Batch image classification | **Inference Package** | [`inference/README.md`](inference/README.md) |
| Semantic search | **AI Extension** | This README |
| Real-time scoring API | **Inference Package** | [`inference/README.md`](inference/README.md) |
| Conversational agent | **AI Extension** | This README |
| Self-hosted LLM (>1000 req/s) | **Both** | [`docs/INFERENCE_VS_AISDK.md`](docs/INFERENCE_VS_AISDK.md) |

**See also:**
- [Inference Package README](inference/README.md) - ML model serving
- [Inference vs AI-SDK Guide](docs/INFERENCE_VS_AISDK.md) - Detailed comparison
- [Inference Examples](inference/examples/) - TensorFlow, PyTorch, batch processing

## State Persistence

The AI extension requires a StateStore implementation from ai-sdk integrations.

### Memory StateStore (Development)

```go
import memory "github.com/xraph/ai-sdk/integrations/statestores/memory"

stateStore := memory.NewMemoryStateStore(memory.Config{
    Logger:  logger,
    Metrics: metrics,
    TTL:     24 * time.Hour, // Auto-cleanup
})
```

### PostgreSQL StateStore (Production)

```go
import postgres "github.com/xraph/ai-sdk/integrations/statestores/postgres"

stateStore, err := postgres.NewPostgresStateStore(ctx, postgres.Config{
    ConnString: "postgres://user:pass@localhost/db",
    TableName:  "agent_states",
    Logger:     logger,
    Metrics:    metrics,
})
// Auto-creates table with JSONB column and indexes
```

### Redis StateStore (Production - Distributed)

```go
import redis "github.com/xraph/ai-sdk/integrations/statestores/redis"

stateStore, err := redis.NewRedisStateStore(ctx, redis.Config{
    Addrs:    []string{"localhost:6379"},
    Password: os.Getenv("REDIS_PASSWORD"),
    Logger:   logger,
    Metrics:  metrics,
})
// Supports cluster and sentinel modes
```

See [ai-sdk integrations](https://github.com/xraph/ai-sdk/tree/main/integrations/statestores) for all options.

## Architecture

```
┌─────────────────┐
│  AI Extension   │
│   (v3.0.0)      │
└────────┬────────┘
         │
         ├──> LLM Manager (ai-sdk)
         ├──> StateStore (ai-sdk interface)
         ├──> AgentManager (tracking)
         └──> AgentFactory (templates)
                   │
                   ├──> Agent Templates
                   └──> Tool Registry
```

## Examples

See:
- `examples/ai-demo/` - Basic ai-sdk integration
- `examples/ai-agents-demo/` - Specialized agents

## Requirements

- Go 1.25.5+ (required by ai-sdk)
- Valid LLM API key (OpenAI, Anthropic, etc.) or local model (Ollama, LM Studio)

## Upgrading from v2.x

See [BREAKING_CHANGES.md](./BREAKING_CHANGES.md) for detailed migration guide.

## License

See main repository LICENSE.
