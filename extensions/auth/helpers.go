package auth

import (
	"fmt"

	"github.com/xraph/forge"
)

// Helper functions for convenient auth registry access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// GetRegistry retrieves the auth Registry from the container.
// Returns error if not found or type assertion fails.
func GetRegistry(c forge.Container) (Registry, error) {
	// Try type-based resolution first
	if registry, err := forge.InjectType[Registry](c); err == nil && registry != nil {
		return registry, nil
	}

	// Fallback to string-based resolution
	return forge.Resolve[Registry](c, RegistryKey)
}

// MustGetRegistry retrieves the auth Registry from the container.
// Panics if not found or type assertion fails.
func MustGetRegistry(c forge.Container) Registry {
	registry, err := GetRegistry(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get auth registry: %v", err))
	}
	return registry
}

// GetRegistryFromApp retrieves the auth Registry from the app.
// Returns error if not found or type assertion fails.
func GetRegistryFromApp(app forge.App) (Registry, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetRegistry(app.Container())
}

// MustGetRegistryFromApp retrieves the auth Registry from the app.
// Panics if not found or type assertion fails.
func MustGetRegistryFromApp(app forge.App) Registry {
	if app == nil {
		panic("app is nil")
	}
	return MustGetRegistry(app.Container())
}
