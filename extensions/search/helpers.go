package search

import (
	"fmt"

	"github.com/xraph/forge"
)

// Helper functions for convenient search service access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// GetSearch retrieves the Search service from the container.
// Returns error if not found or type assertion fails.
func GetSearch(c forge.Container) (Search, error) {
	// Try type-based resolution first
	if search, err := forge.InjectType[*SearchService](c); err == nil && search != nil {
		return search, nil
	}

	// Fallback to string-based resolution
	return forge.Resolve[Search](c, ServiceKey)
}

// MustGetSearch retrieves the Search service from the container.
// Panics if not found or type assertion fails.
func MustGetSearch(c forge.Container) Search {
	search, err := GetSearch(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get search service: %v", err))
	}
	return search
}

// GetSearchFromApp retrieves the Search service from the app.
// Returns error if not found or type assertion fails.
func GetSearchFromApp(app forge.App) (Search, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetSearch(app.Container())
}

// MustGetSearchFromApp retrieves the Search service from the app.
// Panics if not found or type assertion fails.
func MustGetSearchFromApp(app forge.App) Search {
	if app == nil {
		panic("app is nil")
	}
	return MustGetSearch(app.Container())
}
