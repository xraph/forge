package internal

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// AIConfig contains configuration for the AI manager
type AIConfig struct {
	EnableLLM          bool          `yaml:"enable_llm" default:"true"`
	EnableAgents       bool          `yaml:"enable_agents" default:"true"`
	EnableTraining     bool          `yaml:"enable_training" default:"false"`
	EnableInference    bool          `yaml:"enable_inference" default:"true"`
	EnableCoordination bool          `yaml:"enable_coordination" default:"true"`
	MaxConcurrency     int           `yaml:"max_concurrency" default:"10"`
	RequestTimeout     time.Duration `yaml:"request_timeout" default:"30s"`
	CacheSize          int           `yaml:"cache_size" default:"1000"`
	Logger             logger.Logger `yaml:"-"`
	Metrics            forge.Metrics `yaml:"-"`
}
