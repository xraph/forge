package cli

import (
	"fmt"
	"sort"
)

// PluginRegistry manages CLI plugins
type PluginRegistry struct {
	plugins  map[string]Plugin
	metadata map[string]*PluginMetadata
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins:  make(map[string]Plugin),
		metadata: make(map[string]*PluginMetadata),
	}
}

// Register registers a plugin
func (r *PluginRegistry) Register(plugin Plugin) error {
	name := plugin.Name()

	// Check for duplicate
	if _, exists := r.plugins[name]; exists {
		return ErrPluginAlreadyRegistered
	}

	// Check dependencies
	for _, dep := range plugin.Dependencies() {
		if _, exists := r.plugins[dep]; !exists {
			return fmt.Errorf("missing dependency: %s requires %s", name, dep)
		}
	}

	// Initialize plugin
	if err := plugin.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize plugin %s: %w", name, err)
	}

	// Store plugin
	r.plugins[name] = plugin

	// Store metadata
	commandNames := []string{}
	for _, cmd := range plugin.Commands() {
		commandNames = append(commandNames, cmd.Name())
	}

	r.metadata[name] = &PluginMetadata{
		Name:         name,
		Version:      plugin.Version(),
		Description:  plugin.Description(),
		Dependencies: plugin.Dependencies(),
		Commands:     commandNames,
	}

	return nil
}

// Get retrieves a plugin by name
func (r *PluginRegistry) Get(name string) (Plugin, error) {
	plugin, exists := r.plugins[name]
	if !exists {
		return nil, ErrPluginNotFound
	}
	return plugin, nil
}

// Has checks if a plugin exists
func (r *PluginRegistry) Has(name string) bool {
	_, exists := r.plugins[name]
	return exists
}

// All returns all registered plugins
func (r *PluginRegistry) All() []Plugin {
	plugins := make([]Plugin, 0, len(r.plugins))
	for _, plugin := range r.plugins {
		plugins = append(plugins, plugin)
	}
	return plugins
}

// Metadata returns metadata for all plugins
func (r *PluginRegistry) Metadata() []*PluginMetadata {
	metadata := make([]*PluginMetadata, 0, len(r.metadata))
	for _, meta := range r.metadata {
		metadata = append(metadata, meta)
	}
	return metadata
}

// GetMetadata returns metadata for a specific plugin
func (r *PluginRegistry) GetMetadata(name string) (*PluginMetadata, error) {
	meta, exists := r.metadata[name]
	if !exists {
		return nil, ErrPluginNotFound
	}
	return meta, nil
}

// RegisterAll registers multiple plugins with dependency resolution
func (r *PluginRegistry) RegisterAll(plugins []Plugin) error {
	// Sort plugins by dependencies
	sorted, err := r.sortByDependencies(plugins)
	if err != nil {
		return err
	}

	// Register in order
	for _, plugin := range sorted {
		if err := r.Register(plugin); err != nil {
			return err
		}
	}

	return nil
}

// sortByDependencies sorts plugins by their dependencies using topological sort
func (r *PluginRegistry) sortByDependencies(plugins []Plugin) ([]Plugin, error) {
	// Build dependency graph
	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	pluginMap := make(map[string]Plugin)

	for _, plugin := range plugins {
		name := plugin.Name()
		pluginMap[name] = plugin
		graph[name] = plugin.Dependencies()
		inDegree[name] = 0
	}

	// Calculate in-degrees
	for name := range graph {
		for _, dep := range graph[name] {
			// Check if dependency is in the plugin list or already registered
			if _, exists := pluginMap[dep]; !exists && !r.Has(dep) {
				return nil, fmt.Errorf("missing dependency: %s requires %s", name, dep)
			}
			if _, exists := pluginMap[dep]; exists {
				inDegree[dep]++
			}
		}
	}

	// Topological sort using Kahn's algorithm
	queue := []string{}
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	sorted := []Plugin{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		sorted = append(sorted, pluginMap[name])

		for _, neighbor := range graph[name] {
			if _, exists := pluginMap[neighbor]; exists {
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					queue = append(queue, neighbor)
				}
			}
		}
	}

	// Check for circular dependencies
	if len(sorted) != len(plugins) {
		return nil, ErrCircularDependency
	}

	return sorted, nil
}

// Names returns the names of all registered plugins sorted alphabetically
func (r *PluginRegistry) Names() []string {
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of registered plugins
func (r *PluginRegistry) Count() int {
	return len(r.plugins)
}
