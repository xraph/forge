package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/xraph/ai-sdk/llm"
	"github.com/xraph/ai-sdk/llm/providers"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai"
)

func main() {
	fmt.Println("=== Forge: AI SDK Demo ===")
	fmt.Println("Auto-bootstrapping stores from configuration")

	// Step 1: Get API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = "sk-demo-key" // Demo key (won't work for actual calls)
		fmt.Println("\n⚠️  Warning: No OPENAI_API_KEY environment variable set")
		fmt.Println("   Set it to test actual LLM calls: export OPENAI_API_KEY=your-key")
	}

	// Step 2: Create LLM manager manually (for now)
	// Note: Automatic LLM manager creation from config will be added in a future update
	llmMgr, err := llm.NewLLMManager(llm.LLMManagerConfig{
		DefaultProvider: "openai",
	})
	if err != nil {
		log.Fatalf("Failed to create LLM manager: %v", err)
	}

	// Register OpenAI provider
	openaiProvider, err := providers.NewOpenAIProvider(providers.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
	}, nil, nil)
	if err != nil {
		log.Fatalf("Failed to create OpenAI provider: %v", err)
	}
	if err := llmMgr.RegisterProvider(openaiProvider); err != nil {
		log.Fatalf("Failed to register OpenAI provider: %v", err)
	}

	fmt.Println("✓ LLM Manager created")

	// Step 3: Create Forge app
	app := forge.NewApp(forge.AppConfig{
		Name:    "ai-demo",
		Version: "1.0.0",
	})

	// Step 4: Register LLM manager in DI
	if err := app.Container().Register("llmManager", func(c forge.Container) (any, error) {
		return llmMgr, nil
	}); err != nil {
		log.Fatal(err)
	}

	// Step 5: Register AI extension (stores will auto-bootstrap from default config)
	ext := ai.NewExtension()

	fmt.Println("✓ AI Extension created")
	fmt.Println("  - LLM Manager: Manual (DI)")
	fmt.Println("  - State Store: Memory (24h TTL) - auto-configured")
	fmt.Println("  - Vector Store: Memory - auto-configured")

	if err := app.RegisterExtension(ext); err != nil {
		log.Fatal(err)
	}

	fmt.Println("✓ AI Extension registered (stores auto-bootstrapped)")

	// Step 6: Start app
	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := app.Stop(context.Background()); err != nil {
			log.Printf("Error stopping app: %v", err)
		}
	}()

	fmt.Println("✓ Application started")

	// Demo 1: Using GenerateBuilder
	fmt.Println("\n→ Demo 1: Text Generation with GenerateBuilder")
	fmt.Println("  (Skipped - requires valid API key)")
	fmt.Println("  Example:")
	fmt.Println("    result, err := ext.Generate(ctx).WithPrompt(\"Hello\").Execute()")

	// Demo 2: Using StreamBuilder
	fmt.Println("\n→ Demo 2: Streaming with StreamBuilder")
	fmt.Println("  (Skipped - requires valid API key)")
	fmt.Println("  Example:")
	fmt.Println("    result, err := ext.Stream(ctx).WithPrompt(\"Tell me a story\").Stream()")

	// Demo 3: Show specialized agents via extension
	fmt.Println("\n→ Demo 3: Specialized Agent Types Available")
	factory, err := ai.GetAgentFactory(app.Container())
	if err == nil {
		templates := factory.ListTemplates()
		fmt.Printf("  Available agent types: %d\n", len(templates))
		for _, tmpl := range templates {
			fmt.Printf("    - %s\n", tmpl)
		}
	}

	fmt.Println("\n✓ Demo Complete")
	fmt.Println("\nKey Features:")
	fmt.Println("  1. Auto-bootstrapping stores from config")
	fmt.Println("  2. Memory stores with sensible defaults (TTL, metrics)")
	fmt.Println("  3. DI override support for custom implementations")
	fmt.Println("  4. Production-ready store configs (Postgres, Redis)")
	fmt.Println("  5. Specialized agent templates (cache, scheduler, etc.)")
	fmt.Println("\nStore Configuration:")
	fmt.Println("  - Default: Memory stores with 24h TTL")
	fmt.Println("  - Production: Configure via ai.WithConfig(...)")
	fmt.Println("  - Override: Register custom stores in DI before extension")
	fmt.Println("\nNext Steps:")
	fmt.Println("  1. Set OPENAI_API_KEY to test LLM calls")
	fmt.Println("  2. Try PostgreSQL: Use ai.WithConfig for StateStore/VectorStore")
	fmt.Println("  3. Create agents via factory or REST API")
	fmt.Println("  4. Explore specialized agent types")
}
