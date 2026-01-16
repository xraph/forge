package cache

import (
	"fmt"

	"github.com/xraph/forge"
)

// Helper functions for convenient cache service access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// GetCache retrieves the Cache service from the container.
// Returns error if not found or type assertion fails.
func GetCache(c forge.Container) (Cache, error) {
	// Try type-based resolution first
	if cache, err := forge.InjectType[*CacheService](c); err == nil && cache != nil {
		return cache, nil
	}

	// Fallback to string-based resolution
	return forge.Resolve[Cache](c, ServiceKey)
}

// MustGetCache retrieves the Cache service from the container.
// Panics if not found or type assertion fails.
func MustGetCache(c forge.Container) Cache {
	cache, err := GetCache(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get cache service: %v", err))
	}
	return cache
}

// GetCacheFromApp retrieves the Cache service from the app.
// Returns error if not found or type assertion fails.
func GetCacheFromApp(app forge.App) (Cache, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetCache(app.Container())
}

// MustGetCacheFromApp retrieves the Cache service from the app.
// Panics if not found or type assertion fails.
func MustGetCacheFromApp(app forge.App) Cache {
	if app == nil {
		panic("app is nil")
	}
	return MustGetCache(app.Container())
}
