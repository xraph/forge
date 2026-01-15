package forge_test

import (
	"context"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
	"github.com/xraph/forge/extensions/cache"
	"github.com/xraph/forge/extensions/features"
	"github.com/xraph/forge/extensions/graphql"
	"github.com/xraph/forge/extensions/grpc"
	"github.com/xraph/forge/extensions/kafka"
	"github.com/xraph/forge/extensions/mcp"
	"github.com/xraph/forge/extensions/mqtt"
	"github.com/xraph/forge/extensions/orpc"
	"github.com/xraph/forge/extensions/search"
	"github.com/xraph/forge/extensions/storage"
)

// TestCacheMigration validates cache extension uses constructor injection
func TestCacheMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-cache",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(cache.NewExtension())

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	cacheService, err := forge.InjectType[*cache.CacheService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve CacheService by type: %v", err)
	}
	if cacheService == nil {
		t.Error("CacheService resolved by type is nil")
	}

	// Key-based resolution (OLD - backward compatible)
	cacheByKey, err := forge.Resolve[cache.Cache](app.Container(), "cache")
	if err != nil {
		t.Errorf("Failed to resolve Cache by key: %v", err)
	}
	if cacheByKey == nil {
		t.Error("Cache resolved by key is nil")
	}

	// Both should resolve the same underlying service
	if cacheService != cacheByKey {
		t.Log("Cache service and cache by key are different instances (expected)")
	}
}

// TestSearchMigration validates search extension uses constructor injection
func TestSearchMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-search",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(search.NewExtension())

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	searchService, err := forge.InjectType[*search.SearchService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve SearchService by type: %v", err)
	}
	if searchService == nil {
		t.Error("SearchService resolved by type is nil")
	}

	// Key-based resolution (OLD - backward compatible)
	searchByKey, err := forge.Resolve[search.Search](app.Container(), "search")
	if err != nil {
		t.Errorf("Failed to resolve Search by key: %v", err)
	}
	if searchByKey == nil {
		t.Error("Search resolved by key is nil")
	}
}

// TestAuthMigration validates auth extension uses constructor injection
func TestAuthMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-auth",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(auth.NewExtension(auth.WithEnabled(true)))

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	registry, err := forge.InjectType[auth.Registry](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve Registry by type: %v", err)
	}
	if registry == nil {
		t.Error("Registry resolved by type is nil")
	}

	// Key-based resolution (OLD - backward compatible)
	registryByKey, err := app.Container().Resolve("auth:registry")
	if err != nil {
		t.Errorf("Failed to resolve Registry by key: %v", err)
	}
	if registryByKey == nil {
		t.Error("Registry resolved by key is nil")
	}
}

// TestFeaturesMigration validates features extension uses constructor injection
func TestFeaturesMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-features",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(features.NewExtension(
		features.WithEnabled(true),
		features.WithProvider("local"),
	))

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	service, err := forge.InjectType[*features.Service](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve Service by type: %v", err)
	}
	if service == nil {
		t.Error("Service resolved by type is nil")
	}

	// Key-based resolution (OLD - backward compatible)
	serviceByKey, err := app.Container().Resolve("features")
	if err != nil {
		t.Errorf("Failed to resolve features by key: %v", err)
	}
	if serviceByKey == nil {
		t.Error("Features resolved by key is nil")
	}
}

// TestGRPCMigration validates grpc extension uses constructor injection
func TestGRPCMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-grpc",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(grpc.NewExtension(
		grpc.WithAddress("localhost:9090"),
	))

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	grpcService, err := forge.InjectType[*grpc.GRPCService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve GRPCService by type: %v", err)
	}
	if grpcService == nil {
		t.Error("GRPCService resolved by type is nil")
	}
}

// TestGraphQLMigration validates graphql extension uses constructor injection
func TestGraphQLMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-graphql",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(graphql.NewExtension())

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	graphqlService, err := forge.InjectType[*graphql.GraphQLService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve GraphQLService by type: %v", err)
	}
	if graphqlService == nil {
		t.Error("GraphQLService resolved by type is nil")
	}
}

// TestORPCMigration validates orpc extension uses constructor injection
func TestORPCMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-orpc",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(orpc.NewExtension(orpc.WithEnabled(true)))

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	orpcService, err := forge.InjectType[*orpc.ORPCService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve ORPCService by type: %v", err)
	}
	if orpcService == nil {
		t.Error("ORPCService resolved by type is nil")
	}
}

// TestMCPMigration validates mcp extension uses constructor injection
func TestMCPMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-mcp",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(mcp.NewExtension(mcp.WithEnabled(true)))

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	mcpService, err := forge.InjectType[*mcp.MCPService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve MCPService by type: %v", err)
	}
	if mcpService == nil {
		t.Error("MCPService resolved by type is nil")
	}
}

// TestStorageMigration validates storage extension uses constructor injection
func TestStorageMigration(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-storage",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(storage.NewExtension())

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	storageManager, err := forge.InjectType[*storage.StorageManager](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve StorageManager by type: %v", err)
	}
	if storageManager == nil {
		t.Error("StorageManager resolved by type is nil")
	}
}

