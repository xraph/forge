package router

import (
	"strconv"
	"strings"
)

// eventID is a position in an event log, as it appears on the wire.
//
// The epoch exists because a sequence number alone is unsafe across a restart.
// A fresh process restarts its counters, so a client resuming from seq 41 would
// be handed events 42... that are entirely different events reusing the same
// numbers. Comparing epochs turns that silent mis-replay into an honest refusal
// to resume.
type eventID struct {
	Epoch string
	Seq   uint64
}

// formatEventID renders a position for the wire. Both halves are text with no
// newline, so the result passes validSSEFieldValue without a special case.
func formatEventID(epoch string, seq uint64) string {
	return epoch + "-" + strconv.FormatUint(seq, 10)
}

// parseEventID parses a wire position. The bool reports whether s was
// well-formed; a false means the position cannot be honoured and the caller
// must treat the gap as unfillable.
//
// Split on the LAST separator: epochs are UUIDs and contain dashes of their
// own, so splitting on the first would read "3f2504e0" as the whole epoch and
// fail to parse the remainder as a number.
func parseEventID(s string) (eventID, bool) {
	i := strings.LastIndexByte(s, '-')
	if i <= 0 || i == len(s)-1 {
		return eventID{}, false
	}

	// Reject consecutive dashes, which indicate malformed input like "epoch--1".
	if i > 0 && s[i-1] == '-' {
		return eventID{}, false
	}

	seq, err := strconv.ParseUint(s[i+1:], 10, 64)
	if err != nil {
		return eventID{}, false
	}

	return eventID{Epoch: s[:i], Seq: seq}, true
}
