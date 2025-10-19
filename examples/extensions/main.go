package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/pkg/extensions"
	"github.com/xraph/forge/pkg/extensions/ai"
	"github.com/xraph/forge/pkg/extensions/cache"
	"github.com/xraph/forge/pkg/extensions/consensus"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
)

func main() {
	// Create a simple Forge application
	app, err := forge.NewSimpleApplication(forge.SimpleApplicationConfig{
		Name:        "extensions-example",
		Version:     "1.0.0",
		Logger:      logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:     metrics.NewMockMetricsCollector(),
		Port:        8080,
		Description: "Extensions example",
	})
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}
	// Start the core application
	if err := app.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

	// Create extension manager
	extensionManager := extensions.NewExtensionManager(extensions.ExtensionConfig{
		AutoLoad:            true,
		LoadTimeout:         30 * time.Second,
		UnloadTimeout:       10 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		Logger:              app.Logger(),
		Metrics:             app.Metrics(),
	})

	// Create extension registry
	extensionRegistry := extensions.NewExtensionRegistry(extensions.RegistryConfig{
		DiscoveryInterval: 60 * time.Second,
		AutoLoad:          true,
		LoadTimeout:       30 * time.Second,
		Logger:            app.Logger(),
		Metrics:           app.Metrics(),
	})

	// Register extension manager with registry
	if err := extensionRegistry.RegisterManager("default", extensionManager); err != nil {
		log.Fatalf("Failed to register extension manager: %v", err)
	}

	// Create and register AI extension
	aiExtension := ai.NewAIExtension(ai.AIConfig{
		EnableLLM:          true,
		EnableAgents:       true,
		EnableInference:    true,
		EnableCoordination: true,
		EnableTraining:     false, // Disable training for this example
		MaxConcurrency:     10,
		RequestTimeout:     30 * time.Second,
		CacheSize:          1000,
		Logger:             app.Logger(),
		Metrics:            app.Metrics(),
	})

	if err := extensionManager.RegisterExtension(aiExtension); err != nil {
		log.Fatalf("Failed to register AI extension: %v", err)
	}

	// Create and register Cache extension
	cacheExtension := cache.NewCacheExtension(cache.CacheConfig{
		EnableMemory:      true,
		EnableRedis:       false, // Disable Redis for this example
		EnableDistributed: false,
		EnableReplication: false,
		EnableSharding:    false,
		EnableWarming:     false,
		EnableMonitoring:  true,
		DefaultTTL:        1 * time.Hour,
		MaxSize:           1000000,
		MemorySize:        100000,
		Logger:            app.Logger(),
		Metrics:           app.Metrics(),
	})

	if err := extensionManager.RegisterExtension(cacheExtension); err != nil {
		log.Fatalf("Failed to register Cache extension: %v", err)
	}

	// Create and register Consensus extension
	consensusExtension := consensus.NewConsensusExtension(consensus.ConsensusConfig{
		EnableRaft:        true,
		EnableElection:    true,
		EnableDiscovery:   true,
		EnableStorage:     true,
		EnableTransport:   true,
		EnableMonitoring:  true,
		ClusterSize:       3,
		ElectionTimeout:   5 * time.Second,
		HeartbeatInterval: 1 * time.Second,
		StoragePath:       "./consensus",
		TransportPort:     8080,
		Logger:            app.Logger(),
		Metrics:           app.Metrics(),
	})

	if err := extensionManager.RegisterExtension(consensusExtension); err != nil {
		log.Fatalf("Failed to register Consensus extension: %v", err)
	}

	// Start extension registry
	if err := extensionRegistry.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start extension registry: %v", err)
	}

	// Demonstrate extension capabilities
	demonstrateExtensions(extensionManager, aiExtension, cacheExtension, consensusExtension)

	// Wait for shutdown signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start health check routine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				performHealthChecks(extensionManager, extensionRegistry, app)
			case <-shutdown:
				return
			}
		}
	}()

	// Wait for shutdown signal
	<-shutdown
	fmt.Println("\nShutting down...")

	// Stop extension registry
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := extensionRegistry.Stop(ctx); err != nil {
		log.Printf("Error stopping extension registry: %v", err)
	}

	// Stop the application
	if err := app.Stop(ctx); err != nil {
		log.Printf("Error stopping application: %v", err)
	}

	fmt.Println("Application stopped")
}

