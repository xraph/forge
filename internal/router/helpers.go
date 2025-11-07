package router

import (
	"fmt"

	"github.com/xraph/forge/internal/shared"
)

// ManagerKey is the DI key for the router service
const ManagerKey = shared.RouterKey

// GetRouter resolves the router from the container
// This is a convenience function for resolving the router service
func GetRouter(container shared.Container) (Router, error) {
	r, err := container.Resolve(ManagerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve router: %w", err)
	}
	router, ok := r.(Router)
	if !ok {
		return nil, fmt.Errorf("resolved instance is not Router, got %T", r)
	}
	return router, nil
}
