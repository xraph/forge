package ai

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/internal"
)

// Extension implements forge.Extension for AI
type Extension struct {
	config  Config
	ai      internal.AI
	logger  forge.Logger
	metrics forge.Metrics
	app     forge.App
}

// NewExtension creates a new AI extension with default configuration
func NewExtension() *Extension {
	return &Extension{
		config: DefaultConfig(),
	}
}

// NewExtensionWithConfig creates a new AI extension with custom configuration
func NewExtensionWithConfig(config Config) *Extension {
	return &Extension{
		config: config,
	}
}

// Name returns the extension name
func (e *Extension) Name() string {
	return "ai"
}

// Version returns the extension version
func (e *Extension) Version() string {
	return "2.0.0"
}

// Description returns the extension description
func (e *Extension) Description() string {
	return "AI/ML Extension with LLM, agents, inference, and more"
}

// Dependencies returns extension dependencies
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// Register registers the AI extension with the app
func (e *Extension) Register(app forge.App) error {
	e.app = app

	// Get dependencies from DI container
	if logger, err := app.Container().Resolve("logger"); err == nil {
		if l, ok := logger.(forge.Logger); ok {
			e.logger = l
		}
	}

	if metrics, err := app.Container().Resolve("metrics"); err == nil {
		if m, ok := metrics.(forge.Metrics); ok {
			e.metrics = m
		}
	}

	// Get configuration
	if configMgr, err := app.Container().Resolve("config"); err == nil {
		if cm, ok := configMgr.(forge.ConfigManager); ok {
			if err := cm.Bind("ai", &e.config); err != nil {
				if e.logger != nil {
					e.logger.Debug("using default AI config", forge.F("reason", err.Error()))
				}
				e.config = DefaultConfig()
			}
		}
	}

	// Convert to internal config
	internalConfig := e.config.ToInternal(e.logger, e.metrics)

	// Get optional agent store from DI
	var store AgentStore
	if s, err := app.Container().Resolve("agentStore"); err == nil {
		if as, ok := s.(AgentStore); ok {
			store = as
			if e.logger != nil {
				e.logger.Info("agent store configured", forge.F("store_type", fmt.Sprintf("%T", as)))
			}
		}
	}

	// Create agent factory (LLM manager will be set later when LLM subsystem initializes)
	agentFactory := NewAgentFactory(nil, e.logger)

	// Create AI manager with store and factory
	aiManager, err := NewManager(internalConfig,
		WithStore(store),
		WithFactory(agentFactory),
	)
	if err != nil {
		return fmt.Errorf("failed to create AI manager: %w", err)
	}

	// Store as internal.AI interface
	e.ai = aiManager

	// Register service in DI using a factory
	if err := forge.RegisterSingleton[*service](app.Container(), "ai", func(c forge.Container) (*service, error) {
		return &service{ai: e.ai}, nil
	}); err != nil {
		return fmt.Errorf("failed to register AI service: %w", err)
	}

	// Register AI interface for direct access
	if err := forge.RegisterSingleton[internal.AI](app.Container(), "ai.manager", func(c forge.Container) (AI, error) {
		return e.ai, nil
	}); err != nil {
		return fmt.Errorf("failed to register AI manager: %w", err)
	}

	// Register agent factory for direct access
	if err := app.Container().Register("agentFactory", func(c forge.Container) (any, error) {
		return agentFactory, nil
	}); err != nil {
		return fmt.Errorf("failed to register agent factory: %w", err)
	}

	if e.logger != nil {
		e.logger.Info("AI extension registered",
			forge.F("llm_enabled", e.config.EnableLLM),
			forge.F("agents_enabled", e.config.EnableAgents),
			forge.F("inference_enabled", e.config.EnableInference),
			forge.F("coordination_enabled", e.config.EnableCoordination),
			forge.F("storage_enabled", store != nil),
		)
	}

	return nil
}

// Start starts the AI extension
func (e *Extension) Start(ctx context.Context) error {
	if err := e.ai.Start(ctx); err != nil {
		return fmt.Errorf("failed to start AI extension: %w", err)
	}

	if e.logger != nil {
		e.logger.Info("AI extension started")
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.extension.started").Inc()
	}

	return nil
}

// Stop stops the AI extension
func (e *Extension) Stop(ctx context.Context) error {
	if err := e.ai.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop AI extension: %w", err)
	}

	if e.logger != nil {
		e.logger.Info("AI extension stopped")
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.extension.stopped").Inc()
	}

	return nil
}

// Health performs health check on the AI extension
func (e *Extension) Health(ctx context.Context) error {
	return e.ai.HealthCheck(ctx)
}

// GetAI returns the AI interface for direct access
func (e *Extension) GetAI() internal.AI {
	return e.ai
}

// GetManager returns the AI manager implementation (if needed)
func (e *Extension) GetManager() *managerImpl {
	if impl, ok := e.ai.(*managerImpl); ok {
		return impl
	}
	return nil
}
