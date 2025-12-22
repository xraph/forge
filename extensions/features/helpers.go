package features

import (
	"fmt"

	"github.com/xraph/forge"
)

// Get retrieves the features service from the container.
// Returns error if features service is not registered.
func Get(container forge.Container) (*Service, error) {
	return forge.Resolve[*Service](container, "features.Service")
}

// MustGet retrieves the features service from the container.
// Panics if features service is not registered.
// Use this during application startup where failure should halt execution.
func MustGet(container forge.Container) *Service {
	service, err := Get(container)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve features service: %v", err))
	}
	return service
}

// GetFromApp is a convenience helper to get features service from an App.
func GetFromApp(app forge.App) (*Service, error) {
	return Get(app.Container())
}

// MustGetFromApp is a convenience helper to get features service from an App.
// Panics if features service is not registered.
// Use this during application startup where failure should halt execution.
func MustGetFromApp(app forge.App) *Service {
	return MustGet(app.Container())
}
