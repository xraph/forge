# AI Extension Demo

Comprehensive demonstration of the Forge v2 AI Extension with all features.

## Features Demonstrated

### 🤖 Core AI Capabilities
- **LLM Integration** - Multiple providers (OpenAI, Anthropic, Azure, Ollama, HuggingFace)
- **Inference Engine** - High-performance ML inference with batching and caching
- **Model Management** - Support for ONNX, PyTorch, TensorFlow, Scikit-learn
- **AI Agents** - Specialized agents for various tasks
- **Training** - Model training and fine-tuning
- **Monitoring** - Health checks, metrics, and alerts
- **Coordination** - Multi-agent collaboration

### 🎯 Supported LLM Providers

1. **OpenAI** - GPT-4, GPT-3.5-turbo, embeddings
2. **Anthropic** - Claude 3 (Opus, Sonnet, Haiku)
3. **Azure OpenAI** - Enterprise-grade OpenAI models
4. **Ollama** - Local LLM support (Llama 2, Mistral, etc.)
5. **HuggingFace** - Access to thousands of models

### 🤖 Available AI Agents

1. **Optimization Agent** - Performance tuning and resource optimization
2. **Security Agent** - Security monitoring and threat detection
3. **Anomaly Detection Agent** - Pattern recognition and anomaly detection
4. **Load Balancer Agent** - Intelligent traffic routing
5. **Cache Agent** - Cache optimization and hit rate improvement
6. **Resource Manager Agent** - Resource allocation optimization
7. **Scheduler Agent** - Task scheduling and prioritization
8. **Predictor Agent** - Predictive analytics and forecasting

### 🧠 Model Support

- **ONNX** - Cross-platform model runtime
- **PyTorch** - Research and production models
- **TensorFlow** - Enterprise-scale models
- **Scikit-learn** - Classical ML models
- **HuggingFace** - Transformers and language models

## Usage

### Basic Setup

```bash
# Run the demo
cd v2/examples/ai-demo
go run main.go
```

### Configuration

```go
config := ai.DefaultConfig()

// Configure LLM provider
config.LLM.Providers["openai"] = ai.ProviderConfig{
    Type:   "openai",
    APIKey: "sk-...",
    Models: []string{"gpt-4"},
}

// Enable features
config.EnableLLM = true
config.EnableAgents = true
config.EnableInference = true

// Configure inference
config.Inference.Workers = 4
config.Inference.EnableBatching = true
config.Inference.EnableCaching = true

// Create extension
ext := ai.NewExtensionWithConfig(config)
app.RegisterExtension(ext)
```

### Using LLM Providers

```go
// Get AI manager
aiManager := forge.Must[ai.AI](app, "ai.manager")

// Access LLM
llmManager := aiManager.LLM()

// Chat completion
response, err := llmManager.Chat(ctx, ai.ChatRequest{
    Model: "gpt-4",
    Messages: []ai.Message{
        {Role: "user", Content: "Hello!"},
    },
    MaxTokens: 100,
    Temperature: 0.7,
})
```

### Using AI Agents

```go
// Get optimization agent
agent, err := aiManager.GetAgent("optimization")

// Execute agent
output, err := agent.Execute(ctx, ai.AgentInput{
    Task: "Optimize database query performance",
    Context: map[string]interface{}{
        "query": "SELECT * FROM users",
        "current_time": "2.5s",
    },
})

fmt.Printf("Recommendation: %s\n", output.Result)
fmt.Printf("Confidence: %.2f\n", output.Confidence)
```

### Using Inference Engine

```go
// Get inference engine
engine := aiManager.Inference()

// Load model
err := engine.LoadModel(ctx, "sentiment", ai.ModelConfig{
    Type:   "onnx",
    Path:   "models/sentiment.onnx",
    Device: "cpu",
})

// Run inference
result, err := engine.Infer(ctx, "sentiment", ai.ModelInput{
    Data: "This product is amazing!",
})
```

