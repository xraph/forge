package client

import (
	"strings"

	"github.com/xraph/forge/internal/shared"
)

// spokenMessage is one channel message an operation sends or receives.
type spokenMessage struct {
	// key is the message's key under channels.<name>.messages.
	key string
	// name is what the generated binding calls it: the message's own `name`
	// when it carries one, the key otherwise.
	name string
	msg  *shared.AsyncAPIMessage
}

// operationMessages returns the channel messages an operation speaks, in
// sorted key order.
//
// AsyncAPI 3 lets an operation list the subset of the channel's messages it
// carries, as `#/channels/<channel>/messages/<key>` references. When it does,
// only those count: a duplex channel declares one message per direction and
// one operation per direction, and stamping every message with every
// operation's action is what left the send direction unnamed and the receive
// schema holding the send payload. An operation that lists no messages, or
// whose references all point outside the channel, speaks the whole channel,
// which is what every document generated before operations carried
// `messages` relied on.
func operationMessages(channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) []spokenMessage {
	wanted := make(map[string]struct{})

	if operation != nil && operation.Channel != nil {
		prefix := operation.Channel.Ref + "/messages/"

		for _, ref := range operation.Messages {
			if key, ok := strings.CutPrefix(ref.Ref, prefix); ok {
				if _, declared := channel.Messages[key]; declared {
					wanted[key] = struct{}{}
				}
			}
		}
	}

	out := make([]spokenMessage, 0, len(channel.Messages))

	for _, key := range sortedStringKeys(channel.Messages) {
		if _, ok := wanted[key]; len(wanted) > 0 && !ok {
			continue
		}

		msg := channel.Messages[key]

		name := msg.Name
		if name == "" {
			name = key
		}

		out = append(out, spokenMessage{key: key, name: name, msg: msg})
	}

	return out
}

// applyOperationMessages records one operation's direction on the endpoint:
// the payload of the first message it speaks becomes that direction's schema
// if nothing claimed it yet, every payload lands in MessageTypes under its
// key, and Metadata["messages"] maps each message NAME to the direction, which
// is where the generated duplex binding reads `send` and `receive` from.
//
// Both readers of an AsyncAPI document call this: once when the first
// operation on a channel creates the endpoint, and again for every later
// operation folded into it. Keeping the fold in one place is the point; the
// two readers used to disagree, and the URL one never folded at all.
func applyOperationMessages(ws *WebSocketEndpoint, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation, convert func(*shared.Schema) *Schema) {
	if ws.MessageTypes == nil {
		ws.MessageTypes = make(map[string]*Schema)
	}

	names, _ := ws.Metadata["messages"].(map[string]string)
	if names == nil {
		names = make(map[string]string)
		ws.Metadata["messages"] = names
	}

	for _, spoken := range operationMessages(channel, operation) {
		if spoken.msg.Payload == nil {
			continue
		}

		schema := convert(spoken.msg.Payload)
		ws.MessageTypes[spoken.key] = schema

		switch operation.Action {
		case "send":
			if ws.SendSchema == nil {
				ws.SendSchema = schema
			}
		case "receive":
			if ws.ReceiveSchema == nil {
				ws.ReceiveSchema = schema
			}
		}

		names[spoken.name] = operation.Action
	}
}
