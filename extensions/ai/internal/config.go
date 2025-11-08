package internal

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// AIConfig contains configuration for the AI manager.
type AIConfig struct {
	EnableLLM          bool          `default:"true"  yaml:"enable_llm"`
	EnableAgents       bool          `default:"true"  yaml:"enable_agents"`
	EnableTraining     bool          `default:"false" yaml:"enable_training"`
	EnableInference    bool          `default:"true"  yaml:"enable_inference"`
	EnableCoordination bool          `default:"true"  yaml:"enable_coordination"`
	MaxConcurrency     int           `default:"10"    yaml:"max_concurrency"`
	RequestTimeout     time.Duration `default:"30s"   yaml:"request_timeout"`
	CacheSize          int           `default:"1000"  yaml:"cache_size"`
	Logger             logger.Logger `yaml:"-"`
	Metrics            forge.Metrics `yaml:"-"`
}