### Model Training

```go
// Get training manager
trainer := aiManager.Training()

// Create training pipeline
pipeline := trainer.CreatePipeline(ai.TrainingPipelineConfig{
    ModelType: "pytorch",
    DataPath:  "data/training",
    Epochs:    10,
    BatchSize: 32,
})

// Start training
err := pipeline.Train(ctx)
```

## Configuration File Example

```yaml
ai:
  enable_llm: true
  enable_agents: true
  enable_inference: true
  enable_coordination: true
  max_concurrency: 10
  request_timeout: 30s
  cache_size: 1000

  llm:
    default_provider: openai
    providers:
      openai:
        type: openai
        api_key: ${OPENAI_API_KEY}
        models:
          - gpt-4
          - gpt-3.5-turbo
      
      ollama:
        type: ollama
        base_url: http://localhost:11434
        models:
          - llama2
          - mistral

  inference:
    workers: 4
    batch_size: 10
    batch_timeout: 100ms
    cache_size: 1000
    cache_ttl: 1h
    enable_batching: true
    enable_caching: true
    enable_scaling: true

  agents:
    enabled_agents:
      - optimization
      - security
      - anomaly_detection
    agent_configs:
      optimization:
        learning_rate: 0.01
        history_size: 1000
```

## Local Development with Ollama

For local LLM testing without API keys:

```bash
# Install Ollama
curl https://ollama.ai/install.sh | sh

# Pull a model
ollama pull llama2

# Run the demo
go run main.go
```

## Environment Variables

```bash
# OpenAI
export OPENAI_API_KEY="sk-..."

# Anthropic
export ANTHROPIC_API_KEY="sk-ant-..."

# Azure OpenAI
export AZURE_OPENAI_ENDPOINT="https://..."
export AZURE_OPENAI_KEY="..."

# HuggingFace
export HUGGINGFACE_API_KEY="hf_..."
```

## Advanced Features

### Multi-Agent Coordination

```go
// Create coordinator
coordinator := aiManager.Coordinator()

// Create agent team
team := coordinator.CreateTeam([]ai.AIAgent{
    optimizationAgent,
    securityAgent,
    anomalyAgent,
})

// Coordinate task
result, err := team.Execute(ctx, ai.AgentInput{
    Task: "Analyze and optimize system performance",
})
```

### Custom Middleware

```go
// Register custom AI middleware
middleware := ai.NewSecurityScannerMiddleware(config)
aiManager.RegisterMiddleware("security", middleware)
```

### Model Serving

```go
// Start model server
server := models.NewModelServer(ai.ServerConfig{
    Port: 8080,
    EnableREST: true,
    EnableGRPC: true,
})

// Register model
server.RegisterModel("sentiment", model)

// Start server
go server.Start()
```

## Monitoring

Access metrics at:
- **Prometheus**: `http://localhost:9090/metrics`
- **Health**: `http://localhost:8080/health`

## Troubleshooting

### LLM Provider Issues
- Check API keys are set correctly
- Verify network connectivity
- Check rate limits

### Inference Performance
- Increase worker count
- Enable batching
- Use GPU if available
- Adjust cache size

### Agent Issues
- Check agent configuration
- Verify required data is available
- Review agent logs

## Next Steps

1. **Configure Providers** - Add your LLM API keys
2. **Load Models** - Deploy your ML models
3. **Enable Agents** - Configure specialized agents
4. **Setup Training** - Create training pipelines
5. **Monitor** - Set up dashboards and alerts

## Learn More

- [AI Extension Documentation](../../_impl_docs/docs/extensions/ai.md)
- [LLM Provider Guide](../../_impl_docs/docs/ai/llm-providers.md)
- [Inference Engine Guide](../../_impl_docs/docs/ai/inference.md)
- [Agent Development Guide](../../_impl_docs/docs/ai/agents.md)
