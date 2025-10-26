package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai"
)

func main() {
	fmt.Println("=== Forge v2: AI Extension Demo ===")

	// Create custom config
	config := ai.DefaultConfig()

	// Configure LLM providers
	config.LLM.Providers["openai"] = ai.ProviderConfig{
		Type:   "openai",
		APIKey: "sk-...", // Replace with actual key
		Models: []string{"gpt-4", "gpt-3.5-turbo"},
	}

	config.LLM.Providers["ollama"] = ai.ProviderConfig{
		Type:    "ollama",
		BaseURL: "http://localhost:11434",
		Models:  []string{"llama2", "mistral"},
	}

	config.LLM.DefaultProvider = "ollama" // Use local by default

	// Enable all features
	config.EnableLLM = true
	config.EnableAgents = true
	config.EnableInference = true
	config.EnableCoordination = true

	// Configure inference
	config.Inference.Workers = 4
	config.Inference.BatchSize = 10
	config.Inference.EnableBatching = true
	config.Inference.EnableCaching = true
	config.Inference.EnableScaling = true

	// Configure agents
	config.Agents.EnabledAgents = []string{
		"optimization",
		"security",
		"anomaly_detection",
	}

	// Create app
	app := forge.NewApp(forge.AppConfig{
		Name:    "ai-demo",
		Version: "1.0.0",
	})

	// Register AI extension
	ext := ai.NewExtensionWithConfig(config)
	if err := app.RegisterExtension(ext); err != nil {
		log.Fatal(err)
	}

	// Start app
	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := app.Stop(context.Background()); err != nil {
			log.Printf("Error stopping app: %v", err)
		}
	}()

	// Get AI manager
	aiManager := forge.Must[ai.AI](app.Container(), "ai.manager")

	// Display configuration
	fmt.Println("✓ AI Extension Initialized")
	fmt.Printf("  LLM Enabled: %v\n", config.EnableLLM)
	fmt.Printf("  Agents Enabled: %v\n", config.EnableAgents)
	fmt.Printf("  Inference Enabled: %v\n", config.EnableInference)
	fmt.Printf("  Coordination Enabled: %v\n", config.EnableCoordination)
	fmt.Printf("  Max Concurrency: %d\n", config.MaxConcurrency)
	fmt.Printf("  Request Timeout: %v\n", config.RequestTimeout)

	// Display stats
	fmt.Println("\n→ AI Manager Statistics:")
	stats := aiManager.GetStats()
	for key, value := range stats {
		fmt.Printf("  %s: %v\n", key, value)
	}

	// Test health check
	fmt.Println("\n→ Health Check:")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := aiManager.HealthCheck(ctx); err != nil {
		fmt.Printf("  ✗ Health check failed: %v\n", err)
	} else {
		fmt.Println("  ✓ All systems operational")
	}

	// Test LLM (if available)
	if config.EnableLLM {
		fmt.Println("\n→ Testing LLM Provider:")
		fmt.Println("  Note: LLM functionality requires actual API keys or running Ollama")
		fmt.Println("  Skipping actual LLM call in demo")
		// To actually test:
		// llmManager := aiManager.LLM()
		// response, err := llmManager.Chat(ctx, ai.ChatRequest{...})
	}

	// Test Agents (if available)
	if config.EnableAgents {
		fmt.Println("\n→ Registered Agents:")
		if len(config.Agents.EnabledAgents) > 0 {
			for _, agentName := range config.Agents.EnabledAgents {
				fmt.Printf("  - %s\n", agentName)
			}
		} else {
			fmt.Println("  No agents configured")
		}
	}

	// Test Inference Engine (if available)
	if config.EnableInference {
		fmt.Println("\n→ Inference Engine Configuration:")
		fmt.Printf("  Workers: %d\n", config.Inference.Workers)
		fmt.Printf("  Batch Size: %d\n", config.Inference.BatchSize)
		fmt.Printf("  Batching: %v\n", config.Inference.EnableBatching)
		fmt.Printf("  Caching: %v\n", config.Inference.EnableCaching)
		fmt.Printf("  Auto-scaling: %v\n", config.Inference.EnableScaling)
	}

	fmt.Println("\n✓ Demo Complete")
	fmt.Println("\nNext Steps:")
	fmt.Println("  1. Configure LLM providers with actual API keys")
	fmt.Println("  2. Load and deploy ML models")
	fmt.Println("  3. Configure and enable AI agents")
	fmt.Println("  4. Set up training pipelines")
	fmt.Println("  5. Enable multi-agent coordination")
}
