package forge

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge/internal/errors"
)

// LifecyclePhase represents a phase in the application lifecycle.
type LifecyclePhase string

const (
	// PhaseBeforeStart is called before the app starts (before extensions register).
	PhaseBeforeStart LifecyclePhase = "before_start"

	// PhaseAfterRegister is called after extensions register but before they start.
	PhaseAfterRegister LifecyclePhase = "after_register"

	// PhaseAfterStart is called after the app starts (after all extensions start).
	PhaseAfterStart LifecyclePhase = "after_start"

	// PhaseBeforeRun is called before the HTTP server starts listening.
	PhaseBeforeRun LifecyclePhase = "before_run"

	// PhaseAfterRun is called after the HTTP server starts (in a goroutine, non-blocking).
	PhaseAfterRun LifecyclePhase = "after_run"

	// PhaseBeforeStop is called before the app stops (before graceful shutdown).
	PhaseBeforeStop LifecyclePhase = "before_stop"

	// PhaseAfterStop is called after the app stops (after all extensions stop).
	PhaseAfterStop LifecyclePhase = "after_stop"
)

// LifecycleHook is a function called during a lifecycle phase
// Hooks receive the App instance and a context for cancellation.
type LifecycleHook func(ctx context.Context, app App) error

// LifecycleHookOptions configures a lifecycle hook.
type LifecycleHookOptions struct {
	// Name is a unique identifier for this hook (for logging/debugging)
	Name string

	// Priority determines execution order (higher priority runs first)
	// Default: 0
	Priority int

	// ContinueOnError determines if subsequent hooks run if this hook fails
	// Default: false (stop on error)
	ContinueOnError bool
}

// DefaultLifecycleHookOptions returns default hook options.
func DefaultLifecycleHookOptions(name string) LifecycleHookOptions {
	return LifecycleHookOptions{
		Name:            name,
		Priority:        0,
		ContinueOnError: false,
	}
}

// lifecycleHookEntry wraps a hook with its options.
type lifecycleHookEntry struct {
	hook LifecycleHook
	opts LifecycleHookOptions
}

// LifecycleManager manages lifecycle hooks.
type LifecycleManager interface {
	// RegisterHook registers a hook for a specific lifecycle phase
	RegisterHook(phase LifecyclePhase, hook LifecycleHook, opts LifecycleHookOptions) error

	// RegisterHookFn is a convenience method to register a hook with default options
	RegisterHookFn(phase LifecyclePhase, name string, hook LifecycleHook) error

	// ExecuteHooks executes all hooks for a given phase
	ExecuteHooks(ctx context.Context, phase LifecyclePhase, app App) error

	// GetHooks returns all hooks for a given phase (for inspection)
	GetHooks(phase LifecyclePhase) []LifecycleHookOptions

	// RemoveHook removes a hook by name
	RemoveHook(phase LifecyclePhase, name string) error

	// ClearHooks removes all hooks for a given phase
	ClearHooks(phase LifecyclePhase)
}

// lifecycleManager implements LifecycleManager.
type lifecycleManager struct {
	mu     sync.RWMutex
	hooks  map[LifecyclePhase][]lifecycleHookEntry
	logger Logger
}

// NewLifecycleManager creates a new lifecycle manager.
func NewLifecycleManager(logger Logger) LifecycleManager {
	if logger == nil {
		logger = NewNoopLogger()
	}

	return &lifecycleManager{
		hooks:  make(map[LifecyclePhase][]lifecycleHookEntry),
		logger: logger,
	}
}

