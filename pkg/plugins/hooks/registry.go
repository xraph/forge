package hooks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/plugins/common"
)

// HookRegistry manages hook registration, metadata, and discovery
type HookRegistry interface {
	Initialize(ctx context.Context) error
	Stop(ctx context.Context) error
	RegisterHook(hook plugins.Hook) error
	UnregisterHook(hookName string, hookType plugins.HookType) error
	GetHook(hookName string, hookType plugins.HookType) (plugins.Hook, error)
	GetHooksByType(hookType plugins.HookType) []plugins.Hook
	GetHooksByName(hookName string) []plugins.Hook
	GetAllHooks() map[plugins.HookType][]plugins.Hook
	SearchHooks(query HookQuery) []plugins.Hook
	GetHookMetadata(hookName string, hookType plugins.HookType) (*HookMetadata, error)
	UpdateHookMetadata(hookName string, hookType plugins.HookType, metadata *HookMetadata) error
	GetRegistryStats() HookRegistryStats
	ValidateHook(hook plugins.Hook) error
	GetHookDependencies(hookName string, hookType plugins.HookType) ([]HookDependency, error)
}

// HookRegistryImpl implements the HookRegistry interface
type HookRegistryImpl struct {
	hooks        map[plugins.HookType]map[string]*HookEntry
	metadata     map[string]*HookMetadata
	dependencies map[string][]HookDependency
	validator    HookValidator
	logger       common.Logger
	metrics      common.Metrics
	mu           sync.RWMutex
	initialized  bool
	stats        HookRegistryStats
	startTime    time.Time
}

// HookEntry represents a registered hook with metadata
type HookEntry struct {
	Hook         plugins.Hook
	Metadata     *HookMetadata
	RegisteredAt time.Time
	LastUsed     time.Time
	UsageCount   int64
	ErrorCount   int64
	mu           sync.RWMutex
}

// HookMetadata contains metadata about a hook
type HookMetadata struct {
	Name             string                 `json:"name"`
	Type             plugins.HookType       `json:"type"`
	Priority         int                    `json:"priority"`
	Description      string                 `json:"description"`
	Author           string                 `json:"author"`
	Version          string                 `json:"version"`
	Tags             []string               `json:"tags"`
	Categories       []string               `json:"categories"`
	Dependencies     []HookDependency       `json:"dependencies"`
	Capabilities     []string               `json:"capabilities"`
	Configuration    map[string]interface{} `json:"configuration"`
	DocumentationURL string                 `json:"documentation_url"`
	SourceURL        string                 `json:"source_url"`
	License          string                 `json:"license"`
	RegisteredAt     time.Time              `json:"registered_at"`
	LastUpdated      time.Time              `json:"last_updated"`
	Statistics       HookStatistics         `json:"statistics"`
}

// HookDependency represents a hook dependency
type HookDependency struct {
	Name     string           `json:"name"`
	Type     plugins.HookType `json:"type"`
	Required bool             `json:"required"`
	Version  string           `json:"version"`
}

// HookStatistics contains hook usage statistics
type HookStatistics struct {
	UsageCount     int64     `json:"usage_count"`
	ErrorCount     int64     `json:"error_count"`
	SuccessCount   int64     `json:"success_count"`
	AverageLatency float64   `json:"average_latency"`
	TotalLatency   float64   `json:"total_latency"`
	LastUsed       time.Time `json:"last_used"`
	LastError      string    `json:"last_error,omitempty"`
	SuccessRate    float64   `json:"success_rate"`
}

// HookQuery for searching hooks
type HookQuery struct {
	Name         string           `json:"name,omitempty"`
	Type         plugins.HookType `json:"type,omitempty"`
	Tags         []string         `json:"tags,omitempty"`
	Categories   []string         `json:"categories,omitempty"`
	Author       string           `json:"author,omitempty"`
	MinPriority  *int             `json:"min_priority,omitempty"`
	MaxPriority  *int             `json:"max_priority,omitempty"`
	Capabilities []string         `json:"capabilities,omitempty"`
	SortBy       string           `json:"sort_by,omitempty"`    // name, priority, usage, recent
	SortOrder    string           `json:"sort_order,omitempty"` // asc, desc
	Limit        int              `json:"limit,omitempty"`
	Offset       int              `json:"offset,omitempty"`
}

