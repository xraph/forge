package queue

import (
	"fmt"

	"github.com/xraph/forge"
)

// Helper functions for convenient queue service access from DI container.
// Provides lightweight wrappers around Forge's DI system to eliminate verbose boilerplate.

// GetQueue retrieves the Queue service from the container.
// Returns error if not found or type assertion fails.
func GetQueue(c forge.Container) (Queue, error) {
	// Try type-based resolution first
	if queue, err := forge.InjectType[*QueueService](c); err == nil && queue != nil {
		return queue, nil
	}

	// Fallback to string-based resolution
	return forge.Inject[Queue](c)
}

// MustGetQueue retrieves the Queue service from the container.
// Panics if not found or type assertion fails.
func MustGetQueue(c forge.Container) Queue {
	queue, err := GetQueue(c)
	if err != nil {
		panic(fmt.Sprintf("failed to get queue service: %v", err))
	}
	return queue
}

// GetQueueFromApp retrieves the Queue service from the app.
// Returns error if not found or type assertion fails.
func GetQueueFromApp(app forge.App) (Queue, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetQueue(app.Container())
}

// MustGetQueueFromApp retrieves the Queue service from the app.
// Panics if not found or type assertion fails.
func MustGetQueueFromApp(app forge.App) Queue {
	if app == nil {
		panic("app is nil")
	}
	return MustGetQueue(app.Container())
}