// RegisterHook registers a hook for a specific lifecycle phase.
func (m *lifecycleManager) RegisterHook(phase LifecyclePhase, hook LifecycleHook, opts LifecycleHookOptions) error {
	if hook == nil {
		return errors.New("hook cannot be nil")
	}

	if opts.Name == "" {
		return errors.New("hook name is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicate name in this phase
	for _, entry := range m.hooks[phase] {
		if entry.opts.Name == opts.Name {
			return fmt.Errorf("hook with name %s already registered for phase %s", opts.Name, phase)
		}
	}

	// Add hook
	m.hooks[phase] = append(m.hooks[phase], lifecycleHookEntry{
		hook: hook,
		opts: opts,
	})

	// Sort by priority (higher priority first)
	m.sortHooks(phase)

	m.logger.Debug("lifecycle hook registered",
		F("phase", string(phase)),
		F("name", opts.Name),
		F("priority", opts.Priority),
	)

	return nil
}

// RegisterHookFn is a convenience method to register a hook with default options.
func (m *lifecycleManager) RegisterHookFn(phase LifecyclePhase, name string, hook LifecycleHook) error {
	return m.RegisterHook(phase, hook, DefaultLifecycleHookOptions(name))
}

// ExecuteHooks executes all hooks for a given phase.
func (m *lifecycleManager) ExecuteHooks(ctx context.Context, phase LifecyclePhase, app App) error {
	m.mu.RLock()
	hooks := m.hooks[phase]
	m.mu.RUnlock()

	if len(hooks) == 0 {
		return nil
	}

	m.logger.Debug("executing lifecycle hooks",
		F("phase", string(phase)),
		F("count", len(hooks)),
	)

	var firstError error

	executedCount := 0

	for _, entry := range hooks {
		m.logger.Debug("executing lifecycle hook",
			F("phase", string(phase)),
			F("name", entry.opts.Name),
			F("priority", entry.opts.Priority),
		)

		if err := entry.hook(ctx, app); err != nil {
			m.logger.Error("lifecycle hook failed",
				F("phase", string(phase)),
				F("name", entry.opts.Name),
				F("error", err),
			)

			if firstError == nil {
				firstError = fmt.Errorf("hook %s failed: %w", entry.opts.Name, err)
			}

			// Stop execution unless ContinueOnError is true
			if !entry.opts.ContinueOnError {
				m.logger.Warn("stopping hook execution due to error",
					F("phase", string(phase)),
					F("failed_hook", entry.opts.Name),
					F("executed", executedCount),
					F("remaining", len(hooks)-executedCount-1),
				)

				return firstError
			}
		} else {
			m.logger.Debug("lifecycle hook completed",
				F("phase", string(phase)),
				F("name", entry.opts.Name),
			)

			executedCount++
		}
	}

	m.logger.Debug("lifecycle hooks completed",
		F("phase", string(phase)),
		F("executed", executedCount),
		F("total", len(hooks)),
	)

	return firstError
}

// GetHooks returns all hooks for a given phase (for inspection).
func (m *lifecycleManager) GetHooks(phase LifecyclePhase) []LifecycleHookOptions {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hooks := m.hooks[phase]

	result := make([]LifecycleHookOptions, len(hooks))
	for i, entry := range hooks {
		result[i] = entry.opts
	}

	return result
}

// RemoveHook removes a hook by name.
func (m *lifecycleManager) RemoveHook(phase LifecyclePhase, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	hooks := m.hooks[phase]
	for i, entry := range hooks {
		if entry.opts.Name == name {
			// Remove hook by slicing
			m.hooks[phase] = append(hooks[:i], hooks[i+1:]...)
			m.logger.Debug("lifecycle hook removed",
				F("phase", string(phase)),
				F("name", name),
			)

			return nil
		}
	}

	return fmt.Errorf("hook %s not found for phase %s", name, phase)
}

// ClearHooks removes all hooks for a given phase.
func (m *lifecycleManager) ClearHooks(phase LifecyclePhase) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := len(m.hooks[phase])
	m.hooks[phase] = nil

	m.logger.Debug("lifecycle hooks cleared",
		F("phase", string(phase)),
		F("count", count),
	)
}

// sortHooks sorts hooks by priority (higher priority first).
func (m *lifecycleManager) sortHooks(phase LifecyclePhase) {
	hooks := m.hooks[phase]
	// Simple bubble sort (sufficient for small lists)
	for i := range hooks {
		for j := i + 1; j < len(hooks); j++ {
			if hooks[j].opts.Priority > hooks[i].opts.Priority {
				hooks[i], hooks[j] = hooks[j], hooks[i]
			}
		}
	}
}