// HookRegistryStats contains registry statistics
type HookRegistryStats struct {
	TotalHooks      int                      `json:"total_hooks"`
	HooksByType     map[plugins.HookType]int `json:"hooks_by_type"`
	HooksByCategory map[string]int           `json:"hooks_by_category"`
	TotalUsage      int64                    `json:"total_usage"`
	TotalErrors     int64                    `json:"total_errors"`
	AverageLatency  float64                  `json:"average_latency"`
	LastRegistered  time.Time                `json:"last_registered"`
	LastUsed        time.Time                `json:"last_used"`
	RegistryUptime  time.Duration            `json:"registry_uptime"`
	TopHooksByUsage []HookUsageInfo          `json:"top_hooks_by_usage"`
	RecentlyUsed    []HookUsageInfo          `json:"recently_used"`
}

// HookUsageInfo contains information about hook usage
type HookUsageInfo struct {
	Name        string           `json:"name"`
	Type        plugins.HookType `json:"type"`
	UsageCount  int64            `json:"usage_count"`
	ErrorCount  int64            `json:"error_count"`
	SuccessRate float64          `json:"success_rate"`
	LastUsed    time.Time        `json:"last_used"`
}

// HookValidator validates hooks before registration
type HookValidator interface {
	ValidateHook(hook plugins.Hook) error
	ValidateHookCompatibility(hook plugins.Hook, existingHooks []plugins.Hook) error
	ValidateDependencies(hook plugins.Hook, dependencies []HookDependency) error
}

// NewHookRegistry creates a new hook registry
func NewHookRegistry(logger common.Logger, metrics common.Metrics) HookRegistry {
	return &HookRegistryImpl{
		hooks:        make(map[plugins.HookType]map[string]*HookEntry),
		metadata:     make(map[string]*HookMetadata),
		dependencies: make(map[string][]HookDependency),
		validator:    NewHookValidator(),
		logger:       logger,
		metrics:      metrics,
		stats: HookRegistryStats{
			HooksByType:     make(map[plugins.HookType]int),
			HooksByCategory: make(map[string]int),
			TopHooksByUsage: make([]HookUsageInfo, 0),
			RecentlyUsed:    make([]HookUsageInfo, 0),
		},
	}
}

// Initialize initializes the hook registry
func (hr *HookRegistryImpl) Initialize(ctx context.Context) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	if hr.initialized {
		return nil
	}

	hr.initialized = true
	hr.startTime = time.Now()

	if hr.logger != nil {
		hr.logger.Info("hook registry initialized")
	}

	if hr.metrics != nil {
		hr.metrics.Counter("forge.hooks.registry_initialized").Inc()
	}

	return nil
}

// Stop stops the hook registry
func (hr *HookRegistryImpl) Stop(ctx context.Context) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	if !hr.initialized {
		return nil
	}

	hr.initialized = false

	if hr.logger != nil {
		hr.logger.Info("hook registry stopped")
	}

	if hr.metrics != nil {
		hr.metrics.Counter("forge.hooks.registry_stopped").Inc()
	}

	return nil
}

