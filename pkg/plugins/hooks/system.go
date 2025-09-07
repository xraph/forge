package hooks

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/plugins"
)

// HookSystem manages and executes hooks throughout the framework
type HookSystem interface {
	Initialize(ctx context.Context) error
	Stop(ctx context.Context) error
	RegisterHook(hook plugins.Hook) error
	UnregisterHook(hookName string, hookType plugins.HookType) error
	ExecuteHooks(ctx context.Context, hookType plugins.HookType, data plugins.HookData) ([]plugins.HookResult, error)
	ExecuteHooksSequential(ctx context.Context, hookType plugins.HookType, data plugins.HookData) ([]plugins.HookResult, error)
	ExecuteHooksConcurrent(ctx context.Context, hookType plugins.HookType, data plugins.HookData) ([]plugins.HookResult, error)
	GetHooks(hookType plugins.HookType) []plugins.Hook
	GetAllHooks() map[plugins.HookType][]plugins.Hook
	GetStats() HookSystemStats
	EnableHookType(hookType plugins.HookType) error
	DisableHookType(hookType plugins.HookType) error
	IsHookTypeEnabled(hookType plugins.HookType) bool
}

// HookSystemImpl implements the HookSystem interface
type HookSystemImpl struct {
	hooks        map[plugins.HookType][]plugins.Hook
	enabledTypes map[plugins.HookType]bool
	stats        HookSystemStats
	executor     ExecutionEngine
	registry     HookRegistry
	logger       common.Logger
	metrics      common.Metrics
	mu           sync.RWMutex
	initialized  bool
	config       HookSystemConfig
}

// HookSystemConfig contains configuration for the hook system
type HookSystemConfig struct {
	MaxConcurrentHooks    int           `json:"max_concurrent_hooks" default:"10"`
	DefaultTimeout        time.Duration `json:"default_timeout" default:"30s"`
	EnableMetrics         bool          `json:"enable_metrics" default:"true"`
	EnableTracing         bool          `json:"enable_tracing" default:"true"`
	ErrorHandlingStrategy string        `json:"error_handling" default:"continue"` // continue, stop, fail
	MaxRetries            int           `json:"max_retries" default:"3"`
	RetryDelay            time.Duration `json:"retry_delay" default:"1s"`
}

// HookSystemStats contains statistics about hook execution
type HookSystemStats struct {
	TotalHooks      int                            `json:"total_hooks"`
	HooksByType     map[plugins.HookType]int       `json:"hooks_by_type"`
	EnabledTypes    map[plugins.HookType]bool      `json:"enabled_types"`
	ExecutionCount  map[plugins.HookType]int64     `json:"execution_count"`
	SuccessCount    map[plugins.HookType]int64     `json:"success_count"`
	ErrorCount      map[plugins.HookType]int64     `json:"error_count"`
	AverageLatency  map[plugins.HookType]float64   `json:"average_latency"`
	LastExecuted    map[plugins.HookType]time.Time `json:"last_executed"`
	TotalExecutions int64                          `json:"total_executions"`
	TotalErrors     int64                          `json:"total_errors"`
	SystemUptime    time.Duration                  `json:"system_uptime"`
	LastUpdated     time.Time                      `json:"last_updated"`
}

// NewHookSystem creates a new hook system
func NewHookSystem(logger common.Logger, metrics common.Metrics) HookSystem {
	config := HookSystemConfig{
		MaxConcurrentHooks:    10,
		DefaultTimeout:        30 * time.Second,
		EnableMetrics:         true,
		EnableTracing:         true,
		ErrorHandlingStrategy: "continue",
		MaxRetries:            3,
		RetryDelay:            1 * time.Second,
	}

	hs := &HookSystemImpl{
		hooks:        make(map[plugins.HookType][]plugins.Hook),
		enabledTypes: make(map[plugins.HookType]bool),
		logger:       logger,
		metrics:      metrics,
		config:       config,
		stats: HookSystemStats{
			HooksByType:    make(map[plugins.HookType]int),
			EnabledTypes:   make(map[plugins.HookType]bool),
			ExecutionCount: make(map[plugins.HookType]int64),
			SuccessCount:   make(map[plugins.HookType]int64),
			ErrorCount:     make(map[plugins.HookType]int64),
			AverageLatency: make(map[plugins.HookType]float64),
			LastExecuted:   make(map[plugins.HookType]time.Time),
		},
	}

	// Initialize components
	hs.registry = NewHookRegistry(logger, metrics)
	hs.executor = NewExecutionEngine(hs.registry, logger, metrics)

	// Enable all hook types by default
	hs.enableAllHookTypes()

	return hs
}

