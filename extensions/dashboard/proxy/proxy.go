package proxy

import (
	"context"
	"fmt"
	"io"
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

// FetchPage fetches a page fragment from a remote contributor, using cache
// when available. rawQuery is the request URL's query string (without the
// leading "?"); it is forwarded to the remote and is part of the cache key so
// pages with different query parameters (e.g. /detail?id=A vs ?id=B) don't
// collide.
func (p *FragmentProxy) FetchPage(ctx context.Context, name, route, rawQuery string) ([]byte, error) {
	cacheKey := CacheKey(name, "page", routeWithQuery(route, rawQuery))

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

	data, err := rc.FetchPage(fetchCtx, route, rawQuery)
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

// routeWithQuery joins a route and query string for cache-key purposes.
func routeWithQuery(route, rawQuery string) string {
	if rawQuery == "" {
		return route
	}

	return route + "?" + rawQuery
}

// PostPage proxies a form submission (POST) to a remote contributor's page
// endpoint. POSTs bypass the fragment cache — they're side-effecting and
// the response is bound to the request body, not the URL alone.
//
// rawQuery carries the consumer-side bp/pb plus any inbound query the
// request URL had. body and contentType come straight from the host's
// inbound request (typically application/x-www-form-urlencoded or
// multipart/form-data).
func (p *FragmentProxy) PostPage(ctx context.Context, name, route, rawQuery string, body io.Reader, contentType string) ([]byte, error) {
	rc, ok := p.registry.FindRemoteContributor(name)
	if !ok {
		return nil, fmt.Errorf("remote contributor %q not found", name)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	data, err := rc.PostPage(fetchCtx, route, rawQuery, body, contentType)
	if err != nil {
		return nil, fmt.Errorf("failed to post to %q: %w", name, err)
	}

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
