package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/internal"
	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/llm/providers"
	"github.com/xraph/forge/extensions/ai/sdk"
)

// Extension implements forge.Extension for AI.
type Extension struct {
	config         Config
	configProvided bool // Track if config was explicitly provided via options
	ai             internal.AI
	llmManager     *llm.LLMManager
	logger         forge.Logger
	metrics        forge.Metrics
	app            forge.App
}

// NewExtension creates a new AI extension with variadic options.
func NewExtension(opts ...ConfigOption) *Extension {
	config := DefaultConfig()

	hasOpts := len(opts) > 0
	for _, opt := range opts {
		opt(&config)
	}

	return &Extension{
		config:         config,
		configProvided: hasOpts,
	}
}

// NewExtensionWithConfig creates a new AI extension with a complete config.
func NewExtensionWithConfig(config Config) *Extension {
	ext := NewExtension(WithConfig(config))
	ext.configProvided = true // Mark as explicitly configured

	return ext
}

// Name returns the extension name.
func (e *Extension) Name() string {
	return "ai"
}

// Version returns the extension version.
func (e *Extension) Version() string {
	return "2.0.0"
}

// Description returns the extension description.
func (e *Extension) Description() string {
	return "AI/ML Extension with LLM, agents, inference, and more"
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// Register registers the AI extension with the app.
func (e *Extension) Register(app forge.App) error {
	e.app = app

	// Get dependencies from DI container
	if l, err := forge.GetLogger(app.Container()); err == nil {
		e.logger = l
	}

	if m, err := forge.GetMetrics(app.Container()); err == nil {
		e.metrics = m
	}

	// Get configuration from config manager only if not explicitly provided via options
	if !e.configProvided {
		if configMgr, err := forge.GetConfigManager(app.Container()); err == nil {
			if err := configMgr.Bind("extensions.ai", &e.config); err != nil {
				if e.logger != nil {
					e.logger.Debug("no extensions.ai config found", forge.F("reason", err.Error()))
				}

				if err := configMgr.Bind("ai", &e.config); err != nil {
					if e.logger != nil {
						e.logger.Debug("no ai config found, using defaults", forge.F("reason", err.Error()))
					}
					// DefaultConfig already applied in NewExtension
				}
			}
		}
	} else {
		if e.logger != nil {
			e.logger.Debug("using programmatically provided AI config",
				forge.F("provider", e.config.LLM.DefaultProvider),
				forge.F("providers", len(e.config.LLM.Providers)))
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

	// Create LLM manager if LLM is enabled
	if e.config.EnableLLM {
		llmConfig := llm.LLMManagerConfig{
			Logger:          e.logger,
			Metrics:         e.metrics,
			DefaultProvider: e.config.LLM.DefaultProvider,
			MaxRetries:      e.config.LLM.MaxRetries,
			RetryDelay:      e.config.LLM.RetryDelay,
			Timeout:         e.config.LLM.Timeout,
		}

		// Set defaults if not configured
		if llmConfig.DefaultProvider == "" {
			llmConfig.DefaultProvider = "openai"
		}

		if llmConfig.MaxRetries == 0 {
			llmConfig.MaxRetries = 3
		}

		if llmConfig.RetryDelay == 0 {
			llmConfig.RetryDelay = time.Second
		}

		if llmConfig.Timeout == 0 {
			llmConfig.Timeout = 30 * time.Second
		}

		llmManager, err := llm.NewLLMManager(llmConfig)
		if err != nil {
			return fmt.Errorf("failed to create LLM manager: %w", err)
		}

		e.llmManager = llmManager

		// Register providers from config
		var (
			registeredCount int
			failedCount     int
		)

		for name, providerConfig := range e.config.LLM.Providers {
			var (
				provider llm.LLMProvider
				err      error
			)

			// Auto-infer type from key name if not specified
			providerType := providerConfig.Type
			if providerType == "" {
				providerType = name
				if e.logger != nil {
					e.logger.Warn("provider config missing 'type' field, inferring from key",
						forge.F("provider", name),
						forge.F("inferred_type", providerType),
					)
				}
			}

			switch providerType {
			case "openai":
				openAIConfig := providers.OpenAIConfig{
					APIKey:     providerConfig.APIKey,
					BaseURL:    providerConfig.BaseURL,
					Timeout:    llmConfig.Timeout,
					MaxRetries: llmConfig.MaxRetries,
				}
				if providerConfig.BaseURL == "" {
					openAIConfig.BaseURL = "https://api.openai.com/v1"
				}

				provider, err = providers.NewOpenAIProvider(openAIConfig, e.logger, e.metrics)
			case "anthropic":
				anthropicConfig := providers.AnthropicConfig{
					APIKey:     providerConfig.APIKey,
					BaseURL:    providerConfig.BaseURL,
					Timeout:    llmConfig.Timeout,
					MaxRetries: llmConfig.MaxRetries,
				}
				if providerConfig.BaseURL == "" {
					anthropicConfig.BaseURL = "https://api.anthropic.com"
				}

				provider, err = providers.NewAnthropicProvider(anthropicConfig, e.logger, e.metrics)
			case "huggingface":
				hfConfig := providers.HuggingFaceConfig{
					APIKey:     providerConfig.APIKey,
					BaseURL:    providerConfig.BaseURL,
					Timeout:    llmConfig.Timeout,
					MaxRetries: llmConfig.MaxRetries,
				}
				if providerConfig.BaseURL == "" {
					hfConfig.BaseURL = "https://api-inference.huggingface.co"
				}

				provider, err = providers.NewHuggingFaceProvider(hfConfig, e.logger, e.metrics)
			case "lmstudio":
				lmstudioConfig := providers.LMStudioConfig{
					APIKey:     providerConfig.APIKey,
					BaseURL:    providerConfig.BaseURL,
					Timeout:    llmConfig.Timeout,
					MaxRetries: llmConfig.MaxRetries,
					Models:     providerConfig.Models,
					Logger:     e.logger,
					Metrics:    e.metrics,
				}
				if providerConfig.BaseURL == "" {
					lmstudioConfig.BaseURL = "http://localhost:1234/v1"
				}
				// LMStudio typically needs more time for local inference
				if lmstudioConfig.Timeout < 60*time.Second {
					lmstudioConfig.Timeout = 60 * time.Second
				}

				provider, err = providers.NewLMStudioProvider(lmstudioConfig, e.logger, e.metrics)
			case "ollama":
				// Note: Ollama provider implementation pending
				// Infrastructure ready for when provider is implemented
				ollamaConfig := providers.OllamaConfig{
					BaseURL:    providerConfig.BaseURL,
					Timeout:    llmConfig.Timeout,
					MaxRetries: llmConfig.MaxRetries,
					Models:     providerConfig.Models,
					Logger:     e.logger,
					Metrics:    e.metrics,
				}
				if providerConfig.BaseURL == "" {
					ollamaConfig.BaseURL = "http://localhost:11434"
				}
				// Ollama also needs more time for local inference
				if ollamaConfig.Timeout < 60*time.Second {
					ollamaConfig.Timeout = 60 * time.Second
				}

				provider, err = providers.NewOllamaProvider(ollamaConfig, e.logger, e.metrics)
			default:
				if e.logger != nil {
					if providerType == "" {
						e.logger.Error("provider config missing required 'type' field",
							forge.F("provider", name),
							forge.F("valid_types", []string{"openai", "anthropic", "huggingface", "lmstudio", "ollama"}),
						)
					} else {
						e.logger.Error("unsupported LLM provider type",
							forge.F("provider", name),
							forge.F("type", providerType),
							forge.F("valid_types", []string{"openai", "anthropic", "huggingface", "lmstudio", "ollama"}),
						)
					}
				}

				failedCount++

				continue
			}

			if err != nil {
				if e.logger != nil {
					e.logger.Error("failed to create LLM provider", forge.F("provider", name), forge.F("error", err))
				}

				failedCount++

				continue
			}

			if err := llmManager.RegisterProvider(provider); err != nil {
				if e.logger != nil {
					e.logger.Error("failed to register LLM provider", forge.F("provider", name), forge.F("error", err))
				}

				failedCount++

				continue
			}

			registeredCount++
		}

		// Log registration summary
		registeredProviders := llmManager.GetProviders()
		if e.logger != nil {
			providerNames := make([]string, 0, len(registeredProviders))
			for name := range registeredProviders {
				providerNames = append(providerNames, name)
			}

			e.logger.Info("LLM providers registered",
				forge.F("count", len(registeredProviders)),
				forge.F("providers", providerNames),
				forge.F("default", llmConfig.DefaultProvider),
				forge.F("failed", failedCount),
			)
		}

		// Validate that at least one provider was registered
		if registeredCount == 0 && len(e.config.LLM.Providers) > 0 {
			return fmt.Errorf("failed to register any LLM providers: %d configured, %d failed", len(e.config.LLM.Providers), failedCount)
		}

		// Validate default provider was registered
		if llmConfig.DefaultProvider != "" && registeredCount > 0 {
			if _, err := llmManager.GetProvider(llmConfig.DefaultProvider); err != nil {
				availableProviders := make([]string, 0, len(registeredProviders))
				for name := range registeredProviders {
					availableProviders = append(availableProviders, name)
				}

				return fmt.Errorf("default provider %q not registered, available providers: %v", llmConfig.DefaultProvider, availableProviders)
			}
		}

		// Register LLM manager in DI with forge.ai prefix
		if err := forge.RegisterSingleton[*llm.LLMManager](app.Container(), LLMManagerKey, func(c forge.Container) (*llm.LLMManager, error) {
			return e.llmManager, nil
		}); err != nil {
			return fmt.Errorf("failed to register LLM manager: %w", err)
		}

		// Also register as sdk.LLMManager interface (llm.LLMManager satisfies this interface)
		if err := app.Container().Register(SDKLLMManagerKey, func(c forge.Container) (any, error) {
			return e.llmManager, nil
		}); err != nil {
			return fmt.Errorf("failed to register SDK LLM manager: %w", err)
		}
	}

	// Create agent factory with LLM manager
	agentFactory := NewAgentFactory(e.llmManager, e.logger)

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

	// Register service in DI using a factory with forge.ai prefix
	if err := forge.RegisterSingleton[*service](app.Container(), ServiceKey, func(c forge.Container) (*service, error) {
		return &service{ai: e.ai}, nil
	}); err != nil {
		return fmt.Errorf("failed to register AI service: %w", err)
	}

	// Register AI interface for direct access with forge.ai prefix
	if err := forge.RegisterSingleton[internal.AI](app.Container(), ManagerKey, func(c forge.Container) (internal.AI, error) {
		return e.ai, nil
	}); err != nil {
		return fmt.Errorf("failed to register AI manager: %w", err)
	}

	// Register agent factory for direct access with forge.ai prefix
	if err := app.Container().Register(AgentFactoryKey, func(c forge.Container) (any, error) {
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

// Start starts the AI extension.
func (e *Extension) Start(ctx context.Context) error {
	// Start LLM manager if enabled
	if e.llmManager != nil {
		if err := e.llmManager.Start(ctx); err != nil {
			return fmt.Errorf("failed to start LLM manager: %w", err)
		}
	}

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

// Stop stops the AI extension.
func (e *Extension) Stop(ctx context.Context) error {
	if err := e.ai.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop AI extension: %w", err)
	}

	// Stop LLM manager if enabled
	if e.llmManager != nil {
		if err := e.llmManager.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop LLM manager: %w", err)
		}
	}

	if e.logger != nil {
		e.logger.Info("AI extension stopped")
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.ai.extension.stopped").Inc()
	}

	return nil
}

// Health performs health check on the AI extension.
func (e *Extension) Health(ctx context.Context) error {
	return e.ai.HealthCheck(ctx)
}

// GetAI returns the AI interface for direct access.
func (e *Extension) GetAI() internal.AI {
	return e.ai
}

// GetManager returns the AI manager implementation (if needed).
func (e *Extension) GetManager() *managerImpl {
	if impl, ok := e.ai.(*managerImpl); ok {
		return impl
	}

	return nil
}

// GetLLMManager returns the LLM manager for SDK usage.
func (e *Extension) GetLLMManager() *llm.LLMManager {
	return e.llmManager
}

// SDK convenience methods

// Generate creates a new GenerateBuilder for text generation
// This is a convenience method that uses the extension's LLM manager.
func (e *Extension) Generate(ctx context.Context) *sdk.GenerateBuilder {
	if e.llmManager == nil {
		// Return a builder that will fail with a clear error
		// This allows the SDK to handle the error gracefully
		return sdk.NewGenerateBuilder(ctx, nil, e.logger, e.metrics)
	}

	return sdk.NewGenerateBuilder(ctx, e.llmManager, e.logger, e.metrics)
}

// Stream creates a new StreamBuilder for streaming text generation
// This is a convenience method that uses the extension's LLM manager.
func (e *Extension) Stream(ctx context.Context) *sdk.StreamBuilder {
	if e.llmManager == nil {
		return sdk.NewStreamBuilder(ctx, nil, e.logger, e.metrics)
	}

	return sdk.NewStreamBuilder(ctx, e.llmManager, e.logger, e.metrics)
}

// GenerateObject creates a new GenerateObjectBuilder for structured output generation
// This is a convenience method that uses the extension's LLM manager
// Note: This is a standalone function because Go doesn't support generic methods yet.
func GenerateObject[T any](e *Extension, ctx context.Context) *sdk.GenerateObjectBuilder[T] {
	if e.llmManager == nil {
		return sdk.NewGenerateObjectBuilder[T](ctx, nil, e.logger, e.metrics)
	}

	return sdk.NewGenerateObjectBuilder[T](ctx, e.llmManager, e.logger, e.metrics)
}
