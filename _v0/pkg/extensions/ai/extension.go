package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/v0/pkg/ai"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/extensions"
	"github.com/xraph/forge/v0/pkg/logger"
)

// AIExtension provides AI capabilities as an extension
type AIExtension struct {
	manager      *ai.Manager
	config       AIConfig
	logger       common.Logger
	metrics      common.Metrics
	status       extensions.ExtensionStatus
	startedAt    time.Time
	capabilities []string
}

// AIConfig contains configuration for the AI extension
type AIConfig struct {
	EnableLLM          bool           `yaml:"enable_llm" default:"true"`
	EnableAgents       bool           `yaml:"enable_agents" default:"true"`
	EnableTraining     bool           `yaml:"enable_training" default:"false"`
	EnableInference    bool           `yaml:"enable_inference" default:"true"`
	EnableCoordination bool           `yaml:"enable_coordination" default:"true"`
	MaxConcurrency     int            `yaml:"max_concurrency" default:"10"`
	RequestTimeout     time.Duration  `yaml:"request_timeout" default:"30s"`
	CacheSize          int            `yaml:"cache_size" default:"1000"`
	Logger             common.Logger  `yaml:"-"`
	Metrics            common.Metrics `yaml:"-"`
}

// NewAIExtension creates a new AI extension
func NewAIExtension(config AIConfig) *AIExtension {
	capabilities := []string{"llm", "agents", "inference"}
	if config.EnableTraining {
		capabilities = append(capabilities, "training")
	}
	if config.EnableCoordination {
		capabilities = append(capabilities, "coordination")
	}

	return &AIExtension{
		config:       config,
		logger:       config.Logger,
		metrics:      config.Metrics,
		status:       extensions.ExtensionStatusUnknown,
		capabilities: capabilities,
	}
}

// Extension interface implementation

func (ae *AIExtension) Name() string {
	return "ai"
}

func (ae *AIExtension) Version() string {
	return "1.0.0"
}

func (ae *AIExtension) Description() string {
	return "AI/ML capabilities including LLM, agents, inference, and training"
}

func (ae *AIExtension) Dependencies() []string {
	return []string{} // No dependencies for now
}

func (ae *AIExtension) Initialize(ctx context.Context, config extensions.ExtensionConfig) error {
	ae.status = extensions.ExtensionStatusLoading

	// Initialize AI manager
	aiConfig := ai.ManagerConfig{
		EnableLLM:          ae.config.EnableLLM,
		EnableAgents:       ae.config.EnableAgents,
		EnableTraining:     ae.config.EnableTraining,
		EnableInference:    ae.config.EnableInference,
		EnableCoordination: ae.config.EnableCoordination,
		MaxConcurrency:     ae.config.MaxConcurrency,
		RequestTimeout:     ae.config.RequestTimeout,
		CacheSize:          ae.config.CacheSize,
		Logger:             ae.logger,
		Metrics:            ae.metrics,
	}

	var err error
	ae.manager, err = ai.NewManager(aiConfig)
	if err != nil {
		ae.status = extensions.ExtensionStatusError
		return fmt.Errorf("failed to initialize AI manager: %w", err)
	}

	ae.status = extensions.ExtensionStatusLoaded

	if ae.logger != nil {
		ae.logger.Info("AI extension initialized",
			logger.Strings("capabilities", ae.capabilities),
		)
	}

	return nil
}

func (ae *AIExtension) Start(ctx context.Context) error {
	if ae.manager == nil {
		return fmt.Errorf("AI extension not initialized")
	}

	ae.status = extensions.ExtensionStatusStarting

	if err := ae.manager.Start(ctx); err != nil {
		ae.status = extensions.ExtensionStatusError
		return fmt.Errorf("failed to start AI manager: %w", err)
	}

	ae.status = extensions.ExtensionStatusRunning
	ae.startedAt = time.Now()

	if ae.logger != nil {
		ae.logger.Info("AI extension started")
	}

	return nil
}

func (ae *AIExtension) Stop(ctx context.Context) error {
	if ae.manager == nil {
		return nil
	}

	ae.status = extensions.ExtensionStatusStopping

	if err := ae.manager.Stop(ctx); err != nil {
		ae.logger.Warn("failed to stop AI manager", logger.Error(err))
	}

	ae.status = extensions.ExtensionStatusStopped

	if ae.logger != nil {
		ae.logger.Info("AI extension stopped")
	}

	return nil
}

func (ae *AIExtension) HealthCheck(ctx context.Context) error {
	if ae.manager == nil {
		return fmt.Errorf("AI extension not initialized")
	}

	if ae.status != extensions.ExtensionStatusRunning {
		return fmt.Errorf("AI extension not running")
	}

	// Perform health check on AI manager
	if err := ae.manager.HealthCheck(ctx); err != nil {
		return fmt.Errorf("AI manager health check failed: %w", err)
	}

	return nil
}

func (ae *AIExtension) GetCapabilities() []string {
	return ae.capabilities
}

func (ae *AIExtension) IsCapabilitySupported(capability string) bool {
	for _, cap := range ae.capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

func (ae *AIExtension) GetConfig() interface{} {
	return ae.config
}

func (ae *AIExtension) UpdateConfig(config interface{}) error {
	if aiConfig, ok := config.(AIConfig); ok {
		ae.config = aiConfig
		return nil
	}
	return fmt.Errorf("invalid config type for AI extension")
}

// AI-specific methods

// GetManager returns the AI manager instance
func (ae *AIExtension) GetManager() *ai.Manager {
	return ae.manager
}

// GetStats returns AI extension statistics
func (ae *AIExtension) GetStats() map[string]interface{} {
	if ae.manager == nil {
		return map[string]interface{}{
			"status": ae.status,
		}
	}

	stats := map[string]interface{}{
		"status":       ae.status,
		"started_at":   ae.startedAt,
		"capabilities": ae.capabilities,
	}

	// Add AI manager stats if available
	if aiStats := ae.manager.GetStats(); aiStats != nil {
		stats["ai_manager"] = aiStats
	}

	return stats
}

// IsLLMEnabled returns whether LLM capabilities are enabled
func (ae *AIExtension) IsLLMEnabled() bool {
	return ae.config.EnableLLM
}

// IsAgentsEnabled returns whether agent capabilities are enabled
func (ae *AIExtension) IsAgentsEnabled() bool {
	return ae.config.EnableAgents
}

// IsTrainingEnabled returns whether training capabilities are enabled
func (ae *AIExtension) IsTrainingEnabled() bool {
	return ae.config.EnableTraining
}

// IsInferenceEnabled returns whether inference capabilities are enabled
func (ae *AIExtension) IsInferenceEnabled() bool {
	return ae.config.EnableInference
}

// IsCoordinationEnabled returns whether coordination capabilities are enabled
func (ae *AIExtension) IsCoordinationEnabled() bool {
	return ae.config.EnableCoordination
}
