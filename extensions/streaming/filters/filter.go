package filters

import (
	"context"
	"fmt"
	"sort"
	"sync"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// MessageFilter represents a filter applied before message delivery.
type MessageFilter interface {
	// Name returns unique filter identifier
	Name() string

	// Filter processes message and returns filtered result
	// Returns nil to block message
	Filter(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error)

	// Priority returns filter execution order (lower = earlier)
	Priority() int
}

// FilterChain manages filter execution.
type FilterChain interface {
	// Add registers a filter
	Add(filter MessageFilter)

	// Remove unregisters a filter
	Remove(filterName string)

	// Apply runs all filters in priority order
	Apply(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error)

	// List returns all registered filters
	List() []MessageFilter
}

// filterChain implements FilterChain.
type filterChain struct {
	mu      sync.RWMutex
	filters map[string]MessageFilter
	sorted  []MessageFilter
}

// NewFilterChain creates a new filter chain.
func NewFilterChain() FilterChain {
	return &filterChain{
		filters: make(map[string]MessageFilter),
		sorted:  make([]MessageFilter, 0),
	}
}

// Add registers a filter.
func (fc *filterChain) Add(filter MessageFilter) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.filters[filter.Name()] = filter
	fc.rebuildSorted()
}

// Remove unregisters a filter.
func (fc *filterChain) Remove(filterName string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	delete(fc.filters, filterName)
	fc.rebuildSorted()
}

// Apply runs all filters in priority order.
func (fc *filterChain) Apply(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
	fc.mu.RLock()
	filters := fc.sorted
	fc.mu.RUnlock()

	currentMsg := msg
	for _, filter := range filters {
		filtered, err := filter.Filter(ctx, currentMsg, recipient)
		if err != nil {
			return nil, fmt.Errorf("filter %s failed: %w", filter.Name(), err)
		}

		// Nil means block the message
		if filtered == nil {
			return nil, nil
		}

		currentMsg = filtered
	}

	return currentMsg, nil
}

// List returns all registered filters.
func (fc *filterChain) List() []MessageFilter {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	result := make([]MessageFilter, len(fc.sorted))
	copy(result, fc.sorted)

	return result
}

// rebuildSorted rebuilds the sorted filter list (must be called with lock held).
func (fc *filterChain) rebuildSorted() {
	fc.sorted = make([]MessageFilter, 0, len(fc.filters))
	for _, filter := range fc.filters {
		fc.sorted = append(fc.sorted, filter)
	}

	// Sort by priority (lower = earlier)
	sort.Slice(fc.sorted, func(i, j int) bool {
		return fc.sorted[i].Priority() < fc.sorted[j].Priority()
	})
}

// FilterFunc is a function adapter for simple filters.
type FilterFunc func(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error)

// simpleFilter wraps a function as a filter.
type simpleFilter struct {
	name     string
	priority int
	fn       FilterFunc
}

// NewSimpleFilter creates a filter from a function.
func NewSimpleFilter(name string, priority int, fn FilterFunc) MessageFilter {
	return &simpleFilter{
		name:     name,
		priority: priority,
		fn:       fn,
	}
}

func (sf *simpleFilter) Name() string {
	return sf.name
}

func (sf *simpleFilter) Filter(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
	return sf.fn(ctx, msg, recipient)
}

func (sf *simpleFilter) Priority() int {
	return sf.priority
}
