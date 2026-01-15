package ai

import (
	"context"
	"fmt"

	aisdk "github.com/xraph/ai-sdk"
	memory "github.com/xraph/ai-sdk/integrations/statestores/memory"
	postgres "github.com/xraph/ai-sdk/integrations/statestores/postgres"
	redis "github.com/xraph/ai-sdk/integrations/statestores/redis"

	vmemory "github.com/xraph/ai-sdk/integrations/vectorstores/memory"
	// vpostgres "github.com/xraph/ai-sdk/integrations/vectorstores/postgres"
	// Add other vector store imports as needed

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

	// TODO: Add more vector store types (Pinecone, Weaviate, etc.)

	default:
		return nil, fmt.Errorf("unknown vector store type: %s", cfg.Type)
	}
}

// CreateLLMManager creates an LLM Manager from configuration.
// Note: For now, LLM Manager bootstrap from config is not yet implemented due to ai-sdk dependencies.
// Please register LLM Manager manually via DI as shown in the examples.
func CreateLLMManager(cfg LLMConfiguration, logger forge.Logger, metrics forge.Metrics) (aisdk.LLMManager, error) {
	// TODO: Once ai-sdk is fully separated, implement automatic LLM manager creation with providers
	return nil, fmt.Errorf("LLM manager bootstrap from config not yet implemented - please register LLMManager via DI")
}