// RegisterHook registers a hook in the registry
func (hr *HookRegistryImpl) RegisterHook(hook plugins.Hook) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Validate hook
	if err := hr.validator.ValidateHook(hook); err != nil {
		return fmt.Errorf("hook validation failed: %w", err)
	}

	hookType := hook.Type()
	hookName := hook.Name()

	// Initialize type map if needed
	if hr.hooks[hookType] == nil {
		hr.hooks[hookType] = make(map[string]*HookEntry)
	}

	// Check if hook already exists
	if _, exists := hr.hooks[hookType][hookName]; exists {
		return fmt.Errorf("hook '%s' of type '%s' already registered", hookName, hookType)
	}

	// Create metadata
	metadata := &HookMetadata{
		Name:         hookName,
		Type:         hookType,
		Priority:     hook.Priority(),
		RegisteredAt: time.Now(),
		LastUpdated:  time.Now(),
		Statistics: HookStatistics{
			UsageCount:   0,
			ErrorCount:   0,
			SuccessCount: 0,
			SuccessRate:  0,
		},
	}

	// Create hook entry
	entry := &HookEntry{
		Hook:         hook,
		Metadata:     metadata,
		RegisteredAt: time.Now(),
		UsageCount:   0,
		ErrorCount:   0,
	}

	// Register hook
	hr.hooks[hookType][hookName] = entry
	hr.metadata[hr.getHookKey(hookName, hookType)] = metadata

	// Update statistics
	hr.stats.TotalHooks++
	hr.stats.HooksByType[hookType]++
	hr.stats.LastRegistered = time.Now()

	if hr.logger != nil {
		hr.logger.Info("hook registered in registry",
			logger.String("hook_name", hookName),
			logger.String("hook_type", string(hookType)),
			logger.Int("priority", hook.Priority()),
		)
	}

	if hr.metrics != nil {
		hr.metrics.Counter("forge.hooks.registry.registered", "type", string(hookType)).Inc()
		hr.metrics.Gauge("forge.hooks.registry.total").Set(float64(hr.stats.TotalHooks))
	}

	return nil
}

// UnregisterHook unregisters a hook from the registry
func (hr *HookRegistryImpl) UnregisterHook(hookName string, hookType plugins.HookType) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Check if type exists
	typeHooks, exists := hr.hooks[hookType]
	if !exists {
		return fmt.Errorf("no hooks of type '%s' registered", hookType)
	}

	// Check if hook exists
	entry, exists := typeHooks[hookName]
	if !exists {
		return fmt.Errorf("hook '%s' of type '%s' not registered", hookName, hookType)
	}

	// Remove hook
	delete(typeHooks, hookName)
	delete(hr.metadata, hr.getHookKey(hookName, hookType))
	delete(hr.dependencies, hr.getHookKey(hookName, hookType))

	// Update statistics
	hr.stats.TotalHooks--
	hr.stats.HooksByType[hookType]--
	hr.stats.TotalUsage += entry.UsageCount
	hr.stats.TotalErrors += entry.ErrorCount

	if hr.logger != nil {
		hr.logger.Info("hook unregistered from registry",
			logger.String("hook_name", hookName),
			logger.String("hook_type", string(hookType)),
		)
	}

	if hr.metrics != nil {
		hr.metrics.Counter("forge.hooks.registry.unregistered", "type", string(hookType)).Inc()
		hr.metrics.Gauge("forge.hooks.registry.total").Set(float64(hr.stats.TotalHooks))
	}

	return nil
}

// GetHook retrieves a specific hook
func (hr *HookRegistryImpl) GetHook(hookName string, hookType plugins.HookType) (plugins.Hook, error) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	typeHooks, exists := hr.hooks[hookType]
	if !exists {
		return nil, fmt.Errorf("no hooks of type '%s' registered", hookType)
	}

	entry, exists := typeHooks[hookName]
	if !exists {
		return nil, fmt.Errorf("hook '%s' of type '%s' not found", hookName, hookType)
	}

	// Update usage statistics
	go hr.updateHookUsage(entry)

	return entry.Hook, nil
}

// GetHooksByType returns all hooks of a specific type
func (hr *HookRegistryImpl) GetHooksByType(hookType plugins.HookType) []plugins.Hook {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	typeHooks, exists := hr.hooks[hookType]
	if !exists {
		return []plugins.Hook{}
	}

	hooks := make([]plugins.Hook, 0, len(typeHooks))
	for _, entry := range typeHooks {
		hooks = append(hooks, entry.Hook)
	}

	return hooks
}

// GetHooksByName returns all hooks with a specific name (across all types)
func (hr *HookRegistryImpl) GetHooksByName(hookName string) []plugins.Hook {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	var hooks []plugins.Hook
	for _, typeHooks := range hr.hooks {
		if entry, exists := typeHooks[hookName]; exists {
			hooks = append(hooks, entry.Hook)
		}
	}

	return hooks
}

