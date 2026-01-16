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

The AI extension provides a complete ML/AI platform with three complementary components covering the entire lifecycle:

### Complete ML Lifecycle

```mermaid
flowchart LR
    Data[Your Data] --> Training[Training Package]
    Training --> Model[Trained Model]
    Model --> Inference[Inference Package]
    Inference --> Production[Production API]
    
    LLM[LLM APIs] --> Extension[AI Extension]
    Extension --> Agents[AI Agents]
    
    style Training fill:#e1f5ff
    style Inference fill:#fff4e1
    style Extension fill:#f0ffe1
```

### 1. Training Package ([`training/`](training/))

**Purpose**: Build custom ML models from your data

**Use for:**
- Training TensorFlow, PyTorch, XGBoost models
- Custom fraud detection, churn prediction, recommendations
- Domain-specific classification and regression
- Model hyperparameter tuning
- Dataset preparation and validation

**Key Features:**
- Model training with early stopping
- Data management and quality checks
- Training pipelines with dependencies
- Hyperparameter optimization
- Checkpointing and model versioning

**Produces**: Trained models ready for inference deployment

**[→ Training Documentation](training/README.md)**

### 2. Inference Package ([`inference/`](inference/))

**Purpose**: Serve trained models in production

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

**Consumes**: Models from training package

**[→ Inference Documentation](inference/README.md)**

### 3. LLM Operations & Agents (Main Extension)

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

**Uses**: Pre-trained LLMs via APIs (OpenAI, Anthropic, etc.)

### When to Use What?

```mermaid
flowchart TD
    Start[AI/ML Need] --> Question{What do you need?}
    
    Question -->|Build custom model| HasData{Have labeled data?}
    Question -->|Serve predictions| HaveModel{Have trained model?}
    Question -->|Chat/LLM features| UseLLM[AI Extension]
    
    HasData -->|Yes| UseTraining[Training Package]
    HasData -->|No| ConsiderLLM[Consider pre-trained LLMs]
    
    UseTraining --> TrainComplete[Model trained]
    TrainComplete --> UseInference[Inference Package]
    
    HaveModel -->|Yes| UseInference
    HaveModel -->|No| UseTraining
    
    UseInference --> Production[Production API]
    
    UseLLM --> AgentType{What operations?}
    AgentType -->|Agents/Tools| AgentMgr[AgentManager]
    AgentType -->|Generation| SDK[Direct ai-sdk]
    AgentType -->|Custom + LLM| Hybrid[Hybrid: All three]
```

**Complete Workflow Example:**

```go
// 1. TRAINING: Build custom model from your data
trainer := training.NewModelTrainer(logger, metrics)
job, _ := trainer.StartTraining(ctx, trainingRequest)
trainer.SaveModel(ctx, job.ID(), "./models/my_model.pkl")

// 2. INFERENCE: Serve the trained model
inferenceEngine := inference.NewInferenceEngine(config)
model := LoadTrainedModel("./models/my_model.pkl")
inferenceEngine.RegisterModel("my-model", model)
inferenceEngine.Start(ctx)

// Predictions
result, _ := inferenceEngine.Infer(ctx, inferenceRequest)

// 3. AI AGENTS: Use LLM + custom model together
agent, _ := aisdk.NewAgent("assistant", "gpt-4", llmManager, ...)
agent.RegisterTool(&aisdk.Tool{
    Name: "predict_churn",
    Handler: func(ctx context.Context, input map[string]any) (any, error) {
        return inferenceEngine.Infer(ctx, toInferenceRequest(input))
    },
})

// Agent now has both LLM reasoning + your custom model
response, _ := agent.Execute(ctx, "Analyze this customer and recommend action")
```

**Decision Guide:**

| Scenario | Solution | Reference |
|----------|----------|-----------|
| **Training & Custom Models** |||
| Train fraud detection model | **Training** | [`training/README.md`](training/README.md) |
| Customer churn prediction | **Training** | [`training/README.md`](training/README.md) |
| Recommendation system | **Training** | [`training/README.md`](training/README.md) |
| Fine-tune existing model | **Training** | [`training/README.md`](training/README.md) |
| **Inference & Model Serving** |||
| Serve TensorFlow model | **Inference** | [`inference/README.md`](inference/README.md) |
| Batch image classification | **Inference** | [`inference/README.md`](inference/README.md) |
| Real-time scoring API | **Inference** | [`inference/README.md`](inference/README.md) |
| High-throughput predictions | **Inference** | [`inference/README.md`](inference/README.md) |
| **LLM & Agents** |||
| Chat with GPT-4 | **AI Extension** | This README |
| AI agents with tools | **AI Extension** | This README |
| Semantic search | **AI Extension** | This README |
| Conversational agent | **AI Extension** | This README |
| **Hybrid Use Cases** |||
| Custom model + LLM reasoning | **All Three** | [`docs/ML_LIFECYCLE_GUIDE.md`](docs/ML_LIFECYCLE_GUIDE.md) |
| Train → Serve → Monitor | **Training + Inference** | [`training/examples/train_to_inference.go`](training/examples/train_to_inference.go) |

**See also:**
- [Training Package README](training/README.md) - Build custom models
- [Inference Package README](inference/README.md) - Serve models in production
- [ML Lifecycle Guide](docs/ML_LIFECYCLE_GUIDE.md) - Complete workflow comparison
- [Training Examples](training/examples/) - End-to-end training workflows
- [Inference Examples](inference/examples/) - Production model serving

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
