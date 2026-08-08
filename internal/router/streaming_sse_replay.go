package router

import (
	"encoding/json"
)

// Control event names, reserved for the replay wiring.
//
// Namespaced so an application event can never collide with one: a forged
// "resumed" marker would convince a client that a gap was filled when it was
// not, which is the one failure this whole mechanism exists to prevent.
const (
	EventResumed = "forge.resumed"
	EventGap     = "forge.gap"
)

// ResumedPayload closes a replay: the position resumed from and how many events
// were delivered.
type ResumedPayload struct {
	From  string `json:"from"`
	Count int    `json:"count"`
}

// GapPayload tells the client the gap could not be filled.
//
// One reason value, not several. The log reports resumability as a bool, so the
// wiring cannot distinguish an expired position from a stale epoch without
// widening that interface, and naming a specific cause it has not established
// would be a guess dressed as a diagnosis.
type GapPayload struct {
	Reason string `json:"reason"`
}

// loggedStream records every event before sending it, and sends it under the ID
// the log assigned.
//
// Appending and sending in one place is what keeps the two consistent. If the
// handler sent directly and the log were written elsewhere, the wire and the log
// could disagree about a position, and a resume would then replay from the wrong
// point — silently, since neither side can detect the disagreement.
type loggedStream struct {
	Stream

	log     EventLog
	channel string
}

// Send records the event, then emits it with the recorded ID.
func (s *loggedStream) Send(event string, data []byte) error {
	id, err := s.log.Append(s.Context(), s.channel, event, data)
	if err != nil {
		return err
	}

	return s.Stream.SendWithID(id, event, data)
}

// SendJSON marshals, then follows Send so the logged bytes are the sent bytes.
func (s *loggedStream) SendJSON(event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return s.Send(event, data)
}

// replayInto brings a reconnecting client up to date, or tells it that it
// cannot be.
//
// Control events go to the underlying stream rather than a loggedStream: they
// describe the log and must not become entries in it, or every reconnect would
// append a marker that the next reconnect then replays.
func replayInto(stream Stream, log EventLog, channel string) error {
	last := stream.LastEventID()
	if last == "" {
		// A first connection, not a resumption. Nothing was missed and there is
		// nothing to report.
		return nil
	}

	events, resumable, err := log.Since(stream.Context(), channel, last)
	if err != nil {
		return err
	}

	if !resumable {
		return stream.SendJSON(EventGap, GapPayload{Reason: "unresumable"})
	}

	for _, event := range events {
		if err := stream.SendWithID(event.ID, event.Event, event.Data); err != nil {
			return err
		}
	}

	// Sent last, so receiving it means both "the gap was filled" and "the fill is
	// complete". A marker sent first could not carry the second claim.
	return stream.SendJSON(EventResumed, ResumedPayload{From: last, Count: len(events)})
}

// resumable wraps a stream for a route configured with an event log, replaying
// the client's gap first. Returns the stream the handler should use.
func resumable(stream Stream, log EventLog, channel string) (Stream, error) {
	if err := replayInto(stream, log, channel); err != nil {
		return nil, err
	}

	return &loggedStream{Stream: stream, log: log, channel: channel}, nil
}
