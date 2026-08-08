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
//
// The rule has a mirror image, and this package broke it five times before
// NewLifecycleMessage existed: a frame an application is *not* expected to bind
// must not set Event. A ping or a kick with its name in Event is a name no
// manifest binds, and being non-empty it never reaches the client's
// reserved-kind filter, so it is reported rather than dropped. Those frames put
// their name in Metadata instead, under LifecycleMetadataKey.

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
//
// An event whose name collides with a reserved transport kind is accepted and
// is a mistake: the client will look for a binding named "presence" and find
// none. It is not rejected here because the failure is visible -- an event name
// always takes the client's event branch, so the frame is reported rather than
// dropped -- and because a constructor that can fail is a worse trade than a
// caller running IsTransportKind when the name is not a literal.
func NewEventMessage(event string, data any) *Message {
	return &Message{
		Type:      MessageTypeMessage,
		Event:     event,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// LifecycleMetadataKey is the Metadata key under which NewLifecycleMessage
// records the lifecycle name.
//
// Exported so neither the client nor any other consumer has to match a bare
// string literal against an envelope this package owns. A literal spelled in
// three places is a rename away from a reader that silently finds nothing, and
// a lifecycle frame whose name cannot be read is indistinguishable from one
// that never carried a name at all.
const LifecycleMetadataKey = "lifecycle"

// NewLifecycleMessage builds a transport frame announcing a connection
// lifecycle moment -- a ping, a kick, a shutdown -- named in Metadata rather
// than in Event.
//
// Event is left empty, and that is the entire point of this constructor. Event
// is the binding key: it is what the generated client's manifest is keyed on,
// one row per AsyncAPI domain message, and what forgeStreamingDecoder reads
// first. A lifecycle name sitting in Event is therefore a name no manifest
// binds, and worse, it is non-empty, so the decoder takes its event branch and
// never reaches the reserved-kind filter that exists to drop transport frames
// quietly. The frame is passed through to the runtime, no slot matches it, and
// it surfaces through onUnknown as an unknown message on a channel that is
// working exactly as designed. For a heartbeat firing on a ticker for every
// connection that is a permanent recurring false signal -- into an
// application's own onUnknown, which is typically wired to metrics or Sentry.
//
// The alternatives were both worse. Leaving the name in Event costs every
// heartbeat a spurious unknown-message report and cannot be fixed on the
// decoder side, because a lifecycle name and a genuine system-kind domain
// event (message.deleted, message.edited in manager.go) have the same shape
// on the wire and no decoder-side rule can separate them. Dropping the name
// entirely would make the frames clean but would throw away information a
// client may legitimately want -- a client that wants to show "you were
// removed by a moderator" needs to know a kicked frame from an idle_cleanup
// one. Metadata carries the name where a curious client can still read it,
// while the frame stays a pure transport frame by shape: it sets only Type,
// so the client drops it silently, which is the correct outcome for a frame
// no manifest was ever going to bind.
//
// This is a deliberate wire change. A consumer previously reading
// event: "kicked" must now read metadata.lifecycle.
//
// ID, UserID, Data and the routing fields are left to the caller for the same
// reason NewEventMessage leaves them: they are the producer's, and identity in
// particular carries deduplication and history semantics a wire-contract
// helper has no business inventing. Timestamp is set because a frame without
// one marshals as the zero time, which is a wrong answer rather than an absent
// one.
func NewLifecycleMessage(kind, lifecycle string) *Message {
	return &Message{
		Type:      kind,
		Metadata:  map[string]any{LifecycleMetadataKey: lifecycle},
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