// Initialize initializes the hook system
func (hs *HookSystemImpl) Initialize(ctx context.Context) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.initialized {
		return nil
	}

	// Initialize executor
	if err := hs.executor.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize hook executor: %w", err)
	}

	// Initialize registry
	if err := hs.registry.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize hook registry: %w", err)
	}

	hs.initialized = true

	if hs.logger != nil {
		hs.logger.Info("hook system initialized",
			logger.Int("enabled_types", len(hs.enabledTypes)),
		)
	}

	if hs.metrics != nil {
		hs.metrics.Counter("forge.hooks.system_initialized").Inc()
	}

	return nil
}

// Stop stops the hook system
func (hs *HookSystemImpl) Stop(ctx context.Context) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if !hs.initialized {
		return nil
	}

	// Stop executor
	if err := hs.executor.Stop(ctx); err != nil {
		if hs.logger != nil {
			hs.logger.Error("failed to stop hook executor", logger.Error(err))
		}
	}

	// Stop registry
	if err := hs.registry.Stop(ctx); err != nil {
		if hs.logger != nil {
			hs.logger.Error("failed to stop hook registry", logger.Error(err))
		}
	}

	hs.initialized = false

	if hs.logger != nil {
		hs.logger.Info("hook system stopped")
	}

	if hs.metrics != nil {
		hs.metrics.Counter("forge.hooks.system_stopped").Inc()
	}

	return nil
}

// RegisterHook registers a new hook
func (hs *HookSystemImpl) RegisterHook(hook plugins.Hook) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hookType := hook.Type()

	// Check if hook already exists
	for _, existingHook := range hs.hooks[hookType] {
		if existingHook.Name() == hook.Name() {
			return fmt.Errorf("hook '%s' of type '%s' already registered", hook.Name(), hookType)
		}
	}

	// Add hook to the list
	hs.hooks[hookType] = append(hs.hooks[hookType], hook)

	// Sort hooks by priority
	sort.Slice(hs.hooks[hookType], func(i, j int) bool {
		return hs.hooks[hookType][i].Priority() < hs.hooks[hookType][j].Priority()
	})

	// Register with registry
	if err := hs.registry.RegisterHook(hook); err != nil {
		return fmt.Errorf("failed to register hook in registry: %w", err)
	}

	// Update stats
	hs.stats.TotalHooks++
	hs.stats.HooksByType[hookType]++

	if hs.logger != nil {
		hs.logger.Info("hook registered",
			logger.String("hook_name", hook.Name()),
			logger.String("hook_type", string(hookType)),
			logger.Int("priority", hook.Priority()),
		)
	}

	if hs.metrics != nil {
		hs.metrics.Counter("forge.hooks.registered", "type", string(hookType)).Inc()
		hs.metrics.Gauge("forge.hooks.total").Set(float64(hs.stats.TotalHooks))
	}

	return nil
}

// UnregisterHook unregisters a hook
func (hs *HookSystemImpl) UnregisterHook(hookName string, hookType plugins.HookType) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hooks, exists := hs.hooks[hookType]
	if !exists {
		return fmt.Errorf("no hooks of type '%s' found", hookType)
	}

	// Find and remove the hook
	for i, hook := range hooks {
		if hook.Name() == hookName {
			// Remove hook from slice
			hs.hooks[hookType] = append(hooks[:i], hooks[i+1:]...)

			// Unregister from registry
			if err := hs.registry.UnregisterHook(hookName, hookType); err != nil {
				if hs.logger != nil {
					hs.logger.Warn("failed to unregister hook from registry", logger.Error(err))
				}
			}

			// Update stats
			hs.stats.TotalHooks--
			hs.stats.HooksByType[hookType]--

			if hs.logger != nil {
				hs.logger.Info("hook unregistered",
					logger.String("hook_name", hookName),
					logger.String("hook_type", string(hookType)),
				)
			}

			if hs.metrics != nil {
				hs.metrics.Counter("forge.hooks.unregistered", "type", string(hookType)).Inc()
				hs.metrics.Gauge("forge.hooks.total").Set(float64(hs.stats.TotalHooks))
			}

			return nil
		}
	}

	return fmt.Errorf("hook '%s' of type '%s' not found", hookName, hookType)
}

