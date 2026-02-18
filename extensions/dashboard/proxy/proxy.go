package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// FragmentProxy fetches HTML fragments from remote contributors with caching.
// It provides the primary integration between the dashboard shell and remote services.
type FragmentProxy struct {
	registry *contributor.ContributorRegistry
	cache    *FragmentCache
	timeout  time.Duration
	logger   forge.Logger
}

// NewFragmentProxy creates a new fragment proxy.
func NewFragmentProxy(
	registry *contributor.ContributorRegistry,
	cacheMaxSize int,
	cacheTTL time.Duration,
	timeout time.Duration,
	logger forge.Logger,
) *FragmentProxy {
	return &FragmentProxy{
		registry: registry,
		cache:    NewFragmentCache(cacheMaxSize, cacheTTL),
		timeout:  timeout,
		logger:   logger,
	}
}

// FetchPage fetches a page fragment from a remote contributor, using cache when available.
func (p *FragmentProxy) FetchPage(ctx context.Context, name, route string) ([]byte, error) {
	cacheKey := CacheKey(name, "page", route)

	// Check cache first
	if entry := p.cache.Get(cacheKey); entry != nil {
		p.logger.Debug("proxy cache hit",
			forge.F("contributor", name),
			forge.F("route", route),
			forge.F("age", entry.Age().String()),
		)
		return entry.Data, nil
	}

	// Fetch from remote
	rc, ok := p.registry.FindRemoteContributor(name)
	if !ok {
		return nil, fmt.Errorf("remote contributor %q not found", name)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	data, err := rc.FetchPage(fetchCtx, route)
	if err != nil {
		// Try stale cache on failure
		if stale := p.cache.GetStale(cacheKey); stale != nil {
			p.logger.Warn("serving stale cache after fetch failure",
				forge.F("contributor", name),
				forge.F("route", route),
				forge.F("stale_age", stale.Age().String()),
				forge.F("error", err.Error()),
			)
			return stale.Data, nil
		}

		return nil, fmt.Errorf("failed to fetch page from %q: %w", name, err)
	}

	// Cache the result
	p.cache.Set(cacheKey, data)

	return data, nil
}

// FetchWidget fetches a widget fragment from a remote contributor, using cache when available.
func (p *FragmentProxy) FetchWidget(ctx context.Context, name, widgetID string) ([]byte, error) {
	cacheKey := CacheKey(name, "widget", widgetID)

	// Check cache first
	if entry := p.cache.Get(cacheKey); entry != nil {
		return entry.Data, nil
	}

	// Fetch from remote
	rc, ok := p.registry.FindRemoteContributor(name)
	if !ok {
		return nil, fmt.Errorf("remote contributor %q not found", name)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	data, err := rc.FetchWidget(fetchCtx, widgetID)
	if err != nil {
		// Try stale cache
		if stale := p.cache.GetStale(cacheKey); stale != nil {
			return stale.Data, nil
		}

		return nil, fmt.Errorf("failed to fetch widget from %q: %w", name, err)
	}

	p.cache.Set(cacheKey, data)

	return data, nil
}

// InvalidateContributor removes all cached fragments for a contributor.
func (p *FragmentProxy) InvalidateContributor(name string) {
	p.cache.Clear() // Simple for now — clear all. Could be more targeted.
	p.logger.Debug("invalidated cache for contributor", forge.F("contributor", name))
}

// Cache returns the underlying fragment cache (for metrics/status reporting).
func (p *FragmentProxy) Cache() *FragmentCache {
	return p.cache
}
