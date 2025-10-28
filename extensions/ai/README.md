# Forge AI Extension

The AI Extension is the most comprehensive extension in Forge v2, providing a complete AI/ML platform with LLM integration, intelligent agents, model management, and high-performance inference capabilities.

## 🚀 Features

### Core Capabilities
- **🤖 LLM Integration** - Multiple providers (OpenAI, Anthropic, Azure, Ollama, HuggingFace)
- **🧠 AI Agents** - Intelligent agents for optimization, security, anomaly detection, and more
- **⚡ Inference Engine** - High-performance ML inference with batching, caching, and auto-scaling
- **📊 Model Management** - Support for ONNX, PyTorch, TensorFlow, Scikit-learn, and HuggingFace
- **🔄 Streaming Support** - Real-time streaming for chat and completions
- **📈 Monitoring** - Comprehensive metrics, health checks, and observability
- **🎯 Smart Caching** - Intelligent caching with TTL and invalidation strategies
- **⚖️ Load Balancing** - Automatic load balancing and scaling

### LLM Providers
- **OpenAI** - GPT-4, GPT-3.5-turbo with function calling and vision
- **Anthropic** - Claude 3 (Opus, Sonnet, Haiku) with tool use
- **Azure OpenAI** - Enterprise-grade OpenAI with deployment management
- **Ollama** - Local LLM support (Llama 2, Mistral, etc.)
- **HuggingFace** - Inference API and model hub integration

### AI Agents
- **Optimization Agent** - Performance optimization and resource management
- **Security Agent** - Security monitoring and threat detection
- **Anomaly Detection Agent** - Pattern recognition and anomaly detection
- **Load Balancer Agent** - Intelligent load balancing decisions
- **Cache Agent** - Cache optimization and management
- **Resource Agent** - Resource allocation and monitoring
- **Scheduler Agent** - Task scheduling and prioritization
- **Predictor Agent** - Predictive analytics and forecasting

### Model Frameworks
- **ONNX** - Cross-platform model deployment with GPU acceleration
- **PyTorch** - Research and production models with TorchScript
- **TensorFlow** - Production-scale models with SavedModel format
- **Scikit-learn** - Classical ML models with pipeline support
- **HuggingFace** - Transformer models with AutoModel loading

## 🏗️ Architecture

```
AI Extension
├── LLM Subsystem
│   ├── Manager (provider orchestration)
│   ├── Providers (OpenAI, Anthropic, etc.)
│   ├── Chat & Completion APIs
│   ├── Embedding Support
│   └── Streaming Client
├── Agent Subsystem
│   ├── Agent Factory
│   ├── Base Agent Interface
│   ├── Specialized Agents
│   └── Agent Store (persistence)
├── Model Subsystem
│   ├── Model Registry
│   ├── Framework Adapters
│   ├── Model Server
│   └── Lifecycle Management
├── Inference Engine
│   ├── Request Batching
│   ├── Response Caching
│   ├── Auto-scaling
│   ├── Worker Pool
│   └── Pipeline Processing
└── Core Components
    ├── Configuration
    ├── Metrics & Health
    ├── Storage Interfaces
    └── REST API
```

## 📦 Installation

Add the AI extension to your Forge application:

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/ai"
)

func main() {
    app := forge.New()
    
    // Add AI extension
    app.AddExtension(ai.NewExtension())
    
    app.Run()
}
```

## ⚙️ Configuration

### Basic Configuration

```yaml
# config.yaml
ai:
  # Core features
  llm_enabled: true
  agents_enabled: true
  inference_enabled: true
  training_enabled: false
  coordination_enabled: false
  
  # Performance settings
  max_concurrency: 10
  request_timeout: 30s
  cache_size: 1000
  
  # LLM configuration
  llm:
    default_provider: "openai"
    request_timeout: 30s
    max_retries: 3
    providers:
      openai:
        api_key: "${OPENAI_API_KEY}"
        base_url: "https://api.openai.com/v1"
        timeout: 30s
        max_retries: 3
        rate_limit: 100
      anthropic:
        api_key: "${ANTHROPIC_API_KEY}"
        base_url: "https://api.anthropic.com"
        timeout: 30s
        max_retries: 3
        rate_limit: 50
  
  # Inference configuration
  inference:
    workers: 4
    batch_size: 10
    batch_timeout: 100ms
    cache_size: 1000
    cache_ttl: 1h
    enable_batching: true
    enable_caching: true
    enable_scaling: true
    scaling_threshold: 0.8
    max_workers: 20
    min_workers: 2
  
  # Agent configuration
  agents:
    enabled_agents:
      - "optimization"
      - "security"
      - "anomaly"
    optimization:
      learning_enabled: true
      auto_apply: false
      max_concurrency: 5
    security:
      threat_threshold: 0.8
      auto_block: false
      scan_interval: 5m
