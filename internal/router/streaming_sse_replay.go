package router

import (
	"encoding/json"
	"errors"
	"sync"
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

// ErrEventIDAssignedByLog is returned when a handler supplies its own event ID
// on a resumable route.
//
// The log assigns positions, and the wire must carry exactly what the log
// recorded — a caller-supplied ID would either be overwritten (silently
// discarding what the handler asked for) or emitted as-is (leaving the log and
// the wire disagreeing about where a client is, which is what makes a later
// resume replay from the wrong point). Refusing is the only option that cannot
// lie to somebody.
// Exported because it crosses a package boundary and has to be branched on. A
// caller that supplies IDs — the streaming extension emits a replay cursor on
// every sequenced room message — needs to tell this refusal apart from a
// transport failure, so it can fall back to sending without an ID rather than
// dropping the message. Unexported, the only options were to drop the frame or
// to match on error text.
var ErrEventIDAssignedByLog = errors.New("router: event IDs are assigned by the event log on a resumable stream")

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

	// mu serializes append-then-send across concurrent senders. The underlying
	// sseStream already tolerates concurrent writers, so a handler fanning out
	// across goroutines is a supported shape here too — but the log's append
	// order and the wire's send order must agree, or a client that reconnects
	// mid-race can see an ID the wire hasn't caught up to yet (or vice versa),
	// and a resume computed from that ID replays from the wrong point.
	mu sync.Mutex
}

// Send records the event, then emits it with the recorded ID.
//
// Append and SendWithID run under one lock: splitting them would let two
// goroutines interleave as append(1), append(2), send(2), send(1), so a client
// dropping in between observes id 2 on the wire while the log still reports id
// 1 as the newest thing it can prove was delivered. The write beneath
// SendWithID is deadline-bounded (sseWriteTimeout), so holding the lock across
// it cannot stall the pair indefinitely.
func (s *loggedStream) Send(event string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

// SendWithID refuses: see ErrEventIDAssignedByLog.
func (s *loggedStream) SendWithID(_, _ string, _ []byte) error {
	return ErrEventIDAssignedByLog
}

// SendJSONWithID refuses: see ErrEventIDAssignedByLog.
func (s *loggedStream) SendJSONWithID(_, _ string, _ any) error {
	return ErrEventIDAssignedByLog
}

// replayInto brings a reconnecting client up to date, or tells it that it
// cannot be.
//
// authoritative says whether the log is written by the application's producer
// or only by this route's connections; see WithProducerEventLog for what rides
// on the distinction.
//
// Control events go to the underlying stream rather than a loggedStream: they
// describe the log and must not become entries in it, or every reconnect would
// append a marker that the next reconnect then replays.
func replayInto(stream Stream, log EventLog, channel string, authoritative bool) error {
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

	// A connection-written log that has nothing after the client's position is
	// reporting two different situations under one answer: the client missed
	// nothing, or nothing was recording while it was away. On such a log the
	// second is the ordinary case — a client that was the only listener is by
	// construction at the head when it comes back — and guessing the first
	// leaves it serving stale data with no further signal that anything is
	// wrong. A gap costs one refetch; only that mistake is recoverable, so it
	// is the one to make.
	//
	// Events present settle the question in either mode: something wrote them
	// while this client was away, so the log was demonstrably recording.
	if !authoritative && len(events) == 0 {
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
// the client's gap first.
//
// The returned Stream is always usable, error or not, and the caller is meant
// to use it either way — the error says the *replay* failed, not that the
// connection did. A failed replay costs this client its resumability; running
// the handler anyway is what keeps it from costing the client the stream. The
// wrapping still happens on that path so this connection's own events reach the
// log and a later reconnect can resume even though this one could not.
func resumable(stream Stream, log EventLog, channel string, authoritative bool) (Stream, error) {
	err := replayInto(stream, log, channel, authoritative)

	return &loggedStream{Stream: stream, log: log, channel: channel}, err
}
