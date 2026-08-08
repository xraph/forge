package streaming

import (
	"slices"
	"time"
)

// The wire contract between this extension and the generated TypeScript client.
//
// A Message carries two names, and which one is which is the whole of the
// contract. Type is the transport kind -- one of the seven MessageType*
// constants, and load-bearing all over this package: handleMessage switches on
// it, the metadata filter allow-lists on it, and the schema validator keys its
// schemas by it. Event is the domain name, the one an application means when it
// says order.created, and the one the client's manifest binds. They were
// conflated once, in the client, and the cost was total: the runtime resolved
// the frame name as Type, every frame on every channel decoded as "message",
// nothing in any generated manifest is keyed on "message", and each one was
// dropped as an unknown message on a channel that looked healthy.
//
// The fix is on the client -- packages/client-core/src/streaming.ts, whose
// forgeStreamingDecoder reads Event first and treats Type as a fallback --
// because Type could not be repurposed here without repointing four subsystems
// at names they were never written to understand. Nothing about this envelope
// changed, which is the point: this file is what keeps it from drifting away
// underneath a decoder that now depends on it. See frame_test.go, which asserts
// the marshalled bytes and names that file; that file names this one back.
//
// The one rule a producer has to follow is that a frame an application is
// expected to bind must set Event. A frame that sets only Type is a transport
// frame by definition, and the client drops it silently rather than reporting
// it -- correct for presence and typing, and indistinguishable from them if a
// domain event forgets its name. NewEventMessage below is the way not to forget.

// NewEventMessage builds a frame carrying a domain event, named so the
// generated client can bind it.
//
// Type is MessageTypeMessage and Event is the domain name -- never the reverse,
// which is the mistake this constructor exists to make unavailable. Callers
// wanting a different transport kind may set Type afterwards; the client honours
// Event regardless of the kind it rides on, which is what makes the existing
// system-kind events in manager.go (message.deleted, message.edited) bindable
// without being reclassified.
//
// ID, UserID and the routing fields are deliberately left alone. They are the
// producer's, and identity in particular has semantics -- deduplication, history
// -- that a wire-contract helper has no business inventing. Timestamp is set
// because a frame without one marshals as the zero time, which is a wrong answer
// rather than an absent one.
func NewEventMessage(event string, data any) *Message {
	return &Message{
		Type:      MessageTypeMessage,
		Event:     event,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// TransportKinds returns the reserved values of Message.Type, in the order they
// are declared.
//
// Mirrored by TRANSPORT_KINDS in packages/client-core/src/streaming.ts, which
// drops a frame whose name resolves to one of these through the Type fallback.
// The mirror is asserted in frame_test.go rather than trusted: an eighth kind
// added to this package and not to that set would arrive at the client as a
// frame name no binding can claim, and be reported as an unknown message on
// every channel for as long as it existed.
//
// A fresh slice per call, so a caller ranging over it cannot reorder the set
// every other consumer reads.
func TransportKinds() []string {
	return []string{
		MessageTypeMessage,
		MessageTypePresence,
		MessageTypeTyping,
		MessageTypeSystem,
		MessageTypeJoin,
		MessageTypeLeave,
		MessageTypeError,
	}
}

// IsTransportKind reports whether kind is one of the reserved transport kinds.
//
// Useful to a producer choosing an Event name: a domain event that collides with
// a reserved kind is unbindable on the client, because a frame naming it cannot
// be told apart from the transport frame of the same name.
func IsTransportKind(kind string) bool {
	return slices.Contains(TransportKinds(), kind)
}
