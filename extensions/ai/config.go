package ai

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/internal"
)

// DI container keys for AI extension services.
const (
	// ServiceKey is the DI key for the AI service.
	ServiceKey = "forge.ai.service"
	// ManagerKey is the DI key for the AI manager.
	ManagerKey = "forge.ai.manager"
	// AgentFactoryKey is the DI key for the agent factory.
	AgentFactoryKey = "forge.ai.agentFactory"
	// LLMManagerKey is the DI key for the LLM manager.
	LLMManagerKey = "forge.ai.llmManager"
	// SDKLLMManagerKey is the DI key for the SDK LLM manager interface.
	SDKLLMManagerKey = "forge.ai.sdk.llmManager"
)

// Config is the public configuration for the AI extension.
type Config struct {
	// Core features
	EnableLLM          bool          `json:"enable_llm"          yaml:"enable_llm"`
	EnableAgents       bool          `json:"enable_agents"       yaml:"enable_agents"`
	EnableTraining     bool          `json:"enable_training"     yaml:"enable_training"`
	EnableInference    bool          `json:"enable_inference"    yaml:"enable_inference"`
	EnableCoordination bool          `json:"enable_coordination" yaml:"enable_coordination"`
	MaxConcurrency     int           `json:"max_concurrency"     yaml:"max_concurrency"`
	RequestTimeout     time.Duration `json:"request_timeout"     yaml:"request_timeout"`
	CacheSize          int           `json:"cache_size"          yaml:"cache_size"`

	// LLM configuration
	LLM LLMConfiguration `json:"llm" yaml:"llm"`

	// Inference configuration
	Inference InferenceConfiguration `json:"inference" yaml:"inference"`

	// Agent configuration
	Agents AgentConfiguration `json:"agents" yaml:"agents"`

	// Middleware configuration
	Middleware MiddlewareConfiguration `json:"middleware" yaml:"middleware"`
}

// LLMConfiguration contains LLM-specific settings.
type LLMConfiguration struct {
	DefaultProvider string                    `json:"default_provider" yaml:"default_provider"`
	Providers       map[string]ProviderConfig `json:"providers"        yaml:"providers"`
	MaxRetries      int                       `json:"max_retries"      yaml:"max_retries"`
	RetryDelay      time.Duration             `json:"retry_delay"      yaml:"retry_delay"`
	Timeout         time.Duration             `json:"timeout"          yaml:"timeout"`
}

// ProviderConfig defines configuration for an LLM provider.
type ProviderConfig struct {
	Type    string         `json:"type"     yaml:"type"`
	APIKey  string         `json:"api_key"  yaml:"api_key"`
	BaseURL string         `json:"base_url" yaml:"base_url"`
	Models  []string       `json:"models"   yaml:"models"`
	Options map[string]any `json:"options"  yaml:"options"`
}

// InferenceConfiguration contains inference engine settings.
type InferenceConfiguration struct {
	Workers        int           `json:"workers"         yaml:"workers"`
	BatchSize      int           `json:"batch_size"      yaml:"batch_size"`
	BatchTimeout   time.Duration `json:"batch_timeout"   yaml:"batch_timeout"`
	CacheSize      int           `json:"cache_size"      yaml:"cache_size"`
	CacheTTL       time.Duration `json:"cache_ttl"       yaml:"cache_ttl"`
	EnableBatching bool          `json:"enable_batching" yaml:"enable_batching"`
	EnableCaching  bool          `json:"enable_caching"  yaml:"enable_caching"`
	EnableScaling  bool          `json:"enable_scaling"  yaml:"enable_scaling"`
}

// AgentConfiguration contains agent settings.
type AgentConfiguration struct {
	EnabledAgents []string               `json:"enabled_agents" yaml:"enabled_agents"`
	AgentConfigs  map[string]AgentConfig `json:"agent_configs"  yaml:"agent_configs"`
}

