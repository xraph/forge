package streaming

import (
	"fmt"

	"github.com/xraph/forge"
)

// Helper functions for convenient streaming manager access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// GetManager retrieves the streaming Manager from the container.
// Returns error if not found or type assertion fails.
func GetManager(c forge.Container) (Manager, error) {
	// Try type-based resolution first
	if mgr, err := forge.InjectType[Manager](c); err == nil && mgr != nil {
		return mgr, nil
	}

	// Fallback to string-based resolution
	return forge.Resolve[Manager](c, ManagerKey)
}

// MustGetManager retrieves the streaming Manager from the container.
// Panics if not found or type assertion fails.
func MustGetManager(c forge.Container) Manager {
	mgr, err := GetManager(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get streaming manager: %v", err))
	}
	return mgr
}

// GetManagerFromApp retrieves the streaming Manager from the app.
// Returns error if not found or type assertion fails.
func GetManagerFromApp(app forge.App) (Manager, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetManager(app.Container())
}

// MustGetManagerFromApp retrieves the streaming Manager from the app.
// Panics if not found or type assertion fails.
func MustGetManagerFromApp(app forge.App) Manager {
	if app == nil {
		panic("app is nil")
	}
	return MustGetManager(app.Container())
}