func demonstrateExtensions(
	manager *extensions.ExtensionManager,
	aiExt *ai.AIExtension,
	cacheExt *cache.CacheExtension,
	consensusExt *consensus.ConsensusExtension,
) {
	fmt.Println("=== Extension Framework Demo ===")

	// List all extensions
	extensions := manager.ListExtensions()
	fmt.Printf("Registered extensions: %d\n", len(extensions))
	for _, ext := range extensions {
		fmt.Printf("- %s v%s: %s\n", ext.Name, ext.Version, ext.Description)
		fmt.Printf("  Capabilities: %v\n", ext.Capabilities)
		fmt.Printf("  Dependencies: %v\n", ext.Dependencies)
		fmt.Printf("  Status: %s\n", ext.Status)
	}

	// Demonstrate AI extension
	fmt.Println("\n=== AI Extension Demo ===")
	fmt.Printf("LLM Enabled: %v\n", aiExt.IsLLMEnabled())
	fmt.Printf("Agents Enabled: %v\n", aiExt.IsAgentsEnabled())
	fmt.Printf("Inference Enabled: %v\n", aiExt.IsInferenceEnabled())
	fmt.Printf("Coordination Enabled: %v\n", aiExt.IsCoordinationEnabled())
	fmt.Printf("Training Enabled: %v\n", aiExt.IsTrainingEnabled())

	// Demonstrate Cache extension
	fmt.Println("\n=== Cache Extension Demo ===")
	fmt.Printf("Memory Cache Enabled: %v\n", cacheExt.IsMemoryEnabled())
	fmt.Printf("Redis Cache Enabled: %v\n", cacheExt.IsRedisEnabled())
	fmt.Printf("Distributed Cache Enabled: %v\n", cacheExt.IsDistributedEnabled())
	fmt.Printf("Replication Enabled: %v\n", cacheExt.IsReplicationEnabled())
	fmt.Printf("Sharding Enabled: %v\n", cacheExt.IsShardingEnabled())
	fmt.Printf("Default TTL: %v\n", cacheExt.GetDefaultTTL())
	fmt.Printf("Max Size: %d\n", cacheExt.GetMaxSize())

	// Demonstrate Consensus extension
	fmt.Println("\n=== Consensus Extension Demo ===")
	fmt.Printf("Raft Enabled: %v\n", consensusExt.IsRaftEnabled())
	fmt.Printf("Election Enabled: %v\n", consensusExt.IsElectionEnabled())
	fmt.Printf("Discovery Enabled: %v\n", consensusExt.IsDiscoveryEnabled())
	fmt.Printf("Storage Enabled: %v\n", consensusExt.IsStorageEnabled())
	fmt.Printf("Transport Enabled: %v\n", consensusExt.IsTransportEnabled())
	fmt.Printf("Monitoring Enabled: %v\n", consensusExt.IsMonitoringEnabled())
	fmt.Printf("Cluster Size: %d\n", consensusExt.GetClusterSize())
	fmt.Printf("Election Timeout: %v\n", consensusExt.GetElectionTimeout())
	fmt.Printf("Heartbeat Interval: %v\n", consensusExt.GetHeartbeatInterval())

	// Get extension statistics
	fmt.Println("\n=== Extension Statistics ===")

	aiStats := aiExt.GetStats()
	fmt.Printf("AI Extension Stats: %+v\n", aiStats)

	cacheStats := cacheExt.GetStats()
	fmt.Printf("Cache Extension Stats: %+v\n", cacheStats)

	consensusStats := consensusExt.GetStats()
	fmt.Printf("Consensus Extension Stats: %+v\n", consensusStats)
}

func performHealthChecks(
	manager *extensions.ExtensionManager,
	registry *extensions.ExtensionRegistry,
	app *forge.SimpleForgeApplication,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check extension manager health
	if err := manager.HealthCheck(ctx); err != nil {
		fmt.Printf("Extension manager health check failed: %v\n", err)
	} else {
		fmt.Println("Extension manager health check passed")
	}

	// Check extension registry health
	if err := registry.HealthCheck(ctx); err != nil {
		fmt.Printf("Extension registry health check failed: %v\n", err)
	} else {
		fmt.Println("Extension registry health check passed")
	}
}