// MiddlewareConfiguration contains middleware settings.
type MiddlewareConfiguration struct {
	EnabledMiddleware []string                               `json:"enabled_middleware" yaml:"enabled_middleware"`
	MiddlewareConfigs map[string]internal.AIMiddlewareConfig `json:"middleware_configs" yaml:"middleware_configs"`
}

// DefaultConfig returns the default AI configuration.
func DefaultConfig() Config {
	return Config{
		EnableLLM:          true,
		EnableAgents:       true,
		EnableTraining:     false,
		EnableInference:    true,
		EnableCoordination: true,
		MaxConcurrency:     10,
		RequestTimeout:     30 * time.Second,
		CacheSize:          1000,
		LLM: LLMConfiguration{
			DefaultProvider: "lmstudio",
			Providers: map[string]ProviderConfig{
				"lmstudio": {
					Type:    "lmstudio",
					APIKey:  "",
					BaseURL: "http://localhost:1234/v1",
					Models:  []string{},
				},
			},
			MaxRetries: 3,
			RetryDelay: time.Second,
			Timeout:    30 * time.Second,
		},
		Agents: AgentConfiguration{
			EnabledAgents: []string{},
			AgentConfigs:  make(map[string]AgentConfig),
		},
		Middleware: MiddlewareConfiguration{
			EnabledMiddleware: []string{},
			MiddlewareConfigs: make(map[string]internal.AIMiddlewareConfig),
		},
	}
}

// ToInternal converts public Config to internal AIConfig.
func (c Config) ToInternal(logger forge.Logger, metrics forge.Metrics) internal.AIConfig {
	return internal.AIConfig{
		EnableLLM:          c.EnableLLM,
		EnableAgents:       c.EnableAgents,
		EnableTraining:     c.EnableTraining,
		EnableInference:    c.EnableInference,
		EnableCoordination: c.EnableCoordination,
		MaxConcurrency:     c.MaxConcurrency,
		RequestTimeout:     c.RequestTimeout,
		CacheSize:          c.CacheSize,
		Logger:             logger,
		Metrics:            metrics,
	}
}

// ConfigOption is a functional option for Config.
type ConfigOption func(*Config)

// WithConfig replaces the entire config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}

// WithEnableLLM sets whether LLM is enabled.
func WithEnableLLM(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableLLM = enabled }
}

// WithEnableAgents sets whether agents are enabled.
func WithEnableAgents(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableAgents = enabled }
}

// WithEnableTraining sets whether training is enabled.
func WithEnableTraining(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableTraining = enabled }
}

// WithEnableInference sets whether inference is enabled.
func WithEnableInference(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableInference = enabled }
}

// WithEnableCoordination sets whether coordination is enabled.
func WithEnableCoordination(enabled bool) ConfigOption {
	return func(c *Config) { c.EnableCoordination = enabled }
}

// WithMaxConcurrency sets the maximum concurrency.
func WithMaxConcurrency(max int) ConfigOption {
	return func(c *Config) { c.MaxConcurrency = max }
}

// WithRequestTimeout sets the request timeout.
func WithRequestTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) { c.RequestTimeout = timeout }
}

// WithCacheSize sets the cache size.
func WithCacheSize(size int) ConfigOption {
	return func(c *Config) { c.CacheSize = size }
}

// WithLLMConfig sets the LLM configuration.
func WithLLMConfig(llm LLMConfiguration) ConfigOption {
	return func(c *Config) { c.LLM = llm }
}

// WithInferenceConfig sets the inference configuration.
func WithInferenceConfig(inference InferenceConfiguration) ConfigOption {
	return func(c *Config) { c.Inference = inference }
}

// WithAgentsConfig sets the agents configuration.
func WithAgentsConfig(agents AgentConfiguration) ConfigOption {
	return func(c *Config) { c.Agents = agents }
}

// WithMiddlewareConfig sets the middleware configuration.
func WithMiddlewareConfig(middleware MiddlewareConfiguration) ConfigOption {
	return func(c *Config) { c.Middleware = middleware }
}