```

### Environment Variables

```bash
# LLM Provider API Keys
export OPENAI_API_KEY="your-openai-key"
export ANTHROPIC_API_KEY="your-anthropic-key"
export AZURE_OPENAI_KEY="your-azure-key"
export HUGGINGFACE_API_KEY="your-hf-key"

# Optional: Custom endpoints
export OPENAI_BASE_URL="https://api.openai.com/v1"
export ANTHROPIC_BASE_URL="https://api.anthropic.com"
export OLLAMA_BASE_URL="http://localhost:11434"
```

## 🚀 Quick Start

### 1. Basic LLM Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/ai"
)

func main() {
    app := forge.New()
    app.AddExtension(ai.NewExtension())
    
    // Get AI service
    aiService, err := app.Container().Resolve("ai")
    if err != nil {
        log.Fatal(err)
    }
    
    ai := aiService.(ai.AI)
    
    // Get LLM manager
    llmManager := ai.GetLLMManager()
    
    // Create chat request
    request := ai.ChatRequest{
        Provider: "openai",
        Model:    "gpt-4",
        Messages: []ai.ChatMessage{
            {
                Role:    "user",
                Content: "Explain quantum computing in simple terms",
            },
        },
        Temperature: 0.7,
        MaxTokens:   500,
    }
    
    // Send chat request
    response, err := llmManager.Chat(context.Background(), request)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Response: %s\n", response.Choices[0].Message.Content)
}
```

### 2. Using AI Agents

```go
// Create and register an optimization agent
agentFactory, err := app.Container().Resolve("agentFactory")
if err != nil {
    log.Fatal(err)
}

factory := agentFactory.(ai.AgentFactory)

// Create optimization agent
agent, err := factory.CreateAgent("optimization", ai.AgentConfig{
    ID:              "opt-1",
    Name:            "Performance Optimizer",
    LearningEnabled: true,
    AutoApply:       false,
    MaxConcurrency:  5,
})
if err != nil {
    log.Fatal(err)
}

// Process optimization request
input := ai.AgentInput{
    Type: "performance_analysis",
    Data: map[string]interface{}{
        "metrics": map[string]float64{
            "cpu_usage":    85.5,
            "memory_usage": 72.3,
            "response_time": 250.0,
        },
        "threshold": 80.0,
    },
}

output, err := agent.Process(context.Background(), input)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Optimization suggestions: %+v\n", output.Data)
```

### 3. Model Inference

```go
// Get inference engine
inferenceEngine := ai.GetInferenceEngine()

// Add a model
model := &MyCustomModel{
    id:        "sentiment-model",
    framework: ai.MLFrameworkONNX,
    modelPath: "/path/to/sentiment.onnx",
}

err := inferenceEngine.AddModel(model)
if err != nil {
    log.Fatal(err)
}

// Create inference request
request := ai.InferenceRequest{
    ID:      "req-1",
    ModelID: "sentiment-model",
    Input: ai.ModelInput{
        Data: map[string]interface{}{
            "text": "This product is amazing!",
        },
    },
    Options: ai.InferenceOptions{
        UseCache:  true,
        CacheTTL:  time.Hour,
        BatchSize: 1,
    },
}

// Perform inference
response, err := inferenceEngine.Infer(context.Background(), request)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Prediction: %+v\n", response.Output.Predictions)
```

## 📊 Monitoring and Metrics

The AI extension provides comprehensive monitoring capabilities:

### Health Checks

```go
// Check overall AI health
health := ai.GetHealth()
fmt.Printf("AI Status: %s\n", health.Status)

// Check LLM manager health
llmHealth := llmManager.GetHealth()
fmt.Printf("LLM Status: %s\n", llmHealth.Status)

// Check inference engine health
inferenceHealth := inferenceEngine.GetHealth()
fmt.Printf("Inference Status: %s\n", inferenceHealth.Status)
```

### Metrics

