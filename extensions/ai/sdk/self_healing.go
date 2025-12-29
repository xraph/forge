package sdk

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// SelfHealingAgent wraps an agent with automatic error recovery and learning.
type SelfHealingAgent struct {
	agent   *Agent
	logger  forge.Logger
	metrics forge.Metrics

	// Error tracking
	errorHistory  []ErrorRecord
	recoveryStats map[string]*RecoveryStats
	mu            sync.RWMutex

	// Configuration
	config SelfHealingConfig

	// Learning
	strategies     []RecoveryStrategy
	learnedPattern map[string]RecoveryAction
}

// SelfHealingConfig configures self-healing behavior.
type SelfHealingConfig struct {
	MaxRetries              int
	RetryDelay              time.Duration
	BackoffMultiplier       float64
	MaxRetryDelay           time.Duration
	EnableLearning          bool
	ErrorHistorySize        int
	AdaptiveThreshold       int // Number of failures before adapting
	HealthCheckInterval     time.Duration
	AutoRecover             bool
	FallbackEnabled         bool
	CircuitBreakerEnabled   bool
	CircuitBreakerThreshold int
}

// ErrorRecord tracks a single error occurrence.
type ErrorRecord struct {
	Timestamp    time.Duration
	Error        error
	Context      map[string]any
	RecoveryUsed string
	Recovered    bool
	Attempts     int
}

// RecoveryStats tracks recovery success rates.
type RecoveryStats struct {
	TotalAttempts        int
	SuccessfulRecoveries int
	FailedRecoveries     int
	AverageAttempts      float64
	LastSuccess          time.Time
	LastFailure          time.Time
}

// RecoveryStrategy defines a strategy for recovering from errors.
type RecoveryStrategy interface {
	Name() string
	CanHandle(err error) bool
	Recover(ctx context.Context, agent *Agent, err error) error
	Priority() int
}

// RecoveryAction represents a learned recovery action.
type RecoveryAction struct {
	StrategyName string
	SuccessRate  float64
	Confidence   float64
	LastUsed     time.Time
}

// NewSelfHealingAgent creates a new self-healing agent.
func NewSelfHealingAgent(agent *Agent, config SelfHealingConfig, logger forge.Logger, metrics forge.Metrics) *SelfHealingAgent {
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	if config.RetryDelay == 0 {
		config.RetryDelay = time.Second
	}

	if config.BackoffMultiplier == 0 {
		config.BackoffMultiplier = 2.0
	}

	if config.MaxRetryDelay == 0 {
		config.MaxRetryDelay = 30 * time.Second
	}

	if config.ErrorHistorySize == 0 {
		config.ErrorHistorySize = 100
	}

	if config.AdaptiveThreshold == 0 {
		config.AdaptiveThreshold = 3
	}

	if config.CircuitBreakerThreshold == 0 {
		config.CircuitBreakerThreshold = 5
	}

	sha := &SelfHealingAgent{
		agent:          agent,
		logger:         logger,
		metrics:        metrics,
		config:         config,
		errorHistory:   make([]ErrorRecord, 0, config.ErrorHistorySize),
		recoveryStats:  make(map[string]*RecoveryStats),
		strategies:     make([]RecoveryStrategy, 0),
		learnedPattern: make(map[string]RecoveryAction),
	}

	// Register default strategies
	sha.RegisterStrategy(&RetryStrategy{})
	sha.RegisterStrategy(&ResetStateStrategy{})
	sha.RegisterStrategy(&SimplifyPromptStrategy{})
	sha.RegisterStrategy(&FallbackModelStrategy{})

	return sha
}

// RegisterStrategy registers a recovery strategy.
func (sha *SelfHealingAgent) RegisterStrategy(strategy RecoveryStrategy) {
	sha.mu.Lock()
	defer sha.mu.Unlock()

	sha.strategies = append(sha.strategies, strategy)
}

