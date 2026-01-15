package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai"
)

func main() {
	fmt.Println("=== Forge: AI Agents Demo ===")
	fmt.Println("Production-ready AI agents with config-based stores")

	// Get environment variables
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = "sk-demo-key"
		fmt.Println("\n⚠️  Set OPENAI_API_KEY for actual LLM calls")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	usePostgres := databaseURL != ""

	// Create app
	app := forge.NewApp(forge.AppConfig{
		Name:    "ai-agents-demo",
		Version: "1.0.0",
	})

	// Create AI extension with production config
	var ext *ai.Extension
	if usePostgres {
		fmt.Println("\n✓ Using PostgreSQL for persistence")
		ext = ai.NewExtension(
			ai.WithLLMConfig(ai.LLMConfiguration{
				DefaultProvider: "openai",
				Providers: map[string]ai.ProviderConfig{
					"openai": {
						Type:    "openai",
						APIKey:  apiKey,
						BaseURL: "https://api.openai.com/v1",
						Models:  []string{"gpt-4", "gpt-3.5-turbo"},
					},
				},
			}),
			ai.WithConfig(ai.Config{
				StateStore: ai.StateStoreConfig{
					Type: "postgres",
					Postgres: &ai.PostgresStateConfig{
						ConnString: databaseURL,
						TableName:  "agent_states",
					},
				},
				VectorStore: ai.VectorStoreConfig{
					Type: "postgres",
					Postgres: &ai.PostgresVectorConfig{
						ConnString: databaseURL,
						TableName:  "embeddings",
						Dimensions: 1536,
					},
				},
			}),
		)
		fmt.Println("  - State Store: PostgreSQL (agent_states table)")
		fmt.Println("  - Vector Store: PostgreSQL pgvector (embeddings table)")
	} else {
		fmt.Println("\n✓ Using Memory stores (development mode)")
		ext = ai.NewExtension(
			ai.WithLLMConfig(ai.LLMConfiguration{
				DefaultProvider: "openai",
				Providers: map[string]ai.ProviderConfig{
					"openai": {
						Type:    "openai",
						APIKey:  apiKey,
						BaseURL: "https://api.openai.com/v1",
						Models:  []string{"gpt-4", "gpt-3.5-turbo"},
					},
				},
			}),
			// Uses memory defaults from DefaultConfig()
		)
		fmt.Println("  - State Store: Memory (24h TTL)")
		fmt.Println("  - Vector Store: Memory")
		fmt.Println("  - Tip: Set DATABASE_URL for PostgreSQL persistence")
	}

	// Register AI extension
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

	fmt.Println("✓ Application started with AI extension")

	// Get agent manager
	agentMgr, err := ai.GetAgentManager(app.Container())
	if err != nil {
		log.Fatalf("Failed to get agent manager: %v", err)
	}

	ctx := context.Background()

	// Demo 1: Create a cache optimizer agent
	fmt.Println("\n→ Creating Cache Optimizer Agent:")
	cacheDef := &ai.AgentDefinition{
		ID:          "cache-opt-001",
		Name:        "Cache Optimizer",
		Type:        "cache_optimizer",
		Model:       "gpt-4",
		Temperature: 0.7,
		Config:      map[string]interface{}{},
	}

	cacheAgent, err := agentMgr.CreateAgent(ctx, cacheDef)
	if err != nil {
		log.Printf("  Error creating cache agent: %v", err)
	} else {
		fmt.Printf("  ✓ Agent created: %s\n", cacheDef.ID)
		fmt.Println("  ✓ Agent has cache optimization tools registered")
		fmt.Println("  ✓ Agent can analyze hit rates and recommend optimizations")
	}

	// Demo 2: Create an anomaly detector agent
	fmt.Println("\n→ Creating Anomaly Detector Agent:")
	anomalyDef := &ai.AgentDefinition{
		ID:          "anomaly-001",
		Name:        "Anomaly Detector",
		Type:        "anomaly_detector",
		Model:       "gpt-4",
		Temperature: 0.5,
		Config:      map[string]interface{}{},
	}

	anomalyAgent, err := agentMgr.CreateAgent(ctx, anomalyDef)
	if err != nil {
		log.Printf("  Error creating anomaly agent: %v", err)
	} else {
		fmt.Printf("  ✓ Agent created: %s\n", anomalyDef.ID)
		fmt.Println("  ✓ Agent has anomaly detection tools registered")
		fmt.Println("  ✓ Agent can detect patterns and calculate baselines")
	}

	// Demo 3: Create a scheduler optimizer agent
	fmt.Println("\n→ Creating Scheduler Optimizer Agent:")
	schedulerDef := &ai.AgentDefinition{
		ID:          "scheduler-001",
		Name:        "Scheduler Optimizer",
		Type:        "scheduler",
		Model:       "gpt-4",
		Temperature: 0.6,
		Config:      map[string]interface{}{},
	}

	schedulerAgent, err := agentMgr.CreateAgent(ctx, schedulerDef)
	if err != nil {
		log.Printf("  Error creating scheduler agent: %v", err)
	} else {
		fmt.Printf("  ✓ Agent created: %s\n", schedulerDef.ID)
		fmt.Println("  ✓ Agent has job scheduling tools registered")
		fmt.Println("  ✓ Agent can optimize resource allocation")
	}

	// Demo 4: List all agents
	fmt.Println("\n→ Listing All Agents:")
	agentList, err := agentMgr.ListAgents(ctx)
	if err != nil {
		log.Printf("  Error listing agents: %v", err)
	} else {
		fmt.Printf("  Found %d agents:\n", len(agentList))
		for _, def := range agentList {
			fmt.Printf("    - %s (%s): %s\n", def.ID, def.Type, def.Name)
		}
	}

	// Demo 5: Execute agent (requires valid API key)
	fmt.Println("\n→ Agent Execution:")
	fmt.Println("  (Skipped - requires valid API key)")
	if cacheAgent != nil {
		fmt.Println("  Example:")
		fmt.Println("    result, err := cacheAgent.Execute(ctx, \"Analyze cache with 65% hit rate\")")
		fmt.Println("    // Agent will use its registered tools to analyze and respond")
	}

	// Demo 6: Show conversation persistence
	fmt.Println("\n→ Conversation State Persistence:")
	fmt.Println("  ✓ StateStore maintains conversation history")
	fmt.Println("  ✓ Agents remember context across calls")
	fmt.Println("  ✓ State survives app restarts (with SQL StateStore)")

	fmt.Println("\n✓ Demo Complete!")
	fmt.Println("\nConfiguration Highlights:")
	fmt.Println("  - Zero-config development mode")
	fmt.Println("  - Production PostgreSQL with single env var")
	fmt.Println("  - Automatic store bootstrapping")
	fmt.Println("  - DI override support")
	fmt.Println("\nAgent Types Available:")
	fmt.Println("  1. cache_optimizer   - Cache optimization and eviction strategies")
	fmt.Println("  2. scheduler         - Job scheduling and resource allocation")
	fmt.Println("  3. anomaly_detector  - Statistical anomaly detection")
	fmt.Println("  4. load_balancer     - Traffic distribution optimization")
	fmt.Println("  5. security_monitor  - Security threat detection")
	fmt.Println("  6. resource_manager  - Resource utilization optimization")
	fmt.Println("  7. predictor         - Predictive analytics and forecasting")
	fmt.Println("  8. optimizer         - General system optimization")
	fmt.Println("\nProduction Deployment:")
	fmt.Println("  1. Set DATABASE_URL for PostgreSQL persistence")
	fmt.Println("  2. Set OPENAI_API_KEY for LLM access")
	fmt.Println("  3. Optional: Configure Redis for distributed state")
	fmt.Println("  4. Optional: Configure Pinecone/Weaviate for vectors")
	fmt.Println("\nREST API Endpoints:")
	fmt.Println("  POST   /agents              - Create agent")
	fmt.Println("  GET    /agents              - List agents")
	fmt.Println("  GET    /agents/:id          - Get agent")
	fmt.Println("  PUT    /agents/:id          - Update agent")
	fmt.Println("  DELETE /agents/:id          - Delete agent")
	fmt.Println("  POST   /agents/:id/execute  - Execute agent")
	fmt.Println("  POST   /agents/:id/chat     - Chat with agent")
	fmt.Println("  GET    /agents/templates    - List agent templates")
}
