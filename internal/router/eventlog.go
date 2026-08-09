package router

import "context"

// LoggedEvent is one recorded event, as it will be replayed.
type LoggedEvent struct {
	ID    string
	Event string
	Data  []byte
}

// EventLog stores recent events so a reconnecting client can be handed the ones
// it missed instead of resynchronising from scratch.
type EventLog interface {
	// Append records an event on a channel and returns the ID assigned to it.
	Append(ctx context.Context, channel, event string, data []byte) (string, error)

	// Since returns the events recorded after id, in order.
	//
	// The bool reports whether id was still resolvable. False means the gap
	// cannot be filled and the caller must fall back to a full resync; events is
	// empty in that case and must NOT be read as "nothing was missed".
	//
	// Returning the two separately is the point of the signature. Folding them
	// into an empty slice would make the case that silently serves stale data
	// indistinguishable from the case that is safe.
	Since(ctx context.Context, channel, id string) ([]LoggedEvent, bool, error)
}