// Process processes a request with self-healing.
func (sha *SelfHealingAgent) Process(ctx context.Context, input string) (string, error) {
	var lastErr error

	delay := sha.config.RetryDelay

	for attempt := 0; attempt <= sha.config.MaxRetries; attempt++ {
		// Execute the agent
		response, err := sha.agent.Execute(ctx, input)
		if err == nil {
			// Success! Record it
			if attempt > 0 {
				sha.recordRecovery(lastErr, true, attempt)
			}

			return response.Content, nil
		}

		lastErr = err

		if sha.logger != nil {
			sha.logger.Warn("agent execution failed",
				F("attempt", attempt+1),
				F("error", err),
			)
		}

		// Try to recover
		if sha.config.AutoRecover && attempt < sha.config.MaxRetries {
			recoveryErr := sha.tryRecover(ctx, err)
			if recoveryErr == nil {
				if sha.logger != nil {
					sha.logger.Info("recovery successful",
						F("attempt", attempt+1),
					)
				}
				// Wait before retry
				time.Sleep(delay)

				delay = time.Duration(float64(delay) * sha.config.BackoffMultiplier)
				if delay > sha.config.MaxRetryDelay {
					delay = sha.config.MaxRetryDelay
				}

				continue
			}

			if sha.logger != nil {
				sha.logger.Error("recovery failed",
					F("attempt", attempt+1),
					F("recovery_error", recoveryErr),
				)
			}
		}

		// Wait before retry
		if attempt < sha.config.MaxRetries {
			time.Sleep(delay)

			delay = time.Duration(float64(delay) * sha.config.BackoffMultiplier)
			if delay > sha.config.MaxRetryDelay {
				delay = sha.config.MaxRetryDelay
			}
		}
	}

	// All retries failed
	sha.recordRecovery(lastErr, false, sha.config.MaxRetries+1)

	return "", fmt.Errorf("all recovery attempts failed: %w", lastErr)
}

// tryRecover attempts to recover from an error.
func (sha *SelfHealingAgent) tryRecover(ctx context.Context, err error) error {
	// Check if we have a learned pattern for this error
	if sha.config.EnableLearning {
		errorType := fmt.Sprintf("%T", err)
		if action, exists := sha.learnedPattern[errorType]; exists {
			// Use the learned strategy
			for _, strategy := range sha.strategies {
				if strategy.Name() == action.StrategyName {
					if sha.logger != nil {
						sha.logger.Info("using learned recovery strategy",
							F("strategy", strategy.Name()),
							F("success_rate", action.SuccessRate),
						)
					}

					return strategy.Recover(ctx, sha.agent, err)
				}
			}
		}
	}

	// Try strategies in priority order
	for _, strategy := range sha.strategies {
		if !strategy.CanHandle(err) {
			continue
		}

		if sha.logger != nil {
			sha.logger.Debug("trying recovery strategy",
				F("strategy", strategy.Name()),
			)
		}

		recoveryErr := strategy.Recover(ctx, sha.agent, err)
		if recoveryErr == nil {
			// Strategy succeeded!
			sha.updateRecoveryStats(strategy.Name(), true)

			if sha.config.EnableLearning {
				sha.learnPattern(err, strategy.Name())
			}

			return nil
		}

		sha.updateRecoveryStats(strategy.Name(), false)
	}

	return errors.New("no recovery strategy succeeded")
}

// recordRecovery records a recovery attempt.
func (sha *SelfHealingAgent) recordRecovery(err error, recovered bool, attempts int) {
	sha.mu.Lock()
	defer sha.mu.Unlock()

	record := ErrorRecord{
		Timestamp: time.Duration(time.Now().Unix()),
		Error:     err,
		Recovered: recovered,
		Attempts:  attempts,
		Context:   make(map[string]any),
	}

	sha.errorHistory = append(sha.errorHistory, record)

	// Trim history if too large
	if len(sha.errorHistory) > sha.config.ErrorHistorySize {
		sha.errorHistory = sha.errorHistory[len(sha.errorHistory)-sha.config.ErrorHistorySize:]
	}

	// Update metrics
	if sha.metrics != nil {
		if recovered {
			sha.metrics.Counter("forge.ai.sdk.selfhealing.recoveries", "status", "success").Inc()
		} else {
			sha.metrics.Counter("forge.ai.sdk.selfhealing.recoveries", "status", "failure").Inc()
		}

		sha.metrics.Gauge("forge.ai.sdk.selfhealing.attempts", "type", "total").Set(float64(attempts))
	}
}

// updateRecoveryStats updates recovery statistics for a strategy.
func (sha *SelfHealingAgent) updateRecoveryStats(strategyName string, success bool) {
	sha.mu.Lock()
	defer sha.mu.Unlock()

	stats, exists := sha.recoveryStats[strategyName]
	if !exists {
		stats = &RecoveryStats{}
		sha.recoveryStats[strategyName] = stats
	}

	stats.TotalAttempts++
	if success {
		stats.SuccessfulRecoveries++
		stats.LastSuccess = time.Now()
	} else {
		stats.FailedRecoveries++
		stats.LastFailure = time.Now()
	}

	stats.AverageAttempts = float64(stats.TotalAttempts) / float64(stats.SuccessfulRecoveries+1)
}