// GetAllHooks returns all registered hooks
func (hr *HookRegistryImpl) GetAllHooks() map[plugins.HookType][]plugins.Hook {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	result := make(map[plugins.HookType][]plugins.Hook)
	for hookType, typeHooks := range hr.hooks {
		hooks := make([]plugins.Hook, 0, len(typeHooks))
		for _, entry := range typeHooks {
			hooks = append(hooks, entry.Hook)
		}
		result[hookType] = hooks
	}

	return result
}

// SearchHooks searches for hooks based on criteria
func (hr *HookRegistryImpl) SearchHooks(query HookQuery) []plugins.Hook {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	var matches []*HookEntry

	// Collect all matching hooks
	for _, typeHooks := range hr.hooks {
		for _, entry := range typeHooks {
			if hr.matchesQuery(entry, query) {
				matches = append(matches, entry)
			}
		}
	}

	// Sort results
	hr.sortHookEntries(matches, query.SortBy, query.SortOrder)

	// Apply pagination
	start := query.Offset
	if start >= len(matches) {
		return []plugins.Hook{}
	}

	end := len(matches)
	if query.Limit > 0 && start+query.Limit < len(matches) {
		end = start + query.Limit
	}

	// Extract hooks from entries
	result := make([]plugins.Hook, 0, end-start)
	for i := start; i < end; i++ {
		result = append(result, matches[i].Hook)
	}

	return result
}

// GetHookMetadata retrieves metadata for a hook
func (hr *HookRegistryImpl) GetHookMetadata(hookName string, hookType plugins.HookType) (*HookMetadata, error) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	key := hr.getHookKey(hookName, hookType)
	metadata, exists := hr.metadata[key]
	if !exists {
		return nil, fmt.Errorf("metadata not found for hook '%s' of type '%s'", hookName, hookType)
	}

	// Return a copy to prevent external modification
	return hr.copyMetadata(metadata), nil
}

// UpdateHookMetadata updates metadata for a hook
func (hr *HookRegistryImpl) UpdateHookMetadata(hookName string, hookType plugins.HookType, metadata *HookMetadata) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	key := hr.getHookKey(hookName, hookType)
	existing, exists := hr.metadata[key]
	if !exists {
		return fmt.Errorf("hook '%s' of type '%s' not found", hookName, hookType)
	}

	// Update metadata
	existing.Description = metadata.Description
	existing.Author = metadata.Author
	existing.Version = metadata.Version
	existing.Tags = metadata.Tags
	existing.Categories = metadata.Categories
	existing.Dependencies = metadata.Dependencies
	existing.Capabilities = metadata.Capabilities
	existing.Configuration = metadata.Configuration
	existing.DocumentationURL = metadata.DocumentationURL
	existing.SourceURL = metadata.SourceURL
	existing.License = metadata.License
	existing.LastUpdated = time.Now()

	return nil
}

// GetRegistryStats returns registry statistics
func (hr *HookRegistryImpl) GetRegistryStats() HookRegistryStats {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	// Update dynamic statistics
	hr.stats.RegistryUptime = time.Since(hr.startTime)

	// Calculate top hooks by usage
	hr.stats.TopHooksByUsage = hr.getTopHooksByUsage(10)
	hr.stats.RecentlyUsed = hr.getRecentlyUsedHooks(10)

	return hr.stats
}

// ValidateHook validates a hook before operations
func (hr *HookRegistryImpl) ValidateHook(hook plugins.Hook) error {
	return hr.validator.ValidateHook(hook)
}

// GetHookDependencies returns dependencies for a hook
func (hr *HookRegistryImpl) GetHookDependencies(hookName string, hookType plugins.HookType) ([]HookDependency, error) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	key := hr.getHookKey(hookName, hookType)
	dependencies, exists := hr.dependencies[key]
	if !exists {
		return []HookDependency{}, nil
	}

	return dependencies, nil
}

// Helper methods

func (hr *HookRegistryImpl) getHookKey(hookName string, hookType plugins.HookType) string {
	return fmt.Sprintf("%s:%s", hookType, hookName)
}