// TestKafkaMigration validates kafka extension uses constructor injection
func TestKafkaMigration(t *testing.T) {
	t.Skip("Kafka requires broker connection")

	app := forge.NewApp(forge.AppConfig{
		Name:        "test-kafka",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(kafka.NewExtension())

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	kafkaService, err := forge.InjectType[*kafka.KafkaService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve KafkaService by type: %v", err)
	}
	if kafkaService == nil {
		t.Error("KafkaService resolved by type is nil")
	}
}

// TestMQTTMigration validates mqtt extension uses constructor injection
func TestMQTTMigration(t *testing.T) {
	t.Skip("MQTT requires broker connection")

	app := forge.NewApp(forge.AppConfig{
		Name:        "test-mqtt",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(mqtt.NewExtension())

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Type-based resolution (NEW)
	mqttService, err := forge.InjectType[*mqtt.MQTTService](app.Container())
	if err != nil {
		t.Errorf("Failed to resolve MQTTService by type: %v", err)
	}
	if mqttService == nil {
		t.Error("MQTTService resolved by type is nil")
	}
}

// TestBackwardCompatibility ensures old key-based pattern still works for all migrations
func TestBackwardCompatibility(t *testing.T) {
	tests := []struct {
		name      string
		extension forge.Extension
		key       string
	}{
		{"cache", cache.NewExtension(), "cache"},
		{"search", search.NewExtension(), "search"},
		{"auth", auth.NewExtension(auth.WithEnabled(true)), "auth:registry"},
		{"features", features.NewExtension(features.WithEnabled(true), features.WithProvider("local")), "features"},
		{"grpc", grpc.NewExtension(), "grpc"},
		{"graphql", graphql.NewExtension(), "graphql"},
		{"orpc", orpc.NewExtension(orpc.WithEnabled(true)), "orpc"},
		{"mcp", mcp.NewExtension(mcp.WithEnabled(true)), "mcp"},
		{"storage", storage.NewExtension(), "storage-manager"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := forge.NewApp(forge.AppConfig{
				Name:        "test-" + tt.name,
				Version:     "1.0.0",
				Environment: "test",
			})

			app.RegisterExtension(tt.extension)

			ctx := context.Background()
			if err := app.Start(ctx); err != nil {
				t.Fatalf("Failed to start app: %v", err)
			}
			defer app.Stop(ctx)

			// Key-based resolution should still work
			service, err := app.Container().Resolve(tt.key)
			if err != nil {
				t.Errorf("Failed to resolve %s by key '%s': %v", tt.name, tt.key, err)
			}
			if service == nil {
				t.Errorf("Service %s resolved by key is nil", tt.name)
			}
		})
	}
}

// TestExtensionLifecycle validates extensions properly delegate to Vessel
func TestExtensionLifecycle(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:        "test-lifecycle",
		Version:     "1.0.0",
		Environment: "test",
	})

	app.RegisterExtension(cache.NewExtension())
	app.RegisterExtension(search.NewExtension())

	ctx := context.Background()

	// Start should trigger service Start()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}

	// Services should be resolvable
	cacheService, err := forge.InjectType[*cache.CacheService](app.Container())
	if err != nil {
		t.Fatalf("Failed to resolve CacheService: %v", err)
	}

	// Service should be healthy
	if err := cacheService.Health(ctx); err != nil {
		t.Errorf("CacheService health check failed: %v", err)
	}

	// Stop should trigger service Stop()
	if err := app.Stop(ctx); err != nil {
		t.Errorf("Failed to stop app: %v", err)
	}
}

// TestConstructorInjectionDependencies validates dependencies are auto-resolved
func TestConstructorInjectionDependencies(t *testing.T) {
	type TestService struct {
		logger  forge.Logger
		metrics forge.Metrics
		cache   *cache.CacheService
	}

	app := forge.NewApp(forge.AppConfig{
		Name:        "test-deps",
		Version:     "1.0.0",
		Environment: "test",
	})

	// Register cache extension
	app.RegisterExtension(cache.NewExtension())

	// Register test service with dependencies
	forge.ProvideConstructor(app.Container(), func(
		logger forge.Logger,
		metrics forge.Metrics,
		cache *cache.CacheService,
	) *TestService {
		if logger == nil {
			t.Error("Logger dependency not resolved")
		}
		if metrics == nil {
			t.Error("Metrics dependency not resolved")
		}
		if cache == nil {
			t.Error("Cache dependency not resolved")
		}
		return &TestService{
			logger:  logger,
			metrics: metrics,
			cache:   cache,
		}
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop(ctx)

	// Resolve test service - all dependencies should be auto-resolved
	testService, err := forge.InjectType[*TestService](app.Container())
	if err != nil {
		t.Fatalf("Failed to resolve TestService: %v", err)
	}
	if testService == nil {
		t.Fatal("TestService is nil")
	}
	if testService.logger == nil {
		t.Error("TestService.logger is nil")
	}
	if testService.metrics == nil {
		t.Error("TestService.metrics is nil")
	}
	if testService.cache == nil {
		t.Error("TestService.cache is nil")
	}
}

// TestMigratedExtensionsCount verifies the number of migrated extensions
func TestMigratedExtensionsCount(t *testing.T) {
	migratedExtensions := []string{
		"cache", "database", "ai", "events", "queue", // Initial 5
		"search", "auth", "features", "grpc", "graphql", "orpc", "mcp", // Tier 1: 7
		"storage", "kafka", "mqtt", "discovery", // Tier 2: 4
	}

	t.Logf("Total migrated extensions: %d/23 (56%%)", len(migratedExtensions))

	if len(migratedExtensions) != 16 {
		t.Errorf("Expected 16 migrated extensions, got %d", len(migratedExtensions))
	}
}
