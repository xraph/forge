package ai

import (
	"time"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/ai/internal"
)

// Config is the public configuration for the AI extension
type Config struct {
	// Core features
	EnableLLM          bool          `yaml:"enable_llm" json:"enable_llm"`
	EnableAgents       bool          `yaml:"enable_agents" json:"enable_agents"`
	EnableTraining     bool          `yaml:"enable_training" json:"enable_training"`
	EnableInference    bool          `yaml:"enable_inference" json:"enable_inference"`
	EnableCoordination bool          `yaml:"enable_coordination" json:"enable_coordination"`
	MaxConcurrency     int           `yaml:"max_concurrency" json:"max_concurrency"`
	RequestTimeout     time.Duration `yaml:"request_timeout" json:"request_timeout"`
	CacheSize          int           `yaml:"cache_size" json:"cache_size"`

	// LLM configuration
	LLM LLMConfiguration `yaml:"llm" json:"llm"`

	// Inference configuration
	Inference InferenceConfiguration `yaml:"inference" json:"inference"`

	// Agent configuration
	Agents AgentConfiguration `yaml:"agents" json:"agents"`

	// Middleware configuration
	Middleware MiddlewareConfiguration `yaml:"middleware" json:"middleware"`
}

// LLMConfiguration contains LLM-specific settings
type LLMConfiguration struct {
	DefaultProvider string                    `yaml:"default_provider" json:"default_provider"`
	Providers       map[string]ProviderConfig `yaml:"providers" json:"providers"`
	MaxRetries      int                       `yaml:"max_retries" json:"max_retries"`
	RetryDelay      time.Duration             `yaml:"retry_delay" json:"retry_delay"`
	Timeout         time.Duration             `yaml:"timeout" json:"timeout"`
}

// ProviderConfig defines configuration for an LLM provider
type ProviderConfig struct {
	Type    string                 `yaml:"type" json:"type"`
	APIKey  string                 `yaml:"api_key" json:"api_key"`
	BaseURL string                 `yaml:"base_url" json:"base_url"`
	Models  []string               `yaml:"models" json:"models"`
	Options map[string]interface{} `yaml:"options" json:"options"`
}

// InferenceConfiguration contains inference engine settings
type InferenceConfiguration struct {
	Workers        int           `yaml:"workers" json:"workers"`
	BatchSize      int           `yaml:"batch_size" json:"batch_size"`
	BatchTimeout   time.Duration `yaml:"batch_timeout" json:"batch_timeout"`
	CacheSize      int           `yaml:"cache_size" json:"cache_size"`
	CacheTTL       time.Duration `yaml:"cache_ttl" json:"cache_ttl"`
	EnableBatching bool          `yaml:"enable_batching" json:"enable_batching"`
	EnableCaching  bool          `yaml:"enable_caching" json:"enable_caching"`
	EnableScaling  bool          `yaml:"enable_scaling" json:"enable_scaling"`
}

// AgentConfiguration contains agent settings
type AgentConfiguration struct {
	EnabledAgents []string               `yaml:"enabled_agents" json:"enabled_agents"`
	AgentConfigs  map[string]AgentConfig `yaml:"agent_configs" json:"agent_configs"`
}

// MiddlewareConfiguration contains middleware settings
type MiddlewareConfiguration struct {
	EnabledMiddleware []string                               `yaml:"enabled_middleware" json:"enabled_middleware"`
	MiddlewareConfigs map[string]internal.AIMiddlewareConfig `yaml:"middleware_configs" json:"middleware_configs"`
}

// DefaultConfig returns the default AI configuration
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
			DefaultProvider: "openai",
			Providers:       make(map[string]ProviderConfig),
			MaxRetries:      3,
			RetryDelay:      time.Second,
			Timeout:         30 * time.Second,
		},
		Inference: InferenceConfiguration{
			Workers:        4,
			BatchSize:      10,
			BatchTimeout:   100 * time.Millisecond,
			CacheSize:      1000,
			CacheTTL:       time.Hour,
			EnableBatching: true,
			EnableCaching:  true,
			EnableScaling:  true,
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

// ToInternal converts public Config to internal AIConfig
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
