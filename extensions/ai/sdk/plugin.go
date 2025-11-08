package sdk

import (
	"context"
	"fmt"
	"plugin"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// PluginSystem manages dynamic plugin loading and execution.
type PluginSystem struct {
	plugins map[string]Plugin
	logger  forge.Logger
	metrics forge.Metrics
	mu      sync.RWMutex
}

// Plugin represents a loadable plugin.
type Plugin interface {
	Name() string
	Version() string
	Initialize(ctx context.Context) error
	Execute(ctx context.Context, input any) (any, error)
	Shutdown(ctx context.Context) error
}

// PluginInfo contains plugin metadata.
type PluginInfo struct {
	Name        string
	Version     string
	Description string
	Author      string
	Loaded      bool
}

// NewPluginSystem creates a new plugin system.
func NewPluginSystem(logger forge.Logger, metrics forge.Metrics) *PluginSystem {
	return &PluginSystem{
		plugins: make(map[string]Plugin),
		logger:  logger,
		metrics: metrics,
	}
}

// LoadPlugin loads a plugin from a shared library file.
func (ps *PluginSystem) LoadPlugin(ctx context.Context, path string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Load the plugin
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open plugin: %w", err)
	}

	// Look up the New function
	sym, err := p.Lookup("New")
	if err != nil {
		return fmt.Errorf("plugin missing New function: %w", err)
	}

	// Assert the correct function signature
	newFunc, ok := sym.(func() Plugin)
	if !ok {
		return errors.New("invalid New function signature")
	}

	// Create plugin instance
	plugin := newFunc()

	// Initialize plugin
	if err := plugin.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	// Register plugin
	ps.plugins[plugin.Name()] = plugin

	if ps.logger != nil {
		ps.logger.Info("plugin loaded",
			F("name", plugin.Name()),
			F("version", plugin.Version()),
		)
	}

	if ps.metrics != nil {
		ps.metrics.Counter("ai.sdk.plugins.loaded",
			"name", plugin.Name(),
		).Inc()
	}

	return nil
}

// RegisterPlugin registers a plugin instance.
func (ps *PluginSystem) RegisterPlugin(ctx context.Context, p Plugin) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Initialize plugin
	if err := p.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	ps.plugins[p.Name()] = p

	if ps.logger != nil {
		ps.logger.Info("plugin registered",
			F("name", p.Name()),
		)
	}

	return nil
}

// UnloadPlugin unloads a plugin.
func (ps *PluginSystem) UnloadPlugin(ctx context.Context, name string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	p, exists := ps.plugins[name]
	if !exists {
		return fmt.Errorf("plugin not found: %s", name)
	}

	// Shutdown plugin
	if err := p.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown plugin: %w", err)
	}

	delete(ps.plugins, name)

	if ps.logger != nil {
		ps.logger.Info("plugin unloaded", F("name", name))
	}

	return nil
}

// ExecutePlugin executes a plugin by name.
func (ps *PluginSystem) ExecutePlugin(ctx context.Context, name string, input any) (any, error) {
	ps.mu.RLock()
	p, exists := ps.plugins[name]
	ps.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}

	result, err := p.Execute(ctx, input)
	if err != nil {
		if ps.logger != nil {
			ps.logger.Error("plugin execution failed",
				F("name", name),
				F("error", err),
			)
		}

		return nil, err
	}

	if ps.metrics != nil {
		ps.metrics.Counter("ai.sdk.plugins.executions",
			"name", name,
			"status", "success",
		).Inc()
	}

	return result, nil
}

// ListPlugins returns a list of loaded plugins.
func (ps *PluginSystem) ListPlugins() []PluginInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	infos := make([]PluginInfo, 0, len(ps.plugins))
	for _, p := range ps.plugins {
		infos = append(infos, PluginInfo{
			Name:    p.Name(),
			Version: p.Version(),
			Loaded:  true,
		})
	}

	return infos
}

// GetPlugin returns a plugin by name.
func (ps *PluginSystem) GetPlugin(name string) (Plugin, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	p, exists := ps.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}

	return p, nil
}

// Shutdown shuts down all plugins.
func (ps *PluginSystem) Shutdown(ctx context.Context) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	for name, p := range ps.plugins {
		if err := p.Shutdown(ctx); err != nil {
			if ps.logger != nil {
				ps.logger.Error("failed to shutdown plugin",
					F("name", name),
					F("error", err),
				)
			}
		}
	}

	ps.plugins = make(map[string]Plugin)

	return nil
}

// --- Extension Points ---

// Middleware plugin interface.
type MiddlewarePlugin interface {
	Plugin
	Middleware(next func(context.Context, any) (any, error)) func(context.Context, any) (any, error)
}

// Tool plugin interface.
type ToolPlugin interface {
	Plugin
	GetTool() Tool
}

// Provider plugin interface.
type ProviderPlugin interface {
	Plugin
	GetProvider() LLMManager
}
