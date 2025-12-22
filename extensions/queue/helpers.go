package queue

import (
	"fmt"

	"github.com/xraph/forge"
)

// Get retrieves the queue service from the container.
// Returns error if queue is not registered.
func Get(container forge.Container) (Queue, error) {
	return forge.Resolve[Queue](container, "queue")
}

// MustGet retrieves the queue service from the container.
// Panics if queue is not registered.
func MustGet(container forge.Container) Queue {
	q, err := Get(container)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve queue: %v", err))
	}
	return q
}

// GetFromApp is a convenience helper to get queue from an App.
func GetFromApp(app forge.App) (Queue, error) {
	return Get(app.Container())
}

// MustGetFromApp is a convenience helper to get queue from an App.
// Panics if queue is not registered.
func MustGetFromApp(app forge.App) Queue {
	return MustGet(app.Container())
}
