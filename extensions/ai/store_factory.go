package ai

import (
	"context"
	"fmt"

	aisdk "github.com/xraph/ai-sdk"
	"github.com/xraph/ai-sdk/llm"

	memory "github.com/xraph/ai-sdk/integrations/statestores/memory"
	postgres "github.com/xraph/ai-sdk/integrations/statestores/postgres"
	redis "github.com/xraph/ai-sdk/integrations/statestores/redis"

	vmemory "github.com/xraph/ai-sdk/integrations/vectorstores/memory"
	// vpostgres "github.com/xraph/ai-sdk/integrations/vectorstores/postgres"
	// vpinecone "github.com/xraph/ai-sdk/integrations/vectorstores/pinecone"
	// vweaviate "github.com/xraph/ai-sdk/integrations/vectorstores/weaviate"

	"github.com/xraph/forge"
)

// CreateStateStore creates a StateStore from configuration.
func CreateStateStore(ctx context.Context, cfg StateStoreConfig, logger forge.Logger, metrics forge.Metrics) (aisdk.StateStore, error) {
	switch cfg.Type {
	case "memory", "":
		memCfg := memory.Config{
			Logger:  logger,
			Metrics: metrics,
		}
		if cfg.Memory != nil {
			memCfg.TTL = cfg.Memory.TTL
		}
		return memory.NewMemoryStateStore(memCfg), nil

	case "postgres":
		if cfg.Postgres == nil {
			return nil, fmt.Errorf("postgres config required when type=postgres")
		}
		return postgres.NewPostgresStateStore(ctx, postgres.Config{
			ConnString: cfg.Postgres.ConnString,
			TableName:  cfg.Postgres.TableName,
			Logger:     logger,
			Metrics:    metrics,
		})

	case "redis":
		if cfg.Redis == nil {
			return nil, fmt.Errorf("redis config required when type=redis")
		}
		return redis.NewRedisStateStore(ctx, redis.Config{
			Addrs:    cfg.Redis.Addrs,
			Password: cfg.Redis.Password,
			Logger:   logger,
			Metrics:  metrics,
		})

	default:
		return nil, fmt.Errorf("unknown state store type: %s", cfg.Type)
	}
}

// CreateVectorStore creates a VectorStore from configuration.
func CreateVectorStore(ctx context.Context, cfg VectorStoreConfig, logger forge.Logger, metrics forge.Metrics) (aisdk.VectorStore, error) {
	switch cfg.Type {
	case "memory", "":
		return vmemory.NewMemoryVectorStore(vmemory.Config{
			Logger:  logger,
			Metrics: metrics,
		}), nil

	case "postgres":
		if cfg.Postgres == nil {
			return nil, fmt.Errorf("postgres config required when type=postgres")
		}
		// TODO: Uncomment when postgres vector store is available
		// return vpostgres.NewPostgresVectorStore(ctx, vpostgres.Config{
		//     ConnString: cfg.Postgres.ConnString,
		//     TableName:  cfg.Postgres.TableName,
		//     Dimensions: cfg.Postgres.Dimensions,
		//     Logger:     logger,
		//     Metrics:    metrics,
		// })
		return nil, fmt.Errorf("postgres vector store not yet implemented")

	case "pinecone":
		if cfg.Pinecone == nil {
			return nil, fmt.Errorf("pinecone config required when type=pinecone")
		}
		// TODO: Uncomment when Pinecone vector store is available in ai-sdk
		// return vpinecone.NewPineconeVectorStore(ctx, vpinecone.Config{
		//     APIKey:      cfg.Pinecone.APIKey,
		//     Environment: cfg.Pinecone.Environment,
		//     IndexName:   cfg.Pinecone.IndexName,
		//     Logger:      logger,
		//     Metrics:     metrics,
		// })
		return nil, fmt.Errorf("pinecone vector store not yet implemented in ai-sdk")

	case "weaviate":
		if cfg.Weaviate == nil {
			return nil, fmt.Errorf("weaviate config required when type=weaviate")
		}
		// TODO: Uncomment when Weaviate vector store is available in ai-sdk
		// return vweaviate.NewWeaviateVectorStore(ctx, vweaviate.Config{
		//     Scheme:    cfg.Weaviate.Scheme,
		//     Host:      cfg.Weaviate.Host,
		//     APIKey:    cfg.Weaviate.APIKey,
		//     ClassName: cfg.Weaviate.ClassName,
		//     Logger:    logger,
		//     Metrics:   metrics,
		// })
		return nil, fmt.Errorf("weaviate vector store not yet implemented in ai-sdk")

	default:
		return nil, fmt.Errorf("unknown vector store type: %s", cfg.Type)
	}
}

// CreateLLMManager creates an LLM Manager from configuration.
// Note: Due to circular dependency issues in ai-sdk v0.0.2, automatic provider registration
// from config is not yet fully supported. The LLM manager is created, but providers must be
// registered manually via DI or by calling RegisterProvider directly.
//
// This will be fully implemented once ai-sdk resolves the circular dependency issue.
//
// Returns *llm.LLMManager which can be type-asserted to aisdk.LLMManager when needed.
func CreateLLMManager(cfg LLMConfiguration, logger forge.Logger, metrics forge.Metrics) (aisdk.LLMManager, error) {
	// Create LLM manager with default provider
	llmMgr, err := llm.NewLLMManager(llm.LLMManagerConfig{
		DefaultProvider: cfg.DefaultProvider,
		MaxRetries:      cfg.MaxRetries,
		RetryDelay:      cfg.RetryDelay,
		Timeout:         cfg.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM manager: %w", err)
	}

	// Note: Provider registration from config is temporarily disabled due to ai-sdk circular dependency
	// Users should register providers manually via DI as shown in examples/ai-demo/main.go
	if len(cfg.Providers) > 0 {
		logger.Info("LLM manager created - providers must be registered manually",
			forge.F("default_provider", cfg.DefaultProvider),
			forge.F("configured_providers", len(cfg.Providers)),
			forge.F("note", "automatic provider registration will be enabled in future ai-sdk version"))
	} else {
		logger.Warn("no LLM providers configured - LLM manager created but has no providers")
	}

	// Return as interface - the concrete type *llm.LLMManager will be used
	// Note: This returns the concrete type which may not fully implement aisdk.LLMManager
	// due to version mismatch. Cast to interface{} first to work around this.
	return interface{}(llmMgr).(aisdk.LLMManager), nil
}