// ExecuteHooks executes hooks of a specific type (default: sequential)
func (hs *HookSystemImpl) ExecuteHooks(ctx context.Context, hookType plugins.HookType, data plugins.HookData) ([]plugins.HookResult, error) {
	return hs.ExecuteHooksSequential(ctx, hookType, data)
}

// ExecuteHooksSequential executes hooks sequentially
func (hs *HookSystemImpl) ExecuteHooksSequential(ctx context.Context, hookType plugins.HookType, data plugins.HookData) ([]plugins.HookResult, error) {
	hs.mu.RLock()
	enabled := hs.enabledTypes[hookType]
	hooks := hs.hooks[hookType]
	hs.mu.RUnlock()

	if !enabled {
		return []plugins.HookResult{}, nil
	}

	if len(hooks) == 0 {
		return []plugins.HookResult{}, nil
	}

	startTime := time.Now()

	// Execute hooks sequentially
	results, err := hs.executor.ExecuteSequential(ctx, hooks, data)

	// Update statistics
	hs.updateExecutionStats(hookType, startTime, len(results), err)

	if hs.logger != nil {
		hs.logger.Debug("hooks executed sequentially",
			logger.String("hook_type", string(hookType)),
			logger.Int("hook_count", len(hooks)),
			logger.Int("results", len(results)),
			logger.Duration("duration", time.Since(startTime)),
			logger.Error(err),
		)
	}

	return results, err
}

// ExecuteHooksConcurrent executes hooks concurrently
func (hs *HookSystemImpl) ExecuteHooksConcurrent(ctx context.Context, hookType plugins.HookType, data plugins.HookData) ([]plugins.HookResult, error) {
	hs.mu.RLock()
	enabled := hs.enabledTypes[hookType]
	hooks := hs.hooks[hookType]
	hs.mu.RUnlock()

	if !enabled {
		return []plugins.HookResult{}, nil
	}

	if len(hooks) == 0 {
		return []plugins.HookResult{}, nil
	}

	startTime := time.Now()

	// Execute hooks concurrently
	results, err := hs.executor.ExecuteConcurrent(ctx, hooks, data)

	// Update statistics
	hs.updateExecutionStats(hookType, startTime, len(results), err)

	if hs.logger != nil {
		hs.logger.Debug("hooks executed concurrently",
			logger.String("hook_type", string(hookType)),
			logger.Int("hook_count", len(hooks)),
			logger.Int("results", len(results)),
			logger.Duration("duration", time.Since(startTime)),
			logger.Error(err),
		)
	}

	return results, err
}

// GetHooks returns all hooks of a specific type
func (hs *HookSystemImpl) GetHooks(hookType plugins.HookType) []plugins.Hook {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	hooks, exists := hs.hooks[hookType]
	if !exists {
		return []plugins.Hook{}
	}

	// Return a copy to prevent external modification
	result := make([]plugins.Hook, len(hooks))
	copy(result, hooks)
	return result
}

// GetAllHooks returns all registered hooks
func (hs *HookSystemImpl) GetAllHooks() map[plugins.HookType][]plugins.Hook {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	result := make(map[plugins.HookType][]plugins.Hook)
	for hookType, hooks := range hs.hooks {
		result[hookType] = make([]plugins.Hook, len(hooks))
		copy(result[hookType], hooks)
	}
	return result
}

// GetStats returns hook system statistics
func (hs *HookSystemImpl) GetStats() HookSystemStats {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	// Update dynamic stats
	hs.stats.LastUpdated = time.Now()

	// Copy stats to prevent external modification
	return HookSystemStats{
		TotalHooks:      hs.stats.TotalHooks,
		HooksByType:     copyIntMap(hs.stats.HooksByType),
		EnabledTypes:    copyBoolMap(hs.stats.EnabledTypes),
		ExecutionCount:  copyInt64Map(hs.stats.ExecutionCount),
		SuccessCount:    copyInt64Map(hs.stats.SuccessCount),
		ErrorCount:      copyInt64Map(hs.stats.ErrorCount),
		AverageLatency:  copyFloat64Map(hs.stats.AverageLatency),
		LastExecuted:    copyTimeMap(hs.stats.LastExecuted),
		TotalExecutions: hs.stats.TotalExecutions,
		TotalErrors:     hs.stats.TotalErrors,
		SystemUptime:    hs.stats.SystemUptime,
		LastUpdated:     hs.stats.LastUpdated,
	}
}

