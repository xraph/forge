package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai"
	"github.com/xraph/forge/extensions/ai/agents"
	"github.com/xraph/forge/extensions/ai/stores"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("=== Forge v2: AI Agent Storage & API Demo ===\n")

	// Option 1: Use in-memory store (default, no database)
	memoryStore := stores.NewMemoryAgentStore()

	// Option 2: Use SQL store with SQLite (persistent)
	db, err := sql.Open("sqlite3", "agents.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Choose which store to use
	var agentStore ai.AgentStore
	useSQL := true // Set to true to use SQL store, false for memory store

	if useSQL {
		// Get logger from temporary app to create SQL store
		tempApp := forge.NewApp(forge.AppConfig{
			Name:    "temp",
			Version: "1.0.0",
		})
		var logger forge.Logger
		if l, err := tempApp.Container().Resolve("logger"); err == nil {
			if loggerImpl, ok := l.(forge.Logger); ok {
				logger = loggerImpl
			}
		}
		
		sqlStore := stores.NewSQLAgentStore(db, "agents", logger)
		
		// Create table
		if err := sqlStore.CreateTable(context.Background()); err != nil {
			log.Printf("Warning: failed to create agents table: %v", err)
		}
		
		agentStore = sqlStore
		fmt.Println("✓ Using SQL store (persistent)")
	} else {
		agentStore = memoryStore
		fmt.Println("✓ Using memory store (ephemeral)")
	}

	// Configure AI extension
	config := ai.DefaultConfig()
	config.EnableLLM = true
	config.EnableAgents = true
	config.EnableInference = false
	config.EnableCoordination = true

	// Create AI extension
	ext := ai.NewExtensionWithConfig(config)

	// Create app with AI extension
	app := forge.NewApp(forge.AppConfig{
		Name:       "ai-agents-demo",
		Version:    "1.0.0",
		Extensions: []forge.Extension{ext},
	})

	// Register agent store in DI
	if err := app.Container().Register("agentStore", func(c forge.Container) (any, error) {
		return agentStore, nil
	}); err != nil {
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
	var aiManager ai.AI
	if manager, err := app.Container().Resolve("ai.manager"); err == nil {
		if m, ok := manager.(ai.AI); ok {
			aiManager = m
		}
	}

	// Demo: Create agents dynamically
	fmt.Println("\n→ Creating Agents:")

	// Create optimization agent
	optimizationAgent := agents.NewOptimizationAgent("optimization-001", "Performance Optimizer")
	fmt.Printf("  Creating agent: %s\n", optimizationAgent.Name())
	if err := aiManager.RegisterAgent(optimizationAgent); err != nil {
		log.Printf("  Error registering agent: %v", err)
	} else {
		fmt.Printf("  ✓ Agent registered: %s\n", optimizationAgent.ID())
	}

	// Create anomaly detection agent
	anomalyAgent := agents.NewAnomalyDetectionAgent("anomaly-001", "Anomaly Detector")
	fmt.Printf("  Creating agent: %s\n", anomalyAgent.Name())
	if err := aiManager.RegisterAgent(anomalyAgent); err != nil {
		log.Printf("  Error registering agent: %v", err)
	} else {
		fmt.Printf("  ✓ Agent registered: %s\n", anomalyAgent.ID())
	}

	// Demo: List agents
	fmt.Println("\n→ Listing Agents:")
	agents := aiManager.ListAgents()
	fmt.Printf("  Found %d agents:\n", len(agents))
	for _, agent := range agents {
		fmt.Printf("    - %s (%s): %s\n", agent.ID(), agent.Type(), agent.Name())
	}

	// Demo: Create a team
	fmt.Println("\n→ Creating Agent Team:")
	team := ai.NewAgentTeam("dev-team-001", "Development Team", app.Logger())

	// Add agents to team
	agent1, err1 := aiManager.GetAgent("optimization-001")
	agent2, err2 := aiManager.GetAgent("anomaly-001")

	if err1 == nil && err2 == nil {
		team.AddAgent(agent1)
		team.AddAgent(agent2)
		fmt.Printf("  ✓ Team created with %d agents\n", len(team.Agents()))
		fmt.Println("  ✓ Team configured (teams are managed separately from individual agents)")
	} else {
		log.Printf("  Error getting agents for team: %v, %v", err1, err2)
	}

	// Demo: Persistence test (restart simulation)
	if useSQL {
		fmt.Println("\n→ Testing Persistence:")
		fmt.Println("  Agents are saved to database")
		fmt.Println("  Restart the app to see agents load from database")
		fmt.Println("  Database file: agents.db")
	}

	fmt.Println("\n→ Demo Complete!")
	fmt.Println("\nNext Steps:")
	fmt.Println("  1. Set up REST API endpoints using AgentController")
	fmt.Println("  2. Create agents via HTTP POST /agents")
	fmt.Println("  3. Execute agents via HTTP POST /agents/:id/execute")
	fmt.Println("  4. Create teams via HTTP POST /teams")
	fmt.Println("  5. Execute team tasks via HTTP POST /teams/:id/execute")
}
