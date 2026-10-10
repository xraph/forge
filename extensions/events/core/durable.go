package core

import (
	"errors"
	"fmt"
)

// ErrDurableDeliveryUnavailable identifies a route without durable delivery.
var ErrDurableDeliveryUnavailable = errors.New("durable delivery is unavailable")

// RequireDurableBroker checks a route before you wire handlers that require
// committed-effect acknowledgement and recovery of pending deliveries.
func RequireDurableBroker(broker MessageBroker) (DurableMessageBroker, error) {
	durable, ok := broker.(DurableMessageBroker)
	if !ok {
		return nil, ErrDurableDeliveryUnavailable
	}

	capabilities, err := durable.DurableCapabilities()
	if err != nil {
		return nil, fmt.Errorf("durable route unavailable: %w", err)
	}

	if capabilities.Ordering == "" || capabilities.MaxInFlight < 1 ||
		!capabilities.AcknowledgeAfterHandler || !capabilities.PendingRecovery ||
		capabilities.ConsumerGroup == "" || capabilities.ReplicaID == "" {
		return nil, fmt.Errorf("%w: incomplete delivery capabilities", ErrDurableDeliveryUnavailable)
	}

	return durable, nil
}