func (hr *HookRegistryImpl) matchesQuery(entry *HookEntry, query HookQuery) bool {
	metadata := entry.Metadata

	// Name filter
	if query.Name != "" && metadata.Name != query.Name {
		return false
	}

	// Type filter
	if query.Type != "" && metadata.Type != query.Type {
		return false
	}

	// Priority filter
	if query.MinPriority != nil && metadata.Priority < *query.MinPriority {
		return false
	}
	if query.MaxPriority != nil && metadata.Priority > *query.MaxPriority {
		return false
	}

	// Author filter
	if query.Author != "" && metadata.Author != query.Author {
		return false
	}

	// Tags filter
	if len(query.Tags) > 0 {
		if !hr.containsAny(metadata.Tags, query.Tags) {
			return false
		}
	}

	// Categories filter
	if len(query.Categories) > 0 {
		if !hr.containsAny(metadata.Categories, query.Categories) {
			return false
		}
	}

	// Capabilities filter
	if len(query.Capabilities) > 0 {
		if !hr.containsAny(metadata.Capabilities, query.Capabilities) {
			return false
		}
	}

	return true
}

func (hr *HookRegistryImpl) containsAny(slice1, slice2 []string) bool {
	for _, item1 := range slice1 {
		for _, item2 := range slice2 {
			if item1 == item2 {
				return true
			}
		}
	}
	return false
}

func (hr *HookRegistryImpl) sortHookEntries(entries []*HookEntry, sortBy, sortOrder string) {
	// Implementation of sorting logic would go here
	// For brevity, this is simplified
}

func (hr *HookRegistryImpl) updateHookUsage(entry *HookEntry) {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.UsageCount++
	entry.LastUsed = time.Now()
	entry.Metadata.Statistics.UsageCount++
	entry.Metadata.Statistics.LastUsed = time.Now()
}

func (hr *HookRegistryImpl) copyMetadata(metadata *HookMetadata) *HookMetadata {
	copy := *metadata
	copy.Tags = make([]string, len(metadata.Tags))
	copy.Categories = make([]string, len(metadata.Categories))
	copy.Dependencies = make([]HookDependency, len(metadata.Dependencies))
	copy.Capabilities = make([]string, len(metadata.Capabilities))
	copy.Configuration = make(map[string]interface{})

	for k, v := range metadata.Configuration {
		copy.Configuration[k] = v
	}

	return &copy
}

func (hr *HookRegistryImpl) getTopHooksByUsage(limit int) []HookUsageInfo {
	// Implementation would collect and sort hooks by usage
	return []HookUsageInfo{}
}

func (hr *HookRegistryImpl) getRecentlyUsedHooks(limit int) []HookUsageInfo {
	// Implementation would collect and sort hooks by recent usage
	return []HookUsageInfo{}
}

// BasicHookValidator provides basic hook validation
type BasicHookValidator struct{}

// NewHookValidator creates a new hook validator
func NewHookValidator() HookValidator {
	return &BasicHookValidator{}
}

// ValidateHook validates a hook
func (bhv *BasicHookValidator) ValidateHook(hook plugins.Hook) error {
	if hook == nil {
		return fmt.Errorf("hook cannot be nil")
	}

	if hook.Name() == "" {
		return fmt.Errorf("hook name cannot be empty")
	}

	if hook.Type() == "" {
		return fmt.Errorf("hook type cannot be empty")
	}

	return nil
}

// ValidateHookCompatibility validates hook compatibility with existing hooks
func (bhv *BasicHookValidator) ValidateHookCompatibility(hook plugins.Hook, existingHooks []plugins.Hook) error {
	// Basic compatibility checks
	for _, existing := range existingHooks {
		if existing.Name() == hook.Name() && existing.Type() == hook.Type() {
			return fmt.Errorf("hook with same name and type already exists")
		}
	}
	return nil
}

// ValidateDependencies validates hook dependencies
func (bhv *BasicHookValidator) ValidateDependencies(hook plugins.Hook, dependencies []HookDependency) error {
	// Basic dependency validation
	for _, dep := range dependencies {
		if dep.Name == "" {
			return fmt.Errorf("dependency name cannot be empty")
		}
		if dep.Type == "" {
			return fmt.Errorf("dependency type cannot be empty")
		}
	}
	return nil
}
