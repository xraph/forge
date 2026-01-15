package ai

import (
	"context"
	"fmt"
	"time"

	aisdk "github.com/xraph/ai-sdk"
	memory "github.com/xraph/ai-sdk/integrations/statestores/memory"
	vmemory "github.com/xraph/ai-sdk/integrations/vectorstores/memory"
	"github.com/xraph/forge"
)

// Extension implements forge.Extension for AI - pure ai-sdk wrapper.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing service instances - Vessel manages them
}

// NewExtension creates a new AI extension with variadic options.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()

	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("ai", "3.0.0", "AI Extension - Pure ai-sdk wrapper for LLM-powered agents")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new AI extension with a complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the AI extension with the app.
// This method now only loads configuration and registers service constructors.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	cfg := e.config
	ctx := context.Background()

	// Register LLMManager constructor
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (aisdk.LLMManager, error) {
		// Try to resolve existing LLM manager from DI first
		if llm, err := app.Container().Resolve("llmManager"); err == nil {
			if llmMgr, ok := llm.(aisdk.LLMManager); ok {
				logger.Info("LLM manager resolved from DI")
				return llmMgr, nil
			}
		}

		// Create from config
		if cfg.LLM.DefaultProvider != "" {
			llmMgr, err := CreateLLMManager(cfg.LLM, logger, metrics)
			if err != nil {
				return nil, fmt.Errorf("failed to create LLM manager: %w", err)
			}
			logger.Info("LLM manager created from config",
				forge.F("provider", cfg.LLM.DefaultProvider))
			return llmMgr, nil
		}

		return nil, fmt.Errorf("LLM manager required: register in DI or provide config.llm")
	}); err != nil {
		return fmt.Errorf("failed to register LLM manager: %w", err)
	}

	// Register StateStore constructor
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (aisdk.StateStore, error) {
		// Try to resolve existing state store from DI first
		if store, err := app.Container().Resolve("stateStore"); err == nil {
			if ss, ok := store.(aisdk.StateStore); ok {
				logger.Info("state store resolved from DI")
				return ss, nil
			}
		}

		// Try to create from config
		stateStore, err := CreateStateStore(ctx, cfg.StateStore, logger, metrics)
		if err != nil {
			logger.Warn("failed to create state store from config, using memory fallback",
				forge.F("error", err.Error()))
			// Fallback to memory
			stateStore = memory.NewMemoryStateStore(memory.Config{
				Logger:  logger,
				Metrics: metrics,
				TTL:     24 * time.Hour,
			})
		}
		logger.Info("state store created", forge.F("type", cfg.StateStore.Type))
		return stateStore, nil
	}); err != nil {
		return fmt.Errorf("failed to register state store: %w", err)
	}

	// Register VectorStore constructor
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (aisdk.VectorStore, error) {
		// Try to resolve existing vector store from DI first
		if store, err := app.Container().Resolve("vectorStore"); err == nil {
			if vs, ok := store.(aisdk.VectorStore); ok {
				logger.Info("vector store resolved from DI")
				return vs, nil
			}
		}

		// Try to create from config
		vs, err := CreateVectorStore(ctx, cfg.VectorStore, logger, metrics)
		if err != nil {
			logger.Warn("failed to create vector store from config, using memory fallback",
				forge.F("error", err.Error()))
			// Fallback to memory
			vs = vmemory.NewMemoryVectorStore(vmemory.Config{
				Logger:  logger,
				Metrics: metrics,
			})
		}
		logger.Info("vector store created", forge.F("type", cfg.VectorStore.Type))
		return vs, nil
	}); err != nil {
		return fmt.Errorf("failed to register vector store: %w", err)
	}

	// Register AgentFactory constructor
	if err := e.RegisterConstructor(func(llmMgr aisdk.LLMManager, stateStore aisdk.StateStore, logger forge.Logger, metrics forge.Metrics) (*AgentFactory, error) {
		return NewAgentFactory(llmMgr, stateStore, logger, metrics), nil
	}); err != nil {
		return fmt.Errorf("failed to register agent factory: %w", err)
	}

	// Register AgentManager constructor
	if err := e.RegisterConstructor(func(factory *AgentFactory, stateStore aisdk.StateStore, logger forge.Logger, metrics forge.Metrics) (*AgentManager, error) {
		return NewAgentManager(factory, stateStore, logger, metrics), nil
	}); err != nil {
		return fmt.Errorf("failed to register agent manager: %w", err)
	}

	// Register backward-compatible string keys
	if err := forge.RegisterSingleton(app.Container(), AgentManagerKey, func(c forge.Container) (any, error) {
		return forge.InjectType[*AgentManager](c)
	}); err != nil {
		return fmt.Errorf("failed to register agent manager key: %w", err)
	}

	if err := forge.RegisterSingleton(app.Container(), AgentFactoryKey, func(c forge.Container) (any, error) {
		return forge.InjectType[*AgentFactory](c)
	}); err != nil {
		return fmt.Errorf("failed to register agent factory key: %w", err)
	}

	if err := forge.RegisterSingleton(app.Container(), StateStoreKey, func(c forge.Container) (any, error) {
		return forge.InjectType[aisdk.StateStore](c)
	}); err != nil {
		return fmt.Errorf("failed to register state store key: %w", err)
	}

	if err := forge.RegisterSingleton(app.Container(), "vectorStore", func(c forge.Container) (any, error) {
		return forge.InjectType[aisdk.VectorStore](c)
	}); err != nil {
		return fmt.Errorf("failed to register vector store key: %w", err)
	}

	e.Logger().Info("AI extension registered",
		forge.F("version", e.Version()),
	)

	return nil
}

// Start marks the extension as started.
// Services are managed by Vessel lifecycle.
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	e.Logger().Info("AI extension started")

	if e.Metrics() != nil {
		e.Metrics().Counter("forge.ai.extension.started").Inc()
	}

	return nil
}

// Stop marks the extension as stopped.
// Services are managed by Vessel lifecycle.
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	e.Logger().Info("AI extension stopped")

	if e.Metrics() != nil {
		e.Metrics().Counter("forge.ai.extension.stopped").Inc()
	}

	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel.
func (e *Extension) Health(ctx context.Context) error {
	return nil
}
