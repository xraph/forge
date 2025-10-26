# 🚀 Forge AI SDK

> **Enterprise-Grade AI SDK for Go** - Beyond Vercel AI SDK

A production-ready, type-safe AI SDK for Go with advanced features like multi-tier memory, RAG, workflow orchestration, cost management, and enterprise guardrails.

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![Test Coverage](https://img.shields.io/badge/coverage-81%25-brightgreen)](./tests)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## ✨ Features

### 🎯 **Core Capabilities**

| Feature | Description | Status |
|---------|-------------|--------|
| **Fluent API** | Type-safe builders with method chaining | ✅ |
| **Structured Output** | Go generics + JSON schema validation | ✅ |
| **Enhanced Streaming** | Token-by-token with reasoning steps | ✅ |
| **RAG Support** | Full pipeline with semantic search & reranking | ✅ |
| **Multi-Tier Memory** | Working → Short → Long → Episodic | ✅ |
| **Tool System** | Auto-discovery from Go functions | ✅ |
| **Workflow Engine** | DAG-based orchestration | ✅ |
| **Prompt Templates** | Versioning + A/B testing | ✅ |
| **Safety Guardrails** | PII, injection, toxicity detection | ✅ |
| **Cost Management** | Budget tracking + optimization | ✅ |
| **Resilience** | Circuit breakers, retries, rate limiting | ✅ |
| **Observability** | Tracing, profiling, debugging | ✅ |

---

## 🚦 Quick Start

### Installation

```bash
go get github.com/xraph/forge/extensions/ai/sdk
```

### Basic Text Generation

```go
package main

import (
	"context"
	"fmt"
	
	"github.com/xraph/forge/extensions/ai/sdk"
)

func main() {
	// Create LLM manager
	llmManager := sdk.NewLLMManager(logger, metrics)
	
	// Generate text
	result, err := sdk.NewGenerateBuilder(context.Background(), llmManager, logger, metrics).
		WithProvider("openai").
		WithModel("gpt-4").
		WithPrompt("Explain quantum computing in simple terms").
		WithMaxTokens(500).
		WithTemperature(0.7).
		Execute()
	
	if err != nil {
		panic(err)
	}
	
	fmt.Println(result.Content)
}
```

---

## 📚 Examples

### 1. Structured Output with Type Safety

```go
type Person struct {
	Name    string   `json:"name" description:"Full name"`
	Age     int      `json:"age" description:"Age in years"`
	Hobbies []string `json:"hobbies" description:"List of hobbies"`
}

person, err := sdk.NewGenerateObjectBuilder[Person](ctx, llmManager, logger, metrics).
	WithProvider("openai").
	WithModel("gpt-4").
	WithPrompt("Extract person info: John Doe, 30, loves reading and hiking").
	WithValidator(func(p *Person) error {
		if p.Age < 0 || p.Age > 150 {
			return fmt.Errorf("invalid age: %d", p.Age)
		}
		return nil
	}).
	Execute()

fmt.Printf("%+v\n", person)
// Output: &{Name:John Doe Age:30 Hobbies:[reading hiking]}
```

### 2. Enhanced Streaming with Reasoning

```go
stream := sdk.NewStreamBuilder(ctx, llmManager, logger, metrics).
	WithProvider("anthropic").
	WithModel("claude-3-opus").
	WithPrompt("Solve: What is 15% of 240?").
	WithReasoning(true).
	OnToken(func(token string) {
		fmt.Print(token)
	}).
	OnReasoning(func(step string) {
		fmt.Printf("[Thinking: %s]\n", step)
	}).
	OnComplete(func(result *sdk.Result) {
		fmt.Printf("\n✅ Complete! Cost: $%.4f\n", result.Usage.Cost)
	})

result, err := stream.Stream()
```

**Output:**
```
[Thinking: Need to calculate 15% of 240]
[Thinking: 15% = 0.15, so 0.15 * 240]
The answer is 36.
✅ Complete! Cost: $0.0120
```

### 3. RAG (Retrieval Augmented Generation)

```go
// Setup RAG
embeddingModel := &MyEmbeddingModel{}
vectorStore := &MyVectorStore{}

rag := sdk.NewRAG(embeddingModel, vectorStore, logger, metrics, &sdk.RAGOptions{
	ChunkSize:    500,
	ChunkOverlap: 50,
	TopK:         5,
})

// Index documents
rag.IndexDocument(ctx, "doc1", "Quantum computers use qubits that can be 0 and 1 simultaneously...")
rag.IndexDocument(ctx, "doc2", "Machine learning models require training data...")

// Retrieve and generate
generator := sdk.NewGenerateBuilder(ctx, llmManager, logger, metrics).
	WithProvider("openai").
	WithModel("gpt-4")

result, err := rag.GenerateWithContext(ctx, generator, "How do quantum computers work?", 5)
```

### 4. Stateful Agents with Memory

```go
// Create agent
agent, err := sdk.NewAgent("assistant", "AI Assistant", llmManager, stateStore, logger, metrics, &sdk.AgentOptions{
	SystemPrompt: "You are a helpful assistant with memory of past conversations.",
	MaxIterations: 10,
	Temperature: 0.7,
})

// First conversation
result1, err := agent.Execute(ctx, "My name is Alice and I love hiking.")
// Agent: "Nice to meet you, Alice! Hiking is a wonderful activity..."

// Second conversation (agent remembers)
result2, err := agent.Execute(ctx, "What do you know about me?")
// Agent: "You're Alice, and you mentioned you love hiking!"

// Save state
agent.SaveState(ctx)
```

### 5. Multi-Tier Memory System

```go
// Create memory manager
memoryManager := sdk.NewMemoryManager("agent_1", embeddingModel, vectorStore, logger, metrics, &sdk.MemoryManagerOptions{
	WorkingCapacity: 10,
	ShortTermTTL:    24 * time.Hour,
	ImportanceDecay: 0.1,
})

// Store memories
memoryManager.Store(ctx, "User prefers morning meetings", 0.8, nil) // High importance
memoryManager.Store(ctx, "Weather is sunny today", 0.3, nil)         // Low importance

// Recall memories
memories, err := memoryManager.Recall(ctx, "meetings", sdk.MemoryTierAll, 5)

// Promote important memory
memoryManager.Promote(ctx, memories[0].ID)

// Create episodic memory
episode := &sdk.EpisodicMemory{
	Title:       "Project Launch Meeting",
	Description: "Discussed Q1 goals and assigned tasks",
	Memories:    []string{"mem_id_1", "mem_id_2"},
	Tags:        []string{"project", "meeting"},
	Importance:  0.9,
}
memoryManager.CreateEpisode(ctx, episode)
```

### 6. Dynamic Tool System

```go
// Create tool registry
registry := sdk.NewToolRegistry(logger, metrics)

// Register from Go function
registry.RegisterFunc("calculate", "Performs arithmetic", func(ctx context.Context, a, b float64, op string) (float64, error) {
	switch op {
	case "add":
		return a + b, nil
	case "multiply":
		return a * b, nil
	default:
		return 0, fmt.Errorf("unsupported operation: %s", op)
	}
})

// Register with full definition
registry.RegisterTool(&sdk.ToolDefinition{
	Name:        "web_search",
	Version:     "1.0.0",
	Description: "Searches the web",
	Parameters: sdk.ToolParameterSchema{
		Type: "object",
		Properties: map[string]sdk.ToolParameterProperty{
			"query": {Type: "string", Description: "Search query"},
		},
		Required: []string{"query"},
	},
	Handler: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		query := params["query"].(string)
		return performWebSearch(query), nil
	},
	Timeout: 10 * time.Second,
})

// Execute tool
result, err := registry.ExecuteTool(ctx, "calculate", "1.0.0", map[string]interface{}{
	"param0": 10.0,
	"param1": 5.0,
	"param2": "add",
})
// result.Result: 15.0
```

### 7. Workflow Engine (DAG)

```go
// Create workflow
workflow := sdk.NewWorkflow("onboarding", "User Onboarding", logger, metrics)

// Add nodes
workflow.AddNode(&sdk.WorkflowNode{
	ID:       "validate",
	Type:     sdk.NodeTypeTool,
	ToolName: "validate_email",
})

workflow.AddNode(&sdk.WorkflowNode{
	ID:      "send_welcome",
	Type:    sdk.NodeTypeAgent,
	AgentID: "email_agent",
})

workflow.AddNode(&sdk.WorkflowNode{
	ID:        "check_activation",
	Type:      sdk.NodeTypeCondition,
	Condition: "user.activated == true",
})

// Define dependencies
workflow.AddEdge("validate", "send_welcome")
workflow.AddEdge("send_welcome", "check_activation")
workflow.SetStartNode("validate")

// Execute
execution, err := workflow.Execute(ctx, map[string]interface{}{
	"email": "user@example.com",
})

fmt.Printf("Workflow completed: %s\n", execution.Status)
```

### 8. Prompt Templates with A/B Testing

```go
// Create template manager
templateMgr := sdk.NewPromptTemplateManager(logger, metrics)

// Register template
templateMgr.RegisterTemplate(&sdk.PromptTemplate{
	Name:     "greeting",
	Version:  "1.0.0",
	Template: "Hello, {{.Name}}! Welcome to {{.Service}}.",
})

// Render
text, err := templateMgr.Render("greeting", "1.0.0", map[string]interface{}{
	"Name":    "Alice",
	"Service": "AI Platform",
})
// Output: "Hello, Alice! Welcome to AI Platform."

// Create A/B test
templateMgr.RegisterTemplate(&sdk.PromptTemplate{
	Name:     "greeting",
	Version:  "2.0.0",
	Template: "Hi {{.Name}}! 👋 Excited to have you on {{.Service}}!",
})

templateMgr.CreateABTest(&sdk.ABTest{
	Name: "greeting",
	Variants: []sdk.ABVariant{
		{Name: "control", TemplateVersion: "1.0.0", TrafficWeight: 0.5},
		{Name: "friendly", TemplateVersion: "2.0.0", TrafficWeight: 0.5},
	},
})

// Render will automatically select variant
text, _ = templateMgr.Render("greeting", "", vars)

// Track results
templateMgr.RecordABTestResult("greeting", "friendly", true, 120*time.Millisecond, 0.001)
```

### 9. Safety Guardrails

```go
// Create guardrail manager
guardrails := sdk.NewGuardrailManager(logger, metrics, &sdk.GuardrailOptions{
	EnablePII:             true,
	EnableToxicity:        true,
	EnablePromptInjection: true,
	EnableContentFilter:   true,
	MaxInputLength:        10000,
})

// Validate input
violations, err := guardrails.ValidateInput(ctx, userInput)
if sdk.ShouldBlock(violations) {
	return fmt.Errorf("input blocked: %s", sdk.FormatViolations(violations))
}

// Validate output
violations, err = guardrails.ValidateOutput(ctx, aiOutput)

// Redact PII
clean := guardrails.RedactPII("My email is john@example.com")
// Output: "My email is [REDACTED]"
```

### 10. Cost Management

```go
// Create cost tracker
costTracker := sdk.NewCostTracker(logger, metrics, &sdk.CostTrackerOptions{
	RetentionPeriod: 30 * 24 * time.Hour,
})

// Set budget
costTracker.SetBudget("monthly", 1000.0, 30*24*time.Hour, 0.8) // Alert at 80%

// Record usage (automatic via SDK)
costTracker.RecordUsage(ctx, sdk.UsageRecord{
	Provider:     "openai",
	Model:        "gpt-4",
	InputTokens:  1000,
	OutputTokens: 500,
})

// Get insights
insights := costTracker.GetInsights()
fmt.Printf("Cost today: $%.2f\n", insights.CostToday)
fmt.Printf("Projected monthly: $%.2f\n", insights.ProjectedMonthly)
fmt.Printf("Cache hit rate: %.1f%%\n", insights.CacheHitRate*100)

// Get recommendations
recommendations := costTracker.GetOptimizationRecommendations()
for _, rec := range recommendations {
	fmt.Printf("💡 %s: %s (Save $%.2f)\n", rec.Title, rec.Description, rec.PotentialSavings)
}
```

### 11. Resilience Patterns

```go
// Circuit Breaker
cb := sdk.NewCircuitBreaker(sdk.CircuitBreakerConfig{
	Name:         "llm_api",
	MaxFailures:  5,
	ResetTimeout: 60 * time.Second,
}, logger, metrics)

err := cb.Execute(ctx, func(ctx context.Context) error {
	return callLLM(ctx)
})

// Retry with Exponential Backoff
err = sdk.Retry(ctx, sdk.RetryConfig{
	MaxAttempts:  3,
	InitialDelay: 100 * time.Millisecond,
	MaxDelay:     5 * time.Second,
	Multiplier:   2.0,
	Jitter:       true,
}, logger, func(ctx context.Context) error {
	return unreliableOperation(ctx)
})

// Rate Limiter
limiter := sdk.NewRateLimiter("api_calls", 10, 20, logger, metrics) // 10/sec, burst 20

if limiter.Allow() {
	callAPI()
}

// Or wait for token
limiter.Wait(ctx)

// Fallback Chain
fallback := sdk.NewFallbackChain("generation", logger, metrics)
err = fallback.Execute(ctx,
	func(ctx context.Context) error { return tryPrimaryModel(ctx) },
	func(ctx context.Context) error { return trySecondaryModel(ctx) },
	func(ctx context.Context) error { return useCachedResponse(ctx) },
)

// Bulkhead (concurrency limiting)
bulkhead := sdk.NewBulkhead("heavy_ops", 5, logger, metrics) // Max 5 concurrent

bulkhead.Execute(ctx, func(ctx context.Context) error {
	return heavyOperation(ctx)
})
```

### 12. Observability & Debugging

```go
// Distributed Tracing
tracer := sdk.NewTracer(logger, metrics)
span := tracer.StartSpan(ctx, "generate_response")
span.SetTag("model", "gpt-4")
span.SetTag("user_id", "user_123")
defer span.Finish()

// Add span to context
ctx = sdk.ContextWithSpan(ctx, span)

// Log events
span.LogEvent("info", "Processing request", map[string]interface{}{
	"tokens": 1000,
})

// Performance Profiling
profiler := sdk.NewProfiler(logger, metrics)
session := profiler.StartProfile("llm_call")
// ... do work ...
session.End()

// Get profile
profile := profiler.GetProfile("llm_call")
fmt.Printf("Avg: %v, P95: %v, P99: %v\n", profile.AvgTime, profile.Percentiles[95], profile.Percentiles[99])

// Debugging
debugger := sdk.NewDebugger(logger)
debugInfo := debugger.GetDebugInfo()
fmt.Printf("Goroutines: %d\n", debugInfo.Goroutines)
fmt.Printf("Memory: %d MB\n", debugInfo.MemoryStats.Alloc/1024/1024)

// Health Checks
healthChecker := sdk.NewHealthChecker(logger)
healthChecker.RegisterCheck("llm_api", func(ctx context.Context) error {
	return pingLLMAPI(ctx)
})
healthChecker.RegisterCheck("vector_store", func(ctx context.Context) error {
	return pingVectorStore(ctx)
})

status := healthChecker.GetOverallHealth(ctx)
// Output: "healthy", "degraded", or "unhealthy"
```

---

## 🎯 Advanced Use Cases

### Complete AI Agent with All Features

```go
func buildProductionAgent() *sdk.Agent {
	// Setup components
	llmManager := setupLLMManager()
	vectorStore := setupVectorStore()
	embeddingModel := setupEmbeddingModel()
	toolRegistry := setupTools()
	
	// Create memory system
	memory := sdk.NewMemoryManager("agent_id", embeddingModel, vectorStore, logger, metrics, &sdk.MemoryManagerOptions{
		WorkingCapacity: 20,
		ShortTermTTL:    24 * time.Hour,
	})
	
	// Setup RAG
	rag := sdk.NewRAG(embeddingModel, vectorStore, logger, metrics, nil)
	
	// Index knowledge base
	rag.IndexDocument(ctx, "kb_1", knowledgeBase)
	
	// Setup guardrails
	guardrails := sdk.NewGuardrailManager(logger, metrics, &sdk.GuardrailOptions{
		EnablePII:             true,
		EnableToxicity:        true,
		EnablePromptInjection: true,
	})
	
	// Create agent
	agent, _ := sdk.NewAgent("prod_agent", "Production Agent", llmManager, stateStore, logger, metrics, &sdk.AgentOptions{
		SystemPrompt: "You are a helpful AI assistant with access to tools and knowledge.",
		Tools:        toolRegistry.ListTools(),
		Guardrails:   guardrails,
		MaxIterations: 10,
	})
	
	return agent
}
```

---

## 📊 Performance & Scale

- **Throughput**: 1000+ requests/sec (with pooling)
- **Latency**: P99 < 200ms (streaming)
- **Memory**: < 50MB base, scales linearly
- **Concurrency**: Fully thread-safe
- **Test Coverage**: 81%+

---

## 🤝 Comparison

| Feature | Vercel AI SDK | LangChain | **Forge AI SDK** |
|---------|---------------|-----------|------------------|
| Language | TypeScript | Python | **Go** |
| Type Safety | ✅ | ❌ | **✅✅ (Generics)** |
| Structured Output | Basic | Basic | **Advanced + Validation** |
| Memory System | ❌ | Basic | **4-Tier** |
| RAG | ❌ | ✅ | **✅✅ (Full Pipeline)** |
| Cost Management | ❌ | ❌ | **✅** |
| Guardrails | ❌ | ❌ | **✅** |
| Workflow Engine | ❌ | ❌ | **✅ (DAG)** |
| Tool Discovery | Manual | Manual | **Auto** |
| A/B Testing | ❌ | ❌ | **✅** |
| Resilience | Basic | ❌ | **Full Suite** |
| Production Ready | ⚠️ | ⚠️ | **✅** |

---

## 📖 Documentation

- [API Reference](./docs/api.md)
- [Architecture](./docs/architecture.md)
- [Examples](./examples/)
- [Testing Guide](./docs/testing.md)
- [Migration Guide](./docs/migration.md)

---

## 🧪 Testing

```bash
# Run all tests
go test ./...

# With coverage
go test -cover ./...

# Benchmark
go test -bench=. -benchmem ./...

# Race detection
go test -race ./...
```

---

## 🛠️ Development

```bash
# Install dependencies
go mod download

# Run linters
golangci-lint run

# Format code
go fmt ./...

# Generate docs
godoc -http=:6060
```

---

## 📝 License

MIT License - see [LICENSE](LICENSE) for details.

---

## 🌟 Star History

If this project helped you, please ⭐ star it on GitHub!

---

## 🤝 Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md).

---

## 💬 Support

- 📧 Email: support@forge.dev
- 💬 Discord: [Join our community](https://discord.gg/forge)
- 📖 Docs: [forge.dev/docs](https://forge.dev/docs)

---

**Built with ❤️ by the Forge Team**
