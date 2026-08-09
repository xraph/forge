package streaming

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The resume cursor.
//
// SSE gives the client exactly one string to resume from: it echoes the last
// `id:` it saw back as the Last-Event-ID header. One stream can carry many
// rooms, and each room has its own sequence, so a single scalar cannot say
// where the client got to — it can only describe one room, and every other room
// on that connection would silently replay from the wrong place or not at all.
//
// So the cursor is a vector: room -> last sequence delivered. It is bounded by
// rooms-per-connection (MaxRoomsPerUser, 50 by default), which keeps it well
// inside any reasonable header limit.
//
// It must also survive being a header value AND an SSE field value. A newline
// in an `id:` terminates the field and lets the remainder parse as further SSE
// fields — the same event-forgery hole validSSEFieldValue guards in the router.
// Encoding it opaquely is what makes that unrepresentable rather than merely
// unlikely.

func TestReplayCursor_RoundTrips(t *testing.T) {
	want := replayCursor{"room-1": 12, "room-2": 5}

	got, err := decodeReplayCursor(encodeReplayCursor(want))
	if err != nil {
		t.Fatalf("decodeReplayCursor: unexpected error %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("cursor has %d rooms, want %d", len(got), len(want))
	}

	for room, seq := range want {
		if got[room] != seq {
			t.Errorf("room %s = %d, want %d", room, got[room], seq)
		}
	}
}

func TestReplayCursor_EmptyEncodesToEmptyString(t *testing.T) {
	// No rooms means no cursor. An empty string is what a client that has never
	// received an event sends, so the two must agree.
	if got := encodeReplayCursor(replayCursor{}); got != "" {
		t.Errorf("encodeReplayCursor(empty) = %q, want empty string", got)
	}
}

func TestReplayCursor_DecodeEmptyIsEmptyNotError(t *testing.T) {
	got, err := decodeReplayCursor("")
	if err != nil {
		t.Fatalf("decodeReplayCursor(\"\"): unexpected error %v", err)
	}

	if len(got) != 0 {
		t.Errorf("cursor = %v, want empty", got)
	}
}

func TestReplayCursor_EncodingIsSSEFieldSafe(t *testing.T) {
	// Room IDs are caller-controlled. If one containing a newline could reach an
	// SSE `id:` field through the cursor, a client could be fed forged events.
	hostile := replayCursor{
		"room\nid: injected\ndata: forged\n\n": 1,
		"room\rwith-cr":                        2,
		"room:with:colons":                     3,
	}

	encoded := encodeReplayCursor(hostile)

	if strings.ContainsAny(encoded, "\r\n") {
		t.Fatalf("encoded cursor contains a newline: %q", encoded)
	}

	got, err := decodeReplayCursor(encoded)
	if err != nil {
		t.Fatalf("decodeReplayCursor: unexpected error %v", err)
	}

	for room, seq := range hostile {
		if got[room] != seq {
			t.Errorf("room %q = %d, want %d", room, got[room], seq)
		}
	}
}

func TestReplayCursor_DecodeRejectsMalformedInput(t *testing.T) {
	// Last-Event-ID is client-supplied and arrives on an unauthenticated path.
	// Garbage must be an error the caller can fall back from, never a panic and
	// never a partially-populated cursor that would replay from a wrong offset.
	cases := map[string]string{
		"not base64":              "not-base64!!!",
		"wrong base64 alphabet":   "//++",
		"padded, alphabet is raw": "eyJicm9rZW4iOg==",

		// The realistic attack: a well-formed token whose payload is garbage.
		// Without these, every case above fails at the base64 step and the JSON
		// decode is never exercised at all.
		"valid base64, not JSON":       base64.RawURLEncoding.EncodeToString([]byte("not json")),
		"valid base64, JSON but array": base64.RawURLEncoding.EncodeToString([]byte(`["room-1", 4]`)),
		"valid base64, seq not a number": base64.RawURLEncoding.EncodeToString(
			[]byte(`{"room-1": "not-a-number"}`)),
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := decodeReplayCursor(in)
			if err == nil {
				t.Errorf("decodeReplayCursor(%q) = nil error, want an error", in)
			}

			if got != nil {
				t.Errorf("cursor = %v, want nil — a partial cursor resumes from an offset the client never reached", got)
			}
		})
	}
}
