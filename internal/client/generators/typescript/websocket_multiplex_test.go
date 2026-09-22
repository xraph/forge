package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// multiplexedSpec is a channel that sends two message types and receives one,
// with every type a component so each has a codec.
func multiplexedSpec() *client.APISpec {
	obj := func(field, typ string) *client.Schema {
		return &client.Schema{Type: "object", Properties: map[string]*client.Schema{field: {Type: typ}}}
	}
	ref := func(name string) *client.Schema { return &client.Schema{Ref: "#/components/schemas/" + name} }

	return &client.APISpec{
		Schemas: map[string]*client.Schema{
			"Say":    obj("text_body", "string"),
			"Typing": obj("is_on", "boolean"),
			"Said":   obj("id", "string"),
		},
		WebSockets: []client.WebSocketEndpoint{{
			ID: "chat", Path: "/ws/chat",
			SendSchema:      ref("Say"),
			ReceiveSchema:   ref("Said"),
			SendMessages:    map[string]*client.Schema{"say": ref("Say"), "typed": ref("Typing")},
			ReceiveMessages: map[string]*client.Schema{"said": ref("Said")},
		}},
	}
}

// A direction carrying several message types is typed as their union, in a
// fixed order, rather than as whichever message the parser settled on.
func TestWebSocketDirectionIsTypedAsAUnion(t *testing.T) {
	out, _ := NewWebSocketGenerator().Generate(multiplexedSpec(), client.GeneratorConfig{Language: "typescript"})

	for _, want := range []string{
		"async send(message: types.Say | types.Typing): Promise<void>",
		"sendSync(message: types.Say | types.Typing): void",
		"onMessage(handler: (message: types.Said) => void): void",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("websocket.ts missing %q", want)
		}
	}

	if t.Failed() {
		t.Logf("\n%s", out)
	}
}

// One call site encodes every outgoing frame, so a direction whose messages
// resolve to different codecs gets none, said out loud; a direction whose
// messages agree gets the one they agree on.
func TestWebSocketDirectionCodecFollowsAgreement(t *testing.T) {
	out, warnings := NewWebSocketGenerator().Generate(multiplexedSpec(), client.GeneratorConfig{Language: "typescript"})

	if strings.Contains(out, `encode(message, "Say")`) || strings.Contains(out, `encode(message, "Typing")`) {
		t.Errorf("send direction encoded through one of two disagreeing codecs:\n%s", out)
	}

	if !strings.Contains(out, `decode(JSON.parse(data), "Said")`) {
		t.Errorf("receive direction, whose one message has a codec, is not decoded:\n%s", out)
	}

	var named bool

	for _, w := range warnings {
		if strings.Contains(w, "send messages resolve to 2 different codecs (Say, Typing)") {
			named = true
		}
	}

	if !named {
		t.Errorf("no warning names the disagreeing send codecs; warnings = %v", warnings)
	}
}
