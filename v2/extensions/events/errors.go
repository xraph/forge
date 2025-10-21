package events

import (
	"errors"
	"fmt"
)

var (
	// ErrEventNotFound is returned when an event is not found
	ErrEventNotFound = errors.New("event not found")

	// ErrSnapshotNotFound is returned when a snapshot is not found
	ErrSnapshotNotFound = errors.New("snapshot not found")

	// ErrInvalidEvent is returned when an event is invalid
	ErrInvalidEvent = errors.New("invalid event")

	// ErrInvalidSnapshot is returned when a snapshot is invalid
	ErrInvalidSnapshot = errors.New("invalid snapshot")

	// ErrBrokerNotFound is returned when a broker is not found
	ErrBrokerNotFound = errors.New("broker not found")

	// ErrBrokerAlreadyRegistered is returned when a broker is already registered
	ErrBrokerAlreadyRegistered = errors.New("broker already registered")

	// ErrHandlerNotFound is returned when a handler is not found
	ErrHandlerNotFound = errors.New("handler not found")

	// ErrEventBusNotStarted is returned when the event bus is not started
	ErrEventBusNotStarted = errors.New("event bus not started")

	// ErrEventStoreNotStarted is returned when the event store is not started
	ErrEventStoreNotStarted = errors.New("event store not started")
)

// WrapError wraps an error with additional context
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
