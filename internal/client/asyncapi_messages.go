package client

import (
	"fmt"
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
func operationMessages(channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) (spoken []spokenMessage, unresolved bool) {
	wanted := make(map[string]struct{})
	listed := operation != nil && len(operation.Messages) > 0

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

	// Nothing resolved under this channel, so try the trailing segment of each
	// reference as a channel message key. AsyncAPI 3 says an operation's
	// `messages` point at the channel's own, and forge's router obeys that;
	// other generators write `#/components/messages/send` and leave the
	// channel to carry the definition. Under the strict read alone that
	// document resolves nothing, both operations fall back to the whole
	// channel, and the fold labels the two directions with whichever action
	// came last -- the unnamed direction this whole path exists to prevent.
	// The segment has to be a key the channel actually declares, so this can
	// only ever narrow the fallback, never invent a message.
	if listed && len(wanted) == 0 {
		for _, ref := range operation.Messages {
			key := ref.Ref[strings.LastIndex(ref.Ref, "/")+1:]
			if _, declared := channel.Messages[key]; declared {
				wanted[key] = struct{}{}
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

	return out, listed && len(wanted) == 0
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
func applyOperationMessages(spec *APISpec, opID string, ws *WebSocketEndpoint, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation, convert func(*shared.Schema) *Schema) {
	if ws.MessageTypes == nil {
		ws.MessageTypes = make(map[string]*Schema)
	}

	names, _ := ws.Metadata["messages"].(map[string]string)
	if names == nil {
		names = make(map[string]string)
		ws.Metadata["messages"] = names
	}

	spokenMessages, unresolved := operationMessages(channel, operation)

	// The fallback is a guess, and a wrong guess here produces a client that
	// compiles and speaks the wrong message on one direction. Nothing about
	// the output looks wrong, so this is the only place a reader finds out.
	if unresolved && spec != nil {
		spec.Warnings = append(spec.Warnings, fmt.Sprintf(
			"operation %q lists messages, none of which resolve to a message of channel %q; "+
				"it was folded as if it spoke the whole channel, so its direction may claim a message "+
				"the other direction sends",
			opID, channel.Address,
		))
	}

	for _, spoken := range spokenMessages {
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

		// A name the opposite direction already claimed stands. Two operations
		// that both speak the whole channel otherwise relabel each other, and
		// whichever sorted last owned every name -- which is how a duplex
		// binding ended up with one direction empty. First claim wins, so the
		// result no longer depends on operation-id sort order.
		if claimed, ok := names[spoken.name]; ok && claimed != operation.Action {
			continue
		}

		names[spoken.name] = operation.Action
	}
}