// EnableHookType enables a specific hook type
func (hs *HookSystemImpl) EnableHookType(hookType plugins.HookType) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.enabledTypes[hookType] = true
	hs.stats.EnabledTypes[hookType] = true

	if hs.logger != nil {
		hs.logger.Info("hook type enabled", logger.String("hook_type", string(hookType)))
	}

	return nil
}

// DisableHookType disables a specific hook type
func (hs *HookSystemImpl) DisableHookType(hookType plugins.HookType) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.enabledTypes[hookType] = false
	hs.stats.EnabledTypes[hookType] = false

	if hs.logger != nil {
		hs.logger.Info("hook type disabled", logger.String("hook_type", string(hookType)))
	}

	return nil
}

// IsHookTypeEnabled checks if a hook type is enabled
func (hs *HookSystemImpl) IsHookTypeEnabled(hookType plugins.HookType) bool {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	return hs.enabledTypes[hookType]
}

// enableAllHookTypes enables all hook types by default
func (hs *HookSystemImpl) enableAllHookTypes() {
	hookTypes := []plugins.HookType{
		plugins.HookTypePreRequest,
		plugins.HookTypePostRequest,
		plugins.HookTypePreMiddleware,
		plugins.HookTypePostMiddleware,
		plugins.HookTypePreRoute,
		plugins.HookTypePostRoute,
		plugins.HookTypeServiceStart,
		plugins.HookTypeServiceStop,
		plugins.HookTypeError,
		plugins.HookTypeHealthCheck,
		plugins.HookTypeMetrics,
	}

	for _, hookType := range hookTypes {
		hs.enabledTypes[hookType] = true
		hs.stats.EnabledTypes[hookType] = true
	}
}

// updateExecutionStats updates execution statistics
func (hs *HookSystemImpl) updateExecutionStats(hookType plugins.HookType, startTime time.Time, resultCount int, err error) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	duration := time.Since(startTime)

	hs.stats.ExecutionCount[hookType]++
	hs.stats.TotalExecutions++
	hs.stats.LastExecuted[hookType] = time.Now()

	if err != nil {
		hs.stats.ErrorCount[hookType]++
		hs.stats.TotalErrors++
	} else {
		hs.stats.SuccessCount[hookType]++
	}

	// Update average latency
	if hs.stats.ExecutionCount[hookType] > 0 {
		currentAvg := hs.stats.AverageLatency[hookType]
		count := float64(hs.stats.ExecutionCount[hookType])
		hs.stats.AverageLatency[hookType] = (currentAvg*(count-1) + duration.Seconds()) / count
	}

	// Record metrics
	if hs.metrics != nil {
		hs.metrics.Counter("forge.hooks.executed", "type", string(hookType)).Inc()
		hs.metrics.Histogram("forge.hooks.execution_duration", "type", string(hookType)).Observe(duration.Seconds())

		if err != nil {
			hs.metrics.Counter("forge.hooks.execution_errors", "type", string(hookType)).Inc()
		} else {
			hs.metrics.Counter("forge.hooks.execution_success", "type", string(hookType)).Inc()
		}
	}
}

// Helper functions for copying maps
func copyIntMap(m map[plugins.HookType]int) map[plugins.HookType]int {
	result := make(map[plugins.HookType]int)
	for k, v := range m {
		result[k] = v
	}
	return result
}

func copyBoolMap(m map[plugins.HookType]bool) map[plugins.HookType]bool {
	result := make(map[plugins.HookType]bool)
	for k, v := range m {
		result[k] = v
	}
	return result
}

func copyInt64Map(m map[plugins.HookType]int64) map[plugins.HookType]int64 {
	result := make(map[plugins.HookType]int64)
	for k, v := range m {
		result[k] = v
	}
	return result
}

func copyFloat64Map(m map[plugins.HookType]float64) map[plugins.HookType]float64 {
	result := make(map[plugins.HookType]float64)
	for k, v := range m {
		result[k] = v
	}
	return result
}

func copyTimeMap(m map[plugins.HookType]time.Time) map[plugins.HookType]time.Time {
	result := make(map[plugins.HookType]time.Time)
	for k, v := range m {
		result[k] = v
	}
	return result
}