```go
// Get AI statistics
stats := ai.GetStats()
fmt.Printf("Total Requests: %d\n", stats.TotalRequests)
fmt.Printf("Active Agents: %d\n", stats.ActiveAgents)
fmt.Printf("Models Loaded: %d\n", stats.ModelsLoaded)

// Get LLM statistics
llmStats := llmManager.GetStats()
fmt.Printf("LLM Requests: %d\n", llmStats.TotalRequests)
fmt.Printf("Average Latency: %v\n", llmStats.AverageLatency)

// Get inference statistics
inferenceStats := inferenceEngine.GetStats()
fmt.Printf("Inferences: %d\n", inferenceStats.TotalInferences)
fmt.Printf("Cache Hit Rate: %.2f%%\n", inferenceStats.CacheHitRate*100)
```

## 🔧 Advanced Usage

### Custom Agents

```go
// Implement custom agent
type CustomAgent struct {
    *ai.BaseAgent
}

func (a *CustomAgent) Process(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
    // Custom processing logic
    return ai.AgentOutput{
        Type: "custom_result",
        Data: map[string]interface{}{
            "processed": true,
            "result":    "custom processing complete",
        },
    }, nil
}

// Register custom agent template
factory.RegisterTemplate("custom", func(config ai.AgentConfig) (ai.AIAgent, error) {
    capabilities := []ai.Capability{
        {
            Name:        "custom-processing",
            Description: "Custom data processing",
            InputType:   reflect.TypeOf(ai.AgentInput{}),
            OutputType:  reflect.TypeOf(ai.AgentOutput{}),
        },
    }
    
    baseAgent := ai.NewBaseAgent(
        config.ID,
        config.Name,
        ai.AgentTypeCustom,
        capabilities,
    )
    
    return &CustomAgent{BaseAgent: baseAgent}, nil
})
```

### Streaming Responses

```go
// Create streaming chat request
request := ai.ChatRequest{
    Provider:  "openai",
    Model:     "gpt-4",
    Messages:  messages,
    Streaming: true,
}

// Handle streaming response
err := llmManager.ChatStream(context.Background(), request, func(event ai.ChatStreamEvent) {
    if event.Delta != nil && event.Delta.Content != "" {
        fmt.Print(event.Delta.Content)
    }
})
```

## 🔗 Integration Examples

### With HTTP Handlers

```go
func chatHandler(w http.ResponseWriter, r *http.Request) {
    var request ai.ChatRequest
    if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    response, err := llmManager.Chat(r.Context(), request)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}
```

### With gRPC Services

```go
func (s *AIService) Chat(ctx context.Context, req *pb.ChatRequest) (*pb.ChatResponse, error) {
    chatReq := ai.ChatRequest{
        Provider: req.Provider,
        Model:    req.Model,
        Messages: convertMessages(req.Messages),
    }
    
    response, err := s.llmManager.Chat(ctx, chatReq)
    if err != nil {
        return nil, err
    }
    
    return &pb.ChatResponse{
        Id:      response.ID,
        Choices: convertChoices(response.Choices),
        Usage:   convertUsage(response.Usage),
    }, nil
}
```

## 🛠️ Development

### Running Tests

```bash
# Run all AI extension tests
go test ./extensions/ai/...

# Run with race detection
go test -race ./extensions/ai/...

# Run with coverage
go test -cover ./extensions/ai/...
```

### Building with Docker

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o forge-ai ./cmd/forge

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/forge-ai .
CMD ["./forge-ai"]
```

## 📚 API Reference

For detailed API documentation, see:
- [Configuration Reference](docs/configuration.md)
- [LLM Provider Guide](docs/llm-providers.md)
- [Agent Development Guide](docs/agents.md)
- [Model Management Guide](docs/models.md)
- [Inference Engine Guide](docs/inference.md)

## 🤝 Contributing

We welcome contributions to the AI Extension! By contributing, you grant xraph a license to use your contribution under any license terms, including the commercial license.

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## 📄 License

⚠️ **Important**: The AI Extension uses a **Commercial Source-Available License**, which is different from the main Forge framework's MIT license.

### What This Means

✅ **Free for:**
- Personal projects
- Educational and research purposes
- Internal evaluation (90 days)
- Learning and studying the code

❌ **Commercial license required for:**
- Production deployments
- Commercial products and services
- Revenue-generating applications
- SaaS platforms

### Full License Details

- See [LICENSE](LICENSE) for complete terms
- See [LICENSE_NOTICE.md](LICENSE_NOTICE.md) for a summary
- See main [LICENSING.md](../../LICENSING.md) for the complete licensing guide

### Need a Commercial License?

For commercial use of the AI Extension in production:

- **Email**: licensing@xraph.com
- **Web**: https://github.com/xraph/forge
- **Issues**: https://github.com/xraph/forge/issues

We offer flexible pricing for startups, enterprises, and custom agreements.