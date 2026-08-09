package streaming

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/xraph/forge"
)

// replayCursor records how far a client has consumed each room, as
// room ID -> last delivered sequence.
//
// See replay_test.go for why this is a vector rather than a scalar: SSE gives a
// resuming client exactly one string to identify its position, and one stream
// can carry many rooms.
type replayCursor map[string]int64

// errMalformedCursor is returned for a Last-Event-ID this server did not issue.
var errMalformedCursor = errors.New("malformed replay cursor")

// encodeReplayCursor renders a cursor as an opaque, single-line token.
//
// base64url of JSON: the encoding is what makes the result safe to place in an
// SSE `id:` field and an HTTP header regardless of what characters a room ID
// contains. Room IDs are caller-controlled, and a newline reaching an `id:`
// field would let an attacker append arbitrary events to the stream.
// base64url rather than standard base64 so the token is also safe in a URL,
// which is where a client that cannot set headers will have to put it.
func encodeReplayCursor(c replayCursor) string {
	if len(c) == 0 {
		return ""
	}

	data, err := json.Marshal(map[string]int64(c))
	if err != nil {
		// A map[string]int64 cannot fail to marshal. Returning empty rather
		// than panicking degrades to "no cursor", which costs a full resync
		// and never breaks the stream.
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeReplayCursor parses a token produced by encodeReplayCursor.
//
// The input arrives from the client, so every failure mode is reachable by
// anyone. An error means "resync from scratch"; it must never yield a partial
// cursor, which would resume from an offset the client never actually reached
// and silently skip messages.
func decodeReplayCursor(s string) (replayCursor, error) {
	if s == "" {
		return replayCursor{}, nil
	}

	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errMalformedCursor
	}

	var raw map[string]int64
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errMalformedCursor
	}

	return replayCursor(raw), nil
}

// defaultReplayLimitPerRoom bounds how much backlog one room contributes to a
// single reconnect. A client returning after a long outage gets the oldest part
// of its gap and a fresh cursor; it can come back for the rest.
const defaultReplayLimitPerRoom = 200

// Replay sends a reconnecting connection the messages it missed.
//
// The cursor comes from the client (an SSE Last-Event-ID, or an explicit
// parameter), so it is untrusted in two distinct ways and both are handled
// here. It may be malformed, which is an error rather than a silent empty
// replay. And it may name rooms the connection is not in — replay is therefore
// driven by the rooms this connection has actually JOINED, intersected with the
// cursor, never by the room list the cursor happens to contain. Without that
// intersection, naming any room id in a cursor would read that room's history.
//
// Returns the number of messages delivered.
func (m *manager) Replay(ctx context.Context, connID, cursorToken string) (int, error) {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return 0, err
	}

	if !m.config.EnableMessageHistory {
		return 0, ErrHistoryDisabled
	}

	cursor, err := decodeReplayCursor(cursorToken)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errMalformedCursor, err)
	}

	if len(cursor) == 0 {
		// No cursor means a first connection, not "from the beginning of time".
		// Replaying everything here would turn every fresh connect into a full
		// history dump of every room the client joins.
		return 0, nil
	}

	delivered := 0

	for _, roomID := range conn.GetJoinedRooms() {
		after, wanted := cursor[roomID]
		if !wanted {
			// The client holds no position in this room, so it has no gap to
			// fill — it is either newly joined or was never caught up. Sending
			// history here would be the same unbounded dump as above.
			continue
		}

		missed, err := m.messageStore.GetSince(ctx, roomID, after, defaultReplayLimitPerRoom)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("replay: failed to read room backlog",
					forge.F("conn_id", connID),
					forge.F("room_id", roomID),
					forge.F("error", err),
				)
			}

			continue
		}

		for _, msg := range missed {
			if err := m.deliverToConnection(ctx, conn, msg); err != nil {
				if m.logger != nil {
					m.logger.Debug("replay: delivery failed",
						forge.F("conn_id", connID),
						forge.F("room_id", roomID),
						forge.F("error", err),
					)
				}

				break
			}

			delivered++
		}
	}

	if m.metrics != nil && delivered > 0 {
		m.metrics.Counter("streaming.replay.messages").Add(float64(delivered))
		m.metrics.Counter("streaming.replay.sessions").Inc()
	}

	if m.logger != nil && delivered > 0 {
		m.logger.Debug("replayed missed messages",
			forge.F("conn_id", connID),
			forge.F("count", delivered),
		)
	}

	return delivered, nil
}
