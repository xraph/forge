package events

import (
	"github.com/google/uuid"
	"github.com/xraph/forge/extensions/events/core"
)

const anonymousSubscriptionPrefix = "forge-internal-anonymous:"

// registeredAnonymousHandler retains the public name and the transport identity
// used for this registration, including when the bus restores it after a stop.
type registeredAnonymousHandler struct {
	core.EventHandler

	transport core.EventHandler
}

type anonymousTransportHandler struct {
	core.EventHandler

	name string
}

func (h *anonymousTransportHandler) Name() string        { return h.name }
func (h *anonymousTransportHandler) LogicalName() string { return h.EventHandler.Name() }

func newAnonymousRegistration(handler core.EventHandler) core.EventHandler {
	return &registeredAnonymousHandler{
		EventHandler: handler,
		transport: &anonymousTransportHandler{
			EventHandler: handler,
			name:         anonymousSubscriptionPrefix + uuid.NewString(),
		},
	}
}

func transportHandler(handler core.EventHandler) core.EventHandler {
	if registered, ok := handler.(*registeredAnonymousHandler); ok {
		return registered.transport
	}

	return handler
}