// learnPattern learns from a successful recovery.
func (sha *SelfHealingAgent) learnPattern(err error, strategyName string) {
	sha.mu.Lock()
	defer sha.mu.Unlock()

	errorType := fmt.Sprintf("%T", err)

	action, exists := sha.learnedPattern[errorType]
	if !exists {
		action = RecoveryAction{
			StrategyName: strategyName,
			SuccessRate:  1.0,
			Confidence:   0.5,
			LastUsed:     time.Now(),
		}
	} else {
		// Update success rate
		if action.StrategyName == strategyName {
			action.SuccessRate = (action.SuccessRate*0.9 + 1.0*0.1)
			action.Confidence = min(action.Confidence+0.1, 1.0)
		} else {
			// Different strategy worked, reduce confidence
			newConfidence := action.Confidence - 0.2
			if newConfidence < 0.0 {
				newConfidence = 0.0
			}

			action.Confidence = newConfidence
			if action.Confidence < 0.5 {
				// Switch to new strategy
				action.StrategyName = strategyName
				action.SuccessRate = 1.0
				action.Confidence = 0.5
			}
		}

		action.LastUsed = time.Now()
	}

	sha.learnedPattern[errorType] = action
}

// GetRecoveryStats returns recovery statistics.
func (sha *SelfHealingAgent) GetRecoveryStats() map[string]*RecoveryStats {
	sha.mu.RLock()
	defer sha.mu.RUnlock()

	stats := make(map[string]*RecoveryStats)

	for k, v := range sha.recoveryStats {
		statsCopy := *v
		stats[k] = &statsCopy
	}

	return stats
}

// GetErrorHistory returns the error history.
func (sha *SelfHealingAgent) GetErrorHistory() []ErrorRecord {
	sha.mu.RLock()
	defer sha.mu.RUnlock()

	history := make([]ErrorRecord, len(sha.errorHistory))
	copy(history, sha.errorHistory)

	return history
}

// GetLearnedPatterns returns learned patterns.
func (sha *SelfHealingAgent) GetLearnedPatterns() map[string]RecoveryAction {
	sha.mu.RLock()
	defer sha.mu.RUnlock()

	patterns := make(map[string]RecoveryAction)
	maps.Copy(patterns, sha.learnedPattern)

	return patterns
}

// --- Built-in Recovery Strategies ---

// RetryStrategy simply retries the operation.
type RetryStrategy struct{}

func (s *RetryStrategy) Name() string             { return "retry" }
func (s *RetryStrategy) Priority() int            { return 10 }
func (s *RetryStrategy) CanHandle(err error) bool { return true }
func (s *RetryStrategy) Recover(ctx context.Context, agent *Agent, err error) error {
	// Just a simple retry, the actual retry logic is in Process()
	return nil
}

// ResetStateStrategy resets the agent state.
type ResetStateStrategy struct{}

func (s *ResetStateStrategy) Name() string  { return "reset_state" }
func (s *ResetStateStrategy) Priority() int { return 20 }
func (s *ResetStateStrategy) CanHandle(err error) bool {
	// Handle state-related errors
	return true
}
func (s *ResetStateStrategy) Recover(ctx context.Context, agent *Agent, err error) error {
	// Clear conversation history to reset context
	agent.state.History = agent.state.History[:0]

	return nil
}

// SimplifyPromptStrategy simplifies the prompt to reduce complexity.
type SimplifyPromptStrategy struct{}

func (s *SimplifyPromptStrategy) Name() string  { return "simplify_prompt" }
func (s *SimplifyPromptStrategy) Priority() int { return 30 }
func (s *SimplifyPromptStrategy) CanHandle(err error) bool {
	// Handle complexity-related errors
	return true
}
func (s *SimplifyPromptStrategy) Recover(ctx context.Context, agent *Agent, err error) error {
	// Add system message to simplify responses
	agent.state.Data["simplified_mode"] = true

	return nil
}

// FallbackModelStrategy switches to a fallback model.
type FallbackModelStrategy struct{}

func (s *FallbackModelStrategy) Name() string  { return "fallback_model" }
func (s *FallbackModelStrategy) Priority() int { return 40 }
func (s *FallbackModelStrategy) CanHandle(err error) bool {
	// Handle model-related errors
	return true
}
func (s *FallbackModelStrategy) Recover(ctx context.Context, agent *Agent, err error) error {
	// Switch to a more reliable model (this would need model config)
	agent.state.Data["use_fallback_model"] = true

	return nil
}
